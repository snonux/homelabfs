----------------------------- MODULE Takeover -----------------------------
EXTENDS Integers, FiniteSets, TLC

(* One closed volume, one immutable old slot, one successor mutation. Complete
   records stand for bytes plus their dependency/result graph, never hashes.
   Epochs identify allocated attempts; neither time nor UUID order fences a VM. *)
CONSTANTS MaxEpoch, MaxControllerCrashes, AllowRAMLoss, AllowStorageLoss,
          AllowEnvironmentFaults, Bug
Nodes == {"A", "B", "C"}
Pair == {"B", "C"}
VARIABLES old, replica, controller, fence, environment, observation
vars == <<old, replica, controller, fence, environment, observation>>

Init ==
 /\ old = [candidate |-> FALSE, durable |-> FALSE, lost |-> FALSE,
            ackSent |-> FALSE, ackSeen |-> FALSE, replied |-> FALSE,
            admissions |-> TRUE, running |-> TRUE, returned |-> FALSE]
 /\ replica = [memory |-> FALSE, durable |-> FALSE, continuous |-> TRUE,
                incarnation |-> 0, ramLostTail |-> FALSE,
                promises |-> [n \in Pair |-> 0], pairDurable |-> FALSE,
                installed |-> FALSE, view |-> FALSE]
 /\ controller = [epoch |-> 0, incarnation |-> 0, step |-> 0,
                   alive |-> TRUE, crashes |-> 0, owner |-> "B",
                   selected |-> -1, boundIncarnation |-> 0]
 /\ fence = [inhibited |-> FALSE, stopped |-> FALSE, attestedEpoch |-> 0]
 /\ environment = [ownerReachable |-> TRUE, coordination |-> TRUE,
                    management |-> TRUE, paused |-> FALSE]
 /\ observation = [newWrite |-> FALSE, staleRead |-> FALSE,
                    staleAccept |-> FALSE, doubleActivation |-> FALSE,
                    rejected |-> FALSE, caughtUp |-> FALSE, readSeen |-> FALSE]

(* A may propose before its local sync. This models the old ACCEPT/promise race;
   detailed MUTATION/COMMIT/result sequencing is checked separately by fq3. *)
Propose ==
 /\ old.running /\ old.admissions /\ ~old.lost /\ ~old.candidate
 /\ ~environment.paused
 /\ old' = [old EXCEPT !.candidate = TRUE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>
OwnerSync ==
 /\ old.candidate /\ ~old.lost /\ ~old.durable /\ old.running
 /\ ~environment.paused
 /\ old' = [old EXCEPT !.durable = TRUE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>

(* The same old-epoch frame can be delivered indefinitely, including after a
   restart/promise. Reject does not erase a pre-promise accepted candidate. *)
AcceptOld ==
 /\ old.candidate /\ environment.ownerReachable /\ controller.step < 6
 /\ (Bug = "stale-epoch" \/
       (replica.promises["B"] = 0 /\ replica.incarnation = 0))
 /\ replica' = [replica EXCEPT !.memory = TRUE]
 /\ old' = [old EXCEPT !.ackSent = TRUE]
 /\ observation' = [observation EXCEPT !.staleAccept = @ \/
          replica.promises["B"] > 0 \/ replica.incarnation > 0]
 /\ UNCHANGED <<controller, fence, environment>>
RejectOld ==
 /\ old.candidate /\ (replica.promises["B"] > 0 \/ replica.incarnation > 0)
 /\ observation' = [observation EXCEPT !.rejected = TRUE]
 /\ UNCHANGED <<old, replica, controller, fence, environment>>
DeliverOldACK ==
 /\ old.ackSent /\ environment.ownerReachable /\ old.running
 /\ old' = [old EXCEPT !.ackSeen = TRUE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>
OldReply ==
 /\ old.running /\ old.admissions /\ ~environment.paused
 /\ old.durable /\ old.ackSeen
 /\ old' = [old EXCEPT !.replied = TRUE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>
PartnerSync ==
 /\ replica.memory /\ ~replica.durable
 /\ replica' = [replica EXCEPT !.durable = TRUE]
 /\ UNCHANGED <<old, controller, fence, environment, observation>>

(* Deliberately NO atomic notification to A. An emitted ACK can arrive after
   RAM loss. A's durable copy still protects any reply; losing it as well is the
   declared gap. A shorter restarted tail NEVER provides absence evidence. *)
LoseRAM ==
 /\ AllowRAMLoss /\ replica.incarnation = 0 /\ controller.step < 6
 /\ replica' = [replica EXCEPT
       !.incarnation = 1, !.memory = replica.durable,
       !.continuous = FALSE,
       !.ramLostTail = replica.memory /\ ~replica.durable,
       !.pairDurable = FALSE, !.installed = FALSE, !.view = FALSE]
 /\ UNCHANGED <<old, controller, fence, environment, observation>>
LoseOwnerStorage ==
 /\ AllowStorageLoss /\ ~old.lost /\ controller.step < 6
 /\ old' = [old EXCEPT !.lost = TRUE, !.durable = FALSE,
                          !.admissions = FALSE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>

Allocate(n) ==
 /\ controller.alive /\ controller.step = 0 /\ environment.coordination
 /\ controller' = [controller EXCEPT !.step = 1, !.epoch = 1,
       !.owner = n, !.boundIncarnation = replica.incarnation]
 /\ UNCHANGED <<old, replica, fence, environment, observation>>
(* An immutable allocated incarnation is never edited on reconnect. A restart
   abandons its attempt; a higher authorized attempt reconciles durable state. *)
Reallocate ==
 /\ controller.alive /\ environment.coordination
 /\ controller.step \in 1..5 /\ controller.epoch < MaxEpoch
 /\ controller.boundIncarnation # replica.incarnation
 /\ controller' = [controller EXCEPT !.epoch = @ + 1, !.step = 1,
                                     !.boundIncarnation = replica.incarnation]
 /\ replica' = [replica EXCEPT !.pairDurable = FALSE,
                               !.installed = FALSE, !.view = FALSE]
 /\ UNCHANGED <<old, fence, environment, observation>>
Promise(n) ==
 /\ controller.alive /\ environment.coordination /\ controller.step \in 1..5
 /\ controller.boundIncarnation = replica.incarnation
 /\ replica.promises[n] < controller.epoch
 /\ replica' = [replica EXCEPT !.promises[n] = controller.epoch,
       !.durable = IF n = "B" THEN replica.memory ELSE replica.durable]
 /\ UNCHANGED <<old, controller, fence, environment, observation>>
Quorum == \A n \in Pair: replica.promises[n] = controller.epoch

(* native-vm-stop-v1: durable launch inhibition precedes graceful ACPI stop.
   Cache rights survive daemon gating, partitions and arbitrary pause duration.
   A paused/hung target or unavailable management has no successful stop action. *)
Inhibit ==
 /\ controller.alive /\ controller.step > 0 /\ environment.management
 /\ ~fence.inhibited
 /\ fence' = [fence EXCEPT !.inhibited = TRUE]
 /\ UNCHANGED <<old, replica, controller, environment, observation>>
GracefulStop ==
 /\ fence.inhibited /\ environment.management /\ ~environment.paused
 /\ old.running
 /\ (~old.candidate \/ old.lost \/ old.durable \/ replica.durable)
 /\ old' = [old EXCEPT !.running = FALSE, !.admissions = FALSE]
 /\ fence' = [fence EXCEPT !.stopped = TRUE]
 /\ UNCHANGED <<replica, controller, environment, observation>>
Attest ==
 /\ controller.alive /\ controller.step > 0 /\ environment.management
 /\ fence.inhibited /\ fence.stopped /\ ~old.running
 /\ fence' = [fence EXCEPT !.attestedEpoch = controller.epoch]
 /\ UNCHANGED <<old, replica, controller, environment, observation>>
Fenced == fence.inhibited /\ fence.stopped /\ ~old.running
          /\ fence.attestedEpoch = controller.epoch

(* Explicit recovery traffic, not a restart notification or empty-tail guess.
   GracefulStop preserved every proposed bundle on A or B; with A's intact,
   positively closed history in hand the merged complete history can restore
   B's continuity even when both histories contain only the initial baseline. *)
RecoverFromOwner ==
 /\ controller.alive /\ controller.step \in 1..5 /\ Quorum /\ Fenced
 /\ controller.boundIncarnation = replica.incarnation
 /\ old.returned /\ environment.ownerReachable /\ ~old.lost
 /\ ~replica.continuous
 /\ replica' = [replica EXCEPT !.continuous = TRUE,
       !.memory = @ \/ (old.durable /\ controller.selected # 0),
       !.durable = @ \/ (old.durable /\ controller.selected # 0)]
 /\ UNCHANGED <<old, controller, fence, environment, observation>>

HasSource == replica.memory \/ replica.durable \/
             (old.returned /\ environment.ownerReachable
              /\ old.durable /\ ~old.lost)
(* A source is actual complete bytes. No ACK, timeout, owner silence, new UUID
   or new B incarnation proves those bytes absent. *)
Absence == replica.continuous /\ ~replica.memory /\ ~replica.durable
Select ==
 /\ controller.alive /\ controller.step = 1 /\ Quorum
 /\ controller.boundIncarnation = replica.incarnation
 /\ (Fenced \/ Bug = "skip-fence")
 /\ (HasSource \/ Absence \/ Bug = "restart-absence")
 /\ controller' = [controller EXCEPT !.step = 2,
       !.selected = IF controller.selected # -1 THEN controller.selected
                     ELSE IF HasSource THEN 1 ELSE 0]
 /\ UNCHANGED <<old, replica, fence, environment, observation>>
ProtectPair ==
 /\ controller.alive /\ controller.step = 2 /\ Quorum
 /\ controller.boundIncarnation = replica.incarnation
 /\ (controller.selected = 0 \/ HasSource)
 /\ replica' = [replica EXCEPT !.pairDurable = TRUE,
       !.memory = @ \/ controller.selected = 1,
       !.durable = @ \/ controller.selected = 1]
 /\ controller' = [controller EXCEPT !.step = 3]
 /\ UNCHANGED <<old, fence, environment, observation>>
Install ==
 /\ controller.alive /\ controller.step = 3 /\ replica.pairDurable /\ Quorum
 /\ controller.boundIncarnation = replica.incarnation
 /\ replica' = [replica EXCEPT !.installed = TRUE]
 /\ controller' = [controller EXCEPT !.step = 4]
 /\ UNCHANGED <<old, fence, environment, observation>>
ApplyView ==
 /\ controller.alive /\ controller.step = 4 /\ replica.installed
 /\ replica.pairDurable /\ controller.boundIncarnation = replica.incarnation
 /\ replica' = [replica EXCEPT !.view = TRUE]
 /\ controller' = [controller EXCEPT !.step = 5]
 /\ UNCHANGED <<old, fence, environment, observation>>
Activate ==
 /\ controller.alive /\ controller.step = 5 /\ environment.coordination
 /\ Quorum /\ replica.pairDurable /\ replica.installed /\ replica.view
 /\ controller.boundIncarnation = replica.incarnation
 /\ (Fenced \/ Bug = "skip-fence")
 /\ controller' = [controller EXCEPT !.step = 6]
 /\ observation' = [observation EXCEPT !.doubleActivation = old.running]
 /\ UNCHANGED <<old, replica, fence, environment>>
SuccessorWrite ==
 /\ controller.step = 6 /\ ~observation.newWrite
 /\ observation' = [observation EXCEPT !.newWrite = TRUE]
 /\ UNCHANGED <<old, replica, controller, fence, environment>>
SuccessorRead ==
 /\ controller.step = 6
 /\ observation' = [observation EXCEPT !.readSeen = TRUE,
       !.staleRead = @ \/ (old.replied /\ controller.selected # 1)]
 /\ UNCHANGED <<old, replica, controller, fence, environment>>
(* Models a successful old cached read/mmap load bypassing the daemon gate. *)
CachedRead ==
 /\ old.running /\ ~environment.paused /\ observation.newWrite
 /\ observation' = [observation EXCEPT !.staleRead = TRUE]
 /\ UNCHANGED <<old, replica, controller, fence, environment>>
ReturnFormerOwner ==
 /\ Fenced /\ ~old.returned
 /\ old' = [old EXCEPT !.returned = TRUE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>
CatchupFormerOwner ==
 /\ old.returned /\ controller.step = 6
 /\ observation' = [observation EXCEPT !.caughtUp = TRUE]
 /\ UNCHANGED <<old, replica, controller, fence, environment>>

DetectAuthorityLoss ==
 /\ old.running /\ ~environment.paused
 /\ ~environment.coordination /\ old.admissions
 /\ old' = [old EXCEPT !.admissions = FALSE]
 /\ UNCHANGED <<replica, controller, fence, environment, observation>>

CrashController ==
 /\ controller.alive /\ controller.crashes < MaxControllerCrashes
 /\ controller' = [controller EXCEPT !.alive = FALSE, !.crashes = @ + 1]
 /\ UNCHANGED <<old, replica, fence, environment, observation>>
RestartController ==
 /\ ~controller.alive
 /\ controller' = [controller EXCEPT !.alive = TRUE, !.incarnation = @ + 1]
 /\ UNCHANGED <<old, replica, fence, environment, observation>>
ChangeEnvironment ==
 /\ AllowEnvironmentFaults
 /\ \E e \in [ownerReachable : BOOLEAN, coordination : BOOLEAN,
               management : BOOLEAN, paused : BOOLEAN]: environment' = e
 /\ UNCHANGED <<old, replica, controller, fence, observation>>

Next == \/ Propose \/ OwnerSync \/ AcceptOld \/ RejectOld \/ DeliverOldACK
        \/ OldReply \/ PartnerSync \/ LoseRAM \/ LoseOwnerStorage
        \/ (\E n \in Pair: Allocate(n) \/ Promise(n)) \/ Reallocate
        \/ Inhibit \/ GracefulStop \/ Attest \/ Select \/ ProtectPair
        \/ RecoverFromOwner
        \/ Install \/ ApplyView \/ Activate \/ SuccessorWrite \/ SuccessorRead
        \/ CachedRead
        \/ ReturnFormerOwner \/ CatchupFormerOwner
        \/ DetectAuthorityLoss
        \/ CrashController \/ RestartController \/ ChangeEnvironment
Spec == Init /\ [][Next]_vars

TypeOK ==
 /\ controller.step \in 0..6 /\ controller.epoch \in 0..MaxEpoch
 /\ controller.selected \in {-1, 0, 1} /\ controller.owner \in Pair
 /\ controller.crashes \in 0..MaxControllerCrashes
 /\ replica.incarnation \in 0..1
 /\ replica.promises \in [Pair -> 0..MaxEpoch]
 /\ \A n \in Pair: replica.promises[n] <= controller.epoch
 /\ replica.durable => replica.memory
 /\ old.durable => ~old.lost
NoDoubleActivation == ~observation.doubleActivation
NoStaleRead == ~observation.staleRead
OldEpochRejected == ~observation.staleAccept
(* This is a physical survival invariant, independent of what A knew at reply.
   The loss exception requires BOTH permanent A storage loss and B RAM loss of
   an accepted tail before persistence. Merely unreachable A is insufficient. *)
DeclaredGap == old.lost /\ replica.ramLostTail
PreservedAcknowledgement == old.replied =>
    old.durable \/ replica.memory \/ replica.durable \/
    (replica.pairDurable /\ controller.selected = 1) \/ DeclaredGap
NoStaleSelection == controller.step >= 2 /\ old.replied => controller.selected = 1
PreservedAcceptance == controller.step >= 2 /\ replica.memory
                       => controller.selected = 1
ActivationGate == controller.step = 6 =>
    /\ (Fenced \/ Bug = "skip-fence") /\ Quorum
    /\ replica.pairDurable /\ replica.installed /\ replica.view
    /\ controller.boundIncarnation = replica.incarnation
NoFailback == old.returned => ~old.running /\ ~old.admissions
Safety == TypeOK /\ NoDoubleActivation /\ NoStaleRead /\ OldEpochRejected
          /\ PreservedAcknowledgement /\ NoStaleSelection /\ ActivationGate
          /\ PreservedAcceptance /\ NoFailback

DurableStateNeverRegresses == [][
 /\ \A n \in Pair: replica.promises'[n] >= replica.promises[n]
 /\ controller.epoch' >= controller.epoch
 /\ (controller.selected # -1 => controller.selected' = controller.selected)
 /\ (fence.inhibited => fence.inhibited')
 /\ (fence.stopped => fence.stopped')
 /\ (old.replied => old.replied')
]_vars

(* Expected reachability counterexamples: these controls assert a useful
   schedule is absent and TLC must refute that assertion while Safety holds. *)
CompletedWitness(n) == controller.owner = n /\ controller.step = 6
    /\ controller.selected = 1 /\ old.replied /\ observation.newWrite
    /\ observation.readSeen /\ observation.caughtUp
WitnessAbsent == CASE
 Bug = "witness-sync" -> ~(CompletedWitness("B") /\ replica.incarnation = 1)
 [] Bug = "witness-async" -> ~CompletedWitness("C")
 [] Bug = "witness-gap" -> ~(old.replied /\ DeclaredGap /\ ~HasSource
       /\ ~replica.continuous /\ controller.step = 1 /\ Quorum /\ Fenced
       /\ controller.boundIncarnation = replica.incarnation)
 [] Bug = "witness-intact-wait" -> ~(old.replied /\ ~old.lost
       /\ ~environment.ownerReachable /\ replica.ramLostTail /\ ~HasSource
       /\ ~replica.continuous /\ controller.step = 1 /\ Quorum /\ Fenced
       /\ controller.boundIncarnation = replica.incarnation)
 [] OTHER -> TRUE

(* Conditional progress assumes stable management/coordination/data access and
   a stoppable target. No condition asks for eventual selection or activation.
   RAM loss/storage loss remain finite optional faults. Permanent source loss
   without a complete source/continuous absence deliberately has no liveness
   promise. Fairness applies to concrete enabled workers and real source reads. *)
StableEnvironment == <>[](environment.coordination /\ environment.management
                         /\ environment.ownerReachable /\ ~environment.paused)
Workers ==
 /\ WF_vars(RestartController) /\ WF_vars(Reallocate)
 /\ WF_vars(OwnerSync)
 /\ WF_vars(\E n \in Pair: Allocate(n))
 /\ \A n \in Pair: WF_vars(Promise(n))
 /\ WF_vars(Inhibit) /\ WF_vars(GracefulStop) /\ WF_vars(Attest)
 /\ WF_vars(RecoverFromOwner)
 /\ WF_vars(Select) /\ WF_vars(ProtectPair) /\ WF_vars(Install)
 /\ WF_vars(ApplyView) /\ WF_vars(Activate)
 /\ WF_vars(ReturnFormerOwner) /\ WF_vars(CatchupFormerOwner)
LiveSpec == Spec /\ StableEnvironment /\ Workers
TakeoverProgress == <> (controller.step = 6)
FormerOwnerProgress == (controller.step = 6) ~> observation.caughtUp
=============================================================================
