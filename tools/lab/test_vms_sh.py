"""Offline checks of the vms.sh HOST script: no SSH, doas, VM or host contact.

The test extracts the POSIX sh heredoc that vms.sh pipes to `doas sh` on f3 and
runs it locally under each available POSIX shell. `vm`, `ps`, `sysctl`,
`swapinfo`, `sysrc` and `sleep` are PATH stubs that read canned output and log
calls; the fixed `/dev/vmm` path is rewritten to a temporary directory because
it cannot be stubbed through PATH. Run with:

    python3 -B -m unittest discover -s tools/lab -p test_vms_sh.py -v
"""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / 'vms.sh'
HEADER = 'NAME DATASTORE LOADER CPU MEMORY VNC AUTO STATE'
GUESTS = ('rocky', 'freebsd', 'hfs-test0', 'hfs-test1', 'hfs-test2')
# Stubs read $FAKE (set only by this test) and append every call to calls.
STUBS = {
    'vm': '''case "$1" in
    list) cat "$FAKE/vm-list" ;;
    *) echo "vm $*" >> "$FAKE/calls" ;;
esac''',
    'ps': '''echo "ps $*" >> "$FAKE/calls"
test ! -e "$FAKE/ps-fails" || exit 1
cat "$FAKE/ps"''',
    'sysctl': '''case "$2" in
    hw.physmem) echo 17179869184 ;;
    hw.pagesize) echo 4096 ;;
    vm.stats.vm.v_free_count) echo 3000000 ;;
    vm.stats.vm.v_inactive_count) echo 0 ;;
    *) exit 1 ;;
esac''',
    'swapinfo': "printf 'Device 1K-blocks Used Avail Capacity\\n/dev/sw 100 0 100 0%%\\n'",
    'sysrc': 'echo 2G',
    'sleep': ':',
}


def host_script():
    """The exact heredoc body vms.sh sends as `doas -n /bin/sh -s`."""
    lines = SCRIPT.read_text().splitlines()
    start = next(i for i, line in enumerate(lines) if line.endswith("<<'HOST'"))
    end = lines.index('HOST', start + 1)
    return '\n'.join(lines[start + 1:end]) + '\n'


def posix_shells():
    """dash/busybox when present; otherwise bash in POSIX mode."""
    shells = []
    for name, argv in (('dash', ['dash']), ('busybox', ['busybox', 'sh']),
                       ('bash', ['bash', '--posix'])):
        if shutil.which(name):
            shells.append(argv)
    return shells


class HostScript(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix='vms-sh-test-'))
        self.addCleanup(shutil.rmtree, self.tmp)
        self.vmm = self.tmp / 'vmm'
        stubs = self.tmp / 'bin'
        stubs.mkdir()
        for name, body in STUBS.items():
            path = stubs / name
            path.write_text('#!/bin/sh\n' + body + '\n')
            path.chmod(0o755)
        self.path = '%s:/usr/bin:/bin' % stubs
        source = host_script()
        self.assertIn('test -e /dev/vmm', source)
        self.assertIn('ls /dev/vmm', source)
        self.script = source.replace('/dev/vmm', str(self.vmm))
        self.inventory()
        self.processes('    1 /sbin/init', '  900 /usr/sbin/sshd', ' 1200 bhyve: rocky-old')

    def inventory(self, running=()):
        rows = [HEADER]
        for name in GUESTS:
            state = 'Running (1234)' if name in running else 'Stopped'
            rows.append('%s default uefi 2 2G - No %s' % (name, state))
        (self.tmp / 'vm-list').write_text('\n'.join(rows) + '\n')

    def processes(self, *lines):
        (self.tmp / 'ps').write_text('\n'.join(lines) + '\n')

    def context(self, name):
        self.vmm.mkdir(exist_ok=True)
        (self.vmm / name).touch()

    def run_action(self, action):
        results = []
        for shell in posix_shells():
            calls = self.tmp / 'calls'
            calls.unlink(missing_ok=True)
            proc = subprocess.run(
                shell + ['-s', '--', action], input=self.script, text=True,
                capture_output=True, timeout=30,
                env={'PATH': self.path, 'FAKE': str(self.tmp), 'LC_ALL': 'C'})
            log = calls.read_text().splitlines() if calls.exists() else []
            results.append((shell, proc, log))
        self.assertTrue(results, 'no POSIX shell available')
        return results

    def assert_refused(self, action, *needles):
        for shell, proc, log in self.run_action(action):
            with self.subTest(shell=shell):
                self.assertEqual(proc.returncode, 1, proc.stderr)
                self.assertIn('Refusing %s' % action, proc.stderr)
                self.assertIn('Nothing was stopped or killed', proc.stderr)
                for needle in needles:
                    self.assertIn(needle, proc.stderr)
                self.assertEqual([c for c in log if c.startswith('vm ')], [])

    def assert_started(self, action, expected):
        for shell, proc, log in self.run_action(action):
            with self.subTest(shell=shell):
                self.assertEqual(proc.returncode, 0, proc.stderr)
                self.assertIn('ps -ax -ww -o pid= -o args=', log)
                self.assertEqual([c for c in log if c.startswith('vm ')], expected)

    def test_start_rocky_without_zq3(self):
        self.context('hfs-test0-stale-name-only')
        self.assert_started('start-rocky', ['vm start rocky'])

    def test_start_rocky_refuses_vmm_context(self):
        self.context('hfs-zq3-r1')
        self.assert_refused('start-rocky', 'vmm contexts: hfs-zq3-r1')

    def test_start_rocky_refuses_bhyve_title(self):
        self.processes('  900 /usr/sbin/sshd', ' 4242 bhyve: hfs-zq3-r1 (bhyve)')
        self.assert_refused('start-rocky', '4242 bhyve: hfs-zq3-r1')

    def test_start_rocky_refuses_fd_exec_title(self):
        self.processes(' 4243 4: hfs-zq3-r2')
        self.assert_refused('start-rocky', '4243 4: hfs-zq3-r2')

    def test_start_rocky_refuses_argv_token(self):
        self.processes(' 4244 /usr/sbin/bhyve -c 2 -m 6G hfs-zq3-r3')
        self.assert_refused('start-rocky', 'hfs-zq3-r3')

    def test_near_miss_tokens_do_not_refuse(self):
        self.processes(' 10 tail -f /var/tmp/hfs-zq3-r1/log', ' 11 vi hfs-zq3x',
                       ' 12 bhyve: hfs-test0')
        self.context('hfs-zq3')
        self.assert_started('start-rocky', ['vm start rocky'])

    def test_failing_ps_fails_closed(self):
        (self.tmp / 'ps-fails').touch()
        for shell, proc, log in self.run_action('start-rocky'):
            with self.subTest(shell=shell):
                self.assertNotEqual(proc.returncode, 0)
                self.assertEqual([c for c in log if c.startswith('vm ')], [])

    def test_start_rocky_keeps_lab_refusal(self):
        self.inventory(running=('hfs-test1',))
        for shell, proc, log in self.run_action('start-rocky'):
            with self.subTest(shell=shell):
                self.assertNotEqual(proc.returncode, 0)
                self.assertEqual([c for c in log if c.startswith('vm ')], [])

    def test_start_lab_without_zq3(self):
        self.assert_started('start-lab', ['vm start hfs-test%d' % i for i in range(3)])

    def test_start_lab_refuses_zq3(self):
        self.context('hfs-zq3-r1')
        self.processes(' 4242 bhyve: hfs-zq3-r1')
        self.assert_refused('start-lab', 'hfs-zq3-r1', '4242 bhyve: hfs-zq3-r1')

    def test_start_lab_keeps_rocky_refusal(self):
        self.inventory(running=('rocky',))
        for shell, proc, log in self.run_action('start-lab'):
            with self.subTest(shell=shell):
                self.assertEqual(proc.returncode, 1)
                self.assertIn('Stop Rocky in an approved window first.', proc.stderr)
                self.assertEqual([c for c in log if c.startswith('vm ')], [])

    def test_stop_actions_do_not_inspect_zq3(self):
        self.context('hfs-zq3-r1')
        for shell, proc, log in self.run_action('stop-rocky'):
            with self.subTest(shell=shell):
                self.assertEqual(proc.returncode, 0, proc.stderr)
                self.assertEqual(log, [])


class Lint(unittest.TestCase):
    @unittest.skipUnless(shutil.which('shellcheck'), 'shellcheck not installed')
    def test_shellcheck(self):
        subprocess.run(['shellcheck', str(SCRIPT)], check=True)
        subprocess.run(['shellcheck', '-s', 'sh', '-'], input=host_script(),
                       text=True, check=True)


if __name__ == '__main__':
    unittest.main()
