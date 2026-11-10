#!/usr/bin/env python3
"""Local tests of the zq3 G1 Phase 8 bootstrap orchestrator (bootstrap.py).

No SSH, network, lab, VM or key. Every boundary is simulated by `World`: it
plays f3 (guard records, runtime stdout, proposal, enrollment handoff), Rocky
(ping, admission, sysfs writes, mounts), hfs-runtime-prep and the operator
who runs the signer. The signed files are REAL signer output exported with
throwaway keys by internal/runtimeprep/bootstrap_fixture_test.go
(testdata/bootstrap/), so the binding checks run against the exact JSON the
Go signer emits. Its guest budget (reservations 1024, received 256, 512/512
MiB, regenerated in x54) must stay above the w54 guest floors; Python cannot
import those, so TestBootstrapFixtureAboveGuestFloor in the same Go file
checks the committed files against internal/runtimeprep/guestfloor.go.
"""
import base64
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
LAB = HERE.parents[2] / "docs" / "lab"
FIXTURE = HERE / "testdata" / "bootstrap"


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


bootstrap = module("bootstrap_under_test", "bootstrap.py")


def fixture(name):
    return (FIXTURE / name).read_bytes()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def compact(value):
    return json.dumps(value, separators=(",", ":"))


REQUEST = json.loads(fixture("enroll-request.json"))
BIND_REQUEST = json.loads(fixture("bind-request.json"))
AWAITING = json.loads(fixture("awaiting.json"))
BOOT, NONCE = REQUEST["BootID"], REQUEST["ProviderNonce"]
MANIFEST = bytes(REQUEST["Observer"]["ManifestHash"]).hex()
RELEASE = "6.1.190-hfs-xz3"
RUN = "/var/tmp/hfs-validation-xz3/zq3-r1"
OWNER = RUN + "/runtime/owner"
PROPOSAL_LINE = compact({"stage": "AWAITING_OPERATOR_ENROLLMENT", "proposal": OWNER + "/proposal.json",
                         "publication": None})
AWAITING_LINE = compact({"stage": "AWAITING_GUEST_BIND", "controller": AWAITING["controller"],
                         "provision_hash": AWAITING["provision_hash"], "publication": None})
READY_LINE = compact({"stage": "READY", "profile": "CONTROLLED_LOCAL", "domains": 2, "publication": None,
                      "control": {"socket": OWNER + "/lifecycle.sock", "runtime": BOOT, "cut": NONCE, "epoch": 1}})


def input_files(inputs):
    """Local pinned inputs; template and runtime config private like the bundle."""
    inputs.mkdir()
    config = {"BootstrapSeconds": 120, "OwnerDirectory": OWNER, "EnrollmentFile": RUN + "/runtime/handoff/enrollment.json",
              "HostKeys": ["not read by the orchestrator"]}
    files = {"ssh": b"pinned ssh stand-in", "prep": b"pinned hfs-runtime-prep stand-in",
             "host-known_hosts": (LAB / "f3-host-known_hosts").read_bytes(),
             "guest-known_hosts": (LAB / "rocky-known_hosts").read_bytes(),
             "template": fixture("request-template.json"), "config": json.dumps(config).encode()}
    for name, raw in files.items():
        (inputs / name).write_bytes(raw)
        (inputs / name).chmod(0o600)
    return files


def plan_fixture(root, change=None):
    """A complete plan whose observer and template match the real fixture."""
    inputs = root / "in"
    files = input_files(inputs)
    observer = REQUEST["Observer"]
    plan = {
        "version": bootstrap.VERSION, "run_id": "zq3-r1", "bootstrap_seconds": 120,
        "remote_sha256": sha(bootstrap.REMOTE.read_bytes()),
        "signer": "/usr/local/bin/hfs-retained9p-operator",
        "mounts": [{"tag": "hfs0", "mountpoint": "/var/tmp/hfs-zq3-zq3-r1/m0"},
                   {"tag": "hfs1", "mountpoint": "/var/tmp/hfs-zq3-zq3-r1/m1"}],
        "ssh": {"path": str(inputs / "ssh"), "sha256": sha(files["ssh"])},
        "host": {"address": "10.20.0.133", "alias": "f3", "user": "paul",
                 "known_hosts": str(inputs / "host-known_hosts"), "known_hosts_sha256": sha(files["host-known_hosts"]),
                 "doas": "/usr/local/bin/doas", "python": "/usr/local/bin/python3.12"},
        "guest": {"address": "10.20.0.123", "alias": "rocky", "user": "root",
                  "known_hosts": str(inputs / "guest-known_hosts"),
                  "known_hosts_sha256": sha(files["guest-known_hosts"]), "python": "/usr/bin/python3"},
        "guard": {"script": RUN + "/bin/runtime-guard.py", "script_sha256": "d" * 64, "run_dir": RUN + "/guard",
                  "vm_name": "hfs-zq3-r1", "disk_roots": [RUN, "/var/tmp/hfs-fdescfs-windows/zq3-r1"],
                  "skip_paths": [RUN + "/volume/mnt"]},
        "runtime": {"executable": RUN + "/bin/hfs-controlled-runtime", "sha256": "5" * 64,
                    "config": RUN + "/runtime/runtime-config.json", "owner_directory": OWNER,
                    "enrollment_file": RUN + "/runtime/handoff/enrollment.json"},
        "kernel": {"script": "/root/stage/rocky_kernel.py", "script_sha256": "e" * 64,
                   "plan": "/root/stage/kernel-plan.json", "plan_sha256": "f" * 64,
                   "state": "/root/hfs-zq3-zq3-r1-kernel-state", "release": RELEASE},
        "prep": {"path": str(inputs / "prep"), "sha256": sha(files["prep"]), "template": str(inputs / "template"),
                 "template_sha256": sha(files["template"]), "config": str(inputs / "config"),
                 "config_sha256": sha(files["config"])},
        "observer": {"ssh": observer["SSH"], "ssh_sha256": bytes(observer["SSHHash"]).hex(),
                     "known_hosts": observer["KnownHosts"],
                     "known_hosts_sha256": bytes(observer["KnownHostsHash"]).hex(),
                     "identity": observer["Identity"], "host": observer["Host"]},
    }
    if change:
        change(plan)
    raw = json.dumps(plan).encode()
    (root / "plan.json").write_bytes(raw)
    return root / "plan.json", sha(raw)


def reply(value, code=0):
    return bootstrap.Reply(code, (json.dumps(value) + "\n").encode())


class Guard:
    def __init__(self, world):
        self.world, self.pid = world, 4242

    def poll(self):
        return self.world.guard_code


class World:
    """Simulated f3, runtime, Rocky, hfs-runtime-prep, operator and clock."""

    def __init__(self, root):
        self.root, self.clock = root, 1000.0
        self.calls, self.prep_calls, self.notes = [], [], []
        self.stdout, self.pending, self.guard_files = [], [], {}
        self.launched, self.guard_code, self.ping_failures = False, None, 1
        self.overrides, self.tamper, self.extra, self.sign = {}, {}, {}, True
        self.prep_tamper, self.mounted = None, []
        self.stat_outcomes, self.stat_clocks, self.mount_clock = [], [], None

    # -- Boundaries factory (bootstrap.run calls it with the run's inputs)

    def boundaries(self, plan, inputs_dir, program):
        self.plan, self.inputs_dir, self.program = plan, inputs_dir, program
        return bootstrap.Boundaries(self, self, self, self, self.notify)

    # -- clock

    def now(self):
        return self.clock

    def sleep(self, seconds):
        self.clock += seconds

    # -- remote

    def guard_argv(self):
        return bootstrap.SshRemote(self.plan, self.inputs_dir, self.program).guard_argv()

    def call(self, target, action, args, payload, seconds):
        self.clock += 0.3
        options = dict(zip(args[::2], args[1::2]))
        self.calls.append((target, action, options, payload))
        handler = self.overrides.get(action) or getattr(self, "on_" + action.replace("-", "_"))
        result = handler(options, payload)
        if isinstance(result, bootstrap.Reply):
            return result
        return reply(dict(result, ok=True, action=action))

    def on_host_preflight(self, options, payload):
        return {"guard_sha256": options["--guard-sha256"], "runtime_sha256": options["--runtime-sha256"]}

    def on_host_poll(self, options, payload):
        if not self.launched:
            return {"guard_dir": False, "stdout": None, "files": {}}
        self.stdout += self.pending  # Records land one poll after their cause.
        self.pending = []
        return {"guard_dir": True, "stdout": "".join(line + "\n" for line in self.stdout),
                "files": self.guard_files}

    def on_host_proposal(self, options, payload):
        raw = fixture("proposal.json")
        return {"sha256": sha(raw), "bytes": len(raw), "base64": base64.b64encode(raw).decode()}

    def on_guest_ping(self, options, payload):
        if self.ping_failures:
            self.ping_failures -= 1
            return bootstrap.Reply(255, b"", b"ssh: connect to host 10.20.0.123 port 22: Connection refused")
        return {"release": RELEASE, "boot_id": BOOT}

    def on_guest_admission(self, options, payload):
        admission = {"step": "admission", "manifest_sha256": MANIFEST, "boot_id": BOOT, "modules": ["virtio_net"]}
        return {"admission": admission, "manifest_sha256": MANIFEST, "boot_id": BOOT, "provider_nonce": NONCE,
                "devices": {"hfs0": "virtio2", "hfs1": "virtio3"}}

    def on_guest_write(self, options, payload):
        return {"target": options["--target"], "bytes": len(payload), "sha256": sha(payload)}

    def on_host_install(self, options, payload):
        self.pending.append(AWAITING_LINE)
        return {"path": options["--path"], "sha256": sha(payload)}

    def on_guest_mount(self, options, payload):
        self.mounted.append(options["--tag"])
        self.mount_clock = self.clock  # The usable gate counts from the later mount.
        if len(self.mounted) == 2:
            self.pending.append(READY_LINE)
        return {"tag": options["--tag"], "mountpoint": options["--mountpoint"], "mountinfo": "row"}

    def on_guest_stat(self, options, payload):
        """Both roots answer the lstat; `stat_outcomes` scripts a failing probe
        (one list of per-root changes per guest-stat call, in call order)."""
        self.stat_clocks.append(self.clock)
        changes = self.stat_outcomes.pop(0) if self.stat_outcomes else [{}, {}]
        roots = []
        for tag, mountpoint, change in zip(options["--tags"].split(","), options["--mountpoints"].split(","), changes):
            root = {"tag": tag, "mountpoint": mountpoint, "mountinfo": ["row"], "outcome": "ok",
                    "stat": {"mode": 0o40755, "uid": 0, "gid": 0, "ino": 1, "dev": 55}}
            roots.append(dict(root, **change))
        return {"boot_id": options["--boot-id"], "roots": roots}

    # -- guard launcher

    def start(self, argv, stdout_path, stderr_path):
        for path in (stdout_path, stderr_path):
            open(path, "xb").close()
        self.launched, self.guard_argv_seen = True, argv
        self.stdout.append(PROPOSAL_LINE)
        return Guard(self)

    # -- hfs-runtime-prep stand-in (writes what the Go tool would)

    def run(self, argv, seconds):
        self.clock += 0.1
        options = dict(zip(argv[2::2], argv[3::2]))
        self.prep_calls.append((argv[1], options))
        request = self.enroll_request(options) if argv[1] == "enroll-request" else self.bind_request(options)
        if self.prep_tamper:
            self.prep_tamper(argv[1], request)
        raw = json.dumps(request).encode()
        fd = os.open(options["--output"], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw)
        return bootstrap.Reply(0, json.dumps({"output": options["--output"], "sha256": sha(raw)}, indent=2).encode())

    def enroll_request(self, options):
        observer = json.loads(Path(options["--observer"]).read_text())
        request = json.loads(fixture("enroll-request.json"))
        request.update(Proposal=options["--proposal"], BootID=options["--boot-id"],
                       ProviderNonce=options["--provider-nonce"])
        for mount, name in zip(request["Mounts"], options["--device-names"].split(",")):
            mount["device_name"] = name
        request["Observer"] = {"SSH": observer["SSH"], "KnownHosts": observer["KnownHosts"],
                               "Identity": observer["Identity"],
                               "SSHHash": list(bytes.fromhex(observer["SSHSHA256"])),
                               "KnownHostsHash": list(bytes.fromhex(observer["KnownHostsSHA256"])),
                               "ManifestHash": list(bytes.fromhex(observer["ManifestSHA256"])),
                               "Host": observer["Host"]}
        return request

    def bind_request(self, options):
        awaiting = json.loads(Path(options["--awaiting"]).read_text())
        return dict(BIND_REQUEST, Provision=options["--provision"], Controller=awaiting["controller"])

    # -- operator (runs the signer when asked; never part of the orchestrator)

    def notify(self, record):
        self.notes.append(record)
        if record.get("operator_action") and self.sign:
            self.operate(shlex.split(record["command"]))

    def operate(self, command):
        verb, directory = command[1], Path(command[5])
        names = ["enrollment.json", "provision.json"] if verb == "enroll" else ["bind.json"]
        directory.mkdir(mode=0o700)
        for name in names + list(self.extra):
            raw = self.tamper.get(name, lambda value: value)(fixture(name) if name in names else self.extra[name])
            fd = os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, "wb") as stream:
                stream.write(raw)

    # -- views

    def actions(self):
        return [action for _, action, _, _ in self.calls]

    def mutating(self):
        return [(action, options.get("--target") or options.get("--tag")) for _, action, options, _ in self.calls
                if action in ("guest-admission", "guest-write", "host-install", "guest-mount")]


def records(output):
    return [json.loads(line) for line in (output / "journal.jsonl").read_text().splitlines()]


def outcomes(output):
    return {r["step"]: r["outcome"] for r in records(output) if r["kind"] == "result"}


def edit_json(name, change):
    def tamper(raw):
        value = json.loads(raw)
        change(value)
        return json.dumps(value).encode()
    return tamper


class Case(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.output = self.root / "out"
        self.world = World(self.root)

    def run_plan(self, change=None):
        plan, digest = plan_fixture(self.root, change)
        return bootstrap.run(plan, digest, self.output, boundaries=self.world.boundaries)

    def assert_stopped(self, step, outcome, text=""):
        found = outcomes(self.output)
        self.assertEqual(list(found)[-1], step, found)
        self.assertEqual(found[step], outcome)
        self.assertTrue(all(value == "ok" for name, value in found.items() if name != step), found)
        detail = [r for r in records(self.output) if r.get("step") == step and r["kind"] == "result"][0]["detail"]
        self.assertIn(text, detail.get("error", ""))


class SequenceTests(Case):
    def test_complete_bootstrap_follows_the_code_order(self):
        opened = []
        real_open = os.open

        def watch(path, *args, **kwargs):
            opened.append(str(path))
            return real_open(path, *args, **kwargs)
        with mock.patch("os.open", side_effect=watch):
            self.assertEqual(self.run_plan(), 0)
        self.assertEqual(list(outcomes(self.output)), list(bootstrap.STEP_NAMES))
        self.assertEqual(set(outcomes(self.output).values()), {"ok"})
        kinds = [(r["kind"], r["step"]) for r in records(self.output)[1:]]
        self.assertEqual(kinds, [(k, s) for s in bootstrap.STEP_NAMES for k in ("intent", "result")])
        # Each mutating action exactly once, in the driver's order.
        self.assertEqual(self.world.mutating(), [("guest-admission", None), ("guest-write", "provision"),
                                                 ("host-install", None), ("guest-write", "bind"),
                                                 ("guest-mount", "hfs0"), ("guest-mount", "hfs1")])
        payloads = {(a, o.get("--target")): p for _, a, o, p in self.world.calls if p}
        self.assertEqual(payloads[("guest-write", "provision")], fixture("provision.json"))
        self.assertEqual(payloads[("host-install", None)], fixture("enrollment.json"))
        self.assertEqual(payloads[("guest-write", "bind")], fixture("bind.json"))
        # The operator key path is never opened; the signer is only announced.
        self.assertNotIn(REQUEST["OperatorKey"], opened)
        signs = [n["operator_action"] for n in self.world.notes if "operator_action" in n]
        self.assertEqual(signs, ["sign-enroll", "sign-bind"])
        summary = json.loads((self.output / "summary.json").read_text())
        self.assertEqual((summary["outcome"], summary["acceptance"]), ("ready", False))
        ready = [r for r in records(self.output) if r.get("step") == "ready" and r["kind"] == "result"][0]
        self.assertLess(ready["detail"]["seconds_from_launch"], 120)
        # The usable gate (k64) is read-only: no further mutating action above.
        usable = [r for r in records(self.output) if r.get("step") == "usable" and r["kind"] == "result"][0]
        self.assertEqual([root["outcome"] for root in usable["detail"]["final"]], ["ok", "ok"])
        self.assertGreaterEqual(usable["detail"]["final_seconds_after_mount"], bootstrap.USABLE_SETTLE_SECONDS)

    def test_enrollment_request_carries_measured_manifest_and_devices(self):
        self.assertEqual(self.run_plan(), 0)
        request = json.loads((self.output / "work" / "enroll-request.json").read_text())
        self.assertEqual(bytes(request["Observer"]["ManifestHash"]).hex(), MANIFEST)
        self.assertEqual((request["BootID"], request["ProviderNonce"]), (BOOT, NONCE))
        self.assertEqual([m["device_name"] for m in request["Mounts"]], ["virtio2", "virtio3"])
        self.assertEqual(request["Proposal"], str(self.output / "work" / "proposal.json"))
        self.assertEqual((self.output / "work" / "proposal.json").read_bytes(), fixture("proposal.json"))
        self.assertEqual((self.output / "work" / "awaiting-guest-bind.json").read_text(), AWAITING_LINE)

    def test_guard_runs_detached_with_the_plan_argv(self):
        self.assertEqual(self.run_plan(), 0)
        argv = self.world.guard_argv_seen
        self.assertIn("-oServerAliveInterval=15", argv)
        self.assertEqual(argv[-1], " ".join([
            "/usr/local/bin/doas", "-n", "/usr/local/bin/python3.12", "-I", "-B", RUN + "/bin/runtime-guard.py",
            "--run-dir", RUN + "/guard", "--runtime-sha256", "5" * 64, "--vm-name", "hfs-zq3-r1",
            "--disk-root", RUN, "--disk-root", "/var/tmp/hfs-fdescfs-windows/zq3-r1",
            "--skip-path", RUN + "/volume/mnt", "--", RUN + "/bin/hfs-controlled-runtime",
            "--config", RUN + "/runtime/runtime-config.json", "--disposable-validation"]))


class FailureTests(Case):
    def test_guard_refusal_stops_before_any_guest_contact(self):
        def refuse(options, payload):
            self.world.guard_code = 1
            return {"guard_dir": True, "stdout": "", "files": {"receipt.json": json.dumps(
                {"refused": "a VM context exists before launch (Rocky must be stopped)", "breach": None,
                 "runtime_exit": None})}}
        self.world.overrides["host-poll"] = refuse
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("proposal", "failed", "a VM context exists before launch")
        self.assertFalse([a for a in self.world.actions() if a.startswith("guest-")])

    def test_guard_transport_exit_without_records_fails(self):
        self.world.overrides["host-poll"] = lambda o, p: {"guard_dir": False, "stdout": None, "files": {}}
        self.world.guard_code = 1
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("proposal", "failed", "guard transport exited with 1")
        # The evidence must not claim the guard stopped: after a lost SSH
        # connection it keeps supervising the runtime on f3.
        self.assert_stopped("proposal", "failed", "KEEP RUNNING on f3")
        summary = json.loads((self.output / "summary.json").read_text())
        self.assertIn("reconnect to f3", summary["guard_transport_note"])

    def test_breach_during_bootstrap_fails(self):
        self.world.guard_files = {"breach.json": '{"found": [["vm-context", "rocky"]]}'}
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("proposal", "failed", "runtime guard breach")

    def test_timeout_mid_step_is_uncertain_and_stops_dependents(self):
        self.world.overrides["guest-write"] = lambda o, p: bootstrap.Reply(None, b"", b"", True, "deadline")
        self.assertEqual(self.run_plan(), 3)
        self.assert_stopped("provision", "uncertain", "remote state unknown")
        self.assertEqual(self.world.mutating(), [("guest-admission", None), ("guest-write", "provision")])
        self.assertNotIn("host-install", self.world.actions())

    def test_uncertain_step_is_never_replayed(self):
        self.world.overrides["guest-write"] = lambda o, p: bootstrap.Reply(None, b"", b"", True, "deadline")
        self.assertEqual(self.run_plan(), 3)
        self.assertEqual(bootstrap.status(self.output)["uncertain"], ["provision"])
        calls = len(self.world.calls)
        plan, digest = self.root / "plan.json", sha((self.root / "plan.json").read_bytes())
        with self.assertRaisesRegex(bootstrap.Refused, "never reused"):
            bootstrap.run(plan, digest, self.output, boundaries=self.world.boundaries)
        self.assertEqual(len(self.world.calls), calls)
        journal = bootstrap.Journal.open(self.output / "journal.jsonl")
        for step in ("provision", "enrollment-handoff"):
            with self.assertRaisesRegex(bootstrap.Refused, "run stopped at step provision"):
                journal.begin(step)

    def test_remote_exit_1_is_uncertain(self):
        self.world.overrides["guest-mount"] = lambda o, p: reply(
            {"ok": False, "uncertain": True, "error": "mount did not return"}, 1)
        self.assertEqual(self.run_plan(), 3)
        self.assert_stopped("mount-0", "uncertain", "mount did not return")
        self.assertEqual(self.world.mutating()[-1], ("guest-mount", "hfs0"))

    def test_remote_known_refusal_is_failed_not_uncertain(self):
        self.world.overrides["guest-admission"] = lambda o, p: reply(
            {"ok": False, "changed": False, "error": "admission refused before change: udev"}, 2)
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("admission", "failed", "udev")
        self.assertEqual(self.world.prep_calls, [])

    def test_loader_rejection_is_failed(self):
        self.world.overrides["guest-write"] = lambda o, p: bootstrap.Reply(97, b"", b"")
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("provision", "failed", "loader rejected")

    def test_wrong_manifest_hash_from_admission_fails(self):
        def admission(options, payload):
            result = self.world.on_guest_admission(options, payload)
            result["manifest_sha256"] = "0" * 64
            return result
        self.world.overrides["guest-admission"] = admission
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("admission", "failed", "manifest SHA256 readback differs")
        self.assertEqual(self.world.prep_calls, [])

    def test_wrong_kernel_or_changed_boot_fails(self):
        self.world.overrides["guest-ping"] = lambda o, p: {"release": "5.14.0-687.66.1.el9_8.x86_64", "boot_id": BOOT}
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("guest-ready", "failed", "not the one-shot test kernel")
        self.assertNotIn("guest-admission", self.world.actions())

    def test_changed_boot_during_admission_fails(self):
        def admission(options, payload):
            result = self.world.on_guest_admission(options, payload)
            result["boot_id"] = NONCE
            return result
        self.world.overrides["guest-admission"] = admission
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("admission", "failed", "boot ID changed")

    def test_prep_output_without_the_measured_manifest_fails(self):
        def tamper(command, request):
            if command == "enroll-request":
                request["Observer"]["ManifestHash"] = [0] * 32
        self.world.prep_tamper = tamper
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("enroll-request", "failed", "measured")
        self.assertFalse([n for n in self.world.notes if "operator_action" in n])

    def test_bind_request_mismatch_fails_before_signing(self):
        def tamper(command, request):
            if command == "bind-request":
                request["Controller"] = BOOT
        self.world.prep_tamper = tamper
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("bind-request", "failed", "runtime controller")
        self.assertEqual([n["operator_action"] for n in self.world.notes if "operator_action" in n], ["sign-enroll"])
        self.assertNotIn(("guest-write", "bind"), self.world.mutating())

    def test_admission_is_not_dispatched_without_time_for_udev_settle(self):
        def late(options, payload):
            self.world.clock += 85  # The guest answered with < 40 s left.
            return {"release": RELEASE, "boot_id": BOOT}
        self.world.ping_failures, self.world.overrides["guest-ping"] = 0, late
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("admission", "failed", "needs 40 s")
        self.assertNotIn("guest-admission", self.world.actions())

    def test_prep_pin_change_fails(self):
        def run(argv, seconds):
            raise AssertionError("a changed hfs-runtime-prep must not run")
        self.world.run = run
        plan, digest = plan_fixture(self.root)
        original = self.world.boundaries

        def boundaries(plan_value, inputs_dir, program):
            Path(plan_value["prep"]["path"]).write_bytes(b"replaced")
            return original(plan_value, inputs_dir, program)
        self.assertEqual(bootstrap.run(plan, digest, self.output, boundaries=boundaries), 1)
        self.assert_stopped("enroll-request", "failed", "pin")


EACCES = {"outcome": "errno", "errno": 13, "error": "Permission denied"}


class UsableTests(Case):
    """The post-READY usability gate (k64), against zq3-r7's failure shape:
    READY with every earlier step ok, then the mount roots refuse root."""

    def usable(self):
        return [r for r in records(self.output) if r.get("step") == "usable" and r["kind"] == "result"][0]["detail"]

    def assert_preserved(self):
        """Nothing was retried, remounted or written after READY."""
        self.assertEqual(self.world.mutating()[-2:], [("guest-mount", "hfs0"), ("guest-mount", "hfs1")])
        self.assertEqual(len(self.world.mutating()), 6)
        summary = json.loads((self.output / "summary.json").read_text())
        self.assertEqual((summary["outcome"], summary["stopped_at"]), ("failed", "usable"))
        self.assertIn("never remount or retry", summary["usable_note"])

    def test_success_probes_twice_and_waits_past_the_query_timeout(self):
        self.assertEqual(self.run_plan(), 0)
        self.assertEqual(self.world.actions().count("guest-stat"), 2)
        final = self.world.stat_clocks[-1] - self.world.mount_clock
        self.assertGreaterEqual(final, bootstrap.USABLE_SETTLE_SECONDS)
        self.assertGreater(bootstrap.USABLE_SETTLE_SECONDS, bootstrap.GUEST_QUERY_SECONDS)
        self.assertLessEqual(self.world.clock - self.world.mount_clock,
                             bootstrap.USABLE_SETTLE_SECONDS + bootstrap.USABLE_PROBE_SECONDS)
        options = [o for _, a, o, _ in self.world.calls if a == "guest-stat"][0]
        self.assertEqual(options, {"--tags": "hfs0,hfs1", "--boot-id": BOOT, "--python": "/usr/bin/python3",
                                   "--mountpoints": "/var/tmp/hfs-zq3-zq3-r1/m0,/var/tmp/hfs-zq3-zq3-r1/m1",
                                   "--seconds": str(bootstrap.STAT_SECONDS)})

    def test_eacces_on_the_final_root_stat_fails_with_the_errno(self):
        self.world.stat_outcomes = [[{}, {}], [EACCES, EACCES]]
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("usable", "failed", "final probe: mount root unusable: hfs0")
        self.assert_stopped("usable", "failed", "errno 13 (EACCES, Permission denied)")
        detail = self.usable()
        self.assertEqual((detail["errno"], detail["probe"]), (13, "final"))
        self.assertEqual([root["outcome"] for root in detail["roots"]], ["errno", "errno"])
        self.assertGreaterEqual(self.world.stat_clocks[-1] - self.world.mount_clock,
                                bootstrap.USABLE_SETTLE_SECONDS)
        self.assert_preserved()

    def test_eacces_on_the_first_probe_fails_without_waiting(self):
        self.world.stat_outcomes = [[{}, EACCES]]
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("usable", "failed", "first probe: mount root unusable: hfs1")
        self.assertEqual((self.usable()["errno"], self.world.actions().count("guest-stat")), (13, 1))
        self.assertLess(self.world.clock - self.world.mount_clock, bootstrap.USABLE_SETTLE_SECONDS)
        self.assert_preserved()

    def test_lstat_timeout_fails(self):
        self.world.stat_outcomes = [[{}, {}], [{}, {"outcome": "timeout", "seconds": 8}]]
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("usable", "failed", "hfs1 /var/tmp/hfs-zq3-zq3-r1/m1: lstat did not return within 8 s")
        self.assertIsNone(self.usable()["errno"])
        self.assert_preserved()

    def test_probe_transport_timeout_fails_not_uncertain(self):
        self.world.overrides["guest-stat"] = lambda o, p: bootstrap.Reply(None, b"", b"", True, "deadline")
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("usable", "failed", "guest-stat: transport deadline")
        detail = self.usable()
        self.assertEqual((detail["probe"], detail["roots"], detail["errno"]), ("first", None, None))
        self.assert_preserved()

    def test_usable_deadline_failure_names_its_probe(self):
        def stall(seconds):
            self.world.clock += seconds
            if self.world.stat_clocks:  # During the settle: overrun the gate's own bound.
                self.world.clock += 100
        self.world.sleep = stall
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("usable", "failed", "usable gate deadline")
        detail = self.usable()
        self.assertEqual((detail["probe"], detail["roots"], detail["errno"]), ("settle", None, None))

    def test_lost_mount_or_missing_root_fails(self):
        for outcomes, text in [([[{}, {"outcome": "not-mounted"}]], "no longer exactly one 9p mount"),
                               ([[{}, {"tag": "hfs9"}]], "did not report exactly the plan's mount roots")]:
            with self.subTest(text=text):
                self.setUp()
                self.world.stat_outcomes = outcomes
                self.assertEqual(self.run_plan(), 1)
                self.assert_stopped("usable", "failed", text)
                self.assert_preserved()

    def test_runtime_exit_during_the_settle_fails(self):
        def stat(options, payload):
            self.world.guard_files = {"receipt.json": json.dumps({"refused": None, "breach": None,
                                                                  "runtime_exit": 1})}
            return self.world.on_guest_stat(options, payload)
        self.world.overrides["guest-stat"] = stat
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("usable", "failed", "runtime guard finished")
        self.assertEqual(self.world.actions().count("guest-stat"), 1)
        self.assertEqual(self.usable()["probe"], "settle")

    def test_late_ready_still_gets_its_final_probe(self):
        original = self.world.on_host_poll

        def slow(options, payload):
            if len(self.world.mounted) == 2 and not self.world.stat_clocks:
                self.world.clock += 50  # READY seen well after the settle point.
            return original(options, payload)
        self.world.overrides["host-poll"] = slow
        self.assertEqual(self.run_plan(), 0)
        self.assertEqual(self.world.actions().count("guest-stat"), 2)


class SignerTests(Case):
    def test_signed_manifest_hash_mismatch_fails_before_provision(self):
        self.world.tamper["enrollment.json"] = edit_json("enrollment.json", lambda v: v["Observer"].update(
            ManifestHash=[1] * 32))
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-enrollment", "failed", "ManifestHash is not the measured manifest")
        self.assertEqual(self.world.mutating(), [("guest-admission", None)])

    def test_provision_file_mismatch_fails(self):
        self.world.tamper["provision.json"] = lambda raw: raw.replace(BOOT.encode(), NONCE.encode())
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-enrollment", "failed", "different provision than provision.json")

    def test_signed_proposal_change_fails(self):
        def change(value):
            value["Authorization"]["Enrollment"]["PageSize"] += 1
        self.world.tamper["enrollment.json"] = edit_json("enrollment.json", change)
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-enrollment", "failed", "exact proposal")

    def test_unexpected_signer_file_fails(self):
        self.world.extra = {"operator.pem": b"not for the orchestrator"}
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-enrollment", "failed", "unexpected files operator.pem")

    def test_bind_controller_mismatch_fails_before_bind_write(self):
        self.world.tamper["bind.json"] = edit_json("bind.json", lambda v: v["payload"].update(controller=BOOT))
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-bind", "failed", "bind payload")
        self.assertNotIn(("guest-write", "bind"), self.world.mutating())

    def test_provision_local_mounts_must_name_the_measured_devices(self):
        provision = fixture("provision.json").replace(b'"device_name":"virtio3"', b'"device_name":"virtio9"')
        self.assertNotEqual(provision, fixture("provision.json"))
        self.world.tamper["provision.json"] = lambda raw: provision
        # A consistent signer output (enrollment embeds the same provision):
        # only the device binding is wrong.
        self.world.tamper["enrollment.json"] = edit_json("enrollment.json", lambda v: v.update(
            Provision=base64.b64encode(provision).decode()))
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-enrollment", "failed", "provision local mounts")
        detail = [r for r in records(self.output) if r.get("step") == "signed-enrollment"][-1]["detail"]["error"]
        self.assertNotIn("different provision", detail)
        self.assertEqual(self.world.mutating(), [("guest-admission", None)])

    def test_deadline_during_signer_wait_stops_without_provision(self):
        self.world.sign = False
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("signed-enrollment", "failed", "bootstrap deadline (120 s from launch) passed")
        self.assertEqual(self.world.mutating(), [("guest-admission", None)])
        self.assertLessEqual(self.world.clock - 1000.0, 122)

    def test_no_mutating_dispatch_close_to_the_deadline(self):
        original = self.world.notify

        def late(record):
            original(record)
            if record.get("operator_action") == "sign-enroll":
                self.world.clock += 117  # The operator signed with ~2 s left.
        self.world.notify = late
        self.assertEqual(self.run_plan(), 1)
        found = outcomes(self.output)
        self.assertNotIn(("guest-write", "provision"), self.world.mutating())
        self.assertEqual(found[list(found)[-1]], "failed")


class OrderTests(Case):
    def test_journal_refuses_out_of_order_steps(self):
        self.output.mkdir()
        journal = bootstrap.Journal.create(self.output / "journal.jsonl", {"plan_sha256": "0" * 64})
        with self.assertRaisesRegex(bootstrap.Refused, "step order violation: launch before host-preflight"):
            journal.begin("launch")
        with self.assertRaisesRegex(bootstrap.Refused, "without an open intent"):
            journal.finish("host-preflight", "ok", {})
        journal.begin("host-preflight")
        with self.assertRaisesRegex(bootstrap.Refused, "intent without a result"):
            journal.begin("host-preflight")

    def test_killed_orchestrator_leaves_the_step_uncertain(self):
        self.output.mkdir()
        journal = bootstrap.Journal.create(self.output / "journal.jsonl", {"plan_sha256": "0" * 64})
        journal.begin("host-preflight")
        journal.finish("host-preflight", "ok", {})
        journal.begin("launch")  # ... and the process died here.
        self.assertEqual(bootstrap.status(self.output)["uncertain"], ["launch"])
        with self.assertRaisesRegex(bootstrap.Refused, "launch has an intent without a result"):
            bootstrap.Journal.open(self.output / "journal.jsonl").begin("proposal")

    def test_runtime_bind_stage_before_handoff_is_a_violation(self):
        original = self.world.start

        def early(argv, stdout_path, stderr_path):
            guard = original(argv, stdout_path, stderr_path)
            self.world.stdout.append(AWAITING_LINE)
            return guard
        self.world.start = early
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("proposal", "failed", "AWAITING_GUEST_BIND before proposal completed")
        self.assertFalse([a for a in self.world.actions() if a.startswith("guest-")])

    def test_ready_before_mounts_is_a_violation(self):
        original = self.world.on_host_install

        def install(options, payload):
            result = original(options, payload)
            self.world.pending.append(READY_LINE)
            return result
        self.world.overrides["host-install"] = install
        self.assertEqual(self.run_plan(), 1)
        self.assert_stopped("awaiting-bind", "failed", "READY before awaiting-bind completed")
        self.assertNotIn(("guest-write", "bind"), self.world.mutating())

    def test_unknown_or_published_stage_is_refused(self):
        for line, text in [(compact({"stage": "READY", "publication": None}), "expected AWAITING_OPERATOR"),
                           (compact({"stage": "AWAITING_OPERATOR_ENROLLMENT", "proposal": "x",
                                     "publication": {"name": "x"}}), "claims a publication")]:
            with self.subTest(text=text), self.assertRaisesRegex(bootstrap.StepFailed, text):
                bootstrap.stage_records(line + "\n")
        self.assertEqual(bootstrap.stage_records(PROPOSAL_LINE + "\n" + AWAITING_LINE[:10]),
                         [(json.loads(PROPOSAL_LINE), PROPOSAL_LINE)])


class PlanTests(Case):
    def refuses(self, change, text):
        plan, digest = plan_fixture(self.root, change)
        with mock.patch.object(bootstrap.subprocess, "Popen") as spawn:
            with self.assertRaisesRegex((bootstrap.Refused, bootstrap.controller.Refused), text):
                bootstrap.run(plan, digest, self.output, boundaries=self.world.boundaries)
            spawn.assert_not_called()
        self.assertFalse(self.output.exists())
        self.assertEqual(self.world.calls, [])

    def test_plan_refusals_happen_before_any_effect(self):
        cases = [
            (lambda p: p.update(extra=1), "closed keys"),
            (lambda p: p.update(bootstrap_seconds=121), "bootstrap_seconds"),
            # Below 60 s admission could only fail after the one-shot boot.
            (lambda p: p.update(bootstrap_seconds=59), "bootstrap_seconds 60..120"),
            (lambda p: p["host"].update(address="192.168.1.133"), "retired"),
            (lambda p: p["guest"].update(user="paul"), "target must be exactly"),
            (lambda p: p["guard"].update(run_dir="/var/tmp/$HOME/guard"), "safe ASCII"),
            (lambda p: p["kernel"].update(script="/root/stage/../x.py"), "safe ASCII"),
            (lambda p: p["mounts"].reverse(), "mount 0 must be hfs0"),
            (lambda p: p["observer"].update(host="10.20.0.124"), "observer host"),
            (lambda p: p["runtime"].update(sha256="5" * 63), "SHA256 hex"),
            (lambda p: p.update(remote_sha256="0" * 64), "pinned input changed"),
            (lambda p: p["prep"].update(sha256="0" * 64), "pinned input changed"),
            (lambda p: p.update(bootstrap_seconds=90), "BootstrapSeconds"),
            (lambda p: p["runtime"].update(owner_directory=RUN + "/elsewhere"), "OwnerDirectory"),
            (lambda p: Path(p["prep"]["template"]).chmod(0o640), "private single-link"),
        ]
        for change, text in cases:
            with self.subTest(text=text):
                root = Path(tempfile.mkdtemp(dir=self.root))
                self.root, saved = root, self.root
                try:
                    self.refuses(change, text)
                finally:
                    self.root = saved

    def test_plan_pin_and_input_pins(self):
        plan, digest = plan_fixture(self.root)
        with self.assertRaisesRegex(bootstrap.Refused, "pinned SHA256"):
            bootstrap.load_plan(plan, "0" * 64)
        (self.root / "in" / "guest-known_hosts").write_bytes(b"changed\n")
        with self.assertRaises(bootstrap.controller.Refused):
            bootstrap.load_plan(plan, digest)

    def test_template_tags_must_match_mounts(self):
        plan, digest = plan_fixture(self.root)
        template = json.loads(fixture("request-template.json"))
        template["Mounts"].reverse()
        raw = json.dumps(template).encode()
        (self.root / "in" / "template").write_bytes(raw)
        value = json.loads((self.root / "plan.json").read_text())
        value["prep"]["template_sha256"] = sha(raw)
        raw_plan = json.dumps(value).encode()
        plan.write_bytes(raw_plan)
        with self.assertRaisesRegex(bootstrap.Refused, "device tags differ"):
            bootstrap.load_plan(plan, sha(raw_plan))

    def test_check_command_prints_the_plan_hash(self):
        plan, digest = plan_fixture(self.root)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(bootstrap.main(["check", "--plan", str(plan), "--plan-sha256", digest]), 0)
        self.assertEqual(json.loads(out.getvalue())["plan_sha256"], digest)


# The loader test runs a real local interpreter; remote tokens must be plain.
PLAIN_PYTHON = next((p for p in ("/usr/bin/python3", sys.executable)
                     if os.path.exists(p) and bootstrap.SAFE_PATH.match(p)), None)


class TransportTests(Case):
    def remote(self, python=sys.executable):
        plan_path, digest = plan_fixture(self.root, lambda p: p["guest"].update(python=python))
        _, plan, _ = bootstrap.load_plan(plan_path, digest)
        return bootstrap.SshRemote(plan, self.root / "inputs", bootstrap.REMOTE.read_bytes())

    def test_ssh_argv_uses_the_pins_and_the_f3_jump(self):
        remote = self.remote("/usr/bin/python3")
        argv = remote.argv("guest", remote.remote_command("guest", "guest-ping", []))
        self.assertEqual(argv[0], str(self.root / "inputs" / "ssh"))
        for option in ("-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oHostKeyAlias=rocky",
                       "-oUserKnownHostsFile=" + str(self.root / "inputs" / "guest-known_hosts")):
            self.assertIn(option, argv)
        proxy = [a for a in argv if a.startswith("-oProxyCommand=")][0]
        self.assertIn("-W 10.20.0.123:22 -- paul@10.20.0.133", proxy)
        self.assertEqual(argv[-2], "root@10.20.0.123")
        self.assertTrue(argv[-1].startswith('/usr/bin/python3 -I -B -c "import sys,hashlib;'))
        with self.assertRaisesRegex(bootstrap.Refused, "unsafe remote command token"):
            remote.remote_command("host", "host-poll", ["--guard-dir", "/x;reboot"])

    @unittest.skipUnless(PLAIN_PYTHON, "no python3 at a plain ASCII path")
    def test_loader_runs_the_exact_helper_through_a_shell(self):
        remote = self.remote(PLAIN_PYTHON)
        command = remote.remote_command("guest", "guest-ping", [])
        for shell in [s for s in ("/bin/sh", "/bin/bash") if os.path.exists(s)]:
            with self.subTest(shell=shell):
                done = subprocess.run([shell, "-c", command], input=remote.program, capture_output=True, timeout=30)
                self.assertEqual(done.returncode, 0, done.stderr)
                result = json.loads(done.stdout)
                self.assertEqual((result["ok"], result["action"], result["release"]),
                                 (True, "guest-ping", os.uname().release))
        tampered = bytearray(remote.program)
        tampered[-2:-1] = b"#"
        done = subprocess.run(["/bin/sh", "-c", command], input=bytes(tampered), capture_output=True, timeout=30)
        self.assertEqual((done.returncode, done.stdout), (97, b""))

    def test_bounded_local_child(self):
        done = bootstrap.run_bounded([sys.executable, "-c", "import sys; sys.stdout.write(sys.stdin.read())"],
                                     b"x" * 200000, 30, bootstrap.local_env())
        self.assertEqual((done.returncode, len(done.stdout), done.incomplete), (0, 200000, False))
        slow = bootstrap.run_bounded([sys.executable, "-c", "import time; time.sleep(30)"], b"", 0.5,
                                     bootstrap.local_env())
        self.assertEqual((slow.returncode, slow.incomplete, slow.reason), (None, True, "deadline"))
        with mock.patch.object(bootstrap, "STREAM_LIMIT", 1000):
            flood = bootstrap.run_bounded([sys.executable, "-c", "print('y' * 5000)"], b"", 30,
                                          bootstrap.local_env())
        self.assertEqual((flood.incomplete, flood.reason), (True, "stream overflow"))

    def test_classification(self):
        ok = reply({"ok": True, "action": "guest-ping"})
        self.assertTrue(bootstrap.classify(ok, "guest-ping", False)["ok"])
        with self.assertRaises(bootstrap.Retry):
            bootstrap.classify(bootstrap.Reply(255, b"", b"refused"), "guest-ping", False)
        with self.assertRaises(bootstrap.StepUncertain):
            bootstrap.classify(bootstrap.Reply(255, b"", b"lost"), "guest-write", True)
        with self.assertRaises(bootstrap.StepUncertain):
            bootstrap.classify(reply({"ok": True, "action": "guest-ping"}), "guest-write", True)
        with self.assertRaises(bootstrap.StepFailed):
            bootstrap.classify(reply({"ok": False, "changed": True, "error": "x"}, 2), "guest-admission", True)


class ConstantTests(unittest.TestCase):
    def test_shared_constants_agree(self):
        remote = module("bootstrap_remote_constants", "bootstrap_remote.py")
        self.assertEqual(remote.MOUNT_OPTIONS, bootstrap.MOUNT_OPTIONS)
        plan = (HERE.parents[2] / "docs" / "lab" / "zq3-run-plan.md").read_text()
        self.assertIn(bootstrap.MOUNT_OPTIONS, plan)
        self.assertEqual([s[0] for s in bootstrap.STEPS], list(bootstrap.STEP_NAMES))
        self.assertTrue(shutil.which("python3") or sys.executable)


if __name__ == "__main__":
    unittest.main()
