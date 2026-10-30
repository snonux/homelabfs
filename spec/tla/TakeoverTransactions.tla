---------------------- MODULE TakeoverTransactions ----------------------
EXTENDS Integers, FiniteSets, TLC

(* sx3: one already bound transaction in a discovered two-volume closure.
   Complete graph symbols denote original immutable bytes, never just hashes.
   Nodes N0/N1 are the old pair; N1/N2 are the prospective durable pair.
   Both controllers propose from predecessor 0; there is no second transfer. *)
CONSTANTS Initial, AllowFaults, Bug, Witness
Groups == {"A", "B"}
Pair == {"N1", "N2"}
Attempts == {1, 2}
Votes == Groups \X Pair
Values == {"commit", "abort"}
None == "none"
Identity == <<"anchor-A/session/request", "original-request", "bound-A", Groups>>
Result(v) == [identity |-> Identity, outcome |-> v,
              objectAndPositions |-> "original-object-and-positions"]
VARIABLES old, replica, control, recovery, fence, observation
vars == <<old, replica, control, recovery, fence, observation>>

Init ==
 /\ old = [running |-> TRUE, diskLost |-> FALSE,
            prep |-> IF Initial = "pending" THEN {} ELSE Groups,
            candidate |-> IF Initial = "pending" THEN None ELSE "commit",
            synced |-> Initial # "pending",
            protected |-> IF Initial \in {"pending", "candidate"} THEN None ELSE "commit",
            installed |-> CASE Initial = "complete" -> Groups
                          [] Initial = "partial" -> {"A"}
                          [] Initial = "partial-B" -> {"B"} [] OTHER -> {},
            applied |-> CASE Initial = "complete" -> Groups
                        [] Initial = "partial" -> {"A"}
                        [] Initial = "partial-B" -> {"B"} [] OTHER -> {},
            complete |-> Initial = "complete",
            result |-> Result(IF Initial = "complete" THEN "commit" ELSE None)]
 /\ replica = [identity |-> Groups,
                identityDurable |-> IF Initial = "pending" THEN {} ELSE Groups,
                prep |-> IF Initial = "pending" THEN {} ELSE Groups,
                prepDurable |-> {},
                accepted |-> IF Initial \in {"pending", "candidate"} THEN None ELSE "commit",
                captured |-> None,
                continuous |-> TRUE, restarted |-> FALSE,
                promises |-> [v \in Votes |-> 0]]
 /\ control = [proposed |-> {}, allocated |-> {}, rejected |-> {},
                registered |-> {}, alive |-> TRUE, crashed |-> FALSE,
                resumedSelection |-> FALSE,
                boundRestart |-> [i \in Attempts |-> FALSE]]
 /\ recovery = [reports |-> {}, selected |-> [i \in Attempts |-> None],
                 deps |-> [i \in Attempts |-> {}],
                 absence |-> [i \in Attempts |-> FALSE],
                 selectedWithoutOwner |-> [i \in Attempts |-> FALSE],
                 pair |-> {}, installed |-> {}, applied |-> {},
                 barrier |-> {}, complete |-> {}, configuration |-> {},
                 identity |-> [i \in Attempts |-> Identity],
                 result |-> [i \in Attempts |-> Result(None)], active |-> {}]
 /\ fence = [inhibited |-> FALSE, stopped |-> FALSE, attested |-> {}]
 /\ observation = [protected |-> IF Initial \in {"pending", "candidate"} THEN {} ELSE {"commit"},
                    replied |-> {}, retried |-> {},
                    staleAccept |-> FALSE, partialAtAllocation |-> FALSE,
                    competedAtAllocation |-> FALSE,
                    acceptedBeforePromise |-> FALSE, rejectedAfterPromise |-> FALSE]

(* Normal work can race allocation and each group's promise, until positive
   exclusion. Promise and acceptance share each replica's serialized machine. *)
PrepareOwner(g) ==
 /\ old.running /\ ~old.diskLost /\ old.candidate = None
 /\ g \notin old.prep
 /\ old' = [old EXCEPT !.prep = @ \cup {g}]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>
PreparePartner(g) ==
 /\ old.running /\ g \in old.prep \ replica.prep
 /\ ~replica.restarted /\ replica.promises[<<g,"N1">>] = 0
 /\ replica' = [replica EXCEPT !.prep = @ \cup {g}]
 /\ UNCHANGED <<old, control, recovery, fence, observation>>
SyncPartner(g) ==
 /\ g \in replica.identity \ replica.identityDurable
       \/ g \in replica.prep \ replica.prepDurable
 /\ replica' = [replica EXCEPT
       !.identityDurable = IF g \in replica.identity THEN @ \cup {g} ELSE @,
       !.prepDurable = IF g \in replica.prep THEN @ \cup {g} ELSE @]
 /\ UNCHANGED <<old, control, recovery, fence, observation>>
(* A reporter/decision stream can reach stable storage before another volume's
   prepare stream. Its immutable references do not invent those missing bytes.
   The older claim/binding prefix is already durable in these fault inputs. *)
SyncAccepted ==
 /\ AllowFaults /\ replica.accepted # None /\ replica.captured = None
 /\ replica' = [replica EXCEPT !.captured = replica.accepted]
 /\ UNCHANGED <<old, control, recovery, fence, observation>>
Propose(v) ==
 /\ old.running /\ ~old.diskLost /\ old.candidate = None
 /\ v = "abort" \/ (old.prep = Groups /\ replica.prep = Groups)
 /\ old' = [old EXCEPT !.candidate = v]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>
SyncOld ==
 /\ old.running /\ ~old.diskLost /\ old.candidate # None /\ ~old.synced
 /\ old' = [old EXCEPT !.synced = TRUE]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>
AcceptOld ==
 /\ old.candidate # None /\ replica.accepted = None /\ ~replica.restarted
 /\ replica.promises[<<"A","N1">>] = 0 \/ Bug = "stale-accept"
 /\ replica' = [replica EXCEPT !.accepted = old.candidate]
 /\ observation' = [observation EXCEPT
       !.acceptedBeforePromise = replica.promises[<<"A","N1">>] = 0,
       !.staleAccept = replica.promises[<<"A","N1">>] # 0]
 /\ UNCHANGED <<old, control, recovery, fence>>
RejectOld ==
 /\ old.candidate # None /\ replica.promises[<<"A","N1">>] # 0
 /\ ~observation.rejectedAfterPromise
 /\ observation' = [observation EXCEPT !.rejectedAfterPromise = TRUE]
 /\ UNCHANGED <<old, replica, control, recovery, fence>>
ProtectOld ==
 /\ old.running /\ ~old.diskLost /\ old.synced
 /\ replica.accepted = old.candidate /\ old.candidate # None
 /\ old.protected = None
 /\ old' = [old EXCEPT !.protected = old.candidate]
 /\ observation' = [observation EXCEPT !.protected = @ \cup {old.candidate}]
 /\ UNCHANGED <<replica, control, recovery, fence>>
OldInstall(g) ==
 /\ Initial = "pending"
 /\ old.running /\ ~old.diskLost /\ old.protected # None
 /\ g \notin old.installed /\ replica.promises[<<g,"N1">>] = 0
 /\ old' = [old EXCEPT !.installed = @ \cup {g}]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>
OldApply(g) ==
 /\ Initial = "pending"
 /\ old.running /\ ~old.diskLost /\ old.protected = "commit"
 /\ g \in old.installed \ old.applied
 /\ old' = [old EXCEPT !.applied = @ \cup {g}]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>
OldComplete ==
 /\ Initial = "pending"
 /\ old.running /\ ~old.diskLost /\ old.protected # None
 /\ old.installed = Groups /\ (old.protected = "abort" \/ old.applied = Groups)
 /\ \A v \in Votes: replica.promises[v] = 0
 /\ ~old.complete
 /\ old' = [old EXCEPT !.complete = TRUE, !.result = Result(old.protected)]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>

ProposeTakeover(i) ==
 /\ control.alive /\ i \notin control.proposed
 /\ control' = [control EXCEPT !.proposed = @ \cup {i}]
 /\ UNCHANGED <<old, replica, recovery, fence, observation>>
Allocate(i) ==
 /\ control.alive /\ i \in control.proposed \ (control.allocated \cup control.rejected)
 /\ control.allocated = {} \/ Bug = "reservation"
 /\ control' = [control EXCEPT !.allocated = @ \cup {i},
       !.boundRestart[i] = replica.restarted]
 /\ observation' = [observation EXCEPT !.partialAtAllocation = @ \/
       (old.applied # {} /\ old.applied # Groups /\ ~old.complete),
       !.competedAtAllocation = @ \/ control.proposed = Attempts]
 /\ UNCHANGED <<old, replica, recovery, fence>>
RejectTakeover(i) ==
 /\ control.alive /\ i \in control.proposed \ (control.allocated \cup control.rejected)
 /\ control.allocated # {}
 /\ control' = [control EXCEPT !.rejected = @ \cup {i}]
 /\ UNCHANGED <<old, replica, recovery, fence, observation>>
Authorized(i) == control.alive /\ i \in control.allocated
                /\ control.boundRestart[i] = replica.restarted
Promise(i,g,n) ==
 /\ Authorized(i) /\ replica.promises[<<g,n>>] < i
 /\ replica' = [replica EXCEPT !.promises[<<g,n>>] = i,
       !.identityDurable = IF n = "N1" /\ g \in replica.identity THEN @ \cup {g} ELSE @,
       !.prepDurable = IF n = "N1" /\ g \in replica.prep THEN @ \cup {g} ELSE @,
       !.captured = IF n = "N1" /\ g = "A" THEN replica.accepted ELSE @]
 /\ UNCHANGED <<old, control, recovery, fence, observation>>
Report(i,g,n) ==
 /\ Authorized(i) /\ replica.promises[<<g,n>>] = i
 /\ <<i,g,n>> \notin recovery.reports
 /\ recovery' = [recovery EXCEPT !.reports = @ \cup {<<i,g,n>>}]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Quorum(i) == \A v \in Votes: replica.promises[v] = i
Reports(i) == \A g \in Groups, n \in Pair: <<i,g,n>> \in recovery.reports

(* Positive native-vm-stop-v1 assumption: inhibition precedes verified exit.
   This model has no management failure/clock/physical fence implementation. *)
Inhibit ==
 /\ control.alive /\ control.allocated # {} /\ ~fence.inhibited
 /\ fence' = [fence EXCEPT !.inhibited = TRUE]
 /\ UNCHANGED <<old, replica, control, recovery, observation>>
Stop ==
 /\ fence.inhibited /\ old.running
 /\ old.candidate = None \/ old.diskLost \/ old.synced \/ replica.captured # None
 /\ old' = [old EXCEPT !.running = FALSE]
 /\ fence' = [fence EXCEPT !.stopped = TRUE]
 /\ UNCHANGED <<replica, control, recovery, observation>>
Attest(i) ==
 /\ Authorized(i) /\ fence.inhibited /\ fence.stopped /\ ~old.running
 /\ i \notin fence.attested
 /\ fence' = [fence EXCEPT !.attested = @ \cup {i}]
 /\ UNCHANGED <<old, replica, control, recovery, observation>>
Fenced(i) == fence.inhibited /\ fence.stopped /\ ~old.running /\ i \in fence.attested

(* Sources are operational retained bytes. The historical protected oracle
   never supplies a missing decision, claim, prepare or stable result. *)
IdentitySources == IF ~old.diskLost THEN Groups
                   ELSE replica.identity \cup replica.identityDurable
PrepareSources == IF ~old.diskLost THEN old.prep \cup replica.prep \cup replica.prepDurable
                  ELSE replica.prep \cup replica.prepDurable
OldValue == IF replica.captured # None THEN replica.captured
            ELSE IF replica.accepted # None THEN replica.accepted
            ELSE IF old.synced /\ ~old.diskLost THEN old.candidate ELSE None
Select(i) ==
 /\ Authorized(i) /\ Quorum(i) /\ Reports(i) /\ Fenced(i)
 /\ recovery.selected[i] = None /\ IdentitySources = Groups
 /\ IF OldValue = None THEN replica.continuous \/ Bug = "absence"
      ELSE OldValue = "abort" \/ PrepareSources = Groups \/ Bug = "prepares"
 /\ recovery' = [recovery EXCEPT !.selected[i] = IF OldValue = None THEN "abort" ELSE OldValue,
       !.deps[i] = PrepareSources, !.absence[i] = OldValue = None,
       !.selectedWithoutOwner[i] = old.diskLost]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
CopyPair(i,g,n) ==
 /\ Authorized(i) /\ Quorum(i) /\ recovery.selected[i] # None
 /\ <<i,g,n>> \notin recovery.pair /\ IdentitySources = Groups
 /\ OldValue = recovery.selected[i] \/ recovery.absence[i]
 /\ recovery.selected[i] = "abort" \/ g \in PrepareSources \/ Bug = "prepares"
 /\ recovery' = [recovery EXCEPT !.pair = @ \cup {<<i,g,n>>},
       !.identity[i] = IF Bug = "identity" THEN <<"new-anchor", "new-request", "new-binding", Groups>> ELSE @]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
PairReady(i) == \A g \in Groups, n \in Pair: <<i,g,n>> \in recovery.pair
Install(i,g) ==
 /\ Authorized(i) /\ PairReady(i) /\ <<i,g>> \notin recovery.installed
 /\ recovery' = [recovery EXCEPT !.installed = @ \cup {<<i,g>>}]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Apply(i,g) ==
 /\ Authorized(i) /\ recovery.selected[i] = "commit"
 /\ <<i,g>> \in recovery.installed \ recovery.applied
 /\ recovery' = [recovery EXCEPT !.applied = @ \cup {<<i,g>>}]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Installed(i) == \A g \in Groups: <<i,g>> \in recovery.installed
Applied(i) == recovery.selected[i] = "abort" \/
              (\A g \in Groups: <<i,g>> \in recovery.applied)
Barrier(i) ==
 /\ Authorized(i) /\ Installed(i) /\ Applied(i) /\ i \notin recovery.barrier
 /\ recovery' = [recovery EXCEPT !.barrier = @ \cup {i}]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Complete(i) ==
 /\ Authorized(i) /\ i \in recovery.barrier /\ i \notin recovery.complete
 /\ recovery' = [recovery EXCEPT !.complete = @ \cup {i},
       !.result[i] = IF Bug = "result"
          THEN [Result(recovery.selected[i]) EXCEPT !.objectAndPositions = "replacement-object-and-positions"]
          ELSE Result(recovery.selected[i])]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Configure(i,g,n) ==
 /\ Authorized(i) /\ PairReady(i) /\ i \in recovery.complete
 /\ <<i,g,n>> \notin recovery.configuration
 /\ recovery' = [recovery EXCEPT !.configuration = @ \cup {<<i,g,n>>}]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Configured(i) == \A g \in Groups, n \in Pair: <<i,g,n>> \in recovery.configuration
RegisterActivation(i) ==
 /\ Authorized(i) /\ Quorum(i) /\ Fenced(i) /\ PairReady(i)
 /\ Configured(i) /\ i \notin control.registered
 /\ control.registered = {}
 /\ control' = [control EXCEPT !.registered = @ \cup {i}]
 /\ UNCHANGED <<old, replica, recovery, fence, observation>>
Activate(i) ==
 /\ Authorized(i) /\ Quorum(i) /\ Fenced(i) /\ i \notin recovery.active
 /\ (i \in control.registered /\ Configured(i) /\ PairReady(i)
       /\ i \in recovery.complete) \/ Bug = "publication"
 /\ recovery' = [recovery EXCEPT !.active = @ \cup {i}]
 /\ UNCHANGED <<old, replica, control, fence, observation>>
Reply(i) ==
 /\ i \in recovery.active /\ i \notin observation.replied
 /\ observation' = [observation EXCEPT !.replied = @ \cup {i}]
 /\ UNCHANGED <<old, replica, control, recovery, fence>>
Retry(i) ==
 /\ i \in observation.replied /\ i \notin observation.retried
 /\ observation' = [observation EXCEPT !.retried = @ \cup {i}]
 /\ UNCHANGED <<old, replica, control, recovery, fence>>

RestartPartner ==
 /\ AllowFaults /\ ~replica.restarted /\ recovery.pair = {}
 /\ replica' = [replica EXCEPT !.restarted = TRUE, !.continuous = FALSE,
       !.identity = replica.identityDurable, !.prep = replica.prepDurable,
       !.accepted = replica.captured]
 /\ UNCHANGED <<old, control, recovery, fence, observation>>
LoseOwnerDisk ==
 /\ AllowFaults /\ ~old.diskLost /\ recovery.pair = {}
 /\ old' = [old EXCEPT !.diskLost = TRUE, !.synced = FALSE,
       !.prep = {}, !.installed = {}, !.applied = {}, !.result = Result(None)]
 /\ UNCHANGED <<replica, control, recovery, fence, observation>>
CrashController ==
 /\ AllowFaults /\ control.alive /\ ~control.crashed
 /\ control' = [control EXCEPT !.alive = FALSE, !.crashed = TRUE]
 /\ UNCHANGED <<old, replica, recovery, fence, observation>>
RestartController ==
 /\ ~control.alive
 /\ control' = [control EXCEPT !.alive = TRUE,
       !.resumedSelection = recovery.active = {} /\
          (\E i \in Attempts: recovery.selected[i] # None)]
 /\ UNCHANGED <<old, replica, recovery, fence, observation>>

Next == (\E g \in Groups: PrepareOwner(g) \/ PreparePartner(g) \/ SyncPartner(g)
                         \/ OldInstall(g) \/ OldApply(g))
 \/ (\E v \in Values: Propose(v)) \/ SyncOld \/ AcceptOld \/ ProtectOld \/ OldComplete \/ RejectOld
 \/ (\E i \in Attempts: ProposeTakeover(i) \/ Allocate(i) \/ RejectTakeover(i)
       \/ Attest(i) \/ Select(i) \/ Barrier(i) \/ Complete(i)
       \/ RegisterActivation(i) \/ Activate(i) \/ Reply(i) \/ Retry(i)
       \/ (\E g \in Groups: Install(i,g) \/ Apply(i,g))
       \/ (\E g \in Groups, n \in Pair: Promise(i,g,n) \/ Report(i,g,n)
                                      \/ CopyPair(i,g,n) \/ Configure(i,g,n)))
 \/ Inhibit \/ Stop \/ SyncAccepted \/ RestartPartner \/ LoseOwnerDisk \/ CrashController \/ RestartController
Spec == Init /\ [][Next]_vars

TypeOK ==
 /\ old.candidate \in Values \cup {None} /\ old.protected \in Values \cup {None}
 /\ old.prep \subseteq Groups /\ replica.prep \subseteq Groups
 /\ replica.promises \in [Votes -> 0..2]
 /\ control.allocated \subseteq Attempts /\ control.rejected \subseteq Attempts
 /\ recovery.selected \in [Attempts -> Values \cup {None}]
 /\ recovery.pair \subseteq Attempts \X Groups \X Pair
 /\ recovery.applied \subseteq recovery.installed
 /\ recovery.active \subseteq control.allocated
NoPartialPublication == \A i \in recovery.active:
 /\ PairReady(i) /\ Installed(i) /\ Applied(i) /\ i \in recovery.barrier
 /\ i \in recovery.complete /\ Configured(i) /\ i \in control.registered
 /\ Fenced(i) /\ recovery.result[i] = Result(recovery.selected[i])
 /\ control.boundRestart[i] = replica.restarted
PreservedOutcome == \A i \in Attempts: recovery.selected[i] # None =>
 /\ observation.protected \subseteq {recovery.selected[i]}
 /\ recovery.selected[i] = "abort" \/ recovery.deps[i] = Groups
PreservedIdentity == \A i \in Attempts: recovery.identity[i] = Identity
PreservedResult == \A i \in Attempts: recovery.result[i].outcome # None =>
                  recovery.result[i] = Result(recovery.selected[i])
NoCompetingActivation == Cardinality(recovery.active) <= 1
NoCompetingAllocation == Cardinality(control.allocated) <= 1
NoStaleAccept == ~observation.staleAccept
StableResult == \A i \in observation.retried:
 /\ i \in recovery.active /\ recovery.identity[i] = Identity
 /\ recovery.result[i] = Result(recovery.selected[i])
Safety == TypeOK /\ NoPartialPublication /\ PreservedOutcome
          /\ PreservedIdentity /\ PreservedResult /\ NoCompetingAllocation /\ NoCompetingActivation
          /\ NoStaleAccept /\ StableResult

Finished(i) == i \in observation.retried /\ recovery.selected[i] = "commit"
Contended(i) == observation.competedAtAllocation
               /\ control.rejected = Attempts \ {i}
WitnessAbsent == ~CASE
 Witness = "commit" -> \E i \in Attempts: Finished(i) /\ observation.protected = {"commit"}
 [] Witness = "partial" -> \E i \in Attempts: Finished(i) /\ observation.partialAtAllocation
                           /\ old.applied = {"A"} /\ ~old.complete
 [] Witness = "contention" -> \E i \in Attempts: Finished(i) /\ Contended(i)
 [] Witness = "restart" -> \E i \in Attempts: Finished(i) /\ control.resumedSelection
                           /\ control.alive /\ replica.restarted
 [] Witness = "result" -> \E i \in Attempts: Finished(i) /\ old.complete
 [] Witness = "accept" -> \E i \in Attempts: Finished(i) /\ observation.acceptedBeforePromise
                         /\ observation.rejectedAfterPromise /\ recovery.selectedWithoutOwner[i]
 [] Witness = "gap" -> \E i \in Attempts:
       /\ Authorized(i) /\ Quorum(i) /\ Reports(i) /\ Fenced(i)
       /\ old.diskLost /\ replica.restarted /\ ~replica.continuous
       /\ observation.protected = {"commit"} /\ OldValue = None
       /\ IdentitySources = Groups /\ PrepareSources = Groups
       /\ recovery.selected[i] = None /\ recovery.active = {}
 [] Witness = "dependencies" -> \E i \in Attempts:
       /\ Authorized(i) /\ Quorum(i) /\ Reports(i) /\ Fenced(i)
       /\ old.diskLost /\ replica.restarted /\ OldValue = "commit"
       /\ IdentitySources = Groups /\ PrepareSources # Groups
       /\ recovery.selected[i] = None /\ recovery.active = {}
 [] Witness = "absence" -> \E i \in observation.retried:
       recovery.selected[i] = "abort" /\ old.candidate = None /\ replica.continuous
 [] OTHER -> FALSE
=============================================================================
