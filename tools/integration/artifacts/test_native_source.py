# SPDX-License-Identifier: BSD-2-Clause
"""Real tar/filesystem negative controls; native full extraction is separate."""
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import native_source


class SourceExtractionTests(unittest.TestCase):
    def fixture(self, name='sys/guard-fixture.h', symlink=False, commit=None):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        archive = root / 'freebsd-pinned.tar.gz'
        with tarfile.open(archive, 'w:gz', format=tarfile.PAX_FORMAT,
                          pax_headers={'comment': commit or native_source.REVISION}) as tar:
            item = tarfile.TarInfo('freebsd-src-' + native_source.REVISION + '/' + name)
            if symlink:
                item.type = tarfile.SYMTYPE
                item.linkname = '/etc/passwd'
                tar.addfile(item)
            else:
                item.size = 2
                tar.addfile(item, io.BytesIO(b'xx'))
        record = {'version': 'homelabfs-freebsd-source-capture/1',
                  'url': native_source.URL, 'revision': native_source.REVISION,
                  'pax_commit': native_source.REVISION,
                  'bytes': archive.stat().st_size,
                  'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()}
        (root / 'archive.capture.json').write_text(json.dumps(record))
        return root

    def test_captured_source_identity_or_bytes_mismatch(self):
        for field, value in [('version', 'unknown/2'), ('revision', 'ab' * 20),
                             ('url', 'https://example.invalid/wrong-source'),
                             ('pax_commit', 'ab' * 20), ('sha256', 'ab' * 32)]:
            root = self.fixture()
            path = root / 'archive.capture.json'
            record = json.loads(path.read_text())
            record[field] = value
            path.write_text(json.dumps(record))
            with self.subTest(field=field), self.assertRaises(ValueError):
                native_source.extract(root)
            self.assertFalse((root / 'source').exists())

    def test_real_archive_traversal_refuses_before_escape(self):
        root = self.fixture('sys/../../escape.h')
        with self.assertRaises(ValueError):
            native_source.extract(root)
        self.assertFalse((root / 'escape.h').exists())

    def test_selected_symlink_refuses_without_opening_target(self):
        root = self.fixture(symlink=True)
        with self.assertRaises(ValueError):
            native_source.extract(root)
        self.assertFalse((root / 'source').exists())

    def test_actual_pax_identity_is_rechecked(self):
        root = self.fixture(commit='ab' * 20)
        with self.assertRaises(ValueError):
            native_source.extract(root)
        self.assertFalse((root / 'source').exists())

    def test_source_budget_refuses_before_extracting_payload(self):
        root = self.fixture()
        with patch.object(native_source, 'MAX_SELECTED', 1), self.assertRaises(ValueError):
            native_source.extract(root)
        self.assertFalse((root / 'source').exists())
        self.assertFalse(json.loads((root / 'source.selection.json').read_text())['within_ceiling'])


if __name__ == '__main__':
    unittest.main()
