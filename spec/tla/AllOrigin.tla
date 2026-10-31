-------------------------- MODULE AllOrigin --------------------------
(* Atomic complete-fragment model; see README for finite seeds and refinement limits. *)
EXTENDS Naturals, Sequences, FiniteSets, TLC
CONSTANTS MaxOps, Scenario, OwnerOrigin, ThirdOrigin, LossNode, DiskNode,
          Transition, Seed, Bug, Witness
ASSUME /\ MaxOps \in {2,3}
       /\ Scenario \in {"overwrite","append","create","rename","truncate","chmod","transaction"}
       /\ Transition \in {"none","crash","handoff"}
       /\ Seed \in {"fresh","published","replied","pending","decline","staged","result"}
       /\ (Seed#"fresh" => MaxOps=2 /\ ~OwnerOrigin /\ ~ThirdOrigin /\ Scenario="overwrite")
       /\ LossNode \in {"none","A","B","C","X"}
       /\ DiskNode \in {"none","A","B","C","X"}
Ops == 1..MaxOps
Nodes == {"A","B","C","X"}
ReplicaPlacement == {"A","B","C"}
Domains == {"d1","d2"}
Origin(i) == IF i = 1 THEN IF OwnerOrigin THEN "A" ELSE "B"
             ELSE IF ThirdOrigin /\ i = 2 THEN "C" ELSE "X"
Memory(i) == IF Bug = "self-memory" THEN Origin(i)
             ELSE IF Origin(i) = "A" THEN "B" ELSE "A"
LocalIndex(i) == Cardinality({j \in 1..i: Origin(j)=Origin(i)})
(* Stream positions are per-origin; slot serials are separately assigned. *)
GrantPosition(i) == 3*LocalIndex(i)-2
ChoicePosition(i) == 3*LocalIndex(i)-1
ResultPosition(i) == 3*LocalIndex(i)
Initial == [bytes |-> IF Scenario="append" THEN <<>> ELSE <<"a","b","c","d">>,
            names |-> IF Scenario="create" THEN {} ELSE {"src"}, writable |-> TRUE]
Resize(b,n) == [j \in 1..n |-> IF j <= Len(b) THEN b[j] ELSE "0"]
Outcome(i,v) ==
    CASE Scenario="append" ->
         [after |-> [v EXCEPT !.bytes = @ \o (IF i=1 THEN <<"a","a">> ELSE <<"b","b">>)], result |-> Len(v.bytes)]
      [] Scenario="create" ->
         [after |-> [v EXCEPT !.names = @ \cup {"new"}], result |-> IF "new" \in v.names THEN "EEXIST" ELSE "created"]
      [] Scenario="rename" ->
         IF i=1 THEN [after |-> [v EXCEPT !.names = IF "src" \in @ THEN (@ \ {"src"}) \cup {"dst"} ELSE @], result |-> "rename"]
         ELSE [after |-> [v EXCEPT !.names = @ \ {"dst"}], result |-> IF "dst" \in v.names THEN "unlink" ELSE "ENOENT"]
      [] Scenario="chmod" ->
         IF i=1 THEN [after |-> [v EXCEPT !.writable=FALSE], result |-> "chmod"]
         ELSE [after |-> IF v.writable THEN [v EXCEPT !.bytes= <<"W">>] ELSE v,
               result |-> IF v.writable THEN "written" ELSE "EACCES"]
      [] Scenario="truncate" ->
         [after |-> [v EXCEPT !.bytes = CASE i=1 -> <<"a","X","Y","d">>
                                                  [] i=2 -> Resize(@,2)
                                                  [] OTHER -> Resize(@,4)], result |-> i]
      [] Scenario="transaction" ->
         IF i=1 THEN [after |-> [v EXCEPT !.bytes= <<"T">>, !.names={"dst"}], result |-> "transaction"]
         ELSE [after |-> [v EXCEPT !.writable=FALSE], result |-> "barrier-chmod"]
      [] OTHER -> [after |-> [v EXCEPT !.bytes=[j \in 1..Len(@) |-> IF j=2 THEN IF i=1 THEN "X" ELSE "Y" ELSE @[j]]], result |-> i]

Seeded == Seed#"fresh"
SeedVisible == IF Seed \in {"published","replied"} THEN 2 ELSE 1
SeedSelected == IF Seed \in {"published","replied","result"} THEN 2 ELSE 1
SeedPhase(i) == IF i=1 THEN 3 ELSE CASE Seed="staged" -> 1
                [] Seed \in {"pending","decline"} -> 2 [] OTHER -> 3
SeedChoice(i) == IF i=1 THEN "commit" ELSE CASE Seed="staged" -> "none"
                 [] Seed="decline" -> "decline" [] OTHER -> "commit"
RECURSIVE SeedView(_)
SeedView(k) == IF k=0 THEN Initial ELSE Outcome(k,SeedView(k-1)).after

VARIABLES grant, base, plan, status, choice, disk, ram,
          sent, emitted, learned, stamp, selected, visible, view,
          backing, backingView, replied, acked, mapClosed, up, eligible,
          known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
          originFenced, transition, excluded, reports, archive, frontier, projection,
          activated, dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut,
          readOnNewOwner, readOK, publishOK, selectionOK, recoveryOK
vars == <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, reports, archive, frontier, projection,
        activated, dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut,
        readOnNewOwner, readOK, publishOK, selectionOK, recoveryOK>>


(* choice is immutable external history; disk/ram are physical retained bytes.
   An emitted ACK remains in the network after RAM loss. Never consult up[M],
   ram or the actual incarnation to infer whether a remote old ACK is current. *)
Range(s) == {s[k]: k \in 1..Len(s)}
Granted == {i \in Ops: grant[i]>0}
SlotOp(s) == CHOOSE i \in Ops: grant[i]=s
GrantOrder == [s \in 1..Cardinality(Granted) |-> SlotOp(s)]
RECURSIVE Oracle(_)
Oracle(s) == IF Len(s)=0 THEN Initial
             ELSE LET i == s[Len(s)]
                      prior == Oracle(SubSeq(s,1,Len(s)-1))
                  IN IF choice[i]#"commit" THEN prior ELSE Outcome(i,prior).after
Expected(k) == Oracle(SubSeq(selected,1,k))
Current == {i \in Granted: grant[i]>visible}
OldActive == ~transition /\ ~activated
OwnerRunning == up["A"] /\ "A" \in eligible
KnownCurrent(i) == Memory(i) \notin known /\ Origin(i) \notin known
Matching(i) == stamp[i]= <<pin[Memory(i)],pin[Origin(i)]>> /\ KnownCurrent(i)
HasSource(i,l) == archive[i]>=l \/ \E n \in Nodes: disk[n][i]>=l
AllSources == \A i \in Granted: choice[i]="none" \/ HasSource(i,2)
EquivalentSource(i,n) == archive[i]>=2 \/ \E m \in Nodes \ {n}: disk[m][i]>=2
Covered(n) == n \in reports \/
    (frontier /\ n \in excluded /\
     \A i \in Granted: Origin(i)#n \/ (choice[i]#"none" /\ EquivalentSource(i,n)))
CompleteSurvey == \A n \in Nodes: Covered(n)
ClosedChoices == \A i \in Granted:
    IF choice[i]="none" THEN Origin(i) \in reports ELSE HasSource(i,2)
Max2(a,b) == IF a>b THEN a ELSE b
Save(n) == [i \in Ops |-> Max2(archive[i], Max2(disk[n][i],IF Memory(i)=n THEN ram[i] ELSE 0))]
SelectionEvidence(i) == IF Origin(i)="A" THEN learned[i]>=2 /\ Matching(i) ELSE ram[i]>=2 /\ KnownCurrent(i)
ResultEvidence(i) == IF Origin(i)="A" THEN learned[i]>=3 /\ Matching(i) ELSE ram[i]>=3 /\ KnownCurrent(i)
NoSelf == \A i \in Granted: Memory(i)#Origin(i)

Init == /\ grant = (IF Seeded THEN ([i \in Ops |-> i]) ELSE ([i \in Ops |-> 0]))
        /\ base = (IF Seeded THEN ([i \in Ops |-> i-1]) ELSE ([i \in Ops |-> 0]))
        /\ plan = (IF Seeded THEN ([i \in Ops |-> SeedView(i)]) ELSE ([i \in Ops |-> Initial]))
        /\ status = (IF Seeded THEN ([i \in Ops |-> IF SeedChoice(i)="decline" THEN "NO_EFFECT" ELSE Outcome(i,SeedView(i-1)).result]) ELSE ([i \in Ops |-> "none"]))
        /\ choice = (IF Seeded THEN ([i \in Ops |-> SeedChoice(i)]) ELSE ([i \in Ops |-> "none"]))
        /\ disk = (IF Seeded THEN ([n \in Nodes |-> [i \in Ops |-> IF n=Origin(i) THEN SeedPhase(i) ELSE 0]]) ELSE ([n \in Nodes |-> [i \in Ops |-> 0]]))
        /\ ram = (IF Seeded THEN ([i \in Ops |-> SeedPhase(i)]) ELSE ([i \in Ops |-> 0]))
        /\ sent = (IF Seeded THEN ([i \in Ops |-> SeedPhase(i)]) ELSE ([i \in Ops |-> 0]))
        /\ emitted = (IF Seeded THEN ([i \in Ops |-> SeedPhase(i)]) ELSE ([i \in Ops |-> 0]))
        /\ learned = (IF Seeded THEN ([i \in Ops |-> SeedPhase(i)]) ELSE ([i \in Ops |-> 0]))
        /\ stamp = ([i \in Ops |-> <<0,0>>])
        /\ selected = (IF Seeded THEN ([i \in 1..SeedSelected |-> i]) ELSE (<<>>))
        /\ visible = (IF Seeded THEN (SeedVisible) ELSE (0))
        /\ view = (IF Seeded THEN (SeedView(SeedVisible)) ELSE (Initial))
        /\ backing = ([n \in Nodes |-> 0])
        /\ backingView = ([n \in Nodes |-> Initial])
        /\ replied = (IF Seeded THEN (IF Seed="replied" THEN Ops ELSE {}) ELSE ({}))
        /\ acked = ({})
        /\ mapClosed = (IF Seeded THEN (TRUE) ELSE (FALSE))
        /\ up = ([n \in Nodes |-> TRUE])
        /\ eligible = (Nodes)
        /\ known = ({})
        /\ inc = ([n \in Nodes |-> 0])
        /\ pin = ([n \in Nodes |-> 0])
        /\ ramLost = (FALSE)
        /\ diskLost = (FALSE)
        /\ originFenced = (FALSE)
        /\ lossCut = (0)
        /\ lossSelected = (0)
        /\ transition = (FALSE)
        /\ excluded = ({})
        /\ reports = ({})
        /\ archive = ([i \in Ops |-> 0])
        /\ frontier = (FALSE)
        /\ projection = (FALSE)
        /\ activated = (FALSE)
        /\ dependencyReady = (Scenario#"transaction")
        /\ retrySeen = (FALSE)
        /\ conflictSeen = (FALSE)
        /\ lateAck = (FALSE)
        /\ readCut = ([d \in Domains |-> 0])
        /\ readOnNewOwner = (FALSE)
        /\ readBeforeApply = (FALSE)
        /\ readOK = (TRUE)
        /\ publishOK = (TRUE)
        /\ selectionOK = (TRUE)
        /\ recoveryOK = (TRUE)

Grant(i) ==
    /\ (OldActive /\ OwnerRunning) = TRUE
    /\ (i \notin Granted /\ Origin(i) \notin known) = TRUE
    /\ (Current={} \/ Bug="skip-hole") = TRUE
    /\ (Len(selected)=visible) = TRUE
    /\ (Scenario#"truncate" \/ i=Cardinality(Granted)+1) = TRUE
    /\ grant' = ([grant EXCEPT ![i]=Cardinality(Granted)+1])
    /\ base' = ([base EXCEPT ![i]=IF Bug="stale-plan" THEN backing["A"] ELSE visible])
    /\ plan' = ([plan EXCEPT ![i]=Outcome(i,IF Bug="stale-plan" THEN backingView["A"] ELSE view).after])
    /\ status' = ([status EXCEPT ![i]=Outcome(i,view).result])
    /\ UNCHANGED <<choice, disk, ram, sent, emitted, learned, stamp,
        selected, visible, view, backing, backingView, replied, acked,
        mapClosed, up, eligible, known, inc, pin, ramLost,
        diskLost, lossCut, lossSelected, originFenced, transition, excluded, reports,
        archive, frontier, projection, activated, dependencyReady, retrySeen, conflictSeen,
        lateAck, readBeforeApply, readCut, readOnNewOwner, readOK, publishOK, selectionOK,
        recoveryOK>>

Stage(i) ==
    /\ (~activated /\ i \in Granted /\ Origin(i) \notin excluded) = TRUE
    /\ (up[Origin(i)] /\ Origin(i) \in eligible) = TRUE
    /\ (disk[Origin(i)][i]=0) = TRUE
    /\ disk' = ([disk EXCEPT ![Origin(i)][i]=1])
    /\ sent' = ([sent EXCEPT ![i]=1])
    /\ UNCHANGED <<grant, base, plan, status, choice, ram, emitted,
        learned, stamp, selected, visible, view, backing, backingView,
        replied, acked, mapClosed, up, eligible, known, inc,
        pin, ramLost, diskLost, lossCut, lossSelected, originFenced, transition,
        excluded, reports, archive, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

Retain(i,l) ==
    /\ (~activated /\ i \in Granted /\ up[Memory(i)] /\ Memory(i) \in eligible) = TRUE
    /\ (Memory(i) \notin excluded /\ ram[i]<l /\ sent[i]>=l) = TRUE
    /\ (l \in 1..3) = TRUE
    /\ (dependencyReady \/ Bug="dependencies") = TRUE
    /\ (Memory(i)#Origin(i) \/ Bug="self-memory") = TRUE
    /\ ram' = ([ram EXCEPT ![i]=l])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, sent,
        emitted, learned, stamp, selected, visible, view, backing,
        backingView, replied, acked, mapClosed, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

Emit(i) ==
    /\ (~activated /\ up[Memory(i)] /\ Memory(i) \in eligible) = TRUE
    /\ (ram[i]>0 /\ (emitted[i]<ram[i] \/ stamp[i]#<<inc[Memory(i)],inc[Origin(i)]>>)) = TRUE
    /\ (Memory(i) \notin excluded) = TRUE
    /\ emitted' = ([emitted EXCEPT ![i]=ram[i]])
    /\ stamp' = ([stamp EXCEPT ![i]= <<inc[Memory(i)],inc[Origin(i)]>>])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, learned, selected, visible, view, backing, backingView,
        replied, acked, mapClosed, up, eligible, known, inc,
        pin, ramLost, diskLost, lossCut, lossSelected, originFenced, transition,
        excluded, reports, archive, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

Deliver(i) ==
    /\ (~activated /\ emitted[i]>learned[i] /\ Matching(i)) = TRUE
    /\ (up[Origin(i)] /\ Origin(i) \in eligible /\ Origin(i) \notin excluded) = TRUE
    /\ learned' = ([learned EXCEPT ![i]=emitted[i]])
    /\ lateAck' = (IF Witness="late-ack" THEN lateAck \/ ~up[Memory(i)] ELSE lateAck)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, stamp, selected, visible, view, backing,
        backingView, replied, acked, mapClosed, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

Commit(i) ==
    /\ (~activated /\ i \in Granted /\ Origin(i) \notin excluded) = TRUE
    /\ (up[Origin(i)] /\ Origin(i) \in eligible) = TRUE
    /\ (choice[i]="none" /\ disk[Origin(i)][i]=1 /\ learned[i]>=1 /\ Matching(i)) = TRUE
    /\ choice' = ([choice EXCEPT ![i]="commit"])
    /\ disk' = ([disk EXCEPT ![Origin(i)][i]=2])
    /\ sent' = ([sent EXCEPT ![i]=2])
    /\ UNCHANGED <<grant, base, plan, status, ram, emitted, learned,
        stamp, selected, visible, view, backing, backingView, replied,
        acked, mapClosed, up, eligible, known, inc, pin,
        ramLost, diskLost, lossCut, lossSelected, originFenced, transition, excluded,
        reports, archive, frontier, projection, activated, dependencyReady, retrySeen,
        conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK, publishOK,
        selectionOK, recoveryOK>>

Decline(i) ==
    /\ (~activated /\ i \in Granted /\ Origin(i) \notin excluded) = TRUE
    /\ (up[Origin(i)] /\ Origin(i) \in eligible) = TRUE
    /\ (choice[i]="none" /\ disk[Origin(i)][i]>=1) = TRUE
    /\ status' = ([status EXCEPT ![i]="NO_EFFECT"])
    /\ choice' = ([choice EXCEPT ![i]="decline"])
    /\ disk' = ([disk EXCEPT ![Origin(i)][i]=2])
    /\ sent' = ([sent EXCEPT ![i]=2])
    /\ UNCHANGED <<grant, base, plan, ram, emitted, learned, stamp,
        selected, visible, view, backing, backingView, replied, acked,
        mapClosed, up, eligible, known, inc, pin, ramLost,
        diskLost, lossCut, lossSelected, originFenced, transition, excluded, reports,
        archive, frontier, projection, activated, dependencyReady, retrySeen, conflictSeen,
        lateAck, readBeforeApply, readCut, readOnNewOwner, readOK, publishOK, selectionOK,
        recoveryOK>>

Select(i) ==
    /\ (OldActive /\ OwnerRunning /\ i \in Granted) = TRUE
    /\ (grant[i]=Len(selected)+1 /\ choice[i]#"none") = TRUE
    /\ (SelectionEvidence(i) \/ Bug="prepare-commit") = TRUE
    /\ selected' = (Append(selected,i))
    /\ selectionOK' = (selectionOK /\ SelectionEvidence(i) /\ sent[i]>=2 /\ Memory(i)#Origin(i) /\ dependencyReady /\ base[i]=Len(selected))
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, visible, view, backing,
        backingView, replied, acked, mapClosed, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, recoveryOK>>

Result(i) ==
    /\ (~activated /\ i \in Range(selected)) = TRUE
    /\ (up[Origin(i)] /\ Origin(i) \in eligible /\ Origin(i) \notin excluded) = TRUE
    /\ (disk[Origin(i)][i]=2) = TRUE
    /\ disk' = ([disk EXCEPT ![Origin(i)][i]=3])
    /\ sent' = ([sent EXCEPT ![i]=3])
    /\ UNCHANGED <<grant, base, plan, status, choice, ram, emitted,
        learned, stamp, selected, visible, view, backing, backingView,
        replied, acked, mapClosed, up, eligible, known, inc,
        pin, ramLost, diskLost, lossCut, lossSelected, originFenced, transition,
        excluded, reports, archive, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

CloseWritableMap ==
    /\ (OldActive /\ ~mapClosed) = TRUE
    /\ mapClosed' = (TRUE)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

ViewBarrier(d) ==
    /\ (OldActive /\ OwnerRunning /\ visible<Len(selected)) = TRUE
    /\ (d \notin acked /\ (d#"d2" \/ mapClosed)) = TRUE
    /\ (LET i == selected[visible+1] IN ResultEvidence(i) \/ Bug="result") = TRUE
    /\ acked' = (acked \cup {d})
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, mapClosed, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

Publish ==
    /\ (OldActive /\ OwnerRunning /\ visible<Len(selected)) = TRUE
    /\ (LET i == selected[visible+1] IN ResultEvidence(i) \/ Bug="result") = TRUE
    /\ (acked=Domains \/ Bug="barrier") = TRUE
    /\ visible' = (visible+1)
    /\ view' = (IF choice[selected[visible+1]]="decline" THEN view ELSE plan[selected[visible+1]])
    /\ acked' = ({})
    /\ publishOK' = (publishOK /\ acked=Domains /\ ResultEvidence(selected[visible+1]))
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, backing, backingView,
        replied, mapClosed, up, eligible, known, inc, pin,
        ramLost, diskLost, lossCut, lossSelected, originFenced, transition, excluded,
        reports, archive, frontier, projection, activated, dependencyReady, retrySeen,
        conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK, selectionOK,
        recoveryOK>>

Reply(i) ==
    /\ (~activated /\ i \in Granted /\ grant[i]<=visible /\ i \notin replied /\ Origin(i) \notin excluded) = TRUE
    /\ (up[Origin(i)] /\ Origin(i) \in eligible /\ KnownCurrent(i)) = TRUE
    /\ (disk[Origin(i)][i]>=3 /\ learned[i]>=3) = TRUE
    /\ replied' = (replied \cup {i})
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, acked, mapClosed, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

Apply(n) ==
    /\ (backing[n]<Len(selected)) = TRUE
    /\ (OldActive /\ n \in {"A","C"} /\ up[n] /\ n \in eligible) = TRUE
    /\ backing' = ([backing EXCEPT ![n]=@+1])
    /\ backingView' = ([backingView EXCEPT ![n]=Expected(backing[n]+1)])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        replied, acked, mapClosed, up, eligible, known, inc,
        pin, ramLost, diskLost, lossCut, lossSelected, originFenced, transition,
        excluded, reports, archive, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

Read(d) ==
    /\ ((~transition /\ OwnerRunning) \/ activated) = TRUE
    /\ readOnNewOwner' = (IF Witness \in {"handoff","recovery","closed-absence","closed-decline"} THEN readOnNewOwner \/ activated ELSE readOnNewOwner)
    /\ readCut' = (IF Witness="served" THEN [readCut EXCEPT ![d]=visible] ELSE readCut)
    /\ readOK' = (LET observed == IF Bug="stale-read" THEN backingView["A"] ELSE IF Bug="speculative-read" /\ Current#{} THEN plan[CHOOSE i \in Current: TRUE] ELSE view IN readOK /\ observed=Expected(visible) /\ (\A i \in replied: grant[i]<=visible))
    /\ readBeforeApply' = (IF Witness="staged-hidden" THEN readBeforeApply \/ (Current#{} /\ visible=1 /\ visible>backing["A"]) ELSE IF Witness \in {"served","truncate","handoff"} THEN readBeforeApply \/ (visible>backing[IF activated THEN "C" ELSE "A"] /\ visible>0) ELSE readBeforeApply)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, reports, archive, frontier, projection,
        activated, dependencyReady, retrySeen, conflictSeen, lateAck, publishOK, selectionOK,
        recoveryOK>>

Retry(i,exact) ==
    /\ (i \in Granted) = TRUE
    /\ retrySeen' = (IF Witness \in {"append-order","unknown"} THEN retrySeen \/ exact ELSE retrySeen)
    /\ conflictSeen' = (IF Witness="unknown" THEN conflictSeen \/ ~exact ELSE conflictSeen)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, reports, archive, frontier, projection,
        activated, dependencyReady, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

FetchDependency ==
    /\ (~dependencyReady) = TRUE
    /\ dependencyReady' = (TRUE)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, reports, archive, frontier, projection,
        activated, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

LoseRAM ==
    /\ (LossNode#"none" /\ ~ramLost /\ ~activated) = TRUE
    /\ lossCut' = (IF LossNode="A" THEN visible ELSE lossCut)
    /\ lossSelected' = (IF LossNode="A" THEN Len(selected) ELSE lossSelected)
    /\ ramLost' = (TRUE)
    /\ up' = ([up EXCEPT ![LossNode]=FALSE])
    /\ eligible' = (eligible \ {LossNode})
    /\ ram' = ([i \in Ops |-> IF Memory(i)=LossNode THEN 0 ELSE ram[i]])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, sent,
        emitted, learned, stamp, selected, visible, view, backing,
        backingView, replied, acked, mapClosed, known, inc, pin,
        diskLost, originFenced, transition, excluded, reports, archive, frontier,
        projection, activated, dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply,
        readCut, readOnNewOwner, readOK, publishOK, selectionOK, recoveryOK>>

LoseDisk ==
    /\ (DiskNode#"none" /\ ~diskLost /\ ~activated) = TRUE
    /\ diskLost' = (TRUE)
    /\ disk' = ([disk EXCEPT ![DiskNode]=[i \in Ops |-> 0]])
    /\ up' = ([up EXCEPT ![DiskNode]=FALSE])
    /\ eligible' = (eligible \ {DiskNode})
    /\ ram' = ([i \in Ops |-> IF Memory(i)=DiskNode THEN 0 ELSE ram[i]])
    /\ UNCHANGED <<grant, base, plan, status, choice, sent, emitted,
        learned, stamp, selected, visible, view, backing, backingView,
        replied, acked, mapClosed, known, inc, pin, ramLost,
        lossCut, lossSelected, originFenced, transition, excluded, reports, archive,
        frontier, projection, activated, dependencyReady, retrySeen, conflictSeen, lateAck,
        readBeforeApply, readCut, readOnNewOwner, readOK, publishOK, selectionOK, recoveryOK>>

LearnLoss(n) ==
    /\ (~up[n] /\ n \notin known /\ ~activated) = TRUE
    /\ known' = (known \cup {n})
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

FenceOrigin ==
    /\ (OldActive /\ ~up["X"] /\ "X" \in known /\ ~originFenced) = TRUE
    /\ originFenced' = (TRUE)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

RestartOrigin ==
    /\ (~up["X"] /\ "X" \in known /\ inc["X"]=0 /\ ~diskLost /\ originFenced) = TRUE
    /\ (OldActive /\ OwnerRunning) = TRUE
    /\ up' = ([up EXCEPT !["X"]=TRUE])
    /\ inc' = ([inc EXCEPT !["X"]=1])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, eligible, known,
        pin, ramLost, diskLost, lossCut, lossSelected, originFenced, transition,
        excluded, reports, archive, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

ReconcileOrigin ==
    /\ (up["X"] /\ "X" \notin eligible /\ inc["X"]=1) = TRUE
    /\ (OldActive /\ OwnerRunning) = TRUE
    /\ (\A i \in Granted: Origin(i)#"X" \/ disk["X"][i]>0) = TRUE
    /\ eligible' = (eligible \cup {"X"})
    /\ known' = (known \ {"X"})
    /\ pin' = ([pin EXCEPT !["X"]=1])
    /\ learned' = ([i \in Ops |-> IF Origin(i)="X" THEN 0 ELSE learned[i]])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, stamp, selected, visible, view, backing,
        backingView, replied, acked, mapClosed, up, inc, ramLost,
        diskLost, lossCut, lossSelected, originFenced, transition, excluded, reports,
        archive, frontier, projection, activated, dependencyReady, retrySeen, conflictSeen,
        lateAck, readBeforeApply, readCut, readOnNewOwner, readOK, publishOK, selectionOK,
        recoveryOK>>

BeginTransition ==
    /\ (Transition#"none" /\ ~transition /\ ~activated) = TRUE
    /\ transition' = (TRUE)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

TransferProjection ==
    /\ (transition /\ Transition="handoff" /\ up["A"] /\ "A" \notin excluded) = TRUE
    /\ (visible=Len(selected) /\ ~projection) = TRUE
    /\ projection' = (TRUE)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, reports, archive, frontier, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

SaveRAM(n) ==
    /\ (transition /\ up[n] /\ n \notin excluded) = TRUE
    /\ (archive#Save(n)) = TRUE
    /\ archive' = (Save(n))
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, reports, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

Exclude(n) ==
    /\ (transition /\ n \notin excluded) = TRUE
    /\ ((Transition="crash" /\ (\A i \in Ops: Memory(i)#n \/ ~up[n] \/ archive[i]>=ram[i])) \/ (Transition="handoff" /\ up[n] /\ (n#"A" \/ projection))) = TRUE
    /\ excluded' = (excluded \cup {n})
    /\ archive' = (IF Transition="handoff" THEN Save(n) ELSE archive)
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, reports, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

RecoverReporter(n) ==
    /\ (transition /\ Transition="crash" /\ n \in excluded) = TRUE
    /\ (~up[n] /\ inc[n]=0 /\ ~(diskLost /\ DiskNode=n)) = TRUE
    /\ up' = ([up EXCEPT ![n]=TRUE])
    /\ inc' = ([inc EXCEPT ![n]=1])
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, eligible, known,
        pin, ramLost, diskLost, lossCut, lossSelected, originFenced, transition,
        excluded, reports, archive, frontier, projection, activated, dependencyReady,
        retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK,
        publishOK, selectionOK, recoveryOK>>

Report(n) ==
    /\ (transition /\ (n \in excluded \/ Bug="fence") /\ n \notin reports) = TRUE
    /\ (up[n] /\ ~(diskLost /\ DiskNode=n)) = TRUE
    /\ reports' = (reports \cup {n})
    /\ archive' = (Save(n))
    /\ frontier' = (frontier \/ (n="A" /\ ~((ramLost /\ LossNode="A") \/ (diskLost /\ DiskNode="A"))))
    /\ UNCHANGED <<grant, base, plan, status, choice, disk, ram,
        sent, emitted, learned, stamp, selected, visible, view,
        backing, backingView, replied, acked, mapClosed, up, eligible,
        known, inc, pin, ramLost, diskLost, lossCut, lossSelected,
        originFenced, transition, excluded, projection, activated, dependencyReady, retrySeen,
        conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner, readOK, publishOK,
        selectionOK, recoveryOK>>

AsyncPersist(i,n) ==
    /\ ((Transition#"none" \/ LossNode#"none" \/ DiskNode#"none") /\ ~activated) = TRUE
    /\ (n="C" /\ i \in Granted /\ sent[i]>=3 /\ up[n] /\ n \notin excluded) = TRUE
    /\ (HasSource(i,3) /\ disk[n][i]<3) = TRUE
    /\ disk' = ([disk EXCEPT ![n][i]=3])
    /\ UNCHANGED <<grant, base, plan, status, choice, ram, sent,
        emitted, learned, stamp, selected, visible, view, backing,
        backingView, replied, acked, mapClosed, up, eligible, known,
        inc, pin, ramLost, diskLost, lossCut, lossSelected, originFenced,
        transition, excluded, reports, archive, frontier, projection, activated,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK, recoveryOK>>

Activate ==
    /\ (transition /\ ~activated) = TRUE
    /\ (((excluded=Nodes \/ Bug="fence") /\ CompleteSurvey /\ ClosedChoices /\ AllSources /\ dependencyReady) \/ (Bug="majority" /\ excluded=Nodes /\ ramLost /\ diskLost)) = TRUE
    /\ (Transition#"handoff" \/ projection) = TRUE
    /\ status' = ([i \in Ops |-> IF i \in Granted /\ choice[i]="none" THEN "NO_EFFECT" ELSE status[i]])
    /\ activated' = (TRUE)
    /\ transition' = (FALSE)
    /\ selected' = (GrantOrder)
    /\ choice' = ([i \in Ops |-> IF i \in Granted /\ choice[i]="none" THEN "decline" ELSE choice[i]])
    /\ visible' = (Cardinality(Granted))
    /\ view' = (Oracle(GrantOrder))
    /\ disk' = (IF Transition="handoff" THEN disk ELSE [n \in Nodes |-> IF n \in {"B","C"} THEN [i \in Ops |-> IF i \in Granted THEN 3 ELSE disk[n][i]] ELSE disk[n]])
    /\ recoveryOK' = (recoveryOK /\ excluded=Nodes /\ CompleteSurvey /\ ClosedChoices /\ AllSources /\ dependencyReady)
    /\ UNCHANGED <<grant, base, plan, ram, sent, emitted, learned,
        stamp, backing, backingView, replied, acked, mapClosed, up,
        eligible, known, inc, pin, ramLost, diskLost, lossCut,
        lossSelected, originFenced, excluded, reports, archive, frontier, projection,
        dependencyReady, retrySeen, conflictSeen, lateAck, readBeforeApply, readCut, readOnNewOwner,
        readOK, publishOK, selectionOK>>

Next == \/ \E i \in Ops: Grant(i) \/ Stage(i) \/ Emit(i) \/ Deliver(i)
                  \/ Commit(i) \/ Decline(i) \/ Select(i) \/ Result(i) \/ Reply(i)
                  \/ \E l \in 1..3: Retain(i,l)
        \/ \E n \in Nodes: Apply(n) \/ LearnLoss(n) \/ Exclude(n) \/ Report(n) \/ SaveRAM(n) \/ RecoverReporter(n)
        \/ \E i \in Ops, n \in Nodes: AsyncPersist(i,n)
        \/ \E d \in Domains: ViewBarrier(d) \/ Read(d)
        \/ \E i \in Ops, exact \in BOOLEAN: Retry(i,exact)
        \/ Publish \/ CloseWritableMap \/ FetchDependency \/ LoseRAM \/ LoseDisk
        \/ FenceOrigin \/ RestartOrigin \/ ReconcileOrigin \/ BeginTransition \/ TransferProjection \/ Activate
Spec == Init /\ [][Next]_vars
ResultCorrect(i) == IF choice[i]="decline" THEN status[i]="NO_EFFECT"
                    ELSE status[i]=Outcome(i,Expected(grant[i]-1)).result
Safety == /\ selectionOK /\ publishOK /\ readOK /\ recoveryOK
          /\ visible<=Len(selected)
          /\ view=Expected(visible)
          /\ Cardinality(Range(selected))=Len(selected)
          /\ \A k \in 1..Len(selected): grant[selected[k]]=k
          /\ \A n \in Nodes: backing[n]<=Len(selected)
          /\ \A i \in replied: i \in Range(selected) /\ grant[i]<=visible /\ ResultCorrect(i)
          /\ \A i \in Granted: base[i]=grant[i]-1
          /\ \A i,j \in Granted: i=j \/ grant[i]#grant[j]
(* Reachability checks are expected WitnessAbsent failures, never safety proofs. *)
WitnessReached ==
    CASE Witness="served" -> visible=MaxOps /\ readBeforeApply /\ (\A d \in Domains: readCut[d]=MaxOps) /\ Cardinality(replied)=MaxOps /\ \A i \in Ops: choice[i]="commit"
      [] Witness="append-order" -> Scenario="append" /\ visible=2 /\ selected= <<2,1>> /\ view.bytes= <<"b","b","a","a">> /\ status[2]=0 /\ status[1]=2 /\ retrySeen
      [] Witness="truncate" -> Scenario="truncate" /\ visible=3 /\ view.bytes= <<"a","X","0","0">> /\ readBeforeApply
      [] Witness="permission" -> Scenario="chmod" /\ selected= <<1,2>> /\ visible=2 /\ status[2]="EACCES" /\ choice[1]="commit" /\ choice[2]="commit"
      [] Witness="decline" -> visible=MaxOps /\ choice[1]="decline" /\ status[1]="NO_EFFECT" /\ choice[2]="commit"
      [] Witness="staged-hidden" -> visible=1 /\ Current#{} /\ readBeforeApply
      [] Witness="late-ack" -> lateAck /\ OwnerOrigin /\ 1 \in Range(selected) /\ choice[1]="commit" /\ visible>=grant[1] /\ ~up["B"]
      [] Witness="loss-gap" -> ramLost /\ diskLost /\ \E i \in Range(selected): choice[i]="commit" /\ ~HasSource(i,2) /\ ram[i]=0
      [] Witness="unknown" -> Len(selected)>0 /\ replied={} /\ retrySeen /\ conflictSeen
      [] Witness="restart" -> inc["X"]=1 /\ "X" \in eligible /\ visible=MaxOps
      [] Witness="blocked-survey" -> transition /\ excluded=Nodes /\ ~CompleteSurvey /\ "X" \notin reports /\ ~frontier /\ \E i \in Granted: Origin(i)="X" /\ choice[i]="commit"
      [] Witness="recovery" -> activated /\ Transition="crash" /\ ramLost /\ inc["A"]=1 /\ readOnNewOwner /\ visible=MaxOps /\ lossCut=SeedVisible /\ lossSelected=SeedSelected /\ \A i \in Ops: choice[i]="commit" /\ disk["B"][i]=3 /\ disk["C"][i]=3
      [] Witness="closed-absence" -> activated /\ ramLost /\ inc["A"]=1 /\ readOnNewOwner /\ lossCut=1 /\ choice[2]="decline" /\ disk["X"][2]=1
      [] Witness="closed-decline" -> activated /\ ramLost /\ inc["A"]=1 /\ readOnNewOwner /\ lossCut=1 /\ choice[2]="decline" /\ disk["X"][2]=2
      [] Witness="handoff" -> activated /\ Transition="handoff" /\ visible=MaxOps /\ readBeforeApply /\ readOnNewOwner /\ \A i \in Ops: choice[i]="commit" /\ disk["C"][i]=0
      [] Witness="equivalent" -> activated /\ diskLost /\ "X" \notin reports /\ frontier /\ \E i \in Granted: Origin(i)="X" /\ choice[i]="commit"
      [] OTHER -> FALSE
WitnessAbsent == ~WitnessReached
=====================================================================
