# SPDX-License-Identifier: BSD-2-Clause
"""Offline structural/digest controls; synthetic ELF notes are parser fixtures."""
import copy
import hashlib
import json
import os
from pathlib import Path
import struct
import tempfile
import unittest

import elf
import package


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
