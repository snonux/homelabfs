#!/usr/bin/env python3
"""Read-only first driver for an already approved hfs-lab canonical Plan.

No approval, lifecycle, package installation, snapshot or report state is owned
here. All remote commands are fixed inspection operations; policy data is quoted
as shell arguments and never sourced. Offline tests inject the transport.
"""

import argparse
from datetime import date
import hashlib
import ipaddress
import json
import os
from pathlib import Path, PurePosixPath
import re
import selectors
import shlex
import signal
import subprocess
import sys
import time

CONTRACT = "prepared-lab-preflight-v1"
GIB = 1 << 30
NAMES = {"hfs-test0", "hfs-test1", "hfs-test2", "rocky", "freebsd"}
REPO_KEYS = Path(__file__).resolve().parents[2] / "docs/lab/f3-known_hosts"
LIMIT = 256 << 10
VMS = Path(__file__).resolve().with_name("vms.sh")
# Coupled deliberately: conditional pre-stop admission requires this exact guard.
GUARDED_VMS_SHA256 = "b71df8d9696a515bc3bf86bcdbc3ffaaef7f14156665a024511b150889f4c498"


class Blocked(Exception):
    """Known-terminal admission refusal; no inspection can still be in flight."""


class Uncertain(Blocked):
    """Remote inspection may survive transport loss; reconciliation required."""


class TimedOut(Uncertain):
    pass


def require(condition, message):
    if not condition:
        raise Blocked(message)


def read_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, f"duplicate JSON key: {key}")
            result[key] = value
        return result
    with open(path, "rb") as source:
        raw = source.read((4 << 20) + 1)
    require(len(raw) <= 4 << 20, "JSON exceeds 4 MiB")
    return json.loads(raw, object_pairs_hook=unique)


def digest(path):
    with open(path, "rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def canonical_hash(plan):
    # The runner writes a typed Go Plan snapshot in struct field order. Preserve
    # that order and Go's HTML escaping; reject other encodings via hash mismatch.
    raw = json.dumps(plan, ensure_ascii=False, separators=(",", ":"), allow_nan=False)
    for char, escaped in (("&", r"\u0026"), ("<", r"\u003c"), (">", r"\u003e"),
                          ("\u2028", r"\u2028"), ("\u2029", r"\u2029")):
        raw = raw.replace(char, escaped)
    return hashlib.sha256(raw.encode()).hexdigest()


def fields(value, expected, label):
    require(isinstance(value, dict) and set(value) == set(expected.split()),
            f"{label}: exact fields required: {expected}")


def positive(value, label, zero=False):
    require(type(value) is int and value >= (0 if zero else 1),
            f"{label}: integer required")
    return value


def safe_path(value):
    require(isinstance(value, str) and re.fullmatch(r"/[A-Za-z0-9_./-]+", value)
            and str(PurePosixPath(value)) == value and ".." not in value.split("/"),
            f"unsafe absolute path: {value!r}")
    return value


def sha(value):
    require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value),
            "lowercase SHA-256 required")
    return value


def verified_artifact(plan, artifact_id):
    matches = [a for a in plan["artifacts"] if a["id"] == artifact_id]
    require(len(matches) == 1, f"missing/duplicate artifact: {artifact_id}")
    artifact = matches[0]
    path = Path(artifact["path"])
    require(path.is_absolute() and path.is_file() and not path.is_symlink(),
            f"regular nonsymlink pinned artifact required: {artifact_id}")
    require(digest(path) == sha(artifact["sha256"]), f"artifact changed: {artifact_id}")
    return path


def load_context(policy_id, env):
    require(re.fullmatch(r"[0-9a-f]{64}", env.get("HFS_LAB_PLAN_HASH", "")),
            "runner Plan hash required; this driver grants no approval")
    plan = read_json(env["HFS_LAB_PLAN"])
    require(canonical_hash(plan) == env["HFS_LAB_PLAN_HASH"], "canonical Plan hash mismatch")
    require(plan["schema"] == 1 and plan["stage"] == "stage1" and plan["prepared_guests"] is True,
            "prepared Stage 1 Plan required")
    require(plan["run_id"] == env.get("HFS_LAB_RUN_ID"), "runner run ID mismatch")
    require(Path(env["HFS_LAB_PLAN"]) == Path(plan["state_dir"]) / "plan.json",
            "require runner controller Plan snapshot")
    first = plan["steps"][0]
    require(first["contract"] == CONTRACT and first["action"] == "driver"
            and first["phase"] == "setup" and first["effects"] == "read-only"
            and first["args"] == ["--policy-artifact", policy_id], "first driver contract/argv mismatch")
    require(verified_artifact(plan, first["artifact"]) == Path(__file__).resolve(),
            "first driver must pin this executable")
    require(60 <= first["timeout_seconds"] <= 600, "preflight timeout must be 60..600 seconds")
    startup = [s for s in plan["steps"] if s["action"] == "start-lab"]
    require(len(startup) == 1, "exactly one guarded lab startup required")
    helper = verified_artifact(plan, startup[0]["artifact"])
    require(helper == VMS and digest(helper) == GUARDED_VMS_SHA256,
            "lab startup must pin the reviewed vms.sh with actual post-stop memory guard")
    policy = read_json(verified_artifact(plan, policy_id))
    validate_policy(plan, policy)
    return plan, policy


def validate_policy(plan, policy):
    fields(policy, "schema host_address host_keys guest_keys host_kernel host_packages allowed_users max_sessions "
           "command_timeout_seconds max_load_1m disk_path disk_reserve_bytes allocations stopped", "policy")
    require(policy["schema"] == 1, "policy schema 1 required")
    ipaddress.ip_address(policy["host_address"])
    for key in ("host_kernel", "host_packages"):
        require(isinstance(policy[key], str) and policy[key], f"{key} evidence required")
    require(re.fullmatch(r"sha256:[0-9a-f]{64}", policy["host_packages"]), "host package manifest SHA required")
    require(isinstance(policy["allowed_users"], list) and all(
        isinstance(user, str) and re.fullmatch(r"[A-Za-z0-9_-]+", user)
        for user in policy["allowed_users"]), "explicit allowed login users required")
    positive(policy["max_sessions"], "max_sessions", zero=True)
    require(1 <= positive(policy["command_timeout_seconds"], "command timeout") <= 15,
            "command timeout must be at most 15 seconds")
    positive(policy["max_load_1m"], "max_load_1m")
    positive(policy["disk_reserve_bytes"], "disk reserve", zero=True)
    safe_path(policy["disk_path"])
    targets = {t["name"]: t for t in plan["targets"]}
    require(len(plan["targets"]) == 5 and set(targets) == NAMES, "exact reviewed five-target inventory required")
    require(set(policy["allocations"]) == NAMES, "exact reviewed VM allocations required")
    for name, target in targets.items():
        require(target["host"] == "f3" and target["initial_state"] in ("Running", "Stopped"),
                f"unsupported target: {name}")
        require(name == "rocky" or target["initial_state"] == "Stopped",
                "current Go Plan requires initially stopped lab/development guests")
        require(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.-]*", target["address"]),
                f"literal safe reviewed guest address required: {name}")
        if name.startswith("hfs-test"):
            require(target["address"] == f"172.31.253.{11 + int(name[-1])}", "native address mismatch")
        require(re.fullmatch(r"sha256:[0-9a-f]{64}", target["packages"]), "target package manifest SHA required")
        allocation = policy["allocations"][name]
        fields(allocation, "config config_sha256 memory_bytes cpus os_disk", f"allocation {name}")
        safe_path(allocation["config"])
        safe_path(allocation["os_disk"])
        sha(allocation["config_sha256"])
        positive(allocation["memory_bytes"], "VM memory")
        positive(allocation["cpus"], "VM CPUs")
        if name.startswith("hfs-test"):
            require(allocation["memory_bytes"] == 2 * GIB and allocation["cpus"] == 2,
                    "native guests require exactly 2 GiB/two CPUs each")
    require(plan["budget"]["guest_memory_bytes"] == 6 * GIB
            and plan["budget"]["host_headroom_bytes"] >= 4 * GIB
            and plan["budget"]["cpus"] == 6, "prepared resource budget mismatch")
    rocky_running = targets["rocky"]["initial_state"] == "Running"
    require(not rocky_running or plan["budget"]["host_headroom_bytes"] == 4 * GIB,
            "running-Rocky admission requires exactly 4 GiB host headroom supported by the post-stop guard")
    stopped = {name for name, t in targets.items() if t["initial_state"] == "Stopped"}
    require(set(policy["stopped"]) == stopped,
            "stopped guests require reviewed offline-inspected snapshot attestations; do not boot to collect evidence")
    for name, evidence in policy["stopped"].items():
        fields(evidence, "kind snapshot guid os_disk kernel packages provenance", f"stopped evidence {name}")
        require(evidence["kind"] == "offline-inspected-zfs-snapshot-v1", "unsupported stopped evidence kind")
        require(re.fullmatch(r"[A-Za-z0-9_/-]+@[A-Za-z0-9_.:-]+", evidence["snapshot"])
                and ".." not in evidence["snapshot"].split("/")
                and not evidence["snapshot"].startswith("/"), "safe ZFS snapshot required")
        require(re.fullmatch(r"[0-9]+", evidence["guid"]), "snapshot GUID required")
        require(evidence["os_disk"] == policy["allocations"][name]["os_disk"], "stopped OS disk evidence mismatch")
        require(evidence["kernel"] == targets[name]["kernel"]
                and evidence["packages"] == targets[name]["packages"], "stopped version evidence mismatch")
        require(isinstance(evidence["provenance"], str) and evidence["provenance"].strip(),
                "dated offline inspection provenance required")
        date.fromisoformat(evidence["provenance"][:10])


class Transport:
    """Bounded SSH subprocess owner; never spawns a remote mutating operation."""

    def __init__(self, host_keys, guest_keys, command_timeout, deadline, host_address):
        self.host_address = host_address
        self.host_keys = str(host_keys)
        self.guest_keys = str(guest_keys)
        self.command_timeout = command_timeout
        self.deadline = deadline

    def ssh(self, keys):
        return ["/usr/bin/ssh", "-F", "/dev/null", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
                "-o", "GlobalKnownHostsFile=/dev/null", "-o", f"UserKnownHostsFile={keys}",
                "-o", "UpdateHostKeys=no", "-o", "ControlMaster=no", "-o", "ControlPath=none",
                "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=2", "-o", "ServerAliveCountMax=2"]

    def host_ssh(self):
        return self.ssh(self.host_keys) + ["-o", "HostName=" + self.host_address, "-o", "HostKeyAlias=f3"]

    def __call__(self, destination, script):
        argv = self.host_ssh() if destination == "host" else self.ssh(self.guest_keys)
        if destination == "host":
            argv += ["paul@f3", "doas -n /bin/sh -s"]
        else:
            proxy = self.host_ssh() + ["-W", "%h:%p", "paul@f3"]
            argv += ["-o", "ProxyCommand=" + shlex.join(proxy), "root@" + destination, "/bin/sh -s"]
        end = min(self.deadline, time.monotonic() + self.command_timeout)
        require(end > time.monotonic(), "preflight total deadline exhausted")
        with subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=subprocess.STDOUT, start_new_session=True) as child:
            try:
                child.stdin.write(("set -eu\nexport LC_ALL=C\n" + script + "\n").encode())
                child.stdin.close()
                result = bytearray()
                with selectors.DefaultSelector() as selector:
                    selector.register(child.stdout, selectors.EVENT_READ)
                    while selector.get_map():
                        left = end - time.monotonic()
                        if left <= 0:
                            raise TimedOut(f"{destination}: read-only SSH timeout; evidence unavailable")
                        for key, _ in selector.select(min(left, 0.2)):
                            block = os.read(key.fd, 8192)
                            if not block:
                                selector.unregister(key.fileobj)
                                continue
                            result.extend(block)
                            if len(result) > LIMIT:
                                raise Uncertain(f"{destination}: inspection output exceeds {LIMIT} bytes; remote completion unknown")
                left = end - time.monotonic()
                if left <= 0:
                    raise TimedOut(f"{destination}: read-only SSH deadline")
                try:
                    code = child.wait(timeout=left)
                except subprocess.TimeoutExpired as error:
                    raise TimedOut(f"{destination}: read-only SSH deadline") from error
                if code == 255 or code < 0:
                    raise Uncertain(f"{destination}: SSH transport lost ({code}); remote completion unknown; "
                                    + result[:1024].decode(errors="replace"))
                require(code == 0, f"{destination}: confirmed remote inspection failure ({code}); "
                        + result[:1024].decode(errors="replace"))
                return result.decode("utf-8", errors="strict")
            except OSError as error:
                raise Uncertain(f"{destination}: local SSH I/O failed; remote completion unknown: {error}") from error
            finally:
                if child.poll() is None:
                    try:
                        os.killpg(child.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    except OSError as error:
                        raise Uncertain(f"{destination}: local SSH cleanup failed; remote completion unknown: {error}") from error
                    child.wait()


def inventory(text):
    rows = text.strip().splitlines()
    require(rows and rows[0].split() == ["NAME", "DATASTORE", "LOADER", "CPU", "MEMORY", "VNC", "AUTO", "STATE"],
            "unrecognized complete vm list header")
    result = {}
    for row in rows[1:]:
        match = re.fullmatch(r"(\S+)\s+\S+\s+\S+\s+([0-9]+)\s+(\S+)\s+\S+\s+"
                             r"(?:No|Yes(?:\s+\[[0-9]+\])?)\s+(Stopped|Running)(?:\s+\(([0-9]+)\))?", row)
        require(match is not None, f"unrecognized/unknown/transitional VM row: {row}")
        name, cpus, allocation, state, pid = match.groups()
        require(name not in result, "duplicate live VM")
        require((state == "Stopped" and pid is None) or (state == "Running" and pid is not None),
                f"ambiguous VM state: {name}")
        result[name] = {"state": state, "cpus": number(cpus), "memory": memory(allocation)}
    require(result, "empty VM inventory")
    return result


def number(text):
    require(re.fullmatch(r"[0-9]+", text.strip()), f"nonnegative integer evidence required: {text!r}")
    return int(text.strip())


def memory(text):
    match = re.fullmatch(r"([1-9][0-9]*)([KMGT]?)", text)
    require(match is not None, f"unrecognized memory allocation: {text}")
    return int(match[1]) * (1024 ** ("KMGT".index(match[2]) + 1) if match[2] else 1)


def compare_inventory(plan, policy, live):
    for name, row in live.items():
        require(name in NAMES or row["state"] == "Stopped", f"additional running VM: {name}")
    for target in plan["targets"]:
        name = target["name"]
        require(name in live and live[name]["state"] == target["initial_state"],
                f"initial-state mismatch/missing VM: {name}")
        allocation = policy["allocations"][name]
        require(live[name]["memory"] == allocation["memory_bytes"]
                and live[name]["cpus"] == allocation["cpus"], f"live VM allocation changed: {name}")


def swap_used(text):
    rows = text.strip().splitlines()
    require(rows and rows[0].split() == ["Device", "1K-blocks", "Used", "Avail", "Capacity"],
            "unrecognized swapinfo header")
    used = 0
    for row in rows[1:]:
        cells = row.split()
        require(len(cells) == 5 and cells[0] != "Total", "unrecognized swapinfo device row")
        used += number(cells[2]) * 1024
    return used


def disk_available(text, dataset=None):
    rows = text.strip().splitlines()
    require(len(rows) == 2 and rows[0].split() == ["Filesystem", "1024-blocks", "Used", "Avail", "Capacity", "Mounted", "on"],
            "unrecognized POSIX df -kP output")
    cells = rows[1].split()
    require(len(cells) == 6 and (dataset is None or cells[0] == dataset), "disk backing dataset mismatch")
    return number(cells[3]) * 1024


def key_present(path, address):
    for line in path.read_text().splitlines():
        if line and not line.startswith("#"):
            parts = line.split()
            if len(parts) >= 3 and address in parts[0].split(",") and parts[1] == "ssh-ed25519":
                return True
    return False


HOST_METRICS = """sysctl -n hw.physmem hw.ncpu hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count vm.stats.vm.v_free_target vm.stats.vm.v_swappgsin vm.stats.vm.v_swappgsout
sysctl -n vm.loadavg"""
PKG_FREEBSD = "pkg query -a '%n-%v'"
PKG_LINUX = "rpm -qa --qf '%{NAME}-%{VERSION}-%{RELEASE}.%{ARCH}\\n'"


def package_hash(text):
    rows = text.splitlines()
    require(rows and all(re.fullmatch(r"[A-Za-z0-9_+.,:~%-]+", row) for row in rows),
            "nonempty complete package manifest required")
    canonical = "\n".join(sorted(rows)) + "\n"
    return "sha256:" + hashlib.sha256(canonical.encode()).hexdigest()


def config_values(text):
    values = {}
    for line in text.splitlines():
        match = re.match(r"^\s*(memory|cpu|disk0_name)\s*=", line)
        if not match:
            continue
        key = match[1]
        require(key not in values, f"duplicate VM config field: {key}")
        tokens = shlex.split(line, comments=True)
        require(len(tokens) == 1 and tokens[0].startswith(key + "="), f"unsupported VM config literal: {key}")
        value = tokens[0][len(key) + 1:]
        require(re.fullmatch(r"[A-Za-z0-9_./-]+", value), f"unsafe VM config literal: {key}")
        values[key] = value
    require(set(values) == {"memory", "cpu", "disk0_name"}, "missing VM config allocation/OS disk")
    return values


def metrics(text):
    rows = text.strip().splitlines()
    require(len(rows) == 9, "missing memory/pressure metrics")
    values = [number(row) for row in rows[:8]]
    require(values[0] > 0 and values[1] > 0 and values[2] > 0, "invalid host hardware metrics")
    match = re.fullmatch(r"\{ ([0-9.]+) ([0-9.]+) ([0-9.]+) \}", rows[8])
    require(match is not None, "unrecognized host load average")
    return values, float(match[1])


def check_metrics(plan, policy, text):
    values, load = metrics(text)
    physical, cpus, page, free, inactive, target, _, _ = values
    needed = plan["budget"]["guest_memory_bytes"] + plan["budget"]["host_headroom_bytes"]
    require(physical >= needed, "inadequate host physical memory for guests + headroom")
    rocky_running = any(t["name"] == "rocky" and t["initial_state"] == "Running" for t in plan["targets"])
    if not rocky_running:
        require((free + inactive) * page >= needed,
                "inadequate current free+inactive host memory for guests + headroom")
    # Running Rocky has not released its RAM. Exact inventory excludes all other
    # live VMs; the pinned lifecycle helper checks actual availability after stop.
    require(cpus >= 2 and free >= target and load <= policy["max_load_1m"], "host memory/CPU pressure exceeds policy")
    return values


def inspect_stopped(name, evidence, allocation, call):
    snapshot = evidence["snapshot"]
    dataset, tag = snapshot.split("@")
    q = shlex.quote
    guid = call("host", f"zfs get -Hp -o value guid {q(snapshot)}").strip()
    require(guid == evidence["guid"], f"stopped snapshot missing/changed: {name}")
    written = call("host", f"zfs get -Hp -o value {q('written@' + tag)} {q(dataset)}")
    require(number(written) == 0, f"stopped snapshot no longer matches VM backing: {name}")
    mountpoint = call("host", f"zfs get -Hp -o value mountpoint {q(dataset)}").strip()
    safe_path(mountpoint)
    require(allocation["os_disk"].startswith(mountpoint.rstrip("/") + "/"),
            f"OS disk outside attested dataset: {name}")
    script = f"test -f {q(allocation['os_disk'])}; test ! -L {q(allocation['os_disk'])}; df -kP {q(allocation['os_disk'])}"
    disk_available(call("host", script), dataset)
    return {"kind": evidence["kind"], "snapshot": snapshot, "guid": guid,
            "kernel_source": "reviewed offline image inspection; not a live guest kernel observation"}


def run_checks(plan, policy, call):
    live = inventory(call("host", "vm list"))
    compare_inventory(plan, policy, live)
    users = call("host", "who").strip().splitlines()
    require(len(users) <= policy["max_sessions"] and all(
        len(row.split()) >= 4 and row.split()[0] in policy["allowed_users"] for row in users),
        "active login sessions exceed reviewed user policy")
    before = check_metrics(plan, policy, call("host", HOST_METRICS))
    require(swap_used(call("host", "swapinfo -k")) == 0,
            "host swap is in use; fixed vms.sh admission requires zero used swap")
    capacity = disk_available(call("host", "df -kP " + shlex.quote(policy["disk_path"])))
    require(capacity >= plan["budget"]["disk_bytes"] + policy["disk_reserve_bytes"], "inadequate host disk capacity")
    require(call("host", "uname -r").strip() == policy["host_kernel"], "host kernel version changed")
    packages = call("host", PKG_FREEBSD)
    require(package_hash(packages) == policy["host_packages"],
            "host package manifest changed")
    versions = {}
    for target in plan["targets"]:
        name = target["name"]
        allocation = policy["allocations"][name]
        q = shlex.quote
        config = q(allocation["config"])
        script = f"test -f {config}; test ! -L {config}; cat {config}"
        content = call("host", script)
        require(hashlib.sha256(content.encode()).hexdigest() == allocation["config_sha256"],
                f"VM config changed: {name}")
        values = config_values(content)
        require(memory(values["memory"]) == allocation["memory_bytes"]
                and number(values["cpu"]) == allocation["cpus"], f"VM allocation changed: {name}")
        disk = values["disk0_name"]
        disk = disk if disk.startswith("/") else str(PurePosixPath(allocation["config"]).parent / disk)
        require(disk == allocation["os_disk"], f"VM OS disk binding changed: {name}")
        if target["initial_state"] == "Stopped":
            versions[name] = inspect_stopped(name, policy["stopped"][name], allocation, call)
        else:
            require(call(target["address"], "uname -r").strip() == target["kernel"], f"live guest kernel changed: {name}")
            packages = call(target["address"], PKG_LINUX if name == "rocky" else PKG_FREEBSD)
            require(package_hash(packages) == target["packages"],
                    f"live guest package manifest changed: {name}")
            versions[name] = {"kind": "live-pinned-ssh", "kernel": target["kernel"]}
    after = check_metrics(plan, policy, call("host", HOST_METRICS))
    require(after[6:] == before[6:], "active host swap paging observed during admission")
    require(swap_used(call("host", "swapinfo -k")) == 0, "host swap use changed during admission")
    compare_inventory(plan, policy, inventory(call("host", "vm list")))
    return {"contract": CONTRACT, "run_id": plan["run_id"], "status": "admitted",
            "inventory": live, "active_sessions": len(users), "host_disk_available_bytes": capacity,
            "version_evidence": versions,
            "memory_admission": "physical/current-pressure preflight; actual free+inactive checked again by pinned start-lab helper",
            "scope": "read-only admission; not a native test pass"}


def execute(policy_id, env, transport_factory=Transport):
    plan, policy = load_context(policy_id, env)
    host_keys = verified_artifact(plan, policy["host_keys"])
    guest_keys = verified_artifact(plan, policy["guest_keys"])
    require(key_present(host_keys, "f3"), "pinned f3 ed25519 SSH key required")
    require(REPO_KEYS.is_file(), "repository native guest SSH pins missing")
    for i in range(3):
        address = f"172.31.253.{11 + i}"
        require(key_present(guest_keys, address), f"native guest SSH pin missing: {address}")
        expected = [line.split()[1:3] for line in REPO_KEYS.read_text().splitlines()
                    if line and address in line.split()[0].split(",")]
        actual = [line.split()[1:3] for line in guest_keys.read_text().splitlines()
                  if line and address in line.split()[0].split(",")]
        require(actual == expected and len(expected) == 1, f"repository native SSH pin changed: {address}")
    for target in plan["targets"]:
        if target["initial_state"] == "Running":
            require(key_present(guest_keys, target["address"]), "running guest pinned SSH key missing")
    call = transport_factory(host_keys, guest_keys, policy["command_timeout_seconds"],
                             time.monotonic() + plan["steps"][0]["timeout_seconds"] - 5, policy["host_address"])
    return run_checks(plan, policy, call)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--policy-artifact", required=True, help="ID of reviewed pinned admission policy in canonical Plan")
    args = parser.parse_args()
    def interrupted(signum, _frame):
        raise TimedOut(f"read-only admission interrupted by signal {signum}")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        print(json.dumps(execute(args.policy_artifact, os.environ), sort_keys=True))
        return 0
    except (Blocked, OSError, ValueError, KeyError, IndexError, TypeError) as error:
        uncertain = isinstance(error, Uncertain)
        print(json.dumps({"contract": CONTRACT, "status": "uncertain" if uncertain else "blocked", "error": str(error),
                          "scope": "read-only admission; no lifecycle or workload requested",
                          "remote_completion": "unknown; operator reconciliation required" if uncertain else "terminal"}),
              file=sys.stderr)
        # Unrecognized nonzero preserves the runner's uncertainty/restoration gate.
        # Reaping local SSH never proves its remote shell and descendants exited.
        return 1 if uncertain else 20


if __name__ == "__main__":
    sys.exit(main())
