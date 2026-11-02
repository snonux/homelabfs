--------------------------- MODULE PreferredOwner ---------------------------
EXTENDS Naturals, Integers, FiniteSets, Sequences, TLC

CONSTANTS Mode, Scenario, Mutation, Witness
Nodes == {"A", "B", "C", "W"}
Placement == {"A", "B", "C"}
Volumes == {"v1", "v2"}
Domains == {"native", "guest"}
Ops == 1..5
Obligations == {"frontier", "claims", "results", "sources", "homes",
                "handles", "pins", "transaction", "view"}
Origin(i) == CASE i = 1 -> "A" [] i = 2 -> "W" [] i = 3 -> "C"
                  [] i = 4 -> "A" [] OTHER -> "W"
Memory(o, a) == IF o = a THEN "B" ELSE a
ChargeVolumes(i) == IF i = 2 THEN Volumes ELSE {IF i = 1 THEN "v1" ELSE "v2"}
Kinds == {"original", "forward", "retry", "replication"}

VARIABLES m, p, h
vars == <<m, p, h>>

Init ==
 /\ m = [seen |-> {}, events |-> {},
          perVolume |-> [v \in Volumes |-> [n \in Nodes |-> 0]],
          total |-> [n \in Nodes |-> 0]]
 /\ p = [tick |-> 0, evaluated |-> FALSE, stamp |-> [n \in Nodes |-> -1],
          counts |-> [n \in Nodes |-> 0], owner |-> "A", winner |-> "A",
          streak |-> 0, age |-> IF Scenario = "young" THEN 0 ELSE 15,
          cooldown |-> IF Scenario = "cooldown" THEN 10 ELSE 0, moves |-> 0,
          bad |-> FALSE, replayed |-> FALSE, rejectedReplay |-> FALSE,
          staleRound |-> FALSE, oscillation |-> FALSE, freshRounds |-> {}]
 /\ h = [owner |-> "A", term |-> 1, phase |-> "serving", gate |-> FALSE,
          step |-> [i \in Ops |-> 0], bound |-> [i \in Ops |-> 0],
          issued |-> 0, durable |-> {}, selected |-> {}, visible |-> {},
          ram |-> [n \in Nodes |-> {}], disk |-> [n \in Nodes |-> {}],
          mem |-> [i \in Ops |-> "none"], result |-> {}, effects |-> {},
          reports |-> {}, drained |-> FALSE, closed |-> {}, installed |-> {},
          newView |-> {}, transferred |-> FALSE, transferredIssued |-> 0,
          ledger |-> {}, continuous |-> TRUE, interrupted |-> FALSE,
          origins |-> Nodes, volumeOwner |-> [v \in Volumes |-> "A"],
          unknownMoved |-> FALSE, retried |-> FALSE, served |-> {},
          beforeDisk |-> FALSE, ownerBefore |-> FALSE, ownerAfter |-> FALSE]

Ingress(i, kind) ==
 /\ Mode = "counting" /\ i \in 1..3 /\ kind \in Kinds
 /\ <<i, kind>> \notin m.events
 /\ kind \in {"original", "replication"} \/ i \in m.seen
 /\ LET fresh == kind # "replication" /\ i \notin m.seen
        duplicate == Mutation = "count" /\ kind \in {"forward", "retry"}
        charge == fresh \/ duplicate
    IN m' = [m EXCEPT
       !.events = @ \cup {<<i, kind>>},
       !.seen = IF fresh THEN @ \cup {i} ELSE @,
       !.total = IF charge THEN [@ EXCEPT ![Origin(i)] = @ + 1] ELSE @,
       !.perVolume = IF charge THEN [v \in Volumes |->
          IF v \in ChargeVolumes(i)
          THEN [m.perVolume[v] EXCEPT ![Origin(i)] = @ + 1]
          ELSE m.perVolume[v]] ELSE @]
 /\ UNCHANGED <<p, h>>

Score(t, n) ==
 CASE Scenario = "tie" -> IF n \in {"A", "C"} THEN 128 ELSE 0
 [] Scenario = "outside" -> CASE n = "W" -> 512 [] n = "C" -> 256
                             [] n = "A" -> 64 [] OTHER -> 0
 [] Scenario = "oscillate" ->
      IF n = (IF t % 2 = 0 THEN "C" ELSE "B") THEN 256
      ELSE IF n = "A" THEN 64 ELSE 0
 [] OTHER -> CASE n = "C" -> 256 [] n = "A" -> 64 [] OTHER -> 0
Eligible == IF Scenario = "capacity" THEN {"A", "B"} ELSE Placement
Pool == IF Mutation = "eligibility" THEN Nodes ELSE Eligible
Best == LET winners == {n \in Pool : \A q \in Pool : p.counts[n] >= p.counts[q]}
        IN IF Cardinality(winners) # 1 THEN p.owner
           ELSE CHOOSE n \in winners : TRUE
Fresh == \A n \in Nodes : p.stamp[n] = p.tick
Samples == p.counts["A"] + p.counts["B"] + p.counts["C"] + p.counts["W"]
Advantage(n) == /\ p.counts[n] >= p.counts[p.owner] + 32
                /\ 4 * p.counts[n] >= 5 * p.counts[p.owner]
Report(n) ==
 /\ Mode = "policy" /\ ~p.evaluated /\ p.stamp[n] < p.tick
 /\ p' = [p EXCEPT !.stamp[n] = p.tick, !.counts[n] = Score(p.tick, n)]
 /\ UNCHANGED <<m, h>>
Replay ==
 /\ Mode = "policy" /\ ~p.replayed /\ p.tick > 0
 /\ \E n \in Nodes : p.stamp[n] = p.tick /\
     p' = [p EXCEPT !.replayed = TRUE,
          !.rejectedReplay = Mutation # "replay",
          !.stamp[n] = IF Mutation = "replay" THEN p.tick - 1 ELSE @,
          !.counts[n] = IF Mutation = "replay" THEN Score(p.tick - 1, n) ELSE @,
          !.bad = @ \/ Mutation = "replay"]
 /\ UNCHANGED <<m, h>>
Evaluate ==
 /\ Mode = "policy" /\ ~p.evaluated
 /\ LET candidate == Best
        valid == Fresh /\ Samples >= 128 /\ candidate # p.owner /\ Advantage(candidate)
        considered == (Fresh \/ Mutation = "freshness") /\ Samples >= 128
                       /\ candidate # p.owner /\ Advantage(candidate)
        streak == IF considered THEN IF candidate = p.winner THEN p.streak + 1 ELSE 1 ELSE 0
        stable == streak >= 3 /\ p.age >= 15 /\ p.cooldown = 0
        permitted == streak >= 3 /\ (p.age >= 15 \/ Mutation = "tenure")
                     /\ (p.cooldown = 0 \/ Mutation = "cooldown")
        move == considered /\ (permitted \/ Mutation = "stability")
    IN p' = [p EXCEPT !.evaluated = TRUE, !.winner = candidate,
        !.streak = IF move THEN 0 ELSE streak,
        !.owner = IF move THEN candidate ELSE @,
        !.age = IF move THEN 0 ELSE @,
        !.cooldown = IF move THEN 10 ELSE @,
        !.moves = IF move THEN @ + 1 ELSE @,
        !.bad = @ \/ (move /\ (~valid \/ ~stable \/ candidate \notin Eligible)),
        !.staleRound = @ \/ ~Fresh,
        !.freshRounds = IF Witness = "oscillate" /\ Fresh THEN @ \cup {p.tick} ELSE @,
        !.oscillation = @ \/ (p.streak > 0 /\ candidate # p.winner)]
 /\ UNCHANGED <<m, h>>
Tick ==
 /\ Mode = "policy" /\ p.evaluated /\ p.tick < 5
 /\ p' = [p EXCEPT !.tick = @ + 1, !.evaluated = FALSE,
        !.age = @ + 1, !.cooldown = IF @ > 0 THEN @ - 1 ELSE 0]
 /\ UNCHANGED <<m, h>>

Grant(i) ==
 /\ Mode = "handoff" /\ h.phase = "serving" /\ ~h.gate
 /\ h.step[i] = 0 /\ i = h.issued + 1
 /\ IF i <= 2 THEN h.term = 1 ELSE h.term = 2
 /\ IF i = 1 THEN TRUE ELSE h.step[i-1] >= 6
 /\ h' = [h EXCEPT !.step[i] = 1, !.bound[i] = h.term, !.issued = i]
 /\ UNCHANGED <<m, p>>
Stage(i) ==
 /\ Mode = "handoff" /\ h.step[i] = 1
 /\ (h.bound[i] = 2 \/ Origin(i) \notin h.reports) /\ (h.phase = "serving" \/ h.phase = "closing")
 /\ h' = [h EXCEPT !.step[i] = 2]
 /\ UNCHANGED <<m, p>>
Sync(i) ==
 /\ Mode = "handoff" /\ h.step[i] = 2 /\ (h.bound[i] = 2 \/ Origin(i) \notin h.reports)
 /\ (h.phase = "serving" \/ h.phase = "closing")
 /\ h' = [h EXCEPT !.step[i] = 3, !.durable = @ \cup {i}]
 /\ UNCHANGED <<m, p>>
Select(i) ==
 /\ Mode = "handoff" /\ h.step[i] = 3
 /\ (h.phase = "serving" /\ ~h.gate) \/ (h.phase = "installed" /\ i = 2)
 /\ LET receiver == IF Mutation = "self" /\ h.term = 2 /\ Origin(i) = h.owner
                     THEN h.owner ELSE Memory(Origin(i), h.owner)
    IN h' = [h EXCEPT !.step[i] = 4, !.selected = @ \cup {i},
         !.ram[h.owner] = @ \cup {i}, !.mem[i] = receiver,
         !.ownerBefore = @ \/ (i = 1 /\ receiver = "B"),
         !.ownerAfter = @ \/ (i = 3 /\ receiver = "B")]
 /\ UNCHANGED <<m, p>>
ProtectResult(i) ==
 /\ Mode = "handoff" /\ h.step[i] = 4
 /\ (h.phase = "serving" /\ ~h.gate) \/ (h.phase = "installed" /\ i = 2)
 /\ h' = [h EXCEPT !.step[i] = 5, !.result = @ \cup {i}]
 /\ UNCHANGED <<m, p>>
Publish(i) ==
 /\ Mode = "handoff" /\ h.step[i] = 5
 /\ (h.phase = "serving" /\ ~h.gate) \/
       (h.phase = "installed" /\ i = 2 /\ h.newView = Domains)
 /\ IF i = 1 THEN TRUE ELSE h.step[i-1] >= 6
 /\ h' = [h EXCEPT !.step[i] = 6, !.visible = @ \cup {i}, !.effects = @ \cup {i}]
 /\ UNCHANGED <<m, p>>
Reply(i) ==
 /\ Mode = "handoff" /\ h.step[i] = 6 /\ h.phase = "serving" /\ ~h.gate
 /\ h' = [h EXCEPT !.step[i] = 7]
 /\ UNCHANGED <<m, p>>
Begin ==
 /\ Mode = "handoff" /\ h.phase = "serving" /\ h.term = 1
 /\ h.step[1] >= 6 /\ h.step[2] \in 1..3
 /\ h' = [h EXCEPT !.phase = "closing", !.gate = TRUE]
 /\ UNCHANGED <<m, p>>
Drain ==
 /\ Mode = "handoff" /\ h.phase = "closing" /\ ~h.drained
 /\ h' = [h EXCEPT !.drained = TRUE]
 /\ UNCHANGED <<m, p>>
CloseOrigin(n) ==
 /\ Mode = "handoff" /\ h.phase = "closing" /\ h.drained /\ n \notin h.reports
 /\ h' = [h EXCEPT !.reports = @ \cup {n}]
 /\ UNCHANGED <<m, p>>
CloseCache(d) ==
 /\ Mode = "handoff" /\ h.phase = "closing" /\ h.drained /\ d \notin h.closed
 /\ h' = [h EXCEPT !.closed = @ \cup {d}]
 /\ UNCHANGED <<m, p>>
Transfer ==
 /\ Mode = "handoff" /\ h.phase = "closing" /\ ~h.transferred /\ h.continuous
 /\ h.reports = Nodes \/ (Mutation = "survey" /\ h.reports = Placement)
 /\ h.closed = Domains \/ Mutation = "fence"
 /\ h' = [h EXCEPT !.transferred = TRUE,
      !.ram["C"] = IF Mutation = "projection" THEN h.selected \ {1} ELSE h.selected,
      !.transferredIssued = IF Mutation = "frontier" THEN h.issued - 1 ELSE h.issued,
      !.ledger = IF Mutation = "result" THEN Obligations \ {"results"} ELSE Obligations,
      !.unknownMoved = h.step[2] = 3,
      !.step[2] = IF Mutation = "unknown" /\ h.step[2] = 3 THEN 1 ELSE @]
 /\ UNCHANGED <<m, p>>
Interrupt ==
 /\ Mode = "handoff" /\ Scenario = "interrupt" /\ h.phase = "closing"
 /\ h.transferred /\ ~h.interrupted
 /\ h' = [h EXCEPT !.continuous = FALSE, !.interrupted = TRUE]
 /\ UNCHANGED <<m, p>>
Install(n) ==
 /\ Mode = "handoff" /\ h.phase = "closing" /\ h.transferred
 /\ h.continuous /\ n \notin h.installed
 /\ h' = [h EXCEPT !.installed = @ \cup {n}]
 /\ UNCHANGED <<m, p>>
Activate ==
 /\ Mode = "handoff" /\ h.phase = "closing" /\ h.transferred
 /\ h.installed = Nodes
 /\ h.continuous \/ Mutation = "continuity"
 /\ h' = [h EXCEPT !.owner = "C", !.term = 2, !.phase = "installed",
      !.volumeOwner = [v \in Volumes |-> IF Mutation = "closure" /\ v = "v2" THEN "A" ELSE "C"]]
 /\ UNCHANGED <<m, p>>
ResolveAbsent ==
 /\ Mode = "handoff" /\ h.phase = "installed" /\ h.step[2] \in 1..2
 /\ h' = [h EXCEPT !.step[2] = 7, !.result = @ \cup {2}]
 /\ UNCHANGED <<m, p>>
NewView(d) ==
 /\ Mode = "handoff" /\ h.phase = "installed" /\ h.step[2] >= 5
 /\ d \notin h.newView
 /\ h' = [h EXCEPT !.newView = @ \cup {d}]
 /\ UNCHANGED <<m, p>>
Reopen ==
 /\ Mode = "handoff" /\ h.phase = "installed" /\ h.step[2] >= 6
 /\ h.newView = Domains
 /\ h' = [h EXCEPT !.phase = "serving", !.gate = FALSE]
 /\ UNCHANGED <<m, p>>
Apply(i) ==
 /\ Mode = "handoff" /\ i \in h.visible /\ i \notin h.disk[h.owner]
 /\ h' = [h EXCEPT !.disk[h.owner] = @ \cup {i}]
 /\ UNCHANGED <<m, p>>
RetryUnknown ==
 /\ Mode = "handoff" /\ h.term = 2 /\ h.unknownMoved /\ ~h.retried
 /\ h' = [h EXCEPT !.retried = TRUE]
 /\ UNCHANGED <<m, p>>
Read(n) ==
 /\ Mode = "handoff" /\ h.phase = "serving" /\ ~h.gate /\ h.term = 2
 /\ h.step[5] >= 6 /\ n \in Nodes /\ n \notin h.served
 /\ h' = [h EXCEPT !.served = @ \cup {n},
      !.beforeDisk = @ \/ (h.disk["C"] = {} /\ h.visible \subseteq h.ram["C"])]
 /\ UNCHANGED <<m, p>>

Next == (\E i \in 1..3, k \in Kinds : Ingress(i,k))
 \/ (\E n \in Nodes : Report(n)) \/ Replay \/ Evaluate \/ Tick
 \/ (\E i \in Ops : Grant(i) \/ Stage(i) \/ Sync(i) \/ Select(i)
                   \/ ProtectResult(i) \/ Publish(i) \/ Reply(i) \/ Apply(i))
 \/ Begin \/ Drain \/ (\E n \in Nodes : CloseOrigin(n) \/ Install(n) \/ Read(n))
 \/ (\E d \in Domains : CloseCache(d) \/ NewView(d))
 \/ Transfer \/ Interrupt \/ Activate \/ ResolveAbsent \/ Reopen \/ RetryUnknown
Spec == Init /\ [][Next]_vars

CountSafety ==
 /\ \A n \in Nodes : m.total[n] = Cardinality({i \in m.seen : Origin(i) = n})
 /\ \A v \in Volumes, n \in Nodes : m.perVolume[v][n] =
         Cardinality({i \in m.seen : Origin(i) = n /\ v \in ChargeVolumes(i)})
PolicySafety == /\ ~p.bad /\ p.owner \in Eligible
                 /\ (Scenario \in {"tie", "oscillate", "capacity"} => p.moves = 0)
HandoffSafety ==
 /\ h.origins = Nodes
 /\ h.selected \subseteq h.durable
 /\ h.visible \subseteq h.selected
 /\ \A i \in h.selected : h.mem[i] # Origin(i)
 /\ \A i \in h.effects : i \in h.result
 /\ h.step[2] >= 6 /\ 2 \in h.durable => 2 \in h.selected
 /\ \A v \in Volumes : h.volumeOwner[v] = h.owner
 /\ h.transferred => /\ h.transferredIssued = 2 /\ h.transferredIssued <= h.issued
                         /\ h.ledger = Obligations
                         /\ 1 \in h.ram["C"]
 /\ h.term = 2 => /\ h.reports = Nodes /\ h.closed = Domains /\ h.continuous
                   /\ h.visible \subseteq h.ram["C"]
 /\ h.term = 2 /\ ~h.gate => h.installed = Nodes /\ h.newView = Domains
 /\ 3 \in h.selected => h.ownerBefore /\ h.ownerAfter /\ h.mem[3] = "B"
 /\ 4 \in h.selected => h.mem[4] = "C"
 /\ 5 \in h.selected => h.mem[5] = "C"
Safety == CASE Mode = "counting" -> CountSafety
            [] Mode = "policy" -> PolicySafety
            [] OTHER -> HandoffSafety
WitnessReached ==
 CASE Witness = "count" -> m.seen = 1..3 /\ Cardinality(m.events) = 12
 [] Witness = "move" -> p.moves = 1 /\ p.owner = "C" /\ p.rejectedReplay /\ p.staleRound
 [] Witness = "oscillate" -> p.tick = 5 /\ p.evaluated /\ p.oscillation /\ p.moves = 0
       /\ p.freshRounds = 0..5
 [] Witness = "outside" -> p.owner = "C" /\ p.counts["W"] > p.counts["C"]
 [] Witness = "interrupted" -> h.interrupted /\ h.term = 1 /\ h.transferred
 [] Witness = "service" -> h.ownerBefore /\ h.ownerAfter /\ h.beforeDisk
        /\ h.served = Nodes /\ h.disk["C"] = {} /\ h.unknownMoved /\ h.retried /\ h.step[5] >= 6
 [] OTHER -> FALSE
WitnessAbsent == ~WitnessReached
=============================================================================
