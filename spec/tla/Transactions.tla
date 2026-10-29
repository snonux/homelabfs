--------------------------- MODULE Transactions ---------------------------
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS Profile, Bug, Witness, AllowFaults, RetainHistory
ASSUME Profile \in {"authority", "protocol", "namespace"}
ASSUME Bug \in {"none", "prepare", "fence", "dedup", "visibility", "retention"}
ASSUME AllowFaults \in BOOLEAN /\ RetainHistory \in BOOLEAN
VARIABLE s
vars == <<s>>
Groups == {1, 2}
Replicas == {"A", "B", "C"}
Quorum == {"B", "C"}
AllPromises == Groups \X Quorum
AllCoverage == Groups \X Replicas
None == "none"
Values == {"commit", "abort"}
Min(S) == CHOOSE n \in S : \A m \in S : n <= m

(* AUTHORITY projection, including a complete integrated no-fault execution.
   Digests denote exact canonical request bytes, not probabilistic hashes.
   Endpoint closures {3,4} and {1,2} are initially disjoint; all claims must
   meet at anchor 3 before resolution/affinity can add their dependencies. *)
Endpoints(d) == IF d = 1 THEN {3, 4} ELSE {1, 2}
AuthorityInit ==
  \E placement \in {"compatible", "owner", "partner", "membership"} :
    s = [registered |-> FALSE, closed |-> FALSE, anchorRetired |-> FALSE,
         authorityKnown |-> TRUE, restarted |-> FALSE,
         floor |-> 0, claims |-> {}, everClaims |-> {}, digest |-> 0,
         placement |-> placement, resolved |-> FALSE, rejected |-> FALSE,
         members |-> {3}, manifests |-> {}, joined |-> FALSE,
         binding |-> 0, boundMembers |-> {}, originalBinding |-> 0,
         coordinator |-> 3, prep |-> {}, decision |-> None,
         installed |-> {}, applied |-> {}, cache |-> FALSE,
         visible |-> FALSE, result |-> FALSE, effects |-> 0,
         source |-> TRUE, retry |-> None, retryMerged |-> FALSE,
         retriedBinding |-> 0, merged |-> FALSE]
Register == /\ ~s.registered /\ ~s.closed /\ ~s.anchorRetired
            /\ s' = [s EXCEPT !.registered = TRUE]
Claim(d) ==
  /\ s.registered /\ ~s.closed /\ s.floor = 0
  /\ IF Bug = "dedup"
        THEN <<3, d>> \notin s.claims
        ELSE s.claims = {}
  /\ LET route == 3
         claim == <<route, d>>
     IN s' = [s EXCEPT !.claims = @ \cup {claim},
              !.everClaims = @ \cup {claim},
              !.digest = IF @ = 0 THEN d ELSE @]
Resolve == /\ s.digest # 0 /\ ~s.resolved /\ ~s.rejected
           /\ s.placement = "compatible"
           /\ s' = [s EXCEPT !.resolved = TRUE,
                    !.members = Endpoints(s.digest) \cup {3}]
RejectPlacement == /\ s.digest # 0 /\ s.placement # "compatible"
                   /\ ~s.rejected
                   /\ s' = [s EXCEPT !.rejected = TRUE, !.result = TRUE]
JoinMember(g) == /\ s.resolved /\ ~s.joined /\ g \in s.members \ s.manifests
                 /\ s' = [s EXCEPT !.manifests = @ \cup {g}]
PublishJoin == /\ s.resolved /\ ~s.joined /\ s.manifests = s.members
               /\ s' = [s EXCEPT !.joined = TRUE,
                        !.coordinator = Min(s.members)]
Bind == /\ s.joined /\ s.binding = 0
        /\ s' = [s EXCEPT !.binding = s.coordinator,
                 !.originalBinding = s.coordinator, !.boundMembers = s.members]
AnchorPrepare(g) == /\ s.binding # 0 /\ s.decision = None
                    /\ g \in s.boundMembers \ s.prep
                    /\ s' = [s EXCEPT !.prep = @ \cup {g}]
AnchorDecide == /\ s.binding # 0 /\ s.prep = s.boundMembers
                /\ s.decision = None
                /\ s' = [s EXCEPT !.decision = "commit"]
AnchorInstall(g) == /\ s.decision = "commit"
                    /\ g \in s.boundMembers \ s.installed
                    /\ s' = [s EXCEPT !.installed = @ \cup {g}]
AnchorApply(g) == /\ g \in s.installed \ s.applied
                  /\ s' = [s EXCEPT !.applied = @ \cup {g}]
AnchorBarrier == /\ s.binding # 0 /\ s.applied = s.boundMembers /\ ~s.cache
                  /\ s' = [s EXCEPT !.cache = TRUE]
AnchorPublish == /\ s.cache /\ ~s.visible
                  /\ s' = [s EXCEPT !.visible = TRUE, !.source = FALSE,
                           !.effects = @ + 1]
AnchorResult == /\ s.visible /\ ~s.result
                 /\ s' = [s EXCEPT !.result = TRUE]
Merge == /\ s.visible /\ ~s.merged
         /\ s' = [s EXCEPT !.members = {1, 2, 3, 4},
                  !.manifests = {1, 2, 3, 4}, !.coordinator = 1,
                  !.merged = TRUE]
Retry == /\ s.registered
         /\ s' = [s EXCEPT !.retry = IF s.closed \/ s.floor = 1 THEN "retired"
                         ELSE IF s.result THEN "result" ELSE "pending",
                  !.retryMerged = s.merged /\ ~s.source /\ s.result,
                  !.retriedBinding = s.binding]
Retire == /\ s.result /\ s.floor = 0
          /\ s' = [s EXCEPT !.floor = 1]
CompactClaim == /\ s.floor = 1 /\ s.claims # {}
                /\ s' = [s EXCEPT !.claims = {}]
CloseSession == /\ s.floor = 1 /\ ~s.closed
                /\ s' = [s EXCEPT !.closed = TRUE]
RetireAnchor == /\ s.closed /\ ~s.anchorRetired
                /\ s' = [s EXCEPT !.anchorRetired = TRUE]
AuthorityWorkers == Register \/ (\E d \in {1, 2} : Claim(d)) \/ Resolve
  \/ RejectPlacement \/ (\E g \in {1, 2, 3, 4} : JoinMember(g)) \/ PublishJoin
  \/ Bind \/ (\E g \in {1, 2, 3, 4} : AnchorPrepare(g) \/ AnchorInstall(g)
                                               \/ AnchorApply(g))
  \/ AnchorDecide \/ AnchorBarrier \/ AnchorPublish \/ AnchorResult \/ Merge
  \/ Retry \/ Retire \/ CompactClaim \/ CloseSession \/ RetireAnchor
AnchorRestart == /\ ~s.restarted
                 /\ s' = [s EXCEPT !.restarted = TRUE, !.authorityKnown = FALSE,
                          !.retry = None]
ReconcileAnchor == /\ ~s.authorityKnown
                   /\ s' = [s EXCEPT !.authorityKnown = TRUE]
AuthorityNext == (s.authorityKnown /\ AuthorityWorkers)
  \/ (AllowFaults /\ (AnchorRestart \/ ReconcileAnchor))
AuthoritySafety ==
  /\ s.claims \subseteq ((1..4) \X {1, 2})
  /\ s.everClaims \subseteq ((1..4) \X {1, 2})
  /\ s.floor \in 0..1 /\ s.digest \in 0..2
  /\ s.prep \subseteq s.boundMembers /\ s.applied \subseteq s.installed
  /\ Cardinality(s.everClaims) <= 1
  /\ \A c \in s.everClaims : c[1] = 3
  /\ s.binding = s.originalBinding
  /\ s.joined => s.manifests = s.members
  /\ s.binding # 0 => 3 \in s.boundMembers /\ s.joined
  /\ s.decision = "commit" => s.prep = s.boundMembers
  /\ s.visible => s.applied = s.boundMembers /\ s.cache /\ s.effects = 1
  /\ s.effects <= 1
  /\ s.floor = 1 => s.result
  /\ s.closed => s.floor = 1
  /\ s.anchorRetired => s.closed
  /\ s.rejected => ~s.visible /\ s.effects = 0
  /\ s.result => s.rejected \/ s.visible
AuthorityWitness == s.merged /\ s.binding = 3 /\ s.coordinator = 1
  /\ ~s.source /\ s.result /\ s.retry = "result" /\ s.retryMerged
  /\ s.retriedBinding = s.originalBinding

(* PROTOCOL projection. Each prepare is a complete immutable protected graph:
   owner sync and B retention are separate; graph membership cannot be invented.
   The normal pair is A/B; fenced recovery uses the intersecting majority B/C.
   Each group has a separate promise and prepare position. *)
ProtocolInit ==
 s = [oprep |-> {}, bprep |-> {}, bdur |-> {}, candidate |-> None,
      synced |-> FALSE, accepted |-> None, captured |-> None,
      capturedDeps |-> {}, promises |-> {}, continuous |-> TRUE,
      bRestart |-> FALSE, diskLost |-> FALSE, fenced |-> FALSE,
      selected |-> None, pair |-> FALSE, term |-> 1,
      decision |-> None, chosen |-> {}, prepareWitness |-> {},
      installed |-> {}, applied |-> {}, barrier |-> FALSE,
      visible |-> FALSE, result |-> FALSE, replied |-> FALSE,
      staleAccept |-> FALSE, checkpoint |-> {}, evidence |-> TRUE,
      snapshot |-> TRUE, gc |-> FALSE]
PrepareLocal(g) == /\ ~s.fenced /\ ~s.diskLost /\ s.candidate = None
                   /\ g \in Groups \ s.oprep
                   /\ s' = [s EXCEPT !.oprep = @ \cup {g}]
PreparePartner(g) == /\ ~s.fenced /\ g \in s.oprep \ s.bprep
                     /\ ~s.bRestart
                     /\ s' = [s EXCEPT !.bprep = @ \cup {g}]
Propose(v) == /\ ~s.fenced /\ ~s.diskLost /\ s.candidate = None
              /\ (v = "abort" \/ Bug = "prepare"
                  \/ (s.oprep = Groups /\ s.bprep = Groups))
              /\ s' = [s EXCEPT !.candidate = v]
SyncCandidate == /\ ~s.fenced /\ ~s.diskLost /\ s.candidate # None /\ ~s.synced
                  /\ s' = [s EXCEPT !.synced = TRUE]
AcceptOld == /\ ~s.bRestart /\ s.candidate # None /\ s.accepted = None
             /\ (~(<<1, "B">> \in s.promises)
                 \/ (Bug = "fence" /\ s.candidate = "commit"))
             /\ (s.candidate = "abort" \/ Bug = "prepare"
                 \/ s.bprep = Groups)
             /\ s' = [s EXCEPT !.accepted = s.candidate,
                      !.staleAccept = <<1, "B">> \in s.promises]
ProtectOld == /\ ~s.fenced /\ ~s.diskLost /\ s.synced
              /\ s.accepted = s.candidate /\ s.candidate # None
              /\ s.decision = None
              /\ s' = [s EXCEPT !.decision = s.candidate,
                       !.chosen = @ \cup {s.candidate},
                       !.prepareWitness = s.oprep \cap s.bprep]
Fence == /\ ~s.fenced
         /\ s' = [s EXCEPT !.fenced = TRUE, !.visible = FALSE,
                  !.barrier = FALSE]
Promise(g,r) ==
  /\ s.fenced /\ <<g,r>> \notin s.promises
  /\ s' = [s EXCEPT !.promises = @ \cup {<<g,r>>},
           !.captured = IF r = "B" /\ g = 1 THEN s.accepted ELSE @,
           !.capturedDeps = IF r = "B" /\ g = 1 /\ s.accepted # None
                             THEN s.bprep ELSE @,
           !.bdur = IF r = "B" THEN @ \cup s.bprep ELSE @]
PartnerRestart ==
  /\ ~s.bRestart /\ ~s.pair
  /\ s' = [s EXCEPT !.bRestart = TRUE, !.bprep = s.bdur,
           !.accepted = s.captured, !.continuous = FALSE,
           !.visible = FALSE, !.barrier = FALSE]
LoseOwnerDisk == /\ ~s.diskLost /\ ~s.pair
                 /\ s' = [s EXCEPT !.diskLost = TRUE, !.oprep = {},
                          !.synced = FALSE, !.visible = FALSE,
                          !.installed = {}, !.applied = {}, !.barrier = FALSE]
Replacement == s.checkpoint = AllCoverage
EvidenceAvailable == s.evidence \/ Replacement
AvailablePrepares == IF Replacement THEN Groups
                     ELSE s.oprep \cup s.bprep \cup s.capturedDeps
OldValue == IF Replacement THEN s.decision
            ELSE IF s.captured # None THEN s.captured
            ELSE IF s.accepted # None THEN s.accepted
            ELSE IF s.synced /\ ~s.diskLost THEN s.candidate ELSE None
Select ==
  /\ s.fenced /\ s.promises = AllPromises /\ s.selected = None
  /\ IF OldValue = None THEN s.continuous
        ELSE OldValue = "abort" \/ AvailablePrepares = Groups
  /\ s' = [s EXCEPT !.selected = IF OldValue = None THEN "abort" ELSE OldValue,
           !.term = 2]
ProtectRecovered ==
  /\ s.selected # None /\ ~s.pair
  /\ (s.selected = "abort" \/ AvailablePrepares = Groups)
  /\ s' = [s EXCEPT !.pair = TRUE, !.decision = s.selected,
           !.chosen = @ \cup {s.selected},
           !.prepareWitness = IF s.selected = "commit" THEN AvailablePrepares
                              ELSE s.prepareWitness]
Ready == s.pair \/ (~s.fenced /\ ~s.diskLost /\ ~s.bRestart)
Install(g) == /\ Ready /\ s.decision # None /\ EvidenceAvailable
              /\ g \in Groups \ s.installed
              /\ (s.decision = "abort" \/ s.pair
                   \/ (s.oprep = Groups /\ s.bprep = Groups))
              /\ s' = [s EXCEPT !.installed = @ \cup {g}]
Apply(g) == /\ Ready /\ s.decision = "commit"
            /\ g \in s.installed \ s.applied
            /\ s' = [s EXCEPT !.applied = @ \cup {g}]
CacheBarrier == /\ Ready /\ s.decision # None /\ ~s.barrier
                /\ s.installed = Groups
                /\ (s.decision = "abort" \/ s.applied = Groups)
                /\ s' = [s EXCEPT !.barrier = TRUE]
Publish == /\ Ready /\ s.decision # None /\ ~s.visible
           /\ ((Bug = "visibility" /\ s.decision = "commit") \/ (s.barrier /\ s.installed = Groups
                /\ (s.decision = "abort" \/ s.applied = Groups)))
           /\ s' = [s EXCEPT !.visible = TRUE]
Result == /\ Ready /\ s.visible /\ ~s.result /\ EvidenceAvailable
          /\ s' = [s EXCEPT !.result = TRUE]
Reply == /\ Ready /\ s.result /\ s.visible /\ ~s.replied
         /\ s' = [s EXCEPT !.replied = TRUE]
Checkpoint(g,r) == /\ s.result /\ <<g,r>> \notin s.checkpoint
                   /\ s' = [s EXCEPT !.checkpoint = @ \cup {<<g,r>>}]
ReleaseSnapshot == /\ s.result /\ s.snapshot
                    /\ s' = [s EXCEPT !.snapshot = FALSE]
GC == /\ ~s.gc /\ s.candidate # None
      /\ ((Bug = "retention" /\ s.candidate = "commit") \/ (s.checkpoint = AllCoverage /\ ~s.snapshot))
      /\ s' = [s EXCEPT !.gc = TRUE, !.evidence = FALSE]
ProtocolNext == (\E g \in Groups : PrepareLocal(g) \/ PreparePartner(g)
                                \/ Install(g) \/ Apply(g))
  \/ (\E v \in Values : Propose(v)) \/ SyncCandidate \/ AcceptOld \/ ProtectOld
  \/ (AllowFaults /\ (Fence \/ (\E g \in Groups, r \in Quorum : Promise(g,r))
                       \/ PartnerRestart \/ LoseOwnerDisk \/ Select \/ ProtectRecovered))
  \/ CacheBarrier \/ Publish \/ Result \/ Reply
  \/ (~RetainHistory /\ ((\E g \in Groups, r \in Replicas : Checkpoint(g,r))
                          \/ ReleaseSnapshot \/ GC))
ProtocolSafety ==
  /\ s.oprep \subseteq Groups /\ s.bprep \subseteq Groups
  /\ s.promises \subseteq AllPromises /\ s.checkpoint \subseteq AllCoverage
  /\ s.chosen \subseteq Values /\ s.term \in 1..2
  /\ s.candidate \in Values \cup {None}
  /\ s.accepted \in Values \cup {None}
  /\ s.applied \subseteq s.installed /\ s.installed \subseteq Groups
  /\ Cardinality(s.chosen) <= 1
  /\ "commit" \in s.chosen => s.prepareWitness = Groups
  /\ ~s.staleAccept
  /\ s.visible => s.barrier /\ s.installed = Groups
                  /\ (s.decision = "abort" \/ s.applied = Groups)
  /\ s.decision = "abort" => s.applied = {}
  /\ s.pair => s.fenced /\ s.promises = AllPromises /\ s.term = 2
  /\ s.selected = "commit" => "abort" \notin s.chosen
  /\ ~s.evidence => s.checkpoint = AllCoverage /\ ~s.snapshot /\ s.result
ProtocolWitness ==
  CASE Witness = "recover" -> s.pair /\ s.selected = "commit" /\ s.replied
                                 /\ s.diskLost /\ ~s.bRestart /\ s.visible
                                 /\ s.installed = Groups /\ s.applied = Groups
    [] Witness = "gap" -> s.diskLost /\ s.bRestart /\ ~s.continuous
                          /\ s.promises = AllPromises /\ s.selected = None
                          /\ s.chosen = {"commit"} /\ AvailablePrepares # Groups
    [] Witness = "partial" -> s.installed = {1} /\ s.applied = {1}
                              /\ ~s.visible /\ s.decision = "commit"
    [] OTHER -> s.replied /\ s.checkpoint = AllCoverage /\ s.gc

(* NAMESPACE projection: stable home IDs, opposing directory moves serialized
   under one gate, exact revisions/permissions held through the decision. *)
NamespaceInit == s =
 [parent |-> [d \in Groups |-> 0], requested |-> {}, rejected |-> {},
  lock |-> 0, prepared |-> FALSE, decision |-> None,
  revision |-> 0, expected |-> 0, permission |-> TRUE,
  entries |-> {"src"}, nlink |-> 1, handle |-> FALSE,
  payload |-> 0, homeAlive |-> TRUE, affinity |-> Groups,
  snapshot |-> TRUE, catchup |-> TRUE, evidence |-> TRUE,
  coverage |-> {}, linked |-> FALSE, wrote |-> FALSE,
  homeVersion |-> 0, replacementVersion |-> 0, staleBlocked |-> FALSE]
Other(d) == 3-d
WouldCycle(d) == s.parent[Other(d)] = d
BeginMove(d) ==
 /\ s.lock = 0 /\ d \notin s.requested
 /\ s' = [s EXCEPT !.requested = @ \cup {d},
          !.lock = IF WouldCycle(d) \/ ~s.permission THEN 0 ELSE d,
          !.rejected = IF WouldCycle(d) \/ ~s.permission THEN @ \cup {d} ELSE @,
          !.expected = s.revision, !.decision = None, !.prepared = FALSE]
PrepareMove == /\ s.lock # 0 /\ ~s.prepared /\ s.evidence
               /\ s.expected = s.revision /\ s.permission
               /\ s' = [s EXCEPT !.prepared = TRUE]
DecideMove == /\ s.prepared /\ s.decision = None /\ s.lock # 0
              /\ s' = [s EXCEPT !.decision = "commit"]
ApplyMove == /\ s.lock # 0 /\ s.decision = "commit" /\ s.evidence
             /\ s' = [s EXCEPT !.parent[s.lock] = Other(s.lock), !.lock = 0,
                      !.revision = @ + 1, !.prepared = FALSE]
Chmod == /\ s.lock = 0 /\ s.permission
         /\ s' = [s EXCEPT !.permission = FALSE, !.revision = @ + 1]
Open == /\ s.entries # {} /\ ~s.handle /\ s.homeAlive
        /\ s' = [s EXCEPT !.handle = TRUE]
Close == /\ s.handle /\ s' = [s EXCEPT !.handle = FALSE]
(* Every same-file namespace/data delta invalidates the prior replacement cut.
   Releasing a snapshot hold is permanent here; new writes restore the distinct
   assigned-replica catch-up obligation, not that already released hold. *)
ChangedHome == [s EXCEPT !.homeVersion = @ + 1,
  !.coverage = IF Bug = "retention" THEN @ ELSE {},
  !.catchup = IF Bug = "retention" THEN @ ELSE TRUE,
  !.staleBlocked = @ \/ (s.coverage = AllCoverage /\ ~s.snapshot /\ ~s.catchup)]
Link == /\ ~s.linked /\ "src" \in s.entries /\ s.homeAlive /\ s.lock = 0
        /\ s' = [ChangedHome EXCEPT !.entries = @ \cup {"dst"}, !.nlink = @ + 1,
                 !.linked = TRUE]
WriteAlias == /\ s.linked /\ ~s.wrote /\ s.entries # {} /\ s.homeAlive
              /\ s.lock = 0
              /\ s' = [ChangedHome EXCEPT !.payload = 1, !.wrote = TRUE]
Unlink(name) == /\ name \in s.entries /\ s.lock = 0
                /\ s' = [ChangedHome EXCEPT !.entries = @ \ {name}, !.nlink = @ - 1]
HomeCheckpoint(g,r) == /\ s.lock = 0 /\ <<g,r>> \notin s.coverage
                       /\ s' = [s EXCEPT !.coverage = @ \cup {<<g,r>>},
                                !.replacementVersion =
                         IF s.coverage \cup {<<g,r>>} = AllCoverage
                           THEN s.homeVersion ELSE @]
ReleasePins == /\ s.coverage = AllCoverage /\ (s.snapshot \/ s.catchup)
                /\ s' = [s EXCEPT !.snapshot = FALSE, !.catchup = FALSE]
ReclaimHome == /\ s.homeAlive /\ s.entries = {} /\ ~s.handle
               /\ ~s.snapshot /\ ~s.catchup /\ s.lock = 0
               /\ s.coverage = AllCoverage
               /\ s' = [s EXCEPT !.homeAlive = FALSE]
NamespaceNext == (\E d \in Groups : BeginMove(d)) \/ PrepareMove \/ DecideMove
 \/ ApplyMove \/ Chmod \/ Open \/ Close \/ Link \/ WriteAlias
 \/ (\E name \in {"src", "dst"} : Unlink(name))
 \/ (\E g \in Groups, r \in Replicas : HomeCheckpoint(g,r))
 \/ ReleasePins \/ ReclaimHome
NamespaceSafety ==
 /\ s.parent \in [Groups -> 0..2] /\ s.lock \in 0..2
 /\ s.entries \subseteq {"src", "dst"} /\ s.nlink \in 0..2
 /\ s.coverage \subseteq AllCoverage
 /\ s.homeVersion \in 0..4 /\ s.replacementVersion \in 0..4
 /\ ~(s.parent[1] = 2 /\ s.parent[2] = 1)
 /\ s.nlink = Cardinality(s.entries)
 /\ (s.entries # {} \/ s.handle \/ s.snapshot \/ s.catchup) => s.homeAlive
 /\ s.lock # 0 => s.expected = s.revision /\ s.permission
 /\ s.affinity = Groups
 /\ ~s.homeAlive => s.coverage = AllCoverage /\ s.entries = {}
                     /\ ~s.handle /\ ~s.snapshot /\ ~s.catchup
                     /\ s.replacementVersion = s.homeVersion
NamespaceLifetimeWitness == s.requested = Groups /\ Cardinality(s.rejected) = 1
 /\ s.permission /\ (s.parent[1] = 2 \/ s.parent[2] = 1)
 /\ s.linked /\ s.wrote /\ s.entries = {"dst"} /\ s.handle /\ s.homeAlive

NamespaceWitness ==
 CASE Witness = "staleCoverage" -> s.staleBlocked /\ s.coverage = {}
          /\ s.catchup /\ ~s.snapshot /\ s.homeAlive
   [] Witness = "reclaimed" -> ~s.homeAlive /\ s.linked /\ s.wrote
          /\ s.staleBlocked /\ s.homeVersion = 4
          /\ s.coverage = AllCoverage /\ s.replacementVersion = s.homeVersion
   [] OTHER -> NamespaceLifetimeWitness

Init == CASE Profile = "authority" -> AuthorityInit
          [] Profile = "protocol" -> ProtocolInit
          [] Profile = "namespace" -> NamespaceInit
Next == CASE Profile = "authority" -> AuthorityNext
          [] Profile = "protocol" -> ProtocolNext
          [] Profile = "namespace" -> NamespaceNext
Safety == CASE Profile = "authority" -> AuthoritySafety
            [] Profile = "protocol" -> ProtocolSafety
            [] Profile = "namespace" -> NamespaceSafety
WitnessAbsent == ~CASE Profile = "authority" -> AuthorityWitness
                        [] Profile = "protocol" -> ProtocolWitness
                        [] Profile = "namespace" -> NamespaceWitness
DiskWorker == (\E g \in Groups : PrepareLocal(g)) \/ SyncCandidate
PartnerWorker == (\E g \in Groups : PreparePartner(g)) \/ AcceptOld
DecisionWorker == (\E v \in Values : Propose(v)) \/ ProtectOld
InstallWorker == (\E g \in Groups : Install(g) \/ Apply(g))
PublicationWorker == CacheBarrier \/ Publish \/ Result \/ Reply
Spec == Init /\ [][Next]_vars
FairSpec == Spec /\ WF_vars(DiskWorker) /\ WF_vars(PartnerWorker)
                 /\ WF_vars(DecisionWorker) /\ WF_vars(InstallWorker)
                 /\ WF_vars(PublicationWorker)
Completion == (s.candidate # None) ~> s.replied
RecoveryWorker == (\E g \in Groups, r \in Quorum : Promise(g,r))
                  \/ Select \/ ProtectRecovered
FairRecoverySpec == FairSpec /\ WF_vars(RecoveryWorker)
RecoveryFeasible == s.fenced /\ (s.pair
  \/ OldValue = "abort" \/ (OldValue = "commit" /\ AvailablePrepares = Groups)
  \/ (OldValue = None /\ s.continuous))
RecoveryCompletion == (<>[]RecoveryFeasible) => <>s.replied
=============================================================================
