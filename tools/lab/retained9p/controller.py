#!/usr/bin/env python3
"""Pinned bounded guest syscall batch; no install/VM/mount/exclusion authority.

This controller preserves invocation/reply logs outside the lab, journals each
dispatch before SSH, and refuses automatic replay of an uncertain dispatch.
Its report always leaves full zq3 acceptance incomplete.

Each successful case leaves its payload RETAINED in the guest. The run then
waits, durably, for host source/journal correlation of that exact case. Only
`release` with a correlation record bound to the case's retained stdout and
inventory digests dispatches the guest's authenticated one-shot release, and
only `next` after a completed release dispatches the following case, so the
4MiB logical canary never holds two cases' payloads.

After all five releases, `lifecycle --step` dispatches the zq3 T9-T11 guest
steps (lost-reply CALL, re-read, quiesce, after-loss) once each and in order,
with the same intent journal and no replay. correlate.py interprets them.
"""
import argparse
import base64
import binascii
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
# CGO-disabled daemon: can never observe a native exporter (see quarantine.json).
QUARANTINED = {"7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b"}
# Superseded candidates, each refused for its own reason:
# - 3f770a88 (e0cca0c) cannot arm the MKDIR-only lost reply (d44);
# - b203799e (f44, defd974) has a guest kernel whose mount builds HELLO before
#   enrollment, so every hfs1.4 mount returns EPROTO (zq3-r2 R2-4, fixed by r54);
# - 5eb42c50 (r54, ca920a2) has a guest kernel whose kill_sb cleanup after a
#   failed hfs_fill_super dereferences unset completion state and oopses
#   (zq3-r3 R3-1, fixed by w54);
# - 89c928d5 (w54, ff866c5) has the pre-z54 daemon 174949c1, which leaves a
#   VIEW_POLL that arrives before the domain is acquired unanswered, so the
#   first mount blocks in the transport (zq3-r4 R4-1, fixed by z54);
# - 2e0412a6 (z54, a20aca1) has a guest kernel that lets the next owner of a
#   fixed 9p tag allocate it before the previous request is released, so the
#   release loop's VIEW_QUERY and the event worker's VIEW_ACK on tag 65533
#   collide, the session blocks and DRAINED is never sent (zq3-r5 R5-1,
#   fixed by 964);
# - e01fab21 (964, 06c27a8) has a guest kernel whose CONFIG_LSM lacks selinux,
#   so the guest boots without SELinux (c64; kernel.py/admit.py now refuse its
#   config), and the z54 daemon 39c92d01, which keeps one view fence for all
#   domains and refuses an unissued out-of-order guest cell (zq3-r6 R6-1/R6-2,
#   fixed by d64).
SUPERSEDED = {"3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1",
              "b203799e3f631888b2c2edc24e1d38b913249bea978e0d4d954886f589d04fa2",
              "5eb42c5066792b55e439373db48e8e8e88532efc61ff7b82f64db22a87798f99",
              "89c928d5072838bb509ec19e471109b549ce7b06c59451fab64a6d09250bec66",
              "2e0412a62d1c1f0aa0b29865c521bf3df7a925ea5ef293428fa9b38b647d8f2f",
              "e01fab21cd7a47822d7b56d84e15150cdc7a36c939fceb23852d4fdef0121ec2"}
# The only candidate this controller pins: the xz3 c64+d64 rebuild
# (2026-11-10, source 060dea0). Its guest kernel is built from the c64 recipe
# (CONFIG_LSM includes selinux, so SELinux is an active LSM) with the
# unchanged 964 patch series; its CGO daemon 9b7eed8d keeps the view fence per
# domain and admits a bounded out-of-order guest cell once (d64). The modules,
# the native exporter and the Linux source records are byte-identical reuses
# from e01fab21; the window artifacts were rebuilt from 060dea0. The new
# daemon bytes still need the f3 observer check. Admission here is a dispatch
# precondition, never deployment eligibility.
ADMITTED = {"66b5bb2b6c1dd1a663f26c50d0d9b3bfeebd620127c4a5859ca146074f2c2e2d"}
CASES = ["credentials", "sync", "metadata", "namespace", "mapped-pressure"]
RELEASE_VERSION = "homelabfs-retained9p-release/1"  # Same as guest.py.
CORRELATION_VERSION = "homelabfs-retained9p-host-correlation/1"
STEP_VERSION = "homelabfs-retained9p-lifecycle-step/1"  # Same as guest.py.
# zq3 T9-T11 guest steps in their only allowed order. A step may be skipped
# (a failed gate stops dependent work) but never repeated or run out of order;
# the re-read additionally needs its lost-reply CALL.
STEPS = ["lost-reply-call", "lost-reply-reread", "quiesce", "after-loss"]
STEP_SECONDS = 30  # Guest alarm per step; quiesce needs syncfs plus a 2 s settle.
# Payload may stay retained at most this long after the run started (the base
# gate's 60-minute total window); later commands refuse and preserve.
RUN_WINDOW = 3600
# Exact SSH targets: (address, HostKeyAlias, user). The addresses are the
# IQ-verified primaries after the 10.20.0.0/24 LAN migration (IQ
# infra/f3s/network/host-addresses.md, checked 2026-11-08). Phase 1 of the zq3
# run plan re-verifies them before any contact.
TARGETS = {"host": ("10.20.0.133", "f3", "paul"),
           "guest": ("10.20.0.123", "rocky", "root")}
# Pre-migration addresses stay configured on both machines as aliases, but a
# plan may name only the primary: the pinned known_hosts lines, the observer
# key's from= restriction (observer_key.py) and the evidence all name one route,
# so accepting either address would let one plan digest mean two network paths.
RETIRED = {"192.168.1.133": "host", "192.168.1.123": "guest"}
# Ed25519 host key fingerprints, cross-checked on 2026-11-08 between Paul's
# existing known_hosts entries and the earlier lab record in
# docs/lab/Retained-9p-fixtures.md. Never scanned or accepted-new. The public
# pin files are docs/lab/f3-host-known_hosts and docs/lab/rocky-known_hosts.
HOST_KEYS = {"f3": "SHA256:VZLFFSJZ9K7iOBQ/cFehASFzCfIOn0TKFD1L89ziL5g",
             "rocky": "SHA256:UCoipW2LqGWOpaRnuShloHwgo0w2sHAUthKdvPt5KsA"}


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
    if manifest_hash in SUPERSEDED:
        # Each superseded digest lacks a fix the run needs (d44 lost-reply
        # arming, the r54 mount HELLO, the w54 failed-bind kill_sb fence, the
        # z54 pre-acquire VIEW_POLL answer, the 964 fixed-tag release wait, or
        # the c64 SELinux LSM and the d64 per-domain fence); the comment at
        # SUPERSEDED says which.
        raise Refused("superseded xz3 candidate: use the c64+d64 rebuild (d44 lost reply, r54 mount HELLO, w54 kill_sb, z54 VIEW_POLL, 964 fixed tag, c64 SELinux, d64 domain fence)")
    if manifest_hash not in ADMITTED:
        # Fail closed: an unknown digest is not a qualified candidate.
        raise Refused("candidate manifest is not the pinned admitted xz3 candidate")
    # Pinning is not native qualification. Actual artifact admission, installed
    # observations and root's completed setup remain separate gates.


def pinned_sources(plan):
    """Pinned original input paths: name -> (path, sha256, bound)."""
    sources = {"ssh": (plan["ssh"]["path"], plan["ssh"]["sha256"], 16 << 20),
               "probe.py": (GUEST, plan["probe_sha256"], 512 << 10)}
    for name in ["host", "guest"]:
        sources[name + "-known_hosts"] = (plan[name]["known_hosts"],
                                          plan[name]["known_hosts_sha256"], 65536)
    return sources


def validate(plan):
    """Static plan checks, then one verified read of every pinned input."""
    validate_shape(plan)
    inputs = {name: verified_bytes(path, expected, bound)
              for name, (path, expected, bound) in pinned_sources(plan).items()}
    inputs["probe.py"].decode("utf-8")
    for name in ["host", "guest"]:
        check_pin(inputs[name + "-known_hosts"], plan[name])
    return inputs


def key_fingerprint(encoded):
    """OpenSSH SHA256 fingerprint of one base64 ssh-ed25519 public key blob."""
    try:
        blob = base64.b64decode(encoded, validate=True)
    except binascii.Error:
        raise Refused("pinned host key is not base64") from None
    # Wire format: string "ssh-ed25519", then string of the 32-byte key; no more.
    if len(blob) != 51 or not blob.startswith(b"\0\0\0\x0bssh-ed25519\0\0\0\x20"):
        raise Refused("pinned host key is not one ssh-ed25519 key")
    return "SHA256:" + base64.b64encode(hashlib.sha256(blob).digest()).decode().rstrip("=")


def check_pin(raw, target):
    """Pin file content: exactly `ALIAS,ADDRESS ssh-ed25519 KEY` of the reviewed key.

    The plan hash only binds bytes; this binds them to the reviewed host
    identity. The alias serves the controller's HostKeyAlias lookup, the
    address the runtime observer's plain `root@ADDRESS` lookup. A retired
    address, a second line or a different key refuses.
    """
    try:
        lines = [line for line in raw.decode("ascii").splitlines()
                 if line.strip() and not line.startswith("#")]
    except UnicodeDecodeError:
        raise Refused("pinned host key file must be ASCII") from None
    expected = target["alias"] + "," + target["address"]
    fields = lines[0].split(" ") if len(lines) == 1 else []
    if len(fields) != 3 or fields[0] != expected or fields[1] != "ssh-ed25519":
        raise Refused("pin file must hold exactly one line: " + expected + " ssh-ed25519 KEY")
    if key_fingerprint(fields[2]) != HOST_KEYS[target["alias"]]:
        raise Refused("pinned host key differs from reviewed " + target["alias"] + " fingerprint")


def validate_shape(plan):
    closed(plan, ["version", "run_id", "ssh", "host", "guest", "probe_sha256",
                  "candidate_manifest_sha256", "installed_manifest_sha256", "requests", "seconds"])
    require_candidate(plan["candidate_manifest_sha256"])
    if plan["version"] != VERSION or not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,23}", plan["run_id"]):
        raise Refused("batch version/run ID")
    # plan["seconds"] caps every single SSH dispatch (see dispatch_seconds).
    if type(plan["seconds"]) is not int or not 60 <= plan["seconds"] <= 600:
        raise Refused("per-dispatch deadline cap60..600 seconds")
    closed(plan["ssh"], ["path", "sha256"])
    if not HASH.fullmatch(plan["installed_manifest_sha256"]):
        raise Refused("probe/installed-manifest pin")
    for name in ["host", "guest"]:
        target = plan[name]
        closed(target, ["address", "alias", "user", "known_hosts", "known_hosts_sha256"])
        if (str(ipaddress.IPv4Address(target["address"])) != target["address"] or
                not re.fullmatch(r"[a-z0-9_.-]{1,128}", target["alias"]) or
                not re.fullmatch(r"[a-z_][a-z0-9_-]{0,31}", target["user"])):
            raise Refused("explicit pinned SSH target")
    validate_targets(plan)
    validate_requests(plan)


def validate_targets(plan):
    """Only the IQ primaries; a retired alias refuses with its reason, not silently."""
    for name, expected in TARGETS.items():
        address = plan[name]["address"]
        if address in RETIRED:
            primary = TARGETS[RETIRED[address]]
            raise Refused("pre-migration address " + address + " of " + primary[1] +
                          " is a retained alias only; plans name the primary " + primary[0])
        if (address, plan[name]["alias"], plan[name]["user"]) != expected:
            raise Refused("only the recorded f3/standalone Rocky lab path is in scope")


def validate_requests(plan):
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
    if names != CASES:
        raise Refused("exact ordered syscall cases required")


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


def read_events(raw):
    return [json.loads(line, object_pairs_hook=unique) for line in raw.splitlines()]


def retained_event(events, run_case):
    """Exactly one retention record whose inventory hashes as the guest claims."""
    found = [event for event in events if event.get("event") == "payload-retained"]
    if len(found) != 1:
        raise Refused("successful case must retain its payload exactly once: " + run_case)
    event = found[0]
    inventory, token = event.get("inventory"), event.get("release_token")
    claimed = event.get("inventory_sha256")
    # guest.py hashes compact sorted JSON without the trailing newline.
    if (not isinstance(inventory, dict) or not isinstance(token, str) or not HASH.fullmatch(token) or
            not isinstance(claimed, str) or claimed != digest(canonical(inventory)[:-1]) or
            event.get("leaf") != inventory.get("leaf")):
        raise Refused("retention record malformed or inventory digest differs")
    return event


def check_terminal(path, run_case, action="case", correlation=None):
    """Require a complete truthful terminal; a case must retain, a release remove."""
    raw = read(path, STREAM_LIMIT)
    events = read_events(raw)
    if not events or events[-1].get("event") != "terminal":
        raise Refused("missing complete guest terminal; remote state uncertain")
    terminal = events[-1]
    if terminal.get("uncertain_children"):
        raise Refused("guest child quiescence uncertain")
    if terminal.get("acceptance") is not False or terminal.get("publication", "missing") is not None:
        raise Refused("guest claims unauthorized acceptance/publication")
    if terminal.get("syscall_success") is not True:
        raise Refused("required actual syscall " + action + " failed: " + run_case)
    if terminal.get("action") != action:
        raise Refused("guest terminal action differs from dispatch")
    summary = {"syscall_success": True, "acceptance": False, "stdout_sha256": digest(raw)}
    if action == "release":
        released = [event for event in events if event.get("event") == "payload-released"]
        if (terminal.get("payload_released") is not True or terminal.get("payload_retained") is not False or
                len(released) != 1 or released[0].get("correlation_sha256") != correlation):
            raise Refused("release terminal does not prove the bound payload release")
        return {**summary, "payload": "released"}
    if terminal.get("payload_retained") is not True or terminal.get("payload_released") is not False:
        raise Refused("successful case must retain its payload for host correlation")
    event = retained_event(events, run_case)
    return {**summary, "payload": "retained", "leaf": event["leaf"],
            "inventory_sha256": event["inventory_sha256"],
            "scope": "refusal-boundary-only" if run_case == "namespace" else "real-guest-syscalls-only"}


def check_step_terminal(path, step):
    """A completed lifecycle step: truthful terminal plus exactly one observation.

    syscall_success here means the guest ran the step to completion; the
    errno values inside the observation are data for the host correlator.
    """
    raw = read(path, STREAM_LIMIT)
    events = read_events(raw)
    if not events or events[-1].get("event") != "terminal":
        raise Refused("missing complete guest terminal; remote state uncertain")
    terminal = events[-1]
    if terminal.get("uncertain_children"):
        raise Refused("guest child quiescence uncertain")
    if terminal.get("acceptance") is not False or terminal.get("publication", "missing") is not None:
        raise Refused("guest claims unauthorized acceptance/publication")
    if terminal.get("action") != "step" or terminal.get("step") != step:
        raise Refused("guest terminal action differs from dispatch")
    if (terminal.get("syscall_success") is not True or terminal.get("payload_retained") is not False or
            terminal.get("payload_released") is not False):
        raise Refused("required lifecycle step did not complete: " + step)
    observed = [event for event in events if event.get("event") == "step-observed"]
    if (len(observed) != 1 or observed[0].get("step") != step or
            not isinstance(observed[0].get("observation"), dict) or
            observed[0].get("acceptance") is not False):
        raise Refused("lifecycle step must record exactly one observation: " + step)
    return {"stdout_sha256": digest(raw), "observation": observed[0]["observation"],
            "acceptance": False}


def check_step_order(state, step):
    """All five payloads released, then STEPS strictly in order, each once."""
    if step not in STEPS:
        raise Refused("unknown lifecycle step")
    if not state.get("syscall_batch_success"):
        raise Refused("lifecycle steps require all five case payloads released")
    done = [item["step"] for item in state.get("lifecycle", [])]
    if done and STEPS.index(done[-1]) >= STEPS.index(step):
        raise Refused("lifecycle step repeated or out of order: " + step)
    if step == "lost-reply-reread" and done[-1:] != ["lost-reply-call"]:
        raise Refused("re-read requires the immediately preceding lost-reply CALL")


def step_request(plan, step):
    """The plan's exact boot/provider/manifest/mount identity naming one step."""
    identity = {key: value for key, value in plan["requests"][0].items()
                if key not in {"version", "case", "seconds"}}
    return {**identity, "version": STEP_VERSION, "step": step, "seconds": STEP_SECONDS}


def check_correlation(value, run_id, case):
    """Bind host correlation to this exact retained case; contents stay external."""
    closed(value, ["version", "run_id", "case", "case_stdout_sha256", "inventory_sha256",
                   "host_observe", "acceptance"])
    if (value["version"] != CORRELATION_VERSION or value["run_id"] != run_id or
            value["case"] != case["name"] or value["case_stdout_sha256"] != case["stdout_sha256"] or
            value["inventory_sha256"] != case["inventory_sha256"]):
        raise Refused("host correlation is not bound to this retained case")
    if not isinstance(value["host_observe"], dict) or not value["host_observe"] or value["acceptance"] is not False:
        raise Refused("host correlation evidence missing or claims acceptance")


def dispatch_seconds(plan, request):
    """Local SSH deadline: guest case alarm plus 15 s margin, capped by the plan."""
    return min(request["seconds"] + 15, plan["seconds"])


def new_state(expected_hash):
    return {"version": VERSION, "plan_sha256": expected_hash,
            "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "started_epoch": time.time(), "cases": [], "syscall_batch_success": False,
            "zq3_acceptance_complete": False, "deployment_eligible": False, "publication": None,
            "missing": ["independent verification of host journal/source/error correlation",
                        "funded drains/PTE/TLB/old-client fences",
                        "ordinary pod namespaces/securityContext/SELinux audit", "same-mount advisory locks",
                        "C9 same-owner lost-reply recovery", "replacement/RESUME refusals", "K04"]}


class Run:
    """One owned output directory: plan, staged inputs, durable state and lock."""

    def __init__(self, output, plan, state, lock, inputs, probe_bytes):
        self.output, self.plan, self.state, self.lock = output, plan, state, lock
        self.inputs, self.probe_bytes = inputs, probe_bytes

    def save(self):
        persist(self.output / "state.json", self.state)

    def step(self, action):
        """Run one state-changing step; any failure is recorded, never replayed."""
        started = time.monotonic()
        try:
            action()
        except BaseException as failure:
            self.state["error"] = type(failure).__name__ + ": " + str(failure)
        finally:
            self.state["seconds"] = self.state.get("seconds", 0) + time.monotonic() - started
            self.save()
            self.lock.close()
        print(json.dumps(self.state, sort_keys=True))
        return 1 if "error" in self.state else 0

    def dispatch(self, request, folder, action, correlation=None):
        """One SSH dispatch; a retained terminal plus zero exit is mandatory."""
        result = bounded_child(invocation(self.plan, self.inputs, self.probe_bytes),
                               request, folder, dispatch_seconds(self.plan, request))
        result.update(check_terminal(folder / "stdout.log", request["case"], action, correlation))
        if result["returncode"] != 0:
            raise Refused("SSH/guest nonzero exit")
        fsync_directory(folder)
        return result

    def run_case(self, index):
        request = self.plan["requests"][index]
        folder = self.output / request["case"]
        mkdir_durable(folder)
        case = {"name": request["case"], "state": "dispatching-uncertain"}
        self.state["cases"].append(case)
        self.save()
        case.update(self.dispatch(request, folder, "case"))
        if case["leaf"] != "zq3-" + self.plan["run_id"] + "-" + case["name"]:
            raise Refused("retained leaf outside exact run case")
        case["state"] = "payload-retained-awaiting-correlation"
        self.save()

    def next_index(self):
        cases = self.state["cases"]
        if not cases or cases[-1]["state"] != "released" or len(cases) >= len(CASES):
            raise Refused("next case requires the previous payload released and a remaining case")
        return len(cases)

    def release_request(self, correlation_path, correlation_sha256):
        """Pre-dispatch checks; refusal here changes no run state."""
        case = self.state["cases"][-1] if self.state["cases"] else {}
        if case.get("state") != "payload-retained-awaiting-correlation":
            raise Refused("no retained payload awaiting host correlation")
        if not HASH.fullmatch(correlation_sha256):
            raise Refused("exact host correlation digest required")
        raw = verified_bytes(correlation_path, correlation_sha256, 512 << 10)
        check_correlation(parse(raw), self.plan["run_id"], case)
        stdout = read(self.output / case["name"] / "stdout.log", STREAM_LIMIT)
        if digest(stdout) != case["stdout_sha256"]:
            raise Refused("retained case evidence changed")
        token = retained_event(read_events(stdout), case["name"])["release_token"]
        request = {**self.plan["requests"][len(self.state["cases"]) - 1],
                   "version": RELEASE_VERSION, "token": token,
                   "inventory_sha256": case["inventory_sha256"],
                   "correlation_sha256": correlation_sha256}
        return case, raw, request

    def release(self, case, raw, request):
        folder = self.output / case["name"]
        persist_bytes(folder / "correlation.json", raw)
        mkdir_durable(folder / "release")
        case["correlation_sha256"] = request["correlation_sha256"]
        case["state"] = "release-dispatching-uncertain"
        self.save()
        case["release"] = self.dispatch(request, folder / "release", "release",
                                        request["correlation_sha256"])
        case["state"] = "released"
        self.state["syscall_batch_success"] = (
            len(self.state["cases"]) == len(CASES) and
            all(item["state"] == "released" for item in self.state["cases"]))
        self.save()

    def run_lifecycle(self, step):
        """One guest lifecycle step; intent is durable before SSH, never replayed."""
        request = step_request(self.plan, step)
        folder = self.output / ("lifecycle-" + step)
        mkdir_durable(folder)
        record = {"step": step, "state": "dispatching-uncertain"}
        self.state.setdefault("lifecycle", []).append(record)
        self.save()
        result = bounded_child(invocation(self.plan, self.inputs, self.probe_bytes),
                               request, folder, dispatch_seconds(self.plan, request))
        result.update(check_step_terminal(folder / "stdout.log", step))
        if result["returncode"] != 0:
            raise Refused("SSH/guest nonzero exit")
        fsync_directory(folder)
        record.update(result)
        # Workstation clock; correlate.py prints it as what a RESTART OBSERVE must follow.
        record["completed_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        record["state"] = "completed"
        self.save()


def open_run(output, expected_hash):
    """Reopen an owned run; staged snapshots must still match the exact plan."""
    check_output_path(output)
    output = Path(output)
    lock = os.fdopen(os.open(output / "owner.lock", os.O_RDWR | os.O_NOFOLLOW), "rb+", buffering=0)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        raw = read(output / "plan.json")
        if digest(raw) != expected_hash:
            raise Refused("exact batch plan digest differs")
        plan = parse(raw)
        validate_shape(plan)
        inputs = output / "inputs"
        for name, (_, expected, bound) in pinned_sources(plan).items():
            verified_bytes(inputs / name, expected, bound)
        probe_bytes = verified_bytes(inputs / "probe.py", plan["probe_sha256"], 512 << 10)
        state = load(output / "state.json")
        if state.get("plan_sha256") != expected_hash or "error" in state:
            raise Refused("run state differs or recorded an error; inspect, never replay")
        if time.time() - state["started_epoch"] > RUN_WINDOW:
            raise Refused("run window expired; retained payload preserved for inspection")
    except BaseException:
        lock.close()
        raise
    return Run(output, plan, state, lock, inputs, probe_bytes)


def probe(plan_path, expected_hash, output):
    """Create the run and dispatch only the first case, which stays retained."""
    check_output_path(output)
    raw, plan, verified = plan_snapshot(plan_path, expected_hash)
    output = Path(output)
    check_output_parent(output)
    mkdir_durable(output)
    lock = open(output / "owner.lock", "xb")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    os.fsync(lock.fileno())
    run = Run(output, plan, new_state(expected_hash), lock, None, verified["probe.py"])
    persist_bytes(output / "plan.json", raw)
    run.save()

    def first():
        run.inputs = stage_inputs(output, verified)
        run.run_case(0)
    return run.step(first)


def next_case(output, expected_hash):
    run = open_run(output, expected_hash)
    try:
        index = run.next_index()
    except BaseException:
        run.lock.close()
        raise
    return run.step(lambda: run.run_case(index))


def release(output, expected_hash, correlation_path, correlation_sha256):
    run = open_run(output, expected_hash)
    try:
        prepared = run.release_request(correlation_path, correlation_sha256)
    except BaseException:
        run.lock.close()
        raise
    return run.step(lambda: run.release(*prepared))


def lifecycle(output, expected_hash, step):
    """Dispatch one zq3 T9-T11 guest step after the released batch."""
    run = open_run(output, expected_hash)
    try:
        check_step_order(run.state, step)
    except BaseException:
        run.lock.close()
        raise
    return run.step(lambda: run.run_lifecycle(step))


def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    sub.add_parser("check").add_argument("--plan", required=True)
    run = sub.add_parser("probe")
    run.add_argument("--plan", required=True)
    run.add_argument("--sha256", required=True)
    run.add_argument("--output", required=True)
    following = sub.add_parser("next")
    following.add_argument("--sha256", required=True)
    following.add_argument("--output", required=True)
    freeing = sub.add_parser("release")
    freeing.add_argument("--sha256", required=True)
    freeing.add_argument("--output", required=True)
    freeing.add_argument("--correlation", required=True)
    freeing.add_argument("--correlation-sha256", required=True)
    stepping = sub.add_parser("lifecycle")
    stepping.add_argument("--sha256", required=True)
    stepping.add_argument("--output", required=True)
    stepping.add_argument("--step", required=True, choices=STEPS)
    sub.add_parser("status").add_argument("--output", required=True)
    return parser.parse_args()


def main():
    args = arguments()
    if args.action == "check":
        raw, _, _ = plan_snapshot(args.plan)
        print(json.dumps({"plan_sha256": digest(raw),
                          "execution_authorized": False, "deployment_eligible": False,
                          "acceptance": False}))
        return 0
    if args.action == "status":
        print(json.dumps(load(str(Path(args.output) / "state.json")), sort_keys=True))
        return 0
    if args.action == "next":
        return next_case(args.output, args.sha256)
    if args.action == "release":
        return release(args.output, args.sha256, args.correlation, args.correlation_sha256)
    if args.action == "lifecycle":
        return lifecycle(args.output, args.sha256, args.step)
    return probe(args.plan, args.sha256, args.output)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, Refused) as failure:
        print("refuse: " + str(failure), file=sys.stderr)
        sys.exit(1)
