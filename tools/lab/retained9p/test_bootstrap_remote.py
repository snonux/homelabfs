#!/usr/bin/env python3
"""Local tests of bootstrap_remote.py (zq3 G1 remote half).

The f3 and Rocky sides run against a temporary root: sysfs, /proc, the
EnrollmentFile directory and the guard records are plain files there, and the
remote children (rocky_kernel.py admission, mount, the guest-stat lstat) are
scripted; only the lstat program itself also runs for real against the
temporary root. Root ownership is substituted by the test user's UID.
Nothing touches the real system, a VM or the network.
"""
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("bootstrap_remote_under_test", HERE / "bootstrap_remote.py")
remote = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(remote)

BOOT = "327664ad-2d2c-4e45-ab0e-a43b3f8fcd6c"
NONCE = "ce154025-a59c-4852-bce2-0d2f62af791c"
RELEASE = "6.1.190-hfs-xz3"
SCRIPT = b"# rocky_kernel.py stand-in\n"
MANIFEST = b'{"version":"homelabfs-installed-artifacts/1"}'


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


class FakeSystem(remote.System):
    """Temporary root, the test UID as 'root', scripted children."""

    def __init__(self, root):
        super().__init__(str(root), os.geteuid())
        self.children, self.outcomes, self.on_mount = [], [], None

    def release(self):
        return RELEASE

    def run(self, argv, seconds):
        self.children.append((argv, seconds))
        outcome = self.outcomes.pop(0)
        if argv[0] == remote.MOUNT and self.on_mount:
            self.on_mount(argv)
        return outcome


class RemoteCase(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.system = FakeSystem(self.root)
        self.put("/proc/sys/kernel/random/boot_id", BOOT + "\n")
        self.put("/proc/self/mountinfo", "22 1 0:21 / /proc rw - proc proc rw\n")

    def put(self, path, text, mode=0o600):
        target = self.root / path.lstrip("/")
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(text if isinstance(text, bytes) else text.encode())
        target.chmod(mode)
        return target

    def act(self, argv, payload=b""):
        out = io.StringIO()
        code = remote.main(argv, self.system, io.BytesIO(payload), out)
        return code, json.loads(out.getvalue())


class HostTests(RemoteCase):
    ENROLL = "/var/tmp/run/runtime/handoff/enrollment.json"

    def install(self, payload, size=None, digest=None):
        return self.act(["host-install", "--path", self.ENROLL, "--sha256", digest or sha(payload),
                         "--size", str(size or len(payload))], payload)

    def handoff_directory(self):
        directory = self.root / "var/tmp/run/runtime/handoff"
        directory.mkdir(parents=True)
        directory.chmod(0o700)
        return directory

    def test_install_places_one_private_single_link_file(self):
        directory = self.handoff_directory()
        code, result = self.install(b'{"Authorization":{}}')
        self.assertEqual((code, result["ok"]), (0, True))
        info = os.lstat(directory / "enrollment.json")
        self.assertEqual((stat.S_IMODE(info.st_mode), info.st_nlink), (0o600, 1))
        self.assertEqual(sorted(os.listdir(directory)), ["enrollment.json"])

    def test_install_never_overwrites_and_checks_the_payload(self):
        directory = self.handoff_directory()
        (directory / "enrollment.json").write_bytes(b"earlier")
        code, result = self.install(b"new")
        self.assertEqual((code, result["changed"]), (2, False))
        self.assertEqual((directory / "enrollment.json").read_bytes(), b"earlier")
        (directory / "enrollment.json").unlink()
        code, result = self.install(b"new", digest="0" * 64)
        self.assertEqual((code, result["changed"]), (2, False))
        self.assertEqual(os.listdir(directory), [])

    def test_poll_reports_stdout_and_guard_records(self):
        code, result = self.act(["host-poll", "--guard-dir", "/var/tmp/run/guard"])
        self.assertEqual((code, result["guard_dir"]), (0, False))
        self.put("/var/tmp/run/guard/runtime.stdout", '{"stage":"AWAITING_OPERATOR_ENROLLMENT"}\n')
        self.put("/var/tmp/run/guard/receipt.json", '{"refused":"limits"}')
        code, result = self.act(["host-poll", "--guard-dir", "/var/tmp/run/guard"])
        self.assertEqual(result["stdout"], '{"stage":"AWAITING_OPERATOR_ENROLLMENT"}\n')
        self.assertEqual((result["files"]["receipt.json"], result["files"]["breach.json"]), ('{"refused":"limits"}', None))

    def test_preflight_requires_pins_and_fresh_targets(self):
        self.put("/run/bin/runtime-guard.py", b"guard", 0o500)
        self.put("/run/bin/hfs-controlled-runtime", b"runtime", 0o500)
        self.handoff_directory()
        argv = ["host-preflight", "--guard-script", "/run/bin/runtime-guard.py", "--guard-sha256", sha(b"guard"),
                "--runtime", "/run/bin/hfs-controlled-runtime", "--runtime-sha256", sha(b"runtime"),
                "--guard-dir", "/run/guard", "--owner-dir", "/run/owner", "--enrollment-file", self.ENROLL]
        self.assertEqual(self.act(argv)[0], 0)
        self.put("/run/owner/x", "left over")
        self.assertIn("already exists", self.act(argv)[1]["error"])
        argv[4] = "0" * 64
        self.assertIn("pinned file bytes differ", self.act(argv)[1]["error"])

    def test_proposal_must_be_private(self):
        self.put("/run/owner/proposal.json", b'{"Domains":[]}', 0o644)
        code, result = self.act(["host-proposal", "--path", "/run/owner/proposal.json"])
        self.assertEqual((code, "insecure mode" in result["error"]), (2, True))
        (self.root / "run/owner/proposal.json").chmod(0o600)
        code, result = self.act(["host-proposal", "--path", "/run/owner/proposal.json"])
        self.assertEqual((code, result["sha256"]), (0, sha(b'{"Domains":[]}')))


class GuestTests(RemoteCase):
    def setUp(self):
        super().setUp()
        self.put("/root/stage/rocky_kernel.py", SCRIPT, 0o644)
        for name, tag in (("virtio2", b"hfs0\0"), ("virtio3", b"hfs1\n"), ("virtio1", b"other")):
            self.put(remote.VIRTIO_9P + "/" + name + "/mount_tag", tag, 0o444)
        self.put(remote.VIRTIO_9P + "/uevent", "", 0o200)
        self.put(remote.ADMISSION_V1, json.dumps({"boot_id": BOOT, "provider_nonce": NONCE, "domains": []}), 0o444)

    def admission_argv(self, **changes):
        options = {"release": RELEASE, "boot-id": BOOT, "python": "/usr/bin/python3",
                   "script": "/root/stage/rocky_kernel.py", "script-sha256": sha(SCRIPT), "plan": "/root/stage/p.json",
                   "plan-sha256": "f" * 64, "state": "/root/state", "tags": "hfs0,hfs1", "seconds": "70"}
        options.update(changes)
        argv = ["guest-admission"]
        for name, value in options.items():
            argv += ["--" + name, value]
        return argv

    def admitted(self, manifest=MANIFEST):
        self.put(remote.ADMISSION_PATH, manifest, 0o644)
        result = {"step": "admission", "boot_id": BOOT, "manifest_sha256": sha(MANIFEST), "modules": []}
        return (0, json.dumps(result).encode(), b"")

    def test_admission_measures_manifest_nonce_and_devices(self):
        self.system.outcomes = [self.admitted()]
        code, result = self.act(self.admission_argv())
        self.assertEqual(code, 0, result)
        self.assertEqual((result["manifest_sha256"], result["provider_nonce"], result["devices"]),
                         (sha(MANIFEST), NONCE, {"hfs0": "virtio2", "hfs1": "virtio3"}))
        argv, seconds = self.system.children[0]
        self.assertEqual(argv, ["/usr/bin/python3", "-I", "-B", "/root/stage/rocky_kernel.py", "admission", "--plan",
                                "/root/stage/p.json", "--plan-sha256", "f" * 64, "--state", "/root/state"])
        self.assertEqual(seconds, 70)

    def test_admission_refusals_and_uncertainty(self):
        cases = [(self.admission_argv(**{"script-sha256": "0" * 64}), [], 2, "pinned file bytes differ"),
                 (self.admission_argv(release="5.14.0"), [], 2, "not the test kernel"),
                 (self.admission_argv(**{"boot-id": NONCE}), [], 2, "boot changed"),
                 (self.admission_argv(), [(2, b"", b"refused: udev queue")], 2, "udev queue"),
                 (self.admission_argv(), [(1, b"", b"failed: readback")], 1, "failed after starting"),
                 (self.admission_argv(), [(None, b"", b"")], 1, "left running")]
        for argv, outcomes, code, text in cases:
            with self.subTest(text=text):
                self.system.outcomes = list(outcomes)
                found, result = self.act(argv)
                self.assertEqual(found, code)
                self.assertIn(text, result["error"])

    def test_wrong_manifest_or_missing_device_is_a_known_changed_failure(self):
        self.system.outcomes = [self.admitted(b"different manifest")]
        code, result = self.act(self.admission_argv())
        self.assertEqual((code, result["changed"]), (2, True))
        self.assertIn("manifest readback differs", result["error"])
        (self.root / remote.ADMISSION_PATH.lstrip("/")).unlink()
        self.system.outcomes = [self.admitted()]
        code, result = self.act(self.admission_argv(tags="hfs0,hfs2"))
        self.assertEqual((code, result["changed"]), (2, True))
        self.assertIn("missing 9p device", result["error"])

    def write(self, target, payload, boot=BOOT):
        return self.act(["guest-write", "--target", target, "--sha256", sha(payload), "--size", str(len(payload)),
                         "--boot-id", boot], payload)

    def test_sysfs_write_is_one_complete_write(self):
        sysfs = self.put(remote.SYSFS_TARGETS["provision"], b"", 0o600)
        code, result = self.write("provision", b'{"payload":{}}')
        self.assertEqual((code, result["bytes"]), (0, 14))
        self.assertEqual(sysfs.read_bytes(), b'{"payload":{}}')
        code, result = self.write("bind", b"{}")
        self.assertEqual((code, result["changed"]), (2, False))  # No bind_v1: nothing written.
        code, result = self.write("provision", b"{}", boot=NONCE)
        self.assertEqual((code, "boot changed" in result["error"]), (2, True))
        code, result = self.act(["guest-write", "--target", "provision", "--sha256", "0" * 64, "--size", "2",
                                 "--boot-id", BOOT], b"{}")
        self.assertEqual((code, "payload differs" in result["error"]), (2, True))

    def mount_argv(self, tag="hfs0", mountpoint="/var/tmp/hfs-zq3-zq3-r1/m0"):
        return ["guest-mount", "--tag", tag, "--mountpoint", mountpoint, "--boot-id", BOOT, "--seconds", "25"]

    def test_mount_uses_fixed_options_and_verifies_mountinfo(self):
        (self.root / "var/tmp/hfs-zq3-zq3-r1/m0").mkdir(parents=True, mode=0o700)

        def mounted(argv):
            with open(self.root / "proc/self/mountinfo", "a") as stream:
                stream.write("90 22 0:55 / {} rw - 9p hfs0 rw,trans=virtio\n".format(argv[-1]))
        self.system.on_mount, self.system.outcomes = mounted, [(0, b"", b"")]
        code, result = self.act(self.mount_argv())
        self.assertEqual(code, 0, result)
        self.assertEqual(self.system.children[0][0], [remote.MOUNT, "-t", "9p", "-o", remote.MOUNT_OPTIONS, "hfs0",
                                                      "/var/tmp/hfs-zq3-zq3-r1/m0"])
        code, result = self.act(self.mount_argv())
        self.assertEqual((code, "already mounted" in result["error"]), (2, True))

    def test_mount_failure_and_timeout(self):
        (self.root / "var/tmp/hfs-zq3-zq3-r1/m1").mkdir(parents=True, mode=0o700)
        self.system.outcomes = [(32, b"", b"mount: bad option"), (None, b"", b"")]
        code, result = self.act(self.mount_argv("hfs1", "/var/tmp/hfs-zq3-zq3-r1/m1"))
        self.assertEqual((code, result["changed"]), (2, False))
        code, result = self.act(self.mount_argv("hfs1", "/var/tmp/hfs-zq3-zq3-r1/m1"))
        self.assertEqual((code, result["uncertain"]), (1, True))

    def stat_argv(self, boot=BOOT):
        return ["guest-stat", "--tags", "hfs0,hfs1", "--mountpoints",
                "/var/tmp/hfs-zq3-zq3-r1/m0,/var/tmp/hfs-zq3-zq3-r1/m1", "--boot-id", boot,
                "--python", "/usr/bin/python3", "--seconds", "8"]

    def both_mounted(self):
        with open(self.root / "proc/self/mountinfo", "a") as stream:
            for index in (0, 1):
                stream.write("9{0} 22 0:5{0} / /var/tmp/hfs-zq3-zq3-r1/m{0} rw - 9p hfs{0} rw\n".format(index))

    def test_stat_reports_each_root_and_runs_only_lstat(self):
        self.both_mounted()
        ok = json.dumps({"mode": 0o40755, "uid": 0, "gid": 0, "ino": 1, "dev": 55}).encode()
        denied = json.dumps({"errno": 13, "error": "Permission denied"}).encode()
        self.system.outcomes = [(0, ok, b""), (0, denied, b"")]
        code, result = self.act(self.stat_argv())
        self.assertEqual(code, 0, result)
        self.assertEqual([(r["tag"], r["outcome"]) for r in result["roots"]], [("hfs0", "ok"), ("hfs1", "errno")])
        self.assertEqual((result["roots"][1]["errno"], result["roots"][1]["error"]), (13, "Permission denied"))
        argv, seconds = self.system.children[0]
        self.assertEqual((argv[:4], argv[4], argv[5], seconds),
                         (["/usr/bin/python3", "-I", "-B", "-c"], remote.STAT_PROGRAM, "/var/tmp/hfs-zq3-zq3-r1/m0", 8))
        # The program itself: lstat only, and an OSError becomes its errno.
        for forbidden in ("open(", "mkdir", "write", "unlink", "chmod"):
            self.assertNotIn(forbidden, remote.STAT_PROGRAM)

    def test_stat_program_reports_the_errno_of_a_real_lstat(self):
        missing = str(self.root / "absent")
        done = subprocess.run([sys.executable, "-I", "-B", "-c", remote.STAT_PROGRAM, missing],
                              capture_output=True, timeout=30)
        self.assertEqual((done.returncode, json.loads(done.stdout)["errno"]), (0, 2))
        done = subprocess.run([sys.executable, "-I", "-B", "-c", remote.STAT_PROGRAM, str(self.root)],
                              capture_output=True, timeout=30)
        self.assertTrue(stat.S_ISDIR(json.loads(done.stdout)["mode"]))

    def test_stat_timeout_unmounted_root_and_boot_change(self):
        with open(self.root / "proc/self/mountinfo", "a") as stream:
            stream.write("90 22 0:50 / /var/tmp/hfs-zq3-zq3-r1/m0 rw - 9p hfs0 rw\n")
        self.system.outcomes = [(None, b"", b"")]
        code, result = self.act(self.stat_argv())
        self.assertEqual(code, 0, result)
        self.assertEqual([r["outcome"] for r in result["roots"]], ["timeout", "not-mounted"])
        self.assertEqual(len(self.system.children), 1)  # The unmounted root is never stat'ed.
        code, result = self.act(self.stat_argv(boot=NONCE))
        self.assertEqual((code, "boot changed" in result["error"]), (2, True))

    def test_stat_child_garbage_or_file_root_is_not_ok(self):
        self.both_mounted()
        regular = json.dumps({"mode": 0o100644, "uid": 0, "gid": 0, "ino": 1, "dev": 55}).encode()
        self.system.outcomes = [(1, b"", b"Traceback"), (0, regular, b"")]
        code, result = self.act(self.stat_argv())
        self.assertEqual([r["outcome"] for r in result["roots"]], ["error", "not-directory"])

    def test_ping_reports_release_and_boot(self):
        code, result = self.act(["guest-ping"])
        self.assertEqual((code, result["release"], result["boot_id"]), (0, RELEASE, BOOT))


if __name__ == "__main__":
    unittest.main()
