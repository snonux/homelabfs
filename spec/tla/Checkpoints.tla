-------------------------- MODULE Checkpoints --------------------------
EXTENDS Integers, FiniteSets, Sequences, TLC
CONSTANTS Bug, Witness, MaxCrashes, GenesisOnly
ASSUME MaxCrashes \in 0..2
ASSUME Bug \in {"none", "coverage", "archive", "segment", "reservation",
                "payload", "seal", "genesis", "sourceIdentity"}

(* Exact byte/UUID symbols, not a hash implementation or ordered identifiers.
   Two closed source segments and one required open suffix are present. The
   selected effects are only INIT and INSTALL: all other evidence must survive. *)
Frames == <<"OPEN-s1", "VOLUME_INIT", "unselected-create-allocation",
 "request", "session-claim", "PREPARED", "decision", "configuration",
 "authority", "released-lifetime", "SEAL-s1", "OPEN-s2", "INSTALL",
 "unselected-create-attempt", "SEAL-s2", "OPEN-s3", "suffix-write">>
Seals == {11,15}
Cuts == Seals \cup (IF Bug = "seal" THEN {17} ELSE {})
First(k) == IF k = 1 THEN 1 ELSE 12
Last(k) == IF k = 1 THEN 11 ELSE 15
SegmentID(k) == IF k = 1 THEN "s1" ELSE "s2"
Prefix(c) == SubSeq(Frames, 1, c)
Empty == [cut |-> 0, frames |-> <<>>, payload |-> <<"empty",0>>, catalog |-> <<"empty",0>>]
Image(c) == [cut |-> c,
 frames |-> IF Bug = "archive" THEN <<"VOLUME_INIT", "INSTALL">> ELSE Prefix(c),
 payload |-> IF Bug = "payload" THEN <<"changed-bytes",c>> ELSE <<"payload-cut",c>>,
 catalog |-> <<"payload-cut",c>>]
Good(b) == b.cut \in Seals /\ b.frames = Prefix(b.cut) /\ b.payload = b.catalog
ArchivedIDs(b) == {SegmentID(k): k \in {i \in 1..2: Last(i) <= b.cut}}
Reserved == {"external-attempt", "unselected-create-allocation", "unselected-create-attempt"}
VARIABLE s
vars == <<s>>
Init == s = [stage |-> "idle", attempt |-> 0, cut |-> 0,
 captured |-> Empty, baseline |-> Empty, manifest |-> FALSE,
 current |-> Empty, durable |-> Empty, removed |-> {}, durableRemoved |-> {},
 pendingUnlink |-> 0, lease |-> 0, boundary |-> 0, crashes |-> 0,
 up |-> TRUE, recovered |-> FALSE, allocated |-> "none", allocation |-> "none",
 sourceIdentity |-> "s3", publishedCuts |-> {}, observed |-> {}]

Capture(c) ==
 /\ s.up /\ s.stage = "idle" /\ s.attempt < 2 /\ c >= s.current.cut
 /\ s' = [s EXCEPT !.stage = "captured", !.attempt = @+1, !.cut = c,
          !.captured = Image(c), !.baseline = Empty, !.manifest = FALSE,
          !.lease = 0, !.boundary = 0]
PersistBaseline ==
 /\ s.up /\ s.stage = "captured"
 (* Independent verification is abstracted by exact byte equality. Each
    mutation represents the corresponding gate/copy omitted at all layers. *)
 /\ s' = [s EXCEPT !.baseline = s.captured, !.stage = "baseline"]
PersistManifest ==
 /\ s.up /\ s.stage = "baseline"
 /\ s' = [s EXCEPT !.manifest = TRUE, !.stage = "manifest"]
RenameCurrent ==
 /\ s.up /\ s.stage = "manifest" /\ s.manifest
 /\ s' = [s EXCEPT !.current = s.baseline, !.stage = "renamed"]
SyncCurrent ==
 /\ s.up /\ s.stage = "renamed"
 /\ s' = [s EXCEPT !.durable = s.current, !.stage = "published",
          !.publishedCuts = @ \cup {s.current.cut}]
AcquireRetention ==
 /\ s.up /\ s.current.cut # 0 /\ s.lease = 0
 (* One exact fresh complete durable-replica/pin/hold/recovery-point lease.
    Unknown/missing providers have no AcquireRetention transition. *)
 /\ s' = [s EXCEPT !.lease = s.current.cut]
PersistBoundary ==
 /\ s.up /\ s.lease = s.current.cut /\ s.boundary = 0 /\ s.current.cut # 0
 (* Trim re-syncs CURRENT before making even its boundary durable. *)
 /\ s' = [s EXCEPT !.durable = s.current, !.boundary = s.current.cut,
          !.publishedCuts = @ \cup {s.current.cut}]
Unlink(k) ==
 /\ s.up /\ k \notin s.removed /\ s.pendingUnlink = 0
 /\ \A j \in 1..(k-1): j \in s.durableRemoved
 /\ (~GenesisOnly \/ Bug = "genesis")
 /\ ((s.lease = s.current.cut /\ s.boundary = s.current.cut
       /\ s.current.cut # 0 /\ s.current = s.durable /\ Last(k) <= s.durable.cut)
      \/ (Bug = "coverage" /\ s.stage = "captured" /\ Last(k) <= s.cut))
 /\ s' = [s EXCEPT !.removed = @ \cup {k}, !.pendingUnlink = k]
SyncUnlink ==
 /\ s.up /\ s.pendingUnlink # 0
 /\ s' = [s EXCEPT !.durableRemoved = s.removed, !.pendingUnlink = 0]
Finish ==
 /\ s.up /\ s.stage = "published" /\ s.pendingUnlink = 0
 /\ s' = [s EXCEPT !.stage = "idle", !.lease = 0, !.boundary = 0]

(* Before the final parent barriers the rename/unlink can either survive or
   disappear. Immutable files already synced remain, but never grant coverage
   by themselves. A second crash can follow a restart and trim retry. *)
Crash(keepPointer, keepUnlink) ==
 /\ s.up /\ s.crashes < MaxCrashes
 /\ s' = [s EXCEPT !.up = FALSE, !.crashes = @+1,
     !.current = IF keepPointer THEN s.current ELSE s.durable,
     !.durable = IF keepPointer THEN s.current ELSE s.durable,
     !.removed = IF keepUnlink THEN s.removed ELSE s.durableRemoved,
     !.durableRemoved = IF keepUnlink THEN s.removed ELSE s.durableRemoved,
     !.pendingUnlink = 0, !.lease = 0, !.boundary = 0, !.stage = "idle",
     !.captured = Empty,
     !.observed = @ \cup {<<"crash",s.stage>>, <<"unlink",s.pendingUnlink>>}]
Restart ==
 /\ ~s.up
 /\ s' = [s EXCEPT !.up = TRUE, !.recovered = TRUE]

(* Collision admission combines retained source, archive and permanent external
   reservations. Only the input symbol is allocated; the oracle is immutable. *)
AllocateSegment(id) ==
 /\ s.up /\ s.allocated = "none"
 /\ id \notin {SegmentID(k): k \in (1..2) \ s.removed} \cup {"s3"}
 /\ (id \notin ArchivedIDs(s.current) \/ Bug = "segment")
 /\ s' = [s EXCEPT !.allocated = id]
AllocateObject(id) ==
 /\ s.up /\ s.allocation = "none"
 /\ (id \notin Reserved \/ Bug = "reservation")
 /\ s' = [s EXCEPT !.allocation = id]
ImportReusedIdentity ==
 /\ s.up /\ s.removed # {} /\ s.sourceIdentity = "s3"
 /\ Bug = "sourceIdentity"
 /\ s' = [s EXCEPT !.sourceIdentity = SegmentID(CHOOSE k \in s.removed: TRUE)]

Next == (\E c \in Cuts: Capture(c)) \/ PersistBaseline \/ PersistManifest
 \/ RenameCurrent \/ SyncCurrent \/ AcquireRetention \/ PersistBoundary
 \/ (\E k \in 1..2: Unlink(k)) \/ SyncUnlink \/ Finish
 \/ (\E p,u \in BOOLEAN: Crash(p,u)) \/ Restart
 \/ (\E id \in {"s1","s2","s3","fresh"}: AllocateSegment(id))
 \/ (\E id \in Reserved \cup {"fresh"}: AllocateObject(id)) \/ ImportReusedIdentity

PublicationSafety == \A b \in {s.current,s.durable}:
 b.cut = 0 \/ Good(b)
ReclaimSafety == \A k \in s.removed: Last(k) <= s.durable.cut /\ Good(s.durable)
SegmentSafety == s.allocated \notin {"s1","s2","s3"}
ReservationSafety == s.allocation \notin Reserved
SourceIdentitySafety == s.sourceIdentity = "s3"
GenesisSafety == GenesisOnly => s.removed = {}
RecoverySafety == (s.removed # {} => s.current.cut # 0 /\ Good(s.current))
 /\ (2 \in s.removed => 1 \in s.removed)
Safety == PublicationSafety /\ ReclaimSafety /\ SegmentSafety /\ ReservationSafety
 /\ SourceIdentitySafety /\ GenesisSafety /\ RecoverySafety
WitnessReached == CASE Witness = "reclaim" -> s.durableRemoved = {1,2}
 [] Witness = "captureCrash" -> <<"crash","captured">> \in s.observed /\ s.recovered
 [] Witness = "renameCrash" -> <<"crash","renamed">> \in s.observed /\ s.recovered
 [] Witness = "unlinkCrash" -> <<"unlink",1>> \in s.observed /\ s.recovered
                              /\ s.durableRemoved = {1,2}
 [] Witness = "second" -> s.attempt = 2 /\ 11 \in s.publishedCuts
                          /\ s.durable.cut = 15
                          /\ s.durableRemoved = {1,2}
 [] Witness = "collision" -> s.removed = {1,2} /\ s.allocated = "fresh"
                              /\ s.allocation = "fresh"
 [] Witness = "genesis" -> s.durable.cut = 15 /\ s.removed = {} /\ GenesisOnly
 [] OTHER -> FALSE
WitnessAbsent == ~WitnessReached
Spec == Init /\ [][Next]_vars
=============================================================================
