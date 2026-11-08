#!/usr/bin/env python3
"""Offline host correlator for the zq3 retained-9p run (gaps G7, G10, G11).

Runs on the workstation and never contacts f3, Rocky or a runtime. Inputs are
the controller's own run directory (plan, state, guest logs) and raw replies
root copied off f3, each named with its SHA256: lifecycle client output of
`hfs-controlled-runtime --control SOCK --operation observe|arm-lost-reply` and
host path snapshots from host_snapshot.py. Outputs are new exclusive 0600
records; every record says acceptance:false.

  case        homelabfs-retained9p-host-correlation/1, the record controller.py
              `release` binds (T1-T5): an OBSERVE before and one after the case,
              same runtime/cut/epoch, plan boot/provider/sessions, unchanged
              journal/ledger/transport identities and an advanced applied position.
  lost-reply  homelabfs-retained9p-lost-reply-correlation/1 (T9): ARM binding,
              the fired UNCERTAIN exposure, source effect since arming and the
              guest CALL/re-read observations.
  k04         homelabfs-retained9p-k04-after-loss/1 (T11): guest I/O after host
              loss failed and the host path showed no raw-directory effect.
  command     the exact lifecycle client argv for observe, arm-lost-reply or
              restart from the latest OBSERVE; restart only after an observed
              quiesce. It prints argv and executes nothing.

A binding problem (wrong run, digest, runtime, stale cut, wrong case) refuses
and writes nothing. For T9/T11 the observed outcome, good or bad, is the
record's content; it never becomes acceptance.
"""
import argparse
import errno
import importlib.util
import json
import os
from pathlib import Path
import sys


def load_controller():
    """The controller from the same directory, loaded by path (works under -I)."""
    path = Path(__file__).with_name("controller.py")
    spec = importlib.util.spec_from_file_location("retained9p_controller", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


controller = load_controller()
Refused = controller.Refused
EVIDENCE_VERSION = "homelabfs-controlled-evidence/1"
OBSERVE_FORMAT = "homelabfs-retained9p-host-observe/1"
LOST_REPLY_VERSION = "homelabfs-retained9p-lost-reply-correlation/1"
K04_VERSION = "homelabfs-retained9p-k04-after-loss/1"
SNAPSHOT_VERSION = "homelabfs-retained9p-host-path-snapshot/1"  # Same as host_snapshot.py.
# The lifecycle client prints the raw reply (at most 128 KiB) plus a newline.
REPLY_LIMIT = (128 << 10) + 1
SNAPSHOT_LIMIT = 64 << 10
RECORD_LIMIT = 512 << 10  # controller.py reads a correlation with this bound.
POSITION = ["Volume", "Generation", "Configuration", "Epoch", "Sequence"]
SERVICE_IDENTITY = ["journal", "owner", "generation", "configuration"]
RETAINED = "payload-retained-awaiting-correlation"
CASE_LIMITS = [
    "OBSERVE is the daemon's own inert evidence; this record correlates identities, not durability",
    "an advanced applied position shows a committed journal effect, not which guest syscall made it",
    "PTE/TLB exclusion, funded drains and recovery after owner loss are not covered",
]


def retained_run(output, plan_sha256):
    """The controller's run directory, read only: exact plan digest, no error."""
    controller.check_output_path(output)
    output = Path(output)
    raw = controller.read(output / "plan.json")
    if controller.digest(raw) != plan_sha256:
        raise Refused("exact batch plan digest differs")
    plan = controller.parse(raw)
    controller.validate_shape(plan)
    state = controller.load(output / "state.json")
    if state.get("plan_sha256") != plan_sha256 or "error" in state:
        raise Refused("run state differs or recorded an error; inspect, never correlate")
    return output, plan, state


def load_reply(path, sha256):
    """One raw lifecycle reply pinned by its digest; duplicate keys refuse."""
    if not isinstance(sha256, str) or not controller.HASH.fullmatch(sha256):
        raise Refused("exact reply digest required")
    reply = controller.parse(controller.verified_bytes(path, sha256, REPLY_LIMIT))
    if not isinstance(reply, dict) or type(reply.get("sequence")) is not int or reply["sequence"] <= 0:
        raise Refused("lifecycle reply without a positive sequence")
    return {"sha256": sha256, "sequence": reply["sequence"], "reply": reply}


def evidence_of(entry, label):
    """A complete SERVING OBSERVE without publication or a RESTART attempt."""
    reply = entry["reply"]
    evidence = reply.get("evidence")
    if reply.get("status") != "OBSERVED" or not isinstance(evidence, dict):
        raise Refused(label + " is not an OBSERVED reply")
    if evidence.get("version") != EVIDENCE_VERSION or evidence.get("publication", "missing") is not None:
        raise Refused(label + " evidence version/publication differs")
    if evidence.get("complete") is not True or evidence.get("stage") != "SERVING":
        raise Refused(label + " OBSERVE is incomplete or not SERVING")
    if "restart" in evidence:
        raise Refused(label + " OBSERVE follows a RESTART attempt")
    return evidence


def check_same_binding(first, second, labels):
    """Strictly later sequence, same runtime and the same cut/epoch."""
    if second["sequence"] <= first["sequence"]:
        raise Refused("lifecycle sequence must increase: " + " -> ".join(labels))
    a, b = first["reply"]["evidence"], second["reply"]["evidence"]
    if a.get("runtime") != b.get("runtime"):
        raise Refused("runtime differs between " + " and ".join(labels))
    if (a.get("cut"), a.get("epoch")) != (b.get("cut"), b.get("epoch")):
        raise Refused("stale or rotated cut/epoch between " + " and ".join(labels))


def plan_identity(plan):
    request = plan["requests"][0]
    return {"boot_id": request["boot_id"], "provider_nonce": request["provider_nonce"],
            "sessions": [mount["session"] for mount in request["mounts"]]}


def admission_of(evidence, plan, label):
    """Exactly the plan's guest: one unfenced admission, boot, provider, sessions."""
    identity = plan_identity(plan)
    admissions = evidence.get("admissions")
    if not isinstance(admissions, list) or len(admissions) != 1 or not isinstance(admissions[0], dict):
        raise Refused(label + " must carry exactly one guest admission")
    admission = admissions[0]
    if admission.get("fenced") is not False or admission.get("refresh_error"):
        raise Refused(label + " admission fenced or refresh failed")
    if (admission.get("boot_id"), admission.get("provider_nonce")) != (
            identity["boot_id"], identity["provider_nonce"]):
        raise Refused(label + " guest boot/provider differs from the plan")
    domains = admission.get("domains")
    if (not isinstance(domains, list) or not all(isinstance(row, dict) for row in domains) or
            sorted(str(row.get("session")) for row in domains) != sorted(identity["sessions"])):
        raise Refused(label + " admitted domains differ from the plan sessions")
    return {"boot_id": admission["boot_id"], "provider_nonce": admission["provider_nonce"],
            "domains": sorted([row.get("session"), row.get("phase"), row.get("fence")]
                              for row in domains)}


def position(value, label):
    """(identity, order) of a journal position; Volume/Generation/Configuration fixed."""
    if (not isinstance(value, dict) or set(value) != set(POSITION) or
            type(value["Epoch"]) is not int or type(value["Sequence"]) is not int):
        raise Refused(label + " applied position missing or malformed")
    return [value["Volume"], value["Generation"], value["Configuration"]], (value["Epoch"], value["Sequence"])


def service_of(evidence, label):
    service = evidence.get("service")
    if not isinstance(service, dict) or service.get("error") or service.get("state") != "SERVING":
        raise Refused(label + " service source missing, failed or not SERVING")
    if not all(isinstance(service.get(key), str) and service[key] for key in SERVICE_IDENTITY):
        raise Refused(label + " journal/owner/generation/configuration identity missing")
    position(service.get("applied"), label)
    return service


def advanced(before, after, label):
    """True when the applied position moved forward; identity changes refuse."""
    identity, order = position(before.get("applied"), label)
    later_identity, later_order = position(after.get("applied"), label)
    if identity != later_identity:
        raise Refused(label + " applied position identity changed")
    if later_order < order:
        raise Refused(label + " applied position went backwards")
    return later_order > order


def check_service_identity(before, after, label):
    if [before[key] for key in SERVICE_IDENTITY] != [after[key] for key in SERVICE_IDENTITY]:
        raise Refused(label + " journal/owner/generation/configuration changed")


def ledger_identity(ledger, keys, label):
    if not isinstance(ledger, dict) or not all(key in ledger for key in keys):
        raise Refused(label + " ledger evidence missing")
    return [ledger[key] for key in keys]


def owners_of(evidence, plan, label):
    """Provider/view-controller ledger identities and the transport binding."""
    providers = evidence.get("providers")
    if (not isinstance(providers, list) or not all(isinstance(row, dict) for row in providers) or
            sorted(str(row.get("session")) for row in providers) != sorted(plan_identity(plan)["sessions"])):
        raise Refused(label + " providers differ from the plan sessions")
    if any(row.get("error") for row in providers):
        raise Refused(label + " provider evidence failed")
    view = evidence.get("controller") or {}
    transport = evidence.get("transport") or {}
    if view.get("error") or transport.get("error") or type(transport.get("high")) is not int:
        raise Refused(label + " view controller or transport evidence failed")
    return {"providers": sorted([row["session"]] + ledger_identity(
                row.get("ledger"), ["Ledger", "Device", "Inode"], label) for row in providers),
            "controller": ledger_identity(view.get("ledger"), ["Device", "Inode"], label),
            "transport": [transport.get("controller"), transport.get("fuse_session")],
            "high": transport["high"]}


def check_owners(first, second, plan, labels):
    a, b = owners_of(first, plan, labels[0]), owners_of(second, plan, labels[1])
    for key, name in [("providers", "provider ledger"), ("controller", "view controller ledger"),
                      ("transport", "transport controller/FUSE session")]:
        if a[key] != b[key]:
            raise Refused(name + " identity changed between " + " and ".join(labels))
    if b["high"] < a["high"]:
        raise Refused("transport high-water went backwards")
    return b


def check_pair(first, second, plan, labels):
    """Two OBSERVEs of one unchanged runtime incarnation; returns a summary."""
    one, two = evidence_of(first, labels[0]), evidence_of(second, labels[1])
    check_same_binding(first, second, labels)
    admission = admission_of(one, plan, labels[0])
    if admission != admission_of(two, plan, labels[1]):
        raise Refused("admitted domain phase/fence changed between " + " and ".join(labels))
    before, after = service_of(one, labels[0]), service_of(two, labels[1])
    check_service_identity(before, after, labels[1])
    owners = check_owners(one, two, plan, labels)
    return {"runtime": two["runtime"], "cut": two["cut"], "epoch": two["epoch"],
            **{key: after[key] for key in SERVICE_IDENTITY},
            "applied_before": before["applied"], "applied_after": after["applied"],
            "advanced": advanced(before, after, labels[1]), "admission": admission,
            "providers": owners["providers"], "controller_ledger": owners["controller"],
            "transport": owners["transport"], "transport_high": owners["high"]}


def entry_record(entry):
    return {"sha256": entry["sha256"], "sequence": entry["sequence"], "reply": entry["reply"]}


def retained_case(output, state, case):
    """The case the controller holds retained, its verified log and admission."""
    cases = state.get("cases", [])
    if not cases or cases[-1].get("name") != case or cases[-1].get("state") != RETAINED:
        raise Refused("case is not the retained case awaiting host correlation: " + str(case))
    record = cases[-1]
    raw = controller.read(output / case / "stdout.log", controller.STREAM_LIMIT)
    if controller.digest(raw) != record["stdout_sha256"]:
        raise Refused("retained case evidence changed")
    events = controller.read_events(raw)
    if controller.retained_event(events, case)["inventory_sha256"] != record["inventory_sha256"]:
        raise Refused("retained inventory differs from controller state")
    admitted = [event for event in events if event.get("event") == "admitted"]
    if len(admitted) != 1:
        raise Refused("retained case log must hold exactly one guest admission")
    return record, admitted[0]


def previous_retained(output, state):
    """The preceding case's bound retained OBSERVE, or None for the first case."""
    cases = state["cases"]
    if len(cases) < 2:
        return None, None
    previous = cases[-2]
    if previous.get("state") != "released" or not previous.get("correlation_sha256"):
        raise Refused("previous case was not released with a bound correlation")
    raw = controller.verified_bytes(output / previous["name"] / "correlation.json",
                                    previous["correlation_sha256"], RECORD_LIMIT)
    retained = controller.parse(raw)["host_observe"]["retained"]
    return previous["correlation_sha256"], retained


def check_chain(previous, baseline):
    """The baseline follows the previous case's release in the same incarnation."""
    if previous is None:
        return
    one, two = previous["reply"]["evidence"], baseline["reply"]["evidence"]
    if baseline["sequence"] <= previous["sequence"]:
        raise Refused("baseline OBSERVE predates the previous case's correlation")
    if (one.get("runtime"), one.get("cut"), one.get("epoch")) != (
            two.get("runtime"), two.get("cut"), two.get("epoch")):
        raise Refused("stale or rotated cut/epoch since the previous case")
    if not advanced(one["service"], two["service"], "baseline after previous release"):
        raise Refused("baseline shows no applied effect of the previous release")


def case_record(output, plan_sha256, case, baseline, retained):
    """homelabfs-retained9p-host-correlation/1 for the retained case (G7)."""
    output, plan, state = retained_run(output, plan_sha256)
    record, admitted = retained_case(output, state, case)
    summary = check_pair(baseline, retained, plan, ("baseline", "retained"))
    for entry in [baseline, retained]:
        if "lost_reply" in entry["reply"]["evidence"]:
            raise Refused("case OBSERVE shows an armed lost-reply fixture")
    if not summary["advanced"]:
        raise Refused("journal applied position did not advance across the case")
    guest = (admitted.get("boot_id"), admitted.get("provider_nonce"))
    if guest != (summary["admission"]["boot_id"], summary["admission"]["provider_nonce"]):
        raise Refused("guest admission differs from host admission")
    previous_sha256, previous = previous_retained(output, state)
    check_chain(previous, baseline)
    host = {"format": OBSERVE_FORMAT, "baseline": entry_record(baseline),
            "retained": entry_record(retained), "previous_correlation_sha256": previous_sha256,
            "summary": summary, "limits": CASE_LIMITS}
    return {"version": controller.CORRELATION_VERSION, "run_id": plan["run_id"], "case": case,
            "case_stdout_sha256": record["stdout_sha256"],
            "inventory_sha256": record["inventory_sha256"], "host_observe": host,
            "acceptance": False}


def completed_steps(output, state):
    """Completed lifecycle steps with their re-verified guest observations."""
    steps = {}
    for item in state.get("lifecycle", []):
        if item.get("state") != "completed":
            raise Refused("lifecycle step not completed: " + str(item.get("step")))
        path = output / ("lifecycle-" + item["step"]) / "stdout.log"
        checked = controller.check_step_terminal(path, item["step"])
        if checked["stdout_sha256"] != item.get("stdout_sha256"):
            raise Refused("lifecycle step evidence changed: " + item["step"])
        steps[item["step"]] = checked
    return steps


def require_steps(steps, names):
    missing = [name for name in names if name not in steps]
    if missing:
        raise Refused("required lifecycle step evidence missing: " + ",".join(missing))


def armed_view(armed, before, evidence, session):
    """The ARMED reply: current cut/epoch, m0's session, an unfired CALL fixture."""
    reply = armed["reply"]
    view = reply.get("lost_reply")
    if reply.get("status") != "ARMED" or not isinstance(view, dict):
        raise Refused("ARM reply is not ARMED")
    if armed["sequence"] <= before["sequence"]:
        raise Refused("ARM sequence must follow the pre-arm OBSERVE")
    if (view.get("cut"), view.get("epoch")) != (evidence["cut"], evidence["epoch"]):
        raise Refused("ARM used a stale cut/epoch")
    exposure = view.get("exposure")
    if (view.get("session") != session or view.get("error") or not view.get("fixture") or
            not isinstance(exposure, dict) or exposure.get("state") != "ARMED" or
            exposure.get("operation") != "CALL" or exposure.get("session") != session or
            type(exposure.get("armed_high")) is not int):
        raise Refused("ARM reply is not an unfired CALL fixture on m0's session")
    baseline = view.get("baseline")
    if not isinstance(baseline, dict):
        raise Refused("ARM reply without its source baseline")
    check_service_identity(service_of(evidence, "pre-arm"), service_of({"service": baseline}, "arm baseline"),
                           "arm baseline")
    return view


def fired_exposure(evidence, view):
    """The armed fixture in the post-CALL OBSERVE; None while it never fired."""
    found = evidence.get("lost_reply")
    keys = ["fixture", "cut", "epoch", "session"]
    if not isinstance(found, dict) or [found.get(key) for key in keys] != [view[key] for key in keys]:
        raise Refused("post-CALL OBSERVE does not carry the armed fixture")
    exposure = found.get("exposure")
    if not isinstance(exposure, dict) or exposure.get("state") not in {"ARMED", "FIRED", "EXPIRED"}:
        raise Refused("post-CALL exposure record malformed")
    if exposure["state"] != "FIRED":
        return None
    hashes = [exposure.get(key) for key in ["command_hash", "response_hash", "dropped_hash"]]
    if (exposure.get("outcome") != "UNCERTAIN" or exposure.get("operation") != "CALL" or
            exposure.get("session") != view["session"] or type(exposure.get("cell")) is not int or
            exposure["cell"] <= view["exposure"]["armed_high"] or
            not isinstance(exposure.get("claim"), dict) or
            not all(isinstance(value, str) and controller.HASH.fullmatch(value) for value in hashes) or
            type(exposure.get("dropped_bytes")) is not int or exposure["dropped_bytes"] <= 0):
        raise Refused("fired exposure is not an UNCERTAIN CALL above the armed high-water")
    return exposure


def classify(exposure, committed, call, reread):
    """Name what was observed; never a pass value."""
    rows = [row for row in reread.get("domains", []) if row.get("present")]
    visible = [row for row in rows if row.get("lstat_errno") == 0 and row.get("type") == "dir" and
               row.get("listing_matches") == 1]
    absent = rows and all(row.get("lstat_errno") == errno.ENOENT for row in rows)
    if exposure is None:
        return "fixture-not-fired"
    if committed and visible:
        return "dropped-reply-effect-committed-and-visible"
    if not committed and absent and call.get("mkdir_errno") != 0:
        return "dropped-before-mutation"
    return "inconsistent"


LOST_REPLY_LIMITS = [
    "the exposure names cell/claim/hashes, not the operation: the guest path walk issues "
    "GETATTR/LOOKUP CALLs before MKDIR, so only the committed source shows a MKDIR effect",
    "a guest kernel blocks the armed session after a dropped reply; m0 re-read errors are data",
    "the fixture record lives in daemon memory only; a crash loses it",
]


def lost_reply_record(output, plan_sha256, before, armed, after):
    """homelabfs-retained9p-lost-reply-correlation/1 for T9 (G10)."""
    output, plan, state = retained_run(output, plan_sha256)
    steps = completed_steps(output, state)
    require_steps(steps, ["lost-reply-call", "lost-reply-reread"])
    session = plan_identity(plan)["sessions"][0]
    pre = evidence_of(before, "pre-arm")
    if "lost_reply" in pre:
        raise Refused("a fixture was already armed before this ARM")
    view = armed_view(armed, before, pre, session)
    post = evidence_of(after, "post-CALL")
    check_same_binding(before, after, ("pre-arm", "post-CALL"))
    if after["sequence"] <= armed["sequence"]:
        raise Refused("post-CALL OBSERVE must follow the ARM")
    service = service_of(post, "post-CALL")
    check_service_identity(view["baseline"], service, "post-CALL")
    committed = advanced(view["baseline"], service, "post-CALL")
    exposure = fired_exposure(post, view)
    call, reread = steps["lost-reply-call"], steps["lost-reply-reread"]
    return {"version": LOST_REPLY_VERSION, "run_id": plan["run_id"], "acceptance": False,
            "classification": classify(exposure, committed, call["observation"], reread["observation"]),
            "fixture": view["fixture"], "session": session, "exposure": exposure,
            "committed_since_arm": committed,
            "guest": {"call": call, "reread": reread},
            "replies": {"pre_arm": entry_record(before), "armed": entry_record(armed),
                        "post_call": entry_record(after)},
            "limits": LOST_REPLY_LIMITS}


def load_snapshot(path, sha256, run_id, label):
    """One host_snapshot.py output pinned by digest, for this run and label."""
    if not isinstance(sha256, str) or not controller.HASH.fullmatch(sha256):
        raise Refused("exact snapshot digest required")
    value = controller.parse(controller.verified_bytes(path, sha256, SNAPSHOT_LIMIT))
    controller.closed(value, ["version", "run_id", "label", "path", "taken_utc", "parent",
                              "target", "mountpoint", "listing"])
    if value["version"] != SNAPSHOT_VERSION or value["run_id"] != run_id or value["label"] != label:
        raise Refused("host snapshot version/run/label differs: " + label)
    listing = value["listing"]
    controller.closed(listing, ["errno", "names", "truncated"])
    if listing["errno"] is None and (not isinstance(listing["names"], list) or listing["truncated"]):
        raise Refused("host snapshot listing incomplete: " + label)
    return {"sha256": sha256, "snapshot": value}


def after_loss_observation(steps, run_id):
    observation = steps["after-loss"]["observation"]
    attempts = observation.get("attempts")
    if (observation.get("marker") != "zq3-" + run_id + "-after-loss" or not isinstance(attempts, list) or
            len(attempts) != 3 or not all(isinstance(item, dict) and type(item.get("errno")) is int
                                          for item in attempts)):
        raise Refused("after-loss observation malformed")
    return observation


def names_of(snapshot):
    listing = snapshot["listing"]
    return None if listing["errno"] is not None else sorted(listing["names"])


def k04_checks(observation, snapshots):
    """Guest I/O refused, the marker never reached the host path, no raw change."""
    before, after = snapshots["before"]["snapshot"], snapshots["after"]["snapshot"]
    reference = snapshots.get("baseline", snapshots["before"])["snapshot"]
    after_names = names_of(after)
    raw_directory = after["mountpoint"] is False and after_names is not None
    return {"guest_io_refused": all(item["errno"] != 0 for item in observation["attempts"]),
            "marker_absent": after_names is None or observation["marker"] not in after_names,
            "raw_directory_reachable": raw_directory,
            "raw_directory_unchanged": not raw_directory or after_names == names_of(reference),
            "host_path_unchanged_during_attempt": names_of(before) == after_names}


def k04_record(output, plan_sha256, snapshot_args):
    """homelabfs-retained9p-k04-after-loss/1 for T11 (G11)."""
    output, plan, state = retained_run(output, plan_sha256)
    steps = completed_steps(output, state)
    require_steps(steps, ["after-loss"])
    observation = after_loss_observation(steps, plan["run_id"])
    snapshots = {label: load_snapshot(path, sha256, plan["run_id"], label)
                 for label, (path, sha256) in snapshot_args.items()}
    if len({item["snapshot"]["path"] for item in snapshots.values()}) != 1:
        raise Refused("host snapshots name different paths")
    if "baseline" in snapshots and (snapshots["baseline"]["snapshot"]["mountpoint"] is not False or
                                    names_of(snapshots["baseline"]["snapshot"]) is None):
        raise Refused("baseline snapshot must show the raw pre-run directory")
    checks = k04_checks(observation, snapshots)
    held = all(checks[key] for key in ["guest_io_refused", "marker_absent", "raw_directory_unchanged",
                                       "host_path_unchanged_during_attempt"])
    return {"version": K04_VERSION, "run_id": plan["run_id"], "acceptance": False,
            "k04_refusal_observed": held, "checks": checks,
            "guest": steps["after-loss"], "snapshots": snapshots,
            "limits": ["one host path only; another raw path the exporter could reach is not covered",
                       "replacement FUSE/exporter and guest RESUME refusals are recorded separately"]}


def quiesced(evidence):
    """C9 cut precondition visible in OBSERVE: no view lease, reservation or recall."""
    ledger = (evidence.get("controller") or {}).get("ledger") or {}
    counts = {key: ledger.get(key) for key in ["Leases", "Reservations", "Tickets", "Sources",
                                               "Recall", "Matches"]}
    if (counts["Leases"] != 0 or counts["Reservations"] != 0 or counts["Recall"] is not False or
            counts["Matches"] is not True):
        raise Refused("post-quiesce OBSERVE still shows view leases/reservations/recall; "
                      "RESTART not issued: " + json.dumps(counts, sort_keys=True))
    return counts


def command_preconditions(operation, evidence, state):
    done = [item["step"] for item in state.get("lifecycle", [])]
    if operation == "arm-lost-reply":
        if not state.get("syscall_batch_success") or done or "lost_reply" in evidence:
            raise Refused("ARM only once, after the released batch and before any lifecycle step")
    if operation == "restart":
        if done[-1:] != ["quiesce"]:
            raise Refused("RESTART only directly after the guest quiesce step")
        return quiesced(evidence)
    return None


def client_command(args):
    """Exact client argv from the latest OBSERVE; prints, never executes."""
    _, plan, state = retained_run(args.output, args.sha256)
    for path in [args.runtime_binary, args.control]:
        controller.check_output_path(path)
    observe = load_reply(args.observe, args.observe_sha256)
    reply = observe["reply"]
    evidence = reply.get("evidence") if reply.get("status") == "OBSERVED" else None
    if not isinstance(evidence, dict) or not evidence.get("runtime"):
        raise Refused("command requires an OBSERVED reply")
    last = load_reply(args.last, args.last_sha256) if args.last else observe
    if last["sequence"] < observe["sequence"]:
        raise Refused("last reply predates the OBSERVE")
    argv = [args.runtime_binary, "--control", args.control, "--operation", args.operation,
            "--runtime", evidence["runtime"], "--sequence", str(last["sequence"] + 1)]
    quiesce = None
    if args.operation != "observe":
        evidence = evidence_of(observe, "command")
        quiesce = command_preconditions(args.operation, evidence, state)
        argv += ["--cut", evidence["cut"], "--epoch", str(evidence["epoch"])]
    if args.operation == "arm-lost-reply":
        argv += ["--session", plan_identity(plan)["sessions"][0], "--lost-operation", "CALL"]
    return {"argv": argv, "operation": args.operation, "sequence": last["sequence"] + 1,
            "quiesce": quiesce, "execution_authorized": False, "acceptance": False}


def write_record(path, value):
    """Exclusive 0600 record (never overwritten), synced with its directory."""
    controller.check_output_path(path)
    path = Path(path)
    controller.check_output_parent(path)
    raw = controller.canonical(value)
    if len(raw) > RECORD_LIMIT:
        raise Refused("record exceeds the 512 KiB correlation bound")
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    controller.fsync_directory(path.parent)
    return controller.digest(raw)


def pinned(parser, name, required=True):
    parser.add_argument("--" + name, required=required)
    parser.add_argument("--" + name + "-sha256", required=required)


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="action", required=True)
    commands = {name: sub.add_parser(name) for name in ["case", "lost-reply", "k04", "command"]}
    for name, command in commands.items():
        command.add_argument("--output", required=True, help="controller run directory")
        command.add_argument("--sha256", required=True, help="exact batch plan digest")
        if name != "command":
            command.add_argument("--record", required=True, help="new record path")
    commands["case"].add_argument("--case", required=True, choices=controller.CASES)
    for name in ["baseline", "retained"]:
        pinned(commands["case"], name)
    for name in ["before", "armed", "after"]:
        pinned(commands["lost-reply"], name)
    for name in ["before", "after"]:
        pinned(commands["k04"], name)
    pinned(commands["k04"], "baseline", required=False)
    command = commands["command"]
    command.add_argument("--operation", required=True, choices=["observe", "arm-lost-reply", "restart"])
    command.add_argument("--runtime-binary", required=True)
    command.add_argument("--control", required=True)
    pinned(command, "observe")
    pinned(command, "last", required=False)
    return parser.parse_args(argv)


def build(args):
    """The record for one correlating action."""
    if args.action == "case":
        return case_record(args.output, args.sha256, args.case,
                           load_reply(args.baseline, args.baseline_sha256),
                           load_reply(args.retained, args.retained_sha256))
    if args.action == "lost-reply":
        return lost_reply_record(args.output, args.sha256,
                                 load_reply(args.before, args.before_sha256),
                                 load_reply(args.armed, args.armed_sha256),
                                 load_reply(args.after, args.after_sha256))
    snapshots = {"before": (args.before, args.before_sha256), "after": (args.after, args.after_sha256)}
    if args.baseline or args.baseline_sha256:
        snapshots["baseline"] = (args.baseline, args.baseline_sha256)
    return k04_record(args.output, args.sha256, snapshots)


def main(argv=None):
    args = arguments(argv)
    if args.action == "command":
        print(json.dumps(client_command(args), sort_keys=True))
        return 0
    value = build(args)
    sha256 = write_record(args.record, value)
    print(json.dumps({"record": args.record, "record_sha256": sha256, "version": value["version"],
                      "acceptance": False}, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError, Refused) as failure:
        print("refuse: " + type(failure).__name__ + ": " + str(failure), file=sys.stderr)
        sys.exit(1)
