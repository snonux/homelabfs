#!/usr/bin/env python3
"""Local tests for the zq3 direct bhyve launch tools (gap G4).

Every OS boundary is simulated explicitly: a temporary root stands in for the
f3 filesystem, a command simulator for ifconfig/bhyvectl/ps/vm, and a small
model of bhyve's option parser for the argv shape. Nothing here touches a
real host, VM, tap, nmdm or vmm device. Pinned hashes of files that cannot be
reproduced locally (vm-run, firmware) are patched to the fake files' digests.
"""
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent


def module(name):
    spec = importlib.util.spec_from_file_location("directlaunch_" + name, HERE / (name + ".py"))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


ARGV = module("argv")
HOST = module("host")
CONF = (HERE / "testdata" / "rocky.conf").read_text()
LOG = (HERE / "testdata" / "vm-bhyve.log.tail").read_text()
S0 = "1111aaaa-2222-4333-8444-555555555555"
S1 = "66666666-7777-4888-9999-aaaaaaaaaaaa"
FAKE_VM_RUN = b"#!/bin/sh\n# fake vm-run\n"
FAKE_BOOTROM = b"fake firmware" * 64
VARS_BYTES = bytes(range(256)) * 16
# Golden argv written out literally, independent of the generator code.
GOLDEN = [
    "-s", "0,hostbridge", "-s", "31,lpc",
    "-s", "0:4:0,nvme,/zroot/bhyve/rocky/disk0.img",
    "-s", "0:5:0,virtio-net,tap100,mac=58:9c:fc:4c:38:84",
    "-c", "4", "-m", "6G", "-AHPw",
    "-l", "bootrom,/usr/local/share/uefi-firmware/BHYVE_UEFI.fd,"
          "/var/tmp/hfs-validation-xz3/zq3-r1/launch/uefi-vars.fd",
    "-s", "0:7:0,virtio-9p", "-o", "pci.0.7.0.sharename=hfs0",
    "-o", "pci.0.7.0.hfs-controller=" + S0,
    "-s", "0:8:0,virtio-9p", "-o", "pci.0.8.0.sharename=hfs1",
    "-o", "pci.0.8.0.hfs-controller=" + S1,
    "-U", "e10b6225-44d0-11f1-882a-e8ff1ed7f3d7", "-u",
    "-l", "com1,/dev/nmdm-hfs-zq3-r1.1A", "hfs-zq3-r1"]


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def pinned_constants():
    """Patch only the pins whose real bytes cannot exist in a local test."""
    return mock.patch.multiple(ARGV, VM_RUN_SHA256=sha(FAKE_VM_RUN),
                               BOOTROM_SHA256=sha(FAKE_BOOTROM))


def facts(conf=CONF, log=LOG, interfaces=None):
    files = {ARGV.ROCKY_CONF: {"sha256": sha(conf.encode()), "bytes": len(conf)},
             ARGV.VM_RUN: {"sha256": sha(FAKE_VM_RUN), "bytes": len(FAKE_VM_RUN)},
             ARGV.BOOTROM: {"sha256": sha(FAKE_BOOTROM), "bytes": len(FAKE_BOOTROM)},
             ARGV.VARS: {"sha256": sha(VARS_BYTES), "bytes": len(VARS_BYTES)},
             ARGV.DISK: {"bytes": 1 << 20}}
    return {"version": ARGV.FACTS_VERSION, "files": files, "rocky_conf": conf,
            "log": {"tail": log, "truncated": True},
            "interfaces": interfaces or ["re0", "lo0", "vm-public", "tap0"]}


def record(run_id="zq3-r1", sessions=(S0, S1), tap="tap100"):
    pins = ARGV.check_facts(facts())
    value = ARGV.build_record(run_id, list(sessions), tap, pins, "f" * 64)
    raw = ARGV.canonical(value)
    return raw, value, sha(raw)


def bhyve_config(args):
    """Model of bhyve's -s/-o handling for virtio-9p (pci_emul.c, pci_virtio_9p.c).

    The virtio-9p legacy parser maps `a=b` to sharename=a,path=b and rejects a
    second `=`; `-o` sets a dotted config path; -s on an occupied slot fails.
    """
    nodes, i = {}, 0
    while i < len(args):
        flag, value = args[i], args[i + 1] if i + 1 < len(args) else None
        if flag == "-s":
            slot, _, rest = value.partition(",")
            emul, _, config = rest.partition(",")
            parts = [int(p) for p in slot.split(":")]
            bus, dev, func = ([0] + parts if len(parts) == 2 else parts) if len(parts) > 1 \
                else (0, parts[0], 0)
            node = "pci.%d.%d.%d" % (bus, dev, func)
            if node in nodes:
                raise ValueError("pci slot already occupied")
            nodes[node] = {"device": emul}
            if emul == "virtio-9p" and config:
                share = None
                for token in config.split(","):
                    if "=" in token:
                        if share is not None:
                            raise ValueError("virtio-9p: more than one share name given")
                        share, _, path = token.partition("=")
                        nodes[node].update(sharename=share, path=path)
                    else:
                        nodes[node][token] = True
            i += 2
        elif flag == "-o":
            path, _, setting = value.partition("=")
            node, _, key = path.rpartition(".")
            nodes.setdefault(node, {})[key] = setting
            i += 2
        else:
            i += 1
    return nodes


class ArgvTests(unittest.TestCase):
    def setUp(self):
        patcher = pinned_constants()
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_reconstructs_golden_argv_from_launch_facts(self):
        _, value, _ = record()
        self.assertEqual(value["args"], GOLDEN)
        self.assertEqual(value["args_sha256"], sha(ARGV.canonical(GOLDEN)))
        self.assertEqual(value["launch_logged"], "2026-11-02T22:40:41+02:00")
        self.assertEqual(len(value["changes"]), 7)
        self.assertLessEqual(len(value["args"]), 64)  # Supervisor.pinExecutable bound.

    def test_bhyve_parser_model_accepts_controlled_devices(self):
        nodes = bhyve_config(GOLDEN)
        self.assertEqual(nodes["pci.0.7.0"], {"device": "virtio-9p", "sharename": "hfs0",
                                              "hfs-controller": S0})
        self.assertEqual(nodes["pci.0.5.0"]["device"], "virtio-net")
        self.assertEqual(nodes["pci.0.8.0"]["hfs-controller"], S1)
        self.assertNotIn("path", nodes["pci.0.7.0"])  # path would refuse the controller.
        self.assertNotIn("pci.0.6.0", nodes)  # fbuf dropped

    def test_bhyve_parser_model_refuses_inline_controller_form(self):
        with self.assertRaisesRegex(ValueError, "more than one share name"):
            bhyve_config(["-s", "0:7:0,virtio-9p,sharename=hfs0,hfs-controller=" + S0])
        with self.assertRaisesRegex(ValueError, "already occupied"):
            bhyve_config(["-o", "pci.0.7.0.sharename=hfs0", "-s", "0:7:0,virtio-9p"])

    def test_generate_and_verify_through_cli(self):
        with tempfile.TemporaryDirectory() as temp:
            facts_path, out = Path(temp, "facts.json"), Path(temp, "record.json")
            facts_path.write_text(json.dumps(facts()))
            stdout = io.StringIO()
            with mock.patch.object(sys, "stdout", stdout):
                code = ARGV.main(["generate", "--facts", str(facts_path), "--run-id", "zq3-r1",
                                  "--session", S0, "--session", S1, "--tap", "tap100",
                                  "--output", str(out)])
            self.assertEqual(code, 0)
            result = json.loads(stdout.getvalue())
            self.assertEqual(result["record_sha256"], sha(out.read_bytes()))
            runtime = Path(temp, "runtime.json")
            runtime.write_text(json.dumps({"Exporter": {
                "Path": "/var/tmp/hfs-validation-xz3/zq3-r1/bin/bhyve",
                "SHA256": list(bytes.fromhex(ARGV.EXPORTER_SHA256)), "Args": GOLDEN}}))
            with mock.patch.object(sys, "stdout", io.StringIO()):
                self.assertEqual(ARGV.main(["verify", "--record", str(out), "--record-sha256",
                                            result["record_sha256"], "--runtime-config",
                                            str(runtime)]), 0)
            with self.assertRaises(FileExistsError):  # never overwrites a pinned record
                ARGV.main(["generate", "--facts", str(facts_path), "--run-id", "zq3-r1",
                           "--session", S0, "--session", S1, "--tap", "tap100",
                           "--output", str(out)])

    def test_launch_fact_drift_refuses(self):
        base = LOG.splitlines()
        def log_with(old, new):
            return "\n".join(line.replace(old, new) if "[bhyve" in line else line for line in base)
        cases = {
            "memory": facts(log=log_with("-m 12G", "-m 16G")),
            "msr flag": facts(log=log_with("-AHPw", "-AHP")),
            "mac": facts(log=log_with("mac=58:9c:fc:4c:38:84", "mac=58:9c:fc:00:00:01")),
            "extra device": facts(log=log_with("vga=io]", "vga=io -s 0:9:0,virtio-rnd]")),
            "console": facts(log=log_with("nmdm-rocky.1A", "nmdm-rocky.2A")),
            "later attempt": facts(log=LOG + "\n2026-11-08T10:00:00+02:00: initialising"),
            "iso": facts(log=LOG.replace(
                "starting bhyve (run 1)", "  [bhyve iso device: -s 3:0,ahci-cd,x.iso]")),
            "conf": facts(conf=CONF.replace("memory=12G", "memory=6G")),
            "bridge": facts(interfaces=["re0", "lo0"]),
        }
        for name, value in cases.items():
            with self.subTest(name), self.assertRaises(ARGV.Refused):
                ARGV.check_facts(value)
        for path in (ARGV.VM_RUN, ARGV.BOOTROM):
            changed = facts()
            changed["files"][path]["sha256"] = "0" * 64
            with self.subTest(path), self.assertRaisesRegex(ARGV.Refused, "pinned file drifted"):
                ARGV.check_facts(changed)

    def test_invalid_run_inputs_refuse(self):
        for run_id in ("test0", "zq3-", "ZQ3-r1", "zq3-" + "a" * 21, "zq3-r1/../x"):
            with self.subTest(run_id), self.assertRaises(ARGV.Refused):
                record(run_id=run_id)
        for tap in ("tap0", "tap5", "tap99", "tap1000", "em0", "tap100 "):
            with self.subTest(tap), self.assertRaises(ARGV.Refused):
                record(tap=tap)
        for sessions in ((S0.upper(), S1), (S0, S0), (S0,), (S0, S1, S1), (S0, "not-a-uuid")):
            with self.subTest(sessions), self.assertRaises(ARGV.Refused):
                record(sessions=sessions)

    def test_verify_refuses_any_exporter_drift(self):
        _, value, _ = record()
        good = {"Exporter": {"Path": value["exporter"]["path"],
                             "SHA256": list(bytes.fromhex(ARGV.EXPORTER_SHA256)),
                             "Args": list(GOLDEN), "ControlledBootstrap": True}}
        self.assertTrue(ARGV.verify_runtime(value, good)["verified"])
        mutations = {
            "changed arg": lambda e: e["Args"].__setitem__(9, "12G"),
            "extra arg": lambda e: e["Args"].insert(0, "-D"),
            "missing arg": lambda e: e["Args"].pop(),
            "reordered": lambda e: e["Args"].reverse(),
            "inline controller": lambda e: e.__setitem__("Args", [
                a.replace("0:7:0,virtio-9p", "0:7:0,virtio-9p,sharename=hfs0") for a in GOLDEN]),
            "path": lambda e: e.__setitem__("Path", "/usr/sbin/bhyve"),
            "digest": lambda e: e["SHA256"].__setitem__(0, 0),
            "hex digest": lambda e: e.__setitem__("SHA256", ARGV.EXPORTER_SHA256),
        }
        for name, mutate in mutations.items():
            changed = copy.deepcopy(good)
            mutate(changed["Exporter"])
            with self.subTest(name), self.assertRaises(ARGV.Refused):
                ARGV.verify_runtime(value, changed)

    def test_tampered_or_unapproved_record_refuses(self):
        raw, value, digest = record()
        ARGV.validate_record(raw, value, digest)
        with self.assertRaisesRegex(ARGV.Refused, "approved pin"):
            ARGV.validate_record(raw, value, "0" * 64)
        tampered = copy.deepcopy(value)
        tampered["args"][9] = "12G"
        tampered["args_sha256"] = sha(ARGV.canonical(tampered["args"]))
        raw2 = ARGV.canonical(tampered)
        with self.assertRaisesRegex(ARGV.Refused, "pinned derivation"):
            ARGV.validate_record(raw2, tampered, sha(raw2))
        pretty = json.dumps(value, indent=1).encode()
        with self.assertRaisesRegex(ARGV.Refused, "canonical"):
            ARGV.validate_record(pretty, value, sha(pretty))


class FakeFreeBSD:
    """Command simulator for the f3 boundary; it records every argv."""

    def __init__(self, root):
        self.root = Path(root)
        self.interfaces = ["re0", "lo0", "pflog0", "vm-public", "vm-hfslab", "tap0", "wg0"]
        self.descriptions = {"tap0": "vmnet/rocky/0/public"}
        self.up = {"tap0"}
        self.members = {"vm-public": ["tap0"]}
        self.vms = {"freebsd": "Stopped", "rocky": "Stopped", "hfs-test0": "Stopped",
                    "hfs-test1": "Stopped", "hfs-test2": "Stopped"}
        self.processes = ["1 /sbin/init", "700 /usr/sbin/sshd"]
        self.alive = set()
        self.calls = []
        self.faults = {}

    def __call__(self, argv):
        self.calls.append(list(argv))
        fault = self.faults.get(tuple(argv))
        if fault == "uncertain":
            raise HOST.Uncertain({"argv": argv, "pid": 4242, "failure": "command deadline",
                                  "exit": None})
        if isinstance(fault, int):
            return {"argv": argv, "exit": fault, "stdout": "", "stderr": "simulated failure"}
        tool = os.path.basename(argv[0])
        stdout, code = getattr(self, "_" + tool)(argv[1:])
        return {"argv": argv, "exit": code, "stdout": stdout, "stderr": ""}

    def _vm(self, args):
        assert args == ["list"], args  # no lifecycle command is ever issued
        rows = ["NAME DATASTORE LOADER CPU MEMORY VNC AUTO STATE"]
        rows += ["%s default uefi 4 2G - No %s" % item for item in self.vms.items()]
        return "\n".join(rows) + "\n", 0

    def _ps(self, args):
        if args[0] == "-p":
            pid = int(args[1])
            return ("%d\n" % pid, 0) if pid in self.alive else ("", 1)
        assert args == ["-ax", "-o", "pid=", "-o", "args="], args
        return "\n".join(self.processes) + "\n", 0

    def _bhyvectl(self, args):
        assert args[0] == "--destroy" and args[1].startswith("--vm="), args
        context = self.root / "dev" / "vmm" / args[1][5:]
        if not context.exists():
            return "", 1
        context.unlink()
        return "", 0

    def _ifconfig(self, args):
        if args == ["-l"]:
            return " ".join(self.interfaces) + "\n", 0
        name, rest = args[0], args[1:]
        if rest == ["create"]:
            if name in self.interfaces:
                return "", 1
            self.interfaces.append(name)
            return "", 0
        if name not in self.interfaces:
            return "", 1
        return self._ifconfig_existing(name, rest)

    def _ifconfig_existing(self, name, rest):
        if rest == []:
            flags = "UP,BROADCAST,RUNNING" if name in self.up else "BROADCAST"
            text = "%s: flags=1008843<%s> metric 0 mtu 1500\n" % (name, flags)
            if name in self.descriptions:
                text += "\tdescription: %s\n" % self.descriptions[name]
            for member in self.members.get(name, []):
                text += "\tmember: %s flags=143<LEARNING,DISCOVER>\n" % member
            return text, 0
        if rest[0] == "description":
            self.descriptions[name] = rest[1]
        elif rest[0] == "addm":
            self.members[name].append(rest[1])
        elif rest == ["up"]:
            self.up.add(name)
        elif rest == ["destroy"]:
            self.interfaces.remove(name)
            for members in self.members.values():
                if name in members:
                    members.remove(name)
        else:
            raise AssertionError(rest)
        return "", 0


MUTATING = ("create", "description", "addm", "up", "destroy", "--destroy")


def mutating(calls):
    return [c for c in calls if any(word in c for word in MUTATING)]


class HostTests(unittest.TestCase):
    def setUp(self):
        patcher = pinned_constants()
        patcher.start()
        self.addCleanup(patcher.stop)
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.os = FakeFreeBSD(self.root)
        self.host = HOST.Host(root=self.root, owner=os.getuid(), runner=self.os)
        self.place("/zroot/bhyve/rocky/rocky.conf", CONF.encode())
        self.place("/zroot/bhyve/rocky/vm-bhyve.log", LOG.encode())
        self.place("/zroot/bhyve/rocky/uefi-vars.fd", VARS_BYTES)
        self.place("/zroot/bhyve/rocky/disk0.img", b"\0" * 4096)
        self.place("/usr/local/lib/vm-bhyve/vm-run", FAKE_VM_RUN)
        self.place("/usr/local/share/uefi-firmware/BHYVE_UEFI.fd", FAKE_BOOTROM)
        (self.root / "dev" / "vmm").mkdir(parents=True)
        for name in ("null", "nmdm-rocky.1A", "nmdm-rocky.1B"):
            (self.root / "dev" / name).touch()
        self.raw, self.record, self.digest = record()
        staging = self.root / "var/tmp/hfs-validation-xz3/zq3-r1"
        staging.mkdir(parents=True)
        staging.chmod(0o700)
        self.record_path = "/var/tmp/hfs-validation-xz3/zq3-r1/launch-record.json"
        self.place(self.record_path, self.raw, mode=0o600)
        self.launch = self.root / "var/tmp/hfs-validation-xz3/zq3-r1/launch"

    def place(self, absolute, raw, mode=0o644):
        path = self.root / absolute.lstrip("/")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)
        path.chmod(mode)

    def setup_run(self):
        raw, value = HOST.load_record(self.host, ARGV, self.record_path, self.digest)
        return HOST.setup(self.host, ARGV, raw, value)

    def cleanup_run(self, pid=5151):
        raw, value = HOST.load_record(self.host, ARGV, self.record_path, self.digest)
        return HOST.cleanup(self.host, raw, value, pid)

    def launched(self):
        """Simulate the runtime's bhyve child: vmm context and nmdm node exist."""
        (self.root / "dev/vmm/hfs-zq3-r1").touch()
        (self.root / "dev/nmdm-hfs-zq3-r1.1A").touch()
        self.os.alive.add(5151)
        self.os.processes.append("5151 4: hfs-zq3-r1 (4)")

    def exited(self):
        self.os.alive.discard(5151)
        self.os.processes.remove("5151 4: hfs-zq3-r1 (4)")

    def test_facts_are_read_only_and_never_look_up_cloning_devices(self):
        real_lstat, real_open, real_lexists = os.lstat, os.open, os.path.lexists
        def guarded(function):
            def wrapper(path, *args, **kwargs):
                text = str(path)
                if "/dev/nmdm" in text or "/dev/tap" in text:
                    raise AssertionError("devfs lookup would clone " + text)
                return function(path, *args, **kwargs)
            return wrapper
        with mock.patch.object(HOST.os, "lstat", guarded(real_lstat)), \
             mock.patch.object(HOST.os, "open", guarded(real_open)), \
             mock.patch.object(HOST.os.path, "lexists", guarded(real_lexists)):
            value = HOST.collect_facts(self.host)
            self.setup_run()
        self.assertEqual(mutating(self.os.calls[:4]), [])
        self.assertEqual(value["dev_nmdm"], ["nmdm-rocky.1A", "nmdm-rocky.1B"])
        self.assertEqual(value["bridge_members"], ["tap0"])
        ARGV.check_facts(value)  # host facts feed the generator unchanged

    def test_setup_creates_only_pinned_resources(self):
        owned = self.setup_run()
        self.assertEqual(owned["tap"], "tap100")
        self.assertEqual(mutating(self.os.calls), [
            ["/sbin/ifconfig", "tap100", "create"],
            ["/sbin/ifconfig", "tap100", "description", "homelabfs-hfs-zq3-r1"],
            ["/sbin/ifconfig", "vm-public", "addm", "tap100"],
            ["/sbin/ifconfig", "tap100", "up"]])
        self.assertEqual((self.launch / "uefi-vars.fd").read_bytes(), VARS_BYTES)
        self.assertEqual(oct((self.launch / "uefi-vars.fd").stat().st_mode & 0o777), "0o600")
        self.assertEqual(oct(self.launch.stat().st_mode & 0o777), "0o700")
        self.assertEqual((self.launch / "record.json").read_bytes(), self.raw)
        self.assertEqual(HOST.uncertain_steps(self.host, str(self.launch)[len(str(self.root)):]), [])

    def assert_refused_without_effects(self, pattern):
        with self.assertRaisesRegex(HOST.Refused, pattern):
            self.setup_run()
        self.assertEqual(mutating(self.os.calls), [])
        self.assertFalse(self.launch.exists())

    def test_setup_refusals_dispatch_nothing(self):
        cases = [
            ("rocky running", lambda: self.os.vms.update(rocky="Running (80203)"), "Stopped"),
            ("vmm present", lambda: (self.root / "dev/vmm/rocky").touch(), "vmm contexts"),
            ("tap exists", lambda: self.os.interfaces.append("tap100"), "tap already"),
            ("nmdm exists", lambda: (self.root / "dev/nmdm-hfs-zq3-r1.1B").touch(), "nmdm"),
            ("bridge gone", lambda: self.os.interfaces.remove("vm-public"), "bridge"),
            ("vars changed", lambda: self.place("/zroot/bhyve/rocky/uefi-vars.fd", b"x" * 9),
             "vars changed"),
            ("conf drift", lambda: self.place("/zroot/bhyve/rocky/rocky.conf",
                                              CONF.replace("cpu=4", "cpu=8").encode()), "drifted"),
            ("log drift", lambda: self.place("/zroot/bhyve/rocky/vm-bhyve.log",
                                             LOG.replace("-m 12G", "-m 8G").encode()), "drifted"),
            ("process", lambda: self.os.processes.append("9 4: hfs-zq3-r1"), "names the run"),
            ("writable conf", lambda: (self.root / "zroot/bhyve/rocky/rocky.conf").chmod(0o666),
             "protected"),
        ]
        for name, arrange, pattern in cases:
            with self.subTest(name):
                self.setUp()
                arrange()
                self.assert_refused_without_effects(pattern)

    def test_setup_refuses_unapproved_record_and_replay(self):
        with self.assertRaisesRegex(HOST.Refused, "approved pin"):
            HOST.load_record(self.host, ARGV, self.record_path, "0" * 64)
        self.setup_run()
        calls = len(self.os.calls)
        with self.assertRaisesRegex(HOST.Refused, "no replay"):
            self.setup_run()
        self.assertEqual(len(self.os.calls), calls)

    def test_uncertain_tap_create_blocks_every_later_step(self):
        self.os.faults[("/sbin/ifconfig", "tap100", "create")] = "uncertain"
        with self.assertRaises(HOST.Uncertain):
            self.setup_run()
        self.assertTrue((self.launch / "tap-create.uncertain.json").exists())
        with self.assertRaisesRegex(HOST.Refused, "reconciliation"):
            self.cleanup_run()
        self.assertEqual([c for c in mutating(self.os.calls) if "destroy" in c], [])

    def test_cleanup_waits_for_child_and_never_signals(self):
        self.setup_run()
        self.launched()
        with mock.patch.object(os, "kill", side_effect=AssertionError("kill")), \
             mock.patch.object(os, "killpg", side_effect=AssertionError("killpg")):
            with self.assertRaisesRegex(HOST.Refused, "still exists; never killed"):
                self.cleanup_run()
            self.os.alive.discard(5151)  # PID gone, but a process still names the VM
            with self.assertRaisesRegex(HOST.Refused, "still names hfs-zq3-r1"):
                self.cleanup_run()
        self.assertEqual([c for c in self.os.calls if "destroy" in c or "--destroy" in c], [])
        self.assertFalse((self.launch / "cleanup-summary.json").exists())
        self.exited()
        self.assertEqual(self.cleanup_run()["vmm"], "destroyed")  # resumes after refusal

    def test_cleanup_destroys_only_own_vmm_and_tap(self):
        self.setup_run()
        self.launched()
        self.exited()
        (self.root / "dev/vmm/hfs-test0").touch()  # foreign context must survive
        summary = self.cleanup_run()
        self.assertEqual((summary["vmm"], summary["tap"]), ("destroyed", "destroyed"))
        self.assertEqual(sorted(os.listdir(self.root / "dev/vmm")), ["hfs-test0"])
        self.assertIn("tap0", self.os.interfaces)
        self.assertNotIn("tap100", self.os.interfaces)
        self.assertEqual(summary["nmdm_present"], ["nmdm-hfs-zq3-r1.1A"])
        self.assertTrue(summary["vars"]["original_unchanged"])
        self.assertEqual(summary["vars"]["copy_preserved"]["sha256"], sha(VARS_BYTES))
        destroys = [c for c in self.os.calls if "destroy" in c or "--destroy" in c]
        self.assertEqual(destroys, [["/usr/sbin/bhyvectl", "--destroy", "--vm=hfs-zq3-r1"],
                                    ["/sbin/ifconfig", "tap100", "destroy"]])
        with self.assertRaisesRegex(HOST.Refused, "already completed"):
            self.cleanup_run()

    def test_cleanup_refuses_guest_name_collision(self):
        self.setup_run()
        self.launched()
        self.exited()
        self.os.vms["hfs-zq3-r1"] = "Stopped"
        with self.assertRaisesRegex(HOST.Refused, "vm-bhyve guest"):
            self.cleanup_run()
        self.assertTrue((self.root / "dev/vmm/hfs-zq3-r1").exists())
        self.assertIn("tap100", self.os.interfaces)

    def test_cleanup_refuses_foreign_tap_label(self):
        self.setup_run()
        self.launched()
        self.exited()
        self.os.descriptions["tap100"] = "vmnet/other/0/public"
        with self.assertRaisesRegex(HOST.Refused, "foreign description"):
            self.cleanup_run()
        self.assertIn("tap100", self.os.interfaces)
        self.assertFalse((self.launch / "cleanup-summary.json").exists())

    def test_uncertain_or_failed_destroy_is_never_replayed(self):
        for fault in ("uncertain", 1):
            with self.subTest(fault):
                self.setUp()
                self.setup_run()
                self.launched()
                self.exited()
                self.os.faults[("/usr/sbin/bhyvectl", "--destroy", "--vm=hfs-zq3-r1")] = fault
                with self.assertRaises((HOST.Uncertain, HOST.Refused)):
                    self.cleanup_run()
                with self.assertRaisesRegex(HOST.Refused, "reconciliation"):
                    self.cleanup_run()
                self.assertEqual(sum("--destroy" in c for c in self.os.calls), 1)
                self.assertTrue((self.root / "dev/vmm/hfs-zq3-r1").exists())

    def test_no_child_cleanup_after_abort_before_launch(self):
        with self.assertRaisesRegex(HOST.Refused, "nothing to clean"):
            self.cleanup_run(pid=None)
        self.setup_run()
        (self.root / "dev/vmm/hfs-zq3-r1").touch()  # a child did start after all
        with self.assertRaisesRegex(HOST.Refused, "give its exact PID"):
            self.cleanup_run(pid=None)
        (self.root / "dev/vmm/hfs-zq3-r1").unlink()
        summary = self.cleanup_run(pid=None)
        self.assertEqual((summary["vmm"], summary["tap"]), ("absent", "destroyed"))

    def test_cli_requires_explicit_child_evidence_and_freebsd_root(self):
        with mock.patch.object(sys, "stderr", io.StringIO()), self.assertRaises(SystemExit):
            HOST.parse_args(["cleanup", "--record", "/r", "--record-sha256", "0" * 64])
        with mock.patch.object(sys, "stderr", io.StringIO()), self.assertRaises(SystemExit):
            HOST.parse_args(["cleanup", "--record", "/r", "--record-sha256", "0" * 64,
                             "--child-pid", "5", "--no-child"])
        with mock.patch.object(sys, "stderr", io.StringIO()):
            self.assertEqual(HOST.main(["facts"]), 2)  # not FreeBSD root here

    def test_cleanup_reports_changed_original_vars(self):
        self.setup_run()
        self.place("/zroot/bhyve/rocky/uefi-vars.fd", VARS_BYTES[::-1])
        with self.assertRaisesRegex(HOST.Refused, "original UEFI vars changed"):
            self.cleanup_run()
        summary = json.loads((self.launch / "cleanup-summary.json").read_text())
        self.assertFalse(summary["vars"]["original_unchanged"])


class BoundedCommandTests(unittest.TestCase):
    def test_deadline_records_uncertain_and_leaves_child_alone(self):
        with mock.patch.object(HOST, "COMMAND_SECONDS", 0.2):
            with self.assertRaises(HOST.Uncertain) as caught:
                HOST.bounded_command(["/bin/sleep", "2"])
        child = HOST.RETAINED.pop()
        self.assertEqual(child.pid, caught.exception.evidence["pid"])
        self.assertIsNone(child.poll())  # still running: the tool never signalled it
        child.wait()  # the test reaps its own child

    def test_success_captures_bounded_output(self):
        result = HOST.bounded_command(["/bin/echo", "ok"])
        self.assertEqual((result["exit"], result["stdout"]), (0, "ok\n"))


if __name__ == "__main__":
    unittest.main()
