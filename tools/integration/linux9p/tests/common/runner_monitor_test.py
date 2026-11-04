#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Exercise maintained monitor functions without invoking their build/test CLIs.

Only top-level function definitions are loaded from the real scripts. Children,
process groups, signals, waits, temporary paths and receipts are real. Temporary
evidence is retained under TMPDIR (set it to an owned test directory).
"""
import ast
import ctypes
import hashlib
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock

sys.dont_write_bytecode = True
from runner_process import finish_process_group, process_running

RUNNER_ROOT = Path(__file__).resolve().parents[1]
RUNNERS = (
    "controlled/run.py",
    "controlled_crypto/run.py",
    "controlled_provider/run.py",
    "mapped/run.py",
    "controlled_received/compile_run.py",
    "controlled_received/kernel_run.py",
    "controlled_received/policy_run.py",
    "controlled_received/run.py",
)


class RunnerMonitorTests(unittest.TestCase):
    def load(self, relative):
        path = RUNNER_ROOT / relative
        tree = ast.parse(path.read_text(), filename=str(path))
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)]
        out = Path(tempfile.mkdtemp(prefix="hfs-monitor-test-"))
        scope = dict(
            __doc__=__doc__, Path=Path, os=os, resource=resource, signal=signal,
            subprocess=subprocess, time=time, json=json, hashlib=hashlib,
            out=out, repo=out, short=out, short_tmp=out, owned_roots={out},
            env=dict(os.environ), rows=[], stages=[], frozen_binding={},
            process_running=process_running, finish_process_group=finish_process_group,
        )
        exec(compile(ast.Module(body=functions, type_ignores=[]), str(path), "exec"), scope)
        return scope

    def test_disappearing_directory(self):
        for relative in RUNNERS:
            with self.subTest(runner=relative):
                scope = self.load(relative)
                gone = scope["out"] / "concurrently-removed"

                def walk(_):
                    gone.mkdir()
                    gone.rmdir()
                    yield str(gone), [], []

                with mock.patch.object(os, "walk", walk):
                    self.assertGreaterEqual(scope["resources"](), 0)

    def test_other_directory_errors_are_not_hidden(self):
        for relative in RUNNERS:
            with self.subTest(runner=relative):
                scope = self.load(relative)
                with mock.patch.object(Path, "lstat", side_effect=PermissionError("denied")):
                    with self.assertRaises(PermissionError):
                        scope["resources"]()

    def exercise(self, relative, fault, resistant=False, descendant=False):
        scope = self.load(relative)
        ready = scope["out"] / "ready"
        script = "import time; time.sleep(60)"
        if fault is None and not descendant:
            script = "print('healthy child')"
        elif resistant:
            script = (
                "import signal,time; from pathlib import Path; "
                "signal.signal(signal.SIGTERM,signal.SIG_IGN); "
                f"Path({str(ready)!r}).write_text('ready'); time.sleep(60)"
            )
        elif descendant:
            child_script = (
                "import os,signal,time; from pathlib import Path; "
                "signal.signal(signal.SIGTERM,signal.SIG_IGN); "
                f"Path({str(ready)!r}).write_text(str(os.getpid())); time.sleep(60)"
            )
            script = (
                "import subprocess,sys,time; from pathlib import Path; "
                f"subprocess.Popen([sys.executable,'-c',{child_script!r}]); "
            )
            if fault is None:
                script += f"\nwhile not Path({str(ready)!r}).exists(): time.sleep(.01)\n"
            else:
                script += "time.sleep(60)"

        def fail():
            if resistant or descendant:
                deadline = time.monotonic() + 3
                while not ready.exists():
                    if time.monotonic() > deadline:
                        raise AssertionError("child did not install TERM handler")
                    time.sleep(.01)
            raise OSError("injected monitor failure")

        observations = 0

        def observe():
            nonlocal observations
            observations += 1
            if fault == "resources" and observations > 1:
                fail()
            return 0

        scope["resources"] = observe
        if fault == "sleep":
            scope["time"] = SimpleNamespace(monotonic=time.monotonic, sleep=lambda _: fail())
        actual_read_text = Path.read_text

        def read_text(path, *args, **kwargs):
            # A persistent /proc failure must not recur while saving the receipt.
            if fault == "resources" and observations > 1 and path == Path("/proc/meminfo"):
                fail()
            return actual_read_text(path, *args, **kwargs)

        children = []
        descendant_reaped = False
        actual_popen = subprocess.Popen

        def spawn(*args, **kwargs):
            child = actual_popen(*args, **kwargs)
            children.append(child)
            self.assertEqual(os.getpgid(child.pid), child.pid)
            return child

        try:
            with mock.patch.object(subprocess, "Popen", spawn), mock.patch.object(Path, "read_text", read_text):
                if fault is None:
                    scope["run"]("probe", [sys.executable, "-c", script])
                else:
                    with self.assertRaises(SystemExit):
                        scope["run"]("probe", [sys.executable, "-c", script])
            self.assertEqual(len(children), 1)
            child = children[0]
            self.assertIsNotNone(child.poll(), "monitor left its child alive")
            if descendant:
                descendant_pid = int(ready.read_text())
                deadline = time.monotonic() + 2
                while True:
                    waited, status = os.waitpid(descendant_pid, os.WNOHANG)
                    if waited:
                        descendant_reaped = True
                        self.assertEqual(os.waitstatus_to_exitcode(status), -signal.SIGKILL)
                        break
                    if time.monotonic() >= deadline:
                        self.fail("TERM-resistant descendant survived its group leader")
                    time.sleep(.01)
            with self.assertRaises(ProcessLookupError):
                os.killpg(child.pid, 0)
            receipt = scope["out"] / "receipt.json"
            if receipt.exists():
                row = json.loads(receipt.read_text())["stages"][0]
            else:
                row = json.loads((scope["out"] / "stages.json").read_text())[0]
            if fault is None:
                self.assertEqual(row["exit"], 0)
                self.assertIsNone(row["limit_reason"])
            else:
                self.assertNotEqual(row["exit"], 0)
                self.assertIn("injected monitor failure", row["limit_reason"])
                if fault == "resources":
                    self.assertIsNone(row["pswpout_after"])
                if resistant:
                    self.assertEqual(row["exit"], -signal.SIGKILL)
        finally:
            # A regression in the real monitor must not orphan this test's child.
            if descendant and ready.exists() and not descendant_reaped:
                descendant_pid = int(ready.read_text())
                try:
                    os.kill(descendant_pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            for child in children:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=5)
            if descendant and ready.exists() and not descendant_reaped:
                os.waitpid(int(ready.read_text()), 0)

    def test_healthy_children(self):
        for relative in RUNNERS:
            with self.subTest(runner=relative):
                self.exercise(relative, None)

    def test_monitor_failures_stop_children_and_preserve_receipts(self):
        for relative in RUNNERS:
            for fault in ("resources", "sleep"):
                with self.subTest(runner=relative, fault=fault):
                    self.exercise(relative, fault)

    def test_term_resistant_children_are_killed_and_waited(self):
        for relative in ("controlled/run.py", "controlled_received/run.py"):
            with self.subTest(runner=relative):
                self.exercise(relative, "sleep", resistant=True)

    def test_resistant_descendants_are_stopped_after_leader_exits(self):
        # Process-local subreaping lets this test reap its synthetic grandchildren
        # deterministically instead of depending on the workstation/container init.
        libc = ctypes.CDLL(None, use_errno=True)
        previous = ctypes.c_int()
        self.assertEqual(libc.prctl(37, ctypes.byref(previous), 0, 0, 0), 0)
        self.assertEqual(libc.prctl(36, 1, 0, 0, 0), 0)
        try:
            for relative in RUNNERS:
                for fault in ("sleep", None):
                    with self.subTest(runner=relative, monitor_fault=fault):
                        self.exercise(relative, fault, descendant=True)
        finally:
            self.assertEqual(libc.prctl(36, previous.value, 0, 0, 0), 0)


if __name__ == "__main__":
    unittest.main()
