----------------------------- MODULE Handles -----------------------------
EXTENDS Integers, FiniteSets, TLC

(* Bounded lifetime service, not a FUSE/kernel or retirement verifier model.
   Pin tuples are exact immutable HANDLE_PIN / HANDLE_RELEASE references.
   Protected selections survive; an unprotected append grants no authority. *)
CONSTANTS MaxGeneration, Namespace, VerifierAvailable, Bug
Objects == {"root", "a", "b", "c"}
Files == Objects \ {"root"}
Names == {"source", "alias", "replace"}
Pins == {"a", "b"} \X (1..MaxGeneration)
NoObject == "none"
VARIABLE h
vars == <<h>>

InitialNames == [n \in Names |-> CASE n = "source" -> "a"
                          [] n = "replace" /\ Namespace -> "b"
                          [] OTHER -> NoObject]
InitialObjects == IF Namespace THEN {"root", "a", "b"} ELSE {"root", "a"}
ObjectPins(o) == {p \in Pins : p[1] = o}
CurrentPins == {p \in Pins : p[2] = h.generation}
References(o) == {n \in Names : h.names[n] = o}
Links(o) == IF o = "root" THEN 2 ELSE Cardinality(References(o))
Local(o) == (h.lookups \cup h.opens) \cap CurrentPins \cap ObjectPins(o)
Active(o) == h.active \cap ObjectPins(o)
Releases(o) == h.selectedRelease \cap ObjectPins(o)
Pending == (\E p \in Pins: h.releaseStage[p] \in {1, 2}) \/
           (\E o \in Files: h.reclaimStage[o] \in {1, 2})
Idle == h.gate = "serving" /\ h.sweep = NoObject /\ ~Pending
Candidate(o) == o # "root" /\ o \in h.payload /\ Links(o) = 0 /\
                References(o) = {} /\ Local(o) = {}

Init == h = [generation |-> 1, gate |-> "serving",
  names |-> InitialNames, nameRevision |-> [n \in Names |-> 1],
  nameHistory |-> {<<n, 1, InitialNames[n]>> : n \in Names},
  allocated |-> InitialObjects, payload |-> InitialObjects, retired |-> {},
  content |-> [o \in Objects |-> IF o \in InitialObjects THEN o ELSE "absent"],
  revision |-> [o \in Objects |-> 1],
  lookups |-> {}, opens |-> {}, usedLookup |-> {}, usedOpen |-> {},
  targets |-> [p \in Pins |-> p[1]],
  everPins |-> {}, active |-> {}, fenced |-> {},
  releaseStage |-> [p \in Pins |-> 0], selectedRelease |-> {},
  releaseAuthorized |-> {}, sweep |-> NoObject, lease |-> FALSE,
  proof |-> <<0, 0>>, newReleases |-> {},
  reclaimStage |-> [o \in Files |-> 0], selectedReclaim |-> {},
  reclaimRefs |-> [o \in Files |-> {}], reclaimAuthorized |-> {},
  linked |-> FALSE, renamed |-> FALSE, created |-> FALSE,
  allocatedAgain |-> FALSE, uncertain |-> FALSE,
  observations |-> {}, crashWindows |-> {}]

(* Acquisition returns only after home pin protection. The same exact pin is
   shared by lookup and open, each acquired at most once per generation. *)
Acquire(p, kind) ==
 /\ Idle /\ p[2] = h.generation /\ p[1] \in h.payload
 /\ h.releaseStage[p] = 0 /\ h.reclaimStage[p[1]] = 0
 /\ (p[1] = "a" \/ (Namespace /\ kind = "open"))
 /\ p \notin (IF kind = "lookup" THEN h.usedLookup ELSE h.usedOpen)
 /\ h' = [h EXCEPT !.everPins = @ \cup {p}, !.active = @ \cup {p},
       !.lookups = IF kind = "lookup" THEN @ \cup {p} ELSE @,
       !.opens = IF kind = "open" THEN @ \cup {p} ELSE @,
       !.usedLookup = IF kind = "lookup" THEN @ \cup {p} ELSE @,
       !.usedOpen = IF kind = "open" THEN @ \cup {p} ELSE @]

(* The departing reference is no longer a client obligation. If it was the
   combined final drain, the retained pin is released in three separate steps. *)
Drop(p, kind) ==
 /\ Idle /\ p[2] = h.generation
 /\ p \in (IF kind = "lookup" THEN h.lookups ELSE h.opens)
 /\ LET remainingLookup == h.lookups \ (IF kind = "lookup" THEN {p} ELSE {})
        remainingOpen == h.opens \ (IF kind = "open" THEN {p} ELSE {})
        final == p \notin remainingLookup /\ p \notin remainingOpen
        brokenFinal == Bug = "combined-drain" /\ kind = "open"
    IN h' = [h EXCEPT !.lookups = remainingLookup, !.opens = remainingOpen,
        !.releaseStage[p] = IF final \/ brokenFinal THEN 1 ELSE 0,
        !.releaseAuthorized = IF final THEN @ \cup {p} ELSE @,
        !.observations = @ \cup
          (IF ~final THEN {IF kind = "lookup" THEN "forget-open-retained"
                          ELSE "close-lookup-retained"} ELSE {})]

ProtectRelease(p) ==
 /\ h.gate = "serving" /\ h.releaseStage[p] = 1
 /\ h' = [h EXCEPT !.releaseStage[p] = 2,
       !.selectedRelease = @ \cup {p}]
ApplyRelease(p) ==
 /\ h.gate = "serving" /\ (h.releaseStage[p] = 2 \/
       (Bug = "unprotected-release" /\ h.releaseStage[p] = 1))
 /\ h' = [h EXCEPT !.releaseStage[p] = 3, !.active = @ \ {p},
       !.newReleases = IF h.sweep = p[1] THEN @ \cup {p} ELSE @]

LinkAlias ==
 /\ Namespace /\ Idle /\ ~h.linked /\ h.names["source"] = "a"
 /\ h.names["alias"] = NoObject
 /\ h' = [h EXCEPT !.names["alias"] = "a", !.linked = TRUE,
       !.nameRevision["alias"] = @ + 1, !.revision["a"] = @ + 1,
       !.nameHistory = @ \cup {<<"alias", h.nameRevision["alias"] + 1, "a">>}]
Unlink(n) ==
 /\ Idle /\ h.names[n] # NoObject
 /\ n = "source" \/ (Namespace /\ n \in {"alias", "replace"})
 /\ LET o == h.names[n] IN
    h' = [h EXCEPT !.names[n] = NoObject,
       !.nameRevision[n] = @ + 1, !.revision[o] = @ + 1,
       !.nameHistory = @ \cup {<<n, h.nameRevision[n] + 1, NoObject>>}]
RenameReplace ==
 /\ Namespace /\ Idle /\ ~h.renamed
 /\ h.names["source"] = "a" /\ h.names["replace"] = "b"
 /\ h' = [h EXCEPT !.names["source"] = NoObject,
       !.names["replace"] = "a", !.renamed = TRUE,
       !.nameRevision["source"] = @ + 1, !.nameRevision["replace"] = @ + 1,
       !.revision["a"] = @ + 1, !.revision["b"] = @ + 1,
       !.targets = IF Bug = "retarget" THEN
           [p \in Pins |-> IF p[1] = "b" /\ p \in h.opens THEN "a" ELSE h.targets[p]]
           ELSE @,
       !.nameHistory = @ \cup
          {<<"source", h.nameRevision["source"] + 1, NoObject>>,
           <<"replace", h.nameRevision["replace"] + 1, "a">>}]
CreateSource ==
 /\ Namespace /\ Idle /\ ~h.created /\ h.names["source"] = NoObject
 /\ LET reuse == Bug = "reuse" /\ "a" \in h.retired
        o == IF reuse THEN "a" ELSE "c"
        revision == IF Bug = "name-revision" THEN 1 ELSE h.nameRevision["source"] + 1
    IN h' = [h EXCEPT !.created = TRUE, !.names["source"] = o,
       !.nameRevision["source"] = revision,
       !.nameHistory = @ \cup {<<"source", revision, o>>},
       !.allocated = @ \cup {o}, !.payload = @ \cup {o},
       !.content[o] = o, !.allocatedAgain = reuse]

(* Positive fencing excludes old kernel nodes, descriptors, mappings and
   transferred paths; merely changing generation never adds a fence. *)
FenceOld(p) ==
 /\ Idle /\ p \in h.active /\ p[2] < h.generation /\ p \notin h.fenced
 /\ h' = [h EXCEPT !.fenced = @ \cup {p},
       !.lookups = @ \ {p}, !.opens = @ \ {p}]
Retain(o, response) ==
 /\ Idle /\ Candidate(o)
 /\ IF VerifierAvailable THEN response \in {"Busy", "WouldBlock"} ELSE response = "no-verifier"
 /\ LET token == IF Active(o) = {} THEN response ELSE
             CASE response = "Busy" -> "Busy-pinned"
               [] response = "WouldBlock" -> "WouldBlock-pinned"
               [] OTHER -> "no-verifier-pinned"
    IN /\ token \notin h.observations
       /\ h' = [h EXCEPT !.observations = @ \cup {response, token}]
AuthorizeSweep(o) ==
 /\ VerifierAvailable /\ Idle /\ Candidate(o)
 /\ Active(o) \subseteq h.fenced \/ Bug = "generation-fence"
 /\ h' = [h EXCEPT !.sweep = o, !.lease = TRUE,
       !.proof = <<h.generation, h.revision[o]>>, !.newReleases = {}]
BeginFencedRelease(p) ==
 /\ h.gate = "serving" /\ h.sweep = p[1] /\ h.lease /\ ~Pending
 /\ p \in h.active /\ h.releaseStage[p] = 0
 /\ h' = [h EXCEPT !.releaseStage[p] = 1,
       !.releaseAuthorized = IF p \in h.fenced THEN @ \cup {p} ELSE @]
BeginReclaim ==
 /\ h.gate = "serving" /\ h.sweep # NoObject /\ h.lease /\ ~Pending
 /\ LET o == h.sweep IN
    /\ Candidate(o) /\ Active(o) = {}
    /\ h.reclaimStage[o] = 0
    /\ h' = [h EXCEPT !.reclaimStage[o] = 1,
         !.reclaimRefs[o] = IF Bug = "prior-release" THEN h.newReleases ELSE Releases(o),
         !.reclaimAuthorized = IF h.proof = <<h.generation, h.revision[o]>>
            THEN @ \cup {o} ELSE @]
ProtectReclaim(o) ==
 /\ h.gate = "serving" /\ h.reclaimStage[o] = 1
 /\ h' = [h EXCEPT !.reclaimStage[o] = 2, !.selectedReclaim = @ \cup {o}]
ApplyReclaim(o) ==
 /\ h.gate = "serving" /\ h.reclaimStage[o] = 2
 /\ h' = [h EXCEPT !.reclaimStage[o] = 3, !.retired = @ \cup {o},
       !.payload = @ \ {o}, !.content[o] = "absent",
       !.sweep = NoObject, !.lease = FALSE]

(* An uncertain append/protection/apply failure blocks, without repairing the
   payload. A later restart replays only the protected selection into fresh roots. *)
Uncertain ==
 /\ h.gate = "serving" /\ Pending
 /\ h' = [h EXCEPT !.gate = "blocked", !.uncertain = TRUE,
       !.lease = FALSE,
       !.content["a"] = IF Bug = "fabricate" /\ "a" \in h.payload THEN "empty" ELSE @]
Crash ==
 /\ h.generation < MaxGeneration /\ h.gate \in {"serving", "blocked"}
 /\ h' = [h EXCEPT !.gate = "crashed", !.lease = FALSE,
       !.crashWindows = @ \cup
          (IF \E p \in Pins: h.releaseStage[p] = 1 THEN {"before-release"} ELSE {}) \cup
          (IF \E p \in Pins: h.releaseStage[p] \in {2, 3}
               THEN {"after-release"} ELSE {}) \cup
          (IF \E o \in Files: h.reclaimStage[o] = 1 THEN {"before-reclaim"} ELSE {}) \cup
          (IF \E o \in Files: h.reclaimStage[o] \in {2, 3}
               THEN {"after-reclaim"} ELSE {})]
Restart ==
 /\ h.gate = "crashed"
 /\ h' = [h EXCEPT !.generation = @ + 1, !.gate = "serving",
       !.active = h.everPins \ h.selectedRelease,
       !.payload = h.allocated \ h.selectedReclaim,
       !.retired = h.selectedReclaim,
       !.content = [o \in Objects |-> IF o \in h.allocated \ h.selectedReclaim THEN o ELSE "absent"],
       !.releaseStage = [p \in Pins |-> IF p \in h.selectedRelease THEN 3 ELSE 0],
       !.reclaimStage = [o \in Files |-> IF o \in h.selectedReclaim THEN 3 ELSE 0],
       !.sweep = NoObject, !.newReleases = {}, !.uncertain = FALSE,
       !.observations = @ \cup
          (IF Releases("a") # {} /\ "a" \notin h.selectedReclaim /\ Links("a") = 0
           THEN {"recovered-orphan"} ELSE {})]

Next == (\E p \in Pins, kind \in {"lookup", "open"}: Acquire(p, kind) \/ Drop(p, kind))
 \/ (\E p \in Pins: ProtectRelease(p) \/ ApplyRelease(p) \/ FenceOld(p) \/ BeginFencedRelease(p))
 \/ LinkAlias \/ (\E n \in Names: Unlink(n)) \/ RenameReplace \/ CreateSource
 \/ (\E o \in Files: AuthorizeSweep(o) \/ ProtectReclaim(o) \/ ApplyReclaim(o) \/
         (\E response \in {"Busy", "WouldBlock", "no-verifier"}: Retain(o, response)))
 \/ BeginReclaim \/ Uncertain \/ Crash \/ Restart
Spec == Init /\ [][Next]_vars

TypeOK ==
 /\ h.generation \in 1..MaxGeneration
 /\ h.gate \in {"serving", "blocked", "crashed"}
 /\ h.names \in [Names -> Objects \cup {NoObject}]
 /\ h.payload \subseteq h.allocated /\ h.retired \subseteq h.allocated
 /\ h.active \subseteq h.everPins /\ h.everPins \subseteq Pins
 /\ h.lookups \subseteq Pins /\ h.opens \subseteq Pins
 /\ h.selectedRelease \subseteq h.everPins /\ h.selectedReclaim \subseteq Files
PinsCoverObligations == \A p \in h.lookups \cup h.opens: p \in h.active
ProtectedReleaseAuthority == h.selectedRelease \subseteq h.releaseAuthorized
                            /\ h.everPins \ h.active \subseteq h.selectedRelease
ReclaimSafety == \A o \in h.selectedReclaim:
 /\ o # "root" /\ Links(o) = 0 /\ References(o) = {}
 /\ ObjectPins(o) \cap (h.active \cup h.lookups \cup h.opens) = {}
 /\ h.reclaimRefs[o] = Releases(o)
 /\ h.everPins \cap ObjectPins(o) \subseteq h.reclaimRefs[o]
 /\ o \in h.reclaimAuthorized
StableHandles == \A p \in h.usedOpen: h.targets[p] = p[1]
NoFabrication == \A o \in h.payload: h.content[o] = o
NoReuse == ~h.allocatedAgain /\ h.payload \cap h.retired = {}
NameIdentity == \A x, y \in h.nameHistory:
                  (x[1] = y[1] /\ x[2] = y[2]) => x[3] = y[3]
RootRetained == "root" \in h.payload /\ "root" \notin h.retired
VerifierSafety == VerifierAvailable \/ h.selectedReclaim = {}
Safety == TypeOK /\ PinsCoverObligations /\ ProtectedReleaseAuthority /\ ReclaimSafety
          /\ StableHandles /\ NoFabrication /\ NoReuse /\ NameIdentity /\ RootRetained /\ VerifierSafety

Witness == CASE
 Bug = "witness-drains" -> {"forget-open-retained", "close-lookup-retained"} \subseteq h.observations
 [] Bug = "witness-retain" -> {"Busy-pinned", "WouldBlock-pinned"} \subseteq h.observations
      /\ "a" \in h.payload /\ <<"a", 1>> \in h.active
 [] Bug = "witness-no-verifier" -> ~VerifierAvailable /\ "no-verifier-pinned" \in h.observations
      /\ "a" \in h.payload /\ <<"a", 1>> \in h.active /\ h.selectedReclaim = {}
 [] Bug = "witness-replacement" -> h.renamed /\ h.linked /\ h.created
      /\ <<"a", 1>> \in h.opens /\ <<"b", 1>> \in h.opens
      /\ h.names["source"] = "c" /\ h.names["replace"] = "a"
 [] Bug = "witness-old-pin" -> h.generation = 2 /\ <<"a", 1>> \in h.active
      /\ <<"a", 1>> \in h.opens /\ <<"a", 1>> \notin h.fenced
      /\ "a" \in h.payload /\ Links("a") = 0 /\ "Busy-pinned" \in h.observations
 [] Bug = "witness-prior-new" -> "a" \in h.retired
      /\ h.reclaimRefs["a"] = {<<"a", 1>>, <<"a", 2>>}
      /\ <<"a", 2>> \in h.newReleases
 [] Bug = "witness-orphan" -> h.generation = 2 /\ "after-release" \in h.crashWindows
      /\ "recovered-orphan" \in h.observations /\ h.releaseStage[<<"a", 1>>] = 3 /\ "a" \in h.payload
      /\ Links("a") = 0 /\ h.reclaimStage["a"] = 0 /\ Idle
 [] Bug = "witness-sweep" -> h.generation = 2 /\ "after-release" \in h.crashWindows
      /\ "recovered-orphan" \in h.observations /\ "a" \in h.retired /\ Releases("a") # {}
 [] Bug = "witness-before-release" -> h.generation = 2 /\ "before-release" \in h.crashWindows
      /\ <<"a", 1>> \in h.active /\ "a" \in h.payload
 [] Bug = "witness-before-reclaim" -> h.generation = 2 /\ "before-reclaim" \in h.crashWindows
      /\ "a" \in h.payload /\ h.reclaimStage["a"] = 0
 [] Bug = "witness-after-reclaim" -> h.generation = 2 /\ "after-reclaim" \in h.crashWindows
      /\ "a" \in h.retired /\ "a" \notin h.payload
 [] OTHER -> FALSE
WitnessAbsent == ~Witness
=============================================================================
