#!/usr/bin/env python3
"""zq3 run-time plan fills (workstation only; no SSH, no lab contact).

Two plans of a filled zq3 run hold placeholders for values that exist only
during the run. Hand-editing them under the 120 s bootstrap's time pressure
is error-prone, so this tool writes the final plans from recorded evidence:

* `batch` fills the controller batch plan's Phase 8 values (F6/F7 of the run
  plan) from a READY bootstrap run directory as `bootstrap.py run` wrote it:
  the boot ID, provider nonce and admission manifest SHA256 from the
  `guest-ready` and `admission` journal results, cross-checked against the
  run's own `work/enroll-request.json`, and the m0/m1 mount IDs (first field)
  from the `mount-0`/`mount-1` results' `mountinfo` rows.
* `bootstrap` fills the bootstrap plan's `observer.ssh_sha256` (F1) from the
  Phase 4 fact file `sha256 -r /usr/bin/ssh` printed on f3.

Both refuse on a missing, ambiguous or mismatched value: a template or run
plan that differs from its pinned SHA256, a placeholder that is not exactly
where it is expected (already filled, moved) or one this tool does not know,
a bootstrap run that is not READY with every step `ok`, and evidence that
disagrees with itself. The filled plan must then pass the consuming tool's
own validation (`controller.py check`'s `plan_snapshot`, `bootstrap.py
check`'s `load_plan`) before it appears at the output path, which is never
overwritten. The plan's SHA256 is printed for the next pinned command.

Filling grants no authority: the run sheet's approvals still apply.
"""
import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent


def _sibling(name, filename):
    """Load a sibling module by path: `python3 -I` drops the script directory."""
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# The bootstrap driver brings the controller with it: the filled plans are
# checked by exactly the code that later consumes them.
bootstrap = _sibling("retained9p_bootstrap", "bootstrap.py")
controller = bootstrap.controller
digest, parse, read = controller.digest, controller.parse, controller.read

# Placeholders exactly as the zq3 fill wrote them into the templates.
BOOT, NONCE, MANIFEST = "PHASE8_BOOT_ID", "PHASE8_PROVIDER_NONCE", "PHASE8_ADMISSION_MANIFEST_SHA256"
MOUNT_IDS = ("PHASE8_M0_MOUNT_ID", "PHASE8_M1_MOUNT_ID")
SSH_SHA256 = "PHASE4_F3_USR_BIN_SSH_SHA256"
PLACEHOLDER = re.compile(r"PHASE[0-9]+_[A-Z0-9_]+")
MOUNT_ID = re.compile(r"[1-9][0-9]{0,9}\Z")
# `sha256 -r FILE` on FreeBSD: "HASH FILE", one space, one line.
SHA256_R = re.compile(r"([0-9a-f]{64}) (/[A-Za-z0-9_./-]+)\n\Z")
PLAN_BOUND = 64 << 10


class Refused(RuntimeError):
    """A value is missing, ambiguous or does not match: nothing was written."""


def require(ok, message):
    if not ok:
        raise Refused(message)


# --------------------------------------------------------------------------
# Templates


def load_template(path, expected_sha256):
    """The reviewed template, pinned by SHA256, with only known placeholders."""
    raw = read(path, PLAN_BOUND)
    require(digest(raw) == expected_sha256, "template bytes differ from the pinned SHA256")
    template = parse(raw)
    unknown = sorted(placeholders(template) - {BOOT, NONCE, MANIFEST, SSH_SHA256, *MOUNT_IDS})
    require(not unknown, "unknown run-time placeholder(s) in the template: " + ", ".join(unknown))
    return template


def placeholders(value):
    """Every placeholder-shaped string anywhere in a JSON value."""
    if isinstance(value, dict):
        return set().union(*(placeholders(item) for item in value.values()))
    if isinstance(value, list):
        return set().union(*(placeholders(item) for item in value))
    return {value} if isinstance(value, str) and PLACEHOLDER.search(value) else set()


def put(container, key, placeholder, value, where):
    """Fill one field that must hold exactly its placeholder (not yet filled)."""
    require(isinstance(container, dict) and container.get(key) == placeholder,
            "{}.{} must hold the placeholder {}".format(where, key, placeholder))
    container[key] = value


def filled_bytes(plan):
    """The template's own layout (indent 1, sorted keys) so a diff stays small."""
    require(not placeholders(plan), "placeholder(s) left after the fill: " + ", ".join(sorted(placeholders(plan))))
    return (json.dumps(plan, indent=1, sort_keys=True) + "\n").encode()


# --------------------------------------------------------------------------
# Bootstrap run evidence (bootstrap.py run's output directory)


def journal_details(run_dir, plan_sha256):
    """step -> result detail of a complete run: every step once, in order, ok."""
    journal = bootstrap.Journal.open(run_dir / "journal.jsonl")
    header, records = (journal.records or [{}])[0], journal.records[1:]
    require(header.get("kind") == "run" and header.get("version") == bootstrap.JOURNAL_VERSION,
            "journal does not start with a bootstrap run record")
    require(header.get("plan_sha256") == plan_sha256, "journal is not for the pinned bootstrap plan")
    intents = [r.get("step") for r in records if r.get("kind") == "intent"]
    results = [r for r in records if r.get("kind") == "result"]
    require(len(intents) + len(results) == len(records), "journal holds records of an unknown kind")
    require(intents == list(bootstrap.STEP_NAMES) and [r.get("step") for r in results] == intents,
            "journal is not one intent and one result per bootstrap step in order (incomplete run?)")
    failed = [r["step"] + "=" + str(r.get("outcome")) for r in results if r.get("outcome") != "ok"]
    require(not failed, "bootstrap steps did not end ok: " + ", ".join(failed))
    require(all(isinstance(r.get("detail"), dict) for r in results), "journal result without a detail")
    return header, {r["step"]: r["detail"] for r in results}


def run_plan(run_dir, plan_sha256):
    """The bootstrap plan bytes the run recorded, pinned and still valid."""
    raw = read(run_dir / "plan.json", PLAN_BOUND)
    require(digest(raw) == plan_sha256, "run's plan.json differs from the pinned bootstrap plan SHA256")
    plan = parse(raw)
    bootstrap.validate_plan(plan)
    return plan


def check_summary(run_dir):
    summary = controller.load(run_dir / "summary.json")
    require(summary.get("outcome") == "ready" and summary.get("stopped_at") is None,
            "bootstrap summary is not READY: outcome {!r}, stopped at {!r}".format(
                summary.get("outcome"), summary.get("stopped_at")))


def boot_facts(details):
    """Boot ID, provider nonce and manifest SHA256 of the measured admission."""
    ready, admission = details["guest-ready"], details["admission"]
    boot, nonce, manifest = ready.get("boot_id"), admission.get("provider_nonce"), admission.get("manifest_sha256")
    require(bootstrap.is_uuid(boot), "guest-ready boot ID is not a UUID")
    require(admission.get("boot_id") == boot, "admission boot ID differs from the guest-ready boot ID")
    require(bootstrap.is_uuid(nonce), "admission provider nonce is not a UUID")
    require(bootstrap.is_hash(manifest), "admission manifest SHA256 is not SHA256 hex")
    return {"boot_id": boot, "provider_nonce": nonce, "manifest_sha256": manifest}


def enroll_sessions(run_dir, detail, facts):
    """The run's enrollment request must carry the same boot, nonce and
    manifest; its mounts give each domain's (session, tag)."""
    raw = read(run_dir / "work" / "enroll-request.json", 512 << 10)
    require(digest(raw) == detail.get("sha256"), "work/enroll-request.json differs from its journaled SHA256")
    request = parse(raw)
    require(request.get("BootID") == facts["boot_id"] and request.get("ProviderNonce") == facts["provider_nonce"],
            "enroll request boot ID/provider nonce differ from the admission")
    require(bootstrap.observer_manifest(request.get("Observer")) == facts["manifest_sha256"],
            "enroll request Observer.ManifestHash differs from the admission manifest SHA256")
    mounts = request.get("Mounts")
    require(isinstance(mounts, list) and len(mounts) == 2 and all(isinstance(m, dict) for m in mounts),
            "enroll request must hold two mounts")
    return [(m.get("session"), m.get("device_tag")) for m in mounts]


def mount_id(detail, mount):
    """First mountinfo field of the one 9p row the guest reported for `mount`."""
    require((detail.get("tag"), detail.get("mountpoint")) == (mount["tag"], mount["mountpoint"]),
            "mount result {!r} is not the plan mount {!r}".format(detail, mount))
    line = detail.get("mountinfo")
    require(isinstance(line, str) and "\n" not in line, "mount result needs exactly one mountinfo row")
    left, separator, right = line.partition(" - ")
    fields, tail = left.split(" "), right.split(" ")
    require(separator and len(fields) >= 6 and len(tail) >= 2, "mountinfo row is malformed: " + repr(line))
    require(fields[4] == mount["mountpoint"] and tail[:2] == ["9p", mount["tag"]],
            "mountinfo row is not 9p {} on {}: {!r}".format(mount["tag"], mount["mountpoint"], line))
    require(MOUNT_ID.match(fields[0]) is not None, "mountinfo mount ID is not a positive decimal: " + repr(line))
    return int(fields[0])


def run_evidence(run_dir, plan_sha256):
    """Every Phase 8 value from one READY bootstrap run, cross-checked."""
    run_dir = Path(run_dir)
    header, details = journal_details(run_dir, plan_sha256)
    plan = run_plan(run_dir, plan_sha256)
    require(header.get("run_id") == plan["run_id"], "journal run ID differs from the run's plan")
    check_summary(run_dir)
    facts = boot_facts(details)
    sessions = enroll_sessions(run_dir, details["enroll-request"], facts)
    mounts = []
    for index, mount in enumerate(plan["mounts"]):
        session, tag = sessions[index]
        require(tag == mount["tag"], "enroll request mount {} tag {!r} is not {}".format(index, tag, mount["tag"]))
        mounts.append({"path": mount["mountpoint"], "tag": tag, "session": session,
                       "mount_id": mount_id(details["mount-{}".format(index)], mount)})
    require(mounts[0]["mount_id"] != mounts[1]["mount_id"], "m0 and m1 report the same mount ID")
    return dict(facts, run_id=plan["run_id"], mounts=mounts)


# --------------------------------------------------------------------------
# Fills


def fill_batch(template, facts):
    """The controller batch plan with every Phase 8 placeholder replaced."""
    plan = copy.deepcopy(template)
    require(plan.get("run_id") == facts["run_id"], "template run ID differs from the bootstrap run")
    put(plan, "installed_manifest_sha256", MANIFEST, facts["manifest_sha256"], "plan")
    requests = plan.get("requests")
    require(isinstance(requests, list) and requests, "template holds no requests")
    for number, request in enumerate(requests):
        where = "requests[{}]".format(number)
        put(request, "boot_id", BOOT, facts["boot_id"], where)
        put(request, "provider_nonce", NONCE, facts["provider_nonce"], where)
        put(request, "manifest_sha256", MANIFEST, facts["manifest_sha256"], where)
        mounts = request.get("mounts") if isinstance(request, dict) else None
        require(isinstance(mounts, list) and len(mounts) == 2, where + " must hold two mounts")
        for index, (mount, measured) in enumerate(zip(mounts, facts["mounts"])):
            require(isinstance(mount, dict) and [mount.get(k) for k in ("path", "tag", "session")] ==
                    [measured[k] for k in ("path", "tag", "session")],
                    "{}.mounts[{}] path/tag/session differ from the bootstrap run".format(where, index))
            put(mount, "mount_id", MOUNT_IDS[index], measured["mount_id"], "{}.mounts[{}]".format(where, index))
    return plan


def ssh_fact(path, ssh_path):
    """The f3 SHA256 of `ssh_path` from one `sha256 -r` line."""
    raw = read(path, 4096)
    match = SHA256_R.match(raw.decode("ascii", "replace"))
    require(match is not None, "fact file must be exactly one `sha256 -r` line: HASH PATH")
    require(match.group(2) == ssh_path, "fact file hashes {}, not the plan's observer.ssh {}".format(
        match.group(2), ssh_path))
    return match.group(1)


def fill_bootstrap(template, sha256):
    plan = copy.deepcopy(template)
    put(plan.get("observer"), "ssh_sha256", SSH_SHA256, sha256, "observer")
    return plan


# --------------------------------------------------------------------------
# Output


def publish(output, raw, check):
    """Write `raw` to a fresh `output` only after `check(path, sha256)` accepts
    the written bytes; an existing output is never replaced."""
    controller.check_output_path(output)
    output = Path(output)
    require(not os.path.lexists(output), "output exists: a filled plan is never overwritten")
    pending = output.with_name(output.name + ".next")
    fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        check(str(pending), digest(raw))
        os.link(pending, output)  # Fails instead of replacing a racing output.
    finally:
        os.unlink(pending)
        controller.fsync_directory(output.parent)
    return digest(raw)


def batch(args):
    template = load_template(args.template, args.template_sha256)
    facts = run_evidence(args.bootstrap_run, args.bootstrap_plan_sha256)
    raw = filled_bytes(fill_batch(template, facts))
    sha256 = publish(args.output, raw, lambda path, expected: controller.plan_snapshot(path, expected))
    return {"output": args.output, "sha256": sha256, "boot_id": facts["boot_id"],
            "provider_nonce": facts["provider_nonce"], "manifest_sha256": facts["manifest_sha256"],
            "mount_ids": [mount["mount_id"] for mount in facts["mounts"]]}


def bootstrap_fill(args):
    template = load_template(args.template, args.template_sha256)
    observer = template.get("observer") if isinstance(template.get("observer"), dict) else {}
    value = ssh_fact(args.ssh_fact, observer.get("ssh"))
    raw = filled_bytes(fill_bootstrap(template, value))
    sha256 = publish(args.output, raw, bootstrap.load_plan)
    return {"output": args.output, "sha256": sha256, "observer_ssh_sha256": value}


def arguments(argv):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="action", required=True)
    filling = sub.add_parser("batch", help="controller batch plan from a READY bootstrap run (F6/F7)")
    filling.add_argument("--bootstrap-run", required=True, help="bootstrap.py run --output directory")
    filling.add_argument("--bootstrap-plan-sha256", required=True)
    observing = sub.add_parser("bootstrap", help="bootstrap plan observer.ssh_sha256 from the Phase 4 fact (F1)")
    observing.add_argument("--ssh-fact", required=True, help="output of `sha256 -r /usr/bin/ssh` on f3")
    for command in (filling, observing):
        command.add_argument("--template", required=True)
        command.add_argument("--template-sha256", required=True)
        command.add_argument("--output", required=True)
    return parser.parse_args(argv)


def main(argv=None):
    args = arguments(argv)
    try:
        report = batch(args) if args.action == "batch" else bootstrap_fill(args)
    except (Refused, bootstrap.Refused, controller.Refused, OSError, ValueError) as failure:
        print("refused: " + str(failure), file=sys.stderr)
        return 2
    except (KeyError, TypeError, AttributeError) as failure:
        print("refused: malformed input: " + repr(failure), file=sys.stderr)
        return 2
    print(json.dumps(dict(report, execution_authorized=False), sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
