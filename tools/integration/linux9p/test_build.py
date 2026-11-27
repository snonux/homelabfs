# SPDX-License-Identifier: GPL-2.0-only
"""build.py object-check planning; no kernel tree, make or compiler is used.

The controlled_objects() and main() tests replace every process boundary
(subprocess, shutil.which, CPU affinity, disk/RSS probes) with fakes that
write the .config and .o files a real build would, so the stage argv, the
refusal messages and the inputs/config-binding/stages/receipt files are
pinned without a pinned Linux tree, make or compiler.
"""
import contextlib
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import build  # noqa: E402


class ConfigPlanTests(unittest.TestCase):
    def test_module_pass_argv_is_unchanged(self):
        # Pins the pre-dn4 module pass: its .config, and so the object
        # hashes it produces, must not move when the controlled pass exists.
        argv = build.config_argv(Path('/s'), Path('/o'), '--module')
        self.assertEqual(argv, [
            '/s/scripts/config', '--file', '/o/.config',
            '--module', 'NET_9P', '--module', 'NET_9P_VIRTIO', '--module', '9P_FS',
            '--enable', '9P_FS_HOMELABFS', '--enable', 'CRYPTO_LIB_SHA256',
            '--disable', '9P_FSCACHE', '--disable', '9P_FS_POSIX_ACL'])
        self.assertNotIn('9P_FS_HOMELABFS_CONTROLLED', argv)

    def test_controlled_pass_builds_9p_in(self):
        # 9P_FS_HOMELABFS_CONTROLLED depends on 9P_FS=y and NET_9P_VIRTIO=y,
        # so the controlled pass must enable (not modularise) them.
        argv = build.config_argv(Path('/s'), Path('/o'), '--enable', controlled=True)
        pairs = list(zip(argv[3::2], argv[4::2]))
        for symbol in ('NET_9P', 'NET_9P_VIRTIO', '9P_FS', '9P_FS_HOMELABFS',
                       '9P_FS_HOMELABFS_CONTROLLED'):
            self.assertIn(('--enable', symbol), pairs)
        self.assertNotIn('--module', argv)

    def test_controlled_pass_refuses_module_state(self):
        with self.assertRaises(ValueError):
            build.config_argv(Path('/s'), Path('/o'), '--module', controlled=True)

    def test_controlled_objects_include_provider(self):
        self.assertIn('fs/9p/hfs_controlled.o', build.CONTROLLED_OBJECTS)
        self.assertIn('fs/9p/hfs_crypto_core.o', build.CONTROLLED_OBJECTS)


class WarningTests(unittest.TestCase):
    def check(self, text):
        with tempfile.TemporaryDirectory() as name:
            log = Path(name) / 'objects.log'
            log.write_text(text)
            return build.log_warnings(log)

    def test_compiler_warning_is_reported(self):
        line = 'fs/9p/hfs_controlled.c:12:3: warning: unused variable ‘x’ [-Wunused-variable]'
        self.assertEqual(self.check('  CC      fs/9p/hfs_controlled.o\n' + line + '\n'), [line])

    def test_clean_log_has_no_warnings(self):
        self.assertEqual(self.check('  CC      fs/9p/hfs_controlled.o\n  AR      fs/9p/built-in.a\n'), [])


# Objects a fake fs/9p/ + net/9p/ make writes besides the controlled-only ones.
COMMON_OBJECTS = ('fs/9p/vfs_super.o', 'fs/9p/hfs.o', 'net/9p/client.o',
                  'net/9p/trans_virtio.o')
MM_OBJECTS = ('mm/memory.o', 'mm/gup.o', 'mm/mremap.o', 'mm/huge_memory.o')


class FakeKernel:
    """Plays the kernel tree: applies one stage argv to its O= directory.

    scripts/config writes CONFIG_ lines for its --module/--enable/--disable
    pairs; make with targets writes .o files (controlled-only objects only in
    an O=.../controlled tree). Knobs inject the refusal cases: drop removes
    symbols at olddefconfig, warn appends text to the named stage log, skip
    omits objects, fail_stage makes that stage exit 2.
    """

    def __init__(self, drop=(), warn=None, skip=(), fail_stage=None):
        self.drop, self.warn, self.skip = set(drop), warn or {}, set(skip)
        self.fail_stage = fail_stage

    def apply(self, argv, name, log):
        if self.warn.get(name):
            log.write(self.warn[name].encode())
        if argv[0].endswith('scripts/config'):
            self.write_config(argv)
        elif argv[0] == 'make':
            out = Path(next(a for a in argv if a.startswith('O='))[2:])
            if argv[-1] == 'olddefconfig' and self.drop:
                config = out / '.config'
                config.write_text(''.join(
                    line + '\n' for line in config.read_text().splitlines()
                    if line.split('=')[0][len('CONFIG_'):] not in self.drop))
            elif argv[-1] not in ('x86_64_defconfig', 'olddefconfig'):
                self.write_objects(out, argv[4:])
        return 2 if name == self.fail_stage else 0

    @staticmethod
    def write_config(argv):
        config = Path(argv[argv.index('--file') + 1])
        values = {'--module': 'm', '--enable': 'y', '--disable': 'n'}
        pairs = zip(argv[3::2], argv[4::2])
        config.write_text(''.join(f'CONFIG_{symbol}={values[state]}\n'
                                  for state, symbol in pairs))

    def write_objects(self, out, targets):
        names = []
        if 'fs/9p/' in targets:
            names += COMMON_OBJECTS
            if out.name == 'controlled':
                names += build.CONTROLLED_OBJECTS
        names += [t for t in targets if t.startswith('mm/')]
        for name in names:
            if name in self.skip:
                continue
            path = out / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(f'{out.name}:{name}\n')


def fake_run(kernel, output, calls):
    """run(argv, name) callback for controlled_objects(): logs, applies."""
    def run(argv, name):
        calls.append((name, argv))
        with (output / (name + '.log')).open('wb') as log:
            kernel.apply(argv, name, log)
    return run


class ControlledObjectsTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.source = Path(self.tmp.name) / 'linux'
        self.output = Path(self.tmp.name) / 'out'
        self.output.mkdir()
        self.calls = []

    def build(self, **knobs):
        run = fake_run(FakeKernel(**knobs), self.output, self.calls)
        return build.controlled_objects(self.source, self.output, run)

    def refusal(self, **knobs):
        with self.assertRaises(SystemExit) as caught:
            self.build(**knobs)
        return str(caught.exception.code)

    def test_success_stages_argv_and_artifact_keys(self):
        artifacts, config_sha = self.build()
        out = self.output / 'controlled'
        make = ['make', '-j2', f'O={out}', 'ARCH=x86_64']
        self.assertEqual(self.calls, [
            ('controlled-defconfig', make + ['x86_64_defconfig']),
            ('controlled-config', build.config_argv(self.source, out, '--enable',
                                                    controlled=True)),
            ('controlled-olddefconfig', make + ['olddefconfig']),
            ('controlled-objects', make + ['fs/9p/', 'net/9p/'])])
        expected = sorted('controlled/' + name for name in
                          COMMON_OBJECTS + build.CONTROLLED_OBJECTS)
        self.assertEqual(sorted(artifacts), expected)
        self.assertEqual(artifacts['controlled/fs/9p/hfs_controlled.o'],
                         build.sha(out / 'fs/9p/hfs_controlled.o'))
        self.assertEqual(config_sha, build.sha(out / '.config'))
        self.assertEqual(out.stat().st_mode & 0o777, 0o700)
        # The module pass tree at output/ is never touched.
        self.assertFalse((self.output / '.config').exists())
        self.assertFalse((self.output / 'fs').exists())

    def test_refuses_each_missing_controlled_symbol(self):
        for symbol in ('9P_FS', '9P_FS_HOMELABFS', '9P_FS_HOMELABFS_CONTROLLED'):
            with self.subTest(symbol=symbol):
                self.setUp()
                self.assertEqual(
                    self.refusal(drop=[symbol]),
                    f'refuse: controlled pass requires CONFIG_{symbol}=y')
                # Refused before any object is compiled.
                self.assertNotIn('controlled-objects', [n for n, _ in self.calls])

    def test_refuses_warnings_and_lists_each_line(self):
        lines = ['fs/9p/hfs_controlled.c:1:2: warning: a [-Wx]',
                 'net/9p/client.c:3:4: warning: b [-Wy]']
        message = self.refusal(warn={'controlled-objects':
                                     '  CC x\n' + '\n'.join(lines) + '\n'})
        self.assertEqual(message, 'refuse: controlled objects compiled with warnings:\n'
                         + '\n'.join(lines))

    def test_warning_in_other_stage_log_is_not_gated(self):
        artifacts, _ = self.build(warn={'controlled-olddefconfig': 'x warning: y\n'})
        self.assertIn('controlled/fs/9p/hfs_controlled.o', artifacts)

    def test_refuses_missing_objects_in_declared_order(self):
        skip = ['fs/9p/hfs_controlled.o', 'fs/9p/hfs_crypto_core.o']
        self.assertEqual(self.refusal(skip=skip),
                         'refuse: controlled objects not built: '
                         'fs/9p/hfs_crypto_core.o, fs/9p/hfs_controlled.o')

    def test_refuses_reused_controlled_directory(self):
        (self.output / 'controlled').mkdir()
        with self.assertRaises(FileExistsError):
            self.build()
        self.assertEqual(self.calls, [])


class FakePopen:
    """subprocess.Popen stand-in: applies the stage at once and exits."""
    kernel = None
    calls = []

    def __init__(self, argv, cwd, stdout, stderr, env, start_new_session, preexec_fn):
        name = Path(stdout.name).name[:-len('.log')]
        FakePopen.calls.append({'argv': argv, 'cwd': cwd, 'env': env,
                                'session': start_new_session, 'name': name})
        self.pid = 4242
        self.code = FakePopen.kernel.apply(argv, name, stdout)

    def poll(self):
        return self.code

    def wait(self, timeout=None):
        return self.code


class MainTests(unittest.TestCase):
    """Pins main(): refusals, stage argv, and every file it writes."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.budget = root / 'run'
        self.source = self.budget / 'linux'
        self.source.mkdir(parents=True)
        (self.source / '.homelabfs-owned-source.json').write_text('{"owned": 1}\n')
        self.output = self.budget / 'object-check'
        self.bin = root / 'bin'
        self.bin.mkdir()
        for tool in ('gcc', 'cc1', 'as', 'ld'):
            (self.bin / tool).write_text(f'fake {tool}\n')
        self.missing = set()
        self.tree = 0
        self.free = 4 * 1024 ** 3
        self.verify = []
        FakePopen.calls = []

    def which(self, program):
        if program in self.missing:
            return None
        return str(self.bin / program) if (self.bin / program).exists() else '/usr/bin/' + program

    def check_output(self, argv, text):
        if argv == ['gcc', '-print-prog-name=cc1']:
            return str(self.bin / 'cc1') + '\n'
        self.assertEqual(argv[1:], ['--version'])
        return f'{argv[0]} (fake) 1.0\nsecond line\n'

    def main(self, *flags, kernel=None):
        FakePopen.kernel = kernel or FakeKernel()
        argv = ['build.py', '--source', str(self.source), '--output', str(self.output),
                '--budget-root', str(self.budget), *flags]
        stdout = io.StringIO()
        with contextlib.ExitStack() as stack:
            for target, value in (
                    ('sys.argv', argv),
                    ('build.shutil.which', self.which),
                    ('build.shutil.disk_usage', lambda p: mock.Mock(free=self.free)),
                    ('build.tree_bytes', lambda p: self.tree),
                    ('build.group_rss', lambda pid: 1024),
                    ('build.os.sched_getaffinity', lambda pid: {5, 1, 3}),
                    ('build.subprocess.run', lambda argv, **kw: self.verify.append((argv, kw))),
                    ('build.subprocess.check_output', self.check_output),
                    ('build.subprocess.Popen', FakePopen)):
                stack.enter_context(mock.patch(target, value))
            stack.enter_context(mock.patch.dict(os.environ, {'PATH': '/usr/bin'}, clear=True))
            stack.enter_context(contextlib.redirect_stdout(stdout))
            build.main()
        return stdout.getvalue()

    def refusal(self, *flags, kernel=None):
        with self.assertRaises(SystemExit) as caught:
            self.main(*flags, kernel=kernel)
        return str(caught.exception.code)

    def load(self, name):
        return json.loads((self.output / name).read_text())

    def make(self, out=None):
        return ['make', '-j2', f'O={out or self.output}', 'ARCH=x86_64']

    def compilers(self):
        return {name: {'path': str((self.bin / name).resolve()),
                       'sha256': build.sha(self.bin / name)}
                for name in ('gcc', 'cc1', 'as', 'ld')}

    def test_refuses_paths_outside_budget(self):
        self.budget = self.source
        self.assertEqual(self.refusal(),
                         'refuse: source/output must be inside the allocated budget root')

    def test_refuses_overlapping_source_and_output(self):
        self.output = self.source / 'out'
        self.assertEqual(self.refusal(), 'refuse: source and output must be disjoint')

    def test_refuses_existing_output(self):
        self.output.mkdir()
        self.assertEqual(self.refusal(),
                         'refuse: output must be fresh; failed builds remain for inspection')

    def test_refuses_missing_prerequisite(self):
        self.missing = {'bison', 'perl'}
        self.assertEqual(self.refusal(), 'refuse: missing build prerequisite: bison')

    def test_refuses_disk_budget(self):
        for tree, free in ((build.DISK_MAX, 4 * 1024 ** 3), (0, 2 * 1024 ** 3 - 1)):
            with self.subTest(tree=tree, free=free):
                self.tree, self.free = tree, free
                self.assertEqual(self.refusal(), 'refuse: disk budget/preflight failed')
                self.assertFalse(self.output.exists())
                self.assertEqual(self.verify, [])

    def test_refuses_unselected_guest_code(self):
        self.assertEqual(self.refusal(kernel=FakeKernel(drop=['9P_FS_HOMELABFS'])),
                         'refuse: selected guest code was not enabled')
        self.assertEqual([c['name'] for c in FakePopen.calls],
                         ['defconfig', 'config', 'olddefconfig'])

    def test_refuses_missing_mm_symbol(self):
        message = self.refusal('--mm-objects', kernel=FakeKernel(drop=['SHMEM']))
        self.assertEqual(message, 'refuse: MM observer check requires CONFIG_SHMEM=y')

    def test_failed_stage_keeps_evidence(self):
        message = self.refusal(kernel=FakeKernel(fail_stage='olddefconfig'))
        self.assertEqual(message, f'failed olddefconfig; retained evidence at {self.output}')
        stages = self.load('stages.json')
        self.assertEqual([s['name'] for s in stages], ['defconfig', 'config', 'olddefconfig'])
        self.assertEqual(stages[-1]['exit'], 2)
        self.assertIsNone(stages[-1]['limit_reason'])
        self.assertFalse((self.output / 'receipt.json').exists())

    def test_builtin_pass_stage_argv_and_environment(self):
        printed = self.main().splitlines()
        self.assertEqual(self.verify, [(
            ['python3', str(build.HERE / 'prepare.py'), '--destination',
             str(self.source), '--verify'], {'check': True, 'timeout': 300})])
        self.assertEqual([(c['name'], c['argv']) for c in FakePopen.calls], [
            ('defconfig', self.make() + ['x86_64_defconfig']),
            ('config', build.config_argv(self.source, self.output, '--enable')),
            ('olddefconfig', self.make() + ['olddefconfig']),
            ('objects', self.make() + ['fs/9p/', 'net/9p/'])])
        call = FakePopen.calls[0]
        self.assertEqual((call['cwd'], call['session']), (self.source, True))
        self.assertEqual(call['env'], {
            'PATH': '/usr/bin',
            'KBUILD_BUILD_TIMESTAMP': '2026-11-02 00:00:00 UTC',
            'KBUILD_BUILD_USER': 'homelabfs', 'KBUILD_BUILD_HOST': 'owned-builder',
            'KBUILD_BUILD_VERSION': '1', 'CC': 'gcc', 'HOSTCC': 'gcc',
            'KCFLAGS': f'-fdebug-prefix-map={self.source}=/usr/src/linux '
                       f'-fdebug-prefix-map={self.output}=/usr/src/linux-build '
                       f'-fmacro-prefix-map={self.source}=/usr/src/linux'})
        self.assertEqual(len(printed), 5)
        self.assertEqual([json.loads(line)['name'] for line in printed[:4]],
                         ['defconfig', 'config', 'olddefconfig', 'objects'])
        self.assertEqual(printed[4],
                         f'object checks complete: {self.output / "receipt.json"}')
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o700)

    def test_stage_records_key_order(self):
        self.main()
        for stage in self.load('stages.json'):
            self.assertEqual(list(stage), [
                'name', 'argv', 'exit', 'seconds', 'peak_group_rss_bytes',
                'limit_reason', 'max_rss_sample_gap_seconds'])
            self.assertEqual((stage['exit'], stage['peak_group_rss_bytes'],
                              stage['limit_reason']), (0, 0, None))
        self.assertTrue((self.output / 'stages.json').read_text().endswith(']\n'))

    def test_inputs_json(self):
        self.main()
        text = (self.output / 'inputs.json').read_text()
        inputs = json.loads(text)
        self.assertEqual(text, json.dumps(inputs, indent=2, sort_keys=True) + '\n')
        manifest = json.loads((build.HERE / 'source.json').read_text())
        self.assertEqual(inputs, {
            'compiler_files': self.compilers(),
            'source_inventory_sha256': build.sha(self.source / '.homelabfs-owned-source.json'),
            'manifest_sha256': build.sha(build.HERE / 'source.json'),
            'builder_sha256': build.sha(build.HERE / 'build.py'),
            'disk_measure': 'sum owned entry st_blocks*512, excluding filesystem metadata',
            'unfinished_mmap_development': manifest.get('unfinished_mmap_development', False)})

    def test_module_objects_receipt(self):
        self.main('--module-objects')
        controlled = self.output / 'controlled'
        self.assertEqual([(c['name'], c['argv']) for c in FakePopen.calls], [
            ('defconfig', self.make() + ['x86_64_defconfig']),
            ('config', build.config_argv(self.source, self.output, '--module')),
            ('olddefconfig', self.make() + ['olddefconfig']),
            ('objects', self.make() + ['fs/9p/', 'net/9p/']),
            ('controlled-defconfig', self.make(controlled) + ['x86_64_defconfig']),
            ('controlled-config', build.config_argv(self.source, controlled, '--enable',
                                                    controlled=True)),
            ('controlled-olddefconfig', self.make(controlled) + ['olddefconfig']),
            ('controlled-objects', self.make(controlled) + ['fs/9p/', 'net/9p/'])])
        self.assertEqual(self.load('config-binding.json'), {
            'config_sha256': build.sha(self.output / '.config'),
            'targets': ['fs/9p/', 'net/9p/'], 'mm_check_symbols': {}})
        text = (self.output / 'receipt.json').read_text()
        receipt = json.loads(text)
        self.assertEqual(text, json.dumps(receipt, indent=2, sort_keys=True) + '\n')
        stages = receipt.pop('stages')
        self.assertEqual(stages, self.load('stages.json'))
        self.assertEqual(len(stages), 8)
        manifest = json.loads((build.HERE / 'source.json').read_text())
        artifacts = {name: build.sha(self.output / name) for name in COMMON_OBJECTS}
        artifacts.update({'controlled/' + name: build.sha(controlled / name)
                          for name in COMMON_OBJECTS + build.CONTROLLED_OBJECTS})
        self.assertEqual(receipt, {
            'development_unfinished_mmap': manifest.get('unfinished_mmap_development', False),
            'source_inventory_sha256': build.sha(self.source / '.homelabfs-owned-source.json'),
            'compiler_files': self.compilers(),
            'scope': 'upstream kernel object compilation; no module link, installed '
                     'ELRepo ABI or runtime proof',
            'manifest_sha256': build.sha(build.HERE / 'source.json'),
            'config_sha256': build.sha(self.output / '.config'),
            'controlled_config_sha256': build.sha(controlled / '.config'),
            'artifacts': artifacts, 'cpus': [1, 3], 'rss_nominal_poll_ms': 250,
            'disk_poll_seconds': 5, 'memory_limit_bytes': build.MEMORY_MAX,
            'disk_limit_bytes': build.DISK_MAX,
            'limits_note': 'RSS/disk polling records observed bounds; disk walks can '
                           'delay RSS sampling; actual maximum gaps are recorded per '
                           'stage; transient overshoot is possible',
            'environment': {key: value for key, value in FakePopen.calls[0]['env'].items()
                            if key != 'PATH'},
            'tools': {tool: f'{tool} (fake) 1.0'
                      for tool in ('gcc', 'make', 'ld', 'bison', 'flex')}})

    def test_mm_objects_targets_and_artifacts(self):
        self.main('--mm-objects')
        targets = ['fs/9p/', 'net/9p/'] + list(MM_OBJECTS)
        self.assertEqual(FakePopen.calls[1]['argv'], build.config_argv(
            self.source, self.output, '--enable', mm_objects=True))
        self.assertEqual(FakePopen.calls[3]['argv'], self.make() + targets)
        self.assertEqual(len(FakePopen.calls), 4)
        self.assertEqual(self.load('config-binding.json'), {
            'config_sha256': build.sha(self.output / '.config'), 'targets': targets,
            'mm_check_symbols': {'TRANSPARENT_HUGEPAGE': 'y', 'SHMEM': 'y',
                                 'READ_ONLY_THP_FOR_FS': 'y'}})
        receipt = self.load('receipt.json')
        self.assertEqual(sorted(receipt['artifacts']), sorted(COMMON_OBJECTS + MM_OBJECTS))
        self.assertIsNone(receipt['controlled_config_sha256'])


if __name__ == '__main__':
    unittest.main()
