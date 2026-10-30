#!/usr/bin/env python3
"""Offline safety checks. No SSH, workload binary, mount or ZFS execution."""

import copy
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shlex
import struct
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import uuid
import xml.etree.ElementTree as ET

sys.dont_write_bytecode = True
SOURCE = Path(__file__).with_name("native-single-node.py")
IMPORT = importlib.util.spec_from_file_location("native_single_node", SOURCE)
native = importlib.util.module_from_spec(IMPORT)
IMPORT.loader.exec_module(native)
# Independent, already-reviewed report contract. Never generate mock case names
# from the validator under test: that previously hid two missing lifecycle cases.
CI_SOURCE = SOURCE.parents[1] / "ci/validate-linux-behavior.py"
CI_IMPORT = importlib.util.spec_from_file_location("linux_behavior_contract", CI_SOURCE)
ci_contract = importlib.util.module_from_spec(CI_IMPORT)
CI_IMPORT.loader.exec_module(ci_contract)
EXPECTED_CASES = set(ci_contract.CASES)


def reports(spec, role, profile, volume, node):
    key, work = role + "-" + profile, spec["run_root"]
    generation, session = str(uuid.uuid4()), str(uuid.uuid4())
    uid, groups = (0, []) if role == "root" else (20001, [20002])
    ready = {"volume_id": volume, "generation": generation, "session": session,
             "role": role, "profile": profile, "daemon_uid": 0,
             "client_uid": uid, "client_gid": uid, "client_groups": groups,
             "normal_publication": False, "mountpoint": work + "/m",
             "data_root": work + "/d", "journal_root": work + "/j", "control_root": work + "/c",
             "behavior_sha256": spec["artifacts"][1]["sha256"]}
    fixture = {field: str(ready[field]) for field in ("role", "daemon_uid", "client_uid", "client_gid", "data_root", "journal_root", "control_root", "behavior_sha256")}
    fixture["client_groups"] = "[]" if role == "root" else "[20002]"
    fixture.update({"source": work + "/j/volume/source", "reservations": work + "/d/volume/reservations",
                    "workspace_data": work + f"/d/volume/workspaces/{generation}/data",
                    "workspace_journal": work + f"/j/volume/workspaces/{generation}/journal",
                    "workspace_progress": work + f"/c/workspaces/{generation}/progress"})
    skips = {"concurrent-append"} if profile == "hfs-buffered" else {"mmap-msync-readback"}
    if (role, profile) != ("root", "hfs-buffered"):
        skips.add("multiuser-groups-sticky-permission-cache")
    identity = f"homelabfs-validation-v1\n{volume}\n{generation}\n{session}\n"
    results = [{"name": name, "family": ci_contract.CASES[name], "seconds": 0.001,
                "status": "skip" if name in skips else "pass", "detail": "explicit profile restriction" if name in skips else ""}
               for name in sorted(EXPECTED_CASES)]
    report = {"expected_identity": identity, "profile": profile, "target": work + "/m",
              "platform": "freebsd/amd64", "fixture": fixture, "results": results,
              "started": "2026-10-29T00:00:00Z",
              "scope": "single-node local syscall semantics; no power-loss, replication, failover, arbitrary-client or native FreeBSD acceptance claim",
              "run_directory": work + "/m/hfs-behavior-synthetic",
              "declared_capabilities": {"direct_io": profile == "hfs-direct", "mmap": profile == "hfs-buffered",
                                        "default_permissions": True, "local_locks": True, "file_sync": True,
                                        "directory_sync": True, "normal_publication": False,
                                        "arbitrary_credentials": False, "writeback": False}}
    marker = {"format_version": 1, "node_id": node, "volume_id": volume,
              "data_root": work + "/d", "journal_root": work + "/j", "control_root": work + "/c"}
    suite = ET.Element("testsuite", name="homelabfs.behavior." + profile,
                       tests=str(len(results)), failures="0", skipped=str(len(skips)))
    props = ET.SubElement(suite, "properties")
    for name, value in (("fixture", json.dumps(fixture)), ("target", report["target"]),
                        ("profile", profile), ("platform", report["platform"]),
                        ("scope", report["scope"]), ("run_directory", report["run_directory"]),
                        ("expected_identity", identity), ("declared_capabilities", json.dumps(report["declared_capabilities"]))):
        ET.SubElement(props, "property", name=name, value=value)
    for result in results:
        case = ET.SubElement(suite, "testcase", name=result["name"],
                             classname=result["family"], time=str(result["seconds"]))
        if result["status"] == "skip":
            ET.SubElement(case, "skipped").text = result["detail"]
    files = {key + ".stdout": json.dumps(ready).encode(), key + ".json": json.dumps(report).encode(),
             key + ".xml": ET.tostring(suite), key + ".exit.json": json.dumps({"exit_code": 0, "timed_out": False, "preserved_live_process": False}).encode()}
    files.update({key + f".marker-{kind}.json": json.dumps(marker).encode()
                  for kind in ("data", "journal", "control")})
    return files


class FakeExecutor:
    def __init__(self, spec):
        self.spec, self.calls, self.files = spec, [], [{}, {}, {}]
        self.volumes = [str(uuid.uuid4()) for _ in range(3)]
        self.nodes = [str(uuid.uuid4()) for _ in range(3)]
        self.fail = None

    def __call__(self, argv, **kwargs):
        self.calls.append(argv)
        if argv[0] == "/usr/bin/scp" or argv[-2] == "paul@f3":
            return subprocess.CompletedProcess(argv, 0, b"", b"")
        command = shlex.split(argv[-1])
        action, index = command[3], int(command[5])
        if action == "run":
            role, profile, initialize = command[-3:]
            self.files[index].update(reports(self.spec, role, profile, self.volumes[index], self.nodes[index]))
            if self.fail:
                return subprocess.CompletedProcess(argv, self.fail, b"", b"injected")
        if action == "collect":
            stream = io.BytesIO()
            with tarfile.open(fileobj=stream, mode="w") as archive:
                for name, data in self.files[index].items():
                    item = tarfile.TarInfo("evidence/" + name)
                    item.size = len(data)
                    archive.addfile(item, io.BytesIO(data))
            return subprocess.CompletedProcess(argv, 0, stream.getvalue(), b"")
        return subprocess.CompletedProcess(argv, 0, b"fixed guest evidence\n", b"")


class NativeDriverTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="xq3-offline-test-")
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        binaries = []
        for name in ("homelabfs", "hfs-behavior"):
            binary = self.base / name
            header = bytearray(64)
            header[:9] = b"\x7fELF\x02\x01\x01\x09\x00"
            struct.pack_into("<H", header, 16, 2)
            struct.pack_into("<H", header, 18, 62)
            binary.write_bytes(header)
            binaries.append(binary)
        self.spec = native.specification("xq3-fake", *binaries, inspect=False)
        self.host_keys = self.base / "host-known_hosts"
        key = native.KEYS.read_text().splitlines()[0].split()[1:3]
        self.host_keys.write_text("f3 " + " ".join(key) + "\n")
        self.guest_keys = self.base / "guest-known_hosts"
        self.guest_keys.write_bytes(native.KEYS.read_bytes())
        self.policy = self.base / "policy.json"
        self.policy.write_text(json.dumps({"schema": 1, "host_address": "192.0.2.133",
                                           "host_keys": "host-pins", "guest_keys": "guest-pins"}))
        self.outer = {"steps": [{"contract": "prepared-lab-preflight-v1", "action": "driver",
                                  "effects": "read-only", "args": ["--policy-artifact", "policy"]}],
                      "artifacts": [{"id": name, "path": str(path), "sha256": native.digest(path)}
                                    for name, path in (("policy", self.policy), ("host-pins", self.host_keys),
                                                       ("guest-pins", self.guest_keys))]}
        self.outer_path = self.base / "transport-plan.json"
        self.outer_path.write_text(json.dumps(self.outer))
        self.env = patch.dict(os.environ, {"HFS_LAB_PLAN_HASH": "a" * 64,
                                           "HFS_LAB_PLAN": str(self.outer_path)})
        self.env.start()
        self.addCleanup(self.env.stop)

    def driver(self, phase, executor):
        driver = native.Driver(self.spec, phase, executor)
        driver.evidence = self.base / "evidence"
        return driver

    def local_child(self, source, *args):
        script = self.base / "local-child.py"
        script.write_text(source)
        return [sys.executable, str(script), *args]

    def test_generated_transport_uses_reviewed_context_for_both_hops_and_copy(self):
        fake = FakeExecutor(self.spec)
        driver = self.driver("setup", fake)
        driver.run()
        common = ["BatchMode=yes", "StrictHostKeyChecking=yes", "GlobalKnownHostsFile=/dev/null",
                  "UpdateHostKeys=no", "ControlMaster=no", "ControlPath=none", "ConnectTimeout=10",
                  "ServerAliveInterval=5", "ServerAliveCountMax=2"]
        for call in fake.calls:
            with self.subTest(argv=call):
                self.assertEqual(call[1:3], ["-F", "/dev/null"])
                self.assertNotIn("-J", call)
                for option in common:
                    self.assertIn(option, call)
                if call[-2] == "paul@f3":
                    self.assertIn("HostName=192.0.2.133", call)
                    self.assertIn("HostKeyAlias=f3", call)
                    self.assertIn("UserKnownHostsFile=" + str(self.host_keys), call)
                else:
                    self.assertIn("UserKnownHostsFile=" + str(self.guest_keys), call)
                    proxy = shlex.split(next(arg.split("=", 1)[1] for arg in call if arg.startswith("ProxyCommand=")))
                    self.assertEqual(proxy, ["/usr/bin/ssh", *driver.host_opts, "-W", "%h:%p", "paul@f3"])
        # Have the real OpenSSH parser verify the generated options. -G exits
        # after printing configuration, before any connection/proxy execution.
        for opts, destination, expected_host, keys in (
                (driver.host_opts, "paul@f3", "192.0.2.133", self.host_keys),
                (driver.opts, "root@172.31.253.11", "172.31.253.11", self.guest_keys)):
            result = subprocess.run(["/usr/bin/ssh", "-G", *opts, destination],
                                    capture_output=True, check=True, text=True, timeout=5)
            config = dict(line.split(" ", 1) for line in result.stdout.splitlines())
            self.assertEqual(config["hostname"], expected_host)
            self.assertEqual(config["userknownhostsfile"], str(keys))
            self.assertEqual(config["globalknownhostsfile"], "/dev/null")
            self.assertEqual(config["stricthostkeychecking"], "true")
            self.assertEqual(config["controlmaster"], "false")
            self.assertNotIn("controlpath", config)  # OpenSSH omits disabled paths.
            self.assertEqual(config["updatehostkeys"], "false")

    def test_context_requires_exact_policy_and_unmodified_artifacts_before_contact(self):
        baseline = copy.deepcopy(self.outer)
        symlink = self.base / "linked-host-pins"
        symlink.symlink_to(self.host_keys)
        for mutation in ("contract", "missing-policy", "duplicate-pin", "policy-bytes", "host-key-bytes",
                         "token-path", "quoted-path", "symlink"):
            with self.subTest(mutation=mutation):
                changed = copy.deepcopy(baseline)
                if mutation == "contract":
                    changed["steps"][0]["contract"] = "another-policy"
                elif mutation == "missing-policy":
                    changed["artifacts"].pop(0)
                elif mutation == "duplicate-pin":
                    changed["artifacts"].append(copy.deepcopy(changed["artifacts"][1]))
                elif mutation == "policy-bytes":
                    changed["artifacts"][0]["sha256"] = "0" * 64
                elif mutation in ("token-path", "quoted-path", "symlink"):
                    changed["artifacts"][1]["path"] = str({
                        "token-path": self.base / "host-%h-pins",
                        "quoted-path": self.base / 'host-"pins', "symlink": symlink}[mutation])
                else:
                    changed["artifacts"][1]["sha256"] = "0" * 64
                self.outer_path.write_text(json.dumps(changed))
                fake = FakeExecutor(self.spec)
                with self.assertRaises(native.Refusal):
                    self.driver("collect", fake)
                self.assertEqual(fake.calls, [])

    def test_repinned_invalid_host_and_changed_guest_key_are_refused(self):
        original = {path: path.read_bytes() for path in (self.host_keys, self.guest_keys, self.policy)}
        for mutation in ("no-f3", "invalid-key", "duplicate-f3", "changed-guest", "bad-address"):
            with self.subTest(mutation=mutation):
                for path, content in original.items():
                    path.write_bytes(content)
                if mutation == "no-f3":
                    self.host_keys.write_text(self.host_keys.read_text().replace("f3 ", "another-host "))
                elif mutation == "invalid-key":
                    self.host_keys.write_text("f3 ssh-ed25519 invalid-key!\n")
                elif mutation == "duplicate-f3":
                    self.host_keys.write_bytes(original[self.host_keys] * 2)
                elif mutation == "changed-guest":
                    rows = self.guest_keys.read_text().splitlines()
                    rows[0] = rows[0].split()[0] + " " + " ".join(rows[1].split()[1:])
                    self.guest_keys.write_text("\n".join(rows) + "\n")
                else:
                    policy = json.loads(self.policy.read_text())
                    policy["host_address"] = "implicit-host-name"
                    self.policy.write_text(json.dumps(policy))
                changed = copy.deepcopy(self.outer)
                for item in changed["artifacts"]:
                    item["sha256"] = native.digest(item["path"])
                self.outer_path.write_text(json.dumps(changed))
                fake = FakeExecutor(self.spec)
                with self.assertRaises((native.Refusal, ValueError)):
                    self.driver("collect", fake)
                self.assertEqual(fake.calls, [])

    def test_context_change_between_commands_is_uncertain_without_dispatch(self):
        for path in (self.host_keys, self.guest_keys, self.policy):
            with self.subTest(path=path):
                content = path.read_bytes()
                fake = FakeExecutor(self.spec)
                driver = self.driver("collect", fake)
                path.write_bytes(content + b"\n")
                with self.assertRaisesRegex(native.Uncertain, "SSH context artifact changed"):
                    driver.collect(0)
                self.assertEqual(fake.calls, [])
                path.write_bytes(content)

    def test_uncertain_collection_and_workload_timeout_dispatch_nothing_further(self):
        for phase in ("test", "collect"):
            with self.subTest(phase=phase):
                fake = FakeExecutor(self.spec)

                def execute(argv, **kwargs):
                    if argv[-2].startswith("root@"):
                        fake.calls.append(argv)
                        if phase == "test":
                            raise subprocess.TimeoutExpired(argv, kwargs["timeout"],
                                                            b"partial stdout", b"partial stderr")
                        return subprocess.CompletedProcess(argv, 255, b"", b"Connection timed out during banner exchange\r\n")
                    return fake(argv, **kwargs)

                driver = self.driver(phase, execute)
                with self.assertRaises(native.Uncertain) as caught:
                    driver.run()
                self.assertEqual(len(fake.calls), 2 if phase == "test" else 1)
                self.assertFalse(driver.evidence.exists())
                self.assertIn("timeout" if phase == "test" else "banner exchange", str(caught.exception))

    def test_real_command_success_keeps_binary_stdout_and_logs_only_stderr(self):
        argv = self.local_child(
            "import sys\n"
            "sys.stdout.buffer.write(sys.stdin.buffer.read())\n"
            "sys.stderr.buffer.write(b'W' * 262144 + b'local warning\\n')\n", "success-context")
        payload = b"DO-NOT-LOG-BINARY-OR-STDIN\x00\xff" + bytes(range(256)) * 1024
        driver = self.driver("collect", None)
        stderr, stdout = io.StringIO(), io.StringIO()
        with patch.dict(os.environ, PRIVATE_TEST_VALUE="DO-NOT-LOG-ENVIRONMENT"), \
                contextlib.redirect_stderr(stderr), contextlib.redirect_stdout(stdout):
            result = driver.command(argv, payload, seconds=5)
        self.assertEqual(result, payload)
        self.assertEqual(stdout.getvalue(), "")
        detail = json.loads(stderr.getvalue().split(": ", 1)[1])
        self.assertEqual(detail["returncode"], 0)
        self.assertIn("success-context", detail["argv"])
        self.assertTrue(detail["stderr"]["tail"].endswith("local warning\n"))
        self.assertGreater(detail["stderr"]["omitted_bytes"], 0)
        self.assertNotIn("stdout", detail)
        self.assertNotIn("DO-NOT-LOG", stderr.getvalue())

    def test_real_command_failures_keep_exit_streams_and_terminal_policy(self):
        driver = self.driver("test", None)
        for code, terminal, error in ((1, False, native.Uncertain),
                                      (10, False, native.Uncertain),
                                      (30, False, native.Uncertain),
                                      (10, True, native.Refusal),
                                      (30, True, native.TerminalTimeout),
                                      (255, True, native.Uncertain)):
            with self.subTest(code=code, terminal=terminal):
                argv = self.local_child(
                    "import sys\n"
                    "sys.stdout.buffer.write(b'failed stdout only\\n')\n"
                    "sys.exit(int(sys.argv[1]))\n", str(code))
                with self.assertRaises(error) as caught:
                    driver.command(argv, b"DO-NOT-LOG-PRIVATE-STDIN", seconds=5,
                                   terminal_assertion=terminal)
                message = str(caught.exception)
                self.assertNotIn("DO-NOT-LOG", message)
                detail = json.loads(message.split(": ", 1)[1])
                self.assertEqual(detail["returncode"], code)
                self.assertEqual(detail["argv"], shlex.join(argv))
                self.assertEqual(detail["stdout"]["tail"], "failed stdout only\n")
                self.assertEqual(detail["stderr"]["tail"], "")

    def test_real_command_signal_and_bounded_invalid_utf8_diagnostics(self):
        argv = self.local_child(
            "import os, signal\n"
            "os.write(1, b'X' * 8192 + b'failed stdout tail\\n')\n"
            "os.write(2, b'Y' * 8192 + b'\\xffstderr tail\\n')\n"
            "os.kill(os.getpid(), signal.SIGTERM)\n", "A" * 8192, "end-context")
        driver = self.driver("setup", None)
        with self.assertRaises(native.Uncertain) as caught:
            driver.command(argv, seconds=5)
        message = str(caught.exception)
        detail = json.loads(message.split(": ", 1)[1])
        import signal
        self.assertEqual(detail["returncode"], -signal.SIGTERM)
        self.assertTrue(detail["stdout"]["tail"].endswith("failed stdout tail\n"))
        self.assertTrue(detail["stderr"]["tail"].endswith("\ufffdstderr tail\n"))
        for stream in ("stdout", "stderr"):
            self.assertGreater(detail[stream]["omitted_bytes"], 0)
            self.assertLessEqual(len(detail[stream]["tail"]), 4096)
        self.assertGreater(detail["argv_omitted_chars"], 0)
        self.assertTrue(detail["argv"].startswith(shlex.quote(sys.executable)))
        self.assertTrue(detail["argv"].endswith("end-context"))
        self.assertNotIn("\n", message)
        self.assertLess(len(message), 3 * 4096 + 1024)

    def test_real_command_timeout_preserves_partial_streams_without_retry(self):
        ready = self.base / "child-ready"
        argv = self.local_child(
            "import os, pathlib, sys, time\n"
            "os.write(1, b'partial stdout\\n')\n"
            "os.write(2, b'partial stderr\\n')\n"
            "pathlib.Path(sys.argv[1]).write_text('ready')\n"
            "time.sleep(30)\n", str(ready))
        with patch.object(native.subprocess, "run", wraps=subprocess.run) as execute:
            driver = self.driver("test", None)
            with self.assertRaises(native.Uncertain) as caught:
                driver.command(argv, seconds=1, terminal_assertion=True)
        self.assertTrue(ready.is_file())
        self.assertEqual(execute.call_count, 1)
        message = str(caught.exception)
        self.assertIn("timeout after 1s", message)
        self.assertIn("returncode unavailable", message)
        detail = json.loads(message[message.index("{"):])
        self.assertIsNone(detail["returncode"])
        self.assertEqual(detail["argv"], shlex.join(argv))
        self.assertEqual(detail["stdout"]["tail"], "partial stdout\n")
        self.assertEqual(detail["stderr"]["tail"], "partial stderr\n")

    def test_spawn_failure_and_elapsed_deadline_remain_uncertain(self):
        driver = self.driver("setup", None)
        argv = [str(self.base / "missing-child"), "spawn-context"]
        with self.assertRaises(native.Uncertain) as caught:
            driver.command(argv, seconds=5)
        message = str(caught.exception)
        self.assertIn("returncode unavailable", message)
        detail = json.loads(message[message.index("{"):])
        self.assertEqual(detail["argv"], shlex.join(argv))
        self.assertIsNone(detail["returncode"])
        self.assertIsNone(detail["stdout"])
        self.assertIsNone(detail["stderr"])
        fake = FakeExecutor(self.spec)
        driver = self.driver("setup", fake)
        driver.deadline = native.time.monotonic() - 1
        with self.assertRaisesRegex(native.Uncertain, "phase deadline"):
            driver.command(argv)
        self.assertFalse(driver.contacted)
        self.assertEqual(fake.calls, [])

    def test_setup_cleanup_failures_keep_guest_context_and_stdout(self):
        for phase, action in (("setup", "preflight"), ("cleanup", "cleanup")):
            with self.subTest(phase=phase):
                fake = FakeExecutor(self.spec)

                def execute(argv, **kwargs):
                    if argv[-2].startswith("root@"):
                        fake.calls.append(argv)
                        return subprocess.CompletedProcess(argv, 1, b"guest guard inventory\n", b"")
                    return fake(argv, **kwargs)

                driver = self.driver(phase, execute)
                driver.evidence = self.base / ("failed-" + phase)
                with self.assertRaises(native.Uncertain) as caught:
                    driver.run()
                detail = json.loads(str(caught.exception).split(": ", 1)[1])
                self.assertIn("root@172.31.253.11", detail["argv"])
                self.assertIn(action, detail["argv"])
                self.assertEqual(detail["returncode"], 1)
                self.assertEqual(detail["stdout"]["tail"], "guest guard inventory\n")
                self.assertEqual(detail["stderr"]["tail"], "")
                self.assertEqual(len(fake.calls), 2 if phase == "setup" else 1)

    def test_full_fixed_matrix_one_initialization_per_guest(self):
        self.assertEqual(native.CASE_NAMES, EXPECTED_CASES)
        self.assertEqual(len(EXPECTED_CASES), 15)
        fake = FakeExecutor(self.spec)
        for phase in native.PHASE_SECONDS:
            self.driver(phase, fake).run()
        runs = [shlex.split(c[-1]) for c in fake.calls if c[0] == "/usr/bin/ssh" and c[-2].startswith("root@") and shlex.split(c[-1])[3] == "run"]
        self.assertEqual(len(runs), 12)
        for i in range(3):
            selected = [r for r in runs if r[5] == str(i)]
            self.assertEqual([r[-3:] for r in selected], [[r, p, "yes" if n == 0 else "no"] for n, (r, p) in enumerate(native.MATRIX)])
        self.assertEqual(sum(c[0] == "/usr/bin/scp" for c in fake.calls), 6)
        for call in fake.calls:
            if call[0] == "/usr/bin/ssh" and call[-2].startswith("root@"):
                self.assertIn("StrictHostKeyChecking=yes", call)
                proxy = shlex.split(next(arg.split("=", 1)[1] for arg in call if arg.startswith("ProxyCommand=")))
                self.assertIn("paul@f3", proxy)
                self.assertIn("UserKnownHostsFile=" + str(self.guest_keys), call)

    def test_terminal_and_uncertain_remote_failures_stop_further_calls(self):
        for exit_code, error in ((10, native.Refusal), (30, native.TerminalTimeout), (255, native.Uncertain), (1, native.Uncertain)):
            with self.subTest(exit_code=exit_code):
                fake = FakeExecutor(self.spec)
                fake.fail = exit_code
                driver = self.driver("test", fake)
                driver.evidence = self.base / f"failure-{exit_code}"
                with self.assertRaises(error):
                    driver.run()
                runs = [c for c in fake.calls if c[0] == "/usr/bin/ssh" and c[-2].startswith("root@") and shlex.split(c[-1])[3] == "run"]
                self.assertEqual(len(runs), 1)
                terminal = exit_code in (10, 30)
                self.assertEqual(len(fake.calls), 3 if terminal else 2)
                self.assertEqual((driver.evidence / "hfs-test0/root-hfs-direct.json").is_file(), terminal)

    def test_socket_budget_and_wrong_elf_refused_offline(self):
        bins = [a["path"] for a in self.spec["artifacts"][:2]]
        with self.assertRaisesRegex(native.Refusal, "socket"):
            native.specification("a" * 64, *bins, inspect=False)
        Path(bins[0]).write_bytes(b"not ELF")
        with self.assertRaisesRegex(native.Refusal, "ELF"):
            native.specification("short", *bins, inspect=False)

    def test_evidence_replacement_and_tar_escape_refused(self):
        fake = FakeExecutor(self.spec)
        driver = self.driver("collect", fake)
        driver.save(0, "first.txt", b"first")
        with self.assertRaisesRegex(native.Uncertain, "changed"):
            driver.save(0, "first.txt", b"replacement")
        fake.files[0]["../escape"] = b"unsafe"
        with self.assertRaisesRegex(native.Uncertain, "unsafe"):
            driver.collect(0)
        self.assertFalse((self.base / "escape").exists())

    def test_report_semantic_negatives(self):
        volume, node = str(uuid.uuid4()), str(uuid.uuid4())
        for mutation in ("credentials", "journal", "skip", "xml", "xml-error", "node", "volume", "generation"):
            with self.subTest(mutation=mutation):
                files = reports(self.spec, "root", "hfs-buffered", volume, node)
                ready = json.loads(files["root-hfs-buffered.stdout"])
                seen = {"node": None, "volume": None, "generations": set(), "sessions": set()}
                if mutation == "credentials":
                    ready["client_uid"] = 20001
                    files["root-hfs-buffered.stdout"] = json.dumps(ready).encode()
                elif mutation in ("journal", "skip"):
                    report = json.loads(files["root-hfs-buffered.json"])
                    if mutation == "journal":
                        report["fixture"]["workspace_journal"] = "unused journal claim"
                    else:
                        report["results"][0]["status"] = "skip"
                    files["root-hfs-buffered.json"] = json.dumps(report).encode()
                elif mutation == "xml":
                    files["root-hfs-buffered.xml"] = b'<testsuite tests="0" failures="0"/>'
                elif mutation == "xml-error":
                    suite = ET.fromstring(files["root-hfs-buffered.xml"])
                    ET.SubElement(suite.find("testcase"), "error").text = "failed cleanup"
                    files["root-hfs-buffered.xml"] = ET.tostring(suite)
                elif mutation == "node":
                    seen["node"] = str(uuid.uuid4())
                elif mutation == "volume":
                    seen["volume"] = str(uuid.uuid4())
                else:
                    seen["generations"].add(ready["generation"])
                with self.assertRaises(native.Refusal):
                    native.validate_reports(files, self.spec, "root", "hfs-buffered", seen)

    def test_all_capabilities_and_junit_metadata_are_bound(self):
        key = "root-hfs-buffered"
        volume, node = str(uuid.uuid4()), str(uuid.uuid4())

        def fresh():
            files = reports(self.spec, "root", "hfs-buffered", volume, node)
            seen = {"node": None, "volume": None, "generations": set(), "sessions": set()}
            return files, seen

        baseline, _ = fresh()
        capabilities = json.loads(baseline[key + ".json"])["declared_capabilities"]
        self.assertEqual(len(capabilities), 9)
        for name, value in capabilities.items():
            with self.subTest(capability=name):
                files, seen = fresh()
                report = json.loads(files[key + ".json"])
                report["declared_capabilities"][name] = not value
                files[key + ".json"] = json.dumps(report).encode()
                with self.assertRaises(native.Refusal):
                    native.validate_reports(files, self.spec, "root", "hfs-buffered", seen)
        for name in ("target", "profile", "platform", "scope", "run_directory",
                     "expected_identity", "declared_capabilities", "fixture"):
            with self.subTest(junit_metadata=name):
                files, seen = fresh()
                suite = ET.fromstring(files[key + ".xml"])
                prop = next(p for p in suite.findall("properties/property") if p.get("name") == name)
                if name == "declared_capabilities":
                    changed = json.loads(prop.get("value"))
                    changed["writeback"] = True
                    prop.set("value", json.dumps(changed))
                elif name == "fixture":
                    changed = json.loads(prop.get("value"))
                    changed["role"] = "foreign-role"
                    prop.set("value", json.dumps(changed))
                else:
                    prop.set("value", "linux/arm64" if name == "platform" else "conflicting metadata")
                files[key + ".xml"] = ET.tostring(suite)
                with self.assertRaises(native.Refusal):
                    native.validate_reports(files, self.spec, "root", "hfs-buffered", seen)
        for mutation in ("missing-capability", "extra-capability", "numeric-capability",
                         "missing-property", "duplicate-property"):
            with self.subTest(mutation=mutation):
                files, seen = fresh()
                report = json.loads(files[key + ".json"])
                suite = ET.fromstring(files[key + ".xml"])
                if mutation == "missing-capability":
                    del report["declared_capabilities"]["writeback"]
                elif mutation == "extra-capability":
                    report["declared_capabilities"]["arbitrary_new_feature"] = True
                elif mutation == "numeric-capability":
                    report["declared_capabilities"]["file_sync"] = 1
                elif mutation == "missing-property":
                    props = suite.find("properties")
                    props.remove(props.find("property"))
                else:
                    suite.find("properties").append(copy.deepcopy(suite.find("properties/property")))
                files[key + ".json"] = json.dumps(report).encode()
                files[key + ".xml"] = ET.tostring(suite)
                with self.assertRaises(native.Refusal):
                    native.validate_reports(files, self.spec, "root", "hfs-buffered", seen)

    def test_actual_supervisor_reaped_timeout_live_timeout_and_failure(self):
        source = native.GUEST.read_text().split("<<'SUPERVISOR' || status=$?\n", 1)[1].split("\nSUPERVISOR\n", 1)[0]

        class FakeChild:
            pid = 123456

            def __init__(self, outcomes):
                self.outcomes = list(outcomes)
                self.waits, self.signals = [], []

            def wait(self, timeout):
                self.waits.append(timeout)
                outcome = self.outcomes.pop(0)
                if outcome == "timeout":
                    raise subprocess.TimeoutExpired("fake-owned-child", timeout)
                return outcome

            def send_signal(self, value):
                self.signals.append(value)

        scenarios = (("success", [0], 0, False, False),
                     ("assertion", [9], 10, False, False),
                     ("reaped-timeout", ["timeout", -15], 30, True, False),
                     ("live-timeout", ["timeout", "timeout"], 1, True, True))
        for name, outcomes, expected, timed_out, live in scenarios:
            with self.subTest(scenario=name):
                work = self.base / name
                (work / "evidence").mkdir(parents=True)
                child = FakeChild(outcomes)
                argv = ["supervisor", str(work), "root", "hfs-direct", "yes", "a" * 64]
                code = 0
                with patch.object(sys, "argv", argv), \
                        patch.object(subprocess, "Popen", return_value=child) as popen, \
                        contextlib.redirect_stderr(io.StringIO()):
                    try:
                        # Execute the real shipped heredoc, with fake Popen only:
                        # no child process, sleeps, preexec rlimits or real signal.
                        exec(compile(source, str(native.GUEST) + ":SUPERVISOR", "exec"), {})
                    except SystemExit as exit_status:
                        code = exit_status.code
                self.assertEqual(code, expected)
                self.assertEqual(child.waits, [95, 10] if timed_out else [95])
                self.assertEqual(len(child.signals), 1 if timed_out else 0)
                if timed_out:
                    import signal
                    self.assertEqual(child.signals, [signal.SIGTERM])
                self.assertTrue(popen.call_args.kwargs["start_new_session"])
                status = json.loads((work / "evidence/root-hfs-direct.exit.json").read_text())
                self.assertEqual(status["timed_out"], timed_out)
                self.assertEqual(status["preserved_live_process"], live)
                self.assertEqual(status["exit_code"], None if live else outcomes[-1])

    def test_controller_main_preserves_timeout_and_other_categories(self):
        for remote, expected in ((10, 10), (30, 30), (1, 1), (255, 1)):
            with self.subTest(remote_exit=remote):
                fake = FakeExecutor(self.spec)
                fake.fail = remote
                driver = self.driver("test", fake)
                driver.evidence = self.base / f"main-category-{remote}"
                with patch.object(sys, "argv", [str(SOURCE), "--driver", "test", str(self.base / "subplan.json")]), \
                        patch.object(native, "validate_outer", return_value=self.spec), \
                        patch.object(native, "Driver", return_value=driver), \
                        contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(native.main(), expected)

    def test_outer_binding_rejects_missing_pin_resource_and_cleanup(self):
        source = self.base / "native-spec.json"
        source.write_text(json.dumps(self.spec))
        fragment = native.outer_fragment(source)
        outer = dict(fragment, schema=1, stage="stage1", prepared_guests=True,
                     run_id=self.spec["run_id"], created="2020-01-01T00:00:00Z", expires="2100-01-01T00:00:00Z",
                     budget={"artifact_bytes": 32 << 20, "disk_bytes": 4 << 30,
                             "io_bytes": 48 << 20, "retention_hours": 168})
        plan = self.base / "plan.json"
        with patch.dict(os.environ, HFS_LAB_PLAN=str(plan), HFS_LAB_RUN_ID=self.spec["run_id"]):
            plan.write_text(json.dumps(outer))
            native.validate_outer(source, "setup")
            for field in ("pin", "resource", "cleanup", "argv", "collection-missing", "collection-preflight", "collection-duplicate"):
                changed = copy.deepcopy(outer)
                if field == "pin":
                    changed["artifacts"].pop()
                elif field == "resource":
                    changed["resources"].pop()
                elif field == "cleanup":
                    changed["steps"][-1]["applies_after"] = []
                elif field.startswith("collection-"):
                    changed["steps"][2]["applies_after"] = {
                        "collection-missing": [],
                        "collection-preflight": ["native-setup", "native-test", "preflight"],
                        "collection-duplicate": ["native-setup", "native-setup"],
                    }[field]
                else:
                    changed["steps"][1]["args"][1] = "setup"
                plan.write_text(json.dumps(changed))
                with self.assertRaises(native.Refusal):
                    native.validate_outer(source, "setup")
            # A legacy collection schedule must never strand approved cleanup.
            legacy = copy.deepcopy(outer)
            legacy["steps"][2]["applies_after"] = []
            plan.write_text(json.dumps(legacy))
            native.validate_outer(source, "cleanup")


if __name__ == "__main__":
    unittest.main()
