---------------------------- MODULE Transport ----------------------------
EXTENDS Integers, Sequences, TLC

(* One local Endpoint and one admitted peer; UUIDs are symbolic, not ordered.
   Each pending slot is an incoming socket. Wire bytes/host/registry/greeting
   validation are assumed successful before CheckHello; parser tests remain Go.
   CheckHello and Install are distinct mutex-protected steps. *)
CONSTANTS Bug, MaxControls, MaxExpectations, MaxRegistries, MaxRestarts
Incarnations == {1, 2}
Classes == {"control", "live", "bulk"}
Slots == {1, 2}
EmptyConnection == [inc |-> 0, session |-> 0, expectation |-> 0]
EmptyPending == [phase |-> "empty", inc |-> 0, class |-> "control", session |-> 0]
EmptyClasses == [c \in Classes |-> EmptyConnection]
EmptySlots == [s \in Slots |-> EmptyPending]

VARIABLES pin, requiredPin, classes, pending, revoked, revocationRequired,
          controls, expectations, registries, restarts, reconciled, serving,
          progress
vars == <<pin, requiredPin, classes, pending, revoked, revocationRequired,
          controls, expectations, registries, restarts, reconciled, serving,
          progress>>

WitnessEvents == CASE Bug = "witness-disconnect" -> <<"control1", "disconnect", "reject2", "control1">>
 [] Bug = "witness-registry" -> <<"control1", "revoke", "registry", "reject2", "control1">>
 [] Bug = "witness-expect" -> <<"allclasses", "expect-all2", "reject1", "control2">>
 [] Bug = "witness-same" -> <<"control1", "allclasses", "expect-all1">>
 [] Bug = "witness-revoked" -> <<"revoke", "expect2", "reject-revoked">>
 [] Bug = "witness-restart" -> <<"control2", "restart", "control1", "reconcile">>
 [] Bug = "witness-pending" -> <<"check1", "expect-pending2", "control2">>
 [] OTHER -> <<>>
Observe(event) == IF progress < Len(WitnessEvents)
                 THEN IF event = WitnessEvents[progress + 1] THEN progress + 1 ELSE progress
                 ELSE progress
Event(prefix, inc) == CASE prefix = "check" -> IF inc = 1 THEN "check1" ELSE "check2"
 [] prefix = "control" -> IF inc = 1 THEN "control1" ELSE "control2"
 [] prefix = "expect" -> IF inc = 1 THEN "expect1" ELSE "expect2"
 [] OTHER -> IF inc = 1 THEN "reject1" ELSE "reject2"
NoIdentifiedPending == [s \in Slots |-> IF pending[s].phase = "checked"
                                      THEN EmptyPending ELSE pending[s]]
PinAccepts(inc) == pin = 0 \/ pin = inc \/ Bug = "overwrite"
ControlSession == classes["control"].session

Init ==
 /\ pin = 0 /\ requiredPin = 0 /\ classes = EmptyClasses /\ pending = EmptySlots
 /\ revoked = FALSE /\ revocationRequired = FALSE
 /\ controls = 0 /\ expectations = 0 /\ registries = 0 /\ restarts = 0
 /\ reconciled = FALSE /\ serving = FALSE /\ progress = 0

(* Raw sockets have not yet claimed their identity. A data HELLO carries the
   exact session it named when begun; it cannot silently rebind at installation. *)
Begin(s, inc, class) ==
 /\ pending[s].phase = "empty"
 /\ class = "control" \/ ControlSession # 0
 /\ pending' = [pending EXCEPT ![s] = [phase |-> "raw", inc |-> inc,
       class |-> class, session |-> IF class = "control" THEN 0 ELSE ControlSession]]
 /\ UNCHANGED <<pin, requiredPin, classes, revoked, revocationRequired, controls,
                expectations, registries, restarts, reconciled, serving, progress>>
HelloValid(p) ==
 /\ ~revoked /\ PinAccepts(p.inc)
 /\ IF p.class = "control" THEN ControlSession = 0
    ELSE ControlSession # 0 /\ p.session = ControlSession
         /\ p.inc = classes["control"].inc
CheckHello(s) ==
 LET p == pending[s] IN
 /\ p.phase = "raw" /\ HelloValid(p)
 /\ pending' = [pending EXCEPT ![s].phase = "checked"]
 /\ progress' = Observe(Event("check", p.inc))
 /\ UNCHANGED <<pin, requiredPin, classes, revoked, revocationRequired, controls,
                expectations, registries, restarts, reconciled, serving>>
RejectHello(s) ==
 LET p == pending[s] IN
 /\ p.phase = "raw" /\ ~HelloValid(p)
 /\ pending' = [pending EXCEPT ![s] = EmptyPending]
 /\ progress' = Observe(IF revoked THEN "reject-revoked" ELSE Event("reject", p.inc))
 /\ UNCHANGED <<pin, requiredPin, classes, revoked, revocationRequired, controls,
                expectations, registries, restarts, reconciled, serving>>

Install(s) ==
 LET p == pending[s]
     newSession == IF p.class = "control" THEN controls + 1 ELSE p.session
     nextClasses == [classes EXCEPT ![p.class] =
       [inc |-> p.inc, session |-> newSession, expectation |-> expectations]]
 IN
 /\ p.phase = "checked" /\ HelloValid(p)
 /\ p.class # "control" \/ controls < MaxControls
 /\ classes' = nextClasses
 /\ pending' = [pending EXCEPT ![s] = EmptyPending]
 /\ pin' = p.inc
 /\ requiredPin' = IF requiredPin = 0 THEN p.inc ELSE requiredPin
 /\ controls' = IF p.class = "control" THEN controls + 1 ELSE controls
 /\ progress' = Observe(IF p.class = "control" THEN Event("control", p.inc)
      ELSE IF \A c \in Classes : nextClasses[c].session # 0
           THEN "allclasses" ELSE "data")
 /\ UNCHANGED <<revoked, revocationRequired, expectations, registries, restarts,
                reconciled, serving>>
Abandon(s) ==
 /\ pending[s].phase # "empty"
 /\ pending' = [pending EXCEPT ![s] = EmptyPending]
 /\ UNCHANGED <<pin, requiredPin, classes, revoked, revocationRequired, controls,
                expectations, registries, restarts, reconciled, serving, progress>>

Disconnect ==
 /\ ControlSession # 0
 /\ classes' = EmptyClasses /\ pending' = NoIdentifiedPending
 /\ pin' = IF Bug = "disconnect-forgets" THEN 0 ELSE pin
 /\ progress' = Observe("disconnect")
 /\ UNCHANGED <<requiredPin, revoked, revocationRequired, controls, expectations,
                registries, restarts, reconciled, serving>>
LoseData(c) ==
 /\ c # "control" /\ classes[c].session # 0
 /\ classes' = [classes EXCEPT ![c] = EmptyConnection]
 /\ UNCHANGED <<pin, requiredPin, pending, revoked, revocationRequired, controls,
                expectations, registries, restarts, reconciled, serving, progress>>
Revoke ==
 /\ ~revoked
 /\ revoked' = TRUE /\ revocationRequired' = TRUE
 /\ classes' = EmptyClasses /\ pending' = NoIdentifiedPending
 /\ progress' = Observe("revoke")
 /\ UNCHANGED <<pin, requiredPin, controls, expectations, registries, restarts,
                reconciled, serving>>

(* Even an identical expectation closes every identified class and handshake.
   Revocation survives; this is external reconciliation input, never a grant. *)
Expect(inc) ==
 /\ expectations < MaxExpectations
 /\ pin' = inc /\ requiredPin' = inc /\ expectations' = expectations + 1
 /\ classes' = IF Bug = "expect-keeps-classes" THEN classes ELSE EmptyClasses
 /\ pending' = NoIdentifiedPending
 /\ revoked' = IF Bug = "expect-revocation" THEN FALSE ELSE revoked
 /\ serving' = (serving \/ Bug = "expect-authority")
 /\ progress' = Observe(
      IF Bug = "witness-pending" /\ inc = 2
         /\ \E s \in Slots : pending[s].phase = "checked" /\ pending[s].inc = 1
      THEN "expect-pending2"
      ELSE IF (\A c \in Classes : classes[c].session # 0)
              /\ (Bug # "witness-expect" \/ classes["control"].inc = 1)
           THEN IF inc = 1 THEN "expect-all1" ELSE "expect-all2"
           ELSE Event("expect", inc))
 /\ UNCHANGED <<revocationRequired, controls, registries, restarts, reconciled>>

(* Valid strictly newer same-cluster replacement explicitly resets revocation,
   but retains the peer-incarnation pin. All raw/identified sockets are closed. *)
InstallRegistry ==
 /\ registries < MaxRegistries
 /\ registries' = registries + 1
 /\ pin' = IF Bug = "registry-forgets" THEN 0 ELSE pin
 /\ classes' = EmptyClasses /\ pending' = EmptySlots
 /\ revoked' = FALSE /\ revocationRequired' = FALSE
 /\ progress' = Observe("registry")
 /\ UNCHANGED <<requiredPin, controls, expectations, restarts, reconciled, serving>>

(* A distinct new Endpoint loses ALL in-memory admission knowledge. No durable
   anti-rollback evidence is inferred. Counters only bound this entire search. *)
NewEndpoint ==
 /\ restarts < MaxRestarts
 /\ restarts' = restarts + 1
 /\ pin' = 0 /\ requiredPin' = 0
 /\ classes' = EmptyClasses /\ pending' = EmptySlots
 /\ revoked' = FALSE /\ revocationRequired' = FALSE
 /\ reconciled' = FALSE
 /\ serving' = (Bug = "restart-authority")
 /\ progress' = Observe("restart")
 /\ UNCHANGED <<controls, expectations, registries>>

(* Independent protocol-layer event: its correctness is assumed, not proven.
   Authentication/Expect/registry replacement never enable this transition. *)
Reconcile ==
 /\ ~reconciled /\ ControlSession # 0
 /\ reconciled' = TRUE /\ serving' = TRUE
 /\ progress' = Observe("reconcile")
 /\ UNCHANGED <<pin, requiredPin, classes, pending, revoked, revocationRequired,
                controls, expectations, registries, restarts>>

Next ==
 \/ \E s \in Slots, inc \in Incarnations, c \in Classes : Begin(s, inc, c)
 \/ \E s \in Slots : CheckHello(s) \/ RejectHello(s) \/ Install(s) \/ Abandon(s)
 \/ Disconnect \/ Revoke \/ InstallRegistry \/ NewEndpoint \/ Reconcile
 \/ \E inc \in Incarnations : Expect(inc)
 \/ \E c \in Classes : LoseData(c)

TypeOK ==
 /\ pin \in Incarnations \cup {0} /\ requiredPin \in Incarnations \cup {0}
 /\ revoked \in BOOLEAN /\ revocationRequired \in BOOLEAN
 /\ reconciled \in BOOLEAN /\ serving \in BOOLEAN
 /\ controls \in 0..MaxControls /\ expectations \in 0..MaxExpectations
 /\ registries \in 0..MaxRegistries /\ restarts \in 0..MaxRestarts
 /\ progress \in 0..Len(WitnessEvents)
 /\ classes \in [Classes -> [inc : Incarnations \cup {0}, session : 0..MaxControls,
                             expectation : 0..MaxExpectations]]
 /\ pending \in [Slots -> [phase : {"empty", "raw", "checked"},
       inc : Incarnations \cup {0}, class : Classes, session : 0..MaxControls]]
PinSafety == pin = requiredPin
ClassSafety == \A c \in Classes :
 classes[c].session # 0 =>
  /\ ~revoked /\ classes[c].inc = requiredPin
  /\ classes[c].expectation = expectations
  /\ ControlSession # 0 /\ classes[c].session = ControlSession
RevocationSafety == revoked = revocationRequired
AuthoritySafety == serving => reconciled
Safety == TypeOK /\ PinSafety /\ ClassSafety /\ RevocationSafety /\ AuthoritySafety
WitnessAbsent == progress < Len(WitnessEvents)
Spec == Init /\ [][Next]_vars
=============================================================================
