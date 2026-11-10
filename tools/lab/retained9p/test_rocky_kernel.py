#!/usr/bin/env python3
"""Local tests for rocky_kernel.py (zq3 gap G5).

Nothing here touches Rocky, f3, SSH, grub or a real /boot. The OS boundary is
simulated explicitly by `FakeRocky`: a temporary root directory stands in for
Rocky's filesystem, and grubby, grub2-editenv, grub2-reboot, depmod, dracut,
systemctl and GRUB's own one-shot consumption at boot are emulated in Python.
These tests prove the tool's ordering, verification and refusal logic only,
not real grub/dracut behaviour on Rocky.
"""
import ast
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


def module(name):
    source = Path(__file__).with_name(name + ".py")
    spec = importlib.util.spec_from_file_location(name, source)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


rk = module("rocky_kernel")
REPOSITORY = Path(__file__).resolve().parents[3]
PROBE = REPOSITORY / "internal/controlledruntime/probe/installed.py"
# The admitted xz3 c64+d64 rebuild (manifest 66b5bb2b..., 2026-11-10); its
# rebuilt modules are byte-identical to the w54 bytes of 89c928d5.
REAL_PACKAGE = Path("/home/paul/tmp/claude/hfs-xz3-20261110g/package")
RELEASE = "6.1.190-hfs-xz3"
STOCK = "5.14.0-687.66.1.el9_8.x86_64"
MID = "b26c0ac534534d7fb81b3e0698558bc3"
MODULES = {"drivers/net/virtio_net.ko": "11", "drivers/net/net_failover.ko": "22",
           "net/core/failover.ko": "33", "fs/efivarfs/efivarfs.ko": "44",
           "drivers/thermal/intel/x86_pkg_temp_thermal.ko": "55"}
KERNEL_BUILD_ID = "ab" * 20


# --------------------------------------------------------------------------
# Synthetic ELF files and candidate package


def pad4(raw):
    return raw + b"\0" * (-len(raw) % 4)


def note(name, kind, desc):
    name = name + b"\0"
    return struct.pack("<III", len(name), len(desc), kind) + pad4(name) + pad4(desc)


def notes(build_id_hex):
    """Property, build-id and Linux notes, like the real vmlinux .notes."""
    return (note(b"GNU", 5, b"\0" * 16) + note(b"GNU", 3, bytes.fromhex(build_id_hex)) +
            note(b"Linux", 0x101, b"\0" * 4))


def elf(build_id_hex, payload=b""):
    """Minimal ELF64 LE: header, payload, one SHT_NOTE section, section table."""
    blob = notes(build_id_hex)
    notes_offset = 64 + len(payload)
    shoff = notes_offset + len(blob)
    shoff += -shoff % 8
    header = bytearray(64)
    header[:6] = b"\x7fELF\x02\x01"
    struct.pack_into("<Q", header, 0x28, shoff)
    struct.pack_into("<HH", header, 0x3A, 64, 2)
    section = bytearray(64)
    struct.pack_into("<I", section, 4, 7)
    struct.pack_into("<QQ", section, 0x18, notes_offset, len(blob))
    body = bytes(header) + payload + blob
    return body + b"\0" * (shoff - len(body)) + bytes(64) + bytes(section)


def module_build_id(rel):
    return MODULES[rel] * 20


def package_files():
    files = {"linux/bzImage": b"bzImage carrying vmlinux " + RELEASE.encode(),
             "linux/vmlinux": elf(KERNEL_BUILD_ID, b"vmlinux"),
             "linux/System.map": b"ffffffff81000000 T _text\n",
             "linux/config": b"CONFIG_9P_FS=y\n",
             "linux/kernel.release": (RELEASE + "\n").encode(),
             "linux/modules.order": "".join(rel + "\n" for rel in MODULES).encode(),
             "linux/modules.builtin": b"kernel/fs/9p/9p.ko\n",
             "linux/modules.builtin.modinfo": b"9p.license=GPL\0"}
    for rel in MODULES:
        files["linux/modules/" + rel] = elf(module_build_id(rel), rel.encode())
    return files


KINDS = {"linux/bzImage": "kernel", "linux/vmlinux": "kernel", "linux/config": "config"}


def write_package(directory):
    files = package_files()
    proposal = {"kernel_release": RELEASE,
                "policy": {"physical_contract": rk.PHYSICAL_CONTRACT},
                "artifacts": {k: rk.digest(v) for k, v in files.items()}}
    files["installation-proposal.json"] = json.dumps(proposal).encode()
    entries = {}
    for name, raw in files.items():
        path = directory / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)
        kind = "module" if name.endswith(".ko") else KINDS.get(name, "build-record")
        if name == "installation-proposal.json":
            kind = "proposal"
        entries[name] = {"bytes": len(raw), "kind": kind, "sha256": rk.digest(raw)}
    manifest = json.dumps({"version": "homelabfs-controlled-artifacts/1",
                           "files": entries}).encode()
    (directory / "manifest.json").write_bytes(manifest)
    return rk.digest(manifest)


# --------------------------------------------------------------------------
# Simulated Rocky


def stock_entry(release, title=None):
    return {"kernel": "/boot/vmlinuz-" + release, "args": "ro root=/dev/mapper/rlm_rocky-root",
            "root": "/dev/mapper/rlm_rocky-root",
            "initrd": "/boot/initramfs-{}.img $tuned_initrd".format(release),
            "title": title or "Rocky Linux ({})".format(release), "id": MID + "-" + release}


class FakeRocky(rk.System):
    """Temporary-root Rocky with emulated grub tooling and boots."""

    def __init__(self, root):
        super().__init__(root)
        self.trusted_uid = os.getuid()
        self.current, self.free, self.boots = STOCK, 10 << 30, 0
        self.entries = [stock_entry("7.0.3-1.el9.elrepo.x86_64"),
                        stock_entry("6.1.190-1.el9.elrepo.x86_64"), stock_entry(STOCK),
                        stock_entry("5.14.0-687.65.1.el9_8.x86_64")]
        self.grubenv = {"saved_entry": MID + "-" + STOCK, "boot_success": "1"}
        self.calls, self.faults, self.hooks = [], {}, {}
        for directory in ("boot/loader/entries", "lib/modules", "etc",
                          "proc/sys/kernel/random", "sys/kernel"):
            (self.root / directory).mkdir(parents=True)
        self.boot(STOCK)

    # -- System boundary -------------------------------------------------
    def euid(self):
        return 0

    def platform(self):
        return "linux"

    def release(self):
        return self.current

    def free_bytes(self, absolute):
        return self.free

    def run(self, tool, args):
        self.calls.append((tool, list(args)))
        if tool in self.faults:
            return self.faults[tool]
        handler = {"grubby": self.grubby, "editenv": self.editenv,
                   "reboot-once": self.reboot_once, "depmod": self.depmod,
                   "dracut": self.dracut, "systemctl": self.systemctl,
                   "udevadm": self.udevadm}[tool]
        result = handler(list(args))
        if tool in self.hooks:
            self.hooks[tool](self, list(args))
        return result

    # -- Emulated tools ----------------------------------------------------
    def render(self, entries):
        lines = []
        for index, entry in enumerate(entries):
            lines.append("index={}".format(index))
            for key in ("kernel", "args", "root", "initrd", "title", "id"):
                value = entry[key]
                lines.append('{}="{}"'.format(key, value) if key != "root" else key + "=" + value)
        return "\n".join(lines) + "\n"

    def default(self):
        return next(e for e in self.entries if e["id"] == self.grubenv["saved_entry"])

    def grubby(self, args):
        if args == ["--default-kernel"]:
            return 0, self.default()["kernel"] + "\n", ""
        if args == ["--info=ALL"]:
            return 0, self.render(self.entries), ""
        if args[0].startswith("--add-kernel="):
            return self.grubby_add(args)
        if args[0].startswith("--remove-kernel="):
            kernel = args[0].split("=", 1)[1]
            for entry in [e for e in self.entries if e["kernel"] == kernel]:
                self.entries.remove(entry)
                os.unlink(self.path("/boot/loader/entries/{}.conf".format(entry["id"])))
            return 0, "", ""
        return 1, "", "unsupported grubby call"

    def grubby_add(self, args):
        options = dict(a[2:].split("=", 1) for a in args if "=" in a)
        assert args.count("--copy-default") == 1 and "make-default" not in options
        if not self.path(options["add-kernel"]).exists():
            return 1, "", "kernel image missing"
        release = options["add-kernel"][len("/boot/vmlinuz-"):]
        entry = dict(self.default(), kernel=options["add-kernel"], initrd=options["initrd"],
                     title=options["title"], id=MID + "-" + release)
        self.entries.insert(0, entry)  # Newest first: shifts every old index.
        self.path("/boot/loader/entries/{}.conf".format(entry["id"])).write_text(
            "title {}\nlinux {}\n".format(entry["title"], entry["kernel"]))
        return 0, "", ""

    def editenv(self, args):
        if args == ["-", "list"]:
            return 0, "".join("{}={}\n".format(k, v) for k, v in self.grubenv.items()), ""
        if args == ["-", "unset", "next_entry"]:
            self.grubenv.pop("next_entry", None)
            return 0, "", ""
        return 1, "", "unsupported grub2-editenv call"

    def reboot_once(self, args):
        self.grubenv["next_entry"] = args[0]
        return 0, "", ""

    def depmod(self, args):
        base = self.path("/lib/modules/" + args[0])
        for name in ("modules.dep", "modules.dep.bin", "modules.alias", "modules.softdep"):
            (base / name).write_text("generated\n")
        return 0, "", ""

    def dracut(self, args):
        assert args[:2] == ["--kver", RELEASE] and len(args) == 3
        self.path(args[2]).write_bytes(b"initramfs for " + args[1].encode())
        return 0, "", ""

    def systemctl(self, args):
        assert args == ["reboot"]
        return 0, "", ""

    def udevadm(self, args):
        assert args == rk.UDEV_SETTLE
        return 0, "", ""

    def kdump(self):
        """kdump.service in a test-kernel boot builds its per-kernel initramfs."""
        path = self.path("/boot/initramfs-{}kdump.img".format(self.current))
        path.write_bytes(b"kdump initramfs for " + self.current.encode())
        return path

    # -- Emulated boots ----------------------------------------------------
    def boot(self, release=None, loaded=()):
        """Power on: GRUB consumes next_entry (saving it empty) before booting."""
        self.boots += 1
        if release is None:
            chosen = self.grubenv.get("next_entry") or self.grubenv["saved_entry"]
            if "next_entry" in self.grubenv:
                self.grubenv["next_entry"] = ""
            release = next(e for e in self.entries if e["id"] == chosen)["kernel"][14:]
        self.current = release
        boot_id = "00000000-0000-4000-8000-{:012d}".format(self.boots)
        self.path("/proc/sys/kernel/random/boot_id").write_text(boot_id + "\n")
        self.load(loaded)
        return release

    def load(self, names, kernel_notes=None):
        """Loaded modules; their sysfs notes come from the installed .ko files."""
        self.path("/proc/modules").write_text(
            "".join("{} 16384 0 - Live 0x0\n".format(n) for n in names))
        self.path("/sys/kernel/notes").write_bytes(kernel_notes or notes(KERNEL_BUILD_ID))
        shutil.rmtree(self.path("/sys/module"), ignore_errors=True)
        for name in names:
            rel = next((r for r in MODULES if Path(r).stem == name), None)
            build_id = module_build_id(rel) if rel else "ee" * 20  # Foreign module.
            directory = self.path("/sys/module/{}/notes".format(name))
            directory.mkdir(parents=True)
            (directory / ".note.gnu.build-id").write_bytes(
                note(b"GNU", 3, bytes.fromhex(build_id)))


# --------------------------------------------------------------------------
# Test rig


class Rig(unittest.TestCase):
    LOADED = ("virtio_net", "net_failover", "failover", "efivarfs")

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        package = self.tmp / "package"
        self.manifest_sha256 = write_package(package)
        patcher = mock.patch.object(rk, "ADMITTED", {self.manifest_sha256})
        patcher.start()
        self.addCleanup(patcher.stop)
        raw = rk.build_plan(package, self.manifest_sha256, "zq3-test",
                            "/boot/vmlinuz-" + STOCK, 64 << 20)
        self.plan_path = self.tmp / "plan.json"
        self.plan_path.write_bytes(raw)
        self.plan_sha256, self.plan = rk.digest(raw), json.loads(raw)
        self.stage, self.state = package, self.tmp / "state"
        self.rocky = FakeRocky(self.tmp / "rocky")

    def run_tool(self, command, *extra):
        argv = [command, "--plan", str(self.plan_path), "--plan-sha256", self.plan_sha256,
                "--state", str(self.state)] + list(extra)
        if command == "install" and "--stage" not in extra:
            argv += ["--stage", str(self.stage)]
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = rk.main(argv, system=self.rocky)
        return code, (json.loads(out.getvalue()) if out.getvalue() else None), err.getvalue()

    def ok(self, command, *extra):
        code, result, err = self.run_tool(command, *extra)
        self.assertEqual(code, 0, err)
        return result

    def refused(self, command, text, *extra):
        code, _, err = self.run_tool(command, *extra)
        self.assertEqual(code, 2, err)
        self.assertIn(text, err)
        return err

    def failed(self, command, text, *extra):
        code, _, err = self.run_tool(command, *extra)
        self.assertEqual(code, 1, err)
        self.assertIn(text, err)
        return err

    def journal(self):
        lines = (self.state / "journal.jsonl").read_text().splitlines()
        return [json.loads(line) for line in lines]

    def target(self, name):
        return self.rocky.path(name)

    def installed_entry_id(self):
        return MID + "-" + RELEASE

    def assert_stock_boot_state(self, next_entry=""):
        self.assertEqual(self.rocky.default()["kernel"], "/boot/vmlinuz-" + STOCK)
        self.assertEqual(self.rocky.grubenv["saved_entry"], MID + "-" + STOCK)
        self.assertEqual(self.rocky.grubenv.get("next_entry", ""), next_entry)

    def assert_nothing_installed(self):
        for name in ["/boot/vmlinuz-" + RELEASE, "/boot/initramfs-{}.img".format(RELEASE),
                     "/lib/modules/" + RELEASE, rk.ADMISSION_PATH]:
            self.assertFalse(os.path.lexists(self.target(name)), name)
        self.assertFalse(any(e["kernel"].endswith(RELEASE) for e in self.rocky.entries))

    def grub_mutations(self):
        return [c for c in self.rocky.calls if c[0] == "reboot-once" or
                (c[0] == "editenv" and c[1][1] != "list") or
                (c[0] == "grubby" and c[1][0].startswith(("--add", "--remove", "--set")))]

    def install(self):
        return self.ok("install")

    def select(self):
        self.install()
        return self.ok("select")

    def boot_test_kernel(self, loaded=None):
        """Phase 8 or U3: power on after select; GRUB consumes next_entry."""
        self.select()
        self.assertEqual(self.rocky.boot(loaded=self.LOADED if loaded is None else loaded),
                         RELEASE)


# --------------------------------------------------------------------------
# Plan


class PlanTests(Rig):
    def test_plan_derives_targets_and_build_ids(self):
        self.assertEqual(self.plan["kernel"]["build_id"], KERNEL_BUILD_ID)
        self.assertEqual(sorted(self.plan["modules"]), sorted(Path(r).stem for r in MODULES))
        targets = {e["source"]: e["target"] for e in self.plan["files"]}
        self.assertEqual(targets["linux/bzImage"], "/boot/vmlinuz-" + RELEASE)
        self.assertEqual(targets["linux/modules/net/core/failover.ko"],
                         "/lib/modules/{}/kernel/net/core/failover.ko".format(RELEASE))
        self.assertEqual(self.plan["modules"]["failover"]["build_id"],
                         module_build_id("net/core/failover.ko"))

    def test_plan_cli_writes_exclusive_private_file(self):
        output = self.tmp / "cli-plan.json"
        argv = ["plan", "--package", str(self.stage), "--manifest-sha256",
                self.manifest_sha256, "--run-id", "zq3-test", "--stock-default-kernel",
                "/boot/vmlinuz-" + STOCK, "--output", str(output)]
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(rk.main(argv), 0)
        self.assertEqual(output.stat().st_mode & 0o777, 0o600)
        self.assertEqual(output.read_bytes(), self.plan_path.read_bytes())
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(rk.main(argv), 2)  # Never overwritten.

    def test_plan_refuses_unpinned_or_changed_package(self):
        build = lambda sha: rk.build_plan(self.stage, sha, "zq3-test",
                                          "/boot/vmlinuz-" + STOCK, 0)
        with self.assertRaisesRegex(rk.Refused, "not admitted"):
            build("0" * 64)
        with mock.patch.object(rk, "QUARANTINED", {self.manifest_sha256}):
            with self.assertRaisesRegex(rk.Refused, "not admitted"):
                build(self.manifest_sha256)
        # A superseded candidate refuses with its own reason, even if admitted.
        with mock.patch.object(rk, "SUPERSEDED", {self.manifest_sha256}):
            with self.assertRaisesRegex(rk.Refused, "superseded"):
                build(self.manifest_sha256)
        (self.stage / "linux/modules/net/core/failover.ko").write_bytes(b"tampered")
        with self.assertRaisesRegex(rk.Refused, "does not match manifest"):
            build(self.manifest_sha256)

    def test_plan_refuses_modules_order_mismatch(self):
        manifest = json.loads((self.stage / "manifest.json").read_bytes())
        order = b"net/core/failover.ko\n"
        (self.stage / "linux/modules.order").write_bytes(order)
        manifest["files"]["linux/modules.order"].update(bytes=len(order), sha256=rk.digest(order))
        raw = json.dumps(manifest).encode()
        (self.stage / "manifest.json").write_bytes(raw)
        with mock.patch.object(rk, "ADMITTED", {rk.digest(raw)}):
            with self.assertRaisesRegex(rk.Refused, "modules.order differs"):
                rk.build_plan(self.stage, rk.digest(raw), "zq3-test", "/boot/vmlinuz-" + STOCK, 0)

    def test_load_plan_recomputes_targets_and_closes_keys(self):
        def reload(change):
            plan = json.loads(self.plan_path.read_bytes())
            change(plan)
            raw = json.dumps(plan).encode()
            return rk.load_plan(raw, rk.digest(raw))
        with self.assertRaisesRegex(rk.Refused, "SHA256 mismatch"):
            rk.load_plan(self.plan_path.read_bytes(), "0" * 64)
        with self.assertRaisesRegex(rk.Refused, "derived location"):
            reload(lambda p: p["files"][0].update(target="/etc/passwd"))
        with self.assertRaisesRegex(rk.Refused, "exactly keys"):
            reload(lambda p: p.update(extra=True))
        with self.assertRaisesRegex(rk.Refused, "stock default"):
            reload(lambda p: p.update(stock_default_kernel="/boot/vmlinuz-" + RELEASE))
        with self.assertRaisesRegex(rk.Refused, "unsupported package source"):
            reload(lambda p: p["files"][0].update(source="linux/../../etc/shadow"))
        with self.assertRaisesRegex(rk.Refused, "duplicate JSON key"):
            rk.load_plan(b'{"a":1,"a":2}', rk.digest(b'{"a":1,"a":2}'))

    def test_elf_parser_refuses_ambiguous_or_missing_build_id(self):
        two = note(b"GNU", 3, b"\1" * 20) + note(b"GNU", 3, b"\2" * 20)
        with self.assertRaisesRegex(rk.Refused, "ambiguous"):
            rk.single_build_id(rk.note_build_ids(two))
        with self.assertRaisesRegex(rk.Refused, "ELF64"):
            rk.elf_build_id(b"not an elf" * 10)
        with self.assertRaisesRegex(rk.Refused, "truncated"):
            rk.note_build_ids(b"\0" * 5)

    @unittest.skipUnless((REAL_PACKAGE / "manifest.json").exists(), "candidate package absent")
    def test_real_candidate_package_plan(self):
        """Read-only: the pinned xz3 package yields the build IDs readelf reports."""
        sha = "66b5bb2b6c1dd1a663f26c50d0d9b3bfeebd620127c4a5859ca146074f2c2e2d"
        with mock.patch.object(rk, "ADMITTED", {sha}):
            plan = json.loads(rk.build_plan(REAL_PACKAGE, sha, "zq3-r1",
                                            "/boot/vmlinuz-" + STOCK, 64 << 20))
        self.assertEqual(plan["kernel_release"], RELEASE)
        # The c64 rebuild (selinux in CONFIG_LSM) gave vmlinux a new build ID
        # (964's was a470c7a8, w54's 806cfbb2); the modules are byte-identical
        # to the 964, z54, w54, r54 and f44 packages, so virtio_net keeps its
        # build ID.
        self.assertEqual(plan["kernel"]["build_id"], "2ce818d94ce5990da73dec32a1f60190c24107e7")
        self.assertEqual(plan["modules"]["virtio_net"]["build_id"],
                         "072995182e1a95b83cf3ec794649ce05adf8a59d")
        self.assertEqual(len(plan["modules"]), 5)


# --------------------------------------------------------------------------
# install


class InstallTests(Rig):
    def test_install_copies_verified_bytes_and_adds_unselected_entry(self):
        before = self.rocky.render(self.rocky.entries)
        result = self.install()
        self.assertEqual(result["entry_id"], self.installed_entry_id())
        for entry in self.plan["files"]:
            path = self.target(entry["target"])
            self.assertEqual(rk.digest(path.read_bytes()), entry["sha256"])
            self.assertEqual(path.stat().st_mode & 0o777, 0o644)
        self.assertTrue(self.target("/boot/initramfs-{}.img".format(RELEASE)).exists())
        self.assert_stock_boot_state()
        self.assertNotIn("next_entry", self.rocky.grubenv)
        pre = json.loads((self.state / "pre.json").read_bytes())
        self.assertEqual(pre["grubby_info_all"], before)
        self.assertEqual(self.state.stat().st_mode & 0o777, 0o700)
        steps = [(r["kind"], r["step"], r.get("ok")) for r in self.journal()]
        self.assertEqual(steps, [("intent", "install", None), ("result", "install", True)])
        self.assertEqual([c[0] for c in self.grub_mutations()], ["grubby"])

    def test_install_refuses_before_any_change(self):
        cases = [
            (lambda r: r.grubenv.update(next_entry="x"), "next_entry is already set"),
            (lambda r: r.grubenv.update(saved_entry=MID + "-5.14.0-687.65.1.el9_8.x86_64"),
             "not the pinned stock kernel"),
            (lambda r: setattr(r, "current", RELEASE), "stock kernel"),
            (lambda r: setattr(r, "free", 1 << 20), "lacks"),
            (lambda r: r.path("/boot/vmlinuz-" + RELEASE).write_bytes(b"old"), "already exist"),
            (lambda r: r.entries.append(stock_entry(RELEASE)), "boot entry for the test kernel"),
        ]
        for change, text in cases:
            with self.subTest(text=text):
                self.setUp()
                change(self.rocky)
                self.refused("install", text)
                self.assertFalse(self.state.exists())
                self.assertEqual(self.grub_mutations(), [])

    def test_install_refuses_tampered_stage_without_state(self):
        (self.stage / "linux/modules/fs/efivarfs/efivarfs.ko").write_bytes(b"tampered")
        self.refused("install", "staged file does not match the plan")
        self.assertFalse(self.state.exists())
        self.assert_nothing_installed()

    def test_install_refuses_existing_state_and_wrong_plan(self):
        self.state.mkdir(mode=0o700)
        self.refused("install", "must not exist")
        code, _, err = self.run_tool("select")
        self.assertEqual(code, 2, err)  # No pre.json: nothing to act on.

    def test_dracut_failure_is_recorded_and_u2_cleans_up(self):
        self.rocky.faults["dracut"] = (1, "", "dracut: cannot assemble")
        self.failed("install", "dracut")
        self.assertEqual(self.journal()[-1]["ok"], False)
        self.refused("select", "install has not succeeded")
        self.ok("remove")
        self.assert_nothing_installed()
        self.assert_stock_boot_state()

    def test_grubby_changing_saved_entry_fails_install(self):
        self.rocky.hooks["grubby"] = lambda r, args: (
            args[0].startswith("--add") and
            r.grubenv.update(saved_entry=MID + "-" + RELEASE))
        self.failed("install", "saved_entry is")


# --------------------------------------------------------------------------
# select (Phase 5 step 2) and U1


class SelectTests(Rig):
    def test_select_sets_one_shot_entry_and_keeps_default(self):
        result = self.select()
        self.assertEqual(result["state"]["next_entry"], self.installed_entry_id())
        self.assertIn("stop-rocky", result["next_action"])
        self.assert_stock_boot_state(next_entry=self.installed_entry_id())
        self.refused("select", "already ran")

    def test_reboot_once_failure_runs_u1(self):
        self.install()
        self.rocky.faults["reboot-once"] = (1, "", "grub2-reboot: cannot write grubenv")
        err = self.failed("select", "U1 ran")
        self.assertIn("grub2-reboot", err)
        self.assert_stock_boot_state()
        steps = [(r["step"], r["ok"]) for r in self.journal() if r["kind"] == "result"]
        self.assertEqual(steps[-2:], [("select", False), ("cancel", True)])

    def test_wrong_next_entry_after_select_runs_u1(self):
        self.install()
        self.rocky.hooks["reboot-once"] = lambda r, args: r.grubenv.update(next_entry="other")
        self.failed("select", "U1 ran")
        self.assert_stock_boot_state()

    def test_u1_failure_after_select_failure_is_reported_loudly(self):
        self.install()
        self.rocky.hooks["reboot-once"] = lambda r, args: r.faults.update(
            editenv=(1, "", "grubenv locked"))
        err = self.failed("select", "U1 FAILED")
        self.assertIn("inspect grubenv", err)

    def test_select_refuses_changed_installed_bytes(self):
        self.install()
        self.target("/boot/initramfs-{}.img".format(RELEASE)).write_bytes(b"replaced")
        self.refused("select", "artifact hash changed")
        self.assertNotIn("next_entry", self.rocky.grubenv)

    def test_select_refuses_under_test_kernel_or_set_next_entry(self):
        self.install()
        self.rocky.grubenv["next_entry"] = "something"
        self.refused("select", "next_entry is")
        del self.rocky.grubenv["next_entry"]
        self.rocky.current = RELEASE
        self.refused("select", "stock kernel")

    def test_cancel_unsets_and_verifies(self):
        self.select()
        result = self.ok("cancel")
        self.assertEqual(result["state"]["next_entry"], "")
        self.assert_stock_boot_state()

    def test_cancel_allowed_with_uncertain_step(self):
        self.select()
        self.append_dangling("select")
        self.refused("remove", "uncertain step")
        self.ok("cancel")
        self.assert_stock_boot_state()

    def append_dangling(self, step):
        with (self.state / "journal.jsonl").open("a") as stream:
            stream.write(json.dumps({"seq": 99, "kind": "intent", "step": step,
                                     "boot_id": self.rocky.boot_id()}) + "\n")


# --------------------------------------------------------------------------
# remove (U2), check and U3


class RemoveAndRecoveryTests(Rig):
    def test_remove_restores_exact_pre_run_listing(self):
        before = self.rocky.render(self.rocky.entries)
        self.install()
        result = self.ok("remove")
        self.assertIn("/lib/modules/" + RELEASE, result["removed_dirs"])
        self.assertEqual(self.rocky.render(self.rocky.entries), before)
        self.assert_nothing_installed()
        self.assertEqual(sorted(os.listdir(self.target("/lib/modules"))), [])
        self.refused("remove", "already removed")

    def test_remove_refuses_under_test_kernel_and_with_next_entry(self):
        self.select()
        self.refused("remove", "run cancel (U1) first")
        self.rocky.boot()  # GRUB consumes next_entry, test kernel runs.
        self.refused("remove", "runs only on the stock kernel")
        self.assertTrue(self.target("/boot/vmlinuz-" + RELEASE).exists())

    def test_remove_preserves_everything_on_foreign_content(self):
        self.install()
        foreign = self.target("/lib/modules/{}/kernel/net/core/foreign.ko".format(RELEASE))
        foreign.write_bytes(b"someone else's module")
        self.refused("remove", "unexpected entry, preserved")
        self.assertTrue(self.target("/boot/vmlinuz-" + RELEASE).exists())
        self.assertTrue(any(e["kernel"].endswith(RELEASE) for e in self.rocky.entries))
        link = self.target("/lib/modules/{}/build".format(RELEASE))
        foreign.unlink()
        link.symlink_to("/usr/src")
        self.refused("remove", "unexpected entry, preserved")

    def test_check_reports_unconsumed_next_entry(self):
        self.select()
        code, result, _ = self.run_tool("check", "--expect", "any")
        self.assertEqual(code, 1)
        self.assertIn("next_entry is", result["problems"][0])

    def test_u3_test_kernel_boot_then_clean_reboot_to_stock(self):
        """Abort after the stop: normal start boots the test kernel once."""
        self.boot_test_kernel(loaded=())
        self.assertEqual(self.rocky.grubenv["next_entry"], "")  # Saved empty by GRUB.
        self.refused("reboot-to-stock", "check --expect test")
        self.assertTrue(self.ok("check", "--expect", "test")["ok"])
        self.ok("reboot-to-stock")
        self.assertIn(("systemctl", ["reboot"]), self.rocky.calls)
        self.assertEqual(self.journal()[-1]["step"], "reboot")
        self.assertEqual(self.rocky.boot(), STOCK)
        self.assertTrue(self.ok("check", "--expect", "stock")["ok"])
        self.ok("remove")
        self.assert_nothing_installed()

    def test_reboot_intent_lost_to_shutdown_is_resolved_by_new_boot(self):
        self.boot_test_kernel(loaded=())
        self.ok("check", "--expect", "test")
        self.append_intent("reboot")  # The shutdown won the race.
        self.refused("remove", "runs only on the stock kernel")  # Still the same boot.
        self.rocky.boot()
        self.ok("remove")

    def test_reboot_to_stock_refused_on_stock_or_with_next_entry(self):
        self.install()
        self.refused("reboot-to-stock", "only the test kernel")
        self.rocky.current = RELEASE
        self.ok("check", "--expect", "test")
        self.rocky.grubenv["next_entry"] = self.installed_entry_id()
        self.refused("reboot-to-stock", "next_entry is")
        self.assertNotIn(("systemctl", ["reboot"]), self.rocky.calls)

    def test_reconcile_requires_note_and_unblocks_u2(self):
        self.install()
        self.append_intent("remove")
        self.refused("remove", "uncertain step")
        self.refused("reconcile", "inspection note", "--seq", "99", "--note", " ")
        self.refused("reconcile", "no uncertain intent", "--seq", "7", "--note", "x")
        self.ok("reconcile", "--seq", "99", "--note", "root inspected /boot: untouched")
        self.ok("remove")

    def test_third_kernel_is_neither_stock_nor_test(self):
        """An installed ELRepo kernel must not pass as 'stock'."""
        elrepo = "7.0.3-1.el9.elrepo.x86_64"
        self.rocky.current = elrepo
        self.refused("install", "required " + STOCK)
        self.rocky.current = STOCK
        self.install()
        self.rocky.current = elrepo
        self.refused("select", "required " + STOCK)
        self.refused("remove", "required " + STOCK)
        self.refused("admission", "required " + RELEASE)
        self.refused("reboot-to-stock", "required " + RELEASE)
        self.assertEqual(self.grub_mutations()[1:], [])  # Only install's grubby add.

    def test_u2_removes_kdump_initramfs_with_hash(self):
        self.boot_test_kernel(loaded=())
        kdump = self.rocky.kdump()  # kdump.service in the test-kernel boot.
        expected = rk.digest(kdump.read_bytes())
        self.ok("check", "--expect", "test")
        self.ok("reboot-to-stock")
        self.rocky.boot()
        result = self.ok("remove")
        name = "/boot/initramfs-{}kdump.img".format(RELEASE)
        self.assertEqual(result["removed_files"][name], expected)
        self.assertFalse(kdump.exists())
        stock_kdump = self.rocky.kdump()  # The stock kernel's image is never touched.
        self.assertTrue(stock_kdump.exists())

    def test_install_refuses_existing_kdump_initramfs(self):
        self.rocky.path("/boot/initramfs-{}kdump.img".format(RELEASE)).write_bytes(b"old")
        self.refused("install", "already exist")

    def test_torn_journal_blocks_all_but_cancel_and_status(self):
        self.select()
        with (self.state / "journal.jsonl").open("a") as stream:
            stream.write('{"seq": 7, "kind": "inte')  # Interrupted write.
        self.refused("remove", "torn journal tail")
        self.refused("select", "torn journal tail")
        self.assertTrue(self.ok("status")["torn_tail"])
        # check is read-only diagnostics and still runs (it reports next_entry).
        self.assertEqual(self.run_tool("check", "--expect", "any")[0], 1)
        self.ok("cancel")
        self.assert_stock_boot_state()
        self.refused("remove", "unparseable journal line")  # Root must repair.
        self.ok("cancel")  # Still possible after the fragment became a line.

    def append_intent(self, step):
        with (self.state / "journal.jsonl").open("a") as stream:
            stream.write(json.dumps({"seq": 99, "kind": "intent", "step": step,
                                     "boot_id": self.rocky.boot_id()}) + "\n")


# --------------------------------------------------------------------------
# admission generator


class AdmissionTests(Rig):
    def test_manifest_lists_exactly_the_loaded_modules(self):
        self.boot_test_kernel()
        result = self.ok("admission")
        raw = self.target(rk.ADMISSION_PATH).read_bytes()
        self.assertEqual(result["manifest_sha256"], hashlib.sha256(raw).hexdigest())
        manifest = json.loads(raw)
        self.assertEqual(set(manifest), {"version", "kernel", "modules", "abi"})
        self.assertEqual(sorted(manifest["modules"]), sorted(self.LOADED))
        self.assertNotIn("x86_pkg_temp_thermal", manifest["modules"])
        self.assertEqual(manifest["kernel"], {"path": "/boot/vmlinuz-" + RELEASE,
                                              "sha256": self.plan["kernel"]["sha256"],
                                              "build_id": KERNEL_BUILD_ID})
        self.refused("admission", "never overwritten")

    def test_generated_manifest_passes_the_actual_installed_probe(self):
        """Run probe/installed.py itself against the simulated Rocky root."""
        self.boot_test_kernel()
        self.ok("admission")
        admission = self.target("/sys/kernel/homelabfs/admission_v1")
        admission.parent.mkdir(parents=True)
        admission.write_text(json.dumps({"version": "homelabfs-kernel-admission/1"}))
        output = self.run_probe()
        self.assertEqual(output.returncode, 0, output.stderr)
        observed = json.loads(output.stdout)
        raw = self.target(rk.ADMISSION_PATH).read_bytes()
        self.assertEqual(observed["manifest_hash"], hashlib.sha256(raw).hexdigest())
        self.assertEqual(sorted(observed["modules"]), sorted(self.LOADED))
        self.rocky.load(self.LOADED + ("x86_pkg_temp_thermal",))
        self.assertNotEqual(self.run_probe().returncode, 0)  # Inventory now differs.

    def run_probe(self):
        wrapper = ("import builtins,os,types\nroot={!r}\nreal_open=builtins.open\n"
                   "real_stat=os.stat\n"
                   "def mapped(p):\n return root+str(p) if str(p).startswith('/') else p\n"
                   "def opened(p,*a,**k):\n return real_open(mapped(p),*a,**k)\n"
                   "def observed(p,*a,**k):\n s=real_stat(mapped(p),*a,**k)\n"
                   " return types.SimpleNamespace(st_mode=s.st_mode,st_uid=0,st_size=s.st_size)\n"
                   "builtins.open=opened\nos.stat=observed\nos.geteuid=lambda:0\n"
                   "exec(compile(real_open({!r}).read(),'installed.py','exec'))\n").format(
                       str(self.rocky.root), str(PROBE))
        return subprocess.run([sys.executable, "-I", "-c", wrapper], capture_output=True,
                              text=True, timeout=30, check=False)

    def test_admission_refusals(self):
        cases = [
            (lambda r: r.load(self.LOADED + ("foreign_mod",)), "not installed by the plan"),
            (lambda r: r.load(self.LOADED, kernel_notes=notes("cd" * 20)), "running kernel"),
            (lambda r: (r.path("/sys/module/failover/notes/.note.gnu.build-id")
                        .write_bytes(note(b"GNU", 3, b"\x99" * 20))), "running module failover"),
            (lambda r: r.path("/boot/vmlinuz-" + RELEASE).write_bytes(b"x"), "hash changed"),
            (lambda r: os.chmod(r.path("/lib/modules/{}/kernel/net/core/failover.ko"
                                       .format(RELEASE)), 0o666), "ownership/mode"),
            (lambda r: setattr(r, "current", STOCK), "booted test kernel"),
        ]
        for change, text in cases:
            with self.subTest(text=text):
                self.setUp()
                self.boot_test_kernel()
                change(self.rocky)
                self.refused("admission", text)
                self.assertFalse(os.path.lexists(self.target(rk.ADMISSION_PATH)))

    def test_admission_waits_for_udev_and_refuses_unsettled_queue(self):
        self.boot_test_kernel()
        self.rocky.faults["udevadm"] = (1, "", "timeout waiting for udev queue")
        self.refused("admission", "udev queue did not settle")
        self.assertFalse(os.path.lexists(self.target(rk.ADMISSION_PATH)))
        del self.rocky.faults["udevadm"]
        self.ok("admission")
        self.assertIn(("udevadm", rk.UDEV_SETTLE), self.rocky.calls)

    def test_admission_refuses_unstable_observation(self):
        self.boot_test_kernel()
        ids = iter(["a", "a", "b"])  # settled check, first read, repeated read
        with mock.patch.object(self.rocky, "boot_id", lambda: next(ids)):
            self.refused("admission", "unstable")

    def test_phase_8_to_12_flow_removes_admission_manifest(self):
        self.boot_test_kernel()  # Phase 8: the controlled boot consumes next_entry.
        self.ok("admission")
        self.assertTrue(self.ok("check", "--expect", "test")["ok"])  # Phase 11 step 2.
        self.refused("reboot-to-stock", "admission manifest exists")  # Phase 11 uses poweroff.
        self.assertEqual(self.rocky.boot(), STOCK)  # Phase 12 normal start.
        self.assertTrue(self.ok("check", "--expect", "stock")["ok"])
        self.ok("remove")
        self.assertFalse(os.path.lexists(self.target("/etc/homelabfs")))
        self.assert_nothing_installed()


# --------------------------------------------------------------------------
# Static and boundary checks


class BoundaryTests(unittest.TestCase):
    def test_source_parses_as_python_3_9(self):
        source = Path(rk.__file__).read_text()
        ast.parse(source, feature_version=(3, 9))

    def test_pins_match_controller(self):
        controller = module("controller")
        self.assertEqual(rk.ADMITTED, controller.ADMITTED)
        self.assertEqual(rk.QUARANTINED, controller.QUARANTINED)
        self.assertEqual(rk.SUPERSEDED, controller.SUPERSEDED)

    def test_no_default_changing_grub_call_exists(self):
        source = Path(rk.__file__).read_text()
        for forbidden in ('"--set-default', '"--make-default', "shell=True", "os.system"):
            self.assertNotIn(forbidden, source)

    def test_tool_trust_and_fixed_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            tool = Path(directory) / "tool"
            tool.write_text("#!{}\nimport json,os,sys\nprint(json.dumps([sys.argv[1:], "
                            "os.environ.get('PATH'), os.environ.get('HOME')]))\n"
                            .format(sys.executable))
            tool.chmod(0o755)
            system = rk.System()
            system.trusted_uid = os.getuid()
            with mock.patch.dict(rk.TOOLS, {"grubby": str(tool)}):
                code, out, _ = system.run("grubby", ["--info=ALL", "a b;c"])
                self.assertEqual(code, 0)
                self.assertEqual(json.loads(out), [["--info=ALL", "a b;c"],
                                                   rk.TOOL_ENV["PATH"], None])
                tool.chmod(0o775)
                with self.assertRaisesRegex(rk.Refused, "untrusted tool"):
                    system.run("grubby", [])
                tool.chmod(0o755)
                system.trusted_uid = os.getuid() + 1
                with self.assertRaisesRegex(rk.Refused, "untrusted tool"):
                    system.run("grubby", [])

    def test_parse_info_refuses_garbage(self):
        with self.assertRaises(rk.Failed):
            rk.parse_info('kernel="/boot/x"\n')
        with self.assertRaises(rk.Failed):
            rk.parse_info("index=0\nnonsense\n")


if __name__ == "__main__":
    unittest.main()
