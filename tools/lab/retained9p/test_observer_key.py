#!/usr/bin/env python3
"""Local tests of the run-only observer key tool (G6); no SSH, sshd or root.

OS boundaries are simulated explicitly: TRUSTED_UID is the test user, the run
parent and authorized_keys live in a temporary directory, the "pinned
interpreter" is a copy of this Python, and the public key is a synthetic
ssh-ed25519 blob (no key generation). SELinux labels are mocked.
"""
import ast
import base64
import contextlib
import errno
import hashlib
import io
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from test_preparation import controller, module

observer_key = module("observer_key")
HERE = Path(__file__).resolve().parent
PROBE = HERE.parents[2] / "internal/controlledruntime/probe/installed.py"
OBSERVER = HERE / "observer.py"
PAUL = b"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeExistingOperatorKeyForTestsOnly00000 paul@workstation\n"


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def synthetic_key(seed):
    blob = b"\0\0\0\x0bssh-ed25519\0\0\0\x20" + hashlib.sha256(seed).digest()
    return base64.b64encode(blob).decode()


class ObserverKeyTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        ssh = self.root / "root-ssh"
        ssh.mkdir(mode=0o700)
        self.keys = ssh / "authorized_keys"
        self.keys.write_bytes(PAUL)
        self.keys.chmod(0o600)
        self.python = self.root / "python3.9"
        shutil.copy(sys.executable, self.python)
        self.python.chmod(0o755)
        self.public = self.root / "observer.pub"
        self.encoded = synthetic_key(b"observer")
        self.public.write_text("ssh-ed25519 " + self.encoded + " hfs-zq3-r1-observer\n")
        for name, value in [("TRUSTED_UID", os.getuid()), ("RUN_PARENT", str(self.root / "hfs-zq3-")),
                            ("AUTHORIZED_KEYS", str(self.keys))]:
            patch = mock.patch.object(observer_key, name, value)
            patch.start()
            self.addCleanup(patch.stop)
        self.run_dir = self.root / "hfs-zq3-r1"

    def call(self, *argv):
        printed = io.StringIO()
        with contextlib.redirect_stdout(printed):
            self.assertEqual(observer_key.main(list(argv)), 0)
        return json.loads(printed.getvalue())

    def common(self, run_id="r1"):
        return ["--run-id", run_id, "--observer-sha256", sha(OBSERVER),
                "--python", str(self.python), "--python-sha256", sha(self.python)]

    def stage(self):
        return self.call("stage", *self.common(), "--observer", str(OBSERVER), "--probe", str(PROBE))

    def enrol(self, expect=None):
        return self.call("enrol", *self.common(), "--public-key", str(self.public),
                         "--expect-sha256", expect or sha(self.keys))

    def refuses(self, pattern, *argv):
        before = self.keys.read_bytes()
        with self.assertRaisesRegex(observer_key.Refused, pattern):
            observer_key.main(list(argv))
        self.assertEqual(self.keys.read_bytes(), before)

    def test_full_cycle_restores_exact_bytes_and_preserves_operator_key(self):
        staged = self.stage()
        self.assertEqual(staged["probe_sha256"], sha(PROBE))
        for name in ["observer.py", "installed-probe.py"]:
            self.assertEqual((self.run_dir / name).stat().st_mode & 0o777, 0o400)
        self.assertEqual(sorted(os.listdir(self.run_dir)), ["installed-probe.py", "m0", "m1", "observer.py"])
        self.assertEqual(self.run_dir.stat().st_mode & 0o777, 0o700)
        pre = sha(self.keys)
        enrolled = self.enrol()
        current = self.keys.read_bytes()
        self.assertTrue(current.startswith(PAUL))
        self.assertEqual(current.count(b"\n"), 2)
        self.assertEqual((enrolled["before_sha256"], enrolled["after_sha256"]), (pre, sha(self.keys)))
        self.assertEqual(self.keys.stat().st_mode & 0o777, 0o600)
        status = self.call("status", "--run-id", "r1")
        self.assertEqual(status["enrolled_line_count"], 1)
        removed = self.call("remove", "--run-id", "r1", "--expect-sha256", enrolled["after_sha256"])
        self.assertTrue(removed["restored_pre_run"])
        self.assertFalse(removed["unrelated_changes_acknowledged"])
        self.assertEqual(self.keys.read_bytes(), PAUL)
        self.assertTrue((self.run_dir / "observer-key-removed.json").exists())
        self.refuses("already removed", "remove", "--run-id", "r1", "--expect-sha256", pre)

    def test_line_is_restricted_source_pinned_and_forces_observer(self):
        self.stage()
        self.enrol()
        line = self.keys.read_bytes()[len(PAUL):].decode()
        options, rest = line.split('" ssh-ed25519 ')
        self.assertTrue(options.startswith('restrict,from="10.20.0.133",command="'))
        self.assertEqual(rest, self.encoded + " hfs-zq3-r1-observer\n")
        command = shlex.split(options.split('command="', 1)[1])
        self.assertEqual(command, [str(self.python), "-I", str(self.run_dir / "observer.py"),
                                   "--probe", str(self.run_dir / "installed-probe.py"),
                                   "--probe-sha256", sha(PROBE), "--python", str(self.python),
                                   "--python-sha256", sha(self.python)])
        self.assertEqual(observer_key.OBSERVER_FROM, controller.TARGETS["host"][0])

    def test_forced_command_is_accepted_by_staged_observer_arguments(self):
        # Executes the forced command with the copied interpreter. Not root, so
        # observer.py must get past argparse and refuse at its root check.
        self.stage()
        self.enrol()
        line = self.keys.read_bytes()[len(PAUL):].decode()
        command = shlex.split(line.split('command="', 1)[1].split('" ssh-ed25519 ')[0])
        env = {"PATH": "/usr/bin:/bin", "SSH_ORIGINAL_COMMAND": "python3 -I -c 'x'"}
        result = subprocess.run(command, capture_output=True, text=True, timeout=20, env=env)
        if os.geteuid() == 0:
            self.skipTest("root would pass the observer's root check")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("trusted guest-root observer required", result.stderr)

    def test_not_root_refuses_before_any_change(self):
        if os.geteuid() == 0:
            self.skipTest("test user is root")
        with mock.patch.object(observer_key, "TRUSTED_UID", 0):
            self.refuses("trusted guest root", "stage", *self.common(),
                         "--observer", str(OBSERVER), "--probe", str(PROBE))
            self.refuses("trusted guest root", "enrol", *self.common(),
                         "--public-key", str(self.public), "--expect-sha256", sha(self.keys))
            self.refuses("trusted guest root", "remove", "--run-id", "r1", "--expect-sha256", sha(self.keys))
        self.assertFalse(self.run_dir.exists())

    def test_stage_refuses_wrong_pins(self):
        wrong = self.root / "other.py"
        wrong.write_text("print('not the probe')\n")
        cases = [("probe", ["--observer", str(OBSERVER), "--probe", str(wrong)]),
                 ("observer.py", ["--observer", str(wrong), "--probe", str(PROBE)])]
        for label, extra in cases:
            with self.subTest(changed=label):
                self.refuses("SHA256 differs", "stage", *self.common(), *extra)
        args = self.common()
        args[-1] = "0" * 64
        self.refuses("interpreter", "stage", *args, "--observer", str(OBSERVER), "--probe", str(PROBE))
        self.assertFalse(self.run_dir.exists())

    def test_stage_refuses_existing_files_and_insecure_run_directory(self):
        self.stage()
        with self.assertRaises(FileExistsError):
            self.stage()
        other = self.root / "hfs-zq3-r2"
        other.mkdir(mode=0o755)
        other.chmod(0o755)
        with self.assertRaisesRegex(observer_key.Refused, "0700 run directory"):
            self.call("stage", *self.common("r2"), "--observer", str(OBSERVER), "--probe", str(PROBE))

    def test_unsafe_names_refuse(self):
        for run_id in ["R1", "_r", "r1;x", "a" * 25, "r 1"]:
            with self.subTest(run_id=run_id):
                self.refuses("run ID", "status", "--run-id", run_id)
        for python in ["/tmp/py thon", '/tmp/"py', "relative/python", "/tmp/../usr/bin/python3"]:
            with self.subTest(python=python):
                args = self.common()
                args[5] = python
                self.refuses("safe path", "stage", *args, "--observer", str(OBSERVER), "--probe", str(PROBE))

    def test_enrol_refuses_unexpected_pre_run_state(self):
        self.stage()
        self.refuses("pre-run value", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", "0" * 64)
        self.assertFalse((self.run_dir / "observer-key.json").exists())
        self.keys.write_bytes(PAUL.rstrip(b"\n"))
        self.refuses("end with a newline", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", sha(self.keys))
        self.keys.write_bytes(PAUL + b"ssh-ed25519 " + self.encoded.encode() + b" reused\n")
        self.refuses("already present", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", sha(self.keys))

    def test_enrol_refuses_insecure_authorized_keys(self):
        self.stage()
        self.keys.chmod(0o620)
        self.refuses("group/other write", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", sha(self.keys))
        self.keys.chmod(0o600)
        os.link(self.keys, self.root / "second-link")
        self.refuses("one link", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", sha(self.keys))

    def test_enrol_refuses_changed_staged_files_or_interpreter(self):
        self.stage()
        staged = self.run_dir / "observer.py"
        staged.chmod(0o600)
        staged.write_text("tampered\n")
        staged.chmod(0o400)
        self.refuses("staged observer", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", sha(self.keys))
        args = self.common()
        args[-1] = "f" * 64
        self.refuses("interpreter", "enrol", *args, "--public-key", str(self.public),
                     "--expect-sha256", sha(self.keys))

    def test_public_key_shape_refuses(self):
        self.stage()
        rsa = base64.b64encode(b"\0\0\0\x07ssh-rsa" + b"\1" * 40).decode()
        for label, text in [("wrong comment", "ssh-ed25519 " + self.encoded + " hfs-zq3-r2-observer\n"),
                            ("no comment", "ssh-ed25519 " + self.encoded + "\n"),
                            ("two lines", ("ssh-ed25519 " + self.encoded + " hfs-zq3-r1-observer\n") * 2),
                            ("no newline", "ssh-ed25519 " + self.encoded + " hfs-zq3-r1-observer"),
                            ("rsa type", "ssh-rsa " + rsa + " hfs-zq3-r1-observer\n"),
                            ("rsa blob", "ssh-ed25519 " + rsa + " hfs-zq3-r1-observer\n"),
                            ("options", 'command="sh" ssh-ed25519 ' + self.encoded + " hfs-zq3-r1-observer\n"),
                            ("bad base64", "ssh-ed25519 !!!! hfs-zq3-r1-observer\n")]:
            with self.subTest(key=label):
                self.public.write_text(text)
                self.refuses("public key", "enrol", *self.common(), "--public-key", str(self.public),
                             "--expect-sha256", sha(self.keys))

    def test_second_enrol_for_same_run_refuses(self):
        self.stage()
        enrolled = self.enrol()
        self.public.write_text("ssh-ed25519 " + synthetic_key(b"other") + " hfs-zq3-r1-observer\n")
        self.refuses("already present", "enrol", *self.common(), "--public-key", str(self.public),
                     "--expect-sha256", enrolled["after_sha256"])

    def test_remove_refuses_unexpected_hash_or_duplicated_line(self):
        self.stage()
        enrolled = self.enrol()
        self.refuses("expected current", "remove", "--run-id", "r1", "--expect-sha256", "0" * 64)
        line = self.keys.read_bytes()[len(PAUL):]
        self.keys.write_bytes(PAUL + line + line)
        self.refuses("2 times", "remove", "--run-id", "r1", "--expect-sha256", sha(self.keys))
        self.assertTrue((self.run_dir / "observer-key.json").exists())
        self.assertNotEqual(enrolled["after_sha256"], sha(self.keys))

    def test_remove_preserves_unrelated_change_when_acknowledged(self):
        self.stage()
        self.enrol()
        unrelated = b"ssh-ed25519 " + synthetic_key(b"unrelated").encode() + b" added-meanwhile\n"
        self.keys.write_bytes(self.keys.read_bytes() + unrelated)
        removed = self.call("remove", "--run-id", "r1", "--expect-sha256", sha(self.keys))
        self.assertEqual(self.keys.read_bytes(), PAUL + unrelated)
        self.assertFalse(removed["restored_pre_run"])
        self.assertTrue(removed["unrelated_changes_acknowledged"])

    def test_interrupted_enrol_leaves_receipt_and_remove_retires_it(self):
        self.stage()
        pre = sha(self.keys)
        with mock.patch.object(observer_key, "replace_keys", side_effect=OSError("crash")):
            with self.assertRaisesRegex(OSError, "crash"):
                self.enrol()
        self.assertEqual(self.keys.read_bytes(), PAUL)
        status = self.call("status", "--run-id", "r1")
        self.assertEqual((status["enrolled_line_count"], status["receipt"]["before_sha256"]), (0, pre))
        removed = self.call("remove", "--run-id", "r1", "--expect-sha256", pre)
        self.assertTrue(removed["restored_pre_run"])
        self.assertEqual(self.keys.read_bytes(), PAUL)

    def test_concurrent_change_before_rename_refuses_and_cleans_pending(self):
        self.stage()
        actual = observer_key.read_keys
        calls = []

        def changed_second_time():
            raw, info = actual()
            calls.append(1)
            return (raw + b"# concurrent\n", info) if len(calls) == 3 else (raw, info)

        with mock.patch.object(observer_key, "read_keys", side_effect=changed_second_time):
            self.refuses("changed during the update", "enrol", *self.common(),
                         "--public-key", str(self.public), "--expect-sha256", sha(self.keys))
        self.assertEqual(sorted(p.name for p in self.keys.parent.iterdir()), ["authorized_keys"])

    def test_selinux_label_is_copied_and_failure_preserves_original(self):
        self.stage()
        label = b"system_u:object_r:ssh_home_t:s0"
        written = []
        with mock.patch.object(observer_key.os, "getxattr", return_value=label), \
                mock.patch.object(observer_key.os, "setxattr",
                                  side_effect=lambda fd, name, value: written.append((name, value))):
            self.enrol()
        self.assertEqual(written, [("security.selinux", label)])
        after = sha(self.keys)
        with mock.patch.object(observer_key.os, "getxattr", return_value=label), \
                mock.patch.object(observer_key.os, "setxattr", side_effect=PermissionError("relabel")):
            with self.assertRaises(PermissionError):
                self.call("remove", "--run-id", "r1", "--expect-sha256", after)
        self.assertEqual(sha(self.keys), after)
        self.assertEqual(sorted(p.name for p in self.keys.parent.iterdir()), ["authorized_keys"])

    def test_unexpected_label_read_error_refuses(self):
        self.stage()
        failure = OSError(errno.EACCES, "denied")
        with mock.patch.object(observer_key.os, "getxattr", side_effect=failure):
            with self.assertRaises(PermissionError):
                self.enrol()
        self.assertEqual(self.keys.read_bytes(), PAUL)

    def test_concurrent_invocation_lock_refuses(self):
        self.stage()
        held = observer_key.locked(self.run_dir)
        try:
            self.refuses("another observer_key", "enrol", *self.common(),
                         "--public-key", str(self.public), "--expect-sha256", sha(self.keys))
        finally:
            os.close(held)

    def stage_args(self, python):
        args = self.common()
        args[5], args[7] = str(python), sha(self.python)
        return ["stage", *args, "--observer", str(OBSERVER), "--probe", str(PROBE)]

    def test_symlinked_interpreter_refuses(self):
        # O_NOFOLLOW: even a link to the exact pinned bytes refuses.
        link = self.root / "python3"
        link.symlink_to(self.python)
        before = self.keys.read_bytes()
        with self.assertRaises(OSError) as raised:
            observer_key.main(self.stage_args(link))
        self.assertEqual(raised.exception.errno, errno.ELOOP)
        self.assertEqual(self.keys.read_bytes(), before)
        self.assertFalse(self.run_dir.exists())

    def test_group_or_other_writable_interpreter_refuses(self):
        for mode in [0o775, 0o757]:
            with self.subTest(mode=oct(mode)):
                self.python.chmod(mode)
                self.refuses("without group/other write", *self.stage_args(self.python))
        self.assertFalse(self.run_dir.exists())

    def test_interpreter_not_owned_by_trusted_uid_refuses(self):
        # Simulated: the trusted UID is someone else, the root check passes.
        with mock.patch.object(observer_key, "TRUSTED_UID", os.getuid() + 1), \
                mock.patch.object(observer_key, "require_root"):
            self.refuses("root-owned", *self.stage_args(self.python))
        self.assertFalse(self.run_dir.exists())

    def test_interpreter_refusals_also_block_enrol(self):
        self.stage()
        self.python.chmod(0o775)
        self.refuses("without group/other write", "enrol", *self.common(),
                     "--public-key", str(self.public), "--expect-sha256", sha(self.keys))
        self.assertFalse((self.run_dir / "observer-key.json").exists())

    def test_stale_pending_file_is_reported_and_blocks_replacement(self):
        self.stage()
        stale = self.keys.parent / ".authorized_keys.hfs-observer-next"
        stale.write_bytes(b"left by a killed run\n")
        self.assertTrue(self.call("status", "--run-id", "r1")["stale_pending"])
        with self.assertRaises(FileExistsError):
            self.enrol()
        self.assertEqual(self.keys.read_bytes(), PAUL)
        self.assertEqual(stale.read_bytes(), b"left by a killed run\n")

    def test_malformed_receipt_refuses_cleanly(self):
        self.stage()
        self.enrol()
        receipt = self.run_dir / "observer-key.json"
        original = json.loads(receipt.read_text())
        for label, change in [("line not string", {"line": 7}), ("line missing", {"line": None}),
                              ("hash not string", {"after_sha256": 1}),
                              ("non-ascii line", {"line": "\u00e9"}),
                              ("line hash mismatch", {"line_sha256": "0" * 64}),
                              ("other run", {"run_id": "r2"})]:
            with self.subTest(receipt=label):
                receipt.chmod(0o600)
                receipt.write_text(json.dumps({**original, **change}))
                self.refuses("does not describe this run", "remove", "--run-id", "r1",
                             "--expect-sha256", sha(self.keys))
                self.refuses("does not describe this run", "status", "--run-id", "r1")

    def test_pins_and_python39_syntax(self):
        self.assertEqual(observer_key.PROBE_SHA256, sha(PROBE))
        source = (HERE / "observer_key.py").read_text()
        ast.parse(source, feature_version=(3, 9))


if __name__ == "__main__":
    unittest.main()
