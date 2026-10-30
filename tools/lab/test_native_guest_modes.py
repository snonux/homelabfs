#!/usr/bin/env python3
"""Local shell guard regressions using real temporary inode modes; no lab I/O."""

import os
from pathlib import Path
import shlex
import stat
import subprocess
import sys
import tempfile
import unittest

GUEST = Path(__file__).with_name("native-single-node-guest.sh")
PLAN = "a" * 64

# FreeBSD stat's %Op is the complete octal st_mode; %Lp is only rwx bits.
# Map this unprivileged test owner's UID to guest root. Everything else, including
# modes and link counts, comes from actual temporary inodes. No chown is needed.
STAT_BRIDGE = r'''
import os
from pathlib import Path
import re
import sys

_, flag, fmt, path = sys.argv
assert flag == "-f"
s = Path(path).lstat()
uid = 0 if s.st_uid == os.getuid() else s.st_uid
if path == os.environ.get("HFS_TEST_FOREIGN_OWNER"):
    uid = 20001
fields = {"%Op": format(s.st_mode, "o"), "%Lp": format(s.st_mode & 0o777, "o"),
          "%u": str(uid), "%l": str(s.st_nlink)}
if "HFS_TEST_BAD_MODE" in os.environ:
    fields["%Op"] = os.environ["HFS_TEST_BAD_MODE"]
print(re.sub(r"%Op|%Lp|%u|%l", lambda m: fields[m[0]], fmt))
'''

# Read-only ZFS/df answers admit the disposable local fixture to the exact
# shipped _datasets guard. These functions never call real ZFS, mounts or SSH.
SHELL_BRIDGE = r'''
stat() { "$HFS_TEST_PYTHON" "$HFS_TEST_STAT" "$@"; }
id() { printf '0\n'; }
hostname() { printf 'hfs-test0\n'; }
zfs() {
    test "$1" = get || return 99
    case "$4:$5" in
        source:org.homelabfs:plan) printf 'local\n' ;;
        value:guid) printf '123\n' ;;
        value:org.homelabfs:plan) printf '%s\n' "$plan" ;;
        value:mountpoint)
            case "$6" in
                */data/*) printf '%s/d\n' "$work" ;;
                */journal/*) printf '%s/j\n' "$work" ;;
                */control/*) printf '%s/c\n' "$work" ;;
                *) return 99 ;;
            esac ;;
        value:mounted) printf 'yes\n' ;;
        value:readonly) printf 'off\n' ;;
        value:quota) printf '134217728\n' ;;
        *) return 99 ;;
    esac
}
df() {
    case "$2" in
        */d) kind=data ;; */j) kind=journal ;; */c) kind=control ;;
        *) return 99 ;;
    esac
    printf 'Filesystem Type\nhfsdata0/%s/hfs-run-test zfs\n' "$kind"
}
sha256() { printf '%s\n' "$plan"; }
'''


class NativeGuestModeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="hfs-mode-guards-")
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.root = self.base / "root"
        self.var = self.root / "var"
        self.tmp = self.var / "tmp"
        self.tmp.mkdir(parents=True)
        for path, mode in ((self.root, 0o755), (self.var, 0o755), (self.tmp, 0o1777)):
            path.chmod(mode)
        self.work = self.base / "work"
        self.work.mkdir(mode=0o711)
        for name in ("evidence", "d", "j", "c"):
            (self.work / name).mkdir(mode=0o700)
        (self.work / "plan").write_text(PLAN + "\n")
        (self.work / "plan").chmod(0o600)
        (self.work / "datasets").write_text(
            "data 123 123\njournal 123 123\ncontrol 123 123\n")
        (self.work / "datasets").chmod(0o600)
        for leaf in ("d", "j", "c"):
            marker = self.work / leaf / "fixture.json"
            marker.write_text("{}\n")
            marker.chmod(0o600)
        self.stat_bridge = self.base / "stat.py"
        self.stat_bridge.write_text(STAT_BRIDGE)
        self.env = dict(os.environ, HFS_TEST_BASE=str(self.base),
                        HFS_TEST_PYTHON=sys.executable,
                        HFS_TEST_STAT=str(self.stat_bridge))

        # Execute the shipped header and guard functions under /bin/sh. Redirect
        # only its fixed filesystem roots and identity/stat/ZFS command boundary.
        source = GUEST.read_text()
        guards = source.split('\ncase "$action" in\n', 1)[0]
        guards = guards.replace('work="/var/tmp/hfs-run-$run"',
                                'work="$HFS_TEST_BASE/work"')
        guards = guards.replace('for directory in / /var /var/tmp; do',
                                'for directory in ' + ' '.join(
                                    shlex.quote(str(p)) for p in (self.root, self.var, self.tmp)) + '; do')
        guards = guards.replace('for directory in / /var; do',
                                'for directory in ' + ' '.join(
                                    shlex.quote(str(p)) for p in (self.root, self.var)) + '; do')
        guards = guards.replace('_permissions /var/tmp)',
                                '_permissions ' + shlex.quote(str(self.tmp)) + ')')
        self.guards = guards
        self.markers = source.split('        for pair in data:d journal:j control:c; do\n', 1)[1]
        self.markers = ('for pair in data:d journal:j control:c; do\n' +
                        self.markers.split('        mount -p >', 1)[0])

    def run_guard(self, command=":", **environment):
        return subprocess.run(
            ["/bin/sh", "-s", "--", "guard-test", "test", "0", PLAN, PLAN, PLAN],
            input=SHELL_BRIDGE + self.guards + '\n' + command + '\n',
            env=dict(self.env, **environment), capture_output=True, text=True,
            timeout=10)

    def assert_guard(self, accepted, command=":", **environment):
        result = self.run_guard(command, **environment)
        if accepted:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_captured_freebsd_stat_semantics_match_actual_sticky_inode(self):
        # f3 read-only observation, 2026-10-30 09:22:35 UTC, original artifact
        # n5-restoration-observation.json: %Op %Lp %Mp %Sp /var/tmp produced
        # "41777 777 1 drwxrwxrwt". Do not emulate %Lp as 1777.
        self.assertEqual(stat.S_IMODE(self.tmp.stat().st_mode), 0o1777)
        result = self.run_guard(
            f"stat -f '%Op %Lp' {shlex.quote(str(self.tmp))}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "41777 777\n")

    def test_shared_tmp_requires_exact_sticky_mode(self):
        for mode in (0o1777, 0o777, 0o3777, 0o5777, 0o7777, 0o1775):
            with self.subTest(mode=oct(mode)):
                self.tmp.chmod(mode)
                self.assertEqual(stat.S_IMODE(self.tmp.stat().st_mode), mode)
                self.assert_guard(mode == 0o1777)

    def test_private_inodes_reject_every_unexpected_special_bit(self):
        targets = [(self.work, 0o711, "_owned"),
                   (self.work / "plan", 0o600, "_owned"),
                   (self.work / "datasets", 0o600, "_datasets")]
        targets += [(self.work / leaf, 0o700, "_datasets") for leaf in ("d", "j", "c")]
        targets += [(self.work / leaf / "fixture.json", 0o600,
                     'role=root profile=hfs-direct\n' + self.markers) for leaf in ("d", "j", "c")]
        for path, mode, guard in targets:
            with self.subTest(path=str(path.relative_to(self.base)), mode=oct(mode)):
                self.assert_guard(True, guard)
                for special in (0o1000, 0o2000, 0o4000):
                    with self.subTest(special=oct(special)):
                        path.chmod(mode | special)
                        self.assertEqual(stat.S_IMODE(path.stat().st_mode), mode | special)
                        self.assert_guard(False, guard)
                path.chmod(mode | 0o004)
                self.assert_guard(False, guard)
                path.chmod(mode)

    def test_non_octal_empty_and_oversized_stat_output_fail_closed(self):
        for value in ("", "88888", "-1", "41777+1", "$(id)", "0" * 100, "41777\n41777"):
            with self.subTest(value=value):
                self.assert_guard(False, HFS_TEST_BAD_MODE=value)

    def test_existing_owner_symlink_type_and_link_count_guards_remain(self):
        for path, command in ((self.tmp, ":"), (self.work, "_owned"),
                              (self.work / "plan", "_owned"),
                              (self.work / "datasets", "_datasets"),
                              (self.work / "d", "_datasets"),
                              (self.work / "d/fixture.json", 'role=root profile=hfs-direct\n' + self.markers)):
            with self.subTest(foreign_owner=str(path)):
                self.assert_guard(False, command, HFS_TEST_FOREIGN_OWNER=str(path))
        for name, command in (("plan", "_owned"), ("datasets", "_datasets"),
                              ("d/fixture.json", 'role=root profile=hfs-direct\n' + self.markers)):
            path = self.work / name
            other = path.with_name(path.name + ".other")
            with self.subTest(hardlink=name):
                os.link(path, other)
                self.assert_guard(False, command)
                other.unlink()
            with self.subTest(symlink=name):
                path.rename(other)
                path.symlink_to(other)
                self.assert_guard(False, command)
                path.unlink()
                other.rename(path)
            with self.subTest(wrong_type=name):
                path.rename(other)
                path.mkdir(mode=0o600)
                self.assert_guard(False, command)
                path.rmdir()
                other.rename(path)
        for path, command in ((self.tmp, ":"), (self.work, "_owned"),
                              (self.work / "d", "_datasets")):
            with self.subTest(symlink_directory=str(path)):
                other = path.with_name(path.name + ".other")
                path.rename(other)
                path.symlink_to(other, target_is_directory=True)
                self.assert_guard(False, command)
                path.unlink()
                other.rename(path)
        self.var.chmod(0o775)
        self.assert_guard(False)


if __name__ == "__main__":
    unittest.main()
