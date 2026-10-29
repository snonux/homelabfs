#!/usr/bin/env python3
"""Pinned Stage 1 driver; the existing hfs-lab runner owns approval/lifecycle."""

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shlex
import struct
import subprocess
import sys
import tarfile
import time
import uuid
import xml.etree.ElementTree as ET

REPO = Path(__file__).resolve().parents[2]
ENTRY = REPO / "tools/lab/check-native-single-node.sh"
GUEST = REPO / "tools/lab/native-single-node-guest.sh"
KEYS = REPO / "docs/lab/f3-known_hosts"
PHASE_SECONDS = {"setup": 900, "test": 2400, "collect": 180, "cleanup": 180}
MATRIX = [(role, profile) for role in ("root", "unprivileged")
          for profile in ("hfs-direct", "hfs-buffered")]
CASE_NAMES = {
    "bind", "stop-mutating-peers", "stop-owned-helpers",
    "close-cleanup-lock", "cleanup-data", "normal-unmount",
    "mount-loss-no-fallback", "create-exclusive-rename-replace-hardlink-unlink-open-truncate",
    "umask-chmod-chown-setgid", "sync-open-fsync-fdatasync-directory-sync",
    "process-fcntl-flock-contention-exit", "blocked-lock-process-interruption",
    "concurrent-append", "mmap-msync-readback",
    "multiuser-groups-sticky-permission-cache",
}


class Refusal(Exception):
    """No operation was started, or a confirmed terminal assertion failed."""


class TerminalTimeout(Exception):
    """The workload timed out, then guest guards proved terminal quiescence."""


class Uncertain(Exception):
    """A remote resource/action needs inspection before lifecycle restoration."""


def read_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise Refusal(f"duplicate JSON field: {key}")
            result[key] = value
        return result
    data = Path(path).read_bytes()
    if len(data) > 4 << 20:
        raise Refusal("JSON input exceeds 4 MiB")
    return json.loads(data, object_pairs_hook=unique)


def digest(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def file_info(path, name, binary=False, inspect=False):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise Refusal(f"require nonsymlink regular file: {path}")
    path = path.resolve(strict=True)
    size = path.stat().st_size
    if binary:
        if not 0 < size <= 128 << 20:
            raise Refusal("binary exceeds 128 MiB")
        with path.open("rb") as source:
            header = source.read(64)
        if (len(header) != 64 or header[:9] != b"\x7fELF\x02\x01\x01\x09\x00"
                or struct.unpack_from("<H", header, 18)[0] != 62
                or struct.unpack_from("<H", header, 16)[0] not in (2, 3)):
            raise Refusal(f"{name}: require FreeBSD amd64 ELF executable")
    result = {"name": name, "path": str(path), "sha256": digest(path), "bytes": size}
    if inspect:
        metadata = subprocess.run(["go", "version", "-m", str(path)], check=True,
                                  capture_output=True, text=True, timeout=10).stdout
        fields = [line.strip().split("\t") for line in metadata.splitlines()[1:]]
        for pair in (["path", f"github.com/snonux/homelabfs/cmd/{name}"],
                     ["build", "GOOS=freebsd"], ["build", "GOARCH=amd64"],
                     ["build", "CGO_ENABLED=0"]):
            if pair not in fields:
                raise Refusal(f"wrong Go binary command/platform: {name}")
        result["go_build_metadata"] = metadata
    return result


def specification(run, daemon, helper, inspect=True):
    if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}", run):
        raise Refusal("invalid bounded run ID")
    work = f"/var/tmp/hfs-run-{run}"
    socket = work + "/c/node/admin-" + "1" * 36 + ".sock"
    if len(socket.encode()) + 1 > 104:
        raise Refusal(f"control socket needs {len(socket.encode()) + 1} bytes; FreeBSD permits 104")
    artifacts = [file_info(daemon, "homelabfs", True, inspect),
                 file_info(helper, "hfs-behavior", True, inspect)]
    artifacts += [file_info(path, name) for name, path in (
        ("native-entry", ENTRY), ("native-driver", Path(__file__)),
        ("native-guest", GUEST), ("native-host-keys", KEYS))]
    resources = []
    for i in range(3):
        resources.append({"name": f"native-dir-{i}", "kind": "directory",
                          "target": f"hfs-test{i}", "location": work,
                          "max_bytes": 320 << 20})
        for kind in ("data", "journal", "control"):
            resources.append({"name": f"native-{kind}-{i}", "kind": "dataset",
                              "target": f"hfs-test{i}",
                              "location": f"hfsdata{i}/{kind}/hfs-run-{run}",
                              "max_bytes": 128 << 20})
    return {
        "schema": 1, "contract": "native-single-node-v1", "run_id": run,
        "artifacts": artifacts, "resources": resources,
        "evidence_dir": str(REPO / "docs/lab/results" / run),
        "run_root": work, "socket_bytes_including_nul": len(socket.encode()) + 1,
        "matrix": [{"role": role, "profile": profile, "initialize": n == 0}
                   for n, (role, profile) in enumerate(MATRIX)],
        "phase_seconds": PHASE_SECONDS,
        "evidence_bytes": 32 << 20, "logical_data_bytes_per_volume": 4 << 20,
        "retention_hours": 168,
        "supervisor": {"seconds": 95, "term_grace_seconds": 10,
                       "file_bytes": 262144, "open_files": 512,
                       "on_failure": "TERM-owned-child-once; preserve; never KILL/abort/force-unmount"},
        "scope": "one local volume per guest; four sequential controlled roles/profiles; no replication/9p/production",
    }


def validate_outer(spec_path, phase):
    """Consume the existing runner boundary, never mint/replace its approval."""
    spec = read_json(spec_path)
    if not re.fullmatch(r"[0-9a-f]{64}", os.environ.get("HFS_LAB_PLAN_HASH", "")):
        raise Refusal("driver requires the approved hfs-lab runner environment")
    outer_path = os.environ.get("HFS_LAB_PLAN", "")
    if not outer_path:
        raise Refusal("HFS_LAB_PLAN is missing")
    outer = read_json(outer_path)
    if (outer.get("stage") != "stage1" or outer.get("schema") != 1
            or not outer.get("prepared_guests") or not outer.get("preserve_failures")
            or outer.get("run_id") != spec.get("run_id")
            or os.environ.get("HFS_LAB_RUN_ID") != spec.get("run_id")
            or outer.get("evidence_dir") != spec.get("evidence_dir")):
        raise Refusal("outer plan identity, stage or failure/evidence policy mismatch")
    now = time.time()
    if phase != "cleanup":
        from datetime import datetime
        created = datetime.fromisoformat(outer["created"].replace("Z", "+00:00")).timestamp()
        expires = datetime.fromisoformat(outer["expires"].replace("Z", "+00:00")).timestamp()
        if not created <= now <= expires:
            raise Refusal("expired/future outer plan")
    binaries = {a["name"]: a for a in spec["artifacts"]}
    expected = specification(spec["run_id"], binaries["homelabfs"]["path"],
                             binaries["hfs-behavior"]["path"], inspect=False)
    # Embedded build metadata was inspected offline and is hash-pinned by the
    # outer plan. It is informational; execution never needs Go on PATH.
    stripped = json.loads(json.dumps(spec))
    for item in stripped["artifacts"]:
        item.pop("go_build_metadata", None)
    if stripped != expected:
        raise Refusal("driver specification or pinned artifact bytes changed")
    pins = {(a["path"], a["sha256"]) for a in outer["artifacts"]}
    for item in spec["artifacts"] + [file_info(spec_path, "native-spec")]:
        if (item["path"], item["sha256"]) not in pins:
            raise Refusal(f"outer plan does not pin {item['name']}")
    resources = {(r["kind"], r["target"], r["location"], r["max_bytes"])
                 for r in outer["resources"]}
    if not all((r["kind"], r["target"], r["location"], r["max_bytes"]) in resources
               for r in spec["resources"]):
        raise Refusal("outer plan must enroll every exact resource and byte cap")
    budget = outer["budget"]
    if (budget["artifact_bytes"] < spec["evidence_bytes"]
            or budget["disk_bytes"] < sum(r["max_bytes"] for r in spec["resources"])
            or budget["io_bytes"] < 12 * spec["logical_data_bytes_per_volume"]
            or budget["retention_hours"] != spec["retention_hours"]):
        raise Refusal("outer budget does not cover driver storage/evidence/I/O/retention")
    # The runner validates/hash-binds the complete Plan and checks fresh explicit
    # Approval before invoking us. Require the exact pinned argv/contracts too.
    by_phase = {}
    artifact_ids = {a["id"] for a in outer["artifacts"] if a["path"] == str(ENTRY)}
    for name in PHASE_SECONDS:
        args = ["--driver", name, str(Path(spec_path).resolve())]
        matches = [s for s in outer["steps"] if s["action"] == "driver"
                   and s["artifact"] in artifact_ids and s["args"] == args
                   and s["contract"] == f"native-single-node-v1:{name}"
                   and s["phase"] == name]
        if (len(matches) != 1 or matches[0]["timeout_seconds"] < PHASE_SECONDS[name]
                or matches[0]["effects"] != ("run-owned" if name in ("setup", "test") else "read-only")
                or (name in ("setup", "test") and matches[0]["max_attempts"] != 1)):
            raise Refusal(f"outer plan requires exact {name} driver step/deadline")
        by_phase[name] = matches[0]
    if not {by_phase["setup"]["id"], by_phase["test"]["id"]}.issubset(
            by_phase["cleanup"]["applies_after"]):
        raise Refusal("outer cleanup must apply after both mutating driver steps")
    test = by_phase["test"]["test"]
    if test != "native-single-node" or not any(t["name"] == test for t in outer["tests"]):
        raise Refusal("outer plan must select native-single-node")
    return spec


def outer_fragment(spec_path):
    """Offline inputs to merge into a fully reviewed existing Stage 1 Plan."""
    source = str(Path(spec_path).resolve())
    spec = read_json(source)
    artifacts = [{"id": a["name"], "path": a["path"],
                  "version": "sha256:" + a["sha256"], "sha256": a["sha256"]}
                 for a in spec["artifacts"]]
    artifacts.append({"id": "native-spec", "path": source,
                      "version": "native-single-node-v1", "sha256": digest(source)})
    steps = []
    for phase, seconds in PHASE_SECONDS.items():
        steps.append({"id": "native-" + phase, "phase": phase,
                      "test": "native-single-node" if phase == "test" else "",
                      "action": "driver", "artifact": "native-entry",
                      "args": ["--driver", phase, source],
                      "contract": "native-single-node-v1:" + phase,
                      "effects": "run-owned" if phase in ("setup", "test") else "read-only",
                      "timeout_seconds": seconds, "max_attempts": 1,
                      "applies_after": ["native-setup", "native-test"] if phase == "cleanup" else [],
                      "idempotent": phase == "cleanup"})
    return {"artifacts": artifacts, "resources": spec["resources"],
            "tests": [{"name": "native-single-node", "seed": 0}], "steps": steps,
            "evidence_dir": spec["evidence_dir"], "preserve_failures": True}


HOST_CHECK = r'''set -eu
test "$(doas -n sysctl -n hw.physmem)" -ge 10737418240
test "$(doas -n swapinfo -k | awk 'NR>1 {used+=$3} END {print used+0}')" -eq 0
inventory=$(doas -n vm list)
printf '%s\n' "$inventory"
printf '%s\n' "$inventory" | awk '
NR>1 && $1 ~ /^hfs-test[012]$/ {if (seen[$1]++ || $(NF-1)!="Running" || $4!=2 || $5!="2G") exit 1; count++; next}
NR>1 && $NF!="Stopped" {exit 1}
END {if (count!=3) exit 1}'
for i in 0 1 2; do
  config="/zroot/bhyve/homelabfs-lab/hfs-test$i/hfs-test$i.conf"
  test "$(doas -n sysrc -f "$config" -n memory)" = 2G
done
'''


class Driver:
    def __init__(self, spec, phase, execute=None):
        self.spec, self.phase = spec, phase
        self.hash = os.environ["HFS_LAB_PLAN_HASH"]
        self.pins = {a["name"]: a for a in spec["artifacts"]}
        self.evidence = Path(spec["evidence_dir"]) / "native-single-node"
        self.execute = execute or subprocess.run
        self.deadline = time.monotonic() + PHASE_SECONDS[phase] - 10
        self.contacted = False
        self.opts = ["-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                     "-o", "StrictHostKeyChecking=yes", "-o", "ServerAliveInterval=5",
                     "-o", "ServerAliveCountMax=2", "-o", f"UserKnownHostsFile={KEYS}",
                     "-J", "paul@f3"]

    def command(self, argv, data=None, seconds=130, terminal_assertion=False):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise Uncertain("phase deadline reached; inspect remote state")
        self.contacted = True
        try:
            result = self.execute(argv, input=data, capture_output=True,
                                  timeout=min(seconds, remaining), check=False)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise Uncertain(f"transport/deadline failure: {error}") from error
        if result.returncode:
            detail = result.stderr.decode(errors="replace")[-4096:]
            # A workload assertion with proven idle remote state is terminal.
            if terminal_assertion and result.returncode == 10:
                raise Refusal(f"native behavior failed: {detail}")
            if terminal_assertion and result.returncode == 30:
                raise TerminalTimeout(f"native behavior timed out after confirmed shutdown: {detail}")
            raise Uncertain(f"remote/transport exit {result.returncode}: {detail}")
        return result.stdout

    def guest(self, index, action, *args):
        if digest(GUEST) != self.pins["native-guest"]["sha256"]:
            raise Uncertain("guest script changed during execution")
        command = shlex.join(["sh", "-s", "--", action, self.spec["run_id"], str(index),
                              self.hash, self.pins["homelabfs"]["sha256"],
                              self.pins["hfs-behavior"]["sha256"], *args])
        return self.command(["ssh", *self.opts, f"root@172.31.253.{11+index}", command],
                            GUEST.read_bytes(),
                            {"preflight": 30, "reserve": 20, "setup": 60,
                             "run": 120, "collect": 30, "cleanup": 30}[action],
                            terminal_assertion=action == "run")

    def host(self):
        return self.command(["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                             "-o", "StrictHostKeyChecking=yes", "paul@f3", "sh -s"],
                            HOST_CHECK.encode(), 30)

    def save(self, index, name, data):
        target = self.evidence / f"hfs-test{index}" / name
        target.parent.mkdir(parents=True, exist_ok=True)
        if target.exists():
            if target.read_bytes() != data:
                raise Uncertain(f"collected evidence changed: {target}")
            return
        with target.open("xb") as output:
            output.write(data)
        if sum(p.stat().st_size for p in self.evidence.rglob("*") if p.is_file()) > self.spec["evidence_bytes"]:
            raise Uncertain("approved evidence byte cap exceeded")

    def collect(self, index):
        data = self.guest(index, "collect")
        if len(data) > 10 << 20:
            raise Uncertain("guest evidence archive exceeds 10 MiB")
        files = {}
        with tarfile.open(fileobj=io.BytesIO(data)) as archive:
            for member in archive:
                if member.isdir() and member.name == "evidence":
                    continue
                if (not member.isfile() or not re.fullmatch(r"evidence/[a-zA-Z0-9_.-]+", member.name)
                        or not 0 <= member.size <= 1 << 20):
                    raise Uncertain("unsafe/oversized guest evidence member")
                name = member.name.split("/")[1]
                if name in files:
                    raise Uncertain("duplicate guest evidence member")
                files[name] = archive.extractfile(member).read()
                self.save(index, name, files[name])
        return files

    def run(self):
        if self.phase == "setup":
            self.evidence.mkdir(parents=False, exist_ok=False)
            self.host()
            for i in range(3):
                self.save(i, "preflight.txt", self.guest(i, "preflight"))
            for i in range(3):
                self.guest(i, "reserve")
                for name in ("homelabfs", "hfs-behavior"):
                    artifact = self.pins[name]
                    if digest(artifact["path"]) != artifact["sha256"]:
                        raise Uncertain("binary changed before transfer")
                    self.command(["scp", *self.opts, artifact["path"],
                                  f"root@172.31.253.{11+i}:{self.spec['run_root']}/bin/{name}"], seconds=60)
                self.guest(i, "setup")
        elif self.phase == "test":
            for i in range(3):
                seen = {"node": None, "volume": None, "generations": set(), "sessions": set()}
                for n, (role, profile) in enumerate(MATRIX):
                    self.host()
                    files = None
                    try:
                        self.guest(i, "run", role, profile, "yes" if n == 0 else "no")
                    finally:
                        # Best effort diagnostics even after a workload failure;
                        # an uncertain transport remains uncertain.
                        files = self.collect(i)
                    validate_reports(files, self.spec, role, profile, seen)
        elif self.phase == "collect":
            for i in range(3):
                self.collect(i)
        else:
            for i in range(3):
                data = self.guest(i, "cleanup")
                self.save(i, "cleanup-" + hashlib.sha256(data).hexdigest()[:16] + ".txt", data)
        print(f"native {self.phase}: completed; preserved roots {self.spec['run_root']}")


def canonical_uuid(value):
    if not isinstance(value, str) or str(uuid.UUID(value)) != value or uuid.UUID(value).int == 0:
        raise Refusal("invalid mounted identity UUID")
    return value


def validate_reports(files, spec, role, profile, seen):
    key = role + "-" + profile
    ready = json.loads(files[key + ".stdout"])
    report = json.loads(files[key + ".json"])
    status = json.loads(files[key + ".exit.json"])
    uid, groups = (0, []) if role == "root" else (20001, [20002])
    work = spec["run_root"]
    expected = {"role": role, "profile": profile, "daemon_uid": 0,
                "client_uid": uid, "client_gid": uid, "client_groups": groups,
                "normal_publication": False, "mountpoint": work + "/m",
                "data_root": work + "/d", "journal_root": work + "/j",
                "control_root": work + "/c",
                "behavior_sha256": spec["artifacts"][1]["sha256"]}
    if any(ready.get(k) != v for k, v in expected.items()):
        raise Refusal("wrong readiness roots/profile/credentials/helper digest")
    if status.get("exit_code") != 0 or status.get("timed_out") is not False or status.get("preserved_live_process") is not False:
        raise Refusal("fixture did not finish within its normal lifecycle")
    volume = canonical_uuid(ready["volume_id"])
    generation, session = canonical_uuid(ready["generation"]), canonical_uuid(ready["session"])
    if seen["volume"] not in (None, volume) or generation in seen["generations"] or session in seen["sessions"]:
        raise Refusal("replay replaced volume or reused generation/session")
    identity = f"homelabfs-validation-v1\n{volume}\n{generation}\n{session}\n"
    markers = [json.loads(files[key + f".marker-{kind}.json"])
               for kind in ("data", "journal", "control")]
    marker = markers[0]
    node = canonical_uuid(marker["node_id"])
    if (any(item != marker for item in markers[1:])
            or marker.get("format_version") != 1 or marker.get("volume_id") != volume
            or seen["node"] not in (None, node)
            or any(marker.get(field) != expected[field]
                   for field in ("data_root", "journal_root", "control_root"))):
        raise Refusal("changed node/volume or inconsistent three-root fixture markers")
    if (report.get("expected_identity") != identity or report.get("profile") != profile
            or report.get("target") != work + "/m" or report.get("platform") != "freebsd/amd64"):
        raise Refusal("wrong report mount identity/platform")
    fixture = report.get("fixture", {})
    for field in ("role", "daemon_uid", "client_uid", "client_gid", "data_root", "journal_root", "control_root", "behavior_sha256"):
        if fixture.get(field) != str(expected[field]):
            raise Refusal(f"wrong report fixture field: {field}")
    expected_groups = "[]" if role == "root" else "[20002]"
    if fixture.get("client_groups") != expected_groups:
        raise Refusal("wrong report helper supplementary groups")
    paths = {"source": work + "/j/volume/source", "reservations": work + "/d/volume/reservations",
             "workspace_data": work + f"/d/volume/workspaces/{generation}/data",
             "workspace_journal": work + f"/j/volume/workspaces/{generation}/journal",
             "workspace_progress": work + f"/c/workspaces/{generation}/progress"}
    if any(fixture.get(k) != v for k, v in paths.items()):
        raise Refusal("report did not use actual three-dataset mapping")
    skips = {"mmap-msync-readback" if profile == "hfs-direct" else "concurrent-append"}
    if (role, profile) != ("root", "hfs-buffered"):
        skips.add("multiuser-groups-sticky-permission-cache")
    results = report.get("results", [])
    by_name = {r["name"]: r for r in results}
    if set(by_name) != CASE_NAMES or len(by_name) != len(results):
        raise Refusal("missing/duplicate/unexpected syscall cases")
    for name, case in by_name.items():
        expected_status = "skip" if name in skips else "pass"
        if case.get("status") != expected_status or (expected_status == "skip" and not case.get("detail")):
            raise Refusal(f"required case did not pass or expected skip missing: {name}")
    caps = report.get("declared_capabilities", {})
    expected_caps = {"direct_io": profile == "hfs-direct", "mmap": profile == "hfs-buffered",
                     "default_permissions": True, "local_locks": True, "file_sync": True,
                     "directory_sync": True, "normal_publication": False,
                     "arbitrary_credentials": False, "writeback": False}
    if (caps != expected_caps or not isinstance(caps, dict)
            or any(type(value) is not bool for value in caps.values())):
        raise Refusal("wrong controlled capabilities")
    suite = ET.fromstring(files[key + ".xml"])
    xml_cases = {c.attrib["name"]: c for c in suite.findall("testcase")}
    if (suite.tag != "testsuite" or suite.get("failures") != "0"
            or suite.get("errors", "0") != "0" or suite.find(".//error") is not None
            or suite.get("tests") != str(len(results)) or suite.get("skipped") != str(len(skips))
            or set(xml_cases) != CASE_NAMES or len(suite.findall("testcase")) != len(results)):
        raise Refusal("JUnit case coverage disagrees with JSON")
    for name, case in xml_cases.items():
        if case.find("failure") is not None or case.find("error") is not None or (case.find("skipped") is not None) != (name in skips):
            raise Refusal("JUnit result disagrees with JSON")
    property_list = suite.findall("properties/property")
    properties = {p.get("name"): p.get("value") for p in property_list}
    text_fields = ("target", "profile", "platform", "scope", "run_directory", "expected_identity")
    if (set(properties) != set(text_fields) | {"declared_capabilities", "fixture"}
            or len(property_list) != len(properties)):
        raise Refusal("JUnit report metadata is missing/duplicate/unexpected")
    for field in text_fields:
        if not isinstance(report.get(field), str) or not report[field] or properties[field] != report[field]:
            raise Refusal(f"JUnit {field} disagrees with JSON")
    xml_caps = json.loads(properties["declared_capabilities"])
    if (xml_caps != caps or not isinstance(xml_caps, dict)
            or any(type(value) is not bool for value in xml_caps.values())
            or json.loads(properties["fixture"]) != fixture):
        raise Refusal("JUnit fixture/capabilities disagree with JSON")
    seen["volume"] = volume
    seen["node"] = node
    seen["generations"].add(generation)
    seen["sessions"].add(session)


def main():
    parser = argparse.ArgumentParser(description=__doc__, epilog="Native execution requires fresh explicit approval of the outer hfs-lab Plan; flags/environment are not consent. See docs/Native-single-node.md.")
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument("--dry-run", nargs=3, metavar=("RUN_ID", "HOMELABFS", "HFS_BEHAVIOR"), help="offline pinned driver subplan JSON; never SSH or execute artifacts")
    modes.add_argument("--outer-fragment", metavar="SUBPLAN", help="offline artifact/resource/step fragment to merge into the existing hfs-lab Plan")
    modes.add_argument("--driver", nargs=2, metavar=("PHASE", "SUBPLAN"), help="setup|test|collect|cleanup; only invoked by approved hfs-lab")
    args = parser.parse_args()
    contacted = False
    driver = None
    try:
        if args.dry_run:
            run, daemon, helper = args.dry_run
            if os.path.lexists(REPO / "docs/lab/results" / run):
                raise Refusal("run ID already exists locally")
            print(json.dumps(specification(run, daemon, helper), indent=2))
            return 0
        if args.outer_fragment:
            print(json.dumps(outer_fragment(args.outer_fragment), indent=2))
            return 0
        phase, source = args.driver
        if phase not in PHASE_SECONDS:
            raise Refusal("unsupported driver phase")
        spec = validate_outer(str(Path(source).resolve()), phase)
        driver = Driver(spec, phase)
        driver.run()
        return 0
    except TerminalTimeout as error:
        print(f"native gate timeout: {error}", file=sys.stderr)
        return 30
    except Refusal as error:
        contacted = driver is not None and driver.contacted
        print(f"native gate refused/failed: {error}", file=sys.stderr)
        return 10 if contacted else 20
    except (Uncertain, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, tarfile.TarError, ET.ParseError) as error:
        print(f"native gate incomplete: {error}; preserve resources; inspect guest run roots, processes, mounts and local evidence", file=sys.stderr)
        # Unknown nonzero exit deliberately blocks automatic lifecycle restoration.
        return 1


if __name__ == "__main__":
    sys.exit(main())
