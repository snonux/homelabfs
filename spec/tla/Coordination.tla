--------------------------- MODULE Coordination ---------------------------
EXTENDS Integers, FiniteSets, TLC

(* Allocation is an infrequent atomic etcd transaction, never a serving grant.
   Complete validated records abstract encoding, RBAC and the Raft implementation.
   Restore is an operator-controlled offline boundary, not an ordinary API. *)
CONSTANTS Profile, Bug, MaxAllocations, MaxPolicyWrites, AllowCrash, RestoreKind
Controllers == {1, 2}
Volumes == IF Profile = "restore" THEN {1} ELSE {1, 2}
Configurations == 1..3
EmptyGrant == [revision |-> 0, epoch |-> 0, term |-> 0,
               configuration |-> 0, owner |-> 0, incarnation |-> 0, policyRevision |-> 0]
EmptyController == [alive |-> TRUE, incarnation |-> 0, phase |-> "idle",
  volume |-> 1, configuration |-> 1, policy |-> 0, floor |-> 0,
  previous |-> EmptyGrant]
InitialStore == [revision |-> 1, policy |-> 1, registry |-> 1, floor |-> 1,
  grants |-> [v \in Volumes |-> EmptyGrant],
  reservations |-> [k \in Configurations |-> [volume |-> 0, grant |-> EmptyGrant]]]
FreshStore == [InitialStore EXCEPT !.revision = 0, !.policy = 0, !.registry = 0]

VARIABLES store, controller, era, allocations, policyWrites, attempts, policies,
          crashed, restored, storage, observation
vars == <<store, controller, era, allocations, policyWrites, attempts, policies,
          crashed, restored, storage, observation>>
Init ==
 /\ store = InitialStore
 /\ controller = [c \in Controllers |-> EmptyController]
 /\ era = 0 /\ allocations = 0 /\ policyWrites = 0
 /\ attempts = {} /\ policies = {} /\ crashed = {} /\ restored = FALSE
 /\ storage = [v \in Volumes |-> 0]
 /\ observation = [collision |-> FALSE, lostReply |-> FALSE,
      reconciled |-> FALSE, superseded |-> FALSE, staleBlocked |-> FALSE,
      promised |-> [v \in Volumes |-> 0], serving |-> FALSE,
      oldServing |-> TRUE, oldCache |-> TRUE]

(* PutPolicy's pre-read and final transaction compare the same exact modrev.
   Nondeterministic expected values include zero and stale revisions. Registry
   revision and etcd key modification revision are deliberately distinct. *)
PutPolicy(expected, registry, floor) ==
 /\ Profile # "restore" \/ store.policy = 0
 /\ policyWrites < MaxPolicyWrites
 /\ (Bug = "policy-cas" \/ expected = store.policy)
 /\ (store.policy = 0 \/ Bug = "registry" \/ registry > store.registry)
 /\ store' = [store EXCEPT !.revision = @ + 1, !.policy = store.revision + 1,
                             !.registry = registry, !.floor = floor]
 /\ policies' = policies \cup {[era |-> era, revision |-> store.revision + 1,
       expected |-> expected, oldRevision |-> store.policy,
       registry |-> registry, oldRegistry |-> store.registry]}
 /\ policyWrites' = policyWrites + 1
 /\ UNCHANGED <<controller, era, allocations, attempts, crashed, restored,
                 storage, observation>>
Read(c, v, configuration) ==
 /\ Profile # "policy" /\ controller[c].alive
 /\ controller[c].phase = "idle" /\ store.policy > 0
 /\ allocations < MaxAllocations
 /\ controller' = [controller EXCEPT ![c].phase = "prepared", ![c].volume = v,
       ![c].configuration = configuration, ![c].policy = store.policy,
       ![c].floor = store.floor, ![c].previous = store.grants[v]]
 /\ UNCHANGED <<store, era, allocations, policyWrites, attempts, policies,
                 crashed, restored, storage, observation>>

Commit(c, loseReply) ==
 LET request == controller[c]
     v == request.volume
     previous == request.previous
     epoch == IF Bug = "epoch" THEN request.floor + 1
              ELSE (IF previous.epoch > request.floor THEN previous.epoch
                    ELSE request.floor) + 1
     term == (IF previous.term > request.floor + 1 THEN previous.term
              ELSE request.floor + 1) + 1
     grant == [revision |-> store.revision + 1, epoch |-> epoch, term |-> term,
       configuration |-> request.configuration, owner |-> c,
       incarnation |-> request.incarnation, policyRevision |-> request.policy]
 IN
 /\ request.alive /\ request.phase = "prepared"
 /\ allocations < MaxAllocations
 /\ (Bug = "allocation-policy" \/ request.policy = store.policy)
 /\ (Bug = "grant-cas" \/ previous.revision = store.grants[v].revision)
 /\ (Bug = "reservation" \/ store.reservations[request.configuration].grant.revision = 0)
 /\ store' = [store EXCEPT !.revision = @ + 1, !.grants[v] = grant,
       !.reservations[request.configuration] = [volume |-> v, grant |-> grant]]
 /\ controller' = [controller EXCEPT ![c].phase =
       IF loseReply THEN "unknown" ELSE "committed"]
 /\ allocations' = allocations + 1
 /\ attempts' = attempts \cup {[era |-> era, volume |-> v, grant |-> grant,
       predecessor |-> previous.revision, previousEpoch |-> store.grants[v].epoch,
       previousTerm |-> store.grants[v].term,
       policy |-> request.policy, actualPolicy |-> store.policy, floor |-> request.floor]}
 /\ observation' = [observation EXCEPT
       !.collision = @ \/ store.reservations[request.configuration].grant.revision > 0,
       !.lostReply = @ \/ loseReply,
       !.superseded = @ \/ store.grants[v].revision > 0,
       !.serving = @ \/ Bug = "allocation-authority"]
 /\ UNCHANGED <<era, policyWrites, policies, crashed, restored, storage>>

(* Pending requests can be discarded on conflict; committed replies may be
   forgotten. Quorum-backed reconciliation observes the actual latest grant,
   including a superseding attempt, rather than inventing a predecessor. *)
Forget(c) ==
 /\ controller[c].alive /\ controller[c].phase # "idle"
 /\ controller' = [controller EXCEPT ![c].phase = "idle"]
 /\ UNCHANGED <<store, era, allocations, policyWrites, attempts, policies,
                 crashed, restored, storage, observation>>
Reconcile(c) ==
 /\ controller[c].alive /\ controller[c].phase = "unknown"
 /\ controller' = [controller EXCEPT ![c].phase = "idle",
       ![c].previous = store.grants[controller[c].volume]]
 /\ observation' = [observation EXCEPT !.reconciled = TRUE]
 /\ UNCHANGED <<store, era, allocations, policyWrites, attempts, policies,
                 crashed, restored, storage>>
Crash(c) ==
 /\ AllowCrash /\ controller[c].alive /\ c \notin crashed
 /\ controller' = [controller EXCEPT ![c].alive = FALSE, ![c].phase = "idle"]
 /\ crashed' = crashed \cup {c}
 /\ UNCHANGED <<store, era, allocations, policyWrites, attempts, policies,
                 restored, storage, observation>>
Restart(c) ==
 /\ ~controller[c].alive
 /\ controller' = [controller EXCEPT ![c] =
       [EmptyController EXCEPT !.incarnation = controller[c].incarnation + 1]]
 /\ UNCHANGED <<store, era, allocations, policyWrites, attempts, policies,
                 crashed, restored, storage, observation>>

(* Storage promises are external durable floors, not etcd revisions. This
   boundary only rejects stale attempts; it does not implement takeover. *)
Promise(v) ==
 /\ Profile = "restore" /\ store.grants[v].revision > 0
 /\ store.grants[v].epoch > storage[v]
 /\ storage' = [storage EXCEPT ![v] = store.grants[v].epoch]
 /\ observation' = [observation EXCEPT !.promised[v] = store.grants[v].epoch]
 /\ UNCHANGED <<store, controller, era, allocations, policyWrites, attempts,
                 policies, crashed, restored>>
RejectStale(v) ==
 /\ Profile = "restore" /\ restored /\ store.grants[v].revision > 0
 /\ store.grants[v].epoch <= storage[v]
 /\ observation' = [observation EXCEPT !.staleBlocked = TRUE]
 /\ UNCHANGED <<store, controller, era, allocations, policyWrites, attempts,
                 policies, crashed, restored, storage>>
Restore ==
 /\ Profile = "restore" /\ RestoreKind # "none" /\ ~restored
 /\ \A c \in Controllers: ~controller[c].alive
 /\ storage[1] >= 3
 /\ store' = IF RestoreKind = "fresh" THEN FreshStore ELSE InitialStore
 /\ era' = era + 1 /\ restored' = TRUE
 /\ storage' = IF Bug = "restore-authority" THEN [v \in Volumes |-> 0]
               ELSE storage
 /\ UNCHANGED <<controller, allocations, policyWrites, attempts, policies,
                 crashed, observation>>

Next ==
 \/ \E expected \in 0..5, registry \in 1..3, floor \in 1..2:
       PutPolicy(expected, registry, floor)
 \/ \E c \in Controllers, v \in Volumes, configuration \in Configurations:
       Read(c, v, configuration)
 \/ \E c \in Controllers, lost \in BOOLEAN: Commit(c, lost)
 \/ \E c \in Controllers: Forget(c) \/ Reconcile(c) \/ Crash(c) \/ Restart(c)
 \/ \E v \in Volumes: Promise(v) \/ RejectStale(v)
 \/ Restore

ExactPolicyCAS == \A p \in policies: p.expected = p.oldRevision
RegistryIncreases == \A p \in policies:
  p.oldRevision = 0 \/ p.registry > p.oldRegistry
ExactAllocationPolicy == \A a \in attempts: a.policy = a.actualPolicy
NoAllocationFork == \A a, b \in attempts:
  (a.era = b.era /\ a.volume = b.volume /\ a.predecessor = b.predecessor)
   => a = b
EpochTermIncrease == \A a \in attempts:
  a.grant.epoch > a.previousEpoch /\ a.grant.term > a.previousTerm /\
  a.grant.epoch > a.floor /\ a.grant.term > a.floor + 1
LatestSupersession == \A a \in attempts:
  a.era = era =>
    /\ store.grants[a.volume].revision >= a.grant.revision
    /\ store.grants[a.volume].epoch >= a.grant.epoch
    /\ store.grants[a.volume].term >= a.grant.term
ConfigurationUnique ==
 /\ ~observation.collision
 /\ \A a, b \in attempts:
     (a.era = b.era /\ a.grant.configuration = b.grant.configuration) => a = b
AtomicReservation == \A v \in Volumes:
  store.grants[v].revision > 0 =>
    store.reservations[store.grants[v].configuration] =
      [volume |-> v, grant |-> store.grants[v]]
ImmutableHistory == \A a \in attempts:
  a.era = era => store.reservations[a.grant.configuration] =
    [volume |-> a.volume, grant |-> a.grant]
StorageFloor == \A v \in Volumes: storage[v] = observation.promised[v]
NoServingAuthority == ~observation.serving /\ observation.oldServing /\ observation.oldCache
Safety == ExactPolicyCAS /\ RegistryIncreases /\ ExactAllocationPolicy /\
          NoAllocationFork /\ EpochTermIncrease /\ LatestSupersession /\ ConfigurationUnique /\
          AtomicReservation /\ ImmutableHistory /\ StorageFloor /\ NoServingAuthority

Witness == CASE Bug = "witness-competing" ->
    observation.superseded /\ Cardinality({k \in Configurations: store.reservations[k].grant.revision > 0}) >= 2 /\
    \E c, d \in Controllers: c # d /\ controller[c].phase = "prepared" /\
      controller[c].previous.revision # store.grants[controller[c].volume].revision
  [] Bug = "witness-unknown" -> observation.lostReply /\ observation.reconciled
  [] Bug = "witness-restart" ->
       \E a \in attempts: a.grant.incarnation > 0
  [] Bug = "witness-restore" -> restored /\ observation.staleBlocked /\
       store.grants[1].epoch < storage[1] /\ observation.oldCache
  [] Bug = "witness-policy" ->
       \E p \in policies: p.expected > 0 /\ p.registry > p.oldRegistry
  [] OTHER -> FALSE
WitnessAbsent == ~Witness
Spec == Init /\ [][Next]_vars
=============================================================================
