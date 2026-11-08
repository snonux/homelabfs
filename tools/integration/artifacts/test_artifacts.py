# SPDX-License-Identifier: BSD-2-Clause
"""Offline structural/digest controls; synthetic ELF notes are parser fixtures."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import tempfile
import unittest

import elf
import kernel
import package


class KernelConfigTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / 'config'
        self.key = 'ab' * 32
        # Parser/admission fixture, not Kconfig resolution or a boot claim.
        self.values = {'CONFIG_' + symbol: 'y' for symbol in kernel.REQUIRED}
        self.values.update(CONFIG_9P_FS_HOMELABFS_OPERATOR_KEY='"' + self.key + '"',
                           CONFIG_LOCALVERSION='"-hfs-xz3"', CONFIG_VIRTIO_NET='m')

    def write(self, extra=''):
        self.path.write_text(''.join(key + '=' + value + '\n'
                                     for key, value in self.values.items()) + extra)

    def test_selected_profile_and_shared_package_admission(self):
        import admit
        self.write()
        self.assertIs(admit.assert_config, kernel.assert_config)
        admit.assert_config(self.path, self.key)
        # VFS storage/private netns require neither overlay nor CNI devices.
        self.write('# CONFIG_OVERLAY_FS is not set\n'
                   '# CONFIG_VETH is not set\n# CONFIG_BRIDGE is not set\n')
        kernel.assert_config(self.path, self.key)

    def test_each_required_feature_missing_disabled_or_module_refuses(self):
        for symbol in dict.fromkeys(kernel.REQUIRED):
            name = 'CONFIG_' + symbol
            for value in (None, 'n', 'm'):
                with self.subTest(symbol=symbol, value=value):
                    if value is None:
                        del self.values[name]
                    else:
                        self.values[name] = value
                    self.write('# ' + name + ' is not set\n' if value is None else '')
                    with self.assertRaisesRegex(ValueError, symbol):
                        kernel.assert_config(self.path, self.key)
                    self.values[name] = 'y'
            del self.values[name]
            self.write()
            with self.assertRaisesRegex(ValueError, symbol):
                kernel.assert_config(self.path, self.key)
            self.values[name] = 'y'

    def test_ambiguous_and_malformed_config_refuses(self):
        for entry in ('CONFIG_UTS_NS=y\n', 'CONFIG_UTS_NS=n\n',
                      '# CONFIG_UTS_NS is not set\n', 'CONFIG_NET_NS=\n'):
            with self.subTest(entry=entry):
                self.write(entry)
                with self.assertRaises(ValueError):
                    kernel.assert_config(self.path, self.key)


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.key = 'ab' * 32
        self.revision = 'cd' * 20
        # These bytes exercise a container, not a kernel/build/platform oracle.
        self.inventory = {}
        for kind in package.REQUIRED:
            name = 'inputs/' + kind
            path = self.root / name
            path.parent.mkdir(exist_ok=True)
            path.write_bytes(('bounded ' + kind + ' bytes').encode())
            self.inventory[name] = kind
        self.pinned = package.seal(self.root, self.inventory, self.key, self.revision)

    def check(self):
        return package.check(self.root, self.pinned, self.key, self.revision)

    def modify_manifest(self, change):
        path = self.root / 'manifest.json'
        value = json.loads(path.read_bytes())
        change(value)
        path.write_bytes(package.canonical(value))
        # Refreshing this test pin ensures metadata semantics are checked even
        # when a trusted caller supplied the hash of an unsupported manifest.
        self.pinned = hashlib.sha256(path.read_bytes()).hexdigest()

    def test_exact_complete_package(self):
        self.assertIsNone(self.check()['policy']['publication'])

    def test_unpinned_changed_manifest(self):
        with (self.root / 'manifest.json').open('ab') as stream:
            stream.write(b' ')
        with self.assertRaises(ValueError):
            self.check()

    def test_changed_missing_or_extra_artifact(self):
        target = self.root / next(iter(self.inventory))
        original = target.read_bytes()
        target.write_bytes(b'changed source')
        with self.assertRaises(ValueError):
            self.check()
        target.unlink()
        with self.assertRaises(ValueError):
            self.check()
        target.write_bytes(original)
        (self.root / 'extra').write_bytes(b'private or untracked bytes')
        with self.assertRaises(ValueError):
            self.check()

    def test_untracked_fifo_refuses_closed_inventory(self):
        os.mkfifo(self.root / 'unexpected-fifo', 0o600)
        with self.assertRaises(ValueError):
            self.check()

    def test_wrong_operator_and_repository(self):
        for key, revision in [('ef' * 32, self.revision), (self.key, 'ef' * 20)]:
            with self.subTest(key=key, revision=revision), self.assertRaises(ValueError):
                package.check(self.root, self.pinned, key, revision)

    def test_old_mixed_protocol_and_runtime_success_refuse(self):
        original = (self.root / 'manifest.json').read_bytes()
        cases = [lambda m: m['protocol'].update(Features=16383),
                 lambda m: m['protocol'].update(BridgeMinor=3),
                 lambda m: m['protocol'].update(BodySchema=4),
                 lambda m: m['protocol'].update(BridgeMajor=True),
                 lambda m: m['source_revisions'].update(linux='ab' * 20),
                 lambda m: m['policy'].update(page_size=65536),
                 lambda m: m['policy'].update(publication={'mode': 'NORMAL_PUBLIC'}),
                 lambda m: m['evidence'].update(installed=True),
                 lambda m: m['evidence'].update(native_accepted=True)]
        for change in cases:
            with self.subTest(change=change):
                (self.root / 'manifest.json').write_bytes(original)
                self.modify_manifest(change)
                with self.assertRaises(ValueError):
                    self.check()

    def test_symlink_and_path_escape(self):
        target = self.root / next(iter(self.inventory))
        target.unlink()
        target.symlink_to('/etc/passwd')
        with self.assertRaises(ValueError):
            self.check()
        for name in ['/etc/passwd', '../outside', 'inputs/../manifest.json', './manifest.json']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                package.file_path(self.root, name)

    def test_duplicate_unknown_or_incomplete_manifest(self):
        with self.assertRaises(ValueError):
            package.unique([('version', 1), ('version', 2)])
        original = self.check()
        for change in [lambda m: m.update(extra=True),
                       lambda m: m['files'].pop('inputs/kernel'),
                       lambda m: m['files']['inputs/kernel'].update(bytes=True)]:
            value = copy.deepcopy(original)
            change(value)
            with self.subTest(change=change), self.assertRaises(ValueError):
                package.validate_metadata(value)


class ELFTests(unittest.TestCase):
    def fixture(self, note, osabi=0):
        # One real ELF64 PT_NOTE table, with bounded encoded GNU/Go note bytes.
        header = b'\x7fELF\x02\x01\x01' + bytes([osabi]) + b'\0' * 8
        header += struct.pack('<HHIQQQIHHHHHH', 2, 62, 1, 0, 64, 0, 0,
                              64, 56, 1, 64, 0, 0)
        program = struct.pack('<IIQQQQQQ', 4, 0, 120, 0, 0, len(note), len(note), 4)
        return header + program + note

    def test_gnu_and_go_notes_have_distinct_contracts(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'elf'
            note = struct.pack('<III', 4, 20, 3) + b'GNU\0' + bytes(range(20))
            path.write_bytes(self.fixture(note))
            self.assertEqual(elf.identity(path)['gnu_build_id'], bytes(range(20)).hex())
            note = struct.pack('<III', 4, 4, 4) + b'Go\0\0' + b'goid'
            path.write_bytes(self.fixture(note, osabi=9))
            observed = elf.identity(path)
            self.assertEqual(observed['go_build_id'], 'goid')
            self.assertIsNone(observed['gnu_build_id'])
            for raw in [path.read_bytes()[:-1], b'not ELF', self.fixture(note, osabi=3)]:
                path.write_bytes(raw)
                with self.assertRaises(ValueError):
                    elf.identity(path)


class NativeSourceBundleTests(unittest.TestCase):
    def test_archive_content_and_type_closed_inventory(self):
        import io
        import tarfile
        import native_bundle
        with tempfile.TemporaryDirectory() as name:
            archive = Path(name) / 'source.tar.gz'
            data = b'actual selected source'
            expected = {'dir/source.c': hashlib.sha256(data).hexdigest()}
            for kind in ('valid', 'changed', 'extra', 'duplicate', 'symlink', 'escape'):
                with self.subTest(kind=kind):
                    with tarfile.open(archive, 'w:gz') as tar:
                        info = tarfile.TarInfo('dir/source.c' if kind != 'escape' else '../escape')
                        if kind == 'symlink':
                            info.type = tarfile.SYMTYPE
                            info.linkname = '/etc/passwd'
                            tar.addfile(info)
                        else:
                            body = b'wrong' if kind == 'changed' else data
                            info.size = len(body)
                            tar.addfile(info, io.BytesIO(body))
                            if kind in ('extra', 'duplicate'):
                                info = tarfile.TarInfo('extra' if kind == 'extra' else 'dir/source.c')
                                info.size = len(data)
                                tar.addfile(info, io.BytesIO(data))
                    if kind == 'valid':
                        self.assertEqual(native_bundle.verify(archive, expected), len(data))
                    else:
                        with self.assertRaises(ValueError):
                            native_bundle.verify(archive, expected)
            with self.assertRaises(ValueError):
                native_bundle.verify(archive, {})


CGO_SYMBOLS = ('x_cgo_init', '_cgo_2c83f6d6b052_Cfunc_hfs_process',
               'github.com/snonux/homelabfs/internal/controlledruntime._Cfunc_hfs_process.abi0')
GO_VERSION = 'go1.26.10-X:nodwarf5'
CGO_SETTINGS = {'-buildmode': 'exe', '-compiler': 'gc', '-trimpath': 'true',
                'CGO_ENABLED': '1', 'GOARCH': 'amd64', 'GOOS': 'freebsd'}


def uvarint(value):
    out = bytearray()
    while value >= 0x80:
        out.append(value & 0x7f | 0x80)
        value >>= 7
    return bytes(out + bytes([value]))


def buildinfo_blob(settings, version=GO_VERSION):
    """Go debug/buildinfo layout: 32-byte header, inline version and modinfo."""
    text = 'path\tgithub.com/snonux/homelabfs/cmd/hfs-controlled-runtime\n'
    text += 'mod\tgithub.com/snonux/homelabfs\t(devel)\t\n'
    text += ''.join('build\t' + key + '=' + value + '\n' for key, value in settings.items())
    modinfo = b'\x30' * 16 + text.encode() + b'\xf9' * 16
    header = b'\xff Go buildinf:' + bytes([8, 2]) + b'\0' * 16
    return header + uvarint(len(version)) + version.encode() + uvarint(len(modinfo)) + modinfo


def synthetic_elf(needed, interpreter=b'/libexec/ld-elf.so.1', symbols=CGO_SYMBOLS,
                  buildinfo=None, kind=2):
    """One loadable ELF64 with PT_INTERP/PT_DYNAMIC and named sections.

    It is a parser fixture: it has the real table layouts that dynamic_needed,
    defined_symbols and go_buildinfo read, but no code and is never run.
    """
    phnum = 3 if interpreter else 2
    data, cursor = bytearray(), 64 + phnum * 56

    def place(raw):
        nonlocal cursor
        offset = cursor
        data.extend(raw)
        cursor += len(raw)
        return offset

    interp_offset = place(interpreter + b'\0') if interpreter else 0
    dynstr = b'\0' + b''.join(name.encode() + b'\0' for name in needed)
    dynstr_offset = place(dynstr)
    entries, index = [], 1
    for name in needed:
        entries.append(struct.pack('<qQ', 1, index))
        index += len(name) + 1
    entries += [struct.pack('<qQ', 5, dynstr_offset), struct.pack('<qQ', 10, len(dynstr)),
                struct.pack('<qQ', 0, 0)]
    dynamic_offset = place(b''.join(entries))
    sections = [(0, 0, 0, 0, 0, 0)]  # name, type, offset, size, link, entsize
    shstr = bytearray(b'\0')

    def section(name, kind_, raw, link=0, entsize=0):
        sections.append((len(shstr), kind_, place(raw), len(raw), link, entsize))
        shstr.extend(name.encode() + b'\0')

    if buildinfo is not None:
        section('.go.buildinfo', 1, buildinfo)
    if symbols is not None:
        strtab = b'\0' + b''.join(name.encode() + b'\0' for name in symbols)
        table, offset = bytearray(24), 1
        for name in symbols:
            table += struct.pack('<IBBHQQ', offset, 0x12, 0, 1, 0, 0)
            offset += len(name) + 1
        section('.symtab', 2, bytes(table), link=len(sections) + 1, entsize=24)
        section('.strtab', 3, strtab)
    section('.shstrtab', 3, bytes(shstr) + b'.shstrtab\0')
    shoff = cursor
    end = shoff + len(sections) * 64
    program = struct.pack('<IIQQQQQQ', 1, 5, 0, 0, 0, end, end, 4096)
    if interpreter:
        program += struct.pack('<IIQQQQQQ', 3, 4, interp_offset, interp_offset, 0,
                               len(interpreter) + 1, len(interpreter) + 1, 1)
    program += struct.pack('<IIQQQQQQ', 2, 6, dynamic_offset, dynamic_offset, 0,
                           len(entries) * 16, len(entries) * 16, 8)
    header = b'\x7fELF\x02\x01\x01\x09' + b'\0' * 8
    header += struct.pack('<HHIQQQIHHHHHH', kind, 62, 1, 0, 64, shoff, 0, 64, 56,
                          phnum, 64, len(sections), len(sections) - 1)
    table = b''.join(struct.pack('<IIQQQQIIQQ', name, kind_, 0, offset, offset, size,
                                 link, 0, 1, entsize)
                     for name, kind_, offset, size, link, entsize in sections)
    return header + program + bytes(data) + table


class DaemonCgoTests(unittest.TestCase):
    """Parser and rule fixtures for the dynamic CGO daemon admission."""

    def setUp(self):
        import daemon_cgo
        self.cgo = daemon_cgo
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.sysroot = self.root / 'sysroot'
        libraries = {'lib/libthr.so.3': ['libc.so.7', 'libsys.so.7'],
                     'lib/libc.so.7': ['libsys.so.7'], 'lib/libsys.so.7': []}
        files = {}
        for name, needed in libraries.items():
            files[name] = synthetic_elf(needed, interpreter=None, symbols=None, kind=3)
        files['libexec/ld-elf.so.1'] = b'synthetic interpreter bytes'
        files['usr/include/stdio.h'] = b'/* header */\n'
        rows = {}
        for name, raw in files.items():
            path = self.sysroot / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(raw)
            rows[name] = {'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
        self.capture = {'version': daemon_cgo.CAPTURE_VERSION, 'platform': ['FreeBSD', '15.1-RELEASE-p2'],
                        'files': rows}
        (self.sysroot / 'capture.json').write_text(json.dumps(self.capture))
        self.binary = self.root / 'daemon'
        self.binary.write_bytes(synthetic_elf(['libthr.so.3', 'libc.so.7'],
                                              buildinfo=buildinfo_blob(CGO_SETTINGS)))

    def facts(self, settings=None, symbols=CGO_SYMBOLS):
        self.binary.write_bytes(synthetic_elf(['libthr.so.3', 'libc.so.7'], symbols=symbols,
                                              buildinfo=buildinfo_blob(settings or CGO_SETTINGS)))
        return self.cgo.observe(self.binary)

    def test_parsers_read_synthetic_cgo_daemon(self):
        facts = self.cgo.observe(self.binary)
        self.assertEqual(facts['buildinfo']['settings']['CGO_ENABLED'], '1')
        self.assertEqual(facts['buildinfo']['go_version'], GO_VERSION)
        self.assertTrue(set(CGO_SYMBOLS) <= facts['symbols'])
        self.cgo.check_cgo(facts, GO_VERSION)
        self.assertEqual(self.cgo.observe_linkage(self.binary),
                         {'needed': ['libthr.so.3', 'libc.so.7'], 'interpreters': ['/libexec/ld-elf.so.1']})

    def test_cgo0_foreign_target_and_missing_observer_refuse(self):
        cases = [({**CGO_SETTINGS, 'CGO_ENABLED': '0'}, CGO_SYMBOLS, 'CGO-enabled'),
                 ({key: value for key, value in CGO_SETTINGS.items() if key != 'CGO_ENABLED'}, CGO_SYMBOLS, 'CGO-enabled'),
                 ({**CGO_SETTINGS, 'GOOS': 'linux'}, CGO_SYMBOLS, 'settings'),
                 ({**CGO_SETTINGS, '-trimpath': 'false'}, CGO_SYMBOLS, 'settings'),
                 (CGO_SETTINGS, CGO_SYMBOLS[1:], 'observer symbols'),
                 (CGO_SETTINGS, CGO_SYMBOLS[:1] + CGO_SYMBOLS[2:], 'observer symbols'),
                 (CGO_SETTINGS, CGO_SYMBOLS[:2], 'observer symbols')]
        for settings, symbols, reason in cases:
            with self.subTest(settings=settings, symbols=symbols):
                with self.assertRaisesRegex(ValueError, reason):
                    self.cgo.check_cgo(self.facts(settings, symbols), GO_VERSION)
        with self.assertRaisesRegex(ValueError, 'build information'):
            self.cgo.check_cgo(self.facts(), 'go1.26.9')

    def test_stripped_or_unidentified_binary_refuses(self):
        self.binary.write_bytes(synthetic_elf(['libc.so.7'], symbols=None,
                                              buildinfo=buildinfo_blob(CGO_SETTINGS)))
        with self.assertRaisesRegex(ValueError, 'unstripped'):
            self.cgo.defined_symbols(self.binary)
        self.binary.write_bytes(synthetic_elf(['libc.so.7']))
        with self.assertRaisesRegex(ValueError, 'build information section'):
            self.cgo.go_buildinfo(self.binary)
        self.binary.write_bytes(synthetic_elf(['libc.so.7'], buildinfo=b'\xff Go buildinf:\x08\x01' + b'\0' * 16))
        with self.assertRaisesRegex(ValueError, 'header'):
            self.cgo.go_buildinfo(self.binary)
        with self.assertRaises(ValueError):
            self.cgo.parse_modinfo(b'go', b'\0' * 16 + b'build\tCGO_ENABLED=1' + b'\0' * 16)
        with self.assertRaisesRegex(ValueError, 'ambiguous'):
            self.cgo.parse_modinfo(b'go', b'\0' * 16 + b'build\tA=1\nbuild\tA=2\n' + b'\0' * 16)

    def test_source_closure_requires_cgo_observer_not_stub(self):
        files = {self.cgo.CGO_SOURCE: 'ab' * 32, self.cgo.CGO_HEADER: 'cd' * 32}
        self.cgo.check_source_closure({self.cgo.CONTROLLED_RUNTIME: {'files': files}})
        for change in [lambda f: f.update({self.cgo.NOCGO_SOURCE: 'ef' * 32}),
                       lambda f: f.pop(self.cgo.CGO_SOURCE), lambda f: f.pop(self.cgo.CGO_HEADER)]:
            mutated = dict(files)
            change(mutated)
            with self.subTest(files=mutated), self.assertRaisesRegex(ValueError, 'cgo process observer'):
                self.cgo.check_source_closure({self.cgo.CONTROLLED_RUNTIME: {'files': mutated}})
        with self.assertRaises(ValueError):
            self.cgo.check_source_closure({})

    def closure(self):
        capture, _ = self.cgo.verify_sysroot(self.sysroot)
        return self.cgo.resolve_closure(self.binary, self.sysroot, capture), capture

    def test_resolved_closure_matches_capture(self):
        closure, capture = self.closure()
        self.assertEqual(closure['needed'], ['libthr.so.3', 'libc.so.7'])
        self.assertEqual(set(closure['libraries']), {'libthr.so.3', 'libc.so.7', 'libsys.so.7'})
        self.assertEqual(closure['libraries']['libc.so.7']['needed'], ['libsys.so.7'])
        self.assertEqual(closure['interpreter']['sha256'], capture['files']['libexec/ld-elf.so.1']['sha256'])
        self.cgo.check_closure(self.cgo.observe_linkage(self.binary), closure, capture)

    def test_foreign_interpreter_dt_needed_and_sysroot_hash_refuse(self):
        closure, capture = self.closure()
        observed = self.cgo.observe_linkage(self.binary)
        cases = [
            ('foreign dynamic loader', lambda o, c, s: o.update(interpreters=['/lib64/ld-linux-x86-64.so.2'])),
            ('foreign dynamic loader', lambda o, c, s: o.update(interpreters=[])),
            ('foreign dynamic loader', lambda o, c, s: c['interpreter'].update(path='/usr/hfs/ld-elf.so.1')),
            ('interpreter sysroot hash', lambda o, c, s: c['interpreter'].update(sha256='ab' * 32)),
            ('DT_NEEDED', lambda o, c, s: o['needed'].append('libhfs.so.1')),
            ('DT_NEEDED', lambda o, c, s: c['needed'].append('libhfs.so.1')),
            ('DT_NEEDED', lambda o, c, s: c['needed'].pop()),
            ('DT_NEEDED', lambda o, c, s: c['needed'].reverse()),
            ('unreachable', lambda o, c, s: c['libraries'].update(extra=dict(c['libraries']['libc.so.7']))),
            ('missing library', lambda o, c, s: c['libraries'].pop('libsys.so.7')),
            ('libc.so.7', lambda o, c, s: s['files']['lib/libc.so.7'].update(sha256='ab' * 32)),
            ('libc.so.7', lambda o, c, s: c['libraries']['libc.so.7'].update(sha256='ab' * 32)),
            ('libc.so.7', lambda o, c, s: c['libraries']['libc.so.7'].update(redistributed=True)),
            ('libc.so.7', lambda o, c, s: c['libraries']['libc.so.7'].update(sysroot_path='lib/libsys.so.7')),
            # Same bytes, but outside rtld's /lib:/usr/lib search directories.
            ('libc.so.7', lambda o, c, s: (s['files'].update({'usr/local/lib/libc.so.7': s['files']['lib/libc.so.7']}),
                                           c['libraries']['libc.so.7'].update(sysroot_path='usr/local/lib/libc.so.7'))),
        ]
        for reason, change in cases:
            o, c, s = copy.deepcopy(observed), copy.deepcopy(closure), copy.deepcopy(capture)
            change(o, c, s)
            with self.subTest(reason=reason, closure=c), self.assertRaisesRegex(ValueError, reason):
                self.cgo.check_closure(o, c, s)

    def test_resolution_refuses_foreign_loader_or_uncaptured_library(self):
        capture, _ = self.cgo.verify_sysroot(self.sysroot)
        self.binary.write_bytes(synthetic_elf(['libc.so.7'], interpreter=b'/lib64/ld-linux-x86-64.so.2'))
        with self.assertRaisesRegex(ValueError, 'foreign dynamic loader'):
            self.cgo.resolve_closure(self.binary, self.sysroot, capture)
        self.binary.write_bytes(synthetic_elf(['libhfs.so.1']))
        with self.assertRaisesRegex(ValueError, 'absent from sysroot capture'):
            self.cgo.resolve_closure(self.binary, self.sysroot, capture)
        # A static (CGO0-style) binary has no dynamic section at all.
        self.binary.write_bytes(self.fixture_static())
        with self.assertRaises(ValueError):
            self.cgo.observe_linkage(self.binary)

    def fixture_static(self):
        raw = bytearray(synthetic_elf(['libc.so.7'], interpreter=None))
        # Turn PT_DYNAMIC into PT_NULL: a static executable has neither table.
        struct.pack_into('<I', raw, 64 + 56, 0)
        return bytes(raw)

    def test_sysroot_capture_is_closed_and_hashed(self):
        capture, value = self.cgo.verify_sysroot(self.sysroot)
        self.assertEqual(value, hashlib.sha256((self.sysroot / 'capture.json').read_bytes()).hexdigest())
        self.assertEqual(set(capture['files']), set(self.capture['files']))
        extra = self.sysroot / 'usr/include/injected.h'
        extra.write_bytes(b'#define injected 1\n')
        with self.assertRaisesRegex(ValueError, 'file set'):
            self.cgo.verify_sysroot(self.sysroot)
        extra.unlink()
        header = self.sysroot / 'usr/include/stdio.h'
        header.write_bytes(b'/* changed */\n')
        with self.assertRaisesRegex(ValueError, 'bytes mismatch'):
            self.cgo.verify_sysroot(self.sysroot)
        header.unlink()
        header.symlink_to('/etc/passwd')
        with self.assertRaisesRegex(ValueError, 'link or special'):
            self.cgo.verify_sysroot(self.sysroot)
        header.unlink()
        header.write_bytes(b'/* header */\n')
        (self.sysroot / 'capture.json').write_text(json.dumps({**self.capture, 'version': 'other/1'}))
        with self.assertRaisesRegex(ValueError, 'unsupported sysroot capture'):
            self.cgo.verify_sysroot(self.sysroot)

    def fake_wrapper(self, search=None):
        """A shell stand-in for the pinned wrapper. It answers the resource-dir
        and header-search probes like clang, and otherwise has the bound text
        that daemon.cgo_toolchain requires. /usr/bin/true stands in for clang
        and ld.lld, so their hashes and host library closures are real."""
        import daemon
        resource = self.root / 'resource'
        (resource / 'include').mkdir(parents=True, exist_ok=True)
        (resource / 'include/stddef.h').write_bytes(b'/* resource stddef */\n')
        search = search or str(self.sysroot / 'usr/include')
        wrapper = self.root / 'freebsd-cc'
        tool = '/usr/bin/true'
        text = ('#!/usr/bin/env bash\nlinker_args=(--ld-path=' + tool + ')\n'
                'case "$1" in\n -print-resource-dir) echo ' + str(resource) + '; exit 0;;\n'
                " -E) printf '%s\\n' '#include <...> search starts here:' ' " + str(resource / 'include')
                + "' ' " + search + "' 'End of search list.' >&2; exit 0;;\nesac\n"
                'exec ' + tool + ' --no-default-config \\\n --target=' + daemon.TARGET + ' \\\n'
                ' --sysroot=' + str(self.sysroot) + ' \\\n "${linker_args[@]}" "$@"\n')
        wrapper.write_text(text)
        wrapper.chmod(0o700)
        args = argparse.Namespace(cc_wrapper=wrapper, clang=Path(tool), lld=Path(tool), sysroot=self.sysroot)
        return args, text, resource

    def test_wrapper_must_bind_selected_tools_and_sysroot(self):
        import daemon
        args, good, _ = self.fake_wrapper()
        record = daemon.cgo_toolchain(args)
        self.assertEqual(record['wrapper']['sha256'], hashlib.sha256(good.encode()).hexdigest())
        self.assertEqual(record['target'], 'x86_64-unknown-freebsd15.1')
        for old, new in [(' --no-default-config', ''), (daemon.TARGET, 'x86_64-unknown-linux-gnu'),
                         ('--sysroot=' + str(self.sysroot), '--sysroot=/'),
                         ('--ld-path=/usr/bin/true', '--ld-path=/usr/bin/ld.bfd')]:
            args.cc_wrapper.write_text(good.replace(old, new))
            with self.subTest(old=old), self.assertRaisesRegex(ValueError, 'does not bind'):
                daemon.cgo_toolchain(args)
        with self.assertRaisesRegex(ValueError, 'explicit absolute'):
            daemon.explicit_path(Path('relative/freebsd-cc'), 'cc wrapper')

    def test_host_toolchain_inputs_recorded_and_change_detected(self):
        import daemon
        args, _, resource = self.fake_wrapper()
        first = daemon.cgo_toolchain(args)
        self.cgo.check_host_toolchain(first)
        self.assertEqual(set(first['resource_dir']['files']), {'include/stddef.h'})
        self.assertEqual(first['include_search'], [str(resource / 'include'), str(self.sysroot / 'usr/include')])
        self.assertTrue(any(name.startswith('libc.so') for name in first['clang']['libraries']))
        self.assertEqual(set(first['interpreter']), {'/usr/bin/env', shutil.which('bash', path='/usr/bin:/bin')})
        self.assertEqual(daemon.cgo_toolchain(args), first)
        # inputs_snapshot compares this record before and after the build, so
        # a resource header changed between snapshots must change the record.
        (resource / 'include/stddef.h').write_bytes(b'/* changed during build */\n')
        self.assertNotEqual(daemon.cgo_toolchain(args), first)
        (resource / 'include/stddef.h').write_bytes(b'/* resource stddef */\n')
        (resource / 'include/alias.h').symlink_to('stddef.h')
        self.assertEqual(daemon.cgo_toolchain(args)['resource_dir']['symlinks'], {'include/alias.h': 'stddef.h'})

    def test_header_search_must_stay_in_resource_dir_or_sysroot(self):
        import daemon
        args, _, _ = self.fake_wrapper(search='/usr/include')
        with self.assertRaisesRegex(ValueError, 'escapes resource dir and sysroot'):
            daemon.cgo_toolchain(args)
        for report in ['', 'End of search list.\n#include <...> search starts here:\n',
                       '#include <...> search starts here:\nEnd of search list.\n']:
            with self.subTest(report=report), self.assertRaises(ValueError):
                self.cgo.parse_search_dirs(report)

    def test_host_loader_parsing_and_admission_shape(self):
        sample = ('\tlinux-vdso.so.1 (0x00007f3d9c6e2000)\n'
                  '\tlibLLVM.so.22.1 => /lib64/libLLVM.so.22.1 (0x00007f3d90000000)\n'
                  '\t/lib64/ld-linux-x86-64.so.2 (0x00007f3d9c6e4000)\n')
        self.assertEqual(self.cgo.parse_ldd(sample), {'libLLVM.so.22.1': '/lib64/libLLVM.so.22.1',
                                                      '/lib64/ld-linux-x86-64.so.2': '/lib64/ld-linux-x86-64.so.2'})
        self.assertEqual(self.cgo.parse_ldd('\tstatically linked\n'), {})
        for bad in ['\tlibLLVM.so.22.1 => not found\n', '\tgarbage line\n',
                    sample + '\tlibLLVM.so.22.1 => /opt/libLLVM.so.22.1 (0x1)\n']:
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.cgo.parse_ldd(bad)
        library = {'libc.so.6': {'sha256': 'ab' * 32}}
        good = {'resource_dir': {'files': {'include/stddef.h': 'cd' * 32}},
                'include_search': ['/usr/lib/clang/22/include'],
                'clang': {'libraries': library}, 'lld': {'libraries': library},
                'interpreter': {'/usr/bin/bash': {'libraries': library}}}
        self.cgo.check_host_toolchain(good)
        cases = [('resource directory', lambda t: t['resource_dir'].update(files={})),
                 ('resource directory', lambda t: t['resource_dir']['files'].update({'include/stddef.h': 'x'})),
                 ('header search', lambda t: t.update(include_search=[])),
                 ('library closure', lambda t: t['clang'].update(libraries={})),
                 ('library closure', lambda t: t['lld'].update(libraries={'libc.so.6': {'sha256': None}})),
                 ('library closure', lambda t: t.update(interpreter={}))]
        for reason, change in cases:
            mutated = copy.deepcopy(good)
            change(mutated)
            with self.subTest(reason=reason), self.assertRaisesRegex(ValueError, reason):
                self.cgo.check_host_toolchain(mutated)

    def test_empty_banner_and_missing_loader_raise_value_errors(self):
        import daemon
        silent = self.root / 'silent'
        silent.write_text('#!/bin/sh\nexit 0\n')
        silent.chmod(0o700)
        with self.assertRaisesRegex(ValueError, 'empty version banner'):
            daemon.tool_record(silent, '--version')
        capture, _ = self.cgo.verify_sysroot(self.sysroot)
        del capture['files']['libexec/ld-elf.so.1']
        with self.assertRaisesRegex(ValueError, 'loader absent from sysroot capture'):
            self.cgo.resolve_closure(self.binary, self.sysroot, capture)


class ProposalTests(unittest.TestCase):
    def test_arithmetic_never_confers_placement_authority(self):
        import proposal
        current = proposal.proposal(16879837184)
        self.assertTrue(current['arithmetic_fits'])
        self.assertEqual(current['configured_total_bytes'], 15 << 30)
        self.assertEqual(current['logical_bytes'], 4 << 20)
        self.assertEqual(current['guest_signed_ceiling_bytes'], 2 << 30)
        self.assertFalse(current['placement_admitted'])
        self.assertIsNone(current['publication'])
        self.assertEqual(current['host_owner_reservations']['durable_c8_mmap_serializer'], 1 << 30)
        self.assertEqual(current['host_owner_reservations']['transport_arenas_parser'], 652 << 20)
        short = proposal.proposal(14 << 30)
        self.assertFalse(short['arithmetic_fits'])
        self.assertFalse(short['placement_admitted'])


if __name__ == '__main__':
    unittest.main()
