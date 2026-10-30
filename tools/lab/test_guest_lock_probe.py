"""Local OS fixture verification only; never constitutes a live 9p pass."""
import importlib.util
import json
import os
import signal
import subprocess
import sys
from pathlib import Path
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("guest_lock_probe", Path(__file__).with_name("guest-lock-probe.py"))
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class GuestLockProbeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hfs-lock-local-")
        self.addCleanup(self.temp.cleanup)
        self.root = os.open(self.temp.name, os.O_RDONLY | os.O_DIRECTORY)
        self.addCleanup(os.close, self.root)

    def fixture(self, **kwargs):
        return probe.Fixture(self.root, "test", overall=10, case=1, **kwargs)

    def request(self, **kwargs):
        request = {"protocol": probe.PROTOCOL, "run_id": "test", "mountpoint": self.temp.name,
                   "tag": "hfs-test", "mount_id": probe.fd_mount_id(self.root),
                   "budgets": {"overall_seconds": 10, "case_seconds": 1}}
        request.update(kwargs)
        return request

    def assert_failure(self, result):
        self.assertFalse(result["passed"], result)
        self.assertTrue(result["error"], result)
        self.assertFalse(result["uncertain_children"], result)
        self.assertTrue(all(c.reaped for c in self.last_fixture.children))

    def run_fixture(self):
        self.last_fixture = self.fixture()
        return self.last_fixture.run()

    def test_cli_refuses_actual_non9p_and_bad_input_before_mutation(self):
        script = str(Path(__file__).with_name("guest-lock-probe.py"))
        for raw in (json.dumps(self.request()).encode(), b"{malformed", b"x" * (probe.MAX_REQUEST + 1)):
            with self.subTest(input_bytes=len(raw)):
                completed = subprocess.run([sys.executable, "-B", script], input=raw,
                                           capture_output=True, timeout=5, check=False)
                self.assertNotEqual(completed.returncode, 0)
                self.assertLessEqual(len(completed.stdout), probe.MAX_REPORT)
                result = json.loads(completed.stdout)
                self.assertFalse(result["passed"])
                self.assertTrue(result["error"])
                self.assertFalse(result["publication_identity_verified"])
                self.assertFalse(result["security_admission_verified"])
                self.assertTrue(result["parent_guard_oracle_required"])
                self.assertEqual(os.listdir(self.root), [])

    def test_unsupported_abi_refused_before_leaf_creation(self):
        with mock.patch.object(probe.platform, "machine", return_value="aarch64"):
            with self.assertRaisesRegex(probe.Refused, "unsupported"):
                self.fixture().run()
        self.assertEqual(os.listdir(self.root), [])

    def test_actual_os_lock_cases(self):
        result = self.run_fixture()
        self.assertTrue(result["passed"], result)
        self.assertEqual(result["scope"], "local-selftest")
        self.assertFalse(result["publication_identity_verified"])
        self.assertFalse(result["security_admission_verified"])
        self.assertTrue(result["parent_guard_oracle_required"])
        self.assertEqual(len(result["checks"]), 10)
        self.assertEqual(len(self.last_fixture.children), 4)
        self.assertEqual(len(list(Path(self.temp.name, "hfs-lock-test").iterdir())), 2)
        self.assertEqual(sum(p.stat().st_size for p in Path(self.temp.name, "hfs-lock-test").iterdir()), 0)
        self.assertEqual(len([e for e in result["events"] if "kernel_wait" in e]), 2)
        self.assertLess(len(json.dumps(result).encode()), probe.MAX_REPORT)
        self.assertTrue(all(item["status"] == 0 for item in result["child_cleanup"]))

    def test_signal_after_fork_is_deferred_until_pid_registration(self):
        real_fork = os.fork
        for signum in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
            with self.subTest(signum=signum):
                fixture = self.fixture()
                fixture.leaf += "-fork-" + str(signum)
                self.last_fixture = fixture
                observations = []
                forked = []
                def raising_handler(*args):
                    observations.append([child.pid for child in fixture.children])
                    raise TimeoutError("injected post-fork signal")
                def queued_fork():
                    pid = real_fork()
                    if pid:
                        forked.append(pid)
                        os.kill(os.getpid(), signum)
                    return pid
                previous = signal.signal(signum, raising_handler)
                try:
                    with mock.patch.object(probe.os, "fork", queued_fork):
                        result = fixture.run()
                finally:
                    signal.signal(signum, previous)
                self.assert_failure(result)
                self.assertEqual(len(forked), 1)
                self.assertEqual(observations, [forked])
                self.assertEqual([child.pid for child in fixture.children], forked)
                self.assertEqual([item["pid"] for item in result["child_cleanup"]
                                  if item.get("reaped")], forked)

    def test_signal_after_waitpid_is_deferred_until_reap_bookkeeping(self):
        real_waitpid, real_kill = os.waitpid, os.kill
        for signum in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
            with self.subTest(signum=signum):
                fixture = self.fixture()
                fixture.leaf += "-reap-" + str(signum)
                self.last_fixture = fixture
                reaped_pid = []
                observations = []
                kills = []
                def raising_handler(*args):
                    child = next(child for child in fixture.children if child.pid == reaped_pid[0])
                    observations.append((child.reaped, [item.copy() for item in fixture.cleanup]))
                    raise TimeoutError("injected post-waitpid signal")
                def queued_waitpid(pid, options):
                    found, status = real_waitpid(pid, options)
                    if found and not reaped_pid:
                        reaped_pid.append(found)
                        os.kill(os.getpid(), signum)
                    return found, status
                def recorded_kill(pid, sent):
                    kills.append((pid, sent))
                    return real_kill(pid, sent)
                previous = signal.signal(signum, raising_handler)
                try:
                    with mock.patch.object(probe.os, "waitpid", queued_waitpid), \
                            mock.patch.object(probe.os, "kill", recorded_kill):
                        result = fixture.run()
                finally:
                    signal.signal(signum, previous)
                self.assert_failure(result)
                self.assertEqual(len(reaped_pid), 1)
                self.assertEqual(len(observations), 1)
                self.assertTrue(observations[0][0])
                receipts = [item for item in observations[0][1] if item.get("pid") == reaped_pid[0]]
                self.assertEqual(receipts, [{"pid": reaped_pid[0], "status": 0, "reaped": True}])
                self.assertNotIn((reaped_pid[0], signal.SIGTERM), kills)

    def test_noop_lock_falsepass_detected(self):
        with mock.patch.object(probe, "lock", lambda *args, **kwargs: None):
            self.assert_failure(self.run_fixture())
        self.assertTrue(Path(self.temp.name, "hfs-lock-test", "posix.lock").exists())

    def test_false_getlk_unlocked_detected(self):
        with mock.patch.object(probe, "getlk", lambda fd: {"type": probe.fcntl.F_UNLCK}):
            result = self.run_fixture()
        self.assert_failure(result)
        self.assertIn("GETLK", result["error"])

    def test_incomplete_protocol_and_nonzero_exit_fail(self):
        def broken(*args):
            os._exit(12)
        with mock.patch.object(probe, "worker", broken):
            self.assert_failure(self.run_fixture())
        self.assertTrue(any(item.get("status", 0) != 0 for item in self.last_fixture.cleanup))

    def test_child_timeout_is_failure_and_term_reaped(self):
        def stalled(sock, *args):
            while True:
                sock.recv(1)
        with mock.patch.object(probe, "worker", stalled):
            self.assert_failure(self.run_fixture())
        self.assertTrue(any(item.get("term_sent") for item in self.last_fixture.cleanup))

    def test_bad_child_pid_protocol_fails(self):
        def wrong(sock, *args):
            sock.sendall(b'{"event":"ready","pid":0}\n')
        with mock.patch.object(probe, "worker", wrong):
            self.assert_failure(self.run_fixture())

    def test_cleanup_failure_cannot_pass(self):
        original = probe.Child.reap
        def failed_receipt(child, deadline):
            original(child, deadline)
            return False
        with mock.patch.object(probe.Child, "reap", failed_receipt):
            self.assert_failure(self.run_fixture())

    def test_no_kernel_wait_evidence_cannot_pass(self):
        original = probe.Fixture.wait_then_release
        def fail_wait(fixture, child, family, release):
            with mock.patch("builtins.open", mock.mock_open(read_data="")):
                return original(fixture, child, family, release)
        with mock.patch.object(probe.Fixture, "wait_then_release", fail_wait):
            result = self.run_fixture()
        self.assert_failure(result)
        self.assertIn("no kernel entered-wait evidence", result["error"])

    def test_existing_leaf_never_adopted_or_deleted(self):
        leaf = Path(self.temp.name, "hfs-lock-test")
        leaf.mkdir()
        sentinel = leaf / "sentinel"
        sentinel.write_text("retain")
        self.assert_failure(self.run_fixture())
        self.assertEqual(sentinel.read_text(), "retain")
        self.assertEqual(list(leaf.iterdir()), [sentinel])

    def test_symlink_leaf_never_followed(self):
        target = Path(self.temp.name, "target")
        target.mkdir()
        Path(self.temp.name, "hfs-lock-test").symlink_to(target)
        self.assert_failure(self.run_fixture())
        self.assertEqual(list(target.iterdir()), [])

    def test_real_non9p_mount_refused_before_mutation(self):
        with self.assertRaises(probe.Refused):
            probe.admit(self.request())
        self.assertEqual(os.listdir(self.root), [])

    def test_invalid_missing_and_symlink_mounts_refused_before_mutation(self):
        for changed in ({"run_id": "../escape"}, {"mount_id": 0},
                        {"mountpoint": self.temp.name + "/missing"},
                        {"budgets": {"overall_seconds": 61, "case_seconds": 1}}):
            with self.subTest(changed=changed), self.assertRaises(probe.Refused):
                probe.admit(self.request(**changed))
        Path(self.temp.name, "link").symlink_to(self.temp.name)
        with self.assertRaises(probe.Refused):
            probe.admit(self.request(mountpoint=self.temp.name + "/link"))
        self.assertEqual(os.listdir(self.root), ["link"])

    def test_retained_descriptor_mount_mismatch_refused(self):
        request = self.request()
        with mock.patch.object(probe, "mount_record", return_value={}), \
                mock.patch.object(probe, "fd_mount_id", return_value=-1):
            with self.assertRaises(probe.Refused):
                probe.admit(request)
        self.assertEqual(os.listdir(self.root), [])

    def test_kernel_waiter_native_and_9p_flock_representations(self):
        expected = {"dev": os.makedev(0, 21), "ino": 42}
        for kind in ("FLOCK", "POSIX"):
            line = "3: -> " + kind + " ADVISORY WRITE 123 00:15:42 0 EOF"
            self.assertEqual(probe.waiter_record(line, 123, expected, "flock"), line)
            self.assertIsNone(probe.waiter_record(line, 456, expected, "flock"))
        posix = "3: -> POSIX ADVISORY WRITE 123 00:15:42 0 7"
        self.assertEqual(probe.waiter_record(posix, 123, expected, "posix"), posix)
        for broken in (posix.replace("0 7", "0 EOF"), posix.replace(":42", ":43"),
                       posix.replace("POSIX", "FLOCK")):
            with self.assertRaises(probe.Refused):
                probe.waiter_record(broken, 123, expected, "posix")

    def test_exact_mount_source_and_id_checked(self):
        line = "17 1 0:99 / /mnt/test rw - 9p exact rw\n"
        with mock.patch("builtins.open", mock.mock_open(read_data=line)):
            self.assertEqual(probe.mount_record("/mnt/test", "exact", 17)["fstype"], "9p")
            for path, source, mount_id in (("/mnt", "exact", 17), ("/mnt/test", "wrong", 17), ("/mnt/test", "exact", 18)):
                with self.subTest(path=path, source=source, mount_id=mount_id), self.assertRaises(probe.Refused):
                    probe.mount_record(path, source, mount_id)


if __name__ == "__main__":
    unittest.main()
