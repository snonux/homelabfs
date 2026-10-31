#!/usr/bin/env python3
"""Bounded CLI regressions: replace probe workloads with tiny synthetic files."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPTS = ('check-filesystem.py', 'check-io.py')
PAYLOADS = {'fixture': b'fixture evidence', 'db-wal': b'WAL evidence',
            'db': b'database evidence'}
# Run the actual __main__ boundary in a child. The directory-creation dependency
# supplies tiny workloads before main dispatches them; benchmark sizes, UID/GID
# changes, multiprocessing, mounts and remote access are never exercised here.
BOOTSTRAP = r'''
from pathlib import Path
import runpy
import shutil
import sys
import tempfile

script, parent, mode = sys.argv[1:]
sys.argv = [script, parent]
original_mkdtemp = tempfile.mkdtemp

def mkdtemp(*args, **kwargs):
    if mode == 'creation-failure':
        raise OSError('injected creation failure')
    root = original_mkdtemp(*args, **kwargs)
    probe = sys._getframe(1).f_globals

    def operation():
        for name, payload in {'fixture': b'fixture evidence',
                              'db-wal': b'WAL evidence',
                              'db': b'database evidence'}.items():
            (probe['root'] / name).write_bytes(payload)
        if mode in ('operation-failure', 'unexpected-failure'):
            raise AssertionError('original injected operation failure')
        return 'synthetic payload verified'

    def workload():
        if 'check' in probe and mode != 'unexpected-failure':
            probe['check']('synthetic-operation', operation)
        else:
            probe['results']['synthetic-operation'] = operation()

    probe['run_checks' if 'check' in probe else 'benchmark'] = workload
    if mode in ('operation-failure', 'unexpected-failure', 'cleanup-failure'):
        def failing_cleanup(path):
            (Path(parent) / 'cleanup-called').write_text(str(path))
            raise OSError('injected cleanup failure')
        shutil.rmtree = failing_cleanup
    return root

tempfile.mkdtemp = mkdtemp
runpy.run_path(script, run_name='__main__')
'''


class PreserveFixturesTest(unittest.TestCase):
    def invoke(self, script, parent, mode):
        script_path = Path(__file__).resolve().parent / script
        completed = subprocess.run(
            [sys.executable, '-B', '-c', BOOTSTRAP, str(script_path), str(parent), mode],
            capture_output=True, text=True, timeout=10, check=False)
        self.assertEqual(completed.stderr, '')
        report = json.loads(completed.stdout)
        if script == 'check-filesystem.py':
            status = report[-1]
            self.assertEqual(status['test'], 'run-lifecycle')
        else:
            status = report
        return completed.returncode, report, status

    def assert_owned_root(self, parent, status):
        root = Path(status['root'])
        self.assertTrue(root.is_absolute())
        self.assertEqual(root.parent, parent)
        self.assertTrue(root.name.startswith(('homelabfs-check-', 'homelabfs-io-')))
        return root

    def assert_payloads(self, root):
        for name, payload in PAYLOADS.items():
            self.assertEqual((root / name).read_bytes(), payload)

    def test_operation_failure_preserves_payload_and_original_error(self):
        for script in SCRIPTS:
            for mode in ('operation-failure', 'unexpected-failure'):
                with self.subTest(script=script, mode=mode), tempfile.TemporaryDirectory() as work:
                    parent = Path(work)
                    code, report, status = self.invoke(script, parent, mode)
                    self.assertEqual(code, 1)
                    self.assertFalse(status['passed'])
                    self.assertTrue(status['retained'])
                    root = self.assert_owned_root(parent, status)
                    self.assert_payloads(root)
                    self.assertIn('original injected operation failure', json.dumps(report))
                    self.assertNotIn('injected cleanup failure', json.dumps(report))
                    self.assertFalse((parent / 'cleanup-called').exists())

    def test_cleanup_failure_reports_root_and_nonzero_status(self):
        for script in SCRIPTS:
            with self.subTest(script=script), tempfile.TemporaryDirectory() as work:
                parent = Path(work)
                code, _, status = self.invoke(script, parent, 'cleanup-failure')
                self.assertEqual(code, 1)
                self.assertFalse(status['passed'])
                self.assertTrue(status['retained'])
                self.assertIn('injected cleanup failure', status['cleanup_error'])
                root = self.assert_owned_root(parent, status)
                self.assert_payloads(root)
                self.assertEqual((parent / 'cleanup-called').read_text(), str(root))

    def test_success_removes_only_owned_root_and_keeps_results(self):
        for script in SCRIPTS:
            with self.subTest(script=script), tempfile.TemporaryDirectory() as work:
                parent = Path(work)
                sibling = parent / 'unrelated'
                sibling.mkdir()
                (sibling / 'keep').write_bytes(b'unrelated data')
                code, report, status = self.invoke(script, parent, 'success')
                self.assertEqual(code, 0)
                self.assertTrue(status['passed'])
                self.assertFalse(status['retained'])
                root = self.assert_owned_root(parent, status)
                self.assertFalse(root.exists())
                self.assertEqual((sibling / 'keep').read_bytes(), b'unrelated data')
                if isinstance(report, list):
                    self.assertTrue(report[0]['passed'])
                    self.assertEqual(report[0]['test'], 'synthetic-operation')
                else:
                    self.assertEqual(report['synthetic-operation'], 'synthetic payload verified')
                self.assertEqual(list(parent.iterdir()), [sibling])

    def test_creation_failure_reports_error_without_claiming_retained_root(self):
        for script in SCRIPTS:
            with self.subTest(script=script), tempfile.TemporaryDirectory() as work:
                code, _, status = self.invoke(script, Path(work), 'creation-failure')
                self.assertEqual(code, 1)
                self.assertFalse(status['passed'])
                self.assertFalse(status['retained'])
                self.assertIsNone(status['root'])
                self.assertIn('injected creation failure', status['error'])
                self.assertEqual(list(Path(work).iterdir()), [])


if __name__ == '__main__':
    unittest.main()
