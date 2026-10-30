---------------------------- MODULE Emergency ----------------------------
EXTENDS Integers, FiniteSets, TLC

CONSTANTS Bug, Witness, MaxFaults
ASSUME MaxFaults \in 0..2
ASSUME Bug \in {"none","registry","lineage","fresh","approval","launch","facts",
                "fence","commit","claim","retirement","peer","regrant","exit","failback"}
(* UUID/digest symbols denote exact identities/bytes, never ordered epochs or
   probabilistic digest comparisons. One activation and its explicit exit are
   bounded here; two independently approved runners compete for that activation. *)
Nodes == {"A", "B", "C"}
Groups == {"x", "y"}
Plans == 1..5
None == "none"
OldFacts == {"ordinaryCommit", "transactionCommit", "claim", "binding",
             "result", "retired", "rejected", "home"}
Prior == [generation |-> <<"generation",0>>, configuration |-> <<"A", "B", "C">>,
          histories |-> <<"hx0", "hy0">>, record |-> "c0", revision |-> 0]
Plan(p) == [generation |-> IF p = 4 THEN <<"generation",0>> ELSE <<"generation", p>>,
            transition |-> IF p = 4 THEN <<"transition",0>> ELSE <<"transition", p>>,
            designated |-> IF p = 2 THEN "B" ELSE "A",
            digest |-> <<"canonical-plan", p>>,
            approved |-> IF p = 5 THEN <<"other-bytes", p>>
                        ELSE <<"canonical-plan", p>>,
            previous |-> IF p = 3 THEN [Prior EXCEPT !.histories = <<"stale", "hy0">>]
                         ELSE Prior]
GoodLineage(p) == Plan(p).previous = Prior
Fresh(p) == Plan(p).generation # Prior.generation /\ Plan(p).transition # <<"transition",0>>
Approved(p) == Plan(p).digest = Plan(p).approved
Peer(p) == IF Plan(p).designated = "A" THEN "B" ELSE "A"
Pair(p) == {Plan(p).designated, Peer(p)}
ExitRecord(p,cut) == [previous |-> p, generation |-> Plan(p).generation,
  transition |-> <<"exit-transition",p>>, configuration |-> Pair(p),
  histories |-> <<Prior.histories,cut>>, cut |-> cut, approvedPlan |-> <<"exit-plan",p>>]
Launches == Nodes \X (0..2)
VARIABLE s
vars == <<s>>
Current(n) == <<n, s.inc[n]>>
Owner == Plan(s.plan).designated
PeerNode == Peer(s.plan)
LiveRegistry == s.continuity /\ s.registryUp /\ s.head = s.frontier /\ s.frontier = s.plan
WorkFacts(w) == (IF w >= 1 THEN {"newClaim"} ELSE {})
             \cup (IF w >= 2 THEN {"emergencyCommit"} ELSE {})
             \cup (IF w >= 3 THEN {"newResult"} ELSE {})
             \cup (IF w >= 4 THEN {"newRetired"} ELSE {})
Graph(w) == s.selected \cup WorkFacts(w)
Copied(n, cut) == \A g \in Groups: s.copy[n][g] = cut
Installed(n) == \A g \in Groups: <<n,g>> \in s.installed
Ready(n, cut) == s.running[n] /\ Copied(n, cut) /\ Installed(n)
                /\ n \in s.cache
OldExcluded == \A n \in Nodes: <<n,0>> \in s.acks
PreviousExcluded(n) == \A i \in 0..s.inc[n]: <<n,i>> \in s.acks

Init == s = [plan |-> 0, candidate |-> 0, head |-> 0, frontier |-> 0,
  registryUp |-> TRUE, continuity |-> TRUE, controllerUp |-> TRUE, faults |-> 0,
  restored |-> FALSE, restoredAfterAck |-> FALSE, crashedAfterAck |-> FALSE,
  continuityLostAfterAck |-> FALSE, intents |-> {}, reserved |-> {}, rejected |-> {},
  phase |-> 0, inhibited |-> {}, stopped |-> {}, acks |-> {}, received |-> {},
  everInhibited |-> {}, everStopped |-> {}, everAcks |-> {},
  inc |-> [n \in Nodes |-> 0], running |-> [n \in Nodes |-> TRUE],
  reads |-> Nodes, writes |-> {"A"}, owns |-> {"A"}, rejoins |-> Nodes,
  modes |-> [n \in Nodes |-> "old-normal"], launches |-> {},
  selected |-> {}, disk |-> [n \in Nodes |-> IF n \in {"A","B"} THEN OldFacts ELSE {}],
  copy |-> [n \in Nodes |-> [g \in Groups |-> -1]], installed |-> {}, cache |-> {},
  admitted |-> FALSE, grantInc |-> -1, grants |-> {}, revocations |-> {}, work |-> 0, chosen |-> FALSE,
  acknowledged |-> FALSE, everRetired |-> FALSE, duplicated |-> FALSE,
  cut |-> -1, receipts |-> [n \in Nodes |-> -1],
  exitReceipts |-> [n \in Nodes |-> -1], normalRequired |-> {},
  coordination |-> FALSE, exitPending |-> FALSE, exitCommitted |-> FALSE, exitRecord |-> ExitRecord(0,-1),
  offlinePins |-> {"C"}, observed |-> {}]

(* The lock spans candidate, external frontier and host CURRENT publication.
   An ambiguous write is resumed by exact identity, never a new contender.
   Crash/restore can independently leave either pointer behind. *)
BeginCAS(p) ==
 /\ s.registryUp /\ s.continuity /\ s.controllerUp /\ s.candidate = 0 /\ s.head = 0
 /\ (s.frontier = 0 \/ Bug = "registry")
 /\ (GoodLineage(p) \/ Bug = "lineage")
 /\ (Fresh(p) \/ Bug = "fresh") /\ (Approved(p) \/ Bug = "approval")
 /\ s' = [s EXCEPT !.candidate = p, !.reserved = @ \cup {p}]
RejectPlan(p) ==
 /\ s.registryUp
 /\ (~GoodLineage(p) \/ ~Fresh(p) \/ ~Approved(p)
      \/ (s.frontier # 0 /\ s.frontier # p))
 /\ LET category == IF GoodLineage(p) /\ Fresh(p) /\ Approved(p)
                       THEN "conflict" ELSE "invalid"
    IN /\ category \notin s.rejected
       /\ s' = [s EXCEPT !.rejected = @ \cup {category}]
PublishFrontier ==
 /\ s.registryUp /\ s.continuity /\ s.candidate # 0 /\ s.frontier # s.candidate
 /\ s' = [s EXCEPT !.frontier = s.candidate,
          !.intents = @ \cup {s.candidate}]
PublishHead ==
 /\ s.registryUp /\ s.continuity /\ s.frontier \in Plans /\ s.head # s.frontier
 /\ s' = [s EXCEPT !.head = s.frontier, !.candidate = 0,
          !.plan = s.frontier, !.phase = IF s.phase = 0 THEN 1 ELSE s.phase]
RegistryCrash ==
 /\ s.registryUp /\ s.faults < MaxFaults
 /\ s' = [s EXCEPT !.registryUp = FALSE, !.faults = @ + 1]
LoseContinuity ==
 /\ s.continuity /\ s.faults < MaxFaults
 /\ s' = [s EXCEPT !.continuity = FALSE, !.continuityLostAfterAck = s.acknowledged,
          !.faults = @ + 1]
RegistryRestart ==
 /\ ~s.registryUp /\ s' = [s EXCEPT !.registryUp = TRUE]
RestoreOldRegistry ==
 /\ ~s.registryUp /\ ~s.restored
 /\ s' = [s EXCEPT !.registryUp = TRUE, !.head = 0, !.candidate = 0,
          !.restored = TRUE, !.restoredAfterAck = s.acknowledged,
          !.inhibited = IF Bug = "facts" THEN {} ELSE @,
          !.stopped = IF Bug = "facts" THEN {} ELSE @,
          !.acks = IF Bug = "facts" THEN {} ELSE @]
ControllerCrash ==
 /\ s.controllerUp /\ s.faults < MaxFaults
 /\ s' = [s EXCEPT !.controllerUp = FALSE, !.received = {}, !.faults = @ + 1]
ControllerRestart ==
 /\ ~s.controllerUp /\ s' = [s EXCEPT !.controllerUp = TRUE]

(* These are exact launch-domain facts. Pauses/timeouts have no stop action.
   Successful stop means positively supervised no execution/cache domain after
   inhibition. This is an external contract, not a model of ACPI or a kernel. *)
Inhibit(n) ==
 /\ s.plan # 0 /\ s.controllerUp /\ s.registryUp
 /\ (s.inc[n] = 0 \/ ~s.running[n])
 /\ Current(n) \notin s.inhibited
 /\ s' = [s EXCEPT !.inhibited = @ \cup {Current(n)},
                   !.everInhibited = @ \cup {Current(n)}]
Stop(n) ==
 /\ s.phase < 9 /\ Current(n) \in s.inhibited /\ Current(n) \notin s.stopped
 /\ s' = [s EXCEPT !.running[n] = FALSE,
          !.stopped = @ \cup {Current(n)}, !.everStopped = @ \cup {Current(n)},
          !.reads = @ \ {n}, !.writes = @ \ {n}, !.owns = @ \ {n},
          !.rejoins = @ \ {n}, !.installed = {v \in @: v[1] # n},
          !.cache = @ \ {n}, !.receipts[n] = -1]
Attest(n) ==
 /\ Current(n) \in s.stopped /\ Current(n) \in s.inhibited
 /\ Current(n) \notin s.acks
 /\ s' = [s EXCEPT !.acks = @ \cup {Current(n)},
                   !.everAcks = @ \cup {Current(n)}]
Receive(n) ==
 /\ s.controllerUp /\ <<n,0>> \in s.acks /\ n \notin s.received
 /\ s' = [s EXCEPT !.received = @ \cup {n}]
Select ==
 /\ s.controllerUp /\ LiveRegistry /\ s.phase = 1
 /\ (s.received = Nodes \/ Bug = "fence")
 /\ OldFacts \subseteq s.disk["A"]
 /\ s' = [s EXCEPT !.phase = 2,
          !.selected = CASE Bug = "commit" -> OldFacts \ {"transactionCommit"}
                       [] Bug = "claim" -> OldFacts \ {"claim", "binding", "result"}
                       [] Bug = "retirement" -> OldFacts \ {"retired", "rejected"}
                       [] OTHER -> OldFacts]
Launch(n) ==
 /\ s.plan # 0 /\ s.controllerUp /\ (LiveRegistry \/ Bug = "launch") /\ ~s.running[n]
 /\ s.inc[n] < 2 /\ PreviousExcluded(n)
 /\ (n = Owner \/ (s.admitted /\ n = PeerNode
                       /\ (s.grantInc = s.inc[n]+1 \/ Bug = "regrant")))
 /\ s.phase \in 2..8 /\ n \notin s.normalRequired
 /\ s' = [s EXCEPT !.inc[n] = @ + 1, !.running[n] = TRUE,
          !.modes[n] = IF n = Owner THEN "emergency" ELSE "recovery",
          !.launches = @ \cup {<<s.plan,n,s.inc[n]+1,
                                 IF n = Owner THEN "designated" ELSE "recovery",
                                 s.head,s.frontier,s.continuity,s.grantInc>>},
          !.installed = {v \in @: v[1] # n}, !.cache = @ \ {n},
          !.receipts[n] = -1]
NodeCrash(n) ==
 /\ s.inc[n] > 0 /\ s.running[n] /\ s.faults < MaxFaults
 /\ ~s.exitCommitted
 /\ s' = [s EXCEPT !.running[n] = FALSE, !.faults = @ + 1,
          !.crashedAfterAck = @ \/ (n = Owner /\ s.acknowledged),
          !.reads = @ \ {n}, !.writes = @ \ {n}, !.owns = @ \ {n},
          !.installed = {v \in @: v[1] # n}, !.cache = @ \ {n},
          !.receipts[n] = -1]
Copy(n,g) ==
 /\ s.plan # 0 /\ s.phase \in 2..8 /\ s.running[n] /\ s.inc[n] > 0
 /\ (n = Owner \/ (s.admitted /\ n = PeerNode /\ s.grantInc = s.inc[n]))
 /\ (s.phase \in {2,7,8} \/ (s.phase = 4 /\ n = Owner /\ s.inc[n] = 2))
 /\ (n = Owner \/ s.phase \in {7,8})
 /\ s.copy[n][g] # s.work
 /\ s' = [s EXCEPT !.copy[n][g] = s.work,
          !.disk[n] = @ \cup Graph(s.work),
          !.installed = @ \ {<<n,g>>}, !.cache = @ \ {n}, !.receipts[n] = -1]
Install(n,g) ==
 /\ s.plan # 0 /\ s.phase \in 2..8 /\ s.running[n] /\ s.inc[n] > 0
 /\ s.copy[n][g] = s.work /\ <<n,g>> \notin s.installed
 /\ s' = [s EXCEPT !.installed = @ \cup {<<n,g>>}]
CacheBarrier(n) ==
 /\ s.plan # 0 /\ s.phase \in 2..8 /\ s.running[n] /\ s.inc[n] > 0
 /\ Copied(n,s.work) /\ Installed(n) /\ n \notin s.cache
 /\ s' = [s EXCEPT !.cache = @ \cup {n}]
Activate ==
 /\ s.plan # 0 /\ s.phase = 2 /\ LiveRegistry
 /\ ((Ready(Owner,s.work) /\ OldExcluded) \/ Bug = "fence")
 /\ s' = [s EXCEPT !.phase = 4]
PublishEmergency ==
 /\ s.plan # 0 /\ s.phase = 4 /\ LiveRegistry
 /\ Ready(Owner,s.work) /\ OldExcluded /\ Owner \notin s.normalRequired
 /\ Owner \notin s.reads
 /\ s' = [s EXCEPT !.reads = @ \cup {Owner}, !.writes = @ \cup {Owner},
          !.owns = @ \cup {Owner}, !.observed = @ \cup {"active"}]
AdmitPeer ==
 /\ s.plan # 0 /\ s.phase = 4 /\ LiveRegistry /\ ~s.admitted
 /\ s' = [s EXCEPT !.admitted = TRUE, !.grantInc = s.inc[PeerNode]+1,
          !.grants = @ \cup {<<PeerNode,s.inc[PeerNode]+1>>}]
(* Each action denotes a separately authorized exact registry CAS. A peer's
   changed incarnation cannot edit or inherit a prior grant on reconnect. *)
RevokePeer ==
 /\ s.plan # 0 /\ s.admitted /\ LiveRegistry /\ s.controllerUp
 /\ s.inc[PeerNode] = 1 /\ s.grantInc = 1
 /\ Current(PeerNode) \in s.acks
 /\ s' = [s EXCEPT !.grantInc = -1,
          !.revocations = @ \cup {Current(PeerNode)}]
RegrantPeer ==
 /\ s.plan # 0 /\ s.admitted /\ LiveRegistry /\ s.controllerUp
 /\ s.phase \in 4..8 /\ PeerNode \notin s.normalRequired
 /\ s.inc[PeerNode] = 1 /\ ~s.running[PeerNode] /\ s.grantInc = -1
 /\ Current(PeerNode) \in s.revocations /\ PreviousExcluded(PeerNode)
 /\ s' = [s EXCEPT !.grantInc = 2,
          !.grants = @ \cup {<<PeerNode,2>>}]
(* Fresh receipt/cache readiness is volatile and incarnation-bound; graphs and
   NORMAL_REQUIRED are durable. A crash after NORMAL_REQUIRED can block this
   bound forever; it cannot roll back to local-only serving. *)
ClaimWork ==
 /\ s.plan # 0 /\ s.phase = 4 /\ Owner \in s.writes /\ s.work = 0
 /\ s' = [s EXCEPT !.work = 1, !.disk[Owner] = @ \cup {"newClaim"}]
SyncWork ==
 /\ s.plan # 0 /\ s.phase \in {4,6} /\ s.work = 1 /\ s.running[Owner]
 /\ Owner \notin s.normalRequired
 /\ s' = [s EXCEPT !.work = 2, !.chosen = TRUE,
          !.disk[Owner] = @ \cup {"emergencyCommit"}]
ReplyWork ==
 /\ s.plan # 0 /\ s.phase \in {4,6} /\ s.work = 2 /\ s.running[Owner]
 /\ "emergencyCommit" \in s.disk[Owner] /\ Owner \notin s.normalRequired
 /\ s' = [s EXCEPT !.work = 3, !.acknowledged = TRUE,
          !.disk[Owner] = @ \cup {"newResult"}]
RetireWork ==
 /\ s.plan # 0 /\ s.phase = 4 /\ s.work = 3 /\ s.running[Owner]
 /\ s' = [s EXCEPT !.work = 4, !.everRetired = TRUE,
          !.disk[Owner] = @ \cup {"newRetired"}]
RetryOld ==
 /\ s.plan # 0 /\ Owner \in s.reads /\ s.inc[Owner] > 0
 /\ "retry" \notin s.observed
 /\ s' = [s EXCEPT !.observed = @ \cup {"retry"},
          !.duplicated = ~({"claim","binding","result","retired","rejected"}
                          \subseteq s.disk[Owner])]
PeerEscalate ==
 /\ Bug = "peer" /\ s.plan # 0 /\ s.admitted /\ s.running[PeerNode]
 /\ s.phase = 4
 /\ s' = [s EXCEPT !.reads = @ \cup {PeerNode}, !.writes = @ \cup {PeerNode},
          !.owns = @ \cup {PeerNode}, !.rejoins = @ \cup {PeerNode}]
BeginExit ==
 /\ s.plan # 0 /\ s.phase = 4 /\ s.admitted /\ LiveRegistry
 /\ "active" \in s.observed
 /\ s' = [s EXCEPT !.phase = 6, !.reads = {}, !.writes = {}, !.owns = {}]
CaptureCut ==
 /\ s.phase = 6 /\ s.work \in {0,3,4}
 /\ s' = [s EXCEPT !.cut = s.work, !.phase = 7]
PairReceipt(n) ==
 /\ s.plan # 0 /\ s.phase \in {7,8} /\ n \in Pair(s.plan)
 /\ Ready(n,s.cut) /\ Graph(s.cut) \subseteq s.disk[n]
 /\ s.receipts[n] # s.inc[n]
 /\ s' = [s EXCEPT !.receipts[n] = s.inc[n], !.phase = 8]
RebuildCoordination ==
 /\ s.phase \in {7,8} /\ ~s.coordination
 /\ s' = [s EXCEPT !.coordination = TRUE]
NormalRequired(n) ==
 /\ s.plan # 0 /\ s.phase = 8 /\ n \in Pair(s.plan)
 /\ \A m \in Pair(s.plan): s.receipts[m] = s.inc[m] /\ Ready(m,s.cut)
 /\ n \notin s.normalRequired
 /\ s' = [s EXCEPT !.normalRequired = @ \cup {n}, !.modes[n] = "normal-required"]
ExitReady == s.plan # 0 /\ s.phase = 8 /\ s.coordination
 /\ s.normalRequired = Pair(s.plan)
 /\ \A n \in Pair(s.plan): Ready(n,s.cut) /\ s.receipts[n] = s.inc[n]
BeginExitCAS ==
 /\ LiveRegistry /\ ~s.exitPending
 /\ (ExitReady \/ (Bug = "exit" /\ s.phase \in {7,8}))
 /\ s' = [s EXCEPT !.exitPending = TRUE, !.exitReceipts = s.receipts,
          !.exitRecord = ExitRecord(s.plan,s.cut)]
PublishExitFrontier ==
 /\ s.registryUp /\ s.continuity /\ s.exitPending /\ s.frontier = s.plan
 /\ s' = [s EXCEPT !.frontier = s.plan + 10]
PublishExitHead ==
 /\ s.registryUp /\ s.continuity /\ s.plan # 0 /\ s.frontier = s.plan + 10
 /\ s.head # s.frontier
 /\ s' = [s EXCEPT !.head = s.frontier, !.exitCommitted = TRUE, !.phase = 9]
ReleaseNormal(n) ==
 /\ s.plan # 0 /\ n \in Pair(s.plan)
 /\ s.exitCommitted /\ s.registryUp /\ s.continuity /\ s.head = s.frontier
 /\ s.normalRequired = Pair(s.plan) /\ s.coordination
 /\ \A m \in Pair(s.plan): Ready(m,s.cut) /\ s.receipts[m] = s.inc[m]
 /\ s.modes[n] # "normal"
 /\ s' = [s EXCEPT !.reads = @ \cup {n},
          !.writes = IF n = Owner THEN @ \cup {n} ELSE @,
          !.owns = IF n = Owner THEN @ \cup {n} ELSE @,
          !.modes[n] = "normal", !.observed = @ \cup {"normal"}]
Failback ==
 /\ Bug = "failback" /\ s.plan # 0 /\ s.phase \in {4,6,7,8}
 /\ s' = [s EXCEPT !.reads = @ \cup {"C"}, !.writes = @ \cup {"C"},
          !.owns = @ \cup {"C"}, !.rejoins = @ \cup {"C"}]

Next == (\E p \in Plans: BeginCAS(p) \/ RejectPlan(p))
 \/ PublishFrontier \/ PublishHead \/ RegistryCrash \/ RegistryRestart
 \/ RestoreOldRegistry \/ LoseContinuity \/ ControllerCrash \/ ControllerRestart
 \/ (\E n \in Nodes: Inhibit(n) \/ Stop(n) \/ Attest(n) \/ Receive(n)
      \/ Launch(n) \/ NodeCrash(n) \/ CacheBarrier(n) \/ PairReceipt(n)
      \/ NormalRequired(n) \/ ReleaseNormal(n)
      \/ (\E g \in Groups: Copy(n,g) \/ Install(n,g)))
 \/ Select \/ Activate \/ PublishEmergency \/ AdmitPeer \/ RevokePeer \/ RegrantPeer \/ ClaimWork \/ SyncWork
 \/ ReplyWork \/ RetireWork \/ RetryOld \/ PeerEscalate \/ BeginExit \/ CaptureCut
 \/ RebuildCoordination \/ BeginExitCAS \/ PublishExitFrontier \/ PublishExitHead \/ Failback
Spec == Init /\ [][Next]_vars

RegistrySafety ==
 /\ Cardinality(s.intents) <= 1
 /\ \A p \in s.intents: GoodLineage(p) /\ Fresh(p) /\ Approved(p)
 /\ \A l \in s.launches: l[1] \in s.intents
       /\ (l[4] = "designated" => l[2] = Plan(l[1]).designated)
       /\ (l[4] = "recovery" => l[2] = Peer(l[1])
          /\ l[8] = l[3] /\ <<l[2],l[3]>> \in s.grants)
       /\ l[5] = l[1] /\ l[6] = l[1] /\ l[7]
       /\ \A i \in 0..(l[3]-1): <<l[2],i>> \in s.acks
DurableFacts == s.everInhibited \subseteq s.inhibited
 /\ s.everStopped \subseteq s.stopped /\ s.everAcks \subseteq s.acks
 /\ s.stopped \subseteq s.inhibited /\ s.acks \subseteq s.stopped
Preservation ==
 /\ (s.phase >= 2 => OldFacts \subseteq s.selected)
 /\ ~s.duplicated
 /\ (s.chosen => "emergencyCommit" \in s.disk[Owner])
 /\ (s.acknowledged => s.chosen /\ "newResult" \in s.disk[Owner])
 /\ (s.everRetired => "newRetired" \in s.disk[Owner])
AdmissionSafety == s.plan = 0 \/
 /\ (s.phase >= 4 => OldExcluded)
 /\ (s.phase \in 4..8 => s.reads \subseteq {Owner}
       /\ s.writes \subseteq {Owner} /\ s.owns \subseteq {Owner} /\ s.rejoins = {})
 /\ (s.exitCommitted => s.reads \subseteq Pair(s.plan)
       /\ s.writes \subseteq {Owner} /\ s.owns \subseteq {Owner} /\ s.rejoins = {})
 /\ \A n \in Nodes: (s.inc[n] > 0 /\ s.modes[n] = "normal") => s.exitCommitted
 /\ \A n \in s.normalRequired: s.modes[n] \in {"normal-required","normal"}
       /\ (s.modes[n] = "normal-required" => n \notin s.writes)
ExitSafety == ~s.exitCommitted \/
 /\ s.cut = s.work /\ s.coordination /\ s.normalRequired = Pair(s.plan)
 /\ s.exitRecord = ExitRecord(s.plan,s.cut)
 /\ \A n \in Pair(s.plan): Copied(n,s.cut) /\ Graph(s.cut) \subseteq s.disk[n]
       /\ s.exitReceipts[n] = s.inc[n]
Safety == RegistrySafety /\ DurableFacts /\ Preservation /\ AdmissionSafety /\ ExitSafety
WitnessReached == CASE
 Witness = "exit" -> s.exitCommitted /\ "normal" \in s.observed
     /\ s.work = 4 /\ "retry" \in s.observed /\ s.reads = Pair(s.plan)
 [] Witness = "restore" -> s.restoredAfterAck /\ s.exitCommitted /\ s.work = 3
 [] Witness = "restart" -> s.plan # 0 /\ s.inc[Owner] = 2 /\ s.crashedAfterAck
     /\ Owner \in s.reads /\ s.acknowledged /\ s.phase = 4
 [] Witness = "peer-restart" -> s.plan # 0 /\ s.inc[PeerNode] = 2
     /\ s.grantInc = 2 /\ <<PeerNode,1>> \in s.revocations
     /\ s.running[PeerNode] /\ s.phase = 4
     /\ PeerNode \notin (s.reads \cup s.writes \cup s.owns \cup s.rejoins)
 [] Witness = "partial" -> s.plan # 0 /\ s.phase = 8
     /\ Cardinality(s.normalRequired) = 1 /\ s.reads = {} /\ s.writes = {}
 [] Witness = "conflict" -> s.plan # 0 /\ s.rejected = {"conflict","invalid"}
     /\ "active" \in s.observed
 [] Witness = "continuity" -> ~s.continuity /\ s.continuityLostAfterAck
     /\ "active" \in s.observed /\ ~s.exitCommitted
 [] Witness = "ambiguous" -> s.frontier \in Plans /\ s.head = 0
     /\ ~s.registryUp /\ s.intents # {} /\ s.launches = {}
 [] OTHER -> FALSE
WitnessAbsent == ~WitnessReached
=============================================================================
