"""Actual driver CLI with owned local fake SSH children; no host contact."""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import runpy
import select
import shlex
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / 'wait-lab-ready.py'
NATIVE_KEYS = HERE.parents[1] / 'docs/lab/f3-known_hosts'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def plan_hash(plan):
    raw = json.dumps(plan, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
    for char, escaped in (('&', r'\u0026'), ('<', r'\u003c'), ('>', r'\u003e'),
                          ('\u2028', r'\u2028'), ('\u2029', r'\u2029')):
        raw = raw.replace(char, escaped)
    return hashlib.sha256(raw.encode()).hexdigest()


def inventory(mode):
    running = {'hfs-test0', 'hfs-test1', 'hfs-test2'} if mode == 'native-boot' else {'rocky'}
    rows = ['NAME DATASTORE LOADER CPU MEMORY VNC AUTO STATE']
    for name in ('hfs-test0', 'hfs-test1', 'hfs-test2', 'rocky', 'freebsd'):
        state = 'Running (1234)' if name in running else 'Stopped'
        rows.append(f'{name} lab uefi 2 2G - Yes [1] {state}')
    return '\n'.join(rows) + '\n'


class ReadinessCLI(unittest.TestCase):
    def run_cli(self, mode='native-boot', behavior=None, mutate=None, env_mutate=None,
                expire_after_round=False, spawn_signal=None, spawn_error=None):
        children, calls = [], []
        real_popen, real_monotonic, real_sleep = subprocess.Popen, time.monotonic, time.sleep
        offset = [0]
        def clock():
            return real_monotonic() * 4 + offset[0]
        def sleep(_seconds):
            if expire_after_round:
                offset[0] += 200
            else:
                real_sleep(0.005)
        def success(argv, _count):
            if argv[-1] == 'doas -n vm list':
                return 'print(' + repr(inventory(mode)) + ', end="")'
            if argv[-1].startswith('exec /usr/bin/nc '):
                return 'pass'
            kernel = 'rocky-kernel' if argv[-2] == 'root@rocky.test' else '15.1-RELEASE'
            return 'print(' + repr(kernel) + '); print("0")'
        def launch(argv, **kwargs):
            self.assertEqual(argv[0], '/usr/bin/ssh')
            self.assertIn('StrictHostKeyChecking=yes', argv)
            self.assertEqual(argv[1:3], ['-F', '/dev/null'])
            calls.append(argv)
            if spawn_error:
                if spawn_signal:
                    os.kill(os.getpid(), spawn_signal)
                raise spawn_error
            script = behavior(argv, len(calls), success) if behavior else success(argv, len(calls))
            if spawn_signal:
                script = ('import signal, time; '
                          'blocked = signal.pthread_sigmask(signal.SIG_BLOCK, set()); '
                          'print("unblocked" if not blocked & {signal.SIGINT, signal.SIGTERM} '
                          'else "blocked", flush=True); time.sleep(10)')
            child = real_popen([sys.executable, '-c', script], **kwargs)
            children.append(child)
            if spawn_signal:
                self.assertTrue(select.select([child.stdout], [], [], 2)[0], 'fake child must start')
                self.assertEqual(child.stdout.readline(), b'unblocked\n', 'child must restore its signal mask')
                # Popen has created a real child, but command() does not own its
                # returned handle yet. Delivery must wait for that handoff.
                os.kill(os.getpid(), spawn_signal)
            return child
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            host_keys, guest_keys = root / 'reviewed-host-keys', root / 'reviewed-guest-keys'
            key = NATIVE_KEYS.read_text().splitlines()[0].split()[2]
            host_keys.write_text('f3 ssh-ed25519 ' + key + '\n')
            guest_keys.write_text(NATIVE_KEYS.read_text() + 'rocky.test ssh-ed25519 ' + key + '\n')
            policy_path = root / 'policy.json'
            policy_path.write_text(json.dumps({'host_address': '192.0.2.133',
                                               'host_keys': 'host-pins', 'guest_keys': 'guest-pins'}))
            artifacts = [{'id': identity, 'path': str(path), 'sha256': digest(path)}
                         for identity, path in (('ready', SCRIPT), ('policy', policy_path),
                                                ('host-pins', host_keys), ('guest-pins', guest_keys))]
            args = ['--mode', mode, '--policy-artifact', 'policy']
            plan = {'schema': 1, 'run_id': 'fake-readiness', 'stage': 'stage1', 'prepared_guests': True,
                    'state_dir': str(root),
                    'targets': [{'name': f'hfs-test{i}', 'host': 'f3', 'initial_state': 'Stopped',
                                 'address': f'172.31.253.{11+i}', 'kernel': '15.1-RELEASE'} for i in range(3)] +
                               [{'name': 'rocky', 'host': 'f3', 'initial_state': 'Running',
                                 'address': 'rocky.test', 'kernel': 'rocky-kernel'},
                                {'name': 'freebsd', 'host': 'f3', 'initial_state': 'Stopped',
                                 'address': 'freebsd.test', 'kernel': '15.1-RELEASE'}],
                    'artifacts': artifacts,
                    'steps': [{'contract': 'prepared-lab-preflight-v1', 'args': ['--policy-artifact', 'policy']},
                              {'contract': 'lab-readiness-v1:' + mode, 'action': 'driver', 'effects': 'read-only',
                               'phase': 'setup' if mode == 'native-boot' else 'restore', 'args': args,
                               'artifact': 'ready', 'timeout_seconds': 180, 'max_attempts': 1}]}
            if mutate:
                mutate(plan, root)
            path = root / 'plan.json'
            path.write_text(json.dumps(plan))
            env = {'HFS_LAB_PLAN': str(path), 'HFS_LAB_PLAN_HASH': plan_hash(plan),
                   'HFS_LAB_RUN_ID': 'fake-readiness'}
            if env_mutate:
                env_mutate(env)
            stdout, stderr = io.StringIO(), io.StringIO()
            handlers = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
            original_mask = signal.pthread_sigmask(signal.SIG_BLOCK, set())
            try:
                with mock.patch.dict(os.environ, env), mock.patch.object(subprocess, 'Popen', launch), \
                        mock.patch.object(time, 'monotonic', clock), mock.patch.object(time, 'sleep', sleep), \
                        mock.patch.object(sys, 'argv', [str(SCRIPT)] + args), \
                        contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                    try:
                        runpy.run_path(str(SCRIPT), run_name='__main__')
                    except SystemExit as error:
                        code = error.code
                    else:
                        self.fail('actual CLI did not return through sys.exit')
            finally:
                restored_mask = signal.pthread_sigmask(signal.SIG_BLOCK, set())
                signal.pthread_sigmask(signal.SIG_SETMASK, original_mask)
                for sig, handler in handlers.items():
                    signal.signal(sig, handler)
                unreaped = [child for child in children if child.returncode is None]
                # Keep a regression failure from leaking the fixture itself;
                # the assertion still requires the driver to perform reaping.
                for child in unreaped:
                    try:
                        os.killpg(child.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    child.wait()
                    child.stdout.close()
                    child.stderr.close()
            self.assertEqual(restored_mask, original_mask, 'parent must restore its signal mask')
            self.assertFalse(unreaped, 'all local children must be waited/reaped by the driver')
            for call in calls:
                if call[-2].startswith('root@'):
                    self.assertIn('UserKnownHostsFile=' + str(guest_keys), call)
                    proxy = shlex.split(next(item for item in call
                                             if item.startswith('ProxyCommand=')).split('=', 1)[1])
                    self.assertIn('UserKnownHostsFile=' + str(host_keys), proxy)
                    self.assertIn('HostName=192.0.2.133', proxy)
                    self.assertIn('HostKeyAlias=f3', proxy)
                else:
                    self.assertIn('UserKnownHostsFile=' + str(host_keys), call)
                    self.assertIn('HostName=192.0.2.133', call)
                    self.assertIn('HostKeyAlias=f3', call)
            return code, stdout.getvalue(), stderr.getvalue(), calls

    def test_both_success_modes_prove_inventory_and_pinned_root_kernel_uid(self):
        for mode, guests in (('native-boot', 3), ('rocky-restore', 1)):
            with self.subTest(mode=mode):
                code, stdout, stderr, calls = self.run_cli(mode)
                self.assertEqual(code, 0, stderr)
                self.assertEqual(len(calls), 2 + guests * 2)
                self.assertEqual(calls[0][-1], 'doas -n vm list')
                self.assertEqual(calls[-1][-1], 'doas -n vm list')
                self.assertEqual(stdout.count('pinned root SSH ready'), guests)
                self.assertEqual(stdout.count('exact completed VM inventory verified'), 2)
                self.assertEqual(sum(call[-1] == '/usr/bin/uname -r; /usr/bin/id -u' for call in calls), guests)

    def test_inventory_mismatch_duplicate_missing_extra_transitional_fail_closed(self):
        for mode in ('native-boot', 'rocky-restore'):
            good = inventory(mode)
            bad_outputs = [inventory('rocky-restore' if mode == 'native-boot' else 'native-boot'),
                           good + good.splitlines()[1] + '\n', '\n'.join(good.splitlines()[:-1]),
                           good + 'other lab uefi 2 2G - No Stopped\n',
                           good.replace('Running (1234)', 'Booting'), good.replace('Running (1234)', 'Running')]
            for output in bad_outputs:
                with self.subTest(mode=mode, output=output):
                    code, _, stderr, calls = self.run_cli(mode, lambda *_: 'print(' + repr(output) + ')')
                    self.assertEqual(code, 20, stderr)
                    self.assertEqual(len(calls), 1)

    def test_final_inventory_must_still_match(self):
        for mode, final_call in (('native-boot', 8), ('rocky-restore', 4)):
            def behavior(argv, count, success):
                return "print('unavailable')" if count == final_call else success(argv, count)
            code, _, stderr, calls = self.run_cli(mode, behavior)
            self.assertEqual(code, 20, stderr)
            self.assertEqual(len(calls), final_call)

    def test_plan_context_contract_targets_and_key_negatives_before_ssh(self):
        changes = [lambda p, r: p.update(stage='stage2'),
                   lambda p, r: p['steps'][1].update(max_attempts=2),
                   lambda p, r: p['steps'][1].update(timeout_seconds=179),
                   lambda p, r: p['steps'][1].update(effects='run-owned'),
                   lambda p, r: p['steps'][1].update(args=['wrong']),
                   lambda p, r: p['targets'].pop(),
                   lambda p, r: p['targets'].append(p['targets'][0]),
                   lambda p, r: p['targets'][0].update(address='172.31.253.99'),
                   lambda p, r: p['targets'][0].update(host='other'),
                   lambda p, r: p['targets'][0].update(kernel=''),
                   lambda p, r: (r / 'reviewed-guest-keys').write_text('changed'),
                   lambda p, r: (r / 'reviewed-host-keys').write_text('changed'),
                   lambda p, r: p['artifacts'][0].update(path=p['artifacts'][1]['path'])]
        for mutate in changes:
            with self.subTest(mutate=mutate):
                code, _, stderr, calls = self.run_cli(mutate=mutate)
                self.assertEqual(code, 20, stderr)
                self.assertEqual(calls, [])
        for key, value in (('HFS_LAB_PLAN_HASH', '1' * 64), ('HFS_LAB_RUN_ID', 'other')):
            code, _, stderr, calls = self.run_cli(env_mutate=lambda env: env.update({key: value}))
            self.assertEqual(code, 20, stderr)
            self.assertEqual(calls, [])

    def test_repinning_wrong_native_key_or_missing_rocky_key_still_fails(self):
        for mode in ('native-boot', 'rocky-restore'):
            def mutate(plan, root):
                path = root / 'reviewed-guest-keys'
                text = path.read_text()
                if mode == 'native-boot':
                    text = text.replace(text.splitlines()[0].split()[2], text.splitlines()[1].split()[2])
                else:
                    text = '\n'.join(text.splitlines()[:-1]) + '\n'
                path.write_text(text)
                plan['artifacts'][3]['sha256'] = digest(path)
            code, _, stderr, calls = self.run_cli(mode, mutate=mutate)
            self.assertEqual(code, 20, stderr)
            self.assertEqual(calls, [])

    def test_completed_tcp_not_ready_can_retry_then_succeed(self):
        for mode in ('native-boot', 'rocky-restore'):
            def behavior(argv, count, success):
                return 'import sys; sys.exit(1)' if count == 2 else success(argv, count)
            code, _, stderr, calls = self.run_cli(mode, behavior)
            self.assertEqual(code, 0, stderr)
            self.assertEqual(len(calls), 9 if mode == 'native-boot' else 5)

    def test_completed_not_ready_exhaustion_is_terminal(self):
        def behavior(argv, count, success):
            return success(argv, count) if count == 1 else 'import sys; sys.exit(1)'
        code, _, stderr, calls = self.run_cli(behavior=behavior, expire_after_round=True)
        self.assertEqual(code, 20, stderr)
        self.assertEqual(len(calls), 4)

    def test_transport_timeout_overflow_signal_never_retry_into_success(self):
        failures = ['import sys; sys.exit(255)', 'import time; time.sleep(10)',
                    "print('X'*17000)",
                    'import os, signal, time; os.kill(os.getppid(), signal.SIGTERM); time.sleep(10)']
        for mode in ('native-boot', 'rocky-restore'):
            for position in (1, 2, 3):
                for failure in failures:
                    with self.subTest(mode=mode, position=position, failure=failure):
                        def behavior(argv, count, success):
                            return failure if count == position else success(argv, count)
                        code, _, stderr, calls = self.run_cli(mode, behavior)
                        self.assertEqual(code, 1, stderr)
                        self.assertEqual(json.loads(stderr)['status'], 'uncertain')
                        self.assertEqual(len(calls), position)

    def test_spawn_boundary_interruption_owns_and_reaps_child_without_retry(self):
        for sig in (signal.SIGINT, signal.SIGTERM):
            with self.subTest(signal=sig):
                code, _, stderr, calls = self.run_cli(spawn_signal=sig)
                self.assertEqual(code, 1, stderr)
                self.assertEqual(json.loads(stderr)['status'], 'uncertain')
                self.assertIn('interrupted', json.loads(stderr)['error'])
                self.assertEqual(len(calls), 1)

    def test_spawn_failure_restores_mask_even_with_pending_interruption(self):
        for error in (OSError('fixture dispatch failed'), subprocess.SubprocessError('fixture child setup failed')):
            for sig in (None, signal.SIGINT, signal.SIGTERM):
                with self.subTest(error=error, signal=sig):
                    code, _, stderr, calls = self.run_cli(spawn_signal=sig, spawn_error=error)
                    self.assertEqual(code, 1, stderr)
                    self.assertEqual(json.loads(stderr)['status'], 'uncertain')
                    self.assertEqual(len(calls), 1)

    def test_unexpected_completed_inventory_or_tcp_result_is_terminal(self):
        for position, failure in ((1, 'import sys; sys.exit(1)'),
                                  (2, 'import sys; sys.exit(2)'), (2, "print('unexpected')")):
            def behavior(argv, count, success):
                return failure if count == position else success(argv, count)
            code, _, stderr, calls = self.run_cli(behavior=behavior)
            self.assertEqual(code, 20, stderr)
            self.assertEqual(len(calls), position)

    def test_completed_kernel_uid_or_command_failure_is_terminal(self):
        for mode in ('native-boot', 'rocky-restore'):
            for failure in ("print('wrong-kernel'); print('0')", "print('15.1-RELEASE'); print('1000')",
                            'import sys; sys.exit(2)'):
                def behavior(argv, count, success):
                    return failure if count == 3 else success(argv, count)
                code, _, stderr, calls = self.run_cli(mode, behavior)
                self.assertEqual(code, 20, stderr)
                self.assertEqual(len(calls), 3)


if __name__ == '__main__':
    unittest.main()
