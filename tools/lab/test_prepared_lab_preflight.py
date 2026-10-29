"""Offline fixtures only: no network, VM, mount, ZFS or package operations.

Set HFS_PREFLIGHT_LAB_BINARY to a previously built hfs-lab executable to also
canonicalize the complete fixture using the real current Go Plan validator.
"""

from datetime import datetime, timedelta, timezone
import hashlib
import io
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import sys
import time

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("prepared_preflight", HERE / "prepared-lab-preflight.py")
p = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(p)

PACKAGES = "python312-3.12.15\npkg-2.4.2\n"
PKG_HASH = p.package_hash(PACKAGES)
CONFIG = 'memory="2G"\ncpu=2\ndisk0_name="disk0.img"\n'
HEADER = "NAME DATASTORE LOADER CPU MEMORY VNC AUTO STATE\n"
# Exact retained FreeBSD 15.1 df -kP planning sample supplied by the root agent
# (2026-10-29); tests never read a host or external evidence path.
FREEBSD_DF = ("Filesystem                1024-blocks Used     Avail Capacity  Mounted on\n"
              "zroot/bhyve/homelabfs-lab   119438908  340 119438568     0%    /zroot/bhyve/homelabfs-lab\n")


def json_file(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")
    return path


def artifact(artifact_id, path):
    return {"id": artifact_id, "path": str(path), "version": "offline-fixture-v1",
            "sha256": p.digest(path)}


def fixture(root, rocky_running=False):
    """A complete proposed Plan, never an approval or an executable lab run."""
    state = root / "state"
    state.mkdir()
    keys = root / "guest_keys"
    keys.write_text(p.REPO_KEYS.read_text() + "rocky.test ssh-ed25519 AAAATESTFIXTURE\n")
    host_keys = root / "host_keys"
    host_keys.write_text("f3 ssh-ed25519 AAAAHOSTFIXTURE\n")
    driver = root / "fixture-only.sh"
    driver.write_text("#!/bin/sh\n# Fixture artifact; never executed.\nexit 20\n")
    allocations = {}
    stopped = {}
    targets = []
    configs = {}
    for i, name in enumerate(("hfs-test0", "hfs-test1", "hfs-test2", "rocky", "freebsd")):
        directory = f"/zroot/bhyve/homelabfs-lab/{name}" if i < 3 else f"/zroot/bhyve/{name}"
        configs[name] = CONFIG if i < 3 else CONFIG.replace('2G', '12G').replace('cpu=2', 'cpu=4')
        initial = "Running" if name == "rocky" and rocky_running else "Stopped"
        kernel = "fixture-FreeBSD-15.1" if name != "rocky" else "fixture-Linux-6.1"
        targets.append({"name": name, "host": "f3", "address": f"172.31.253.{11+i}" if i < 3 else name + ".test",
                        "initial_state": initial, "kernel": kernel, "packages": PKG_HASH})
        allocations[name] = {"config": directory + f"/{name}.conf",
                             "config_sha256": hashlib.sha256(configs[name].encode()).hexdigest(),
                             "memory_bytes": (2 if i < 3 else 12) * p.GIB,
                             "cpus": 2 if i < 3 else 4, "os_disk": directory + "/disk0.img"}
        if initial == "Stopped":
            stopped[name] = {"kind": "offline-inspected-zfs-snapshot-v1",
                             "snapshot": directory.removeprefix("/") + "@fixture-inspected",
                             "guid": str(100 + i), "os_disk": allocations[name]["os_disk"],
                             "kernel": kernel, "packages": PKG_HASH,
                             "provenance": "2099-01-01 synthetic offline fixture; no real inspection"}
    policy = {"schema": 1, "host_address": "192.0.2.133", "host_keys": "host-keys", "guest_keys": "guest-keys",
              "host_kernel": "fixture-host-15.1", "host_packages": PKG_HASH,
              "allowed_users": ["paul"], "max_sessions": 1, "command_timeout_seconds": 5,
              "max_load_1m": 4, "disk_path": "/zroot/bhyve/homelabfs-lab", "disk_reserve_bytes": p.GIB,
              "allocations": allocations, "stopped": stopped}
    policy_path = json_file(root / "policy.json", policy)
    now = datetime.now(timezone.utc).replace(microsecond=0)
    def step(step_id, phase, action, artifact_id, contract, effects="read-only", after=None, test="", args=None, timeout=10):
        return {"id": step_id, "phase": phase, "test": test, "action": action, "artifact": artifact_id,
                "args": args or [], "contract": contract, "effects": effects, "timeout_seconds": timeout,
                "applies_after": after or [], "max_attempts": 1, "idempotent": True}
    steps = [step("preflight", "setup", "driver", "preflight-driver", p.CONTRACT,
                  args=["--policy-artifact", "preflight-policy"], timeout=180)]
    if rocky_running:
        steps.append(step("stop-rocky", "setup", "stop-rocky", "vms", "prepared-vms-v1", "lifecycle"))
    steps += [step("start-lab", "setup", "start-lab", "vms", "prepared-vms-v1", "lifecycle"),
              step("fixture-test", "test", "driver", "fixture-driver", "fixture-no-native-claim-v1", test="fixture-parser"),
              step("fixture-collect", "collect", "driver", "fixture-driver", "fixture-no-native-claim-v1"),
              step("stop-lab", "cleanup", "stop-lab", "vms", "prepared-vms-v1", "lifecycle", ["start-lab"])]
    if rocky_running:
        steps.append(step("restore-rocky", "restore", "start-rocky", "vms", "prepared-vms-v1", "lifecycle", ["stop-rocky"]))
    # Field order matches Go's canonical typed Plan; tests verify that against the
    # actual validator when its explicitly supplied local executable is available.
    plan = {"schema": 1, "run_id": "offline-preflight-fixture", "stage": "stage1",
            "created": now.isoformat().replace("+00:00", "Z"),
            "expires": (now + timedelta(hours=1)).isoformat().replace("+00:00", "Z"),
            "profile": "prepared-preflight-offline-fixture", "prepared_guests": True,
            "targets": targets,
            "artifacts": [artifact("preflight-driver", HERE / "prepared-lab-preflight.py"),
                          artifact("preflight-policy", policy_path), artifact("guest-keys", keys),
                          artifact("host-keys", host_keys), artifact("fixture-driver", driver),
                          artifact("vms", HERE / "vms.sh")],
            "resources": [{"name": "payload", "kind": "directory", "target": "hfs-test0",
                           "location": "/var/tmp/hfs-run-offline-preflight-fixture", "max_bytes": 1 << 20}],
            "budget": {"guest_memory_bytes": 6 * p.GIB, "host_headroom_bytes": 4 * p.GIB, "cpus": 6,
                       "disk_bytes": 1 << 20, "io_bytes": 1 << 20, "artifact_bytes": 16 << 20,
                       "retention_hours": 1, "runtime_seconds": 300, "cleanup_seconds": 30,
                       "restoration_seconds": 30}, "disruptions": ["rocky-downtime"] if rocky_running else [],
            "tests": [{"name": "fixture-parser", "seed": 17}], "state_dir": str(state),
            "evidence_dir": str(root / "evidence"), "preserve_failures": True, "steps": steps}
    snapshot = json_file(state / "plan.json", plan)
    env = {"HFS_LAB_PLAN": str(snapshot), "HFS_LAB_PLAN_HASH": p.canonical_hash(plan), "HFS_LAB_RUN_ID": plan["run_id"]}
    return plan, policy, env, configs


class FakeTransport:
    def __init__(self, plan, policy, configs):
        self.plan, self.policy, self.configs = plan, policy, configs
        self.overrides = {}
        self.calls = []
        self.metric_calls = 0

    def __call__(self, destination, script):
        self.calls.append((destination, script))
        if (destination, script) in self.overrides:
            value = self.overrides[destination, script]
            if callable(value):
                return value()
            if isinstance(value, Exception):
                raise value
            return value
        if script == "vm list":
            rows = []
            for target in self.plan["targets"]:
                a = self.policy["allocations"][target["name"]]
                suffix = " (4321)" if target["initial_state"] == "Running" else ""
                auto = "Yes [1]" if target["name"] == "rocky" else "No"
                rows.append(f"{target['name']} default bhyveload {a['cpus']} {a['memory_bytes']//p.GIB}G - {auto} {target['initial_state']}{suffix}")
            return HEADER + "\n".join(rows) + "\n"
        if script == "who":
            return "paul pts/0 Oct 29 10:00 (fixture)\n"
        if script == p.HOST_METRICS:
            self.metric_calls += 1
            free, inactive = ((100000, 300000) if any(t["initial_state"] == "Running" for t in self.plan["targets"])
                              else (3000000, 1000000))
            return f"17179869184\n8\n4096\n{free}\n{inactive}\n10000\n12\n13\n{{ 0.10 0.12 0.15 }}\n"
        if script == "swapinfo -k":
            return "Device 1K-blocks Used Avail Capacity\n/dev/gpt/swap 1024 0 1024 0%\n"
        if script == "uname -r":
            if destination == "host":
                return self.policy["host_kernel"] + "\n"
            return next(t["kernel"] for t in self.plan["targets"] if t["address"] == destination) + "\n"
        if script in (p.PKG_FREEBSD, p.PKG_LINUX):
            return PACKAGES
        if script.startswith("test -f ") and "; cat " in script:
            name = next(name for name, a in self.policy["allocations"].items() if a["config"] in script)
            return self.configs[name]
        if script.startswith("zfs get -Hp -o value guid "):
            return next(e["guid"] for e in self.policy["stopped"].values() if e["snapshot"] in script) + "\n"
        if script.startswith("zfs get -Hp -o value written@"):
            return "0\n"
        if script.startswith("zfs get -Hp -o value mountpoint "):
            return "/" + script.split()[-1] + "\n"
        if "df -kP " in script:
            path = script.split()[-1]
            filesystem = path.removeprefix("/").removesuffix("/disk0.img")
            return ("Filesystem 1024-blocks Used Avail Capacity Mounted on\n"
                    f"{filesystem} 999999999 1 999999998 1% {path}\n")
        raise AssertionError(f"unexpected transport command: {destination}: {script}")


class PreflightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hfs-preflight-offline-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.plan, self.policy, self.env, self.configs = fixture(self.root)
        self.fake = FakeTransport(self.plan, self.policy, self.configs)

    def execute(self):
        return p.execute("preflight-policy", self.env, lambda *args: self.fake)

    def save(self):
        policy_path = self.root / "policy.json"
        json_file(policy_path, self.policy)
        next(a for a in self.plan["artifacts"] if a["id"] == "preflight-policy")["sha256"] = p.digest(policy_path)
        json_file(Path(self.env["HFS_LAB_PLAN"]), self.plan)
        self.env["HFS_LAB_PLAN_HASH"] = p.canonical_hash(self.plan)

    def test_complete_stopped_admission_is_offline_evidence_only(self):
        report = self.execute()
        self.assertEqual(report["status"], "admitted")
        self.assertTrue(all(destination == "host" for destination, _ in self.fake.calls))
        self.assertTrue(all("not a live" in value["kernel_source"] for value in report["version_evidence"].values()))
        self.assertFalse(any(word in script.split() for _, script in self.fake.calls
                             for word in ("start", "stop", "mount", "install", "snapshot", "destroy")))

    def test_running_rocky_uses_only_pinned_live_version_reads(self):
        other = self.root / "running"
        other.mkdir()
        plan, policy, env, configs = fixture(other, True)
        fake = FakeTransport(plan, policy, configs)
        report = p.execute("preflight-policy", env, lambda *args: fake)
        self.assertEqual(report["version_evidence"]["rocky"]["kind"], "live-pinned-ssh")
        self.assertEqual({d for d, _ in fake.calls}, {"host", "rocky.test"})

    def test_inventory_parser_rejects_unknown_missing_duplicate_and_transitional(self):
        valid = self.fake("host", "vm list")
        for invalid in ("", valid.replace("NAME", "GUEST"), valid + valid.splitlines()[1] + "\n",
                        valid.replace("Stopped", "Starting", 1), valid.replace("2G", "unknown", 1)):
            with self.subTest(invalid=invalid), self.assertRaises(p.Blocked):
                p.inventory(invalid)

    def test_initial_state_extra_vm_and_allocation_change_block(self):
        original = self.fake("host", "vm list")
        bad = [original.replace("Stopped", "Running (55)", 1),
               original + "other default uefi 4 4G - No Running (77)\n",
               original.replace("2G", "3G", 1),
               "\n".join(original.splitlines()[:-1]) + "\n"]
        for value in bad:
            self.fake.overrides["host", "vm list"] = value
            with self.subTest(value=value), self.assertRaises(p.Blocked):
                self.execute()

    def test_other_stopped_vm_is_permitted_and_recorded(self):
        original = self.fake("host", "vm list")
        self.fake.overrides["host", "vm list"] = original + "other default uefi 4 4G - No Stopped\n"
        self.assertIn("other", self.execute()["inventory"])

    def test_memory_pressure_active_swap_capacity_and_users_block(self):
        original = self.fake("host", p.HOST_METRICS)
        cases = [(p.HOST_METRICS, original.replace("3000000", "100")),
                 (p.HOST_METRICS, original.replace("17179869184", "1073741824")),
                 (p.HOST_METRICS, original.replace("0.10", "9.00")),
                 ("swapinfo -k", "Device 1K-blocks Used Avail Capacity\n/dev/swap 1024 1 1023 1%\n"),
                 ("df -kP " + self.policy["disk_path"], "Filesystem 1024-blocks Used Avail Capacity Mounted on\nzroot 1 1 0 100% /zroot\n"),
                 ("who", "stranger pts/0 Oct 29 10:00\n"),
                 ("who", "paul pts/0 Oct 29 10:00\npaul pts/1 Oct 29 10:01\n")]
        for script, response in cases:
            self.fake.overrides = {("host", script): response}
            with self.subTest(script=script, response=response), self.assertRaises(p.Blocked):
                self.execute()

    def test_missing_changed_snapshot_or_changed_disk_binding_blocks(self):
        del self.policy["stopped"]["hfs-test0"]
        self.save()
        with self.assertRaisesRegex(p.Blocked, "attestations"):
            self.execute()
        self.assertEqual(self.fake.calls, [])

    def test_live_snapshot_zero_written_and_dataset_binding_are_enforced(self):
        evidence = self.policy["stopped"]["hfs-test0"]
        dataset, tag = evidence["snapshot"].split("@")
        cases = [("zfs get -Hp -o value guid " + evidence["snapshot"], "999\n"),
                 (f"zfs get -Hp -o value written@{tag} {dataset}", "1\n"),
                 (f"zfs get -Hp -o value mountpoint {dataset}", "/elsewhere\n")]
        for script, response in cases:
            self.fake.overrides = {("host", script): response}
            with self.subTest(script=script), self.assertRaises(p.Blocked):
                self.execute()

    def test_config_and_host_version_changes_block(self):
        cases = [("uname -r", "other-kernel\n"), (p.PKG_FREEBSD, "other-1\n"), (p.PKG_FREEBSD, "")]
        for script, response in cases:
            self.fake.overrides = {("host", script): response}
            with self.subTest(script=script, response=response), self.assertRaises(p.Blocked):
                self.execute()
        self.fake.overrides = {}
        self.configs["hfs-test0"] += "# changed\n"
        with self.assertRaisesRegex(p.Blocked, "config changed"):
            self.execute()

    def test_policy_and_known_hosts_hash_changes_block_before_contact(self):
        (self.root / "guest_keys").write_text("changed\n")
        with self.assertRaisesRegex(p.Blocked, "artifact changed"):
            self.execute()
        self.assertEqual(self.fake.calls, [])

    def test_missing_native_pin_even_with_reviewed_new_artifact_blocks(self):
        keys = self.root / "guest_keys"
        keys.write_text("172.31.253.11 ssh-ed25519 other\n")
        next(a for a in self.plan["artifacts"] if a["id"] == "guest-keys")["sha256"] = p.digest(keys)
        self.save()
        with self.assertRaisesRegex(p.Blocked, "repository native SSH pin changed"):
            self.execute()
        self.assertEqual(self.fake.calls, [])

    def test_transport_failure_timeout_and_malformed_evidence_block(self):
        for failure in (p.Blocked("transport uncertain"), p.TimedOut("transport timeout"), "garbage"):
            self.fake.overrides = {("host", "vm list"): failure}
            with self.subTest(failure=failure), self.assertRaises(p.Blocked):
                self.execute()

    def test_plan_hash_and_exact_invocation_are_required(self):
        self.env["HFS_LAB_PLAN_HASH"] = "0" * 64
        with self.assertRaisesRegex(p.Blocked, "hash mismatch"):
            self.execute()
        self.plan["steps"][0]["effects"] = "run-owned"
        self.save()
        with self.assertRaisesRegex(p.Blocked, "contract/argv"):
            self.execute()

    def test_package_and_config_parsers_do_not_execute_shell(self):
        self.assertEqual(p.package_hash(PACKAGES), p.package_hash("pkg-2.4.2\npython312-3.12.15\n"))
        for content in (CONFIG + 'cpu=9\n', CONFIG.replace('cpu=2', 'cpu=$(touch /tmp/never)'),
                        CONFIG.replace('disk0.img', '../other.img')):
            if "../" in content:
                # Literal traversal parses safely but cannot equal reviewed OS disk.
                continue
            with self.assertRaises(p.Blocked):
                p.config_values(content)

    def test_active_paging_and_low_free_watermark_block_running_rocky(self):
        directory = self.root / "paging"
        directory.mkdir()
        plan, policy, env, configs = fixture(directory, True)
        fake = FakeTransport(plan, policy, configs)
        original = fake("host", p.HOST_METRICS)
        fake.overrides["host", p.HOST_METRICS] = original.replace("100000\n300000", "1\n300000")
        with self.assertRaisesRegex(p.Blocked, "pressure"):
            p.execute("preflight-policy", env, lambda *args: fake)
        count = [0]
        def paging_metrics():
            count[0] += 1
            return original if count[0] == 1 else original.replace("12\n13", "13\n13")
        fake.overrides["host", p.HOST_METRICS] = paging_metrics
        with self.assertRaisesRegex(p.Blocked, "paging"):
            p.execute("preflight-policy", env, lambda *args: fake)

    def test_running_rocky_cannot_request_headroom_beyond_pinned_post_stop_guard(self):
        directory = self.root / "larger-headroom"
        directory.mkdir()
        plan, policy, env, configs = fixture(directory, True)
        plan["budget"]["host_headroom_bytes"] = 5 * p.GIB
        json_file(Path(env["HFS_LAB_PLAN"]), plan)
        env["HFS_LAB_PLAN_HASH"] = p.canonical_hash(plan)
        fake = FakeTransport(plan, policy, configs)
        with self.assertRaisesRegex(p.Blocked, "exactly 4 GiB"):
            p.execute("preflight-policy", env, lambda *args: fake)
        self.assertEqual(fake.calls, [])

    def test_older_unguarded_lifecycle_helper_cannot_satisfy_plan(self):
        older = self.root / "vms.sh"
        older.write_text("#!/bin/sh\nexit 0\n")
        self.plan["artifacts"][-1] = artifact("vms", older)
        self.save()
        with self.assertRaisesRegex(p.Blocked, "post-stop memory guard"):
            self.execute()
        self.assertEqual(self.fake.calls, [])
        self.assertEqual(p.digest(p.VMS), p.GUARDED_VMS_SHA256)

    def test_real_start_lab_memory_guard_with_local_fake_commands_only(self):
        # Extract the exact existing remote shell body, then run it locally with
        # all machine-inspection/lifecycle commands replaced by temporary fakes.
        body = (HERE / "vms.sh").read_text().split("<<'HOST'\n", 1)[1].rsplit("\nHOST", 1)[0]
        fakebin = self.root / "fakebin"
        fakebin.mkdir()
        live = self.fake("host", "vm list")
        inventory_file = self.root / "inventory"
        inventory_file.write_text(live)
        programs = {
            "vm": '#!/bin/sh\ncase "$1" in list) cat "$HFS_FAKE_INVENTORY";; start) printf "%s\\n" "$2" >> "$HFS_FAKE_STARTS";; *) exit 99;; esac\n',
            "sysctl": '#!/bin/sh\ncase "$2" in hw.physmem) echo 17179869184;; hw.pagesize) echo 4096;; vm.stats.vm.v_free_count) printf "%s\\n" "$HFS_FAKE_FREE";; vm.stats.vm.v_inactive_count) echo 1000000;; *) exit 99;; esac\n',
            "swapinfo": '#!/bin/sh\nprintf "Device 1K-blocks Used Avail Capacity\\n/dev/swap 1024 %s 1024 0%%\\n" "$HFS_FAKE_SWAP"\n',
            "sysrc": '#!/bin/sh\necho 2G\n',
        }
        for name, content in programs.items():
            executable = fakebin / name
            executable.write_text(content)
            executable.chmod(0o700)
        for free, swap, succeeds in (("2000000", "0", True), ("1", "0", False),
                                     ("2000000", "1", False), ("unknown", "0", False)):
            starts = self.root / "starts"
            starts.unlink(missing_ok=True)
            env = {"PATH": str(fakebin) + ":/usr/bin:/bin", "HFS_FAKE_INVENTORY": str(inventory_file),
                   "HFS_FAKE_STARTS": str(starts), "HFS_FAKE_FREE": free, "HFS_FAKE_SWAP": swap}
            result = subprocess.run(["/bin/sh", "-s", "--", "start-lab"], input=body,
                                    capture_output=True, text=True, timeout=3, env=env)
            with self.subTest(free=free, swap=swap):
                self.assertEqual(result.returncode == 0, succeeds, result.stderr)
                if succeeds:
                    self.assertEqual(starts.read_text().splitlines(), ["hfs-test0", "hfs-test1", "hfs-test2"])
                else:
                    self.assertFalse(starts.exists(), result.stdout + result.stderr)

    def test_bounded_transport_owns_timeout_output_and_exit_status(self):
        real_popen = subprocess.Popen
        children = []
        def launch(code):
            def fake_popen(argv, **kwargs):
                self.assertEqual(argv[0], "/usr/bin/ssh")
                self.assertIn("HostName=192.0.2.133", argv)
                self.assertIn("HostKeyAlias=f3", argv)
                child = real_popen([sys.executable, "-c", code], **kwargs)
                children.append(child)
                return child
            return fake_popen
        cases = [("import sys; sys.stdin.read(); print('fixture')", None),
                 ("import sys; sys.stdin.read(); sys.exit(255)", p.Uncertain),
                 ("import sys; sys.stdin.read(); print('X'*300000)", p.Uncertain),
                 ("import sys,time; sys.stdin.read(); time.sleep(2)", p.TimedOut)]
        for code, error in cases:
            transport = p.Transport(self.root / "host_keys", self.root / "guest_keys", 0.1,
                                    time.monotonic() + 1, "192.0.2.133")
            with self.subTest(code=code), mock.patch.object(p.subprocess, "Popen", launch(code)):
                if error:
                    with self.assertRaises(error):
                        transport("host", "vm list")
                else:
                    self.assertEqual(transport("host", "vm list"), "fixture\n")
            self.assertIsNotNone(children[-1].poll())

    def test_retained_freebsd_df_format_parser_and_complete_admission(self):
        self.assertEqual(p.disk_available(FREEBSD_DF), 119438568 * 1024)
        self.fake.overrides["host", "df -kP " + self.policy["disk_path"]] = FREEBSD_DF
        self.assertEqual(self.execute()["host_disk_available_bytes"], 119438568 * 1024)
        for invalid in (FREEBSD_DF.replace("Avail", "Available"),
                        FREEBSD_DF.replace("1024-blocks", "Size"),
                        FREEBSD_DF.replace("119438568", "unknown"),
                        FREEBSD_DF + FREEBSD_DF.splitlines()[1] + "\n", "", "df: unavailable\n"):
            with self.subTest(invalid=invalid), self.assertRaises(p.Blocked):
                p.disk_available(invalid)
        with self.assertRaisesRegex(p.Blocked, "dataset mismatch"):
            p.disk_available(FREEBSD_DF, "other/dataset")

    def test_main_preserves_transport_uncertainty_exit_classification(self):
        real_popen = subprocess.Popen
        cases = [("import sys; sys.stdin.read(); print('fixture')", 0, "admitted"),
                 ("import sys; sys.stdin.read(); sys.exit(2)", 20, "blocked"),
                 ("import sys; sys.stdin.read(); sys.exit(255)", 1, "uncertain"),
                 ("import sys; sys.stdin.read(); print('X'*300000)", 1, "uncertain"),
                 ("import sys,time; sys.stdin.read(); time.sleep(2)", 1, "uncertain")]
        for code, expected_exit, status in cases:
            children = []
            def launch(_argv, **kwargs):
                child = real_popen([sys.executable, "-c", code], **kwargs)
                children.append(child)
                return child
            def inspect(*_args):
                transport = p.Transport(self.root / "host_keys", self.root / "guest_keys", 0.1,
                                        time.monotonic() + 1, "192.0.2.133")
                transport("host", "vm list")
                return {"status": "admitted"}
            stdout, stderr = io.StringIO(), io.StringIO()
            with self.subTest(code=code), mock.patch.object(p.subprocess, "Popen", launch), \
                    mock.patch.object(p, "execute", side_effect=inspect), \
                    mock.patch.object(sys, "argv", ["preflight", "--policy-artifact", "preflight-policy"]), \
                    mock.patch.object(sys, "stdout", stdout), mock.patch.object(sys, "stderr", stderr), \
                    mock.patch.object(p.signal, "signal"):
                self.assertEqual(p.main(), expected_exit)
            report = json.loads(stdout.getvalue() if expected_exit == 0 else stderr.getvalue())
            self.assertEqual(report["status"], status)
            self.assertIsNotNone(children[-1].poll())
            if status == "uncertain":
                self.assertIn("reconciliation", report["remote_completion"])

    def test_main_precontact_refusal_and_handled_interruption_exit_codes(self):
        for error, expected_exit, status in ((p.Blocked("missing policy"), 20, "blocked"),
                                              (p.TimedOut("interrupted in flight"), 1, "uncertain")):
            stderr = io.StringIO()
            with self.subTest(error=error), mock.patch.object(p, "execute", side_effect=error), \
                    mock.patch.object(sys, "argv", ["preflight", "--policy-artifact", "preflight-policy"]), \
                    mock.patch.object(sys, "stderr", stderr), mock.patch.object(p.signal, "signal"):
                self.assertEqual(p.main(), expected_exit)
            self.assertEqual(json.loads(stderr.getvalue())["status"], status)
        handlers = {}
        def record_handler(signum, handler):
            handlers[signum] = handler
        def interrupt(*_args):
            handlers[p.signal.SIGTERM](p.signal.SIGTERM, None)
        stderr = io.StringIO()
        with mock.patch.object(p.signal, "signal", side_effect=record_handler), \
                mock.patch.object(p, "execute", side_effect=interrupt), \
                mock.patch.object(sys, "argv", ["preflight", "--policy-artifact", "preflight-policy"]), \
                mock.patch.object(sys, "stderr", stderr):
            self.assertEqual(p.main(), 1)
        self.assertEqual(json.loads(stderr.getvalue())["status"], "uncertain")

    @unittest.skipUnless(os.environ.get("HFS_PREFLIGHT_LAB_BINARY"), "provide already built hfs-lab for real Plan validator")
    def test_actual_go_plan_validator_and_canonical_hash(self):
        binary = os.environ["HFS_PREFLIGHT_LAB_BINARY"]
        for rocky_running in (False, True):
            directory = self.root / ("go-running" if rocky_running else "go-stopped")
            directory.mkdir()
            plan, policy, env, configs = fixture(directory, rocky_running)
            proposed = directory / "proposed.json"
            canonical = directory / "canonical.json"
            json_file(proposed, plan)
            result = subprocess.run([binary, "plan", "--input", str(proposed), "--out", str(canonical)],
                                    capture_output=True, text=True, timeout=15, check=True)
            from_go = p.read_json(canonical)
            self.assertEqual(p.canonical_hash(from_go), env["HFS_LAB_PLAN_HASH"])
            self.assertIn(env["HFS_LAB_PLAN_HASH"], result.stdout)
            json_file(Path(env["HFS_LAB_PLAN"]), from_go)
            report = p.execute("preflight-policy", env, lambda *args: FakeTransport(plan, policy, configs))
            self.assertEqual(report["status"], "admitted")


if __name__ == "__main__":
    unittest.main()
