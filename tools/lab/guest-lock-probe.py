#!/usr/bin/env python3
"""Bounded, same-inode advisory lock fixture; no provisioning or publication proof."""
from contextlib import contextmanager
import ctypes
import errno
import fcntl
import json
import os
import platform
import re
import select
import signal
import socket
import sys
import time

PROTOCOL = "hfs-guest-lock-v1"
# Budget bounds (be4, zq3-r14 R14-1). One hfs syscall can wait about 90-120 s
# (docs/Test-automation.md), and T6 runs this probe on the hfs mount m0, so a
# case may last one worst-case syscall plus margin and the whole run as long
# as a guest.py request. The alarms only catch hangs. ENTRY_SECONDS bounds the
# stdin read and validation before the request's own overall budget applies.
OVERALL_SECONDS_MIN, OVERALL_SECONDS_MAX = 5, 240
CASE_SECONDS_MIN, CASE_SECONDS_MAX = 1, 150
ENTRY_SECONDS = OVERALL_SECONDS_MAX - 2
MAX_REQUEST = 8192
MAX_REPORT = 65536


class Refused(RuntimeError):
    pass


class Flock(ctypes.Structure):
    _fields_ = [("type", ctypes.c_short), ("whence", ctypes.c_short),
                ("start", ctypes.c_long), ("length", ctypes.c_long),
                ("pid", ctypes.c_int)]


def check_abi():
    if (sys.platform != "linux" or platform.machine() != "x86_64"
            or ctypes.sizeof(Flock) != 32
            or [getattr(Flock, n).offset for n in ("type", "whence", "start", "length", "pid")]
            != [0, 2, 8, 16, 24]):
        raise Refused("unsupported Linux x86_64 native struct flock ABI")


def getlk(fd):
    query = Flock(fcntl.F_WRLCK, os.SEEK_SET, 0, 8, 0)
    result = Flock.from_buffer_copy(fcntl.fcntl(fd, fcntl.F_GETLK, bytes(query)))
    return {n: getattr(result, n) for n in ("type", "whence", "start", "length", "pid")}


def lock(fd, family, mode, start=0, length=8, blocking=False):
    operation = {"exclusive": fcntl.LOCK_EX, "shared": fcntl.LOCK_SH,
                 "unlock": fcntl.LOCK_UN}[mode]
    if not blocking and mode != "unlock":
        operation |= fcntl.LOCK_NB
    if family == "posix":
        fcntl.lockf(fd, operation, length, start, os.SEEK_SET)
    else:
        fcntl.flock(fd, operation)


def identity(fd):
    st = os.fstat(fd)
    return {"dev": st.st_dev, "ino": st.st_ino}


def decode_mount(value):
    return re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), value)


def mount_record(path, source, mount_id):
    matches = []
    with open("/proc/self/mountinfo", encoding="utf-8") as stream:
        for line in stream:
            left, right = line.rstrip().split(" - ", 1)
            fields, fs = left.split(), right.split()
            if decode_mount(fields[4]) == path:
                matches.append((fields[0], fs[0], decode_mount(fs[1])))
    if matches != [(str(mount_id), "9p", source)]:
        raise Refused("exact mountpoint, 9p source and mount ID required")
    return {"mountpoint": path, "source": source, "mount_id": mount_id, "fstype": "9p"}


def fd_mount_id(fd):
    with open("/proc/self/fdinfo/" + str(fd), encoding="ascii") as stream:
        for line in stream:
            if line.startswith("mnt_id:"):
                return int(line.split()[1])
    raise Refused("descriptor has no mount ID")


def validate_request(request):
    if not isinstance(request, dict) or set(request) != {"protocol", "run_id", "mountpoint", "tag", "mount_id", "budgets"}:
        raise Refused("invalid request fields")
    if request["protocol"] != PROTOCOL or not isinstance(request["run_id"], str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,47}", request["run_id"]):
        raise Refused("invalid protocol/run ID")
    path, tag = request["mountpoint"], request["tag"]
    if not isinstance(path, str) or not path.startswith("/") or path == "/" or os.path.normpath(path) != path or os.path.realpath(path) != path:
        raise Refused("mountpoint must be canonical and contain no symlink")
    if not isinstance(tag, str) or not re.fullmatch(r"[A-Za-z0-9_.-]{1,128}", tag):
        raise Refused("invalid mount source tag")
    if type(request["mount_id"]) is not int or request["mount_id"] <= 0:
        raise Refused("invalid mount ID")
    budget = request["budgets"]
    if not isinstance(budget, dict) or set(budget) != {"overall_seconds", "case_seconds"}:
        raise Refused("invalid budgets")
    if (any(type(budget[n]) is not int for n in budget)
            or not OVERALL_SECONDS_MIN <= budget["overall_seconds"] <= OVERALL_SECONDS_MAX
            or not CASE_SECONDS_MIN <= budget["case_seconds"] <= CASE_SECONDS_MAX):
        raise Refused("budget outside overall %d..%d / case %d..%d seconds" % (
            OVERALL_SECONDS_MIN, OVERALL_SECONDS_MAX, CASE_SECONDS_MIN, CASE_SECONDS_MAX))


def admit(request, owned):
    """All checks precede creation. Keep this descriptor until fixture completion.

    The root fd is appended to the caller's owned list with the raising
    handlers deferred (child_bookkeeping): an hfs open waits uninterruptibly,
    so an expired alarm would otherwise raise as os.open returns and leak the
    fd until process exit. A refusal closes and removes it again, also with the
    handlers deferred, so the caller never closes it twice.
    """
    validate_request(request)
    observed = mount_record(request["mountpoint"], request["tag"], request["mount_id"])
    with child_bookkeeping():
        owned.append(os.open(request["mountpoint"], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW))
    fd = owned[-1]
    try:
        if fd_mount_id(fd) != request["mount_id"]:
            raise Refused("retained descriptor mount ID differs")
        mount_record(request["mountpoint"], request["tag"], request["mount_id"])
        if identity(fd) != identity_path(request["mountpoint"]):
            raise Refused("mountpoint identity changed")
        return fd, observed
    except BaseException:
        with child_bookkeeping():
            os.close(owned.pop())
        raise


def identity_path(path):
    st = os.stat(path, follow_symlinks=False)
    return {"dev": st.st_dev, "ino": st.st_ino}


def worker(sock, directory, filename, family):
    # Fork does not grant contenders inherited open descriptions. Close every
    # inherited data/transport fd before independently opening via retained dirfd.
    keep = {0, 1, 2, sock.fileno(), directory}
    for entry in os.listdir("/proc/self/fd"):
        fd = int(entry)
        if fd not in keep:
            try:
                os.close(fd)
            except OSError:
                pass
    signal.signal(signal.SIGTERM, signal.SIG_DFL)
    signal.signal(signal.SIGALRM, signal.SIG_DFL)
    fd = os.open(filename, os.O_RDWR | os.O_NOFOLLOW, dir_fd=directory)
    duplicate = None
    evidence = {"pid": os.getpid(), **identity(fd), "independent_open": True}
    def send(event, **values):
        sock.sendall(json.dumps({"event": event, **evidence, **values}).encode() + b"\n")
    send("ready")
    buffer = b""
    while True:
        while b"\n" not in buffer:
            chunk = sock.recv(2048)
            if not chunk:
                return
            buffer += chunk
            if len(buffer) > 2048:
                raise Refused("oversized child command")
        line, buffer = buffer.split(b"\n", 1)
        command = json.loads(line)
        op = command["op"]
        if op == "exit":
            return
        try:
            if op == "lock":
                if command.get("blocking"):
                    send("entering-wait")
                lock(fd, family, **{k: v for k, v in command.items() if k != "op"})
                send("acquired" if command["mode"] != "unlock" else "unlocked")
            elif op == "getlk":
                send("getlk", result=getlk(fd))
            elif op == "dup":
                duplicate = os.dup(fd)
                send("duplicated")
            elif op == "close-original":
                os.close(fd)
                fd = duplicate
                duplicate = None
                send("closed-original")
            elif op == "close-final":
                os.close(fd)
                fd = None
                send("closed-final")
            elif op == "reopen":
                fd = os.open(filename, os.O_RDWR | os.O_NOFOLLOW, dir_fd=directory)
                if identity(fd) != {k: evidence[k] for k in ("dev", "ino")}:
                    raise Refused("reopened inode changed")
                send("reopened")
            elif op == "close-other":
                other = os.open(filename, os.O_RDWR | os.O_NOFOLLOW, dir_fd=directory)
                os.close(other)
                send("closed-other")
            else:
                raise Refused("unknown child command")
        except OSError as error:
            send("errno", errno=error.errno)


def waiter_record(raw, pid, expected_identity, family):
    """Select only our child's waiter; record Linux 9p's POSIX flock conversion.

    /proc/locks prints the superblock s_dev, while fstat reports st_dev. On 9p
    the two are equal. btrfs subvolumes report an anonymous st_dev, so the exact
    match below deliberately refuses there instead of accepting an unproven
    waiter. Local self-tests must therefore run on a filesystem such as tmpfs.
    """
    for line in raw.splitlines():
        fields = line.split()
        if "->" not in fields:
            continue
        index = fields.index("->")
        if len(fields) < index + 8 or fields[index + 4] != str(pid):
            continue
        allowed_types = {"POSIX"} if family == "posix" else {"FLOCK", "POSIX"}
        if (fields[index + 1] not in allowed_types or fields[index + 2:index + 4] != ["ADVISORY", "WRITE"]
                or fields[index + 6:index + 8] != ["0", "7" if family == "posix" else "EOF"]):
            raise Refused("unexpected kernel waiter type/range/PID")
        inode = fields[index + 5].split(":")
        if (len(inode) != 3 or int(inode[2]) != expected_identity["ino"]
                or os.makedev(int(inode[0], 16), int(inode[1], 16)) != expected_identity["dev"]):
            raise Refused("kernel waiter inode differs")
        return line
    return None


CHILD_SIGNALS = {signal.SIGALRM, signal.SIGTERM, signal.SIGINT}


@contextmanager
def child_bookkeeping():
    """Defer raising handlers until kernel child or fd state is durably tracked here.

    Also used around every os.open of the tested mount and the store of its
    fd: a pending alarm then raises only once the cleanup path owns the fd.

    A signal that arrived just before the block is already flagged, so its
    handler raises from pthread_sigmask after the block took effect; unblock
    and re-raise rather than leave the signals blocked. This mask is never
    nested, so the signals were unblocked before the call.
    """
    try:
        previous = signal.pthread_sigmask(signal.SIG_BLOCK, CHILD_SIGNALS)
    except BaseException:
        signal.pthread_sigmask(signal.SIG_UNBLOCK, CHILD_SIGNALS)
        raise
    try:
        yield previous
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous)


class Child:
    def __init__(self, fixture, filename, family):
        self.fixture = fixture
        self.buffer = b""
        parent = child = None
        self.reaped = False
        self.pid = None
        try:
            with child_bookkeeping() as previous_mask:
                parent, child = socket.socketpair()
                self.sock = parent
                self.pid = os.fork()
                if self.pid == 0:
                    parent.close()
                    try:
                        # No raising parent handler or inherited blocked TERM
                        # may survive into a child whose cleanup we supervise.
                        for signum in CHILD_SIGNALS:
                            signal.signal(signum, signal.SIG_DFL)
                        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
                        signal.pthread_sigmask(signal.SIG_UNBLOCK, {signal.SIGTERM})
                        worker(child, fixture.directory, filename, family)
                    except BaseException:
                        os._exit(11)
                    os._exit(0)
                fixture.children.append(self)
                child.close()
            self.receive("ready")
        except BaseException:
            # A successfully forked PID is already registered before handlers
            # resume; Fixture.run retains it even when construction is interrupted.
            if parent is not None:
                parent.close()
            if child is not None:
                child.close()
            raise

    def send(self, op, **values):
        self.sock.sendall(json.dumps({"op": op, **values}).encode() + b"\n")

    def receive(self, expected):
        deadline = self.fixture.case_deadline
        while b"\n" not in self.buffer:
            remaining = min(deadline, self.fixture.deadline) - time.monotonic()
            if remaining <= 0 or not select.select([self.sock], [], [], max(0, remaining))[0]:
                raise TimeoutError("child protocol deadline")
            data = self.sock.recv(4096)
            if not data:
                raise Refused("incomplete child protocol")
            self.buffer += data
            if len(self.buffer) > 4096:
                raise Refused("oversized child response")
        line, self.buffer = self.buffer.split(b"\n", 1)
        event = json.loads(line)
        if event.get("pid") != self.pid or {k: event.get(k) for k in ("dev", "ino")} != self.fixture.file_identity or event.get("independent_open") is not True:
            raise Refused("child PID/inode/open evidence differs")
        self.fixture.events.append(event)
        if event.get("event") != expected:
            raise Refused("expected " + expected + ", received " + str(event))
        return event

    def command(self, op, expected, **values):
        self.send(op, **values)
        return self.receive(expected)

    def reap(self, deadline):
        while not self.reaped:
            with child_bookkeeping():
                pid, status = os.waitpid(self.pid, os.WNOHANG)
                if pid:
                    self.reaped = True
                    self.fixture.cleanup.append({"pid": pid, "status": status, "reaped": True})
                    self.sock.close()
                    return status == 0
            if time.monotonic() >= deadline:
                return False
            select.select([], [], [], 0.01)
        return True

    def exit(self):
        self.send("exit")
        if not self.reap(min(self.fixture.case_deadline, self.fixture.deadline)):
            raise Refused("child did not exit cleanly")


class Fixture:
    def __init__(self, root_fd, run_id, overall=30, case=3, scope="local-selftest"):
        self.root_fd, self.run_id, self.scope = root_fd, run_id, scope
        self.deadline = time.monotonic() + overall - 2
        self.case_seconds = case
        self.case_deadline = self.deadline
        self.children, self.events, self.checks, self.cleanup = [], [], [], []
        self.directory = None
        self.created_leaf = False
        self.leaf = "hfs-lock-" + run_id
        self.file_identity = None

    def begin(self, name):
        self.case_deadline = min(self.deadline, time.monotonic() + self.case_seconds)
        if time.monotonic() >= self.case_deadline:
            raise TimeoutError("overall fixture deadline")
        self.events.append({"case": name})

    def conflict(self, child):
        event = child.command("lock", "errno", mode="exclusive")
        if event["errno"] not in (errno.EACCES, errno.EAGAIN):
            raise Refused("unexpected conflict errno")

    def wait_then_release(self, child, family, release):
        child.send("lock", mode="exclusive", blocking=True)
        child.receive("entering-wait")
        # The handshake announces intent; /proc/locks proves the syscall really
        # entered a kernel wait. A scheduling delay alone can never pass.
        while True:
            with open("/proc/locks", encoding="ascii") as stream:
                raw = stream.read(1 << 20)
            if len(raw) == 1 << 20:
                raise Refused("kernel lock evidence exceeds bound")
            waiting = waiter_record(raw, child.pid, self.file_identity, family)
            if waiting is not None:
                self.events.append({"kernel_wait": waiting})
                break
            if time.monotonic() >= self.case_deadline:
                raise TimeoutError("no kernel entered-wait evidence")
            if select.select([child.sock], [], [], 0.01)[0]:
                child.receive("must-remain-blocked")
        if select.select([child.sock], [], [], 0)[0]:
            raise Refused("wait completed before release")
        release()
        child.receive("acquired")
        child.command("lock", "unlocked", mode="unlock")

    @contextmanager
    def lock_file(self, filename):
        """Create the case's lock file; yield its fd and always close it.

        The fd is registered with raising handlers deferred (child_bookkeeping),
        so an alarm pending since the open still reaches the finally (R10-1).
        """
        owned = []
        try:
            with child_bookkeeping():
                owned.append(os.open(filename, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                     0o600, dir_fd=self.directory))
            yield owned[0]
        finally:
            for each in owned:
                os.close(each)

    def family(self, family):
        self.begin(family + "-setup")
        filename = family + ".lock"
        with self.lock_file(filename) as fd:
            self.file_identity = identity(fd)
            contender = Child(self, filename, family)
            holder = Child(self, filename, family)
            self.nonblocking_and_blocking(fd, family, contender)
            self.lifetime(family, holder, contender)
            if family == "flock":
                self.flock_shared(fd, contender)
            contender.exit()

    def nonblocking_and_blocking(self, fd, family, contender):
        """Conflict (plus POSIX disjoint range and GETLK), then a blocking wakeup."""
        self.begin(family + "-nonblocking")
        lock(fd, family, "exclusive")
        self.conflict(contender)
        if family == "posix":
            contender.command("lock", "acquired", mode="exclusive", start=16, length=8)
            contender.command("lock", "unlocked", mode="unlock", start=16, length=8)
            result = contender.command("getlk", "getlk")["result"]
            if result != {"type": fcntl.F_WRLCK, "whence": os.SEEK_SET, "start": 0, "length": 8, "pid": os.getpid()}:
                raise Refused("GETLK failed conflicting type/range/PID oracle")
        self.checks.append(family + "-conflict" + ("-disjoint-getlk" if family == "posix" else ""))
        self.begin(family + "-blocking")
        self.wait_then_release(contender, family, lambda: lock(fd, family, "unlock"))
        self.checks.append(family + "-blocking-wakeup")

    def lifetime(self, family, holder, contender):
        """Lock lifetime across close/dup/reopen, then holder exit cleanup."""
        self.begin(family + "-lifetime")
        holder.command("lock", "acquired", mode="exclusive")
        if family == "posix":
            holder.command("close-other", "closed-other")
            contender.command("lock", "acquired", mode="exclusive")
            contender.command("lock", "unlocked", mode="unlock")
            self.checks.append("posix-close-any-fd")
            holder.command("lock", "acquired", mode="exclusive")
        else:
            holder.command("dup", "duplicated")
            holder.command("close-original", "closed-original")
            self.conflict(contender)
            holder.command("close-other", "closed-other")
            self.conflict(contender)
            self.checks.append("flock-dup-and-unrelated-close-retain")
            holder.command("close-final", "closed-final")
            contender.command("lock", "acquired", mode="exclusive")
            contender.command("lock", "unlocked", mode="unlock")
            self.checks.append("flock-final-description-close")
            holder.command("reopen", "reopened")
            holder.command("lock", "acquired", mode="exclusive")
        holder.exit()
        contender.command("lock", "acquired", mode="exclusive")
        contender.command("lock", "unlocked", mode="unlock")
        self.checks.append(family + "-exit-cleanup")

    def flock_shared(self, fd, contender):
        self.begin("flock-shared")
        lock(fd, "flock", "shared")
        contender.command("lock", "acquired", mode="shared")
        self.conflict(contender)
        contender.command("lock", "unlocked", mode="unlock")
        lock(fd, "flock", "unlock")
        self.checks.append("flock-shared-shared-exclusive-conflict")

    def _cleanup(self, error):
        """TERM and reap every live child, close the leaf; return the first error."""
        # The production work alarm has a two-second cleanup reservation.
        # Never let that same handler interrupt cleanup and discard PID inventory.
        if self.scope == "same-guest-same-9p-mount":
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            signal.signal(signal.SIGINT, signal.SIG_IGN)
        cleanup_deadline = time.monotonic() + 1.5
        for child in self.children:
            if not child.reaped:
                try:
                    os.kill(child.pid, signal.SIGTERM)
                    self.cleanup.append({"pid": child.pid, "term_sent": True})
                except ProcessLookupError:
                    pass
                except OSError as failure:
                    error = error or "TERM cleanup failed: " + str(failure)
        for child in self.children:
            if not child.reaped:
                try:
                    child.reap(cleanup_deadline)
                except OSError as failure:
                    error = error or "reap cleanup failed: " + str(failure)
        if self.directory is not None:
            try:
                os.close(self.directory)
            except OSError as failure:
                error = error or "directory close failed: " + str(failure)
        return error

    def run(self):
        check_abi()
        error = None
        try:
            os.mkdir(self.leaf, 0o700, dir_fd=self.root_fd)
            self.created_leaf = True
            # Stored with raising handlers deferred so the finally always closes it.
            with child_bookkeeping():
                self.directory = os.open(self.leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                         dir_fd=self.root_fd)
            self.family("posix")
            self.family("flock")
        except BaseException as failure:
            error = type(failure).__name__ + ": " + str(failure)
        finally:
            error = self._cleanup(error)
        uncertain = [child.pid for child in self.children if not child.reaped]
        if uncertain or any(item.get("status", 0) != 0 or item.get("term_sent") for item in self.cleanup):
            error = error or "child cleanup/nonzero exit failure"
        return {"protocol": PROTOCOL, "scope": self.scope, "passed": error is None,
                "error": error, "checks": self.checks, "events": self.events,
                "child_cleanup": self.cleanup, "uncertain_children": uncertain,
                "preserved_leaf": self.leaf if self.created_leaf else None,
                "limits": {"files": 2, "payload_bytes": 0, "concurrent_children": 2,
                           "total_children": 4, "owned_fds": 12, "report_bytes": MAX_REPORT},
                "publication_identity_verified": False,
                "security_admission_verified": False,
                "parent_guard_oracle_required": True}


def main():
    result = {"protocol": PROTOCOL, "passed": False, "scope": "same-guest-same-9p-mount",
              "publication_identity_verified": False, "security_admission_verified": False,
              "parent_guard_oracle_required": True}
    owned = []  # The retained root fd; admit appends it with handlers deferred.
    started = time.monotonic()
    try:
        if len(sys.argv) != 1:
            raise Refused("no CLI options; one bounded JSON request on stdin")
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("entrypoint deadline")))
        signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(InterruptedError("entrypoint terminated")))
        signal.setitimer(signal.ITIMER_REAL, ENTRY_SECONDS)
        raw = sys.stdin.buffer.read(MAX_REQUEST + 1)
        if len(raw) > MAX_REQUEST:
            raise Refused("oversized request")
        request = json.loads(raw)
        check_abi()
        validate_request(request)
        budget = request["budgets"]
        work_deadline = started + budget["overall_seconds"] - 2
        remaining = work_deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("request exhausted approved overall budget")
        signal.setitimer(signal.ITIMER_REAL, remaining)
        fd, observed = admit(request, owned)
        remaining = work_deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("admission exhausted approved overall budget")
        result = Fixture(fd, request["run_id"], remaining + 2, budget["case_seconds"], result["scope"]).run()
        result["transport_observed"] = observed
    except BaseException as error:
        result["error"] = type(error).__name__ + ": " + str(error)
    finally:
        # Cancel the alarm first so no deadline can interrupt the close.
        signal.setitimer(signal.ITIMER_REAL, 0)
        for each in owned:
            os.close(each)
    encoded = json.dumps(result, sort_keys=True)
    if len(encoded.encode()) > MAX_REPORT:
        encoded = json.dumps({"protocol": PROTOCOL, "passed": False, "error": "report exceeded bound; resources uncertain"})
    print(encoded)
    return 0 if result.get("passed") and len(encoded.encode()) <= MAX_REPORT and "report exceeded" not in encoded else 10


if __name__ == "__main__":
    sys.exit(main())
