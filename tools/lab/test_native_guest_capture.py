#!/usr/bin/env python3
"""Real local child tests of shipped capture; no SSH, VM, mount or ZFS."""

import json
import os
from pathlib import Path
import signal
import resource
import subprocess
import sys
import tempfile
import time
import unittest


GUEST = Path(__file__).with_name("native-single-node-guest.sh")


class CaptureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir="/tmp", prefix="hfs-capture-")
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        (self.work / "bin").mkdir()
        (self.work / "evidence").mkdir()
        source = GUEST.read_text().split("<<'SUPERVISOR' || status=$?\n", 1)[1].split("\nSUPERVISOR\n", 1)[0]
        self.assertEqual(source.count("child.wait(timeout=95)"), 1)
        self.assertEqual(source.count("child.wait(timeout=10)"), 1)
        self.source = source.replace("child.wait(timeout=95)", "child.wait(timeout=0.5)").replace("child.wait(timeout=10)", "child.wait(timeout=0.2)")

    def run_child(self, body):
        binary = self.work / "bin/homelabfs"
        if body is not None:
            binary.write_text(f"#!{sys.executable}\nimport os, signal, time, resource\nfrom pathlib import Path\nwork = Path({str(self.work)!r})\n" + body)
            binary.chmod(0o700)
        supervisor = self.work / "supervisor.py"
        supervisor.write_text(self.source)
        def shell_soft_limit():
            _, hard = resource.getrlimit(resource.RLIMIT_FSIZE)
            resource.setrlimit(resource.RLIMIT_FSIZE, (1 << 20, hard))
        result = subprocess.run([sys.executable, str(supervisor), str(self.work), "root", "hfs-direct", "no", "a" * 64], capture_output=True, timeout=4, preexec_fn=shell_soft_limit)
        prefix = self.work / "evidence/root-hfs-direct"
        receipt = json.loads(Path(str(prefix) + ".exit.json").read_text())
        if receipt["preserved_live_process"]:
            self.live_pid = receipt["pid"]
            self.addCleanup(self.stop_owned_child)
        return result, receipt, prefix

    def stop_owned_child(self):
        pid = getattr(self, "live_pid", None)
        if pid is None:
            return
        self.live_pid = None
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass

    def test_child_refuses_an_inherited_hard_limit_below_dataset_budget(self):
        supervisor = self.work / "supervisor.py"
        supervisor.write_text(self.source)
        binary = self.work / "bin/homelabfs"
        binary.write_text(f"#!{sys.executable}\nraise SystemExit('unexpected execution')\n")
        binary.chmod(0o700)
        def restrictive_limit():
            resource.setrlimit(resource.RLIMIT_FSIZE, (262144, 262144))
        result = subprocess.run([sys.executable, str(supervisor), str(self.work), "root", "hfs-direct", "no", "a" * 64],
                                capture_output=True, timeout=4, preexec_fn=restrictive_limit)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"inherited hard file limit below disposable dataset budget", result.stderr)
        self.assertFalse((self.work / "evidence/root-hfs-direct.process.json").exists())

    def test_normal_exit_and_data_file_exceeding_old_limits(self):
        result, receipt, prefix = self.run_child("""
with open(work / 'data', 'wb') as f:
    f.write(b'x' * (4 << 20))
os.write(1, b'normal stdout\\n')
os.write(2, b'normal stderr\\n')
""")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.work / "data").stat().st_size, 4 << 20)
        self.assertEqual(Path(str(prefix) + ".stdout").read_bytes(), b"normal stdout\n")
        self.assertEqual(Path(str(prefix) + ".stderr").read_bytes(), b"normal stderr\n")
        self.assertFalse(receipt["term_sent"])
        self.assertFalse(receipt["timed_out"])
        self.assertFalse(receipt["preserved_live_process"])

    def test_exact_cap_on_both_streams_exits_normally(self):
        result, receipt, prefix = self.run_child("os.write(1, b'o' * 262144)\nos.write(2, b'e' * 262144)\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        for name in ("stdout", "stderr"):
            self.assertEqual(Path(str(prefix) + "." + name).stat().st_size, 262144)
            self.assertFalse(receipt["capture"][name]["exceeded"])
        self.assertFalse(receipt["term_sent"])

    def test_flood_ignoring_term_is_preserved_without_a_second_term(self):
        result, receipt, prefix = self.run_child("""
count = 0
def terminated(signum, frame):
    global count
    count += 1
    (work / 'terms').write_text(str(count))
    if count > 1:
        raise SystemExit(0)
signal.signal(signal.SIGTERM, terminated)
while True:
    os.write(1, b'x' * 65536)
""")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertTrue(receipt["preserved_live_process"])
        self.assertTrue(receipt["timed_out"])
        self.assertTrue(receipt["capture"]["stdout"]["exceeded"])
        self.assertEqual(Path(str(prefix) + ".stdout").stat().st_size, 262144)
        self.assertEqual((self.work / "terms").read_text(), "1")
        os.kill(receipt["pid"], 0)
        self.stop_owned_child()
        self.wait_second_term()

    def wait_second_term(self):
        deadline = time.monotonic() + 2
        while (self.work / "terms").read_text() != "2" and time.monotonic() < deadline:
            time.sleep(0.01)
        self.assertEqual((self.work / "terms").read_text(), "2")

    def test_stdout_flood_is_capped(self):
        self.check_flood(1)

    def test_stderr_flood_is_capped(self):
        self.check_flood(2)

    def check_flood(self, fd):
        result, receipt, prefix = self.run_child(f"""
count = 0
def terminated(signum, frame):
    global count
    count += 1
    (work / 'terms').write_text(str(count))
    raise SystemExit(0)
signal.signal(signal.SIGTERM, terminated)
while True:
    os.write({fd}, b'x' * 65536)
""")
        self.assertEqual(result.returncode, 10, result.stderr)
        self.assertEqual((self.work / "terms").read_text(), "1")
        name = "stdout" if fd == 1 else "stderr"
        self.assertEqual(Path(str(prefix) + "." + name).stat().st_size, 262144)
        self.assertTrue(receipt["capture"][name]["exceeded"])
        self.assertTrue(receipt["term_sent"])
        self.assertFalse(receipt["preserved_live_process"])

    def test_deadline_sends_one_term_and_reaps(self):
        result, receipt, _ = self.run_child("time.sleep(5)\n")
        self.assertEqual(result.returncode, 30, result.stderr)
        self.assertTrue(receipt["term_sent"])
        self.assertTrue(receipt["timed_out"])
        self.assertEqual(receipt["exit_code"], -signal.SIGTERM)
        self.assertFalse(receipt["preserved_live_process"])

    def test_live_go_child_keeps_read_guards_after_supervisor_exits(self):
        build_tmp = self.work / "build-tmp"
        build_tmp.mkdir()
        env = dict(os.environ, GOMAXPROCS="2", GOFLAGS="-p=1", TMPDIR=str(build_tmp))
        subprocess.run(["go", "build", "-o", str(self.work / "bin/homelabfs"),
                        str(GUEST.with_name("testdata") / "capture-child.go")],
                       check=True, env=env, capture_output=True, timeout=30)
        result, receipt, _ = self.run_child(None)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertTrue(receipt["preserved_live_process"])
        self.assertEqual(len(receipt["retained_pipe_read_fds"]), 2)
        deadline = time.monotonic() + 2
        while not (self.work / "survived").exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        self.assertEqual((self.work / "survived").read_text(), "yes")
        os.kill(receipt["pid"], 0)
        self.assertEqual((self.work / "terms").read_text(), "1")
        self.stop_owned_child()
        self.wait_second_term()

if __name__ == "__main__":
    unittest.main()
