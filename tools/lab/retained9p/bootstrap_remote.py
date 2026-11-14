"""zq3 gap G1, remote half: the fixed f3/Rocky actions of the Phase 8 bootstrap.

Never staged as a file and never run by hand. `bootstrap.py` sends these exact
bytes over its pinned SSH channel on stdin, behind a short `-c` loader that
reads exactly this many bytes, checks their SHA256 and only then executes
them (exit 97 when the bytes differ, before anything runs). So the remote side
checks what it runs even though it arrives on stdin. A payload (provision,
bind or enrollment bytes) follows the program on the same stdin.

Python 3.9 compatible: Rocky runs it with /usr/bin/python3 (3.9), f3 with
/usr/local/bin/python3.12 under `doas -n`. Each action prints one JSON line.

Exit status is the contract bootstrap.py classifies:
  0  done and verified;
  2  refused or failed with a KNOWN state (`changed` says whether anything was
     modified, e.g. a manifest written but measured wrong);
  1  uncertain: a change was attempted and its outcome was not observed. The
     orchestrator never replays such a step; root inspects.
Read-only actions (host-poll, host-proposal, host-preflight, host-observe,
guest-ping, guest-stat) never exit 1. Remote child processes (rocky_kernel.py
admission, mount, the guest-stat lstat, the OBSERVE client) are never killed
on a deadline: a hung mount(2) or getattr cannot be killed reliably anyway, so
the action reports uncertain (or, for the read-only lstat, `timeout`; for
OBSERVE, a known failure) and leaves the child for inspection.
"""
import argparse
import base64
import hashlib
import json
import os
import re
import stat
import subprocess
import sys

MOUNT_OPTIONS = "trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536"
ADMISSION_PATH = "/etc/homelabfs/installed-admission-v1.json"
SYSFS_TARGETS = {"provision": "/sys/kernel/homelabfs/provision_v1",
                 "bind": "/sys/kernel/homelabfs/bind_v1"}
ADMISSION_V1 = "/sys/kernel/homelabfs/admission_v1"
VIRTIO_9P = "/sys/bus/virtio/drivers/9pnet_virtio"
BOOT_ID = "/proc/sys/kernel/random/boot_id"
MOUNT = "/usr/bin/mount"
PENDING = ".hfs-bootstrap-enrollment.next"  # beside the EnrollmentFile
# runtime-guard.py records, polled to notice a refusal, breach or runtime exit.
GUARD_FILES = ("intent.json", "started.json", "breach.json", "stop-intent.json",
               "stop-uncertain.json", "receipt.json")
STDOUT_LIMIT = 64 << 10      # runtime.stdout holds three short stage records
FILE_LIMIT = 64 << 10        # guard records, manifest, mount_tag
PROPOSAL_LIMIT = 512 << 10   # the runtime's handoff/config bound
PAYLOAD_LIMIT = 512 << 10    # enrollment.json; provision/bind are smaller
OUTPUT_LIMIT = 1 << 20       # captured child output per stream
SECONDS_MAX = 120            # any remote child bound: at most the whole bootstrap
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")
HASH = re.compile(r"[0-9a-f]{64}\Z")
DEVICE = re.compile(r"virtio(0|[1-9][0-9]{0,3})\Z")
CHILD_ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}
# The one program guest-stat runs per mount root (k64): os.lstat of the root
# only, no open, no create, nothing written into the mount. It is still an
# hfs operation: the guest registers the child as an hfs process and sends a
# GETATTR claim, and the child's exit queues a PROCESS_EXIT claim, so each
# probe adds durable claims to the provider ledger (patch 0003). It runs in
# its own process so a getattr stuck in the hfs transport (the guest waits up
# to 30 s per exchange) cannot hold the action past its bound. An OSError is
# a result, not a crash: the errno is exactly what the usability gate reports.
STAT_PROGRAM = ("import json,os,sys\n"
                "try:\n"
                " s=os.lstat(sys.argv[1])\n"
                " print(json.dumps({'mode':s.st_mode,'uid':s.st_uid,'gid':s.st_gid,"
                "'ino':s.st_ino,'dev':s.st_dev}))\n"
                "except OSError as e:\n"
                " print(json.dumps({'errno':e.errno,'error':os.strerror(e.errno)}))\n")


class Refused(RuntimeError):
    """Refused or failed with a known state (exit 2)."""

    def __init__(self, message, changed=False):
        super().__init__(message)
        self.changed = changed


class Uncertain(RuntimeError):
    """A change was attempted and its outcome is unknown (exit 1)."""


def require(ok, message, changed=False):
    if not ok:
        raise Refused(message, changed)


class System:
    """OS boundary. Tests substitute a temporary root and scripted children."""

    def __init__(self, root="/", trusted_uid=0):
        self.root = root
        self.trusted_uid = trusted_uid

    def path(self, absolute):
        return os.path.join(self.root, absolute.lstrip("/"))

    def release(self):
        return os.uname().release

    def boot_id(self):
        return read_bounded(self.path(BOOT_ID), 64).decode("ascii").strip()

    def run(self, argv, seconds):
        """(exit code or None on deadline, stdout, stderr); never kills."""
        child = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, env=CHILD_ENV, close_fds=True)
        try:
            out, err = child.communicate(timeout=seconds)
        except subprocess.TimeoutExpired:
            return None, b"", b""
        return child.returncode, out[:OUTPUT_LIMIT], err[:OUTPUT_LIMIT]


def read_bounded(path, limit):
    """A regular, unlinked-symlink-free file of at most `limit` bytes."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as stream:
        require(stat.S_ISREG(os.fstat(stream.fileno()).st_mode), "not a regular file: " + path)
        raw = stream.read(limit + 1)
    require(len(raw) <= limit, "file exceeds its bound: " + path)
    return raw


def optional_text(path, limit):
    try:
        return read_bounded(path, limit).decode("utf-8")
    except FileNotFoundError:
        return None


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def trusted(system, path, private=False):
    """lstat of a root-owned, not group/world-writable (or private) entry."""
    info = os.lstat(system.path(path))
    require(info.st_uid == system.trusted_uid, "not root-owned: " + path)
    require(not info.st_mode & (0o077 if private else 0o022), "insecure mode: " + path)
    return info


def pinned_file(system, path, expected):
    info = trusted(system, path)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "pinned file shape: " + path)
    raw = read_bounded(system.path(path), 256 << 20)
    require(digest(raw) == expected, "pinned file bytes differ: " + path)


def absent(system, path):
    require(not os.path.lexists(system.path(path)), "already exists (no replay): " + path)


# --------------------------------------------------------------------------
# f3 (root via doas): read-only polling and the one enrollment handoff


def host_preflight(system, args, payload):
    """Pinned guard/runtime bytes, fresh guard/owner/handoff targets."""
    pinned_file(system, args.guard_script, args.guard_sha256)
    pinned_file(system, args.runtime, args.runtime_sha256)
    for path in (args.guard_dir, args.owner_dir, args.enrollment_file):
        absent(system, path)
    parent = trusted(system, os.path.dirname(args.enrollment_file), private=True)
    require(stat.S_ISDIR(parent.st_mode), "enrollment parent must be a private directory")
    return {"guard_sha256": args.guard_sha256, "runtime_sha256": args.runtime_sha256}


def host_poll(system, args, payload):
    """Runtime stdout and the guard's records, as they are now (read-only)."""
    directory = system.path(args.guard_dir)
    if not os.path.isdir(directory):
        return {"guard_dir": False, "stdout": None, "files": {}}
    files = {name: optional_text(os.path.join(directory, name), FILE_LIMIT) for name in GUARD_FILES}
    return {"guard_dir": True, "files": files,
            "stdout": optional_text(os.path.join(directory, "runtime.stdout"), STDOUT_LIMIT)}


def host_proposal(system, args, payload):
    """The runtime's private proposal.json bytes (root-owned 0600, one link)."""
    info = trusted(system, args.path, private=True)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "proposal shape")
    raw = read_bounded(system.path(args.path), PROPOSAL_LIMIT)
    return {"sha256": digest(raw), "bytes": len(raw), "base64": base64.b64encode(raw).decode("ascii")}


def host_observe(system, args, payload):
    """One lifecycle OBSERVE through the pinned runtime's client mode (b84).

    The usable gate reads provider claim states from it. The executable is
    the plan-pinned runtime itself (`--control SOCKET --operation observe`);
    the socket must sit in a root-owned private directory, and the endpoint
    authenticates our peer UID (root, via doas) before reading a byte.
    OBSERVE refreshes the Admissions through the guest observer and consumes
    `--sequence`; it changes no file or mount. A client that does not return
    within `--seconds` is left running (it ends at its own I/O deadline) and
    reported as a known failure, never retried here.
    """
    pinned_file(system, args.executable, args.executable_sha256)
    require(UUID.match(args.runtime_id) is not None, "--runtime-id must be a UUID")
    require(0 < args.sequence <= 0xFFFFFFFF, "--sequence must be 1..2^32-1")
    parent = trusted(system, os.path.dirname(args.socket), private=True)
    require(stat.S_ISDIR(parent.st_mode), "control socket parent must be a private directory")
    require(stat.S_ISSOCK(os.lstat(system.path(args.socket)).st_mode), "control socket is not a socket")
    code, out, err = system.run([args.executable, "--control", args.socket, "--operation", "observe",
                                 "--runtime", args.runtime_id, "--sequence", str(args.sequence)], args.seconds)
    require(code is not None, "OBSERVE client did not return within {} s (left running)".format(args.seconds))
    lines = [line for line in out.decode("utf-8", "replace").splitlines() if line.strip()]
    try:
        reply = json.loads(lines[-1]) if lines else None
    except ValueError:
        reply = None
    require(code == 0 and isinstance(reply, dict),
            "OBSERVE client exit {}: {} {}".format(code, (lines or [""])[-1][-2000:],
                                                   err.decode("utf-8", "replace")[-2000:]))
    return {"sequence": args.sequence, "reply": reply}


def host_install(system, args, payload):
    """Atomically place the signed enrollment as the runtime's EnrollmentFile.

    The runtime reads it as soon as it appears and requires a root-owned 0600
    regular file with ONE link, so this writes a temporary file beside it and
    renames (a link()+unlink() would expose two links). FreeBSD has no
    rename-noreplace: the target is checked absent immediately before the
    rename, and nothing else may write the private handoff directory.
    """
    final = system.path(args.path)
    pending = os.path.join(os.path.dirname(final), PENDING)
    absent(system, args.path)
    absent(system, os.path.join(os.path.dirname(args.path), PENDING))
    try:
        fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            require(stream.write(payload) == len(payload), "short write", changed=True)
            stream.flush()
            os.fsync(stream.fileno())
        # Only the private temporary file exists so far (changed, but unread
        # by the runtime); a target that appeared meanwhile is never replaced.
        require(not os.path.lexists(final), "enrollment target appeared during the write",
                changed=True)
        os.rename(pending, final)
        sync_directory(os.path.dirname(final))
    except OSError as failure:
        raise Uncertain("enrollment placement: " + str(failure))
    return verify_installed(system, args)


def verify_installed(system, args):
    info = os.lstat(system.path(args.path))
    if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_mode & 0o077 or
            info.st_uid != system.trusted_uid):
        raise Uncertain("placed enrollment file has the wrong shape")
    if digest(read_bounded(system.path(args.path), PAYLOAD_LIMIT)) != args.sha256:
        raise Uncertain("placed enrollment readback differs")
    return {"path": args.path, "sha256": args.sha256}


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


# --------------------------------------------------------------------------
# Rocky (root): readiness, admission + measurement, sysfs writes, mounts


def guest_ping(system, args, payload):
    return {"release": system.release(), "boot_id": system.boot_id()}


def guest_admission(system, args, payload):
    """Pinned rocky_kernel.py admission (G5), then measure what it wrote.

    One SSH round trip (run plan Phase 8 step 0): the admission manifest, its
    SHA256 read back, the boot ID before and after, the provider nonce from
    admission_v1 and the actual 9p virtio device name of each share tag.
    """
    require(system.release() == args.release, "running kernel is not the test kernel")
    boot = system.boot_id()
    require(boot == args.boot_id, "guest boot changed since it answered")
    pinned_file(system, args.script, args.script_sha256)
    code, out, err = system.run([args.python, "-I", "-B", args.script, "admission", "--plan",
                                 args.plan, "--plan-sha256", args.plan_sha256, "--state",
                                 args.state], args.seconds)
    if code is None:
        raise Uncertain("admission did not finish before its deadline; left running")
    if code == 2:
        raise Refused("admission refused before change: " + err.decode("utf-8", "replace")[-2000:])
    if code != 0:
        raise Uncertain("admission failed after starting: " + err.decode("utf-8", "replace")[-2000:])
    result = json.loads(out.decode("utf-8"))
    return measure(system, args, result)


def measure(system, args, admission):
    """Fail with changed=True: the manifest exists, its measurement is wrong."""
    manifest = read_bounded(system.path(ADMISSION_PATH), FILE_LIMIT)
    require(digest(manifest) == admission.get("manifest_sha256"),
            "manifest readback differs from the admission result", changed=True)
    observed = json.loads(read_bounded(system.path(ADMISSION_V1), 131072).decode("utf-8"))
    boot = system.boot_id()
    require(boot == args.boot_id == admission.get("boot_id") == observed.get("boot_id"),
            "boot ID differs between ping, admission, admission_v1 and now", changed=True)
    nonce = observed.get("provider_nonce")
    require(isinstance(nonce, str) and UUID.match(nonce) is not None,
            "admission_v1 provider nonce is not a UUID", changed=True)
    return {"admission": admission, "manifest_sha256": digest(manifest), "boot_id": boot,
            "provider_nonce": nonce, "devices": device_names(system, args.tags.split(","))}


def device_names(system, tags):
    """tag -> virtioN for the 9pnet_virtio devices whose mount_tag is a share tag."""
    found = {}
    base = system.path(VIRTIO_9P)
    for name in sorted(os.listdir(base)):
        if DEVICE.match(name) is None:
            continue
        raw = read_bounded(os.path.join(base, name, "mount_tag"), 256)
        tag = raw.rstrip(b"\0\n").decode("ascii")
        if tag in tags:
            require(tag not in found, "two 9p devices carry tag " + tag, changed=True)
            found[tag] = name
    require(sorted(found) == sorted(tags), "missing 9p device for a share tag", changed=True)
    return found


def guest_write(system, args, payload):
    """One complete write of the signed artifact to its one-shot sysfs file."""
    require(args.target in SYSFS_TARGETS, "unknown sysfs target")
    require(system.boot_id() == args.boot_id, "guest boot changed")
    try:
        fd = os.open(system.path(SYSFS_TARGETS[args.target]),
                     os.O_WRONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    except OSError as failure:
        raise Refused("one-shot sysfs file not writable (nothing written): " + str(failure))
    try:
        written = os.write(fd, payload)
    except OSError as failure:
        raise Uncertain("kernel returned an error for the one-shot write: " + str(failure))
    finally:
        os.close(fd)
    if written != len(payload):
        raise Uncertain("short one-shot sysfs write: {} of {}".format(written, len(payload)))
    return {"target": args.target, "bytes": written, "sha256": args.sha256}


def mounted(system, mountpoint):
    """Rows of /proc/self/mountinfo whose mount point is `mountpoint`."""
    raw = read_bounded(system.path("/proc/self/mountinfo"), 1 << 20).decode("utf-8")
    rows = []
    for line in raw.splitlines():
        left, _, right = line.partition(" - ")
        fields, tail = left.split(" "), right.split(" ")
        if len(fields) >= 5 and fields[4] == mountpoint:
            rows.append({"fstype": tail[0], "source": tail[1] if len(tail) > 1 else "",
                         "line": line})
    return rows


def guest_mount(system, args, payload):
    """mount -t 9p with the fixed controlled options, verified in mountinfo."""
    require(system.boot_id() == args.boot_id, "guest boot changed")
    info = trusted(system, args.mountpoint, private=True)
    require(stat.S_ISDIR(info.st_mode), "mountpoint must be a private directory")
    require(not mounted(system, args.mountpoint), "mountpoint already mounted")
    code, out, err = system.run([MOUNT, "-t", "9p", "-o", MOUNT_OPTIONS, args.tag,
                                 args.mountpoint], args.seconds)
    rows = mounted(system, args.mountpoint)
    if code is None:
        raise Uncertain("mount did not return before its deadline; left running")
    if code != 0 and not rows:
        raise Refused("mount failed: " + err.decode("utf-8", "replace")[-2000:])
    if code != 0 or len(rows) != 1 or rows[0]["fstype"] != "9p" or rows[0]["source"] != args.tag:
        raise Uncertain("mount outcome differs from one 9p mount of " + args.tag)
    return {"tag": args.tag, "mountpoint": args.mountpoint, "mountinfo": rows[0]["line"]}


def guest_stat(system, args, payload):
    """Usability probe of the mount roots (k64, after READY).

    Read-only for the mounts' contents and in the remote-action sense (never
    exit 1), but each lstat adds a GETATTR and a PROCESS_EXIT claim to the
    runtime's provider ledger (see STAT_PROGRAM).

    For each (tag, mount point) pair: the mount point must still be exactly
    one 9p mount of that tag in mountinfo, otherwise its root is never
    stat'ed (an lstat would only see the empty directory below and pass for
    the wrong reason). Then one lstat of the root in a child process bounded
    by `--seconds`. Every per-root failure (errno, timeout, not mounted, not a
    directory) is a reported result with exit 0: the orchestrator decides.
    """
    require(system.boot_id() == args.boot_id, "guest boot changed")
    tags, mountpoints = args.tags.split(","), args.mountpoints.split(",")
    require(0 < len(tags) == len(mountpoints) <= 2, "one or two tag/mount point pairs")
    roots = [stat_root(system, args, tag, mountpoint) for tag, mountpoint in zip(tags, mountpoints)]
    return {"boot_id": args.boot_id, "roots": roots}


def stat_root(system, args, tag, mountpoint):
    """One root's probe result; `outcome` is ok, errno, timeout, error,
    not-mounted or not-directory."""
    rows = mounted(system, mountpoint)
    found = {"tag": tag, "mountpoint": mountpoint, "mountinfo": [row["line"] for row in rows]}
    if len(rows) != 1 or rows[0]["fstype"] != "9p" or rows[0]["source"] != tag:
        return dict(found, outcome="not-mounted")
    code, out, err = system.run([args.python, "-I", "-B", "-c", STAT_PROGRAM, mountpoint], args.seconds)
    if code is None:
        # Left running, never killed: a hung getattr is evidence.
        return dict(found, outcome="timeout", seconds=args.seconds)
    try:
        result = json.loads(out.decode("utf-8")) if code == 0 else None
    except ValueError:
        result = None
    if not isinstance(result, dict):
        return dict(found, outcome="error", exit=code, stderr=err.decode("utf-8", "replace")[-2000:])
    if "errno" in result:
        return dict(found, outcome="errno", errno=result.get("errno"), error=result.get("error"))
    mode = result.get("mode")
    outcome = "ok" if type(mode) is int and stat.S_ISDIR(mode) else "not-directory"
    return dict(found, outcome=outcome, stat=result)


# --------------------------------------------------------------------------
# CLI

READ_ONLY = {"host-preflight": host_preflight, "host-poll": host_poll,
             "host-proposal": host_proposal, "host-observe": host_observe,
             "guest-ping": guest_ping, "guest-stat": guest_stat}
MUTATING = {"host-install": host_install, "guest-admission": guest_admission,
            "guest-write": guest_write, "guest-mount": guest_mount}


def parser():
    result = argparse.ArgumentParser(prog="bootstrap_remote", allow_abbrev=False)
    sub = result.add_subparsers(dest="action", required=True)
    options = {
        "host-preflight": ["guard-script", "guard-sha256", "runtime", "runtime-sha256",
                           "guard-dir", "owner-dir", "enrollment-file"],
        "host-poll": ["guard-dir"], "host-proposal": ["path"], "guest-ping": [],
        "host-observe": ["executable", "executable-sha256", "socket", "runtime-id", "sequence",
                         "seconds"],
        "host-install": ["path", "sha256", "size"],
        "guest-admission": ["release", "boot-id", "python", "script", "script-sha256", "plan",
                            "plan-sha256", "state", "tags", "seconds"],
        "guest-write": ["target", "sha256", "size", "boot-id"],
        "guest-mount": ["tag", "mountpoint", "boot-id", "seconds"],
        "guest-stat": ["tags", "mountpoints", "boot-id", "python", "seconds"]}
    for action, names in options.items():
        command = sub.add_parser(action, allow_abbrev=False)
        for name in names:
            kind = int if name in ("size", "seconds", "sequence") else str
            command.add_argument("--" + name, required=True, type=kind)
    return result


def read_payload(args, stdin):
    """Exactly `--size` bytes with the pinned SHA256, or no payload at all."""
    if not hasattr(args, "size"):
        return b""
    require(0 < args.size <= PAYLOAD_LIMIT and HASH.match(args.sha256) is not None, "payload bound")
    raw = stdin.read(args.size + 1)
    require(len(raw) == args.size and digest(raw) == args.sha256, "payload differs from its pin")
    return raw


def dispatch(args, system, stdin):
    """Run one action; map its outcome to (exit status, JSON result)."""
    try:
        action = READ_ONLY.get(args.action) or MUTATING[args.action]
        # A remote child bound outside 1..SECONDS_MAX is a caller bug: refuse
        # before anything runs (exit 2, nothing changed).
        require(not hasattr(args, "seconds") or 0 < args.seconds <= SECONDS_MAX,
                "--seconds must be 1..{}".format(SECONDS_MAX))
        payload = read_payload(args, stdin)
        return 0, dict(action(system, args, payload), ok=True)
    except Refused as failure:
        return 2, {"ok": False, "changed": failure.changed, "error": str(failure)}
    except Uncertain as failure:
        return 1, {"ok": False, "uncertain": True, "error": str(failure)}
    except (OSError, ValueError, KeyError) as failure:
        if args.action in MUTATING:
            # Cannot tell whether the change landed: never claim a known state.
            return 1, {"ok": False, "uncertain": True, "error": repr(failure)}
        return 2, {"ok": False, "changed": False, "error": repr(failure)}


def main(argv, system=None, stdin=None, stdout=None):
    args = parser().parse_args(argv)
    code, result = dispatch(args, system or System(), stdin or sys.stdin.buffer)
    (stdout or sys.stdout).write(json.dumps(dict(result, action=args.action), sort_keys=True) + "\n")
    return code


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
