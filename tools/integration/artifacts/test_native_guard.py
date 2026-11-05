# SPDX-License-Identifier: GPL-2.0-only
"""Real owned process groups; no VM, SSH or substituted process oracle."""
import os
import ctypes
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

from native_guard import process_running, finish_process_group, processes, check_build_outputs


class GuardCleanupTests(unittest.TestCase):
    def test_native_output_ceiling_counts_both_roots(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            for directory in ("objects", "package"):
                (root / directory).mkdir()
                (root / directory / "data").write_bytes(b'x' * 4096)
            amount = check_build_outputs(root)
            self.assertGreaterEqual(amount, 8192)
            self.assertEqual(check_build_outputs(root, amount), amount)
            with self.assertRaisesRegex(ValueError, "128MiB"):
                check_build_outputs(root, amount - 1)

    def test_actual_ps_inventory_has_numeric_owner_and_birth(self):
        rows = processes()
        parent, group, rss, birth = rows[os.getpid()]
        self.assertEqual(parent, os.getppid())
        self.assertEqual(group, os.getpgrp())
        self.assertGreater(rss, 0)
        self.assertEqual(len(birth), 5)

    def test_exited_leader_and_term_ignoring_child_are_finished(self):
        libc = ctypes.CDLL(None, use_errno=True)
        previous = ctypes.c_int()
        self.assertEqual(libc.prctl(37, ctypes.byref(previous), 0, 0, 0), 0)
        self.assertEqual(libc.prctl(36, 1, 0, 0, 0), 0)
        self.addCleanup(lambda: self.assertEqual(libc.prctl(36, previous.value, 0, 0, 0), 0))
        with tempfile.TemporaryDirectory() as directory:
            pidfile = Path(directory) / 'child'
            script = '''import os,signal,time,sys
pid=os.fork()
if pid:
 sys.exit(0)
signal.signal(signal.SIGTERM,signal.SIG_IGN)
open(sys.argv[1],'w').write(str(os.getpid()))
while True: time.sleep(1)
'''
            # A separate pre-existing process group must survive the cleanup.
            unrelated = subprocess.Popen([sys.executable, '-c', 'import time;time.sleep(30)'], start_new_session=True)
            owned = subprocess.Popen([sys.executable, '-c', script, str(pidfile)], start_new_session=True)
            child, reaped = None, False
            try:
                deadline = time.monotonic() + 3
                while not pidfile.exists() or process_running(owned):
                    if time.monotonic() > deadline:
                        self.fail('owned fixture failed to enter exited-leader state')
                    time.sleep(.01)
                child = int(pidfile.read_text())
                self.assertEqual(finish_process_group(owned, grace=.05), 0)
                deadline = time.monotonic() + 3
                while True:
                    waited, status = os.waitpid(child, os.WNOHANG)
                    if waited:
                        reaped = True
                        self.assertEqual(os.waitstatus_to_exitcode(status), -signal.SIGKILL)
                        break
                    if time.monotonic() > deadline:
                        self.fail('TERM-ignoring owned child survived final group KILL')
                    time.sleep(.01)
                self.assertTrue(process_running(unrelated))
            finally:
                if owned.returncode is None:
                    finish_process_group(owned, grace=.01)
                if child is None and pidfile.exists():
                    child = int(pidfile.read_text())
                if child is not None and not reaped:
                    try:
                        os.kill(child, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    os.waitpid(child, 0)
                finish_process_group(unrelated, grace=.01)


if __name__ == '__main__':
    unittest.main()
