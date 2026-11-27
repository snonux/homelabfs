# SPDX-License-Identifier: GPL-2.0-only
"""build.py object-check planning; no kernel tree, make or compiler is used."""
from pathlib import Path
import sys
import tempfile
import unittest

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


if __name__ == '__main__':
    unittest.main()
