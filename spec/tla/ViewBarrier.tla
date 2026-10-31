----------------------------- MODULE ViewBarrier -----------------------------
EXTENDS Naturals, FiniteSets, Sequences, TLC

CONSTANTS Domains, MaxPages, Capacity, AllowDisconnect, Bug, Witness, NoValue, FinalOp
Pages == 1..MaxPages
None == NoValue
Epoch == 7
Recall == 11

VARIABLES phase, owner, reserved, armed, dirty, delivered, revoked,
          local, frozen, busy, pinned, connected, session, disconnected,
          stoppedAck, drainedAck, installAck, messages, installed, log,
          slot, stage, protected, cleaned, finalGranted, reply, reads, observed,
          waiterResources, recallKnown, commandCut, staleRejected, offlineStopped
vars == <<phase, owner, reserved, armed, dirty, delivered, revoked,
          local, frozen, busy, pinned, connected, session, disconnected,
          stoppedAck, drainedAck, installAck, messages, installed, log,
          slot, stage, protected, cleaned, finalGranted, reply, reads, observed,
          waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>

Cut == Len(log)
AllStopped == \A d \in Domains : local[d] \in {"stopped", "drained", "installed", "reopened"}
Expected(d, kind, cut) ==
  [domain |-> d, kind |-> kind, epoch |-> Epoch, recall |-> Recall,
   session |-> session[d], manifest |-> frozen[d], cut |-> cut]
Old(d, kind) ==
  [domain |-> d, kind |-> kind, epoch |-> Epoch-1, recall |-> Recall-1,
   session |-> 0, manifest |-> {}, cut |-> 0]
Exact(m, d, kind, cut) == m = Expected(d, kind, cut)
Accept(m, d, kind, cut) ==
  /\ m.domain = d /\ m.kind = kind
  /\ (IF Bug = "stale" THEN TRUE ELSE Exact(m, d, kind, cut))
StoppedKnown == \A d \in Domains : stoppedAck[d] = Expected(d, "stopped", 0)
DrainCut == Cardinality(dirty)
DrainedKnown == \A d \in Domains : drainedAck[d] = Expected(d, "drained", DrainCut)
InstalledKnown == \A d \in Domains : installAck[d] = Expected(d, "install", Cut)
InstalledEverywhere == \A d \in Domains : installed[d] = Cut
Resolved == dirty \subseteq cleaned

Init ==
  /\ phase = "idle" /\ owner \in Domains
  /\ reserved = {} /\ armed = {} /\ dirty = {} /\ delivered = {}
  /\ revoked = {} /\ local = [d \in Domains |-> "open"]
  /\ frozen = [d \in Domains |-> {}]
  /\ busy = Domains /\ pinned = Domains
  /\ connected = Domains /\ session = [d \in Domains |-> 1]
  /\ disconnected = {}
  /\ stoppedAck = [d \in Domains |-> None]
  /\ drainedAck = [d \in Domains |-> None]
  /\ installAck = [d \in Domains |-> None]
  /\ messages = {Old(d, k) : d \in Domains, k \in {"stopped", "drained", "install"}}
  /\ installed = [d \in Domains |-> 0] /\ log = <<>>
  /\ slot = None /\ stage = 0 /\ protected = {} /\ cleaned = {}
  /\ finalGranted = FALSE /\ reply = FALSE /\ reads = {} /\ observed = {}
  /\ waiterResources = {}
  /\ recallKnown = {} /\ commandCut = [d \in Domains |-> 0] /\ staleRejected = {}
  /\ offlineStopped = {}

Reserve(p) ==
  /\ phase = "idle" /\ owner \in connected /\ p \notin reserved
  /\ Cardinality(reserved) < Capacity
  /\ reserved' = reserved \cup {p}
  /\ UNCHANGED <<phase, owner, armed, dirty, delivered, revoked, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Arm(p) ==
  /\ phase = "idle" /\ owner \in connected /\ p \notin armed
  /\ (p \in reserved \/ Bug = "credit")
  /\ armed' = armed \cup {p}
  /\ UNCHANGED <<phase, owner, reserved, dirty, delivered, revoked, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Store(p) ==
  /\ p \in armed /\ p \notin dirty /\ owner \notin revoked
  /\ dirty' = dirty \cup {p}
  /\ UNCHANGED <<phase, owner, reserved, armed, delivered, revoked, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Deliver(p) ==
  /\ p \in dirty /\ p \notin delivered
  /\ (phase = "idle" \/ (AllStopped = TRUE /\ StoppedKnown = TRUE))
  /\ delivered' = delivered \cup {p}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, revoked, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Begin ==
  /\ phase = "idle"
  /\ phase' = "drain"
  /\ local' = [d \in Domains |-> "recalling"]
  /\ waiterResources' = IF Bug = "hold" THEN {"sequencer", "vnode", "drain-credit"} ELSE {}
  /\ UNCHANGED <<owner, reserved, armed, dirty, delivered, revoked, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, recallKnown, commandCut, staleRejected, offlineStopped>>
ReleaseBusy(d) ==
  /\ d \in busy /\ busy' = busy \ {d}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
ReleasePin(d) ==
  /\ d \in pinned /\ pinned' = pinned \ {d}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Revoke(d) ==
  /\ d \in recallKnown
  /\ phase = "drain" /\ local[d] = "recalling" /\ d \notin revoked
  /\ revoked' = revoked \cup {d}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Stop(d) ==
  /\ d \in recallKnown
  /\ phase = "drain" /\ local[d] = "recalling" /\ d \in revoked
  /\ (IF Bug = "alias" THEN TRUE ELSE d \notin busy \cup pinned)
  /\ local' = [local EXCEPT ![d] = "stopped"]
  /\ offlineStopped' = IF Witness = "offlineStop" /\ d \notin connected
                        THEN offlineStopped \cup {d} ELSE offlineStopped
  /\ frozen' = [frozen EXCEPT ![d] = IF d = owner THEN dirty ELSE {}]
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected>>
Emit(d, kind) ==
  /\ CASE kind = "stopped" -> local[d] \in {"stopped", "drained", "installed"}
       [] kind = "drained" -> local[d] \in {"drained", "installed"}
       [] OTHER -> local[d] = "installed"
  /\ LET c == CASE kind = "stopped" -> 0 [] kind = "drained" -> DrainCut [] OTHER -> Cut
         m == Expected(d, kind, c)
     IN /\ m \notin messages /\ messages' = messages \cup {m}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Receive(d, m) ==
  /\ d \in connected /\ m \in messages /\ m.domain = d
  /\ CASE m.kind = "stopped" ->
       /\ phase = "drain" /\ stoppedAck[d] # m
       /\ Accept(m, d, "stopped", 0) = TRUE
       /\ stoppedAck' = [stoppedAck EXCEPT ![d] = m]
       /\ UNCHANGED <<drainedAck, installAck>>
     [] m.kind = "drained" ->
       /\ phase = "drain" /\ drainedAck[d] # m
       /\ Accept(m, d, "drained", DrainCut) = TRUE
       /\ drainedAck' = [drainedAck EXCEPT ![d] = m]
       /\ UNCHANGED <<stoppedAck, installAck>>
     [] OTHER ->
       /\ phase = "install" /\ installAck[d] # m
       /\ Accept(m, d, "install", Cut) = TRUE
       /\ installAck' = [installAck EXCEPT ![d] = m]
       /\ UNCHANGED <<stoppedAck, drainedAck>>
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, messages,
       installed, log, slot, stage, protected, cleaned, finalGranted, reply,
       reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
GrantDrain(p) ==
  /\ phase = "drain" /\ AllStopped = TRUE /\ StoppedKnown = TRUE
  /\ InstalledEverywhere = TRUE /\ slot = None /\ p \in delivered \ cleaned
  /\ p \notin protected /\ waiterResources = {}
  /\ slot' = p /\ stage' = 1
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Advance ==
  /\ slot # None /\ stage \in 1..3
  /\ stage' = stage+1
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, slot, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Protect ==
  /\ slot # None /\ stage = 4
  /\ protected' = protected \cup {slot} /\ log' = Append(log, slot)
  /\ slot' = None /\ stage' = 0
  /\ phase' = IF phase = "final" THEN "install" ELSE phase
  /\ UNCHANGED <<owner, reserved, armed, dirty, delivered, revoked, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, cleaned, finalGranted, reply, reads,
       observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
InstallIntermediate(d) ==
  /\ commandCut[d] = Cut
  /\ phase = "drain" /\ AllStopped = TRUE /\ installed[d] < Cut
  /\ (IF Bug = "recursive" THEN DrainedKnown ELSE TRUE) = TRUE
  /\ installed' = [installed EXCEPT ![d] = Cut]
  /\ local' = IF Bug = "reopen" THEN [local EXCEPT ![d] = "reopened"] ELSE local
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Clean(p) ==
  /\ phase = "drain" /\ p \in dirty \ cleaned /\ p \in protected
  /\ InstalledEverywhere = TRUE
  /\ cleaned' = cleaned \cup {p}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, slot, stage, protected,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Drain(d) ==
  /\ phase = "drain" /\ local[d] = "stopped" /\ AllStopped = TRUE
  /\ Resolved = TRUE /\ InstalledEverywhere = TRUE /\ slot = None
  /\ local' = [local EXCEPT ![d] = "drained"]
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
GrantFinal ==
  /\ phase = "drain" /\ slot = None
  /\ (IF Bug = "early" THEN AllStopped ELSE (StoppedKnown /\ DrainedKnown)) = TRUE
  /\ phase' = "final" /\ slot' = FinalOp /\ stage' = 1 /\ finalGranted' = TRUE
  /\ UNCHANGED <<owner, reserved, armed, dirty, delivered, revoked, local, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, protected, cleaned,
       reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
InstallFinal(d) ==
  /\ commandCut[d] = Cut
  /\ phase = "install" /\ local[d] = "drained"
  /\ local' = [local EXCEPT ![d] = "installed"]
  /\ installed' = [installed EXCEPT ![d] = Cut]
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Reopen ==
  /\ phase = "install" /\ InstalledKnown = TRUE
  /\ phase' = "done" /\ local' = [d \in Domains |-> "reopened"] /\ reply' = TRUE
  /\ UNCHANGED <<owner, reserved, armed, dirty, delivered, revoked, frozen,
       busy, pinned, connected, session, disconnected, stoppedAck, drainedAck,
       installAck, messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Disconnect(d) ==
  /\ AllowDisconnect /\ phase # "done" /\ d \in connected /\ d \notin disconnected
  /\ connected' = connected \ {d} /\ disconnected' = disconnected \cup {d}
  /\ stoppedAck' = [stoppedAck EXCEPT ![d] = None]
  /\ drainedAck' = [drainedAck EXCEPT ![d] = None]
  /\ installAck' = [installAck EXCEPT ![d] = None]
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, session, messages, installed, log, slot, stage,
       protected, cleaned, finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Reconnect(d) ==
  /\ d \notin connected /\ connected' = connected \cup {d}
  /\ session' = [session EXCEPT ![d] = @+1]
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, disconnected, stoppedAck, drainedAck, installAck,
       messages, installed, log, slot, stage, protected, cleaned,
       finalGranted, reply, reads, observed, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>
Read(d) ==
  /\ (local[d] \in {"open", "reopened"} \/ (local[d] = "recalling" /\ d \notin recallKnown))
  /\ (dirty = {} \/ phase = "done" \/ (Bug = "read" /\ d # owner))
  /\ d \notin observed
  /\ observed' = IF phase = "done" THEN observed \cup {d} ELSE observed
  /\ reads' = reads \cup {
       [domain |-> d, data |-> SubSeq(log, 1, installed[d]), expected |-> log,
        pendingDirty |-> dirty \ cleaned, afterReply |-> reply]}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, slot, stage, protected,
       cleaned, finalGranted, reply, waiterResources, recallKnown, commandCut, staleRejected, offlineStopped>>

DeliverRecall(d) ==
  /\ d \in connected /\ phase = "drain" /\ d \notin recallKnown
  /\ recallKnown' = recallKnown \cup {d}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, slot, stage, protected,
       cleaned, finalGranted, reply, reads, observed, waiterResources, commandCut,
       staleRejected, offlineStopped>>
DeliverInstall(d) ==
  /\ d \in connected /\ phase \in {"drain", "install"}
  /\ AllStopped = TRUE /\ installed[d] < Cut /\ commandCut[d] < Cut
  /\ commandCut' = [commandCut EXCEPT ![d] = Cut]
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, slot, stage, protected,
       cleaned, finalGranted, reply, reads, observed, waiterResources, recallKnown,
       staleRejected, offlineStopped>>
RejectStale(d, m) ==
  /\ phase # "done"
  /\ d \in connected /\ session[d] = 2 /\ m \in messages
  /\ m.domain = d /\ m.session = 1 /\ d \notin staleRejected
  /\ staleRejected' = staleRejected \cup {d}
  /\ UNCHANGED <<phase, owner, reserved, armed, dirty, delivered, revoked, local,
       frozen, busy, pinned, connected, session, disconnected, stoppedAck,
       drainedAck, installAck, messages, installed, log, slot, stage, protected,
       cleaned, finalGranted, reply, reads, observed, waiterResources, recallKnown,
       commandCut, offlineStopped>>

Next ==
  \/ \E p \in Pages : Reserve(p) \/ Arm(p) \/ Store(p) \/ Deliver(p) \/ GrantDrain(p) \/ Clean(p)
  \/ Begin \/ Advance \/ Protect \/ GrantFinal \/ Reopen
  \/ \E d \in Domains : ReleaseBusy(d) \/ ReleasePin(d) \/ Revoke(d) \/ Stop(d)
       \/ InstallIntermediate(d) \/ Drain(d) \/ InstallFinal(d)
       \/ Disconnect(d) \/ Reconnect(d) \/ Read(d) \/ DeliverRecall(d) \/ DeliverInstall(d)
       \/ (\E k \in {"stopped", "drained", "install"} : Emit(d, k))
       \/ (\E m \in messages : Receive(d, m) \/ RejectStale(d, m))
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ phase \in {"idle", "drain", "final", "install", "done"}
  /\ owner \in Domains /\ reserved \subseteq Pages /\ armed \subseteq Pages
  /\ dirty \subseteq Pages /\ delivered \subseteq dirty /\ cleaned \subseteq dirty
  /\ revoked \subseteq Domains /\ busy \subseteq Domains /\ pinned \subseteq Domains
  /\ connected \subseteq Domains /\ disconnected \subseteq Domains
  /\ recallKnown \subseteq Domains /\ staleRejected \subseteq Domains
  /\ offlineStopped \subseteq Domains
  /\ commandCut \in [Domains -> 0..(MaxPages+1)]
  /\ local \in [Domains -> {"open", "recalling", "stopped", "drained", "installed", "reopened"}]
  /\ frozen \in [Domains -> SUBSET Pages] /\ session \in [Domains -> 1..2]
  /\ installed \in [Domains -> 0..(MaxPages+1)]
  /\ slot \in Pages \cup {None, FinalOp} /\ stage \in 0..4
  /\ protected \subseteq Pages \cup {FinalOp}
Safety ==
  /\ TypeOK
  /\ armed \subseteq reserved /\ Cardinality(reserved) <= Capacity
  /\ waiterResources = {}
  /\ \A d \in Domains :
       /\ (local[d] \in {"stopped", "drained", "installed", "reopened"} =>
             /\ d \in revoked /\ d \notin busy \cup pinned
             /\ frozen[d] = (IF d = owner THEN dirty ELSE {}))
       /\ (stoppedAck[d] # None => stoppedAck[d] = Expected(d, "stopped", 0))
       /\ (drainedAck[d] # None => drainedAck[d] = Expected(d, "drained", DrainCut))
       /\ (installAck[d] # None => installAck[d] = Expected(d, "install", Cut))
       /\ (local[d] \in {"drained", "installed", "reopened"} => Resolved /\ installed[d] >= DrainCut)
       /\ (local[d] = "reopened" => phase = "done")
  /\ (finalGranted => Resolved /\ Cardinality(protected \cap Pages) = DrainCut)
  /\ (reply => /\ phase = "done" /\ InstalledKnown /\ Resolved
                 /\ FinalOp \in protected /\ InstalledEverywhere)
  /\ Len(log) = Cardinality(protected)
  /\ \A i \in 1..Len(log) : log[i] \in protected
  /\ \A r \in reads :
       /\ r.data = r.expected
       /\ r.pendingDirty = {}
       /\ (r.afterReply => FinalOp \in {r.data[i] : i \in 1..Len(r.data)})

ReceiveKind(d, k) == \E m \in messages : m.kind = k /\ Receive(d, m)

FairSpec == Spec /\ WF_vars(Begin) /\ WF_vars(Advance) /\ WF_vars(Protect)
  /\ WF_vars(GrantFinal) /\ WF_vars(Reopen)
  /\ \A p \in Pages : WF_vars(Deliver(p)) /\ WF_vars(GrantDrain(p)) /\ WF_vars(Clean(p))
  /\ \A d \in Domains :
       /\ WF_vars(ReleaseBusy(d)) /\ WF_vars(ReleasePin(d)) /\ WF_vars(Revoke(d))
       /\ WF_vars(Stop(d)) /\ WF_vars(InstallIntermediate(d)) /\ WF_vars(Drain(d))
       /\ WF_vars(InstallFinal(d)) /\ WF_vars(Reconnect(d))
       /\ WF_vars(DeliverRecall(d)) /\ WF_vars(DeliverInstall(d))
       /\ \A k \in {"stopped", "drained", "install"} : WF_vars(Emit(d, k))
       /\ \A k \in {"stopped", "drained", "install"} : WF_vars(ReceiveKind(d, k))
Completion == <>reply
WitnessReached ==
  CASE Witness = "dirty" -> phase = "done" /\ Cardinality(dirty) = MaxPages
       [] Witness = "closedInstall" -> Cut > 0 /\ phase = "drain" /\ InstalledEverywhere /\ cleaned # {}
       [] Witness = "offlineStop" -> \E d \in Domains :
            d \notin connected /\ local[d] = "stopped" /\ d \in recallKnown
            /\ d \in offlineStopped
            /\ Expected(d, "stopped", 0) \in messages
       [] Witness = "reconnect" -> phase = "done" /\ disconnected # {} /\ dirty # {} /\ staleRejected # {}
       [] Witness = "read" -> phase = "done" /\ Domains \subseteq observed /\ dirty # {}
       [] OTHER -> FALSE
WitnessAbsent == ~WitnessReached
=============================================================================
