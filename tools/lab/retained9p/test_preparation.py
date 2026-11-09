#!/usr/bin/env python3
"""Local refusal/transport tests only; never installed/filesystem acceptance."""
import importlib.util
import contextlib
import fcntl
import io
import json
import os
from pathlib import Path
import subprocess
import shlex
import sys
import tempfile
import time
import unittest
from unittest import mock


def module(name):
    source = Path(__file__).with_name(name + ".py")
    spec = importlib.util.spec_from_file_location(name, source)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


controller = module("controller")
guest = module("guest")
observer = module("observer")
# xz3 r54 kernel rebuild (2026-11-09); see controller.ADMITTED.
ADMITTED = "5eb42c5066792b55e439373db48e8e8e88532efc61ff7b82f64db22a87798f99"
# e0cca0c (no d44 MKDIR-only lost reply) and f44 (pre-r54 guest kernel).
SUPERSEDED = ("3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1",
              "b203799e3f631888b2c2edc24e1d38b913249bea978e0d4d954886f589d04fa2")
QUARANTINED = "7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b"
LAB = Path(__file__).resolve().parents[3] / "docs/lab"
HOST_PIN = (LAB / "f3-host-known_hosts").read_bytes()
GUEST_PIN = (LAB / "rocky-known_hosts").read_bytes()


def guest_stdout(request):
    """Guest-shaped JSONL for local controller tests; no real guest or SSH."""
    case = request["case"]
    if request["version"] == guest.RELEASE_VERSION:
        events = [{"event": "payload-released", "leaf": "zq3-test-" + case,
                   "correlation_sha256": request["correlation_sha256"]},
                  {"event": "terminal", "action": "release", "syscall_success": True,
                   "payload_retained": False, "payload_released": True,
                   "uncertain_children": [], "acceptance": False, "publication": None}]
    else:
        inventory = {"leaf": "zq3-test-" + case, "leaf_ino": 7, "listing": [case], "entries": []}
        events = [{"event": "payload-retained", "leaf": inventory["leaf"], "inventory": inventory,
                   "inventory_sha256": controller.digest(guest.canonical(inventory)),
                   "release_token": controller.digest(case.encode())},
                  {"event": "terminal", "action": "case", "syscall_success": True,
                   "payload_retained": True, "payload_released": False,
                   "uncertain_children": [], "acceptance": False, "publication": None}]
    return "".join(json.dumps(event) + "\n" for event in events)


def correlation_for(root, output, name, **changes):
    """A host correlation record bound to the retained case's actual evidence."""
    state = json.loads((output / "state.json").read_bytes())
    record = next(item for item in state["cases"] if item["name"] == name)
    value = {"version": controller.CORRELATION_VERSION, "run_id": "test", "case": name,
             "case_stdout_sha256": record["stdout_sha256"],
             "inventory_sha256": record["inventory_sha256"],
             "host_observe": {"journal": "fixture"}, "acceptance": False, **changes}
    path = root / ("correlation-" + name + ".json")
    path.write_text(json.dumps(value))
    return str(path), controller.digest(path.read_bytes())


def batch_fixture(root):
    ssh = root / "ssh-original"
    ssh.write_text("#!" + sys.executable + "\nimport json,pathlib,sys\n"
                   "sys.stdin.buffer.read()\n"
                   "pins=[pathlib.Path(a.split('=',1)[1]).read_text() for a in sys.argv[1:] "
                   "if a.startswith('-oUserKnownHostsFile=')]\n"
                   "print(json.dumps({'marker':'verified-original','pins':pins,'args':sys.argv[1:]}))\n")
    ssh.chmod(0o700)
    probe = root / "guest-original.py"
    probe.write_text("print('verified-probe')\n")
    # Copies of the reviewed public pin files: the controller checks their
    # content against its fingerprints, not only the plan's byte hashes.
    host, guest_pin = root / "host-pin", root / "guest-pin"
    host.write_bytes(HOST_PIN)
    guest_pin.write_bytes(GUEST_PIN)
    mounts = [{"path": "/var/tmp/hfs-zq3-test/m" + str(i), "tag": "hfs" + str(i),
               "mount_id": i + 10, "session": "session" + str(i)} for i in range(2)]
    requests = [{"version": guest.VERSION, "run_id": "test", "boot_id": "boot",
                 "provider_nonce": "provider", "manifest_sha256": "b" * 64,
                 "mounts": mounts, "case": name, "seconds": 1}
                for name in ["credentials", "sync", "metadata", "namespace", "mapped-pressure"]]
    plan = {"version": controller.VERSION, "run_id": "test", "seconds": 120,
            "candidate_manifest_sha256": ADMITTED, "installed_manifest_sha256": "b" * 64,
            "ssh": {"path": str(ssh), "sha256": controller.digest(ssh.read_bytes())},
            "probe_sha256": controller.digest(probe.read_bytes()), "requests": requests,
            "host": {"address": "10.20.0.133", "alias": "f3", "user": "paul",
                     "known_hosts": str(host), "known_hosts_sha256": controller.digest(host.read_bytes())},
            "guest": {"address": "10.20.0.123", "alias": "rocky", "user": "root",
                      "known_hosts": str(guest_pin), "known_hosts_sha256": controller.digest(guest_pin.read_bytes())}}
    path = root / "plan.json"
    # Preserve a deliberately noncanonical but valid original plan byte image.
    path.write_text(json.dumps(plan, indent=2) + "\n\n")
    return path, plan, probe


class PreparationTests(unittest.TestCase):
    def test_ssh_expansion_output_paths_refuse_before_staging_or_spawn(self):
        for suffix in ["run-%h", "run-%r", "run-%%", "run-${HOME}", "run-$USER", "run-~",
                       "run space", "run\tname", "run\nname", "run'quote", 'run"quote',
                       "run\\name", "run;name", "run(name)", "run-é"]:
            with self.subTest(path=suffix), tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary) / suffix
                with mock.patch.object(controller, "plan_snapshot") as snapshot, \
                        mock.patch.object(controller, "mkdir_durable") as staging, \
                        mock.patch.object(controller.subprocess, "Popen") as spawn:
                    with self.assertRaisesRegex(controller.Refused, "safe ASCII path"):
                        controller.probe("/unused-plan", "a" * 64, output)
                    snapshot.assert_not_called()
                    staging.assert_not_called()
                    spawn.assert_not_called()
                self.assertFalse(output.exists())
        controller.check_output_path("/tmp/hfs-run_2026.11-05/result_1")

    def test_plan_snapshot_and_check_never_reopen_replaced_plan(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path, _, probe = batch_fixture(root)
            original = path.read_bytes()
            actual_read = controller.read
            count = 0

            def replace_after_read(name, *args):
                nonlocal count
                raw = actual_read(name, *args)
                if Path(name) == path:
                    count += 1
                    path.write_text('{"version":"replaced-pathname"}')
                return raw

            with mock.patch.object(controller, "GUEST", probe), \
                    mock.patch.object(controller, "read", side_effect=replace_after_read):
                raw, plan, _ = controller.plan_snapshot(path, controller.digest(original))
                controller.persist_bytes(root / "retained-plan.json", raw)
                self.assertEqual(plan["run_id"], "test")
                self.assertEqual((root / "retained-plan.json").read_bytes(), original)
                self.assertEqual(count, 1)
                path.write_bytes(original)
                count = 0
                printed = io.StringIO()
                with mock.patch.object(sys, "argv", ["controller", "check", "--plan", str(path)]), \
                        contextlib.redirect_stdout(printed):
                    self.assertEqual(controller.main(), 0)
                self.assertEqual(json.loads(printed.getvalue())["plan_sha256"], controller.digest(original))
                self.assertEqual(count, 1)
                path.write_bytes(original)
                count = 0

                def failed_terminal(argv, request, folder, seconds):
                    self.assertEqual(request["run_id"], "test")
                    (folder / "stdout.log").write_text(json.dumps({"event": "terminal", "syscall_success": False,
                        "uncertain_children": [], "acceptance": False, "publication": None}) + "\n")
                    (folder / "stderr.log").write_text("")
                    return {"returncode": 1}

                with mock.patch.object(controller, "bounded_child", side_effect=failed_terminal), \
                        contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(controller.probe(path, controller.digest(original), root / "run"), 1)
                self.assertEqual(count, 1)
                self.assertEqual((root / "run/plan.json").read_bytes(), original)

    def test_replaced_inputs_execute_only_private_verified_snapshots(self):
        # Local executable stand-in checks real execution binding, never SSH or
        # filesystem compatibility. Both outer and ProxyCommand paths are run.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path, plan, probe = batch_fixture(root)
            with mock.patch.object(controller, "GUEST", probe):
                _, _, verified = controller.plan_snapshot(path)
            Path(plan["ssh"]["path"]).write_text("unverified replacement must never execute")
            probe.write_text("unverified probe replacement")
            for target in [plan["host"], plan["guest"]]:
                Path(target["known_hosts"]).write_text("unverified pin replacement")
            output = root / "run"
            controller.mkdir_durable(output)
            inputs = controller.stage_inputs(output, verified)
            argv = controller.invocation(plan, inputs, verified["probe.py"])
            jump = shlex.split(next(arg.split("=", 1)[1] for arg in argv if arg.startswith("-oProxyCommand=")))
            for command, expected_pin in [(argv, GUEST_PIN.decode()),
                                          (jump, HOST_PIN.decode())]:
                result = subprocess.run(command, input=b"", capture_output=True, check=True, timeout=2)
                observed = json.loads(result.stdout)
                self.assertEqual(observed["marker"], "verified-original")
                self.assertEqual(observed["pins"], [expected_pin])
                self.assertEqual(command[0], str(inputs / "ssh"))
            self.assertIn("verified-probe", argv[-1])
            self.assertNotIn("unverified", argv[-1])
            self.assertEqual((inputs.stat().st_mode & 0o777), 0o700)
            for staged in inputs.iterdir():
                self.assertEqual(staged.stat().st_mode & 0o222, 0)

    def test_changed_input_before_snapshot_refuses_before_spawn(self):
        for changed in ["ssh", "probe", "host", "guest"]:
            with self.subTest(input=changed), tempfile.TemporaryDirectory() as temporary:
                path, plan, probe = batch_fixture(Path(temporary))
                target = (Path(plan["ssh"]["path"]) if changed == "ssh" else probe if changed == "probe"
                          else Path(plan[changed]["known_hosts"]))
                target.write_text("different bytes")
                with mock.patch.object(controller, "GUEST", probe), \
                        mock.patch.object(controller.subprocess, "Popen") as spawn:
                    with self.assertRaisesRegex(controller.Refused, "pinned input changed"):
                        controller.plan_snapshot(path)
                    spawn.assert_not_called()

    def test_output_parent_sync_failure_prevents_dispatch(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path, _, probe = batch_fixture(root)
            output = root / "run"
            actual_sync = controller.fsync_directory

            def fail_parent(name):
                if Path(name) == root and output.exists():
                    raise OSError("parent durability failure")
                actual_sync(name)

            with mock.patch.object(controller, "GUEST", probe), \
                    mock.patch.object(controller, "fsync_directory", side_effect=fail_parent), \
                    mock.patch.object(controller.subprocess, "Popen") as spawn:
                with self.assertRaisesRegex(OSError, "parent durability failure"):
                    controller.probe(path, controller.digest(path.read_bytes()), output)
                spawn.assert_not_called()

    def test_log_entries_synced_before_spawn_and_after_final_data(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            events = []
            actual_sync, actual_popen = controller.fsync_directory, controller.subprocess.Popen

            def sync(name):
                events.append("directory-synced")
                actual_sync(name)

            def spawn(*args, **kwargs):
                self.assertEqual(events[-1], "directory-synced")
                self.assertEqual((root / "stdout.log").stat().st_size, 0)
                self.assertEqual((root / "stderr.log").stat().st_size, 0)
                events.append("spawn")
                return actual_popen(*args, **kwargs)

            with mock.patch.object(controller, "fsync_directory", side_effect=sync), \
                    mock.patch.object(controller.subprocess, "Popen", side_effect=spawn):
                controller.bounded_child([sys.executable, "-I", "-c",
                                          "import sys; sys.stdin.buffer.read(); print('completed')"], {}, root, 2)
            self.assertEqual(events, ["directory-synced", "spawn", "directory-synced"])

    def test_final_case_directory_sync_failure_never_publishes_completion(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path, _, probe = batch_fixture(root)
            output = root / "run"
            actual_sync = controller.fsync_directory
            dispatched = 0

            def terminal_only(argv, request, folder, seconds):
                nonlocal dispatched
                dispatched += 1
                (folder / "stdout.log").write_text(guest_stdout(request))
                (folder / "stderr.log").write_text("")
                return {"returncode": 0}

            def fail_after_log_creation(name):
                if Path(name) == output / "credentials" and (Path(name) / "stdout.log").exists():
                    raise OSError("case finalization failure")
                actual_sync(name)

            with mock.patch.object(controller, "GUEST", probe), \
                    mock.patch.object(controller, "bounded_child", side_effect=terminal_only), \
                    mock.patch.object(controller, "fsync_directory", side_effect=fail_after_log_creation), \
                    contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(controller.probe(path, controller.digest(path.read_bytes()), output), 1)
            state = json.loads((output / "state.json").read_bytes())
            self.assertEqual(dispatched, 1)
            self.assertEqual(state["cases"][0]["state"], "dispatching-uncertain")
            self.assertFalse(state["syscall_batch_success"])

    def test_known_candidate_quarantine_refuses_without_contacting_lab(self):
        with self.assertRaisesRegex(controller.Refused, "quarantined CGO-disabled"):
            controller.require_candidate(QUARANTINED)

    def test_superseded_candidate_refuses_with_reason(self):
        # Superseded candidates are not defective but each lacks a fix the run
        # needs (e0cca0c: d44 MKDIR-only lost reply; f44: r54 mount HELLO).
        # They must refuse distinctly from an unknown digest.
        for value in SUPERSEDED:
            with self.subTest(candidate=value), \
                    self.assertRaisesRegex(controller.Refused, "superseded xz3 candidate"):
                controller.require_candidate(value)

    def test_only_pinned_admitted_candidate_passes(self):
        controller.require_candidate(ADMITTED)
        for value in ["a" * 64, ADMITTED.upper(), ADMITTED[:-1], None]:
            with self.subTest(candidate=value), self.assertRaises(controller.Refused):
                controller.require_candidate(value)
        with tempfile.TemporaryDirectory() as temporary:
            path, plan, probe = batch_fixture(Path(temporary))
            plan["candidate_manifest_sha256"] = QUARANTINED
            path.write_text(json.dumps(plan))
            with mock.patch.object(controller, "GUEST", probe), \
                    mock.patch.object(controller, "verified_bytes") as reads:
                with self.assertRaisesRegex(controller.Refused, "quarantined"):
                    controller.plan_snapshot(path)
                reads.assert_not_called()

    def test_terminal_cannot_claim_acceptance_or_hide_uncertain_children(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "reply"
            request = {"version": guest.VERSION, "case": "sync"}
            path.write_text(guest_stdout(request))
            result = controller.check_terminal(path, "sync")
            self.assertFalse(result["acceptance"])
            self.assertEqual(result["payload"], "retained")
            events = [json.loads(line) for line in guest_stdout(request).splitlines()]
            for changes in [{"acceptance": True}, {"publication": {}},
                            {"uncertain_children": [123]}, {"syscall_success": False},
                            {"event": "reply"}, {"action": "release"},
                            {"payload_retained": False}, {"payload_released": True}]:
                path.write_text(json.dumps(events[0]) + "\n" + json.dumps({**events[1], **changes}) + "\n")
                with self.subTest(changes=changes), self.assertRaises(controller.Refused):
                    controller.check_terminal(path, "sync")

    def test_successful_case_without_valid_retention_refuses(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "reply"
            retained, terminal = guest_stdout({"version": guest.VERSION, "case": "sync"}).splitlines()
            # The released-immediately behaviour (no retention record) must refuse.
            path.write_text(terminal + "\n")
            with self.assertRaisesRegex(controller.Refused, "retain its payload exactly once"):
                controller.check_terminal(path, "sync")
            event = json.loads(retained)
            for changes in [{"inventory_sha256": "0" * 64}, {"release_token": "short"},
                            {"leaf": "other"}, {"inventory": None}]:
                path.write_text(json.dumps({**event, **changes}) + "\n" + terminal + "\n")
                with self.subTest(changes=changes), self.assertRaisesRegex(controller.Refused, "retention"):
                    controller.check_terminal(path, "sync")
            path.write_text(retained + "\n" + retained + "\n" + terminal + "\n")
            with self.assertRaisesRegex(controller.Refused, "exactly once"):
                controller.check_terminal(path, "sync")

    def test_release_terminal_must_prove_bound_release(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "reply"
            request = {"version": guest.RELEASE_VERSION, "case": "sync", "correlation_sha256": "c" * 64}
            path.write_text(guest_stdout(request))
            self.assertEqual(controller.check_terminal(path, "sync", "release", "c" * 64)["payload"], "released")
            with self.assertRaisesRegex(controller.Refused, "bound payload release"):
                controller.check_terminal(path, "sync", "release", "d" * 64)
            with self.assertRaisesRegex(controller.Refused, "action differs"):
                controller.check_terminal(path, "sync", "case")
            released, terminal = guest_stdout(request).splitlines()
            path.write_text(terminal + "\n")
            with self.assertRaisesRegex(controller.Refused, "bound payload release"):
                controller.check_terminal(path, "sync", "release", "c" * 64)

    def test_plain_directory_fallback_refused_before_any_open_or_io(self):
        request = {"version": guest.VERSION, "run_id": "test", "case": "sync", "seconds": 5,
                   "boot_id": "boot", "provider_nonce": "provider", "manifest_sha256": controller.digest(b"manifest"),
                   "mounts": [{"path": "/var/tmp/hfs-zq3-test/m" + str(i),
                               "tag": "hfs" + str(i), "mount_id": i + 10,
                               "session": "s" + str(i)} for i in range(2)]}
        admission = {"boot_id": "boot", "provider_nonce": "provider",
                     "domains": [{"session": "s0"}, {"session": "s1"}]}
        values = {"/proc/sys/kernel/random/boot_id": b"boot\n",
                  "/etc/homelabfs/installed-admission-v1.json": b"manifest",
                  "/sys/fs/selinux/enforce": b"1\n",
                  "/sys/kernel/homelabfs/admission_v1": json.dumps(admission).encode(),
                  "/proc/self/mountinfo": b"1 0 0:1 / / rw - ext4 root rw\n"}
        with mock.patch.object(guest, "read", side_effect=lambda path, _: values[path]), \
                mock.patch.object(guest.os, "geteuid", return_value=0), \
                mock.patch.object(guest.os, "open") as opened:
            with self.assertRaisesRegex(guest.Refused, "exact mountpoint"):
                guest.admit(request)
            opened.assert_not_called()

    def test_forced_observer_allows_only_exact_full_or_phase_command(self):
        source = "print('fixture only')\n"
        exact = "python3 -I -c '" + source.replace("'", "'\\''") + "'"
        self.assertEqual(observer.permitted_phase(source, exact), [])
        self.assertEqual(observer.permitted_phase(source, exact + " phase"), ["phase"])
        for command in ["", "id", exact + "; id", exact + " phase extra", exact.replace("-I", "")]:
            with self.assertRaises(ValueError):
                observer.permitted_phase(source, command)

    def test_controller_bounds_real_local_transport_streams(self):
        # These children are local Python transports, never guest or filesystem probes.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result = controller.bounded_child([sys.executable, "-I", "-c",
                                               "import sys; sys.stdin.buffer.read(); print('local transport')"],
                                               {}, root, 3)
            self.assertEqual(result["returncode"], 0)
            self.assertEqual((root / "stdout.log").read_bytes(), b"local transport\n")
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(controller.Refused, "stream cap"):
                controller.bounded_child([sys.executable, "-I", "-c", "print('x' * 300000)"],
                                           {}, Path(temporary), 3)

    def test_transport_timeout_remains_uncertain_and_is_not_retried(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(TimeoutError, "uncertain"):
                controller.bounded_child([sys.executable, "-I", "-c", "import time; time.sleep(10)"],
                                           {}, Path(temporary), 0.1)

    def test_request_backpressure_cannot_escape_case_deadline(self):
        actual_popen = controller.subprocess.Popen

        def small_pipe(*args, **kwargs):
            process = actual_popen(*args, **kwargs)
            fcntl.fcntl(process.stdin.fileno(), fcntl.F_SETPIPE_SZ, 4096)
            return process

        with tempfile.TemporaryDirectory() as temporary, \
                mock.patch.object(controller.subprocess, "Popen", side_effect=small_pipe):
            started = time.monotonic()
            # A real 4KiB pipe, larger allowed request and a child that never
            # reads stdin reproduce the formerly unbounded synchronous write.
            with self.assertRaisesRegex(TimeoutError, "uncertain"):
                controller.bounded_child([sys.executable, "-I", "-c", "import time; time.sleep(10)"],
                                           {"padding": "x" * 12000}, Path(temporary), 0.1)
            self.assertLess(time.monotonic() - started, 3)

    def test_request_wire_bound_refuses_before_process_creation(self):
        with mock.patch.object(controller.subprocess, "Popen") as spawn:
            with self.assertRaisesRegex(controller.Refused, "16KiB wire bound"):
                controller.bounded_child(["unused"], {"padding": "x" * 16384}, Path("/unused"), 1)
            spawn.assert_not_called()

    def test_duplicate_json_and_nonregular_inputs_refuse(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / "duplicate.json"
            path.write_text('{"a":1,"a":2}')
            with self.assertRaises(controller.Refused):
                controller.load(path)
            with self.assertRaises(controller.Refused):
                controller.read(root)
            link = root / "link"
            link.symlink_to(path)
            with self.assertRaises(OSError):
                controller.read(link)


class RetainedReleaseControllerTests(unittest.TestCase):
    """Local controller state machine; dispatch is a guest-shaped stand-in."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.path, self.plan, probe = batch_fixture(self.root)
        self.hash = controller.digest(self.path.read_bytes())
        self.output = self.root / "run"
        self.dispatched, self.deadlines = [], []
        for patcher in [mock.patch.object(controller, "GUEST", probe),
                        mock.patch.object(controller, "bounded_child", side_effect=self.fake_dispatch)]:
            patcher.start()
            self.addCleanup(patcher.stop)

    def fake_dispatch(self, argv, request, folder, seconds):
        self.dispatched.append(request)
        self.deadlines.append(seconds)
        (folder / "stdout.log").write_text(guest_stdout(request))
        (folder / "stderr.log").write_text("")
        return {"returncode": 0}

    def quiet(self, function, *args):
        with contextlib.redirect_stdout(io.StringIO()):
            return function(*args)

    def state(self):
        return json.loads((self.output / "state.json").read_bytes())

    def release(self, name, **changes):
        correlation, sha = correlation_for(self.root, self.output, name, **changes)
        return self.quiet(controller.release, self.output, self.hash, correlation, sha)

    def test_each_case_stays_retained_until_bound_correlation_release(self):
        self.assertEqual(self.quiet(controller.probe, self.path, self.hash, self.output), 0)
        self.assertEqual(self.state()["cases"][0]["state"], "payload-retained-awaiting-correlation")
        # Next case cannot start while the previous payload is still retained.
        with self.assertRaisesRegex(controller.Refused, "previous payload released"):
            controller.next_case(self.output, self.hash)
        for index, case in enumerate(controller.CASES):
            if index:
                self.assertEqual(self.quiet(controller.next_case, self.output, self.hash), 0)
            self.assertEqual(self.release(case), 0)
            request = self.dispatched[-1]
            self.assertEqual(request["version"], guest.RELEASE_VERSION)
            self.assertEqual(request["case"], case)
            self.assertEqual(request["token"], controller.digest(case.encode()))
            record = self.state()["cases"][index]
            self.assertEqual(record["state"], "released")
            self.assertEqual(request["correlation_sha256"], record["correlation_sha256"])
            self.assertTrue((self.output / case / "correlation.json").exists())
        final = self.state()
        self.assertTrue(final["syscall_batch_success"])
        self.assertFalse(final["zq3_acceptance_complete"])
        self.assertEqual(len(self.dispatched), 10)
        with self.assertRaisesRegex(controller.Refused, "remaining case"):
            controller.next_case(self.output, self.hash)

    def test_unbound_correlation_refuses_without_dispatch_or_state_change(self):
        self.quiet(controller.probe, self.path, self.hash, self.output)
        before = (self.output / "state.json").read_bytes()
        for changes in [{"case": "sync"}, {"run_id": "other"}, {"case_stdout_sha256": "0" * 64},
                        {"inventory_sha256": "0" * 64}, {"host_observe": {}},
                        {"acceptance": True}, {"version": "other"}, {"extra": 1}]:
            with self.subTest(changes=changes), self.assertRaises(controller.Refused):
                self.release("credentials", **changes)
        correlation, _ = correlation_for(self.root, self.output, "credentials")
        with self.assertRaisesRegex(controller.Refused, "pinned input changed"):
            controller.release(self.output, self.hash, correlation, "0" * 64)
        with self.assertRaisesRegex(controller.Refused, "plan digest differs"):
            controller.release(self.output, "0" * 64, correlation, "0" * 64)
        self.assertEqual(len(self.dispatched), 1)
        self.assertEqual((self.output / "state.json").read_bytes(), before)
        self.assertEqual(self.release("credentials"), 0)

    def test_changed_retained_evidence_or_staged_input_refuses_release(self):
        self.quiet(controller.probe, self.path, self.hash, self.output)
        correlation, sha = correlation_for(self.root, self.output, "credentials")
        log = self.output / "credentials" / "stdout.log"
        original = log.read_bytes()
        log.write_bytes(original.replace(b'"leaf_ino": 7', b'"leaf_ino": 8'))
        with self.assertRaisesRegex(controller.Refused, "evidence changed"):
            controller.release(self.output, self.hash, correlation, sha)
        log.write_bytes(original)
        staged = self.output / "inputs" / "probe.py"
        staged.chmod(0o600)
        staged.write_text("replaced staged probe")
        with self.assertRaisesRegex(controller.Refused, "pinned input changed"):
            controller.release(self.output, self.hash, correlation, sha)
        self.assertEqual(len(self.dispatched), 1)

    def test_failed_release_dispatch_is_recorded_and_never_replayed(self):
        self.quiet(controller.probe, self.path, self.hash, self.output)

        def failed_release(argv, request, folder, seconds):
            self.dispatched.append(request)
            (folder / "stdout.log").write_text(json.dumps({"event": "terminal", "action": "release",
                "syscall_success": False, "payload_retained": False, "payload_released": False,
                "uncertain_children": [], "acceptance": False, "publication": None}) + "\n")
            return {"returncode": 1}

        with mock.patch.object(controller, "bounded_child", side_effect=failed_release):
            self.assertEqual(self.release("credentials"), 1)
        state = self.state()
        self.assertEqual(state["cases"][0]["state"], "release-dispatching-uncertain")
        self.assertIn("release failed", state["error"])
        with self.assertRaisesRegex(controller.Refused, "never replay"):
            self.release("credentials")
        with self.assertRaisesRegex(controller.Refused, "never replay"):
            controller.next_case(self.output, self.hash)
        self.assertEqual(len(self.dispatched), 2)

    def test_expired_run_window_preserves_retained_payload(self):
        self.quiet(controller.probe, self.path, self.hash, self.output)
        started = self.state()["started_epoch"]
        correlation, sha = correlation_for(self.root, self.output, "credentials")
        with mock.patch.object(controller.time, "time", return_value=started + controller.RUN_WINDOW + 1):
            with self.assertRaisesRegex(controller.Refused, "window expired"):
                controller.release(self.output, self.hash, correlation, sha)
        self.assertEqual(self.state()["cases"][0]["state"], "payload-retained-awaiting-correlation")

    def test_plan_seconds_caps_every_dispatch(self):
        for plan_seconds, case_seconds, expected in [(60, 60, 60), (70, 60, 70),
                                                     (600, 60, 75), (60, 1, 16)]:
            with self.subTest(plan=plan_seconds, case=case_seconds):
                self.plan["seconds"] = plan_seconds
                for request in self.plan["requests"]:
                    request["seconds"] = case_seconds
                self.path.write_text(json.dumps(self.plan))
                self.hash = controller.digest(self.path.read_bytes())
                self.output = self.root / ("run-" + str(plan_seconds) + "-" + str(case_seconds))
                self.deadlines.clear()
                self.assertEqual(self.quiet(controller.probe, self.path, self.hash, self.output), 0)
                self.assertEqual(self.release("credentials"), 0)
                self.assertEqual(self.deadlines, [expected, expected])
        for invalid in [59, 601, "60"]:
            self.plan["seconds"] = invalid
            with self.subTest(invalid=invalid), self.assertRaisesRegex(controller.Refused, "deadline cap"):
                controller.validate_shape(self.plan)

    def test_concurrent_owner_lock_refuses(self):
        self.quiet(controller.probe, self.path, self.hash, self.output)
        with open(self.output / "owner.lock", "rb") as holder:
            fcntl.flock(holder, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaises(BlockingIOError):
                controller.next_case(self.output, self.hash)


class GuestRetentionTests(unittest.TestCase):
    """Real local syscalls on two filesystems: payload retention and release.

    Domain roots live on one filesystem and the run directory on another, as
    the guest's two 9p mounts and its local /var/tmp run directory do. This is
    local logic only, never 9p, host correlation or filesystem acceptance.
    """

    def setUp(self):
        domains = tempfile.TemporaryDirectory(dir="/dev/shm" if os.path.isdir("/dev/shm") else None)
        local = tempfile.TemporaryDirectory()
        self.addCleanup(domains.cleanup)
        self.addCleanup(local.cleanup)
        self.run_dir = Path(local.name) / "hfs-zq3-test"
        self.run_dir.mkdir(mode=0o700)
        self.domain = Path(domains.name)
        if os.stat(self.domain).st_dev == os.stat(self.run_dir).st_dev:
            self.skipTest("needs two distinct local filesystems")
        for name in ["m0", "m1"]:
            (self.domain / name).mkdir()
        self.roots = [os.open(self.domain / name, os.O_RDONLY | os.O_DIRECTORY) for name in ["m0", "m1"]]
        for fd in self.roots:
            self.addCleanup(os.close, fd)
        for name, value in [("RUN_PARENT", str(Path(local.name) / "hfs-zq3-")),
                            ("TRUSTED_UID", os.geteuid())]:
            patcher = mock.patch.object(guest, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        self.request = {"version": guest.VERSION, "run_id": "test", "boot_id": "boot",
                        "provider_nonce": "provider", "manifest_sha256": "b" * 64,
                        "mounts": [], "case": "sync", "seconds": 5}

    def run_case(self):
        printed = io.StringIO()
        with contextlib.redirect_stdout(printed):
            probe = guest.Probe(self.request, self.roots)
            guest.sync(probe)
            guest.Retention(probe).retain()
        events = [json.loads(line) for line in printed.getvalue().splitlines()]
        return next(event for event in events if event["event"] == "payload-retained")

    def release(self, retained, **changes):
        request = {**self.request, "version": guest.RELEASE_VERSION,
                   "token": retained["release_token"],
                   "inventory_sha256": retained["inventory_sha256"],
                   "correlation_sha256": "c" * 64, **changes}
        guest.is_release(request)
        printed = io.StringIO()
        with contextlib.redirect_stdout(printed):
            guest.Retention(guest.Probe(request, self.roots)).release()
        return [json.loads(line) for line in printed.getvalue().splitlines()]

    def leaf(self):
        return self.domain / "m0" / "zq3-test-sync"

    def test_successful_case_retains_until_authenticated_release(self):
        retained = self.run_case()
        names = sorted(path.name for path in self.leaf().iterdir())
        self.assertEqual(names, ["dsync", "mapped", "ordinary", "sync"])
        record = self.run_dir / "release-sync.json"
        self.assertEqual(record.stat().st_mode & 0o777, 0o600)
        self.assertNotIn(retained["release_token"], record.read_text())
        self.assertEqual(json.loads(record.read_text())["inventory_sha256"], retained["inventory_sha256"])
        events = self.release(retained)
        self.assertFalse(self.leaf().exists())
        self.assertFalse(record.exists())
        self.assertTrue((self.run_dir / "released-sync.json").exists())
        self.assertEqual(events[-1]["correlation_sha256"], "c" * 64)
        # Replay finds no retention record and refuses.
        with self.assertRaises(FileNotFoundError):
            self.release(retained)

    def assert_preserved(self, retained):
        self.assertEqual(len(list(self.leaf().iterdir())), 4)
        self.assertTrue((self.run_dir / "release-sync.json").exists())
        self.assertFalse((self.run_dir / "released-sync.json").exists())
        self.assertEqual(self.release(retained)[-1]["event"], "payload-released")

    def test_wrong_token_refuses_and_preserves(self):
        retained = self.run_case()
        with self.assertRaisesRegex(guest.Refused, "release token refused"):
            self.release(retained, token="0" * 64)
        self.assert_preserved(retained)

    def test_changed_payload_or_identity_refuses_and_preserves(self):
        retained = self.run_case()
        for changes, message in [({"inventory_sha256": "0" * 64}, "differs from release request"),
                                 ({"boot_id": "rebooted"}, "differs from release request"),
                                 ({"provider_nonce": "other"}, "differs from release request")]:
            with self.subTest(changes=changes), self.assertRaisesRegex(guest.Refused, message):
                self.release(retained, **changes)
        with open(self.leaf() / "ordinary", "ab") as stream:
            stream.write(b"x")
        with self.assertRaisesRegex(guest.Refused, "inventory changed .*contents not hashed"):
            self.release(retained)
        self.assertEqual(len(list(self.leaf().iterdir())), 4)
        self.assertTrue((self.run_dir / "release-sync.json").exists())

    def test_insecure_or_shared_run_directory_refuses_retention(self):
        self.run_dir.chmod(0o777)
        with self.assertRaisesRegex(guest.Refused, "root-private local run directory"):
            self.run_case()
        self.assertEqual(len(list(self.leaf().iterdir())), 4)
        # Any group or other bit, even read/execute only, refuses.
        probe = guest.Probe(self.request, self.roots)
        for mode in [0o750, 0o705, 0o710, 0o701, 0o740, 0o704]:
            self.run_dir.chmod(mode)
            with self.subTest(mode=oct(mode)), \
                    self.assertRaisesRegex(guest.Refused, "root-private local run directory"):
                guest.Retention(probe).run_directory()
        self.run_dir.chmod(0o700)
        os.close(guest.Retention(probe).run_directory())
        with mock.patch.object(guest, "RUN_PARENT", str(self.domain / "m1" / "hfs-zq3-")):
            (self.domain / "m1" / "hfs-zq3-test").mkdir(mode=0o700)
            probe = guest.Probe(self.request, self.roots)
            with self.assertRaisesRegex(guest.Refused, "root-private local run directory"):
                guest.Retention(probe).run_directory()

    def test_existing_retention_record_is_never_overwritten(self):
        (self.run_dir / "release-sync.json").write_text("{}")
        with self.assertRaises(FileExistsError):
            self.run_case()
        self.assertEqual((self.run_dir / "release-sync.json").read_text(), "{}")

    def test_release_request_shape_is_closed(self):
        base = {**self.request, "version": guest.RELEASE_VERSION, "token": "a" * 64,
                "inventory_sha256": "b" * 64, "correlation_sha256": "c" * 64}
        self.assertTrue(guest.is_release(base))
        self.assertFalse(guest.is_release(self.request))
        for request in [{key: value for key, value in base.items() if key != "correlation_sha256"},
                        {**base, "token": "A" * 64}, {**base, "token": 1},
                        {**self.request, "token": "a" * 64}, {**base, "version": guest.VERSION}, []]:
            with self.subTest(request=request), self.assertRaises(guest.Refused):
                guest.is_release(request)

    def test_main_retains_on_case_and_releases_only_on_release_request(self):
        def stub_case(probe):
            directory, _ = probe.directory()
            os.close(os.open("namespace", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600, dir_fd=directory))
            os.close(directory)

        def main(request):
            printed = io.StringIO()
            stdin = io.TextIOWrapper(io.BytesIO(json.dumps(request).encode()))
            with mock.patch.object(guest, "admit", return_value=list(self.roots)), \
                    mock.patch.object(guest.os, "close", side_effect=lambda fd: None if fd in self.roots else real_close(fd)), \
                    mock.patch.object(guest.resource, "setrlimit"), \
                    mock.patch.dict(guest.CASES, {"namespace": stub_case}), \
                    mock.patch.object(sys, "argv", ["guest"]), mock.patch.object(sys, "stdin", stdin), \
                    contextlib.redirect_stdout(printed):
                code = guest.main()
            return code, [json.loads(line) for line in printed.getvalue().splitlines()]

        real_close = os.close
        request = {**self.request, "case": "namespace"}
        code, events = main(request)
        self.assertEqual(code, 0)
        self.assertEqual({key: events[-1][key] for key in ["action", "payload_retained", "payload_released"]},
                         {"action": "case", "payload_retained": True, "payload_released": False})
        leaf = self.domain / "m0" / "zq3-test-namespace"
        self.assertTrue((leaf / "namespace").exists())
        retained = next(event for event in events if event["event"] == "payload-retained")
        code, events = main({**request, "version": guest.RELEASE_VERSION, "token": "0" * 64,
                             "inventory_sha256": retained["inventory_sha256"], "correlation_sha256": "c" * 64})
        self.assertEqual(code, 1)
        self.assertFalse(events[-1]["payload_released"])
        self.assertTrue((leaf / "namespace").exists())
        code, events = main({**request, "version": guest.RELEASE_VERSION, "token": retained["release_token"],
                             "inventory_sha256": retained["inventory_sha256"], "correlation_sha256": "c" * 64})
        self.assertEqual(code, 0)
        self.assertEqual({key: events[-1][key] for key in ["action", "payload_retained", "payload_released"]},
                         {"action": "release", "payload_retained": False, "payload_released": True})
        self.assertFalse(leaf.exists())


if __name__ == "__main__":
    unittest.main()
