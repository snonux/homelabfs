#!/usr/bin/env python3
"""Offline tests for the zq3 outer runtime guard (task v34, gap G13).

Every FreeBSD boundary (ps, sysctl, swapinfo, /dev/vmm, statvfs, spawn,
wait and signal delivery) is substituted by FakeHost; the run directory and
its durable records are real temporary files. The central negative: whatever
breaks, the only signal ever sent is one SIGTERM to the direct runtime child,
never to bhyve, Rocky or a process group.
"""
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import tempfile
import tokenize
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("runtime_guard", HERE / "runtime-guard.py")
GUARD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GUARD)

GIB = GUARD.GIB
PIN = "a" * 64
RUNTIME, EXPORTER, OBSERVER, ROCKY = 1000, 1001, 1002, 80203
LSTART = "Sun Nov  8 18:00:00 2026"
# setproctitle on an image exec'd as /dev/fd/4 prefixes the image name "4".
EXPORTER_TITLE = "4: hfs-zq3-r1 (4)"


class FakeChild:
    def __init__(self, pid):
        self.pid = pid
        self.alive = True
        self.code = None


class FakeHost:
    """Scripted host: tick() advances the clock and applies scenario hooks."""

    def __init__(self, root):
        self.root = root
        self.clock = 100.0
        self.admit = True
        self.pin = PIN
        self.child = None
        self.signals = []
        self.spawned = []
        self.runtime_rss = 100 << 20
        self.exporter_rss = 2 * GIB
        self.contexts = []
        self.extra_rows = {}
        self.disk = 1 << 20
        self.free = 100 * GIB
        self.swap = 0
        self.available = 4 * GIB
        self.exit_after = None      # seconds after spawn when the runtime exits
        self.exit_on_term = True
        self.hooks = {}             # elapsed second -> callable(host)
        self.fail_processes = False
        self.mounts = set()
        self.skips_seen = []
        self.missing_roots = set()

    # Admission boundaries.
    def admitted(self):
        return self.admit

    def lstat(self, path):
        if Path(path) in self.missing_roots:
            raise FileNotFoundError(path)
        if Path(path) == self.root:
            return os.stat_result((stat.S_IFDIR | 0o700, 0, 0, 0, 0, 0, 0, 0, 0, 0))
        return os.lstat(path)

    def digest(self, path):
        return self.pin

    # Observation boundaries.
    def processes(self):
        if self.fail_processes:
            raise ValueError("process observation shape")
        rows = dict(self.extra_rows)
        if self.child is not None and self.child.alive:
            rows[RUNTIME] = (1, self.runtime_rss, LSTART, "/var/tmp/x/bin/hfs-controlled-runtime --config /c")
            rows[OBSERVER] = (RUNTIME, 8 << 20, LSTART, "/dev/fd/3 -T root@rocky")
        if self.child is not None:
            parent = RUNTIME if self.child.alive else 1
            rows[EXPORTER] = (parent, self.exporter_rss, LSTART, EXPORTER_TITLE)
        return rows

    def vm_contexts(self):
        return list(self.contexts)

    def memory(self):
        return {"available_bytes": self.available}

    def swap_used(self):
        return self.swap

    def allocated(self, root, skip):
        self.skips_seen.append(set(skip))
        return self.disk

    def mountpoints(self, roots):
        return set(self.mounts)

    def free_bytes(self, root):
        return self.free

    # Process boundaries.
    def spawn(self, argv, cwd, stdout, stderr):
        self.spawned.append(argv)
        self.child = FakeChild(RUNTIME)
        self.contexts = ["hfs-zq3-r1"]
        self.spawn_time = self.clock
        return self.child

    def running(self, child):
        return child.alive

    def terminate(self, child):
        self.signals.append((child.pid, "SIGTERM"))
        if self.exit_on_term:
            child.alive, child.code = False, 0

    def reap(self, child):
        assert not child.alive, "reaping a running child"
        return child.code

    def now(self):
        return self.clock

    def sleep(self, seconds):
        self.clock += seconds
        elapsed = int(self.clock - self.spawn_time)
        for second in [s for s in self.hooks if s <= elapsed]:
            self.hooks.pop(second)(self)
        if self.exit_after is not None and elapsed >= self.exit_after and self.child.alive:
            self.child.alive, self.child.code = False, 0


class GuardTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.host = FakeHost(self.root)
        self.host.exit_after = 5

    def argv(self, *extra, command=None):
        command = command or ["/var/tmp/x/bin/hfs-controlled-runtime", "--config", "/var/tmp/x/config.json"]
        return ["--run-dir", str(self.root / "guard"), "--runtime-sha256", PIN,
                "--vm-name", "hfs-zq3-r1", "--disk-root", str(self.root),
                "--interval", "1", "--stop-grace", "3", "--observe-after-stop", "4",
                *extra, "--", *command]

    def run_guard(self, *extra, **kwargs):
        with patch("builtins.print"):
            return GUARD.run(GUARD.parse_args(self.argv(*extra, **kwargs)), self.host)

    def receipt(self):
        return json.loads((self.root / "guard/receipt.json").read_text())

    def breach_kinds(self):
        return [kind for kind, _ in json.loads((self.root / "guard/breach.json").read_text())["violations"]]

    def assert_only_runtime_signalled(self, count=1):
        self.assertEqual(self.host.signals, [(RUNTIME, "SIGTERM")] * count)

    def test_clean_runtime_exit(self):
        self.assertEqual(self.run_guard(), 0)
        receipt = self.receipt()
        self.assertEqual(receipt["runtime_exit"], 0)
        self.assertIsNone(receipt["breach"])
        self.assertEqual(self.host.signals, [])
        # bhyve outlives the runtime (run plan R4); the guard reports, never acts.
        self.assertTrue(receipt["exporter_alive_at_guard_exit"])
        self.assertEqual(receipt["exporter"]["pid"], EXPORTER)
        intent = json.loads((self.root / "guard/intent.json").read_text())
        self.assertEqual(intent["environment"]["PATH"], "/sbin:/bin:/usr/sbin:/usr/bin")
        self.assertTrue((self.root / "guard/observations.jsonl").read_text().count("\n") >= 5)

    def test_limit_breaches_stop_only_the_runtime(self):
        cases = {
            "runtime-rss": lambda h: setattr(h, "runtime_rss", 4 * GIB + 256 * GUARD.MIB + 1),
            "exporter-rss": lambda h: setattr(h, "exporter_rss", 6 * GIB + 512 * GUARD.MIB + 1),
            "disk": lambda h: setattr(h, "disk", 4 * GIB + 1),
            "free-disk": lambda h: setattr(h, "free", 8 * GIB - 1),
            "swap": lambda h: setattr(h, "swap", 4096),
            "memory-floor": lambda h: setattr(h, "available", 512 * GUARD.MIB - 1),
        }
        for kind, change in cases.items():
            with self.subTest(kind=kind):
                self.setUp()
                self.host.exit_after = None
                self.host.hooks = {2: change}
                self.assertEqual(self.run_guard(), 1)
                self.assertEqual(self.breach_kinds(), [kind])
                self.assert_only_runtime_signalled()
                self.assertTrue(self.receipt()["stop_requested"])

    def test_time_bound(self):
        self.host.exit_after = None
        self.assertEqual(self.run_guard("--max-seconds", "3"), 1)
        self.assertEqual(self.breach_kinds(), ["time"])
        self.assert_only_runtime_signalled()

    def test_rocky_started_concurrently_is_reported_never_signalled(self):
        def rocky(host):
            host.contexts = ["hfs-zq3-r1", "rocky"]
            host.extra_rows[ROCKY] = (1, 12 * GIB, LSTART, "bhyve: rocky (bhyve)")
        self.host.exit_after = None
        self.host.hooks = {2: rocky}
        self.assertEqual(self.run_guard(), 1)
        self.assertEqual(self.breach_kinds(), ["foreign-vm-context", "foreign-bhyve"])
        self.assert_only_runtime_signalled()
        self.assertNotIn(ROCKY, [pid for pid, _ in self.host.signals])

    def test_report_policy_never_signals(self):
        self.host.hooks = {2: lambda h: setattr(h, "runtime_rss", 7 * GIB)}
        self.assertEqual(self.run_guard("--on-breach", "report"), 1)
        self.assertEqual(self.breach_kinds(), ["runtime-rss"])
        self.assertEqual(self.host.signals, [])
        self.assertEqual(self.receipt()["runtime_exit"], 0)

    def test_runtime_ignoring_sigterm_is_left_running(self):
        self.host.exit_after = None
        self.host.exit_on_term = False
        self.host.hooks = {1: lambda h: setattr(h, "swap", 1)}
        self.assertEqual(self.run_guard(), 3)
        self.assert_only_runtime_signalled()
        self.assertTrue((self.root / "guard/stop-uncertain.json").exists())
        receipt = self.receipt()
        self.assertTrue(receipt["runtime_running_at_guard_exit"])
        self.assertIsNone(receipt["runtime_exit"])
        self.assertTrue(self.host.child.alive)

    def test_guard_hangup_is_a_breach(self):
        self.host.exit_after = None
        self.host.hooks = {2: lambda h: os.kill(os.getpid(), signal.SIGHUP)}
        before = signal.getsignal(signal.SIGHUP)
        self.assertEqual(self.run_guard(), 1)
        self.assertEqual(self.breach_kinds(), ["guard-interrupted"])
        self.assertEqual(self.receipt()["interrupted"], "SIGHUP")
        self.assert_only_runtime_signalled()
        self.assertEqual(signal.getsignal(signal.SIGHUP), before)

    def test_observation_failure_is_a_breach(self):
        self.host.exit_after = None
        self.host.hooks = {2: lambda h: setattr(h, "fail_processes", True)}
        self.assertEqual(self.run_guard(), 1)
        self.assertEqual(self.breach_kinds(), ["observation-failed"])
        self.assert_only_runtime_signalled()

    def test_preflight_refuses_before_launch(self):
        cases = {"VM context exists": lambda h: setattr(h, "contexts", ["rocky"]),
                 "already violated": lambda h: setattr(h, "free", GIB),
                 "foreign-bhyve": lambda h: h.extra_rows.update(
                     {ROCKY: (1, GIB, LSTART, "bhyve: rocky (bhyve)")})}
        for pattern, change in cases.items():
            with self.subTest(pattern=pattern):
                self.setUp()
                change(self.host)
                self.assertEqual(self.run_guard(), 1)
                self.assertIn(pattern, self.receipt()["refused"])
                self.assertEqual(self.host.spawned, [])
                self.assertFalse((self.root / "guard/intent.json").exists())

    def test_admission_refusals(self):
        cases = {
            "FreeBSD root": ((), lambda h: setattr(h, "admit", False)),
            "differ from the pin": ((), lambda h: setattr(h, "pin", "b" * 64)),
            "max_seconds": (("--max-seconds", "7200"), None),
            "runtime_rss_bytes": (("--runtime-rss-bytes", str(7 * GIB)), None),
            "free_disk_bytes": (("--free-disk-bytes", str(GIB)), None),
            "memory_floor_bytes": (("--memory-floor-bytes", "0"), None),
            "no swap": (("--swap-limit-bytes", "1"), None),
        }
        for pattern, (extra, change) in cases.items():
            with self.subTest(pattern=pattern):
                self.setUp()
                if change:
                    change(self.host)
                with self.assertRaisesRegex(GUARD.Refused, pattern):
                    self.run_guard(*extra)
                self.assertEqual(self.host.spawned, [])
                self.assertFalse((self.root / "guard").exists())

    def test_command_and_directory_refusals(self):
        bad_commands = {
            "RUNTIME --config": ["/r", "--config", "/c", "--extra"],
            "clean absolute": ["/r", "--config", "relative.json"],
        }
        for pattern, command in bad_commands.items():
            with self.subTest(pattern=pattern), self.assertRaisesRegex(GUARD.Refused, pattern):
                self.run_guard(command=command)
        (self.root / "guard").mkdir()
        with self.assertRaisesRegex(GUARD.Refused, "already exists"):
            self.run_guard()
        args = GUARD.parse_args(self.argv())
        elsewhere = tempfile.TemporaryDirectory()
        self.addCleanup(elsewhere.cleanup)
        for field, value, pattern in (("vm_name", "rocky", "hfs-"),
                                      ("disk_root", [Path(elsewhere.name)], "count against"),
                                      ("disk_root", [self.root, Path("/nonexistent-v34")], "unavailable"),
                                      ("skip_path", [Path("/var/tmp/outside")], "strictly below"),
                                      ("disk_root", [self.root, self.root / "sub"], "non-nested")):
            with self.subTest(field=field, value=value):
                changed = GUARD.parse_args(self.argv())
                changed.run_dir = self.root / "fresh"
                setattr(changed, field, value)
                with self.assertRaisesRegex(GUARD.Refused, pattern):
                    GUARD.check_run_dir(changed, self.host)
        self.assertIsNotNone(args)

    def test_non_private_parent_refuses(self):
        host = self.host
        host.lstat = lambda path: os.stat_result((stat.S_IFDIR | 0o755, 0, 0, 0, 0, 0, 0, 0, 0, 0)) \
            if Path(path) == self.root else os.lstat(path)
        with self.assertRaisesRegex(GUARD.Refused, "root-private"):
            self.run_guard()

    def test_real_bhyve_title_uses_exporter_not_runtime_limit(self):
        # 6 GiB of bhyve RSS under its real "/dev/fd/4" title must not count
        # against the 4.25 GiB runtime envelope (review blocker B2).
        self.host.exporter_rss = 6 * GIB
        self.assertEqual(self.run_guard(), 0)
        receipt = self.receipt()
        self.assertEqual(receipt["peaks"]["exporter_rss"], 6 * GIB)
        self.assertLess(receipt["peaks"]["runtime_rss"], GIB)
        self.assertTrue(receipt["exporter_alive_at_guard_exit"])

    def test_mount_table_and_skip_paths_reach_the_disk_walk(self):
        mount = str(self.root / "volume/mnt")
        self.host.mounts = {mount}
        self.assertEqual(self.run_guard("--skip-path", str(self.root / "volume/native")), 0)
        self.assertTrue(self.host.skips_seen)
        for skip in self.host.skips_seen:
            self.assertEqual(skip, {mount, str(self.root / "volume/native")})

    def test_record_failure_still_stops_runtime(self):
        def readonly(host):
            os.chmod(self.root / "guard", 0o500)
        self.addCleanup(lambda: os.chmod(self.root / "guard", 0o700))
        self.host.exit_after = None
        self.host.hooks = {2: readonly}
        self.assertEqual(self.run_guard(), 1)
        self.assert_only_runtime_signalled()
        self.assertFalse((self.root / "guard/receipt.json").exists())

    def test_guard_bug_is_a_breach_and_stops_runtime(self):
        def broken(host):
            host.vm_contexts = lambda: (_ for _ in ()).throw(RuntimeError("guard bug"))
        self.host.exit_after = None
        self.host.hooks = {2: broken}
        self.assertEqual(self.run_guard(), 1)
        self.assertEqual(self.breach_kinds(), ["guard-failure"])
        self.assert_only_runtime_signalled()
        self.assertEqual(self.receipt()["runtime_exit"], 0)

    def test_guard_bug_in_report_mode_never_signals(self):
        def broken(host):
            host.vm_contexts = lambda: (_ for _ in ()).throw(RuntimeError("guard bug"))
        self.host.exit_after = None
        self.host.hooks = {2: broken}
        self.assertEqual(self.run_guard("--on-breach", "report"), 3)
        self.assertEqual(self.host.signals, [])

    def test_preflight_observation_error_refuses_cleanly(self):
        def failing(root, skip):
            raise PermissionError("unreadable staging entry")
        self.host.allocated = failing
        self.assertEqual(self.run_guard(), 1)
        self.assertIn("preflight observation failed", self.receipt()["refused"])
        self.assertEqual(self.host.spawned, [])

    def test_pre_retitle_bhyve_counts_as_exporter(self):
        guard = GUARD.Guard(GUARD.parse_args(self.argv()), self.host)
        self.assertTrue(guard.is_exporter("/dev/fd/4 -c 4 -m 6G -A -H -P hfs-zq3-r1"))
        self.assertTrue(guard.is_exporter(EXPORTER_TITLE))
        self.assertTrue(guard.is_exporter("bhyve: hfs-zq3-r1 (bhyve)"))
        self.assertFalse(guard.is_exporter("4: hfs-zq3-r10 (4)"))
        self.assertFalse(guard.is_exporter("/r --config /var/tmp/hfs-zq3-r1-x/config.json"))
        self.assertFalse(guard.is_exporter("bhyve: rocky (bhyve)"))
        self.assertFalse(guard.is_exporter("/dev/fd/3 -T root@rocky"))

    def test_no_group_or_kill_signal_in_source(self):
        # Code tokens only: docstrings and comments may explain what is banned.
        with open(HERE / "runtime-guard.py", "rb") as stream:
            names = [token.string for token in tokenize.tokenize(stream.readline)
                     if token.type == tokenize.NAME]
        for forbidden in ("killpg", "SIGKILL", "send_signal", "finish_process_group", "terminate_group"):
            self.assertNotIn(forbidden, names)
        self.assertEqual(names.count("kill"), 1)  # os.kill(child.pid, signal.SIGTERM)
        self.assertEqual(names.count("SIGTERM"), 2)  # the one kill and the handler list


class HostBoundaryTests(unittest.TestCase):
    def test_main_requires_isolated_interpreter(self):
        argv = ["--run-dir", "/var/tmp/x/guard", "--runtime-sha256", PIN, "--vm-name", "hfs-zq3-r1",
                "--disk-root", "/var/tmp/x", "--", "/r", "--config", "/c"]
        with patch.object(GUARD, "isolated_interpreter", return_value=False), \
                patch.object(GUARD, "run") as run, patch("builtins.print") as printed:
            self.assertEqual(GUARD.main(argv), 1)
        run.assert_not_called()
        self.assertIn("-I", printed.call_args[0][0])

    def test_ps_parsing(self):
        output = (f"  1000     1 102400 {LSTART} /var/tmp/x/bin/hfs-controlled-runtime --config /c\n"
                  f"  1001  1000 2097152 {LSTART} {EXPORTER_TITLE}\n"
                  f"    12     0      0 {LSTART} \n")
        with patch.object(GUARD.subprocess, "check_output", return_value=output) as ps:
            rows = GUARD.HostOS().processes()
        self.assertIn("-ww", ps.call_args[0][0])
        # lstart words are re-joined with single spaces; only equality matters.
        self.assertEqual(rows[1001], (1000, 2 * GIB, " ".join(LSTART.split()), EXPORTER_TITLE))
        self.assertEqual(rows[12][3], "")
        with patch.object(GUARD.subprocess, "check_output", return_value="1 2\n"):
            with self.assertRaises(ValueError):
                GUARD.HostOS().processes()

    def test_swap_and_memory_parsing(self):
        swap = ("Device          1K-blocks     Used    Avail Capacity\n"
                "/dev/ada0p3       2097152       16  2097136     0%\n"
                "/dev/ada1p3       2097152        4  2097148     0%\n"
                "Total             4194304       20  4194284     0%\n")
        with patch.object(GUARD.subprocess, "check_output", return_value=swap):
            self.assertEqual(GUARD.HostOS().swap_used(), 20 * 1024)
        with patch.object(GUARD.subprocess, "check_output", return_value="garbage\n"):
            with self.assertRaises(ValueError):
                GUARD.HostOS().swap_used()
        with patch.object(GUARD.subprocess, "check_output", return_value="4096\n100\n200\n5000\n1000\n"):
            memory = GUARD.HostOS().memory()
        self.assertEqual(memory["available_bytes"], 4096 * 300 + 4000)
        with patch.object(GUARD.subprocess, "check_output",
                          side_effect=subprocess.TimeoutExpired("sysctl", 5)):
            with self.assertRaises(subprocess.SubprocessError):
                GUARD.HostOS().memory()

    def test_disk_walk_does_not_cross_mounts(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "owned").write_bytes(b"x" * 65536)
            (root / "mnt").mkdir()
            (root / "mnt/fuse-data").write_bytes(b"y" * (1 << 20))
            whole = GUARD.allocated_bytes(root)
            real_lstat = os.lstat

            def mounted(path):
                info = real_lstat(path)
                if Path(path) == root / "mnt" or root / "mnt" in Path(path).parents:
                    values = list(info)
                    values[2] = info.st_dev + 1
                    return os.stat_result(values)
                return info

            with patch.object(GUARD.os, "lstat", side_effect=mounted):
                walled = GUARD.allocated_bytes(root)
            self.assertLess(walled, whole)
            self.assertGreaterEqual(whole - walled, 1 << 20)

    def test_disk_walk_never_lstats_a_skipped_mountpoint(self):
        # A FUSE mountpoint's lstat is a request to the runtime's own server:
        # it may fail or block forever. The walk must not issue it (B1).
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "owned").write_bytes(b"x" * 4096)
            (root / "mnt").mkdir()
            real_lstat = os.lstat

            def fuse_lstat(path):
                if Path(path) == root / "mnt":
                    raise AssertionError("blocked: lstat reached the FUSE mountpoint")
                return real_lstat(path)

            with patch.object(GUARD.os, "lstat", side_effect=fuse_lstat):
                self.assertGreater(GUARD.allocated_bytes(root, frozenset({str(root / "mnt")})), 0)
                with self.assertRaises(AssertionError):
                    GUARD.allocated_bytes(root)

    def test_host_disk_sample_skips_mount_table_entries(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "owned").write_bytes(b"x" * 4096)
            (root / "mnt").mkdir()
            table = (f"zroot/var/tmp /var/tmp zfs rw 0 0\n"
                     f"/dev/fuse {root}/mnt fusefs rw,nosuid 0 0\n")
            real_lstat = os.lstat

            def fuse_lstat(path):
                if Path(path) == root / "mnt":
                    raise OSError(5, "Input/output error (FUSE server)")
                return real_lstat(path)

            host = GUARD.HostOS()
            with patch.object(GUARD.subprocess, "check_output", return_value=table) as mount, \
                    patch.object(GUARD.os, "lstat", side_effect=fuse_lstat):
                skip = host.mountpoints([root])
                self.assertGreater(host.allocated(root, skip), 0)
            self.assertEqual(mount.call_args[0][0], ["/sbin/mount", "-p"])
            self.assertEqual(skip, {str(root / "mnt")})

    def test_mount_table_parsing(self):
        roots = [Path("/var/tmp/hfs-validation-xz3/zq3-r1")]
        table = ("zroot/var/tmp /var/tmp zfs rw 0 0\n"
                 "/dev/fuse /var/tmp/hfs-validation-xz3/zq3-r1/vol\\040a/mnt fusefs rw 0 0\n"
                 "odd line with spaces elsewhere /mnt x y z 0\n"
                 "devfs /dev devfs rw 0 0\n")
        self.assertEqual(GUARD.parse_mount_table(table, roots),
                         {"/var/tmp/hfs-validation-xz3/zq3-r1/vol a/mnt"})
        with self.assertRaisesRegex(ValueError, "ambiguous"):
            GUARD.parse_mount_table("a b /var/tmp/hfs-validation-xz3/zq3-r1/m fusefs rw 0 0\n", roots)
        with self.assertRaisesRegex(ValueError, "itself a mountpoint"):
            GUARD.parse_mount_table("/dev/fuse /var/tmp/hfs-validation-xz3/zq3-r1 fusefs rw 0 0\n", roots)

    def test_disk_walk_tolerates_vanished_entries(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "tmpfile").write_bytes(b"z")
            real_lstat = os.lstat

            def vanished(path):
                if Path(path).name == "tmpfile":
                    raise FileNotFoundError(path)
                return real_lstat(path)

            with patch.object(GUARD.os, "lstat", side_effect=vanished):
                self.assertGreaterEqual(GUARD.allocated_bytes(root), 0)
            with self.assertRaises(FileNotFoundError):
                GUARD.allocated_bytes(root / "missing-root")


class PlanAdmissionBarTests(unittest.TestCase):
    """zq3's Phase 6 bar is the enforced ceilings plus a 3 GiB host reserve (g54)."""

    def test_plan_bar_matches_ceilings(self):
        bar = (GUARD.CEILINGS["runtime_rss_bytes"] + GUARD.CEILINGS["exporter_rss_bytes"]
               + 3 * GIB)
        plan = (HERE.parents[1] / "docs" / "lab" / "zq3-run-plan.md")
        if not plan.exists():  # tools-only snapshots carry no docs
            self.skipTest("run plan not present")
        self.assertIn("{:,} bytes".format(bar), plan.read_text())


if __name__ == "__main__":
    unittest.main()
