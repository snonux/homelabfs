#!/usr/bin/env python3
"""Pinned bounded guest syscall batch; no install/VM/mount/exclusion authority.

This controller preserves invocation/reply logs outside the lab, journals each
dispatch before SSH, and refuses automatic replay of an uncertain dispatch.
Its report always leaves full zq3 acceptance incomplete.
"""
import argparse
import datetime
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import shlex
import signal
import stat
import subprocess
import sys
import time

VERSION = "homelabfs-retained9p-probe-batch/1"
GUEST = Path(__file__).with_name("guest.py")
STREAM_LIMIT = 256 << 10
REQUEST_LIMIT = 16384  # Same wire bound as guest.py, including final newline.
HASH = re.compile(r"[0-9a-f]{64}\Z")
QUARANTINED = {"7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b"}


class Refused(RuntimeError):
    pass


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Refused("duplicate JSON key")
        result[key] = value
    return result


def read(path, bound=512 << 10):
    path = Path(path)
    if not path.is_absolute() or str(path) != os.path.normpath(path):
        raise Refused("clean absolute input path required")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_size > bound:
            raise Refused("bounded regular input required")
        stream = os.fdopen(fd, "rb")
        fd = -1
        with stream:
            raw = stream.read(bound + 1)
    finally:
        if fd >= 0:
            os.close(fd)
    if len(raw) > bound:
        raise Refused("input size bound")
    return raw


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"),
                       allow_nan=False) + "\n").encode()


def parse(raw):
    return json.loads(raw, object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(Refused("nonfinite JSON")))


def load(path):
    return parse(read(path))


def fsync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def mkdir_durable(path):
    path.mkdir(mode=0o700)
    fsync_directory(path)
    fsync_directory(path.parent)


def check_output_parent(output):
    current = Path("/")
    for part in output.parent.parts[1:]:
        current /= part
        info = current.lstat()
        sticky_temporary = (current in {Path("/tmp"), Path("/var/tmp")} and
                            info.st_uid == 0 and info.st_mode & stat.S_ISVTX)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in {0, os.geteuid()} or
                (info.st_mode & 0o022 and not sticky_temporary)):
            raise Refused("output ancestors must exclude untrusted replacement")


def check_output_path(output):
    # OpenSSH expands tokens/environment and parses option whitespace before
    # ProxyCommand shell quoting. Refuse such characters in snapshot paths.
    text = os.fspath(output)
    if (not re.fullmatch(r"/[A-Za-z0-9_./-]+", text) or
            os.path.normpath(text) != text or text == "/"):
        raise Refused("output requires canonical absolute safe ASCII path [A-Za-z0-9_./-]")


def persist_bytes(path, raw, mode=0o600):
    pending = path.with_name(path.name + ".next")
    fd = os.open(pending, os.O_CREAT | os.O_EXCL | os.O_WRONLY, mode)
    with os.fdopen(fd, "wb") as stream:
        if stream.write(raw) != len(raw):
            raise OSError("short durable evidence write")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(pending, path)
    fsync_directory(path.parent)


def persist(path, value):
    persist_bytes(path, canonical(value))


def verified_bytes(path, expected, bound):
    raw = read(path, bound)
    if digest(raw) != expected:
        raise Refused("pinned input changed: " + str(path))
    return raw


def closed(value, names):
    if not isinstance(value, dict) or set(value) != set(names):
        raise Refused("closed input shape: " + ",".join(names))


def require_candidate(manifest_hash):
    if not isinstance(manifest_hash, str) or not HASH.fullmatch(manifest_hash):
        raise Refused("exact candidate manifest digest required")
    if manifest_hash in QUARANTINED:
        raise Refused("quarantined CGO-disabled FreeBSD daemon cannot admit native exporter; r24/s24/xz3 prerequisites pending")
    # A different digest is not native qualification. Actual artifact admission,
    # installed observations and root's completed setup remain separate gates.


def validate(plan):
    closed(plan, ["version", "run_id", "ssh", "host", "guest", "probe_sha256",
                  "candidate_manifest_sha256", "installed_manifest_sha256", "requests", "seconds"])
    require_candidate(plan["candidate_manifest_sha256"])
    if plan["version"] != VERSION or not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,23}", plan["run_id"]):
        raise Refused("batch version/run ID")
    if type(plan["seconds"]) is not int or not 60 <= plan["seconds"] <= 600:
        raise Refused("batch deadline60..600 seconds")
    closed(plan["ssh"], ["path", "sha256"])
    inputs = {"ssh": verified_bytes(plan["ssh"]["path"], plan["ssh"]["sha256"], 16 << 20),
              "probe.py": verified_bytes(GUEST, plan["probe_sha256"], 512 << 10)}
    inputs["probe.py"].decode("utf-8")
    if not HASH.fullmatch(plan["installed_manifest_sha256"]):
        raise Refused("probe/installed-manifest pin")
    for name in ["host", "guest"]:
        target = plan[name]
        closed(target, ["address", "alias", "user", "known_hosts", "known_hosts_sha256"])
        if (str(ipaddress.IPv4Address(target["address"])) != target["address"] or
                not re.fullmatch(r"[a-z0-9_.-]{1,128}", target["alias"]) or
                not re.fullmatch(r"[a-z_][a-z0-9_-]{0,31}", target["user"])):
            raise Refused("explicit pinned SSH target")
        inputs[name + "-known_hosts"] = verified_bytes(target["known_hosts"],
                                                       target["known_hosts_sha256"], 65536)
    if ((plan["host"]["address"], plan["host"]["alias"], plan["host"]["user"]) !=
            ("192.168.1.133", "f3", "paul") or
            (plan["guest"]["address"], plan["guest"]["alias"], plan["guest"]["user"]) !=
            ("192.168.1.123", "rocky", "root")):
        raise Refused("only the recorded f3/standalone Rocky lab path is in scope")
    if plan["guest"]["user"] != "root" or len(plan["requests"]) != 5:
        raise Refused("trusted-root exact five-case syscall batch")
    names = []
    original = None
    for request in plan["requests"]:
        if len(canonical(request)) > REQUEST_LIMIT:
            raise Refused("guest request exceeds 16KiB wire bound")
        closed(request, ["version", "run_id", "boot_id", "provider_nonce", "manifest_sha256",
                         "mounts", "case", "seconds"])
        if (request.get("run_id") != plan["run_id"] or
                request.get("version") != "homelabfs-retained9p-syscalls/1" or
                request.get("manifest_sha256") != plan["installed_manifest_sha256"] or
                type(request["seconds"]) is not int or not 1 <= request["seconds"] <= 60):
            raise Refused("case differs from exact batch")
        identity = {key: value for key, value in request.items() if key not in {"case", "seconds"}}
        if original is not None and original != identity:
            raise Refused("boot/provider/mount identity changes within batch")
        original = identity
        if len(request["mounts"]) != 2:
            raise Refused("two exact independent domain rows required")
        for i, mount in enumerate(request["mounts"]):
            closed(mount, ["path", "tag", "mount_id", "session"])
            if mount["path"] != "/var/tmp/hfs-zq3-" + plan["run_id"] + "/m" + str(i):
                raise Refused("mount path outside exact run")
            if type(mount["mount_id"]) is not int or mount["mount_id"] <= 0:
                raise Refused("actual positive mount ID required")
        for field in ["path", "tag", "mount_id", "session"]:
            if request["mounts"][0][field] == request["mounts"][1][field]:
                raise Refused("duplicate independent mount/domain identity")
        names.append(request.get("case"))
    if names != ["credentials", "sync", "metadata", "namespace", "mapped-pressure"]:
        raise Refused("exact ordered syscall cases required")
    return inputs


def plan_snapshot(path, expected_hash=None):
    # One open supplies the bytes parsed, validated, hashed and later persisted.
    raw = read(path)
    if expected_hash is not None and digest(raw) != expected_hash:
        raise Refused("exact batch plan digest differs")
    plan = parse(raw)
    return raw, plan, validate(plan)


def stage_inputs(output, inputs):
    # New private directory and nonwritable files exclude changes to original
    # paths. Administrative replacement inside this owned root is not allowed.
    directory = output / "inputs"
    mkdir_durable(directory)
    for name, raw in inputs.items():
        persist_bytes(directory / name, raw, 0o500 if name == "ssh" else 0o400)
    fsync_directory(directory)
    return directory


def ssh_base(target, inputs, name):
    return [str(inputs / "ssh"), "-F", "/dev/null", "-T",
            "-oBatchMode=yes", "-oStrictHostKeyChecking=yes",
            "-oGlobalKnownHostsFile=/dev/null",
            "-oUserKnownHostsFile=" + str(inputs / (name + "-known_hosts")),
            "-oHostKeyAlias=" + target["alias"],
            "-oUpdateHostKeys=no", "-oControlMaster=no", "-oControlPath=none",
            "-oConnectTimeout=5", "-oServerAliveInterval=3", "-oServerAliveCountMax=1",
            "-oClearAllForwardings=yes", "-oForwardAgent=no"]


def invocation(plan, inputs, probe_bytes):
    host, guest = plan["host"], plan["guest"]
    jump = ssh_base(host, inputs, "host") + ["-W", guest["address"] + ":22", "--",
                                  host["user"] + "@" + host["address"]]
    return ssh_base(guest, inputs, "guest") + ["-oProxyCommand=" + shlex.join(jump), "--",
                                   guest["user"] + "@" + guest["address"],
                                   "python3 -I -c " + shlex.quote(probe_bytes.decode("utf-8"))]


def bounded_child(argv, request, output, seconds):
    """Bound streams while running; timeout kills only local SSH, never remote VM.

    Any lost transport/timeout leaves the dispatch uncertain. The remote owned
    probe has its own alarm, but that is not a terminal quiescence certificate.
    Input is at most16KiB and sent nonblocking under the same deadline while
    draining output. Never depend on pipe capacity or SSH reading stdin early.
    """
    started = time.monotonic()
    payload = canonical(request)
    if len(payload) > REQUEST_LIMIT:
        raise Refused("guest request exceeds 16KiB wire bound before dispatch")
    process = None
    streams = {}
    counts = {"stdout": 0, "stderr": 0}
    timeout = False
    try:
        # Empty log files and their directory entries must be durable before
        # a child can run; a durable dispatch intent must not refer to lost logs.
        for name in counts:
            fd = os.open(output / (name + ".log"), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            streams[name] = os.fdopen(fd, "wb", buffering=0)
            os.fsync(streams[name].fileno())
        fsync_directory(output)
        if time.monotonic() - started >= seconds:
            raise TimeoutError("local case deadline before dispatch; remote state uncertain")
        process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, start_new_session=True, bufsize=0)
        with selectors.DefaultSelector() as selector:
            offset = 0
            os.set_blocking(process.stdin.fileno(), False)
            selector.register(process.stdin, selectors.EVENT_WRITE, "stdin")
            for name in counts:
                source = getattr(process, name)
                os.set_blocking(source.fileno(), False)
                selector.register(source, selectors.EVENT_READ, name)
            while selector.get_map():
                remaining = seconds - (time.monotonic() - started)
                if remaining <= 0:
                    raise TimeoutError("local SSH/case deadline; remote state uncertain")
                for key, _ in selector.select(min(remaining, 0.25)):
                    if key.data == "stdin":
                        try:
                            written = os.write(key.fd, payload[offset:offset + 4096])
                        except BlockingIOError:
                            continue
                        if written <= 0:
                            raise BrokenPipeError("request delivery stopped; remote state uncertain")
                        offset += written
                        if offset == len(payload):
                            selector.unregister(process.stdin)
                            process.stdin.close()
                        continue
                    try:
                        chunk = os.read(key.fd, 65536)
                    except BlockingIOError:
                        continue
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    name = key.data
                    counts[name] += len(chunk)
                    if counts[name] > STREAM_LIMIT:
                        raise Refused("required evidence exceeded stream cap; remote state uncertain")
                    if streams[name].write(chunk) != len(chunk):
                        raise OSError("short controller evidence write; remote state uncertain")
            code = process.wait(timeout=max(0.001, seconds - (time.monotonic() - started)))
        return {"returncode": code, "bytes": counts, "seconds": time.monotonic() - started}
    except BaseException:
        timeout = True
        raise
    finally:
        if timeout and process is not None and process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                # SSH is a local transport child only. No remote kill command.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=2)
        if process is not None and not process.stdin.closed:
            process.stdin.close()
        for stream in streams.values():
            os.fsync(stream.fileno())
            stream.close()
        fsync_directory(output)
        if process is not None:
            for name in ["stdout", "stderr"]:
                getattr(process, name).close()


def check_terminal(path, run_case):
    events = [json.loads(line, object_pairs_hook=unique) for line in read(path, STREAM_LIMIT).splitlines()]
    if not events or events[-1].get("event") != "terminal":
        raise Refused("missing complete guest terminal; remote state uncertain")
    terminal = events[-1]
    if terminal.get("uncertain_children"):
        raise Refused("guest child quiescence uncertain")
    if terminal.get("acceptance") is not False or terminal.get("publication", "missing") is not None:
        raise Refused("guest claims unauthorized acceptance/publication")
    if terminal.get("syscall_success") is not True:
        raise Refused("required actual syscall case failed: " + run_case)
    return {"syscall_success": True, "acceptance": False,
            "scope": "refusal-boundary-only" if run_case == "namespace" else "real-guest-syscalls-only"}


def probe(plan_path, expected_hash, output):
    check_output_path(output)
    raw, plan, verified = plan_snapshot(plan_path, expected_hash)
    output = Path(output)
    check_output_parent(output)
    mkdir_durable(output)
    lock = open(output / "owner.lock", "xb")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    os.fsync(lock.fileno())
    state = {"version": VERSION, "plan_sha256": expected_hash,
             "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
             "cases": [], "syscall_batch_success": False,
             "zq3_acceptance_complete": False, "deployment_eligible": False, "publication": None,
             "missing": ["full host journal/source/error correlation", "funded drains/PTE/TLB/old-client fences",
                         "ordinary pod namespaces/securityContext/SELinux audit", "same-mount advisory locks",
                         "C9 same-owner lost-reply recovery", "replacement/RESUME refusals", "K04"]}
    persist_bytes(output / "plan.json", raw)
    persist(output / "state.json", state)
    started = time.monotonic()
    try:
        inputs = stage_inputs(output, verified)
        for request in plan["requests"]:
            remaining = plan["seconds"] - (time.monotonic() - started)
            if remaining < request["seconds"] + 15:
                raise Refused("batch deadline cannot fund next case plus SSH margin")
            name = request["case"]
            folder = output / name
            mkdir_durable(folder)
            case = {"name": name, "state": "dispatching-uncertain"}
            state["cases"].append(case)
            persist(output / "state.json", state)
            case.update(bounded_child(invocation(plan, inputs, verified["probe.py"]),
                                      request, folder, request["seconds"] + 15))
            # A retained terminal plus exit status is mandatory; transport exit alone is insufficient.
            case.update(check_terminal(folder / "stdout.log", name))
            if case["returncode"] != 0:
                raise Refused("SSH/guest nonzero exit")
            fsync_directory(folder)
            case["state"] = "syscalls-completed"
            persist(output / "state.json", state)
        state["syscall_batch_success"] = True
    except BaseException as failure:
        state["error"] = type(failure).__name__ + ": " + str(failure)
    finally:
        state["seconds"] = time.monotonic() - started
        persist(output / "state.json", state)
        lock.close()
    print(json.dumps(state, sort_keys=True))
    return 0 if state["syscall_batch_success"] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    check = sub.add_parser("check")
    check.add_argument("--plan", required=True)
    run = sub.add_parser("probe")
    run.add_argument("--plan", required=True)
    run.add_argument("--sha256", required=True)
    run.add_argument("--output", required=True)
    status = sub.add_parser("status")
    status.add_argument("--output", required=True)
    args = parser.parse_args()
    if args.action == "check":
        raw, _, _ = plan_snapshot(args.plan)
        print(json.dumps({"plan_sha256": digest(raw),
                          "execution_authorized": False, "deployment_eligible": False,
                          "acceptance": False}))
        return 0
    if args.action == "status":
        print(json.dumps(load(str(Path(args.output) / "state.json")), sort_keys=True))
        return 0
    return probe(args.plan, args.sha256, args.output)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, Refused) as failure:
        print("refuse: " + str(failure), file=sys.stderr)
        sys.exit(1)
