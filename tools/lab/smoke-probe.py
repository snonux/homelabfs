#!/usr/bin/env python3
"""Bounded L02-L06/L09 probe, invoked INSIDE an approved Rocky guest.

The pinned deployment adapter supplies one hfs-live-v1 request on stdin and
wraps this probe's evidence in its end-to-end terminal receipt. This program
does not provision, mount, unmount, delete volumes, SSH, or touch other paths.
Run as root for the bounded numeric-identity permission checks. Never invoke
directly as a substitute for the separately approved Stage 2 runner.
"""
import fcntl
from contextlib import contextmanager
import hashlib
import json
import mmap
import os
import resource
import signal
import sqlite3
import stat
import sys
import time


CHILD_SIGNALS = {signal.SIGALRM, signal.SIGTERM, signal.SIGINT}
CHILD_CLEANUP_SECONDS = 1.5


@contextmanager
def child_bookkeeping():
    """Defer raising handlers only while recording kernel child transitions."""
    previous = signal.pthread_sigmask(signal.SIG_BLOCK, CHILD_SIGNALS)
    try:
        yield previous
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous)


class Child:
    def __init__(self, probe):
        self.probe = probe
        self.pid = None
        self.reaped = False
        self.status = None
        self.ownership_lost = False

    def start(self, action):
        with child_bookkeeping() as previous_mask:
            self.pid = os.fork()
            if self.pid == 0:
                try:
                    for signum in CHILD_SIGNALS:
                        signal.signal(signum, signal.SIG_DFL)
                    signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
                    action()
                except BaseException:
                    os._exit(1)
                os._exit(0)
            # Registration precedes delivery of any signal queued after fork.
            self.probe.children.append(self)

    def reap(self, deadline=None):
        while not self.reaped:
            with child_bookkeeping():
                try:
                    pid, status = os.waitpid(self.pid, os.WNOHANG)
                except ChildProcessError:
                    # No status means no proof of termination under our ownership.
                    # In particular, never send a signal to this numeric PID again.
                    self.ownership_lost = True
                    raise
                if pid:
                    self.status = status
                    self.reaped = True
                    self.probe.child_cleanup.append({"pid": pid, "status": status, "reaped": True})
            if self.reaped:
                break
            if deadline is not None and time.monotonic() >= deadline:
                return False
            # A blocking wait under the mask would suppress the entry alarm.
            time.sleep(0.01)
        return os.WIFEXITED(self.status) and os.WEXITSTATUS(self.status) == 0

    def cleanup(self):
        try:
            if self.pid is None or self.reaped:
                return
            with child_bookkeeping():
                if self.ownership_lost:
                    return
                try:
                    os.kill(self.pid, signal.SIGKILL)
                    self.probe.child_cleanup.append({"pid": self.pid, "kill_sent": True})
                except ProcessLookupError:
                    pass
            self.reap(time.monotonic() + CHILD_CLEANUP_SECONDS)
        except BaseException as error:
            # Preserve the original failure, including an entry alarm, even if
            # another signal or a kernel error interrupts bounded cleanup.
            self.probe.child_cleanup.append({"pid": self.pid, "cleanup_error": str(error)})


class Probe:
    def __init__(self, request):
        self.request = request
        self.plan = request["plan"]
        self.live = self.plan["live"]
        self.volume = next(v for v in self.live["volumes"] if v["id"] == request["volume"])
        self.op = request["operation"]
        self.byte_limit = self.plan["budget"]["io_bytes"] // (len(self.live["volumes"]) * 7)
        self.op_limit = self.live["max_operations"] // (len(self.live["volumes"]) * 7)
        self.bytes = 0
        self.operations = 0
        self.history = []
        self.checks = []
        self.children = []
        self.child_cleanup = []

    def child_evidence(self):
        return {"child_cleanup": self.child_cleanup,
                "uncertain_children": [child.pid for child in self.children if not child.reaped]}

    def reserve(self, count, size=0):
        self.operations += count
        self.bytes += size
        if self.operations > self.op_limit or self.bytes > self.byte_limit:
            raise RuntimeError("approved operation/I/O reservation exhausted")

    def check(self, name, assertion):
        if not assertion:
            raise RuntimeError(name)
        self.checks.append(name)

    def enter(self):
        v = self.volume
        run_id = self.plan["run_id"]
        if (self.request["protocol"] != "hfs-live-v1" or self.plan["stage"] != "stage2"
                or self.plan["profile"] != "live-smoke" or self.plan["disruptions"]
                or not v["id"].startswith("hfs-smoke-" + run_id + "-")
                or v["guest_mount"] != "/mnt/hfs-smoke/" + v["id"]
                or v["export"] != v["id"]):
            raise RuntimeError("invalid Stage 2 smoke namespace")
        mount = v["guest_mount"]
        if os.path.realpath(mount) != mount:
            raise RuntimeError("smoke mount contains a symlink")
        candidates = []
        with open("/proc/self/mountinfo", encoding="utf-8") as source:
            for line in source:
                left, right = line.rstrip().split(" - ", 1)
                fields, fs = left.split(), right.split()
                if fields[4] == mount and fs[0] == "9p" and fs[1] == v["export"]:
                    candidates.append(fields[0])
        if len(candidates) != 1:
            raise RuntimeError("expected exact deployed 9p mount/export is unavailable")
        fd = os.open(mount, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            with open("/proc/self/fdinfo/" + str(fd), encoding="ascii") as source:
                mounted_id = next(line.split()[1] for line in source if line.startswith("mnt_id:"))
            if mounted_id != candidates[0]:
                raise RuntimeError("mount changed during verification")
            leaf = "hfs-smoke-" + run_id + "-" + self.op
            # Exclusive creation makes retries fail closed and preserves failure
            # evidence. Existing directories are never adopted or removed.
            os.mkdir(leaf, 0o700, dir_fd=fd)
            child = os.open(leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            try:
                os.fchdir(child)
            finally:
                os.close(child)
        finally:
            os.close(fd)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("probe deadline")))
        signal.alarm(max(1, self.request["step"]["timeout_seconds"] - 2))
        resource.setrlimit(resource.RLIMIT_FSIZE, (min(v["max_bytes"], 1 << 20),) * 2)
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))

    def bytes_io(self):
        data = hashlib.sha256(self.request["plan_hash"].encode()).digest() * 128
        self.reserve(8, len(data) * 2)
        with open("payload", "xb", buffering=0) as out:
            out.write(data)
            os.fsync(out.fileno())
        with open("payload", "rb", buffering=0) as source:
            actual = source.read(len(data) + 1)
        self.check("write-fsync-reopen-checksum", actual == data)
        self.history.append({"sha256": hashlib.sha256(actual).hexdigest(), "length": len(actual)})

    def semantics(self):
        self.reserve(28, 128)
        os.mkdir("directory")
        os.rmdir("directory")
        self.check("mkdir-rmdir", not os.path.exists("directory"))
        with open("a", "xb") as out:
            out.write(b"first")
        with open("b", "xb") as out:
            out.write(b"second")
        os.replace("a", "b")
        with open("b", "rb+") as out:
            self.check("rename-replace", out.read() == b"first")
            out.truncate(2)
            out.seek(0)
            self.check("truncate", out.read() == b"fi")
        os.link("b", "link")
        self.check("hardlink", os.stat("b").st_ino == os.stat("link").st_ino and os.stat("b").st_nlink == 2)
        with open("link", "rb") as source:
            os.unlink("link")
            self.check("unlink-open", source.read() == b"fi")

    def child(self, action):
        child = Child(self)
        try:
            child.start(action)
            return child.reap()
        except BaseException:
            child.cleanup()
            raise

    def permissions(self):
        self.reserve(20, 32)
        if os.geteuid() != 0:
            raise RuntimeError("permission probe requires root to select both numeric test identities")
        uid, gid, denied = self.live["uid"], self.live["gid"], self.live["denied_uid"]
        if min(uid, gid, denied) <= 0 or uid == denied:
            raise RuntimeError("unsafe numeric identities")
        with open("private", "xb") as out:
            out.write(b"private")
        os.chown("private", uid, gid)
        os.chmod("private", 0o600)
        st = os.stat("private")
        self.check("uid-gid-mode", (st.st_uid, st.st_gid, stat.S_IMODE(st.st_mode)) == (uid, gid, 0o600))
        os.chmod(".", 0o755)

        def access(identity, allowed):
            os.setgroups([])
            os.setgid(gid)
            os.setuid(identity)
            try:
                with open("private", "rb") as source:
                    data = source.read(8)
            except PermissionError:
                if allowed:
                    raise
            else:
                if not allowed or data != b"private":
                    raise RuntimeError("unexpected permission result")

        self.check("allowed-access", self.child(lambda: access(uid, True)))
        self.check("denied-access", self.child(lambda: access(denied, False)))

    def locking(self):
        self.reserve(30, 16384)
        with open("locked", "xb+") as out:
            for name, acquire, release in (
                ("flock-contention", lambda f: fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB), lambda f: fcntl.flock(f, fcntl.LOCK_UN)),
                ("fcntl-contention", lambda f: fcntl.lockf(f, fcntl.LOCK_EX | fcntl.LOCK_NB), lambda f: fcntl.lockf(f, fcntl.LOCK_UN)),
            ):
                acquire(out)

                def contend():
                    # Close the inherited description before a separate open;
                    # the parent's lock remains held in the other process.
                    os.close(out.fileno())
                    with open("locked", "rb+") as other:
                        try:
                            acquire(other)
                        except BlockingIOError:
                            return
                        raise RuntimeError("contending process obtained held lock")

                blocked = self.child(contend)
                release(out)

                def available():
                    os.close(out.fileno())
                    with open("locked", "rb+") as other:
                        acquire(other)
                        release(other)

                self.check(name, blocked and self.child(available))
            out.truncate(4096)
            with mmap.mmap(out.fileno(), 4096) as mapping:
                mapping[:4] = b"mmap"
                mapping.flush()
            os.fsync(out.fileno())
        with open("locked", "rb") as source:
            self.check("mmap-flush-sync", source.read(4) == b"mmap")

    def databases(self):
        # Fixed four tiny transactions and 8-page main databases. Reserve a
        # conservative 1 MiB of logical I/O for journal/WAL/checkpoint traffic.
        self.reserve(96, 1 << 20)
        for mode, check in (("DELETE", "sqlite-delete-full"), ("WAL", "sqlite-wal-full")):
            path = "database-" + mode
            connection = sqlite3.connect(path, timeout=0.2)
            try:
                connection.execute("PRAGMA page_size=4096")
                connection.execute("PRAGMA max_page_count=8")
                actual = connection.execute("PRAGMA journal_mode=" + mode).fetchone()[0]
                connection.execute("PRAGMA synchronous=FULL")
                if actual.lower() != mode.lower() or connection.execute("PRAGMA synchronous").fetchone()[0] != 2:
                    raise RuntimeError("requested SQLite journal/sync mode unavailable")
                connection.execute("CREATE TABLE smoke (value INTEGER)")
                connection.commit()
                connection.execute("INSERT INTO smoke VALUES (42)")
                connection.commit()
                self.check(check, True)
            finally:
                connection.close()
            connection = sqlite3.connect(path, timeout=0.2)
            try:
                if (connection.execute("PRAGMA integrity_check").fetchone()[0] != "ok"
                        or connection.execute("SELECT value FROM smoke").fetchall() != [(42,)]):
                    raise RuntimeError("SQLite reopen/integrity mismatch")
            finally:
                connection.close()
        self.check("sqlite-reopen-integrity", True)

    def mix(self):
        self.reserve(64, 16 * 1024)
        for i in range(8):
            data = hashlib.sha256((self.request["plan_hash"] + str(i)).encode()).digest() * 16
            with open("mixed-" + str(i), "xb", buffering=0) as out:
                out.write(data)
                os.fsync(out.fileno())
            with open("mixed-" + str(i), "rb", buffering=0) as source:
                if source.read(513) != data:
                    raise RuntimeError("mixed I/O checksum mismatch")
        self.check("bounded-mix", True)

    def run(self):
        operations = {"L02": self.bytes_io, "L03": self.semantics, "L04": self.permissions,
                      "L05": self.locking, "L06": self.databases, "L09": self.mix}
        action = operations[self.op]
        self.enter()
        action()
        return {"checks": self.checks, "io_bytes": self.bytes, "operations": self.operations,
                "history": self.history, "probe": "hfs-rocky-smoke-v1", **self.child_evidence()}


def main():
    raw = sys.stdin.buffer.read((4 << 20) + 1)
    if len(raw) > 4 << 20:
        raise RuntimeError("oversized probe request")
    request = json.loads(raw)
    probe = Probe(request)
    try:
        result = probe.run()
    except BaseException as error:
        result = {"probe": "hfs-rocky-smoke-v1", "error": str(error), **probe.child_evidence()}
        print(json.dumps(result, sort_keys=True))
        # Exit 10 certifies a completed failure only after every owned child
        # was reaped. Other exits require the adapter to retain uncertainty.
        return 1 if result["uncertain_children"] else 10
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        code = main()
    except BaseException as error:
        print(json.dumps({"probe": "hfs-rocky-smoke-v1", "error": str(error)}))
        # main() certifies completed failures itself. If reporting or any
        # other outer operation failed, completion is unknown here.
        sys.exit(1)
    sys.exit(code)
