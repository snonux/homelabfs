#!/usr/bin/env python3
"""Offline tests for the f3 fdescfs window plan filler (task v34, gap G12).

Facts and host hashes are substituted data; no host command runs. The happy
path also feeds the filled plan to the actual controller's validate(), with
only its file-digest and interpreter/script-path boundaries substituted.
"""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("fdescfs_window_plan", HERE / "fdescfs-window-plan.py")
FILLER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FILLER)
WINDOW = FILLER.WINDOW
PINS = HERE.parent.parent / "docs/lab/zq3-fdescfs-window/pins.json"
RECORD = HERE.parent.parent / "docs/lab/zq3-fdescfs-window/build-record.json"
# Daemon of the admitted xz3 w54 kernel rebuild (89c928d5), built for f44 from
# defd974 and reused byte for byte by r54 and w54 (only the guest kernel was
# rebuilt each time).
ADMITTED_DAEMON_SHA256 = "174949c1ebf97d483d49e41f1ade7bce88f63013cb943be794276bb1ddd417a7"
CONTROLLER_SHA = "c" * 64
# f3 stock fusefs.ko, docs/lab/results/20261108-zq3-phase1 (task j44).
FUSEFS_SHA256 = "b207adf6383bee8e022fa6115044508b7c64da07e73865e5ea71594d524cc4bd"


def facts():
    """A facts output shaped like the controller's, with the f3 inventory."""
    return {
        "hostname": "f3.lan.buetow.org",
        "base": "15.1-RELEASE-p2\n15.1-RELEASE-p2\n",
        "kernel": "FreeBSD 15.1-RELEASE-p2 releng/15.1-n283596-aadd58dddcbc GENERIC\n",
        "boot": "{ sec = 1, usec = 0 } Sun Nov  8 00:00:00 2026\n",
        "module": {"argv": ["/sbin/kldstat", "-m", "fdescfs"], "exit": 1,
                   "stdout": "", "stderr": "kldstat: can't find module fdescfs\n"},
        "descriptor": {"version": "homelabfs-descriptor-exec/1", "platform": "freebsd/amd64",
                       "mount_type": "devfs", "mount_id": "00000000:00000001",
                       "supported": False},
        "modules": (HERE / "testdata/fdescfs-kldstat-f3-20261105.txt").read_text(),
        "mounts": "zroot/ROOT/default / zfs rw 0 0\ndevfs /dev devfs rw 0 0\n",
        "fd_directory": {"mode": 0o40555, "uid": 0, "gid": 0, "dev": 1, "ino": 2},
    }


def host_hashes(pins):
    values = dict(pins["pinned"])
    values.update({path: "d" * 64 for path in pins["collect"]})
    values[pins["controller"]] = CONTROLLER_SHA
    values["/usr/local/lib/libpython3.12.so.1.0"] = "e" * 64
    return values


class FillerTests(unittest.TestCase):
    def setUp(self):
        self.pins = WINDOW.load(PINS)
        self.facts = facts()
        self.host = host_hashes(self.pins)
        self.now = int(time.time())

    def fill(self, **changes):
        values = {"pins": self.pins, "facts": self.facts, "host": self.host,
                  "controller_sha256": CONTROLLER_SHA, "expires": self.now + 1800,
                  "now": self.now}
        values.update(changes)
        return FILLER.build_plan(values["pins"], values["facts"], values["host"],
                                 values["controller_sha256"], values["expires"], values["now"])

    def refuses(self, pattern, **changes):
        with self.assertRaisesRegex(FILLER.FillRefused, pattern):
            self.fill(**changes)

    def test_committed_pins_match_build_record(self):
        record = json.loads(RECORD.read_text())
        artifacts = record["artifacts"]
        pinned = self.pins["pinned"]
        self.assertTrue(record["reproducible"]["byte_identical"])
        self.assertTrue(record["toolchain_cross_check"]["equal"])
        # The inspector daemon in the record must be the bytes the cross-check
        # rebuilt and the candidate's admitted daemon; a stale artifact entry
        # would otherwise pass because pins.json is compared with this record.
        cross = record["toolchain_cross_check"]
        self.assertEqual(artifacts["hfs-controlled-runtime"]["sha256"], cross["rebuilt_sha256"])
        self.assertEqual(cross["rebuilt_sha256"], cross["candidate_manifest_sha256"])
        self.assertEqual(cross["rebuilt_sha256"], ADMITTED_DAEMON_SHA256)
        self.assertEqual(pinned[self.pins["inspector"]], artifacts["hfs-controlled-runtime"]["sha256"])
        self.assertEqual(pinned[self.pins["namespace_helper"]], artifacts["fdescfs-namespace"]["sha256"])
        self.assertEqual(pinned[self.pins["qualification_test"]], artifacts["controlledruntime.test"]["sha256"])
        for path, value in record["captured_base_closure_sha256"].items():
            self.assertEqual(pinned[path], value)
        # The window artifacts follow the admitted daemon source: rebuilt for
        # f44 from defd974 (d44 MKDIR-targeted lost reply) on 2026-11-08 and
        # reused by the r54 and w54 kernel rebuilds, whose daemon is the same
        # bytes.
        self.assertEqual(record["source_commit"], "defd974af3721614b1a52d24fd949822ef5cd735")

    def test_filled_plan_is_accepted_by_actual_controller_validate(self):
        plan = self.fill()
        self.assertEqual(plan["before"], self.facts)
        self.assertEqual(plan["sha256"]["/usr/local/lib/libpython3.12.so.1.0"], "e" * 64)
        digests = dict(plan["sha256"])
        with patch.object(WINDOW, "digest", side_effect=digests.__getitem__), \
                patch.object(WINDOW, "__file__", self.pins["controller"]), \
                patch.object(WINDOW.sys, "executable", "/usr/local/bin/python3.12"):
            WINDOW.validate(plan, self.pins["inspector"], "setup")
        raw = FILLER.plan_bytes(plan)
        self.assertEqual(json.loads(raw), plan)

    def test_plan_declares_pinned_fusefs_window_module(self):
        plan = self.fill()
        declared = {"path": "/boot/kernel/fusefs.ko", "sha256": FUSEFS_SHA256}
        self.assertEqual(plan["window_module"], declared)
        self.assertEqual(plan["sha256"]["/boot/kernel/fusefs.ko"], FUSEFS_SHA256)

    def test_controller_rejects_plan_with_mismatched_window_module(self):
        plan = self.fill()
        plan["window_module"]["sha256"] = "4" * 64
        with patch.object(WINDOW, "digest", side_effect=dict(plan["sha256"]).__getitem__), \
                patch.object(WINDOW, "__file__", self.pins["controller"]), \
                patch.object(WINDOW.sys, "executable", "/usr/local/bin/python3.12"):
            with self.assertRaisesRegex(RuntimeError, "window module hash"):
                WINDOW.validate(plan, self.pins["inspector"], "restore")

    def test_window_module_pins_refuse(self):
        unpinned = copy.deepcopy(self.pins)
        del unpinned["pinned"]["/boot/kernel/fusefs.ko"]
        self.refuses("window module must be pinned", pins=unpinned)
        mismatched = copy.deepcopy(self.pins)
        mismatched["window_module"]["sha256"] = "5" * 64
        self.refuses("window module must be pinned", pins=mismatched)
        allowed = copy.deepcopy(self.pins)
        allowed["expected"]["modules_forbidden"].remove("fusefs.ko")
        self.refuses("expected absent", pins=allowed)
        own = copy.deepcopy(self.pins)
        own["window_module"]["path"] = "/boot/kernel/fdescfs.ko"
        self.refuses("stock /boot/kernel module", pins=own)
        loose = copy.deepcopy(self.pins)
        loose["window_module"]["name"] = "fusefs"
        self.refuses("closed window module pins", pins=loose)
        drifted = dict(self.host)
        drifted["/boot/kernel/fusefs.ko"] = "6" * 64
        self.refuses("pinned artifact differs", host=drifted)

    def test_controller_rejects_plan_missing_a_collected_pin(self):
        plan = self.fill()
        del plan["sha256"]["/sbin/kldload"]
        with patch.object(WINDOW, "digest", side_effect=dict(plan["sha256"]).__getitem__), \
                patch.object(WINDOW, "__file__", self.pins["controller"]), \
                patch.object(WINDOW.sys, "executable", "/usr/local/bin/python3.12"):
            with self.assertRaisesRegex(RuntimeError, "incomplete artifact pins"):
                WINDOW.validate(plan, self.pins["inspector"], "setup")

    def test_changed_facts_refuse(self):
        cases = {
            "host identity": ("hostname", "f2.lan.buetow.org"),
            "kernel/userland": ("base", "15.1-RELEASE-p3\n15.1-RELEASE-p3\n"),
            "running kernel": ("kernel", "FreeBSD 15.1-RELEASE-p3 releng/15.1-n9 GENERIC\n"),
            "module present": ("module", {"argv": [], "exit": 0, "stdout": "", "stderr": ""}),
            "a /dev/fd mount": ("mounts", "tmpfs /dev/fd/child tmpfs rw 0 0\n"),
            "fdescfs/fuse mount": ("mounts", "/dev/fuse /mnt fusefs rw 0 0\n"),
            "not the controller's": ("extra", "value"),
        }
        for pattern, (key, value) in cases.items():
            with self.subTest(key=key):
                changed = copy.deepcopy(self.facts)
                changed[key] = value
                self.refuses(pattern, facts=changed)

    def test_supported_or_foreign_descriptor_refuses(self):
        for change in ({"supported": True}, {"mount_type": "fdescfs"}, {"platform": "linux/amd64"}):
            with self.subTest(change=change):
                changed = copy.deepcopy(self.facts)
                changed["descriptor"].update(change)
                self.refuses("devfs|identity", facts=changed)

    def test_module_inventory_drift_refuses(self):
        loaded = copy.deepcopy(self.facts)
        loaded["modules"] += (" 99 1 0xffffffff86000000 1000 fdescfs.ko (/boot/kernel/fdescfs.ko)\n"
                              "\tContains modules:\n\t\t Id Name\n\t\t500 fdescfs\n")
        self.refuses("unexpected module", facts=loaded)
        missing = copy.deepcopy(self.facts)
        missing["modules"] = missing["modules"].replace("vmm.ko (/boot/kernel/vmm.ko)",
                                                        "vmx.ko (/boot/kernel/vmx.ko)")
        self.refuses("expected loaded modules", facts=missing)
        malformed = copy.deepcopy(self.facts)
        malformed["modules"] = "garbage\n"
        self.refuses("kldstat", facts=malformed)

    def test_writable_fd_directory_refuses(self):
        changed = copy.deepcopy(self.facts)
        changed["fd_directory"]["mode"] = 0o40777
        self.refuses("protected /dev/fd", facts=changed)

    def test_artifact_and_controller_mismatches_refuse(self):
        rebuilt = dict(self.host)
        rebuilt[self.pins["qualification_test"]] = "0" * 64
        self.refuses("pinned artifact differs", host=rebuilt)
        stock = dict(self.host)
        stock["/boot/kernel/fdescfs.ko"] = "1" * 64
        self.refuses("pinned artifact differs", host=stock)
        missing = dict(self.host)
        del missing["/usr/local/bin/python3.12"]
        self.refuses("collected fact missing", host=missing)
        self.refuses("staged controller differs", controller_sha256="f" * 64)
        extra = dict(self.host)
        extra["/etc/rc.conf"] = "2" * 64
        self.refuses("unexpected host sha256 path", host=extra)

    def test_expiry_bounds(self):
        self.refuses("expiry", expires=self.now)
        self.refuses("expiry", expires=self.now + 3601)
        self.assertEqual(self.fill(expires=self.now + 3600)["expires"], self.now + 3600)

    def test_host_clock_skew_and_expiry(self):
        FILLER.check_host_clock(self.now + 1800, self.now + 60, self.now)
        with self.assertRaisesRegex(FILLER.FillRefused, "skew"):
            FILLER.check_host_clock(self.now + 1800, self.now + 301, self.now)
        # Valid on the workstation clock but already beyond one hour on f3's.
        with self.assertRaisesRegex(FILLER.FillRefused, "expiry"):
            FILLER.check_host_clock(self.now + 3600, self.now - 200, self.now)

    def test_pins_schema_is_closed(self):
        extra = dict(self.pins, surprise=True)
        self.refuses("closed pins schema", pins=extra)
        loose = copy.deepcopy(self.pins)
        loose["collect"].remove("/bin/hostname")
        self.refuses("collect list", pins=loose)
        own = copy.deepcopy(self.pins)
        own["pinned"][own["controller"]] = "3" * 64
        self.refuses("controller hash", pins=own)

    def test_host_sha256_parser(self):
        parsed = FILLER.parse_host_sha256("a" * 64 + " /sbin/mount\n\n")
        self.assertEqual(parsed, {"/sbin/mount": "a" * 64})
        for text in ("a" * 64 + " relative\n", "zz /sbin/mount\n",
                     "a" * 64 + " /sbin/mount\n" + "b" * 64 + " /sbin/mount\n",
                     "a" * 64 + " /two words\n"):
            with self.subTest(text=text), self.assertRaises(FILLER.FillRefused):
                FILLER.parse_host_sha256(text)

    def test_main_writes_exclusive_outputs(self):
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            controller = temp / "controller.py"
            controller.write_bytes(b"controller bytes\n")
            host = host_hashes(self.pins)
            host[self.pins["controller"]] = FILLER.sha256_file(controller)
            (temp / "facts.json").write_text(json.dumps(self.facts))
            (temp / "host.sha256").write_text("".join(f"{v} {k}\n" for k, v in host.items()))
            argv = ["--pins", str(PINS), "--facts", str(temp / "facts.json"),
                    "--host-sha256", str(temp / "host.sha256"),
                    "--expires", str(int(time.time()) + 600), "--out", str(temp / "out"),
                    "--host-now", str(int(time.time()) + 30),
                    "--controller", str(controller)]
            with patch("builtins.print") as printed:
                FILLER.main(argv)
            digest = printed.call_args[0][0]
            record = json.loads((temp / "out/fill-record.json").read_text())
            self.assertEqual(record["plan_sha256"], digest)
            self.assertEqual(FILLER.sha256_file(temp / "out/plan.json"), digest)
            self.assertIn("none", record["authority"])
            with self.assertRaises(FileExistsError):
                FILLER.main(argv)


if __name__ == "__main__":
    unittest.main()
