#!/usr/bin/env python3
"""Offline regressions for native-test-drive.py and native-test-guard.py.

No SSH, VM, mount or remote effect: only argument validation, archive
admission, command construction and the guard's working-directory checks run.
"""
import argparse
import contextlib
import importlib.util
import inspect
import io
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


DRIVE = load("native_test_drive", "native-test-drive.py")
GUARD = load("native_test_guard", "native-test-guard.py")
COMMIT = "62e0e2b368c12371c7cf2406a48bfdd14e7009d1"


def archive_bytes(members, commit=COMMIT):
    """Build an uncompressed tar shaped like `git archive` output."""
    buffer = io.BytesIO()
    headers = {"comment": commit} if commit else {}
    with tarfile.open(fileobj=buffer, mode="w", format=tarfile.PAX_FORMAT, pax_headers=headers) as archive:
        for info, data in members:
            archive.addfile(info, io.BytesIO(data) if data is not None else None)
    return buffer.getvalue()


def regular(name, data=b"x"):
    info = tarfile.TarInfo(name)
    info.size = len(data)
    return info, data


def special(name, kind, linkname=""):
    info = tarfile.TarInfo(name)
    info.type, info.linkname = kind, linkname
    return info, None


def check(raw, max_bytes=DRIVE.MAX_ARCHIVE_BYTES, max_members=DRIVE.MAX_ARCHIVE_MEMBERS):
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
        return DRIVE.check_archive_members(archive, max_bytes, max_members)


class ArchiveTests(unittest.TestCase):
    def test_git_shaped_tree_returns_commit(self):
        directory = tarfile.TarInfo("internal/")
        directory.type = tarfile.DIRTYPE
        raw = archive_bytes([(directory, None), regular("internal/a.go"), regular("go.mod")])
        self.assertEqual(check(raw), COMMIT)
        self.assertIsNone(check(archive_bytes([regular("go.mod")], commit=None)))

    def test_refused_members(self):
        cases = {
            "absolute": [regular("/etc/passwd")],
            "parent escape": [regular("src/../../escape")],
            "symlink": [special("link", tarfile.SYMTYPE, "/etc")],
            "hardlink": [special("hard", tarfile.LNKTYPE, "go.mod")],
            "fifo": [special("fifo", tarfile.FIFOTYPE)],
            "device": [special("dev", tarfile.CHRTYPE)],
        }
        for name, members in cases.items():
            with self.subTest(name), self.assertRaises(ValueError):
                check(archive_bytes(members))

    def test_bounds(self):
        with self.assertRaisesRegex(ValueError, "too many members"):
            check(archive_bytes([regular("a"), regular("b")]), max_members=1)
        with self.assertRaisesRegex(ValueError, "exceeds bound"):
            check(archive_bytes([regular("a", b"12"), regular("b", b"34")]), max_bytes=3)

    def test_read_source_archive_refusals(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "source.tar"
            path.write_bytes(b"not a tar archive" * 100)
            with self.assertRaisesRegex(ValueError, "uncompressed tar"):
                DRIVE.read_source_archive(path)
            compressed = Path(temp) / "source.tar.gz"
            with tarfile.open(compressed, "w:gz") as archive:
                archive.add(__file__, arcname="x.py")
            with self.assertRaisesRegex(ValueError, "uncompressed tar"):
                DRIVE.read_source_archive(compressed)
            with self.assertRaisesRegex(ValueError, "regular file"):
                DRIVE.read_source_archive(Path(temp))
            path.write_bytes(archive_bytes([regular("go.mod")]))
            with patch.object(DRIVE, "MAX_ARCHIVE_BYTES", 16), self.assertRaisesRegex(ValueError, "at most"):
                DRIVE.read_source_archive(path)
            self.assertEqual(DRIVE.read_source_archive(path)[1], COMMIT)

    def test_remote_stage_program_embeds_the_same_check(self):
        program = DRIVE.stage_program("/var/tmp/hfs-20261108-l34")
        compile(program, "<stage>", "exec")
        self.assertTrue(program.startswith(inspect.getsource(DRIVE.check_archive_members)))
        self.assertIn("filter='data'", program)
        self.assertNotIn("MAX_BYTES", program)
        namespace = {}
        exec(inspect.getsource(DRIVE.check_archive_members), namespace)
        with tarfile.open(fileobj=io.BytesIO(archive_bytes([regular("/abs")])), mode="r:") as archive:
            with self.assertRaises(ValueError):
                namespace["check_archive_members"](archive, 1 << 20, 10)


class DriveArgumentTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.artifacts = Path(temp.name) / "artifacts"
        self.artifacts.mkdir()
        for name in DRIVE.ARTIFACTS:
            (self.artifacts / name).write_bytes(b"\x7fELF" + name.encode())
        self.archive = Path(temp.name) / "source.tar"
        self.archive.write_bytes(archive_bytes([regular("internal/controlledruntime/process.go")]))
        self.out = Path(temp.name) / "out"

    def argv(self, *extra):
        return ["--target", "hfs-test0", "--root", "/var/tmp/hfs-20261108-l34", "--stage", "full-package",
                "--artifacts", str(self.artifacts), "--out", str(self.out), *extra]

    def refuse(self, argv, message):
        with contextlib.redirect_stderr(io.StringIO()) as errors, self.assertRaises(SystemExit):
            DRIVE.parse_args(argv)
        self.assertIn(message, errors.getvalue())

    def test_refusals(self):
        source = ["--source-archive", str(self.archive)]
        base = self.argv()
        cases = [
            (["--target", "f3-production"] + base[2:], "invalid choice"),
            (base[:3] + ["/tmp/hfs-2026x"] + base[4:], "--root must be"),
            (base[:3] + ["/var/tmp/hfs-2026x;rm"] + base[4:], "--root must be"),
            # Upper case could collide with the remote program placeholders.
            (base[:3] + ["/var/tmp/hfs-2026-SOURCE"] + base[4:], "--root must be"),
            (base[:5] + ["stage/../x"] + base[6:], "--stage must be"),
            (self.argv("--outer-seconds", "0"), "--outer-seconds"),
            (self.argv("--cwd", "src/internal"), "--cwd needs --source-archive"),
            (self.argv(*source, "--cwd", "/var/tmp/x"), "canonical relative"),
            (self.argv(*source, "--cwd", "src/../tmp"), "canonical relative"),
            (self.argv(*source, "--cwd", "src//internal"), "canonical relative"),
            (self.argv(*source, "--cwd", "src/./internal"), "canonical relative"),
            (self.argv(*source, "--cwd", "tmp"), "inside the staged source"),
            (self.argv(*source, "--skip-stage"), "cannot be combined"),
            (self.argv("--guard-args", "--profile guest --cwd /var/tmp"), "driver-owned"),
            (self.argv("--guard-args=--root=/var/tmp/hfs-2026y"), "driver-owned"),
        ]
        for argv, message in cases:
            with self.subTest(message=message, argv=argv):
                self.refuse(argv, message)
        (self.artifacts / "diagnostic-child").unlink()
        self.refuse(self.argv(), "missing artifacts: diagnostic-child")

    def test_source_staged_command(self):
        a = DRIVE.parse_args(self.argv("--source-archive", str(self.archive), "--cwd", "src/internal/controlledruntime",
                                       "--guard-args", "--profile guest --guest-hostname hfs-test0 --max-seconds 540",
                                       "--", "-test.count=1", "-test.v"))
        command = DRIVE.guard_command(a)
        root = "/var/tmp/hfs-20261108-l34"
        self.assertEqual(command[:3], [DRIVE.REMOTE_PYTHON, "-B", root + "/native-test-guard.py"])
        self.assertEqual(command[command.index("--cwd") + 1], root + "/src/internal/controlledruntime")
        self.assertLess(command.index("--cwd"), command.index("/usr/bin/env"))
        self.assertEqual(command[-3:], [root + "/controlledruntime-freebsd.test", "-test.count=1", "-test.v"])
        payload = DRIVE.staging_payload(a)
        self.assertEqual(set(payload["files"]), {*DRIVE.ARTIFACTS, "native-test-guard.py", "runner_process.py"})
        self.assertEqual(payload["source"]["commit"], COMMIT)

    def test_plain_command_has_no_cwd_or_source(self):
        a = DRIVE.parse_args(self.argv("--", "-test.run=TestX"))
        self.assertNotIn("--cwd", DRIVE.guard_command(a))
        self.assertNotIn("source", DRIVE.staging_payload(a))


class GuardTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve() / "hfs-2026run"
        self.inside = self.root / "src" / "internal" / "controlledruntime"
        self.inside.mkdir(parents=True)

    def test_cwd_accepts_root_and_real_inside_directory(self):
        self.assertEqual(GUARD.resolve_cwd(self.root, None), self.root)
        self.assertEqual(GUARD.resolve_cwd(self.root, self.inside), self.inside)

    def test_cwd_refusals(self):
        outside = self.root.parent / "outside"
        outside.mkdir()
        (self.root / "escape").symlink_to(outside)
        (self.root / "file").write_text("x")
        cases = {
            "relative": Path("src/internal"),
            "root itself": self.root,
            "outside": outside,
            "dotdot": self.inside / ".." / ".." / ".." / "outside",
            "symlink escape": self.root / "escape",
            "missing": self.root / "absent",
            "file": self.root / "file",
        }
        for name, cwd in cases.items():
            with self.subTest(name), self.assertRaises(ValueError):
                GUARD.resolve_cwd(self.root, cwd)
        with patch.object(GUARD.os, "geteuid", return_value=os.geteuid() + 1), self.assertRaisesRegex(ValueError, "owned"):
            GUARD.resolve_cwd(self.root, self.inside)

    def test_guard_options_are_exact(self):
        base = ["--root", "/var/tmp/hfs-2026x", "--stage", "s", "--local-reserved-bytes", "1", "--profile", "guest"]
        args = GUARD.parse_args(base + ["--cwd", "/var/tmp/hfs-2026x/src", "/bin/true"])
        self.assertEqual((args.cwd, args.command), (Path("/var/tmp/hfs-2026x/src"), ["/bin/true"]))
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            GUARD.parse_args(base + ["--cw", "/var/tmp/hfs-2026x/src", "/bin/true"])
        with self.assertRaises(ValueError):
            GUARD.check_request(argparse.Namespace(stage="bad/stage", command=["/bin/true"], local_reserved_bytes=1))


if __name__ == "__main__":
    unittest.main()
