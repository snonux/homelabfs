---------------------------- MODULE Replication ----------------------------
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS MaxOps, MaxRestarts, MaxCrashes, Bug
ASSUME /\ MaxOps \in Nat \ {0}
       /\ MaxRestarts \in Nat /\ MaxCrashes \in Nat
       /\ Bug \in {"none", "gap-ack", "stale-session", "double-apply"}

Ops == 1..MaxOps
Frames == 1..(3 * MaxOps)
Nodes == {"A", "B", "C"}
Mutation(i) == 3 * i - 2
Commit(i) == 3 * i - 1
Result(i) == 3 * i
Prefix(s) == CHOOSE p \in 0..(3 * MaxOps):
    /\ (1..p) \subseteq s
    /\ (p = 3 * MaxOps \/ p + 1 \notin s)
NoControl == [kind |-> "none", pos |-> 0, incarnation |-> 0]

VARIABLES tail, durable, up, crashes, incarnation, ready,
          received, partnerDurable, accepted, durableAccepted,
          learned, receipt, dataB, dataC, control,
          decided, applied, effects, chosen, replied, replyWitness

vars == <<tail, durable, up, crashes, incarnation, ready,
          received, partnerDurable, accepted, durableAccepted,
          learned, receipt, dataB, dataC, control,
          decided, applied, effects, chosen, replied, replyWitness>>

R(n) == IF n = "A" THEN tail ELSE Prefix(received[n])
CurrentAcceptance(i) == i \in learned
    /\ durable >= Commit(i) /\ up /\ ready
ProtectedResult(i) == CurrentAcceptance(i)
    /\ durable >= Result(i) /\ receipt >= Result(i)

Init == /\ tail = 0 /\ durable = 0 /\ up = TRUE /\ crashes = 0
        /\ incarnation = 0 /\ ready = TRUE
        /\ received = [n \in {"B", "C"} |-> {}]
        /\ partnerDurable = 0 /\ accepted = {} /\ durableAccepted = {}
        /\ learned = {} /\ receipt = 0
        /\ dataB = 0 /\ dataC = 0 /\ control = NoControl
        /\ decided = [n \in Nodes |-> 0]
        /\ applied = [n \in Nodes |-> 0]
        /\ effects = [n \in Nodes |-> [i \in Ops |-> 0]]
        /\ chosen = {} /\ replied = {} /\ replyWitness = {}

(* SESSION/CLAIM protection is an installed immutable baseline. A new command
   gets exactly one fixed identity/intent; transport retries never allocate it. *)
Admit == /\ up /\ ready /\ tail % 3 = 0 /\ tail < 3 * MaxOps
         /\ tail' = tail + 2
         /\ UNCHANGED <<durable, up, crashes, incarnation, ready, received,
              partnerDurable, accepted, durableAccepted, learned, receipt,
              dataB, dataC, control, decided, applied, effects, chosen,
              replied, replyWitness>>

SyncOwner == /\ up /\ durable < tail /\ durable' = tail
             /\ UNCHANGED <<tail, up, crashes, incarnation, ready, received,
                  partnerDurable, accepted, durableAccepted, learned, receipt,
                  dataB, dataC, control, decided, applied, effects, chosen,
                  replied, replyWitness>>

Choose(i) == /\ CurrentAcceptance(i) /\ i = decided["A"] + 1
             /\ decided' = [decided EXCEPT !["A"] = i]
             /\ chosen' = chosen \cup {i}
             /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready,
                  received, partnerDurable, accepted, durableAccepted, learned,
                  receipt, dataB, dataC, control, applied, effects, replied,
                  replyWitness>>

AppendResult(i) == /\ up /\ ready /\ tail = Commit(i)
                  /\ i <= decided["A"]
                  /\ CurrentAcceptance(i)
                  /\ tail' = Result(i)
                  /\ UNCHANGED <<durable, up, crashes, incarnation, ready,
                       received, partnerDurable, accepted, durableAccepted,
                       learned, receipt, dataB, dataC, control, decided, applied,
                       effects, chosen, replied, replyWitness>>

(* One bounded slot per data channel. Any emitted frame may be selected,
   including a later frame first. Delivery does not consume the slot: repeated
   deliveries model duplicates, Drop models loss/consumption, Send retransmits. *)
Send(n, p) == /\ up /\ p <= tail /\ p <= R(n) + 3
             /\ (IF n = "B" THEN /\ dataB = 0 /\ dataB' = p
                                      /\ UNCHANGED dataC
                ELSE /\ dataC = 0 /\ dataC' = p /\ UNCHANGED dataB)
             /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready,
                  received, partnerDurable, accepted, durableAccepted, learned,
                  receipt, control, decided, applied, effects, chosen, replied,
                  replyWitness>>

Deliver(n, p) ==
    /\ p = (IF n = "B" THEN dataB ELSE dataC) /\ p # 0
    /\ received' = [received EXCEPT ![n] = @ \cup {p}]
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready,
         partnerDurable, accepted, durableAccepted, learned, receipt,
         dataB, dataC, control, decided, applied, effects, chosen, replied,
         replyWitness>>

Drop(n) == /\ (IF n = "B" THEN /\ dataB # 0 /\ dataB' = 0
                                   /\ UNCHANGED dataC
              ELSE /\ dataC # 0 /\ dataC' = 0 /\ UNCHANGED dataB)
           /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready,
                received, partnerDurable, accepted, durableAccepted, learned,
                receipt, control, decided, applied, effects, chosen, replied,
                replyWitness>>

Accept(i) == /\ ready /\ i \notin accepted
             /\ (IF Bug = "gap-ack" THEN Commit(i) \in received["B"]
                ELSE R("B") >= Commit(i))
             /\ accepted' = accepted \cup {i}
             /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready,
                  received, partnerDurable, durableAccepted, learned, receipt,
                  dataB, dataC, control, decided, applied, effects, chosen,
                  replied, replyWitness>>

SendControl(kind, p) == /\ control = NoControl /\ ready
    /\ (IF kind = "accept" THEN p \in accepted
       ELSE p = R("B") /\ p > 0)
    /\ control' = [kind |-> kind, pos |-> p, incarnation |-> incarnation]
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, learned, receipt,
         dataB, dataC, decided, applied, effects, chosen, replied, replyWitness>>

DeliverControl(kind, p) == /\ up /\ ready /\ control # NoControl
    /\ control.kind = kind /\ control.pos = p
    /\ (control.incarnation = incarnation \/ Bug = "stale-session")
    /\ learned' = (IF control.kind = "accept"
                  THEN learned \cup {control.pos} ELSE learned)
    /\ receipt' = (IF control.kind = "receipt"
                  THEN IF control.pos > receipt THEN control.pos ELSE receipt
                  ELSE receipt)
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, dataB, dataC, control,
         decided, applied, effects, chosen, replied, replyWitness>>

DropControl == /\ control # NoControl /\ control' = NoControl
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, learned, receipt,
         dataB, dataC, decided, applied, effects, chosen, replied, replyWitness>>

SyncPartner == /\ partnerDurable < R("B") \/ durableAccepted # accepted
    /\ partnerDurable' = R("B") /\ durableAccepted' = accepted
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         accepted, learned, receipt, dataB, dataC, control, decided, applied,
         effects, chosen, replied, replyWitness>>

RestartPartner == /\ incarnation < MaxRestarts
    /\ incarnation' = incarnation + 1 /\ ready' = FALSE
    /\ received' = [received EXCEPT !["B"] = 1..partnerDurable]
    /\ accepted' = durableAccepted /\ learned' = {} /\ receipt' = 0
    /\ UNCHANGED <<tail, durable, up, crashes, partnerDurable,
         durableAccepted, dataB, dataC, control, decided, applied, effects,
         chosen, replied, replyWitness>>

(* No absence inference. Rejoin the same owner only after restoring its full
   durable prefix. Historical retained ACCEPT is re-attested by this incarnation. *)
Reconcile == /\ up /\ ~ready /\ R("B") >= durable /\ ready' = TRUE
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, received,
         partnerDurable, accepted, durableAccepted, learned, receipt,
         dataB, dataC, control, decided, applied, effects, chosen, replied,
         replyWitness>>

CrashOwner == /\ up /\ crashes < MaxCrashes /\ up' = FALSE
    /\ crashes' = crashes + 1 /\ tail' = durable
    /\ learned' = {} /\ receipt' = 0
    /\ UNCHANGED <<durable, incarnation, ready, received, partnerDurable,
         accepted, durableAccepted, dataB, dataC, control, decided, applied,
         effects, chosen, replied, replyWitness>>

(* Retained disk and fixed authority only; this is not takeover. Deterministic
   unflushed records are regenerated with the same ID/intent after restart. *)
RestartOwner == /\ ~up /\ up' = TRUE
    /\ UNCHANGED <<tail, durable, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, learned, receipt,
         dataB, dataC, control, decided, applied, effects, chosen, replied,
         replyWitness>>

LearnDecision(n) == /\ decided[n] < MaxOps
    /\ Result(decided[n] + 1) <= R(n)
    /\ decided' = [decided EXCEPT ![n] = @ + 1]
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, learned, receipt, dataB,
         dataC, control, applied, effects, chosen, replied, replyWitness>>

(* Complete exact-ID apply and its admitted cache barrier are one abstract step.
   At/below the applied prefix, dedup skips replay over later serving state. *)
Apply(n, i) == /\ i <= decided[n]
    /\ (i = applied[n] + 1 \/ i <= applied[n])
    /\ (n # "A" \/ CurrentAcceptance(i))
    /\ (n = "A" \/ Result(i) <= R(n))
    /\ applied' = [applied EXCEPT ![n] = IF i > @ THEN i ELSE @]
    /\ effects' = [effects EXCEPT ![n][i] =
          IF Bug = "double-apply" THEN IF @ = 0 THEN 1 ELSE 2 ELSE 1]
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, learned, receipt, dataB,
         dataC, control, decided, chosen, replied, replyWitness>>

Reply(i) == /\ i \notin replied /\ ProtectedResult(i)
    /\ i <= decided["A"] /\ i <= applied["A"]
    /\ replied' = replied \cup {i}
    /\ replyWitness' = replyWitness \cup {
         [op |-> i, local |-> durable, retained |-> R("B"),
          accept |-> i \in accepted, result |-> receipt,
          authority |-> up /\ ready]}
    /\ UNCHANGED <<tail, durable, up, crashes, incarnation, ready, received,
         partnerDurable, accepted, durableAccepted, learned, receipt, dataB,
         dataC, control, decided, applied, effects, chosen>>

Next == \/ Admit \/ SyncOwner \/ SyncPartner \/ RestartPartner
        \/ Reconcile \/ CrashOwner \/ RestartOwner
        \/ DropControl
        \/ \E kind \in {"accept", "receipt"}, p \in Frames:
              SendControl(kind, p) \/ DeliverControl(kind, p)
        \/ \E i \in Ops: Choose(i) \/ AppendResult(i) \/ Accept(i) \/ Reply(i)
        \/ \E n \in {"B", "C"}: Drop(n) \/ LearnDecision(n)
        \/ \E n \in {"B", "C"}, p \in Frames: Send(n, p) \/ Deliver(n, p)
        \/ \E n \in Nodes, i \in Ops: Apply(n, i)

Spec == Init /\ [][Next]_vars

(* Finite crash/restart budgets retain all faults in the graph. Strong fairness
   for the next missing frame prevents infinite loss/reordering starvation.
   Workers are weakly fair; available storage/capacity and atomic barriers are
   assumptions of these abstract enabled actions, not runtime claims. *)
DeliverNext(n) == Deliver(n, R(n) + 1)
DeliverReceipt == \E p \in Frames: DeliverControl("receipt", p)

(* Fair transmission exposes recurrent wire opportunities, not receipt/apply.
   With finite tail/incarnation changes and WF Drop, this is the queue-level
   form of retry fairness: a persistently missing next frame must be queued
   repeatedly. SF delivery below then forces an actual retained-prefix advance. *)
TransmissionOpportunities ==
    /\ \A n \in {"B", "C"}: []<>(
          R(n) >= tail \/ (IF n = "B" THEN dataB ELSE dataC) = R(n) + 1)
    /\ \A i \in Ops: []<>(
          i \notin accepted \/ i \in learned \/ ~ready \/
          control = [kind |-> "accept", pos |-> i, incarnation |-> incarnation])
    /\ []<>(receipt >= R("B") \/ ~ready \/
          control = [kind |-> "receipt", pos |-> R("B"),
                     incarnation |-> incarnation])

Fairness == /\ TransmissionOpportunities
            /\ WF_vars(RestartOwner) /\ WF_vars(Reconcile)
            /\ WF_vars(SyncOwner) /\ WF_vars(SyncPartner)
            /\ \A i \in Ops:
                  SF_vars(DeliverControl("accept", i))
            /\ SF_vars(DeliverReceipt)
            /\ WF_vars(DropControl)
            /\ \A n \in {"B", "C"}:
                  /\ WF_vars(Drop(n))
                  /\ WF_vars(LearnDecision(n))
                  /\ SF_vars(DeliverNext(n))
            /\ \A i \in Ops:
                  /\ WF_vars(Accept(i)) /\ WF_vars(Choose(i))
                  /\ WF_vars(AppendResult(i)) /\ WF_vars(Reply(i))
                  /\ \A n \in Nodes: WF_vars(Apply(n, i))
LiveSpec == Spec /\ Fairness

TypeOK == /\ tail \in 0..(3 * MaxOps) /\ durable \in 0..tail
    /\ up \in BOOLEAN /\ ready \in BOOLEAN
    /\ crashes \in 0..MaxCrashes /\ incarnation \in 0..MaxRestarts
    /\ received \in [ {"B", "C"} -> SUBSET Frames]
    /\ partnerDurable \in 0..R("B")
    /\ accepted \subseteq Ops /\ durableAccepted \subseteq accepted
    /\ learned \subseteq Ops /\ receipt \in 0..(3 * MaxOps)
    /\ dataB \in 0..(3 * MaxOps) /\ dataC \in 0..(3 * MaxOps)
    /\ control \in [kind : {"none", "accept", "receipt"},
                     pos : 0..(3 * MaxOps), incarnation : 0..MaxRestarts]
    /\ decided \in [Nodes -> 0..MaxOps]
    /\ applied \in [Nodes -> 0..MaxOps]
    /\ effects \in [Nodes -> [Ops -> 0..2]]
    /\ chosen \subseteq Ops /\ replied \subseteq chosen

AcceptanceHasPrefix == \A i \in accepted: Commit(i) <= R("B")
FreshReceipts == ready => /\ receipt <= R("B") /\ learned \subseteq accepted
ProtectedSuccess == \A w \in replyWitness:
    /\ w.authority /\ w.local >= Result(w.op)
    /\ w.retained >= Result(w.op) /\ w.result >= Result(w.op) /\ w.accept
OrderedApply == \A n \in Nodes:
    /\ applied[n] <= decided[n]
    /\ (1..decided[n]) \subseteq chosen
    /\ \A i \in Ops: effects[n][i] = IF i <= applied[n] THEN 1 ELSE 0
Safety == TypeOK /\ AcceptanceHasPrefix /\ FreshReceipts
          /\ ProtectedSuccess /\ OrderedApply
AsyncCatchup == \A i \in Ops: (i \in chosen) ~> (applied["C"] >= i)
Completion == \A i \in Ops: (Commit(i) <= durable) ~> (i \in replied)
=============================================================================
