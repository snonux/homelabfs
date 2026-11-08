#!/usr/local/bin/python3.12 -I
"""zq3 G3: initialise the disposable node/volume with the pinned helper.

Runs on the FreeBSD runtime host (f3) as root, inside an approved window and
below the private staging prefix. It drives only the source-matching helper:
the actual cmd/homelabfs built from the xz3 source commit by
tools/lab/build-init-helper.sh (node-init, volume-init, inspect). It writes the
daemoncli volume configuration the generator planned, creates the empty 0700
mountpoint the runtime's FUSE shell requires, and keeps durable evidence.

The input plan is volume-init.json from `hfs-runtime-prep generate`; its
SHA256 is pinned on the command line. No SSH, VM lifecycle, mount, module,
package or policy action. Nothing is ever deleted: a failed attempt preserves
every partial resource, its intent record makes it uncertain, and a retry
needs a fresh run directory (new run ID). Genesis/journal files are created
only by the helper itself, never written here.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import stat
import subprocess
import sys
import time

PLAN_VERSION = "homelabfs-runtime-volume-init/1"
INTENT_VERSION = "homelabfs-runtime-volume-init-intent/1"
RESULT_VERSION = "homelabfs-runtime-volume-init-result/1"
PROFILE = "homelabfs-cli-disposable-self-client-v1"
PLAN_KEYS = {"version", "run_id", "helper", "helper_sha256", "config",
             "control_dir", "volume_dir", "mountpoint", "volume_id"}
VOLUME_KEYS = {"format_version", "profile", "node_id", "volume_id", "generation",
               "configuration", "root_allocation", "fixture_partner", "fixture_async",
               "owner_incarnation", "partner_incarnation"}
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
DEADLINE = 30            # seconds per helper command (pure local file work)
OUTPUT_LIMIT = 64 << 10  # bytes per helper command, both streams


class CommandUncertain(RuntimeError):
    """A helper command whose effect may have happened but was not observed."""

    def __init__(self, evidence):
        super().__init__(evidence["failure"])
        self.evidence = evidence


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def unique(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "duplicate JSON key")
        value[key] = item
    return value


def strict_json(text):
    return json.loads(text, object_pairs_hook=unique)


def clean_absolute(path):
    return isinstance(path, str) and path.startswith("/") and path != "/" and \
        os.path.normpath(path) == path


def nested(a, b):
    return a == b or a.startswith(b + "/") or b.startswith(a + "/")


def load_plan(path, pinned):
    with open(path, "rb") as stream:
        raw = stream.read((64 << 10) + 1)
    require(len(raw) <= 64 << 10, "oversized plan")
    require(hashlib.sha256(raw).hexdigest() == pinned, "plan SHA256 differs from the pin")
    plan = strict_json(raw)
    validate_plan(plan)
    return plan


def validate_plan(plan):
    require(isinstance(plan, dict) and set(plan) == PLAN_KEYS, "closed plan schema")
    require(plan["version"] == PLAN_VERSION and
            re.fullmatch(r"[a-z0-9][a-z0-9-]{0,23}", plan["run_id"]), "plan identity")
    require(re.fullmatch(r"[0-9a-f]{64}", plan["helper_sha256"]), "helper pin")
    require(UUID.fullmatch(plan["volume_id"]) and plan["volume_id"] != "00000000-0000-0000-0000-000000000000",
            "volume UUID")
    roots = [plan[k] for k in ("config", "control_dir", "volume_dir", "mountpoint")]
    for i, path in enumerate(roots + [plan["helper"]]):
        require(clean_absolute(path), "clean absolute plan paths")
        for other in roots[:min(i, len(roots))]:
            require(not nested(path, other), "plan paths overlap")


def check_new(path, owner):
    """The name must not exist; its parent must be a private real directory."""
    require(not os.path.lexists(path), "already exists (preserved, never reused): " + path)
    info = os.lstat(os.path.dirname(path))
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == owner and not info.st_mode & 0o022,
            "parent must be an owner-controlled real directory: " + path)


def digest(path, owner):
    """Hash a helper that only root (or the explicit test owner) can replace."""
    for parent in Path(path).parents:
        info = os.lstat(parent)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid in (0, owner) and
                (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX),
                "helper ancestors must be owner-controlled directories")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_size <= 128 << 20 and info.st_nlink == 1,
                "bounded singly linked regular helper required")
        require(info.st_uid in (0, owner) and not info.st_mode & 0o022, "protected helper required")
        result = hashlib.sha256()
        while chunk := os.read(fd, 65536):
            result.update(chunk)
        return result.hexdigest()
    finally:
        os.close(fd)


def command(argv):
    """Run one bounded helper command with separate, capped output streams."""
    environment = {"PATH": "/sbin:/bin:/usr/sbin:/usr/bin", "LC_ALL": "C",
                   "GOMAXPROCS": "1", "GOMEMLIMIT": "256MiB", "GOGC": "50"}
    child = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env=environment, cwd="/")
    streams = {"stdout": bytearray(), "stderr": bytearray()}
    try:
        drain(child, streams, time.monotonic() + DEADLINE)
    except BaseException as failure:
        # Only this exact short helper child, never a VM or process group.
        # Its file effects may already exist; the attempt stays uncertain.
        try:
            child.kill()
            child.wait(timeout=1)
        except (ProcessLookupError, subprocess.TimeoutExpired):
            pass
        raise CommandUncertain({"argv": argv, "pid": child.pid, "failure": str(failure),
                                "exit": child.poll(),
                                **{k: v.decode("utf-8", "replace") for k, v in streams.items()}}) from failure
    finally:
        child.stdout.close()
        child.stderr.close()
    return {"argv": argv, "exit": child.returncode,
            **{k: v.decode("utf-8", "strict") for k, v in streams.items()}}


def drain(child, streams, deadline):
    with selectors.DefaultSelector() as selector:
        for name, stream in (("stdout", child.stdout), ("stderr", child.stderr)):
            selector.register(stream, selectors.EVENT_READ, name)
        while selector.get_map():
            require(time.monotonic() < deadline, "helper deadline; effect uncertain")
            for key, _ in selector.select(min(0.1, max(0, deadline - time.monotonic()))):
                chunk = os.read(key.fd, 4096)
                if not chunk:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                streams[key.data].extend(chunk)
                require(sum(map(len, streams.values())) <= OUTPUT_LIMIT,
                        "helper output overflow; effect uncertain")
        child.wait(timeout=max(0.01, deadline - time.monotonic()))


def helper_json(evidence, argv, keys):
    """Run the helper, require success and exactly one JSON object with keys."""
    result = command(argv)
    evidence.append(result)
    require(result["exit"] == 0, "helper failed: " + " ".join(argv[1:2]))
    value = strict_json(result["stdout"])
    require(isinstance(value, dict) and set(value) == keys, "unexpected helper output schema")
    return value


def persist(path, value):
    """Exclusively create one 0600 JSON file and make it durable."""
    raw = (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()
    require(len(raw) <= 512 << 10, "evidence bound")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    sync_directory(os.path.dirname(path))


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def file_sha256(path):
    """Hash one small identity/config file (daemoncli bounds them to 64 KiB)."""
    with open(path, "rb") as stream:
        raw = stream.read((64 << 10) + 1)
    require(len(raw) <= 64 << 10, "identity file exceeds the daemoncli bound: " + path)
    return hashlib.sha256(raw).hexdigest()


def initialise(plan, plan_sha256, record, owner):
    """Run the whole G3 sequence; always leave an intent and a result record."""
    intent = record + ".intent"
    for path in (record, intent):
        check_new(path, owner)
    for key in ("config", "control_dir", "volume_dir", "mountpoint"):
        check_new(plan[key], owner)
    helper = digest(plan["helper"], owner)
    require(helper == plan["helper_sha256"], "helper SHA256 differs from the pin")
    persist(intent, {"version": INTENT_VERSION, "run_id": plan["run_id"], "plan_sha256": plan_sha256,
                     "helper_sha256": helper, "started": time.time()})
    evidence, state = [], {"stage": "node-init"}
    try:
        result = run_steps(plan, owner, evidence, state)
    except BaseException as failure:
        status = "uncertain" if isinstance(failure, CommandUncertain) else "failed"
        extra = failure.evidence if isinstance(failure, CommandUncertain) else {}
        persist(record, {"version": RESULT_VERSION, "run_id": plan["run_id"], "status": status,
                         "stage": state["stage"], "error": str(failure), "uncertain_command": extra,
                         "commands": evidence, "publication": None, "completed": time.time()})
        raise
    result.update({"version": RESULT_VERSION, "run_id": plan["run_id"], "status": "initialised",
                   "plan_sha256": plan_sha256, "helper_sha256": helper, "commands": evidence,
                   "publication": None, "completed": time.time()})
    persist(record, result)
    return result


def run_steps(plan, owner, evidence, state):
    node = helper_json(evidence, [plan["helper"], "node-init", "--control", plan["control_dir"]],
                       {"format_version", "node_id"})
    require(node["format_version"] == 1 and UUID.fullmatch(node["node_id"]), "node identity")
    state["stage"] = "config"
    config = {"format_version": 1, "node_id": node["node_id"], "volume_id": plan["volume_id"],
              "control_dir": plan["control_dir"], "volume_dir": plan["volume_dir"],
              "mountpoint": plan["mountpoint"]}
    persist(plan["config"], config)
    state["stage"] = "volume-init"
    volume = helper_json(evidence, [plan["helper"], "volume-init", "--config", plan["config"],
                                    "--disposable-validation"], VOLUME_KEYS)
    require(volume["format_version"] == 1 and volume["profile"] == PROFILE and
            volume["node_id"] == node["node_id"] and volume["volume_id"] == plan["volume_id"],
            "volume identity differs from the plan")
    state["stage"] = "inspect"
    inspected = helper_json(evidence, [plan["helper"], "inspect", "--config", plan["config"]], VOLUME_KEYS)
    require(inspected == volume, "inspect differs from the initialised volume")
    state["stage"] = "mountpoint"
    make_mountpoint(plan["mountpoint"], owner)
    state["stage"] = "helper-recheck"
    require(digest(plan["helper"], owner) == plan["helper_sha256"], "helper changed during the run")
    return {"node": node, "volume": volume, "files": {
        "config": file_sha256(plan["config"]),
        "node.json": file_sha256(os.path.join(plan["control_dir"], "node.json")),
        "volume.json": file_sha256(os.path.join(plan["volume_dir"], "volume.json"))}}


def make_mountpoint(path, owner):
    """The runtime's FUSE shell needs an existing, empty, private 0700 directory."""
    os.mkdir(path, 0o700)
    # fchmod through an O_NOFOLLOW handle: exact mode despite any umask, and
    # never through a name that something replaced in between.
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fchmod(fd, 0o700)
    finally:
        os.close(fd)
    info = os.lstat(path)
    require(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700 and
            info.st_uid == owner and not os.listdir(path), "private empty mountpoint")
    sync_directory(os.path.dirname(path))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--plan", required=True, help="volume-init.json from hfs-runtime-prep")
    parser.add_argument("--plan-sha256", required=True, help="pinned SHA256 of the plan bytes")
    parser.add_argument("--record", required=True, help="new result record path")
    args = parser.parse_args(argv)
    require(os.geteuid() == 0, "run as root: the runtime and its volume belong to root")
    require(clean_absolute(args.plan) and clean_absolute(args.record), "absolute plan/record paths")
    plan = load_plan(args.plan, args.plan_sha256)
    result = initialise(plan, args.plan_sha256, args.record, 0)
    json.dump({"status": result["status"], "record": args.record,
               "volume_id": result["volume"]["volume_id"]}, sys.stdout, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, ValueError) as failure:
        print("runtime-volume-init:", failure, file=sys.stderr)
        sys.exit(1)
