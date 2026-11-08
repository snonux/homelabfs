#!/usr/bin/env python3
"""Real bounded guest syscalls. Success is not host durability/PTE acceptance.

One private controller request arrives on stdin; JSONL invocation/reply evidence
goes to stdout outside the tested filesystem. No mounting, lifecycle or cleanup
deletion is performed. Failed resources and exact errno remain for inspection.

A successful case RETAINS its payload: host source/journal correlation must be
captured against the still-present files before anything is removed. The case
writes a root-private retention record (token hash plus exact inventory) beside
the run mounts, outside the tested volume, and emits the one-shot release token
only to the controller's private stdout. A later, separately dispatched release
request must pass full re-admission and present that token, the unchanged
inventory digest and the host correlation digest; only then are the exact known
payload entries unlinked. Missing, changed or replayed releases refuse and
preserve the payload for inspection.
"""
import ctypes
import errno
import hashlib
import hmac
import json
import mmap
import os
import re
import resource
import secrets
import select
import signal
import stat
import sys
import time

VERSION = "homelabfs-retained9p-syscalls/1"
# Run-owned local directory holding the m0/m1 mount points and retention records.
RUN_PARENT = "/var/tmp/hfs-zq3-"
TRUSTED_UID = 0  # Trusted guest root; local tests substitute their own UID.
RELEASE_VERSION = "homelabfs-retained9p-release/1"
RECORD_VERSION = "homelabfs-retained9p-retention/1"
REQUEST_FIELDS = {"version", "run_id", "boot_id", "provider_nonce", "manifest_sha256",
                  "mounts", "case", "seconds"}
# A release names the retained case and carries the one-shot token, the exact
# inventory digest it expects and the digest of the host correlation captured
# while the payload was still present.
RELEASE_FIELDS = REQUEST_FIELDS | {"token", "inventory_sha256", "correlation_sha256"}
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
RECORD_LIMIT = 65536
REQUEST_LIMIT = 16384
REPLY_LIMIT = 65536
PAYLOAD = 4 << 20
DENIED = {errno.EACCES, errno.EPERM, errno.EOPNOTSUPP}
# Exact entries each successful case leaves; directories are removed after files.
PAYLOAD_FILES = {
    "credentials": ["setgid/created", "group", "group-tail", "sticky/owned"],
    "sync": ["ordinary", "sync", "dsync", "mapped"],
    "namespace": ["namespace"],
    "mapped-pressure": ["full"],
    "metadata": ["renamed", "linked"],
}
PAYLOAD_DIRS = {"credentials": ["setgid", "sticky"]}


class Refused(RuntimeError):
    """Admission or a required assertion refused."""


def unique(pairs):
    value = {}
    for key, child in pairs:
        if key in value:
            raise Refused("duplicate JSON key")
        value[key] = child
    return value


def read(path, limit):
    with open(path, "rb") as stream:
        value = stream.read(limit + 1)
    if len(value) > limit:
        raise Refused("read bound: " + path)
    return value


def emit(kind, **fields):
    raw = json.dumps({"version": VERSION, "event": kind,
                      "monotonic_ns": time.monotonic_ns(), **fields},
                     sort_keys=True, separators=(",", ":"))
    if len(raw.encode()) > REPLY_LIMIT:
        raise Refused("reply bound")
    print(raw, flush=True)


def decode_mount(value):
    return re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), value)


def fd_mount(fd):
    for line in read("/proc/self/fdinfo/" + str(fd), 4096).decode().splitlines():
        if line.startswith("mnt_id:"):
            return int(line.split()[1])
    raise Refused("descriptor mount identity unavailable")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def is_release(request):
    """Closed request shapes: a syscall case or the release of its payload."""
    if not isinstance(request, dict):
        raise Refused("request shape/version")
    if request.get("version") == VERSION and set(request) == REQUEST_FIELDS:
        return False
    if request.get("version") == RELEASE_VERSION and set(request) == RELEASE_FIELDS:
        for name in ["token", "inventory_sha256", "correlation_sha256"]:
            if not isinstance(request[name], str) or not HEX64.fullmatch(request[name]):
                raise Refused("release token/digest shape")
        return True
    raise Refused("request shape/version")


def match_mount(lines, path):
    """Return the single mountinfo tuple for path; absence/duplicates refuse."""
    matches = []
    for line in lines:
        left, right = line.split(" - ", 1)
        fields, fs = left.split(), right.split()
        if decode_mount(fields[4]) == path:
            matches.append((int(fields[0]), fs[0], decode_mount(fs[1]),
                            set(fields[5].split(",")), set(fs[2].split(","))))
    if len(matches) != 1:
        raise Refused("exact mountpoint not present once")
    return matches[0]


def admit(request):
    """Full admission for every request, a release included; no reduced path."""
    is_release(request)
    admit_identity(request)
    return open_domains(request)


def admit_identity(request):
    """Bounds plus actual boot, installed manifest, SELinux and provider inventory."""
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,23}", request["run_id"]):
        raise Refused("run ID bound")
    if type(request["seconds"]) is not int or not 1 <= request["seconds"] <= 60:
        raise Refused("case deadline bound")
    if request["case"] not in CASES or len(request["mounts"]) != 2:
        raise Refused("case/two-domain inventory")
    if os.geteuid() != 0 or sys.platform != "linux":
        raise Refused("trusted Linux guest root required")
    if read("/proc/sys/kernel/random/boot_id", 64).decode().strip() != request["boot_id"]:
        raise Refused("guest boot changed")
    manifest = read("/etc/homelabfs/installed-admission-v1.json", 65536)
    if hashlib.sha256(manifest).hexdigest() != request["manifest_sha256"]:
        raise Refused("installed manifest changed")
    if read("/sys/fs/selinux/enforce", 2).strip() != b"1":
        raise Refused("SELinux enforcing required")
    observed = json.loads(read("/sys/kernel/homelabfs/admission_v1", 131072),
                          object_pairs_hook=unique)
    if (observed["boot_id"] != request["boot_id"] or
            observed["provider_nonce"] != request["provider_nonce"]):
        raise Refused("provider incarnation changed")
    scopes = observed["domains"]
    if sorted(row["session"] for row in scopes) != sorted(row["session"] for row in request["mounts"]):
        raise Refused("complete admitted domain inventory changed")


def open_domains(request):
    """Open both exact controlled 9p mounts as independent retained root fds."""
    lines = read("/proc/self/mountinfo", 1 << 20).decode().splitlines()
    fds, devices, ids = [], [], []
    try:
        for row in request["mounts"]:
            if set(row) != {"path", "tag", "mount_id", "session"}:
                raise Refused("mount row shape")
            path = row["path"]
            if (path != RUN_PARENT + request["run_id"] + "/m" + str(len(fds)) or
                    os.path.normpath(path) != path or os.path.realpath(path) != path):
                raise Refused("private canonical run mount path required")
            mount_id, kind, tag, options, super_options = match_mount(lines, path)
            combined = options | super_options
            if (mount_id != row["mount_id"] or kind != "9p" or tag != row["tag"] or
                    not {"rw", "cache=mmap", "version=9P2000.L.hfs1.4",
                         "msize=65536"} <= combined):
                raise Refused("exact controlled mount tuple differs")
            fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            fds.append(fd)
            if fd_mount(fd) != mount_id:
                raise Refused("opened descriptor differs from admitted mount")
            devices.append(os.fstat(fd).st_dev)
            ids.append(mount_id)
        if len(set(ids)) != 2 or len(set(devices)) != 2:
            raise Refused("two independent kernel mount/cache domains required")
        emit("admitted", boot_id=request["boot_id"],
             provider_nonce=request["provider_nonce"], mounts=request["mounts"],
             devices=devices, user_namespace=os.readlink("/proc/self/ns/user"),
             publication=None, acceptance=False)
        return fds
    except BaseException:
        for fd in fds:
            os.close(fd)
        raise


def checked(condition, message):
    if not condition:
        raise Refused(message)


class Probe:
    def __init__(self, request, roots):
        self.request, self.roots = request, roots
        self.sequence = 0
        self.writes = 0
        self.children = set()

    def call(self, operation, function, **args):
        self.sequence += 1
        checked(self.sequence <= 256, "operation budget")
        emit("invoke", sequence=self.sequence, operation=operation, args=args)
        try:
            value = function()
        except OSError as failure:
            emit("reply", sequence=self.sequence, operation=operation,
                 errno=failure.errno, result=None)
            raise
        emit("reply", sequence=self.sequence, operation=operation, errno=0,
             result=value if isinstance(value, (int, str, dict, list)) else None)
        return value

    def write(self, fd, data, offset=0):
        self.writes += len(data)
        checked(self.writes <= 12 << 20, "cumulative issued write budget")
        return self.call("pwrite", lambda: os.pwrite(fd, data, offset),
                         bytes=len(data), sha256=hashlib.sha256(data).hexdigest(), offset=offset)

    def leaf_name(self):
        return "zq3-" + self.request["run_id"] + "-" + self.request["case"]

    def open_leaf(self):
        return os.open(self.leaf_name(), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                       dir_fd=self.roots[0])

    def directory(self):
        leaf = self.leaf_name()
        self.call("mkdir-exclusive", lambda: os.mkdir(leaf, 0o777, dir_fd=self.roots[0]), name=leaf)
        return self.open_leaf(), leaf

    def child(self, action, uid=None, gid=None, groups=None):
        receive, send = os.pipe2(os.O_CLOEXEC)
        pid = os.fork()
        if pid == 0:
            os.close(receive)
            try:
                signal.alarm(8)
                if groups is not None:
                    os.setgroups(groups)
                    os.setgid(gid)
                    os.setuid(uid)
                result = {"uid": os.getuid(), "gid": os.getgid(),
                          "groups": os.getgroups(), "result": action()}
                raw = json.dumps(result).encode()
                checked(len(raw) < 16384, "child reply size")
                os.write(send, raw)
                os._exit(0)
            except BaseException as failure:
                raw = json.dumps({"error": type(failure).__name__,
                                  "errno": getattr(failure, "errno", None),
                                  "detail": str(failure)[:256]}).encode()
                os.write(send, raw)
                os._exit(1)
        os.close(send)
        self.children.add(pid)
        try:
            chunks, total = [], 0
            deadline = time.monotonic() + 9
            while True:
                remaining = deadline - time.monotonic()
                checked(remaining > 0 and select.select([receive], [], [], remaining)[0],
                        "child reply timeout")
                chunk = os.read(receive, 16384)
                if not chunk:
                    break
                total += len(chunk)
                checked(total <= 16384, "child reply bound")
                chunks.append(chunk)
            raw = b"".join(chunks)
            reaped, status = os.waitpid(pid, os.WNOHANG)
            if reaped == 0:
                # A complete reply precedes normal exit by a bounded instant.
                deadline = time.monotonic() + 0.5
                while reaped == 0 and time.monotonic() < deadline:
                    time.sleep(0.01)
                    reaped, status = os.waitpid(pid, os.WNOHANG)
            checked(reaped == pid, "child has not exited; preserve for inspection")
            self.children.remove(pid)
            result = json.loads(raw, object_pairs_hook=unique)
            emit("child", pid=pid, status=status, observed=result)
            return result, status
        finally:
            os.close(receive)


class Retention:
    """Retain a successful payload until an authenticated post-correlation release.

    The record lives in the root-private local run directory that holds both
    mount points, never inside a tested domain, so it consumes no canary bytes
    and cannot be altered through the 9p path under test. Only the token's hash
    is stored; the token itself reaches the controller through private stdout.
    """

    def __init__(self, probe):
        self.probe = probe
        self.request = probe.request

    def record_name(self, prefix="release-"):
        return prefix + self.request["case"] + ".json"

    def run_directory(self):
        path = RUN_PARENT + self.request["run_id"]
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            info = os.fstat(fd)
            domains = {os.fstat(root).st_dev for root in self.probe.roots}
            checked(info.st_uid == TRUSTED_UID and not info.st_mode & 0o022 and
                    info.st_dev not in domains, "root-private local run directory required")
        except BaseException:
            os.close(fd)
            raise
        return fd

    def inventory(self, fd):
        """Exact payload identity: names, types, inode, owner, mode, size, links."""
        case = self.request["case"]
        entries = []
        for name in PAYLOAD_DIRS.get(case, []) + PAYLOAD_FILES[case]:
            seen = os.stat(name, dir_fd=fd, follow_symlinks=False)
            regular = stat.S_ISREG(seen.st_mode)
            checked(regular or stat.S_ISDIR(seen.st_mode), "payload entry type")
            entries.append({"name": name, "type": "file" if regular else "dir",
                            "ino": seen.st_ino, "uid": seen.st_uid, "gid": seen.st_gid,
                            "mode": stat.S_IMODE(seen.st_mode),
                            "size": seen.st_size if regular else None,
                            "nlink": seen.st_nlink if regular else None})
        return {"leaf": self.probe.leaf_name(), "leaf_ino": os.fstat(fd).st_ino,
                "listing": sorted(os.listdir(fd)), "entries": entries}

    def current_inventory(self):
        fd = self.probe.open_leaf()
        try:
            return self.inventory(fd)
        finally:
            os.close(fd)

    def write_record(self, record):
        run = self.run_directory()
        try:
            out = os.open(self.record_name(), os.O_CREAT | os.O_EXCL | os.O_WRONLY |
                          os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=run)
            try:
                raw = canonical(record)
                checked(os.write(out, raw) == len(raw), "short retention record write")
                os.fsync(out)
            finally:
                os.close(out)
            os.fsync(run)
        finally:
            os.close(run)

    def retain(self):
        """Keep the payload for host correlation; persist only the token hash."""
        inventory = self.current_inventory()
        expected = sorted({name.split("/", 1)[0] for name in PAYLOAD_FILES[self.request["case"]]})
        checked(inventory["listing"] == expected, "unexpected payload entries")
        token = secrets.token_hex(32)
        inventory_sha256 = hashlib.sha256(canonical(inventory)).hexdigest()
        self.write_record({
            "version": RECORD_VERSION, "run_id": self.request["run_id"],
            "case": self.request["case"], "boot_id": self.request["boot_id"],
            "provider_nonce": self.request["provider_nonce"], "inventory": inventory,
            "inventory_sha256": inventory_sha256,
            "token_sha256": hashlib.sha256(token.encode()).hexdigest()})
        # The token leaves only through the controller's private stdout log.
        emit("payload-retained", leaf=inventory["leaf"], inventory=inventory,
             inventory_sha256=inventory_sha256, release_token=token,
             required_host_correlation=True)

    def read_record(self, run):
        fd = os.open(self.record_name(), os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=run)
        try:
            info = os.fstat(fd)
            checked(stat.S_ISREG(info.st_mode) and info.st_uid == TRUSTED_UID and
                    not info.st_mode & 0o077 and info.st_size <= RECORD_LIMIT,
                    "root-private bounded retention record required")
            raw = os.read(fd, RECORD_LIMIT + 1)
        finally:
            os.close(fd)
        checked(len(raw) <= RECORD_LIMIT, "retention record bound")
        return json.loads(raw, object_pairs_hook=unique)

    def authenticate(self, record):
        request = self.request
        checked(record.get("version") == RECORD_VERSION and
                all(record.get(key) == request[key] for key in
                    ["run_id", "case", "boot_id", "provider_nonce", "inventory_sha256"]),
                "retention record differs from release request")
        presented = hashlib.sha256(request["token"].encode()).hexdigest()
        checked(hmac.compare_digest(str(record.get("token_sha256")), presented),
                "release token refused")
        current = self.current_inventory()
        checked(current == record.get("inventory") and
                hashlib.sha256(canonical(current)).hexdigest() == request["inventory_sha256"],
                "retained payload changed since correlation")

    def release(self):
        """One-shot release after authentication; any refusal preserves all."""
        case, p = self.request["case"], self.probe
        run = self.run_directory()
        try:
            self.authenticate(self.read_record(run))
            # Consume before deleting: a replay finds no record and refuses.
            os.rename(self.record_name(), self.record_name("released-"),
                      src_dir_fd=run, dst_dir_fd=run)
            os.fsync(run)
        finally:
            os.close(run)
        fd = p.open_leaf()
        try:
            for name in PAYLOAD_FILES[case]:
                p.call("unlink-released-payload", lambda n=name: os.unlink(n, dir_fd=fd), name=name)
            for name in PAYLOAD_DIRS.get(case, []):
                p.call("rmdir-released-payload", lambda n=name: os.rmdir(n, dir_fd=fd), name=name)
        finally:
            os.close(fd)
        p.call("rmdir-released-leaf", lambda: os.rmdir(p.leaf_name(), dir_fd=p.roots[0]),
               name=p.leaf_name())
        emit("payload-released", leaf=p.leaf_name(), journal_preserved=True,
             correlation_sha256=self.request["correlation_sha256"])


def credentials(p):
    directory, leaf = p.directory()
    try:
        os.fchmod(directory, 0o777)
        fd = os.open("group", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o640, dir_fd=directory)
        try:
            os.fchown(fd, 41002, 42002)
            os.fchmod(fd, 0o640)
            p.write(fd, b"group-secret")
        finally:
            os.close(fd)

        def access():
            fd = os.open("group", os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory)
            try:
                return os.read(fd, 64).decode()
            finally:
                os.close(fd)

        allowed, status = p.child(access, 41001, 42001, [42001, 42002, 42003])
        checked(status == 0 and allowed["result"] == "group-secret" and
                allowed["groups"] == [42001, 42002, 42003], "supplementary group access")
        denied, status = p.child(access, 41001, 42001, [42001, 42003])
        checked(status != 0 and denied.get("errno") == errno.EACCES,
                "same UID different complete groups must deny")
        tail = os.open("group-tail", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o640,
                       dir_fd=directory)
        try:
            os.fchown(tail, 41002, 44022)
            os.fchmod(tail, 0o640)
            p.write(tail, b"full-group-tail")
        finally:
            os.close(tail)
        full_groups = [42001, *range(43000, 44023)]
        checked(len(full_groups) == 1024, "complete maximum group-vector fixture")

        def access_tail():
            fd = os.open("group-tail", os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory)
            try:
                return os.read(fd, 64).decode()
            finally:
                os.close(fd)

        allowed, status = p.child(access_tail, 41001, 42001, full_groups)
        checked(status == 0 and allowed["groups"] == full_groups and
                allowed["result"] == "full-group-tail", "all 1024 groups including final entry must survive")
        denied, status = p.child(access_tail, 41001, 42001, full_groups[:-1])
        checked(status != 0 and denied.get("errno") == errno.EACCES,
                "removing only the final supplementary group must deny")
        os.mkdir("setgid", 0o2777, dir_fd=directory)
        sg = os.open("setgid", os.O_RDONLY | os.O_DIRECTORY, dir_fd=directory)
        try:
            os.fchown(sg, 0, 42002)
            os.fchmod(sg, 0o2777)

            def create():
                old = os.umask(0o027)
                try:
                    fd = os.open("created", os.O_CREAT | os.O_EXCL | os.O_RDWR,
                                 0o666, dir_fd=sg)
                    try:
                        value = os.fstat(fd)
                        return [value.st_uid, value.st_gid, stat.S_IMODE(value.st_mode)]
                    finally:
                        os.close(fd)
                finally:
                    os.umask(old)

            result, status = p.child(create, 41001, 42001, [42001, 42002])
            checked(status == 0 and result["result"] == [41001, 42002, 0o640],
                    "atomic create owner/group/setgid/umask")
        finally:
            os.close(sg)
        # Record actual numeric ownership and permissions through the second domain.
        seen = os.stat(leaf + "/setgid/created", dir_fd=p.roots[1], follow_symlinks=False)
        checked((seen.st_uid, seen.st_gid, stat.S_IMODE(seen.st_mode)) ==
                (41001, 42002, 0o640), "cross-domain credential metadata freshness")
        os.mkdir("sticky", 0o1777, dir_fd=directory)
        sticky = os.open("sticky", os.O_RDONLY | os.O_DIRECTORY, dir_fd=directory)
        try:
            os.fchmod(sticky, 0o1777)
            owned = os.open("owned", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o666, dir_fd=sticky)
            try:
                os.fchown(owned, 41002, 42002)
            finally:
                os.close(owned)
            denied, status = p.child(lambda: os.unlink("owned", dir_fd=sticky),
                                     41001, 42001, [42001, 42002])
            checked(status != 0 and denied.get("errno") in {errno.EACCES, errno.EPERM},
                    "sticky directory owner denial")
        finally:
            os.close(sticky)
        owner = os.open("group", os.O_RDWR, dir_fd=directory)
        try:
            p.call("chmod-special-bits", lambda: os.fchmod(owner, 0o6750))
            checked(stat.S_IMODE(os.fstat(owner).st_mode) == 0o6750, "special bits round trip")
            p.call("chown-clears-special-bits", lambda: os.fchown(owner, 41001, 42001))
            checked(stat.S_IMODE(os.fstat(owner).st_mode) & 0o6000 == 0,
                    "chown must clear setuid/setgid")
        finally:
            os.close(owner)
    finally:
        os.close(directory)


def sync(p):
    directory, _ = p.directory()
    try:
        for name, flag in (("ordinary", 0), ("sync", os.O_SYNC), ("dsync", os.O_DSYNC)):
            fd = os.open(name, os.O_CREAT | os.O_EXCL | os.O_RDWR | flag, 0o600,
                         dir_fd=directory)
            try:
                checked(p.write(fd, (name + "-receipt").encode()) == len(name) + 8,
                        "short regular write")
                p.call("fdatasync", lambda: os.fdatasync(fd), file=name, open_flags=flag)
                p.call("fsync", lambda: os.fsync(fd), file=name, open_flags=flag)
                checked(os.pread(fd, 64, 0) == (name + "-receipt").encode(), "sync readback")
            finally:
                os.close(fd)
        p.call("directory-fsync", lambda: os.fsync(directory))
        fd = os.open("mapped", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600, dir_fd=directory)
        try:
            os.ftruncate(fd, 4096)
            # Slice and readback lengths derive from the receipt itself: a fixed
            # 16-byte slice of this 17-byte receipt raised IndexError locally.
            receipt = b"mmap-receipt-0001"
            with mmap.mmap(fd, 4096, flags=mmap.MAP_SHARED,
                           prot=mmap.PROT_READ | mmap.PROT_WRITE) as view:
                view[:len(receipt)] = receipt
                p.call("MS_SYNC", view.flush, bytes=4096)
                p.call("mapped-fsync", lambda: os.fsync(fd))
                checked(os.pread(fd, len(receipt), 0) == receipt, "mapped readback")
        finally:
            os.close(fd)
    finally:
        os.close(directory)


def namespace(p):
    directory, _ = p.directory()
    try:
        fd = os.open("namespace", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o666, dir_fd=directory)
        os.close(fd)

        def isolated():
            libc = ctypes.CDLL(None, use_errno=True)
            before = os.readlink("/proc/self/ns/user")
            if libc.unshare(0x10000000) != 0:  # CLONE_NEWUSER, Linux ABI
                raise OSError(ctypes.get_errno(), "unshare(CLONE_NEWUSER)")
            after = os.readlink("/proc/self/ns/user")
            checked(before != after, "user namespace did not change")
            try:
                fd = os.open("namespace", os.O_RDWR, dir_fd=directory)
            except OSError as failure:
                return {"before": before, "after": after, "errno": failure.errno}
            else:
                os.close(fd)
                raise Refused("foreign user namespace unexpectedly authorized")

        result, status = p.child(isolated)
        checked(status == 0 and result["result"]["errno"] in DENIED,
                "actual foreign namespace denial required; unshare failure is not a pass")
    finally:
        os.close(directory)


def metadata(p):
    directory, leaf = p.directory()
    try:
        fd = os.open("original", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600,
                     dir_fd=directory)
        try:
            checked(p.write(fd, b"metadata-receipt") == 16, "metadata initial write")
            p.call("rename", lambda: os.rename("original", "renamed",
                                              src_dir_fd=directory, dst_dir_fd=directory))
            p.call("hardlink", lambda: os.link("renamed", "linked",
                                              src_dir_fd=directory, dst_dir_fd=directory))
            p.call("truncate", lambda: os.ftruncate(fd, 8), bytes=8)
            seen = os.stat(leaf + "/linked", dir_fd=p.roots[1], follow_symlinks=False)
            checked(seen.st_size == 8 and seen.st_nlink == 2, "hardlink/size fresh through other domain")
            p.call("directory-fsync", lambda: os.fsync(directory))
        finally:
            os.close(fd)
        unlinked = os.open("unlinked", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600,
                           dir_fd=directory)
        try:
            p.call("unlink-open", lambda: os.unlink("unlinked", dir_fd=directory))
            checked(p.write(unlinked, b"retained") == 8, "open-unlinked write")
            p.call("fsync-open-unlinked", lambda: os.fsync(unlinked))
            checked(os.pread(unlinked, 8, 0) == b"retained", "open-unlinked read")
        finally:
            os.close(unlinked)
    finally:
        os.close(directory)


def mapped_pressure(p):
    directory, leaf = p.directory()
    try:
        fd = os.open("full", os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600, dir_fd=directory)
        try:
            p.call("ftruncate", lambda: os.ftruncate(fd, PAYLOAD), bytes=PAYLOAD)
            with mmap.mmap(fd, PAYLOAD, flags=mmap.MAP_SHARED,
                           prot=mmap.PROT_READ | mmap.PROT_WRITE) as view:
                # Deterministic full 4MiB payload; no reduced canary or SIGBUS pass.
                for offset in range(0, PAYLOAD, 4096):
                    view[offset:offset + 4096] = bytes([offset // 4096 % 251]) * 4096
                p.call("MS_SYNC-full-canary", view.flush, bytes=PAYLOAD)
                p.call("fsync-full-canary", lambda: os.fsync(fd))
                expected = hashlib.sha256(view).hexdigest()
            other = os.open(leaf + "/full", os.O_RDONLY | os.O_NOFOLLOW, dir_fd=p.roots[1])
            try:
                digest = hashlib.sha256()
                for offset in range(0, PAYLOAD, 65536):
                    data = os.pread(other, min(65536, PAYLOAD - offset), offset)
                    checked(len(data) == min(65536, PAYLOAD - offset), "short cross-domain canary read")
                    digest.update(data)
                checked(digest.hexdigest() == expected, "full canary cross-domain hash")
                emit("canary", bytes=PAYLOAD, sha256=expected)
            finally:
                os.close(other)
        finally:
            os.close(fd)
    finally:
        os.close(directory)


CASES = {"credentials": credentials, "sync": sync, "metadata": metadata,
         "namespace": namespace, "mapped-pressure": mapped_pressure}


def main():
    roots, p, success, release = [], None, False, None
    try:
        checked(len(sys.argv) == 1, "one stdin request; no arguments")
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("case deadline")))
        signal.alarm(10)
        raw = sys.stdin.buffer.read(REQUEST_LIMIT + 1)
        checked(len(raw) <= REQUEST_LIMIT, "request limit")
        request = json.loads(raw, object_pairs_hook=unique)
        release = is_release(request)
        roots = admit(request)
        signal.alarm(request["seconds"])
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
        resource.setrlimit(resource.RLIMIT_NOFILE, (128, 128))
        p = Probe(request, roots)
        if release:
            Retention(p).release()
        else:
            CASES[request["case"]](p)
            # Never delete here: host correlation needs the payload present.
            Retention(p).retain()
        success = True
    except BaseException as failure:
        emit("failure", error=type(failure).__name__, detail=str(failure)[:1024],
             errno=getattr(failure, "errno", None))
    finally:
        signal.alarm(0)
        for fd in roots:
            os.close(fd)
        # A case success retains its payload; only a release success removes it.
        emit("terminal", action="release" if release else "case", syscall_success=success,
             payload_retained=success and release is False,
             payload_released=success and release is True,
             uncertain_children=sorted(p.children) if p else [],
             required_host_correlation=True, acceptance=False, publication=None)
    return 0 if success and not p.children else 1


if __name__ == "__main__":
    sys.exit(main())
