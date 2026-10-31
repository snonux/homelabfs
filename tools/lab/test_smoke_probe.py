"""Local-only tests: temporary files, no mounts, guests or remote transports."""
import importlib.util
import io
import json
import os
import pathlib
import runpy
import signal
import tempfile
import time
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("smoke_probe", pathlib.Path(__file__).with_name("smoke-probe.py"))
probe_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe_module)


class SmokeProbeTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.previous = os.open(".", os.O_RDONLY | os.O_DIRECTORY)
        os.chdir(self.directory.name)
        request = {"protocol": "hfs-live-v1", "volume": "hfs-smoke-unit-fake", "operation": "L02", "plan_hash": "a" * 64,
                   "plan": {"live": {"volumes": [{"id": "hfs-smoke-unit-fake"}],
                                     "max_operations": 1000, "uid": 21001, "gid": 21001, "denied_uid": 21002},
                            "budget": {"io_bytes": 16 << 20}}}
        self.probe = probe_module.Probe(request)

    def tearDown(self):
        os.fchdir(self.previous)
        os.close(self.previous)
        self.directory.cleanup()

    def test_write_sync_reopen(self):
        self.probe.bytes_io()
        self.assertEqual(self.probe.checks, ["write-fsync-reopen-checksum"])
        self.assertEqual(self.probe.history[0]["length"], 4096)

    def test_metadata_semantics(self):
        self.probe.semantics()
        self.assertEqual(self.probe.checks, ["mkdir-rmdir", "rename-replace", "truncate", "hardlink", "unlink-open"])

    def test_two_process_locks_and_mmap(self):
        self.probe.locking()
        self.assertEqual(self.probe.checks, ["flock-contention", "fcntl-contention", "mmap-flush-sync"])

    def test_sqlite_modes_reopen_integrity(self):
        self.probe.databases()
        self.assertEqual(self.probe.checks, ["sqlite-delete-full", "sqlite-wal-full", "sqlite-reopen-integrity"])

    def test_bounded_independent_file_mix(self):
        self.probe.mix()
        self.assertEqual(self.probe.checks, ["bounded-mix"])
        self.assertEqual(len(os.listdir(".")), 8)

    def test_budget_rejected_before_payload(self):
        self.probe.byte_limit = 1
        with self.assertRaisesRegex(RuntimeError, "reservation exhausted"):
            self.probe.bytes_io()
        self.assertEqual(os.listdir("."), [])

    def assert_children_reaped(self, pids):
        self.assertEqual([child.pid for child in self.probe.children], pids)
        self.assertTrue(all(child.reaped for child in self.probe.children))
        self.assertEqual(self.probe.child_evidence()["uncertain_children"], [])
        self.assertEqual([item["pid"] for item in self.probe.child_cleanup if item.get("reaped")], pids)
        for pid in pids:
            with self.assertRaises(ChildProcessError):
                os.waitpid(pid, os.WNOHANG)

    def cleanup_owned(self, pids):
        # Independent fallback keeps even a failing race regression disposable.
        for pid in pids:
            try:
                found, _ = os.waitpid(pid, os.WNOHANG)
            except ChildProcessError:
                continue
            if not found:
                os.kill(pid, signal.SIGKILL)
                deadline = time.monotonic() + 2
                while not os.waitpid(pid, os.WNOHANG)[0]:
                    if time.monotonic() >= deadline:
                        self.fail("local regression child did not terminate")
                    time.sleep(0.01)

    def test_signal_after_fork_is_deferred_until_registration(self):
        real_fork, real_kill = os.fork, os.kill
        for signum in probe_module.CHILD_SIGNALS:
            with self.subTest(signum=signum):
                self.probe.children.clear()
                self.probe.child_cleanup.clear()
                forked, observations = [], []

                def raising_handler(*_):
                    observations.append([child.pid for child in self.probe.children])
                    raise TimeoutError("injected post-fork signal")

                def queued_fork():
                    pid = real_fork()
                    if pid:
                        forked.append(pid)
                        real_kill(os.getpid(), signum)
                    return pid

                previous = signal.signal(signum, raising_handler)
                try:
                    with mock.patch.object(probe_module.os, "fork", queued_fork):
                        with self.assertRaisesRegex(TimeoutError, "post-fork signal"):
                            self.probe.child(lambda: time.sleep(5))
                    self.assertEqual(observations, [forked])
                    self.assertEqual(len(forked), 1)
                    self.assert_children_reaped(forked)
                finally:
                    signal.signal(signum, previous)
                    self.cleanup_owned(forked)

    def test_signal_after_waitpid_is_deferred_until_reaped_receipt(self):
        real_waitpid, real_kill = os.waitpid, os.kill
        for signum in probe_module.CHILD_SIGNALS:
            with self.subTest(signum=signum):
                self.probe.children.clear()
                self.probe.child_cleanup.clear()
                reaped, observations, kills = [], [], []

                def raising_handler(*_):
                    observations.append((self.probe.children[0].reaped,
                                         [item.copy() for item in self.probe.child_cleanup]))
                    raise TimeoutError("injected post-waitpid signal")

                def queued_waitpid(pid, options):
                    found, status = real_waitpid(pid, options)
                    if found:
                        reaped.append(found)
                        real_kill(os.getpid(), signum)
                    return found, status

                def recorded_kill(pid, sent):
                    kills.append((pid, sent))
                    self.assertNotIn(pid, reaped, "signaled a known reaped PID")
                    return real_kill(pid, sent)

                previous = signal.signal(signum, raising_handler)
                try:
                    with mock.patch.object(probe_module.os, "waitpid", queued_waitpid), \
                            mock.patch.object(probe_module.os, "kill", recorded_kill):
                        with self.assertRaisesRegex(TimeoutError, "post-waitpid signal"):
                            self.probe.child(lambda: None)
                    self.assertEqual(len(reaped), 1)
                    self.assertEqual(observations, [(True, [{"pid": reaped[0], "status": 0, "reaped": True}])])
                    self.assertEqual(kills, [])
                    self.assert_children_reaped(reaped)
                finally:
                    signal.signal(signum, previous)
                    self.cleanup_owned([child.pid for child in self.probe.children])

    def test_entry_alarm_interrupts_child_wait_and_preserves_original_failure(self):
        # Use the same raising handler as enter(), with a short real timer.
        previous_handler = signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("probe deadline")))
        previous_timer = signal.setitimer(signal.ITIMER_REAL, 0.05)
        before = signal.pthread_sigmask(signal.SIG_BLOCK, set())
        started = time.monotonic()
        try:
            with self.assertRaisesRegex(TimeoutError, "probe deadline"):
                self.probe.child(lambda: time.sleep(5))
            self.assertLess(time.monotonic() - started, 2)
            self.assertEqual(signal.pthread_sigmask(signal.SIG_BLOCK, set()), before)
            self.assert_children_reaped([self.probe.children[0].pid])
            self.assertTrue(any(item.get("kill_sent") for item in self.probe.child_cleanup))
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous_handler)
            signal.setitimer(signal.ITIMER_REAL, *previous_timer)
            self.cleanup_owned([child.pid for child in self.probe.children])

    def test_cleanup_timeout_retains_uncertainty_and_original_failure(self):
        real_fork, real_kill = os.fork, os.kill
        forked = []

        def queued_fork():
            pid = real_fork()
            if pid:
                forked.append(pid)
                real_kill(os.getpid(), signal.SIGALRM)
            return pid

        previous = signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("original alarm")))
        started = time.monotonic()
        try:
            with mock.patch.object(probe_module.os, "fork", queued_fork), \
                    mock.patch.object(probe_module.os, "waitpid", return_value=(0, 0)), \
                    mock.patch.object(probe_module, "CHILD_CLEANUP_SECONDS", 0.03):
                with self.assertRaisesRegex(TimeoutError, "original alarm"):
                    self.probe.child(lambda: time.sleep(5))
            self.assertLess(time.monotonic() - started, 1)
            self.assertEqual(self.probe.child_evidence()["uncertain_children"], forked)
            self.assertFalse(any(item.get("reaped") for item in self.probe.child_cleanup))
        finally:
            signal.signal(signal.SIGALRM, previous)
            self.cleanup_owned(forked)

    def test_lost_child_ownership_never_signals_pid(self):
        child = probe_module.Child(self.probe)
        child.pid = 123456
        self.probe.children.append(child)
        with mock.patch.object(probe_module.os, "waitpid", side_effect=ChildProcessError("no owned child")), \
                mock.patch.object(probe_module.os, "kill") as kill:
            with self.assertRaises(ChildProcessError):
                child.reap()
            child.cleanup()
        kill.assert_not_called()
        self.assertEqual(self.probe.child_evidence()["uncertain_children"], [child.pid])

    def test_failed_child_returns_false_and_fork_failure_has_no_owned_pid(self):
        self.assertFalse(self.probe.child(lambda: (_ for _ in ()).throw(RuntimeError("child failure"))))
        self.assert_children_reaped([self.probe.children[0].pid])
        with mock.patch.object(probe_module.os, "fork", side_effect=OSError("fork refused")), \
                mock.patch.object(probe_module.os, "kill") as kill:
            with self.assertRaisesRegex(OSError, "fork refused"):
                self.probe.child(lambda: None)
        kill.assert_not_called()
        self.assertEqual(len(self.probe.children), 1)

    def test_main_distinguishes_terminal_failure_from_uncertain_children(self):
        for uncertain, expected_exit in (([], 10), ([123456], 1)):
            with self.subTest(uncertain=uncertain):
                evidence = {"child_cleanup": [], "uncertain_children": uncertain}
                stdin = mock.Mock(buffer=io.BytesIO(json.dumps(self.probe.request).encode()))
                output = io.StringIO()
                with mock.patch.object(probe_module.sys, "stdin", stdin), \
                        mock.patch.object(probe_module.sys, "stdout", output), \
                        mock.patch.object(probe_module.Probe, "run", side_effect=TimeoutError("original alarm")), \
                        mock.patch.object(probe_module.Probe, "child_evidence", return_value=evidence):
                    self.assertEqual(probe_module.main(), expected_exit)
                result = json.loads(output.getvalue())
                self.assertEqual(result["error"], "original alarm")
                self.assertEqual(result["uncertain_children"], uncertain)

    def test_entrypoint_reporting_failure_retains_uncertain_exit(self):
        reports = []
        stdin = mock.Mock(buffer=io.BytesIO(json.dumps(self.probe.request).encode()))

        def fail_first_report(value):
            reports.append(json.loads(value))
            if len(reports) == 1:
                raise OSError("injected failure while reporting probe error")

        with mock.patch.object(probe_module.sys, "stdin", stdin), \
                mock.patch("builtins.print", side_effect=fail_first_report):
            with self.assertRaises(SystemExit) as result:
                runpy.run_path(probe_module.__file__, run_name="__main__")
        self.assertEqual(result.exception.code, 1)
        self.assertEqual(len(reports), 2)
        self.assertIn("error", reports[0])
        self.assertEqual(reports[1]["error"], "injected failure while reporting probe error")

    def test_raw_fallback_mount_rejected_before_directory_creation(self):
        self.probe.plan.update({"stage": "stage2", "profile": "live-smoke", "run_id": "unit", "disruptions": []})
        mount = "/mnt/hfs-smoke/hfs-smoke-unit-fake"
        self.probe.volume.update({"guest_mount": mount, "export": "hfs-smoke-unit-fake"})
        record = "23 1 0:1 / " + mount + " rw - ext4 /dev/fake rw\n"
        with mock.patch("builtins.open", mock.mock_open(read_data=record)), mock.patch.object(
                os.path, "realpath", side_effect=lambda path: path), mock.patch.object(os, "mkdir") as create:
            with self.assertRaisesRegex(RuntimeError, "exact deployed 9p mount/export"):
                self.probe.enter()
            create.assert_not_called()

    @unittest.skipUnless(os.geteuid() == 0, "numeric identity switching requires root")
    def test_numeric_identities(self):
        self.probe.permissions()
        self.assertEqual(self.probe.checks, ["uid-gid-mode", "allowed-access", "denied-access"])


if __name__ == "__main__":
    unittest.main()
