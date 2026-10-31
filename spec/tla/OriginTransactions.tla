------------------------- MODULE OriginTransactions -------------------------
EXTENDS Naturals, FiniteSets, Sequences, TLC
CONSTANTS Scenario, OwnerOrigin, Fault, MissingSource, Bug, Witness
VARIABLE s
vars == <<s>>
Ops == {"T", "U"}
Volumes == {1, 2}
Origins == {"A", "B", "C", "W"}
Placement == {"A", "B", "C"}
Domains == Origins \cup {"cache"}
Readers == {"r0", "r1"}
Labels == {"claim", "bind", "begin", "p1", "p2", "decision", "i1", "i2",
           "view", "complete", "effect", "result"}
Origin(op) == IF op = "T" THEN IF OwnerOrigin THEN "A" ELSE "W" ELSE "B"
Memory(op) == IF Bug = "self" THEN Origin(op)
              ELSE IF s.epoch = 2 THEN "C"
              ELSE IF Origin(op) = "A" THEN "B" ELSE "A"
Key(op, label) == <<op, label>>
Has(op, label) == Key(op, label) \in s.protected
BothPrepares == Has("T", "p1") /\ Has("T", "p2")
BothInstalls == Has("T", "i1") /\ Has("T", "i2")
TxSelected == Has("T", "decision") /\ s.choice = "commit"
InitialView == [entries |-> {"src"}, data |-> 0, permission |-> TRUE, links |-> 1]
Init == s = [active |-> "", done |-> {}, order |-> <<>>, resolved |-> FALSE,
  allowed |-> FALSE, current |-> "", phase |-> 0,
  durable |-> {}, retained |-> {}, emitted |-> {}, protected |-> {},
  choice |-> "none", cancelled |-> FALSE, published |-> {}, replied |-> {},
  sent |-> {}, lostReplies |-> {},
  view |-> InitialView, backing |-> InitialView, effects |-> [op \in Ops |-> 0],
  readers |-> {}, originBinding |-> [op \in Ops |-> Origin(op)],
  claims |-> {}, requestDigest |-> [op \in Ops |-> 1],
  planView |-> [op \in Ops |-> InitialView],
  observed |-> [cut |-> {}, value |-> InitialView],
  claimsChecked |-> {}, fixedResult |-> [op \in Ops |-> "unknown"],
  retry |-> "none", retryFrom |-> "", pathResolutions |-> [op \in Ops |-> 0],
  memAlive |-> TRUE, seqAlive |-> TRUE, lost |-> FALSE, knownLoss |-> FALSE,
  late |-> FALSE, lostUnknownCommit |-> FALSE,
  transition |-> FALSE, reports |-> {}, excluded |-> {},
  pair |-> {}, recovered |-> FALSE, transferred |-> FALSE, epoch |-> 1,
  sourcePins |-> Origins, home |-> 1, homeAlive |-> TRUE, handle |-> TRUE,
  affinity |-> Volumes]

(* One reservation encloses all semantic 2PC phases. No other origin gets a
   mutating grant until this reservation has its protected result/publication. *)
Reserve(op) == /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ s.active = "" /\ op \notin s.done
 /\ s' = [s EXCEPT !.active = op, !.order = Append(@, op),
                   !.resolved = FALSE, !.allowed = FALSE, !.readers = {}]
Resolve == /\ s.active # "" /\ Has(s.active, "claim") /\ ~s.resolved
 /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ LET ok == IF s.active = "T" THEN s.view.permission /\ "src" \in s.view.entries
               ELSE IF Scenario = "write" THEN "src" \in s.view.entries
               ELSE TRUE
    IN s' = [s EXCEPT !.resolved = TRUE, !.allowed = ok,
       !.pathResolutions[s.active] = @ + 1,
       !.claimsChecked = @ \cup {s.active}, !.planView[s.active] = s.view,
       !.fixedResult[s.active] = IF ~ok THEN "denied"
          ELSE IF s.active = "T" /\ s.cancelled THEN "aborted" ELSE "success"]

(* These are SEMANTIC records in one acyclic graph. phase below describes the
   ENCLOSING origin-durable/retained-memory exchange, not a second 2PC layer. *)
Enabled(label) ==
 /\ s.active # "" /\ ~Has(s.active, label)
 /\ CASE label = "claim" -> TRUE
      [] ~s.resolved -> FALSE
      [] label = "result" -> IF ~s.allowed THEN TRUE
           ELSE IF s.active = "T" THEN "T" \in s.published
           ELSE Has("U", "effect")
      [] ~s.allowed -> FALSE
      [] s.active = "U" -> label = "effect"
      [] label = "bind" -> TRUE
      [] label = "begin" -> Has("T", "bind")
      [] label \in {"p1", "p2"} -> Has("T", "begin") /\ ~Has("T", "decision")
      [] label = "decision" -> Has("T", "begin") /\
           (BothPrepares \/ s.cancelled \/ Bug = "prepare")
      [] label \in {"i1", "i2"} -> Has("T", "decision")
      [] label = "view" -> BothInstalls /\ s.readers = Readers
      [] label = "complete" -> Has("T", "view")
      [] OTHER -> FALSE
Stage(label) == /\ s.phase = 0 /\ Enabled(label)
 /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ s' = [s EXCEPT !.current = label, !.phase = 1,
       !.choice = IF s.active = "T" /\ label = "decision"
                    THEN IF s.choice # "none" THEN s.choice
                         ELSE IF s.cancelled THEN "abort" ELSE "commit"
                    ELSE @,
       !.claims = IF label = "claim" THEN @ \cup {s.active} ELSE @]
EmitPrepared == /\ s.phase = 1 /\ s.memAlive /\ ~s.knownLoss /\ ~s.transition
 /\ s' = [s EXCEPT !.phase = 2,
       !.retained = @ \cup {Key(s.active, s.current)}]
DeliverPrepared == /\ s.phase = 2 /\ ~s.knownLoss
 /\ s' = [s EXCEPT !.phase = 3]
(* After a prepared receipt has arrived, an origin can still sync its issued
   old grant before its own close/report, even if sequencer loss is unobserved. *)
OriginSync == /\ s.phase = 3
 /\ (s.epoch = 2 \/ Origin(s.active) \notin s.reports)
 /\ s' = [s EXCEPT !.phase = 4,
       !.durable = @ \cup {Key(s.active, s.current)}]
EmitAcceptance == /\ (s.phase = 4 \/ (Bug = "sync" /\ s.phase = 3))
 /\ s.memAlive /\ ~s.knownLoss /\ ~s.transition
 /\ s' = [s EXCEPT !.phase = 5,
       !.emitted = @ \cup {Key(s.active, s.current)}]
Protect == /\ s.phase = 5 /\ s.seqAlive /\ ~s.knownLoss /\ ~s.transition
 /\ s' = [s EXCEPT !.phase = 0, !.current = "",
       !.protected = @ \cup {Key(s.active, s.current)},
       !.late = @ \/ ~s.memAlive]
Cancel == /\ s.active = "T" /\ ~s.cancelled /\ s.choice = "none"
 /\ (~s.resolved \/ s.allowed)
 /\ s' = [s EXCEPT !.cancelled = TRUE, !.fixedResult["T"] = "aborted"]

ReadyBarrier == s.active # "" /\ s.allowed /\
  (IF s.active = "T" THEN BothInstalls ELSE Has("U", "effect"))
Barrier(reader) == /\ ReadyBarrier /\ reader \notin s.readers
 /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ s' = [s EXCEPT !.readers = @ \cup {reader}]
PublishReady == IF s.active = "T" THEN Has("T", "complete")
                ELSE Has("U", "result") /\ s.readers = Readers
AfterEffect(op) ==
 IF op = "T" THEN
   IF s.choice = "abort" THEN s.planView[op]
   ELSE [s.planView[op] EXCEPT !.entries = IF Scenario = "write"
                                  THEN @ \cup {"dst"} ELSE {"dst"},
                       !.links = IF Scenario = "write" THEN 2 ELSE 1]
 ELSE CASE Scenario = "chmod" -> [s.planView[op] EXCEPT !.permission = FALSE]
           [] Scenario = "write" -> [s.planView[op] EXCEPT !.data = 1]
           [] OTHER -> s.planView[op]
Publish == /\ s.active # "" /\ s.allowed /\ s.active \notin s.published
 /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ (PublishReady \/ Bug = "visibility")
 /\ s' = [s EXCEPT !.published = @ \cup {s.active},
       !.view = AfterEffect(s.active),
       !.effects[s.active] = IF s.active = "T" /\ s.choice = "abort" THEN @ ELSE @ + 1]
Finish == /\ s.active # "" /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ (~s.allowed \/ s.active \in s.published)
 /\ (Has(s.active, "result") \/ Bug = "result")
 /\ s' = [s EXCEPT !.done = @ \cup {s.active}, !.active = ""]
EmitReply(op) == /\ op \in s.done \ s.sent /\ op \notin s.replied
 /\ ~s.knownLoss
 /\ s' = [s EXCEPT !.sent = @ \cup {op}]
DropReply(op) == /\ op \in s.sent \ s.replied /\ op \notin s.lostReplies
 /\ s' = [s EXCEPT !.sent = @ \ {op}, !.lostReplies = @ \cup {op}]
DeliverReply(op) == /\ op \in s.sent \ s.replied
 /\ s' = [s EXCEPT !.replied = @ \cup {op}]
ApplyBacking == /\ s.seqAlive /\ ~s.transition /\ s.backing # s.view
 /\ s' = [s EXCEPT !.backing = s.view]
(* Retrying through a different origin consults the claim/result before paths.
   No retry changes the original origin, binding, plan, effect count or order. *)
Retry(digest) == /\ "T" \in s.claims
 /\ (s.seqAlive \/ s.knownLoss \/ s.transition)
 /\ s' = [s EXCEPT !.retryFrom = "B",
       !.retry = IF digest # s.requestDigest["T"] THEN "conflict"
                 ELSE IF s.knownLoss \/ s.transition THEN "indeterminate"
                 ELSE IF "T" \in s.done THEN "result" ELSE "pending"]

(* An external logical oracle observes the committed cut, including before
   backing apply. Cache barriers here are abstract positive facts, not kernels. *)
Oracle(cut) == [entries |-> IF "T" \in cut /\ s.choice = "commit"
                    THEN IF Scenario = "write" THEN {"src", "dst"} ELSE {"dst"}
                    ELSE {"src"},
 data |-> IF "U" \in cut /\ Scenario = "write" THEN 1 ELSE 0,
 permission |-> ~("U" \in cut /\ Scenario = "chmod"),
 links |-> IF "T" \in cut /\ s.choice = "commit" /\ Scenario = "write" THEN 2 ELSE 1]
Observe == /\ s.seqAlive /\ ~s.transition /\ ~s.knownLoss
 /\ s' = [s EXCEPT !.observed = [cut |-> s.published, value |-> s.view]]

(* One real incoming pipeline-to-recovery transition is included. Complete
   source reports/bytes and positive domain closure are abstract atomic facts;
   source loss, equivalent reports, torn files and repeated transitions are out. *)
LoseMemory == /\ Fault = "crash" /\ ~s.lost /\ s.phase \in {2, 3, 4, 5}
 /\ s' = [s EXCEPT !.lost = TRUE, !.memAlive = FALSE, !.retained = {},
       !.lostUnknownCommit = s.active = "T" /\ s.current = "decision"
          /\ s.choice = "commit" /\ Key("T", "decision") \in s.durable
          /\ ~Has("T", "decision"),
       !.seqAlive = IF Memory(s.active) = "A" THEN FALSE ELSE @]
BeginTransition == /\ ~s.transition /\ ~s.recovered
 /\ ((Fault = "crash" /\ s.lost) \/ (Fault = "handoff" /\ s.done = Ops))
 /\ s' = [s EXCEPT !.transition = TRUE, !.seqAlive = FALSE,
                   !.knownLoss = TRUE, !.readers = {}]
Report(node) == /\ s.transition /\ node \notin s.reports
 /\ (~MissingSource \/ node # "W")
 /\ s' = [s EXCEPT !.reports = @ \cup {node}]
Exclude(domain) == /\ s.transition /\ domain \notin s.excluded
 /\ s' = [s EXCEPT !.excluded = @ \cup {domain}]
SurveyReady == s.reports = Origins \/
              (Bug = "survey" /\ s.reports = Placement)
Materialize(node) == /\ s.transition /\ Fault = "crash" /\ SurveyReady
 /\ node \in {"B", "C"} \ s.pair
 /\ s' = [s EXCEPT !.pair = @ \cup {node}]
Transfer == /\ s.transition /\ Fault = "handoff" /\ SurveyReady /\ ~s.transferred
 /\ s' = [s EXCEPT !.transferred = TRUE]
Activate == /\ s.transition /\ SurveyReady
 /\ (s.excluded = Domains \/ Bug = "fence")
 /\ (IF Fault = "crash" THEN s.pair = {"B", "C"} ELSE s.transferred)
 /\ s' = [s EXCEPT !.transition = FALSE, !.seqAlive = TRUE, !.memAlive = TRUE,
       !.knownLoss = FALSE, !.epoch = 2, !.recovered = TRUE,
       !.protected = s.durable, !.retained = s.durable,
       !.phase = 0, !.current = "", !.readers = {}]

Next == (\E op \in Ops : Reserve(op)) \/ Resolve
 \/ (\E label \in Labels : Stage(label))
 \/ EmitPrepared \/ DeliverPrepared \/ OriginSync \/ EmitAcceptance \/ Protect
 \/ Cancel \/ (\E reader \in Readers : Barrier(reader)) \/ Publish \/ Finish
 \/ (\E op \in Ops : EmitReply(op) \/ DropReply(op) \/ DeliverReply(op))
 \/ ApplyBacking \/ (\E digest \in {1, 2} : Retry(digest)) \/ Observe \/ LoseMemory \/ BeginTransition
 \/ (\E node \in Origins : Report(node))
 \/ (\E domain \in Domains : Exclude(domain))
 \/ (\E node \in {"B", "C"} : Materialize(node)) \/ Transfer \/ Activate

Safety ==
 /\ s.active \in Ops \cup {""} /\ s.phase \in 0..5
 /\ s.durable \subseteq Ops \X Labels /\ s.protected \subseteq s.durable
 /\ s.done \subseteq Ops /\ s.replied \subseteq s.done /\ s.sent \subseteq s.done
 /\ Len(s.order) <= 2 /\ Cardinality({s.order[i] : i \in 1..Len(s.order)}) = Len(s.order)
 /\ s.originBinding = [op \in Ops |-> Origin(op)]
 /\ s.requestDigest = [op \in Ops |-> 1]
 /\ s.view = Oracle(s.published) /\ s.view.links = Cardinality(s.view.entries)
 /\ s.observed.value = Oracle(s.observed.cut)
 /\ \A op \in Ops : s.pathResolutions[op] = 1 => op \in s.claimsChecked /\ Has(op, "claim")
 /\ \A op \in Ops : s.effects[op] <= 1 /\ s.pathResolutions[op] <= 1
 /\ \A key \in s.protected : key \in s.emitted \/ s.recovered
 /\ \A op \in Ops : Has(op, "effect") \/ Has(op, "decision") => Memory(op) # Origin(op)
 /\ TxSelected => BothPrepares /\ Has("T", "bind") /\ Has("T", "begin")
 /\ BothInstalls => Has("T", "decision")
 /\ "T" \in s.published => BothInstalls /\ Has("T", "view") /\ Has("T", "complete")
 /\ \A op \in s.replied : Has(op, "result")
 /\ s.view.entries \in {{"src"}, {"dst"}, {"src", "dst"}}
 /\ "dst" \in s.view.entries => "T" \in s.published /\ s.choice = "commit"
 /\ s.effects["T"] = 1 => s.fixedResult["T"] = "success"
 /\ s.fixedResult["T"] = "denied" => s.effects["T"] = 0
 /\ (Scenario = "chmod" /\ s.order = <<"U", "T">> /\ "T" \in s.done)
       => s.fixedResult["T"] = "denied" /\ s.view.entries = {"src"}
 /\ (Scenario = "write" /\ s.done = Ops /\ s.choice = "commit")
       => s.view.entries = {"src", "dst"} /\ s.view.data = 1
 /\ s.recovered => s.reports = Origins /\ s.excluded = Domains
       /\ (IF Fault = "crash" THEN s.pair = {"B", "C"} ELSE s.transferred)
 /\ s.sourcePins = Origins /\ s.affinity = Volumes /\ s.home = 1
 /\ s.homeAlive /\ s.handle

WitnessState == CASE
 Witness = "unknown" -> s.choice = "commit"
       /\ Key("T", "decision") \in s.durable /\ ~Has("T", "decision")
 [] Witness = "lostreply" -> "T" \in s.lostReplies /\ "T" \notin s.replied
       /\ s.retry = "result" /\ s.effects["T"] = 1
 [] Witness = "partial" -> Has("T", "i1") /\ ~Has("T", "i2") /\ "T" \notin s.published
 [] Witness = "retry" -> s.retry = "result" /\ s.effects["T"] = 1
       /\ s.retryFrom = "B" /\ s.pathResolutions["T"] = 1
 [] Witness = "late" -> s.late /\ "T" \in s.replied /\ ~s.memAlive
 [] Witness = "blocked" -> s.transition /\ s.reports = Placement /\ s.excluded = Domains
       /\ ~s.recovered /\ s.lostUnknownCommit
       /\ Key("T", "decision") \in s.durable
 [] Witness = "recovery" -> s.lostUnknownCommit /\ s.recovered
       /\ "T" \in s.replied /\ s.effects["T"] = 1
 [] Witness = "handoff" -> s.recovered /\ s.transferred /\ s.backing = InitialView
       /\ s.view # InitialView /\ s.done = Ops /\ s.effects["T"] = 1
 [] Witness = "permission" -> s.order = <<"U", "T">> /\ s.done = Ops
       /\ s.fixedResult["T"] = "denied"
 [] Witness = "alias" -> s.done = Ops /\ s.view.entries = {"src", "dst"}
       /\ s.view.data = 1 /\ s.homeAlive /\ s.home = 1
 [] OTHER -> s.done = Ops /\ s.order = <<"T", "U">> /\ s.effects["T"] = 1
WitnessAbsent == ~WitnessState
Spec == Init /\ [][Next]_vars
FairSpec == Spec /\ (\A op \in Ops : WF_vars(Reserve(op))) /\ WF_vars(Resolve)
 /\ WF_vars(\E label \in Labels : Stage(label)) /\ WF_vars(EmitPrepared)
 /\ WF_vars(DeliverPrepared) /\ WF_vars(OriginSync) /\ WF_vars(EmitAcceptance)
 /\ WF_vars(Protect) /\ (\A reader \in Readers : WF_vars(Barrier(reader)))
 /\ WF_vars(Publish) /\ WF_vars(Finish)
Completion == <> (s.done = Ops)
=============================================================================
