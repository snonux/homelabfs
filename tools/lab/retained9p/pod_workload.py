#!/usr/bin/python3.9
"""Container-side workload of the zq3 T7 ordinary pod namespace fixture (G9).

pod_fixture.py copies these exact bytes into the locally built, pinned rootfs
(/hfs/pod_workload.py) and starts two rootful containers from it on Rocky:

- w0 owns fresh mount, PID, network, UTS and IPC namespaces (the "pod");
- w1 has its own mount namespace and joins w0's PID, network, UTS and IPC
  namespaces, like a second container of one pod.

Both stay in the guest's initial user namespace and bind-mount the same guest
9p mount m0 at /hfs/m0, so they share one 9p mount / kernel inode domain. This
file only REPORTS what it sees; pod_fixture.py judges every observation against
the guest's own namespaces and its pins. Nothing here decides acceptance.

Each workload prints JSONL events to stdout (the driver reads them through
podman's attached stdout), then waits on stdin for the driver's "observed" line
so the driver can inspect it from outside (/proc/PID/...) while it is alive.

- w0 runs the numeric UID/GID permission matrix (two UIDs, supplementary-only,
  same UID with different full 1024-entry group vectors, and changing groups
  under setfsuid) in forked children, then holds POSIX and flock locks that w1
  contends for: nonblocking conflicts, GETLK owner PID, kernel-observed blocking
  waits, wakeup only after release, and release on close/exit.
- w1 contends through its own bind of the same mount.

The two coordinate over an abstract UNIX socket, which exists per network
namespace: w1 reaching it is itself evidence that it joined w0's netns.

`closure` (run on the Rocky host only, by pod_fixture.py's rootfs manifest)
exercises the same code paths without a mount and prints every module file and
mapped file the interpreter loaded; that list defines the rootfs contents.

Only modules whose closure is Rocky-installed and needs no configuration files
are imported here (deliberately no hashlib/ssl: OpenSSL would want its config
files inside the container). Python 3.9 compatible; Linux x86_64 only.
"""
import ctypes
import errno
import fcntl
import json
import os
import select
import signal
import socket
import struct
import sys
import time

VERSION = "homelabfs-retained9p-pod-workload/1"
MOUNT = "/hfs/m0"
PARAMS_LIMIT = 4096
EVENT_LIMIT = 65536
MESSAGE_LIMIT = 16384
NAMESPACES = ("mnt", "pid", "net", "uts", "ipc", "user", "cgroup")
STATUS_KEYS = ("Uid", "Gid", "Groups", "CapInh", "CapPrm", "CapEff", "CapBnd",
               "CapAmb", "NoNewPrivs", "Seccomp")
# Linux x86_64 struct flock: short type, short whence, 4 pad, off_t start,
# off_t len, pid_t pid, 4 pad = 32 bytes (checked by check_abi before use).
FLOCK = struct.Struct("=hhxxxxqqixxxx")
# Matrix fixture files: owner UID, owner GID, mode. Numeric only: the rootfs
# has no passwd entries for these IDs, nothing resolves names.
FILES = {"uid-only": (41001, 41001, 0o600), "group": (41002, 42002, 0o640),
         "group-tail": (41002, 44022, 0o640)}
# 1024 entries, the maximum the provider admits; the final entry 44022 is the
# only one granting access to group-tail, so truncation anywhere shows as EACCES.
FULL_GROUPS = [42001] + list(range(43000, 44023))
# name, file, uid, gid, complete supplementary groups, expected allow.
MATRIX = [
    ("two-uid-owner", "uid-only", 41001, 41001, [41001], True),
    ("two-uid-other", "uid-only", 41002, 42002, [42002], False),
    ("supplementary-only", "group", 41001, 42001, [42001, 42002], True),
    ("supplementary-missing", "group", 41001, 42001, [42001], False),
    ("full-groups-tail", "group-tail", 41001, 42001, FULL_GROUPS, True),
    ("full-groups-without-tail", "group-tail", 41001, 42001, FULL_GROUPS[:-1], False),
]
# Changing groups inside ONE process under fsuid 41001/fsgid 42001: the
# expected outcome flips with each setgroups() while the process stays alive.
CHANGING = [([42001, 42002], True), ([42001], False), ([42001, 42002], True)]
DENIED = (errno.EACCES,)
CONFLICT = (errno.EACCES, errno.EAGAIN)
LOCK_FILES = ("lock-posix", "lock-flock")


class Refused(RuntimeError):
    """A workload step could not produce the required observation."""


def emit(kind, **fields):
    raw = json.dumps({"version": VERSION, "event": kind,
                      "monotonic_ns": time.monotonic_ns(), **fields},
                     sort_keys=True, separators=(",", ":"))
    if len(raw) > EVENT_LIMIT:
        raise Refused("event bound: " + kind)
    print(raw, flush=True)


def read_text(path, limit=65536):
    with open(path, "rb") as stream:
        raw = stream.read(limit + 1)
    if len(raw) > limit:
        raise Refused("read bound: " + path)
    return raw.decode("ascii", "replace")


def names(run_id):
    """Run-derived leaf and abstract socket names (no free-form input)."""
    return "zq3-" + run_id + "-pod", "hfs-zq3-" + run_id + "-pod"


def check_abi():
    if sys.platform != "linux" or os.uname().machine != "x86_64" or FLOCK.size != 32:
        raise Refused("unsupported platform: Linux x86_64 struct flock required")


# --- identity report -------------------------------------------------------

def status_fields():
    fields = {}
    for line in read_text("/proc/self/status").splitlines():
        key, _, value = line.partition(":")
        if key in STATUS_KEYS:
            fields[key] = value.split()
    return fields


def mount_records(path):
    """Every mountinfo row whose mount point is exactly path (driver wants one)."""
    rows = []
    for line in read_text("/proc/self/mountinfo", 1 << 20).splitlines():
        left, _, right = line.partition(" - ")
        fields, fs = left.split(), right.split()
        if len(fields) >= 5 and len(fs) >= 2 and fields[4] == path:
            rows.append({"mount_id": int(fields[0]), "root": fields[3],
                         "fstype": fs[0], "source": fs[1]})
    return rows


def identity(with_mount=True):
    """Self-reported namespaces, ID maps, credentials, label, UTS, links, mount."""
    links = sorted(os.listdir("/sys/class/net"))
    report = {
        "pid": os.getpid(),
        "namespaces": {n: os.readlink("/proc/self/ns/" + n) for n in NAMESPACES},
        "uid_map": read_text("/proc/self/uid_map").split(),
        "gid_map": read_text("/proc/self/gid_map").split(),
        "status": status_fields(),
        "selinux": read_text("/proc/self/attr/current").rstrip("\0\n"),
        "hostname": socket.gethostname(),
        "links": links,
        "link_flags": {name: read_text("/sys/class/net/" + name + "/flags").strip()
                       for name in links},
    }
    if with_mount:
        report["mounts"] = mount_records(MOUNT)
        report["mount_dev"] = os.stat(MOUNT).st_dev
    return report


# --- permission matrix (w0) ------------------------------------------------

def child(action, timeout=8):
    """Run action in a forked child; return (reply, status). Never kills."""
    receive, send = os.pipe2(os.O_CLOEXEC)
    pid = os.fork()
    if pid == 0:
        os.close(receive)
        try:
            signal.alarm(timeout)
            raw = json.dumps({"ok": True, "result": action()}).encode()
        except BaseException as failure:  # The child reports, never raises.
            raw = json.dumps({"ok": False, "errno": getattr(failure, "errno", None),
                              "error": type(failure).__name__,
                              "detail": str(failure)[:256]}).encode()
        os.write(send, raw[:MESSAGE_LIMIT])
        os._exit(0)
    os.close(send)
    try:
        raw = read_until_eof(receive, time.monotonic() + timeout + 1)
    finally:
        os.close(receive)
    reaped, status = os.waitpid(pid, 0)
    return json.loads(raw.decode()), status


def read_until_eof(fd, deadline):
    chunks, total = [], 0
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([fd], [], [], remaining)[0]:
            raise Refused("child reply timeout")
        chunk = os.read(fd, MESSAGE_LIMIT)
        if not chunk:
            return b"".join(chunks)
        total += len(chunk)
        if total > MESSAGE_LIMIT:
            raise Refused("child reply bound")
        chunks.append(chunk)


def try_read(leaf_fd, name):
    """Open+read for the access check; an OSError becomes its errno."""
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=leaf_fd)
    except OSError as failure:
        return {"errno": failure.errno}
    try:
        return {"errno": 0, "read": os.read(fd, 64).decode()}
    finally:
        os.close(fd)


def create_fixture_files(leaf_fd):
    """Root (CAP_CHOWN/CAP_FOWNER only) creates the numeric-owner files."""
    for name, (uid, gid, mode) in sorted(FILES.items()):
        fd = os.open(name, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW,
                     0o600, dir_fd=leaf_fd)
        try:
            os.write(fd, name.encode())
            os.fchown(fd, uid, gid)
            os.fchmod(fd, mode)
            os.fsync(fd)
        finally:
            os.close(fd)


def matrix_case(leaf_fd, case):
    name, filename, uid, gid, groups, allow = case

    def action():
        # Order matters: groups and gid need CAP_SETGID, which setuid drops.
        os.setgroups(groups)
        os.setgid(gid)
        os.setuid(uid)
        observed = try_read(leaf_fd, filename)
        observed.update(uid=os.getuid(), gid=os.getgid(), groups=os.getgroups())
        return observed

    reply, status = child(action)
    return {"case": name, "file": filename, "uid": uid, "gid": gid,
            "groups": groups, "expected_allow": allow, "status": status, "reply": reply}


def changing_groups(leaf_fd):
    """One process, fsuid/fsgid switched, groups changed between opens."""
    def action():
        libc = ctypes.CDLL(None, use_errno=True)
        libc.setfsgid(42001)
        libc.setfsuid(41001)
        steps = []
        for groups, allow in CHANGING:
            os.setgroups(groups)  # CAP_SETGID survives setfsuid (fs caps only drop).
            steps.append({"groups": groups, "expected_allow": allow,
                          "fsuid": libc.setfsuid(-1), "fsgid": libc.setfsgid(-1),
                          **try_read(leaf_fd, "group")})
        return steps

    reply, status = child(action)
    return {"case": "changing-groups-fsuid", "status": status, "reply": reply}


def run_matrix(leaf_fd):
    create_fixture_files(leaf_fd)
    cases = [matrix_case(leaf_fd, case) for case in MATRIX]
    cases.append(changing_groups(leaf_fd))
    return cases


# --- coordination channel ----------------------------------------------------

class Channel:
    """Newline-delimited bounded JSON over a connected abstract UNIX socket."""

    def __init__(self, sock):
        self.sock, self.buffer = sock, b""

    def send(self, kind, **fields):
        raw = json.dumps({"kind": kind, **fields}).encode() + b"\n"
        if len(raw) > MESSAGE_LIMIT:
            raise Refused("message bound")
        self.sock.sendall(raw)

    def receive(self, kind, deadline):
        while b"\n" not in self.buffer:
            if not self.fill(deadline):
                raise Refused("peer closed before " + kind)
        line, _, self.buffer = self.buffer.partition(b"\n")
        message = json.loads(line.decode())
        if message.get("kind") != kind:
            raise Refused("expected " + kind + ", got " + str(message.get("kind")))
        return message

    def fill(self, deadline):
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([self.sock], [], [], remaining)[0]:
            raise Refused("peer message timeout")
        chunk = self.sock.recv(MESSAGE_LIMIT)
        self.buffer += chunk
        if len(self.buffer) > MESSAGE_LIMIT:
            raise Refused("message bound")
        return bool(chunk)

    def wait_eof(self, deadline):
        while self.fill(deadline):
            pass
        if self.buffer:
            raise Refused("unexpected trailing peer data")


def wait_observed(deadline):
    """Barrier: the driver writes one 'observed' line after outside inspection."""
    remaining = deadline - time.monotonic()
    if remaining <= 0 or not select.select([sys.stdin], [], [], remaining)[0]:
        raise Refused("driver observation barrier timeout")
    if sys.stdin.readline(64).strip() != "observed":
        raise Refused("unexpected driver barrier line")


# --- locks -------------------------------------------------------------------

def file_identity(fd):
    seen = os.fstat(fd)
    return {"dev": seen.st_dev, "ino": seen.st_ino}


def getlk(fd):
    query = FLOCK.pack(fcntl.F_WRLCK, os.SEEK_SET, 0, 1, 0)
    kind, whence, start, length, pid = FLOCK.unpack(fcntl.fcntl(fd, fcntl.F_GETLK, query))
    return {"type": kind, "whence": whence, "start": start, "length": length, "pid": pid}


def try_lock(fd, family):
    """Nonblocking exclusive attempt; returns 0 or the errno."""
    try:
        if family == "posix":
            fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB, 1, 0, os.SEEK_SET)
        else:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError as failure:
        return failure.errno
    return 0


def blocking_lock(fd, family):
    """Blocking exclusive lock; the global alarm bounds a wait that never ends."""
    if family == "posix":
        fcntl.lockf(fd, fcntl.LOCK_EX, 1, 0, os.SEEK_SET)
    else:
        fcntl.flock(fd, fcntl.LOCK_EX)


def waiter_line(raw, pid, ident, family):
    """The /proc/locks blocked-waiter row of pid on exactly this inode, or None.

    Linux 9p implements flock through POSIX locks, so a flock waiter may show
    as POSIX over the whole file (0..EOF), as the guest lock probe records.
    """
    for line in raw.splitlines():
        fields = line.split()
        if "->" not in fields:
            continue
        index = fields.index("->")
        if len(fields) < index + 8 or fields[index + 4] != str(pid):
            continue
        inode = fields[index + 5].split(":")
        if (len(inode) != 3 or int(inode[2]) != ident["ino"] or
                os.makedev(int(inode[0], 16), int(inode[1], 16)) != ident["dev"]):
            continue
        allowed = ("POSIX",) if family == "posix" else ("FLOCK", "POSIX")
        end = "0" if family == "posix" else "EOF"
        if fields[index + 1] in allowed and fields[index + 6:index + 8] == ["0", end]:
            return line
    return None


def poll_waiter(pid, ident, family, deadline):
    """Kernel evidence that the peer is really blocked, not merely announced."""
    while time.monotonic() < deadline:
        line = waiter_line(read_text("/proc/locks", 1 << 20), pid, ident, family)
        if line is not None:
            return line
        time.sleep(0.01)
    raise Refused("no kernel blocked-waiter evidence for " + family)


def open_lock_files(leaf_fd, create):
    flags = os.O_RDWR | os.O_NOFOLLOW | (os.O_CREAT | os.O_EXCL if create else 0)
    return [os.open(name, flags, 0o644, dir_fd=leaf_fd) for name in LOCK_FILES]


def hold_and_release(channel, leaf_fd, deadline):
    """w0: hold both locks, observe w1 contend and block, release one by one."""
    fds = open_lock_files(leaf_fd, True)
    idents = [file_identity(fd) for fd in fds]
    held = [try_lock(fd, family) for fd, family in zip(fds, ("posix", "flock"))]
    if held != [0, 0]:
        raise Refused("w0 could not take its own locks: " + str(held))
    hello = channel.receive("hello", deadline)
    channel.send("held", pid=os.getpid(), posix=idents[0], flock=idents[1])
    contended = channel.receive("contended", deadline)
    result = {"peer_pid": hello["pid"], "identities": idents, "contended": contended}
    for index, family in enumerate(("posix", "flock")):
        channel.receive(family + "-waiting", deadline)
        result[family + "_waiter"] = poll_waiter(hello["pid"], idents[index], family, deadline)
        result[family + "_released_ns"] = time.monotonic_ns()
        os.close(fds[index])  # Close releases POSIX and (last fd) flock locks.
        result[family + "_acquired"] = channel.receive(family + "-acquired", deadline)
    channel.receive("done", deadline)
    channel.wait_eof(deadline)
    result["after_peer_exit"] = acquire_after_exit(leaf_fd, deadline)
    return result


def acquire_after_exit(leaf_fd, deadline):
    """Peer exit must release its locks; bounded retry covers the exit race."""
    fds = open_lock_files(leaf_fd, False)
    try:
        outcome = {}
        stop = min(deadline, time.monotonic() + 2)
        for fd, family in zip(fds, ("posix", "flock")):
            attempts, code = 0, None
            while code != 0 and time.monotonic() < stop:
                attempts += 1
                code = try_lock(fd, family)
                if code != 0:
                    time.sleep(0.02)
            outcome[family] = {"errno": code, "attempts": attempts}
        return outcome
    finally:
        for fd in fds:
            os.close(fd)


# --- roles -------------------------------------------------------------------

def parse_params(raw):
    if len(raw) > PARAMS_LIMIT:
        raise Refused("params bound")
    params = json.loads(raw)
    if (not isinstance(params, dict) or set(params) != {"version", "run_id", "role", "seconds"} or
            params["version"] != VERSION or params["role"] not in ("w0", "w1") or
            type(params["seconds"]) is not int or not 5 <= params["seconds"] <= 60):
        raise Refused("params shape")
    return params


def role_w0(params, deadline):
    leaf, address = names(params["run_id"])
    listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM | socket.SOCK_CLOEXEC)
    listener.bind("\0" + address)
    listener.listen(1)
    emit("identity", **identity())
    emit("ready")
    wait_observed(deadline)
    root = os.open(MOUNT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    os.mkdir(leaf, 0o755, dir_fd=root)
    leaf_fd = os.open(leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root)
    os.chmod(leaf_fd, 0o755)
    emit("leaf", name=leaf, identity=file_identity(leaf_fd))
    emit("matrix", cases=run_matrix(leaf_fd))
    if not select.select([listener], [], [], max(0, deadline - time.monotonic()))[0]:
        raise Refused("w1 never connected")
    connection, _ = listener.accept()
    emit("locks", **hold_and_release(Channel(connection), leaf_fd, deadline))


def role_w1(params, deadline):
    leaf, address = names(params["run_id"])
    emit("identity", **identity())
    emit("ready")
    wait_observed(deadline)
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM | socket.SOCK_CLOEXEC)
    sock.connect("\0" + address)
    channel = Channel(sock)
    # w0 creates the leaf only after its own barrier; the hello/held exchange
    # orders us after its matrix, so the leaf exists before we open it.
    channel.send("hello", pid=os.getpid())
    held = channel.receive("held", deadline)
    root = os.open(MOUNT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    leaf_fd = os.open(leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root)
    emit("leaf", name=leaf, identity=file_identity(leaf_fd), held=held)
    emit("locks", **contend(channel, leaf_fd, held, deadline))


def contend(channel, leaf_fd, held, deadline):
    """w1: conflict, GETLK owner, then block until w0 releases each lock."""
    fds = open_lock_files(leaf_fd, False)
    report = {"held": held, "identities": [file_identity(fd) for fd in fds],
              "getlk": getlk(fds[0]),
              "nonblocking": [try_lock(fd, family) for fd, family in zip(fds, ("posix", "flock"))]}
    channel.send("contended", **report)
    for fd, family in zip(fds, ("posix", "flock")):
        channel.send(family + "-waiting")
        started = time.monotonic_ns()
        blocking_lock(fd, family)
        report[family] = {"wait_started_ns": started, "acquired_ns": time.monotonic_ns()}
        channel.send(family + "-acquired", acquired_ns=report[family]["acquired_ns"])
    channel.send("done")
    return report  # Locks stay held; process exit must release them.


def closure():
    """Host-only: exercise the workload's lazy paths, list what was loaded."""
    check_abi()
    identity(with_mount=False)
    json.loads(json.dumps({"closure": [1, "x"]}))
    libc = ctypes.CDLL(None, use_errno=True)
    libc.setfsuid(-1)
    memfd = os.memfd_create("hfs-closure")
    try:
        try_lock(memfd, "posix")
        try_lock(memfd, "flock")
        getlk(memfd)
    finally:
        os.close(memfd)
    probe = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM | socket.SOCK_CLOEXEC)
    probe.bind("\0hfs-closure-" + str(os.getpid()))
    probe.close()
    receive, send = os.pipe2(os.O_CLOEXEC)
    select.select([receive], [], [], 0)
    os.close(receive)
    os.close(send)
    signal.alarm(0)
    modules = sorted({getattr(m, "__file__", None) for m in list(sys.modules.values())} - {None})
    maps = sorted({line.split(None, 5)[5] for line in
                   read_text("/proc/self/maps", 1 << 20).splitlines()
                   if len(line.split(None, 5)) == 6})
    print(json.dumps({"version": VERSION, "executable": sys.executable,
                      "modules": modules, "maps": maps}, sort_keys=True))


def main(argv):
    if argv[1:] == ["closure"]:
        closure()
        return 0
    success = False
    try:
        if len(argv) != 3 or argv[1] != "workload":
            raise Refused("usage: workload PARAMS_JSON | closure")
        check_abi()
        params = parse_params(argv[2])
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("deadline")))
        signal.alarm(params["seconds"])
        deadline = time.monotonic() + params["seconds"] - 1
        (role_w0 if params["role"] == "w0" else role_w1)(params, deadline)
        success = True
    except BaseException as failure:
        emit("failure", error=type(failure).__name__, detail=str(failure)[:1024],
             errno=getattr(failure, "errno", None))
    finally:
        signal.alarm(0)
        emit("terminal", success=success, acceptance=False)
    return 0 if success else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
