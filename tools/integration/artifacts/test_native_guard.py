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
from unittest import mock

import native_guard
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


class RacingWalk:
    """Wrap the real os.walk and run race() right after the first yield is
    produced, before the guard lstat()s its entries or descends: the exact
    window in which make renamed pci_fbuf-*.pieo.tmp during xd4."""

    def __init__(self, race):
        self.race, self.real = race, os.walk

    def __call__(self, *args, **kwargs):
        raced = False
        for entry in self.real(*args, **kwargs):
            if not raced:
                raced = True
                self.race()
            yield entry


class AllocatedRaceTests(unittest.TestCase):
    """Disk walk skips only vanished entries and never drops live ones."""

    def setUp(self):
        self.root = Path(tempfile.mkdtemp())
        self.addCleanup(subprocess.run, ['rm', '-rf', str(self.root)], check=True)
        (self.root / 'kept').write_bytes(b'k' * 65536)
        (self.root / 'object.pieo.tmp').write_bytes(b't' * 65536)
        (self.root / 'sub').mkdir()
        (self.root / 'sub' / 'inner').write_bytes(b'i' * 65536)

    def walk_with(self, race):
        with mock.patch.object(native_guard.os, 'walk', RacingWalk(race)):
            return native_guard.allocated(self.root)

    def live_bytes(self):
        return native_guard.allocated(self.root)

    def test_file_renamed_mid_walk_counts_live_files_only(self):
        def race():
            (self.root / 'object.pieo.tmp').rename(self.root / 'sub' / 'object.pieo')
        total = self.walk_with(race)
        kept = (self.root / 'kept').lstat().st_blocks * 512
        self.assertGreaterEqual(total, kept)
        # The renamed object landed in a not yet walked directory: still counted.
        self.assertEqual(total, self.live_bytes())

    def test_directory_vanishing_mid_walk_is_skipped(self):
        def race():
            subprocess.run(['rm', '-rf', str(self.root / 'sub')], check=True)
        self.assertEqual(self.walk_with(race), self.live_bytes())

    def test_directory_replaced_by_file_is_counted(self):
        def race():
            subprocess.run(['rm', '-rf', str(self.root / 'sub')], check=True)
            (self.root / 'sub').write_bytes(b'f' * 65536)
        total = self.walk_with(race)
        self.assertEqual(total, self.live_bytes())
        self.assertGreaterEqual(total, (self.root / 'sub').lstat().st_blocks * 512)

    def test_missing_root_counts_zero(self):
        self.assertEqual(native_guard.allocated(self.root / 'absent'), 0)

    def test_unreadable_entry_still_fails_closed(self):
        def denied(path):
            raise PermissionError(13, 'denied', str(path))
        with mock.patch.object(native_guard.Path, 'lstat', denied):
            with self.assertRaises(PermissionError):
                native_guard.allocated(self.root)

    def test_walk_error_other_than_vanish_fails_closed(self):
        def walk(top, onerror=None, followlinks=False):
            onerror(PermissionError(13, 'denied', str(top)))
            yield from ()
        with mock.patch.object(native_guard.os, 'walk', walk):
            with self.assertRaises(PermissionError):
                native_guard.allocated(self.root)


if __name__ == '__main__':
    unittest.main()
