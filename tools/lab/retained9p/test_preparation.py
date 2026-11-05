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
    host, guest_pin = root / "host-pin", root / "guest-pin"
    host.write_text("original-host-public-pin\n")
    guest_pin.write_text("original-guest-public-pin\n")
    mounts = [{"path": "/var/tmp/hfs-zq3-test/m" + str(i), "tag": "hfs" + str(i),
               "mount_id": i + 10, "session": "session" + str(i)} for i in range(2)]
    requests = [{"version": guest.VERSION, "run_id": "test", "boot_id": "boot",
                 "provider_nonce": "provider", "manifest_sha256": "b" * 64,
                 "mounts": mounts, "case": name, "seconds": 1}
                for name in ["credentials", "sync", "metadata", "namespace", "mapped-pressure"]]
    plan = {"version": controller.VERSION, "run_id": "test", "seconds": 120,
            "candidate_manifest_sha256": "a" * 64, "installed_manifest_sha256": "b" * 64,
            "ssh": {"path": str(ssh), "sha256": controller.digest(ssh.read_bytes())},
            "probe_sha256": controller.digest(probe.read_bytes()), "requests": requests,
            "host": {"address": "192.168.1.133", "alias": "f3", "user": "paul",
                     "known_hosts": str(host), "known_hosts_sha256": controller.digest(host.read_bytes())},
            "guest": {"address": "192.168.1.123", "alias": "rocky", "user": "root",
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
            for command, expected_pin in [(argv, "original-guest-public-pin\n"),
                                          (jump, "original-host-public-pin\n")]:
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
                (folder / "stdout.log").write_text(json.dumps({"event": "terminal", "syscall_success": True,
                        "uncertain_children": [], "acceptance": False, "publication": None}) + "\n")
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
            controller.require_candidate("7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b")

    def test_terminal_cannot_claim_acceptance_or_hide_uncertain_children(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "reply"
            terminal = {"event": "terminal", "syscall_success": True,
                        "uncertain_children": [], "acceptance": False,
                        "publication": None}
            path.write_text(json.dumps(terminal) + "\n")
            self.assertFalse(controller.check_terminal(path, "sync")["acceptance"])
            for changes in [{"acceptance": True}, {"publication": {}},
                            {"uncertain_children": [123]}, {"syscall_success": False},
                            {"event": "reply"}]:
                path.write_text(json.dumps({**terminal, **changes}) + "\n")
                with self.assertRaises(controller.Refused):
                    controller.check_terminal(path, "sync")

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


if __name__ == "__main__":
    unittest.main()
