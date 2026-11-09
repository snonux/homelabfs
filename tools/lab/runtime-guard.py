#!/usr/local/bin/python3.12 -I
# SPDX-License-Identifier: GPL-2.0-only
"""Outer resource guard for the long-running zq3 controlled runtime (gap G13).

Runs on the f3 host as root, in the foreground, around exactly one
`hfs-controlled-runtime --config ABSOLUTE` invocation (zq3 run plan, Phase 8).
It grants no authority: the runtime launch itself is approval class A4.

Concepts reused from native-test-guard.py: a fresh private run directory,
fixed child environment, durable write-once records, incarnation-checked
process accounting over the child's descendants, a disk walk of run-owned
roots plus a filesystem free-space floor, and a time bound. The difference is
what happens on a breach. native-test-guard ends its short test job with a
process-group TERM/KILL; here the runtime's descendants include the candidate
bhyve child that runs Rocky's real disk, so this guard NEVER signals a process
group, NEVER sends SIGKILL and NEVER signals bhyve, Rocky or any process other
than its own direct, unreaped runtime child.

Breach policy. With the default `--on-breach stop-runtime` the first breach
is recorded in `breach.json` and exactly one SIGTERM goes to that runtime. The
runtime handles SIGTERM as its documented retained stop
(`controlled-runtime-stopping-retained`): it closes the lifecycle endpoint,
blocks admission and exits WITHOUT killing bhyve, unmounting or deleting
anything (internal/controlledruntime/runtime.md). The SIGTERM is sent even if
writing a record fails, and an unexpected guard failure is itself a breach, so
a broken guard never leaves the runtime running unguarded. The runtime runs in
its own session, so a terminal Ctrl-C or SSH hangup reaches only the guard;
SIGHUP/SIGINT/SIGTERM to the guard is a `guard-interrupted` breach, which in
this mode becomes the same orderly retained stop. If the runtime does not exit
within the grace period the guard records `stop-uncertain.json`, keeps
observing for a bounded time and then exits with status 3, leaving the runtime
running for root inspection. `--on-breach report` never signals anything, not
even on Ctrl-C, a guard failure or the time limit: it only records, and
stopping the runtime is then the operator's job. Guest shutdown and vmm/tap
cleanup stay Phase 11 steps.

Plan limits (defaults; options may only tighten them, never loosen):
runtime window 3600 s from launch to runtime exit; runtime and its non-bhyve
descendants 4 GiB + 256 MiB RSS (the verified 4 GiB `Budget.HostBytes` cap of
the run config plus the SSH observer children and Go runtime overhead that the
budget does not account); bhyve 6 GiB guest memory plus 512 MiB process
overhead; run-owned disk 4 GiB over every --disk-root; 8 GiB free on each
disk-root filesystem; no swap use; at least 512 MiB available memory (free +
inactive pages + ARC above its minimum). Only VM context `--vm-name` may exist
under /dev/vmm while the runtime runs; a `rocky` context there means vm-bhyve
started Rocky on the same disk (run plan R3) and is reported as a critical
breach, never acted upon. Failure to observe is itself a breach: what cannot
be measured cannot be bounded.

The disk walk never touches a mounted filesystem below a disk root, above
all the runtime's own native FUSE mount: an lstat of that mountpoint would be
served by the runtime's FUSE server (perturbing evidence, or blocking the
guard and with it every bound). Each sample reads the cached mount table
(`mount -p` uses getmntinfo(MNT_NOWAIT), no filesystem contact) and skips
those mountpoints before any lstat; `--skip-path` additionally pre-declares
paths (the native mountpoint from the run config) that are never touched,
closing the race with a mount that appears between table read and walk.

Exit status: 0 runtime exited 0 without breach; 1 breach, runtime failure or
refusal before launch; 3 runtime still running after a stop request.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time

VERSION = "homelabfs-runtime-guard/1"
GIB = 1 << 30
MIB = 1 << 20
# Plan ceilings; argparse values above them refuse (tighten-only options).
CEILINGS = {"max_seconds": 3600, "runtime_rss_bytes": 4 * GIB + 256 * MIB,
            "exporter_rss_bytes": 6 * GIB + 512 * MIB, "disk_bytes": 4 * GIB}
FLOORS = {"free_disk_bytes": 8 * GIB, "memory_floor_bytes": 512 * MIB}
VM_NAME = re.compile(r"hfs-[a-z0-9][a-z0-9-]{0,22}")
RUNTIME_ENVIRONMENT = {"PATH": "/sbin:/bin:/usr/sbin:/usr/bin", "LC_ALL": "C",
                       "TZ": "UTC", "HOME": "/root"}
OBSERVATION_ERRORS = (OSError, ValueError, subprocess.SubprocessError)


class Refused(RuntimeError):
    """A precondition failed before the runtime was launched."""


def require(ok, message):
    if not ok:
        raise Refused(message)


def title_tokens(args_text):
    """Process title split on blanks and colons, like vms.sh refuse_zq3_launch.

    The runtime execs bhyve as /dev/fd/4, so FreeBSD's setproctitle prefix is
    the image name "4", e.g. "4: hfs-zq3-r1 (4)", not "bhyve: ...". Before
    retitling, the argv still ends with the VM name. Matching the VM-name token
    covers both forms without depending on the image name.
    """
    return {token for token in re.split(r"[\s:]+", args_text) if token}


def own_lineage(rows, pid):
    """The guard's pid and its ancestors present in one process sample.

    Follows ppid links from `pid` up to init; stops at a pid missing from the
    sample or at a cycle, so a malformed sample cannot loop.
    """
    lineage = set()
    while pid in rows and pid not in lineage and pid > 1:
        lineage.add(pid)
        pid = rows[pid][0]
    return lineage


def parse_mount_table(text, roots):
    """Mountpoints strictly below any disk root, from `mount -p` output.

    Lines are "FROM ON TYPE OPTS DUMP PASS"; spaces in names are octal-escaped
    (\\040). A line that does not split into six fields cannot be located
    reliably: it is ignored unless it mentions a disk root, which refuses.
    """
    found = set()
    for line in text.splitlines():
        fields = line.split()
        if not fields:
            continue
        if len(fields) != 6:
            if any(str(root) in line for root in roots):
                raise ValueError("ambiguous mount table line below a disk root")
            continue
        point = Path(re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), fields[1]))
        if any(root in point.parents for root in roots):
            found.add(str(point))
        elif point in roots:
            raise ValueError("a disk root is itself a mountpoint: " + str(point))
    return found


class HostOS:
    """Every FreeBSD host boundary the guard touches; tests substitute it."""

    def admitted(self):
        return sys.platform.startswith("freebsd") and os.geteuid() == 0

    def lstat(self, path):
        return os.lstat(path)

    def digest(self, path):
        # Same admission as fdescfs-window.py: root-controlled ancestors and a
        # root-owned, non-writable regular file, read without following links.
        for parent in Path(path).parents:
            info = os.lstat(parent)
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or (
                    info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX):
                raise Refused("runtime ancestors must be root-controlled directories")
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
                raise Refused("root-owned non-writable regular runtime required")
            result = hashlib.sha256()
            for chunk in iter(lambda: os.read(fd, 1 << 16), b""):
                result.update(chunk)
            return result.hexdigest()
        finally:
            os.close(fd)

    def processes(self):
        """pid -> (ppid, rss_bytes, lstart, args) from one bounded ps call.

        -ww keeps full argv/titles, so the VM-name token is never truncated.
        """
        raw = subprocess.check_output(["/bin/ps", "-ax", "-ww", "-o", "pid=", "-o", "ppid=",
                                       "-o", "rss=", "-o", "lstart=", "-o", "args="],
                                      text=True, timeout=5)
        rows = {}
        for line in raw.splitlines():
            words = line.split(None, 8)
            if len(words) < 8:
                raise ValueError("process observation shape")
            pid, parent, rss = map(int, words[:3])
            rows[pid] = (parent, rss * 1024, " ".join(words[3:8]),
                         words[8] if len(words) == 9 else "")
        return rows

    def mountpoints(self, roots):
        raw = subprocess.check_output(["/sbin/mount", "-p"], text=True, timeout=5)
        return parse_mount_table(raw, roots)

    def vm_contexts(self):
        root = Path("/dev/vmm")
        return sorted(p.name for p in root.iterdir()) if root.exists() else []

    def memory(self):
        names = ["hw.pagesize", "vm.stats.vm.v_free_count", "vm.stats.vm.v_inactive_count",
                 "kstat.zfs.misc.arcstats.size", "kstat.zfs.misc.arcstats.c_min"]
        values = subprocess.check_output(["/sbin/sysctl", "-n", *names], text=True, timeout=5).split()
        page, free, inactive, arc, arc_min = map(int, values)
        return {"free_bytes": page * free, "inactive_bytes": page * inactive,
                "arc_reclaimable_bytes": max(0, arc - arc_min),
                "available_bytes": page * (free + inactive) + max(0, arc - arc_min)}

    def swap_used(self):
        raw = subprocess.check_output(["/usr/sbin/swapinfo", "-k"], text=True, timeout=5)
        lines = raw.splitlines()
        if not lines or lines[0].split()[:4] != ["Device", "1K-blocks", "Used", "Avail"]:
            raise ValueError("swapinfo shape")
        devices = [line.split() for line in lines[1:] if line and not line.startswith("Total")]
        return sum(int(words[2]) * 1024 for words in devices)

    def allocated(self, root, skip):
        return allocated_bytes(Path(root), frozenset(skip))

    def free_bytes(self, root):
        space = os.statvfs(root)
        return space.f_bavail * space.f_frsize

    def spawn(self, argv, cwd, stdout, stderr):
        # Own session so terminal/SSH hangups reach only this guard; the guard
        # still never signals that session or group, only the child PID.
        return subprocess.Popen(argv, cwd=cwd, env=dict(RUNTIME_ENVIRONMENT),
                                stdin=subprocess.DEVNULL, stdout=stdout,
                                stderr=stderr, start_new_session=True)

    def running(self, child):
        # WNOWAIT observes exit without reaping, so the PID stays reserved for
        # this unreaped child and a later SIGTERM cannot reach a reused PID.
        return os.waitid(os.P_PID, child.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is None

    def terminate(self, child):
        os.kill(child.pid, signal.SIGTERM)

    def reap(self, child):
        return child.wait(timeout=5)

    def now(self):
        return time.monotonic()

    def sleep(self, seconds):
        time.sleep(seconds)


def allocated_bytes(root, skip=frozenset(), device=None):
    """Allocated bytes below root, never touching skipped or foreign paths.

    `skip` holds mountpoints and pre-declared paths; they are dropped BEFORE
    any lstat, because the lstat of a FUSE mountpoint is itself a request to
    that filesystem. The st_dev comparison remains a second line of defence for
    a mount that appeared after the table was read.
    """
    if str(root) in skip:
        return 0
    try:
        info = os.lstat(root)
    except FileNotFoundError:
        if device is None:
            raise
        return 0  # The runtime removed a temporary entry during the walk.
    device = info.st_dev if device is None else device
    if info.st_dev != device:
        return 0
    total = info.st_blocks * 512
    if stat.S_ISDIR(info.st_mode):
        try:
            # scandir only reads the directory itself; entries are not stat'ed.
            with os.scandir(root) as entries:
                names = [entry.path for entry in entries]
        except FileNotFoundError:
            return total
        for name in names:
            total += allocated_bytes(Path(name), skip, device)
    return total


def persist(path, value):
    """Write-once durable JSON record (O_EXCL, fsync file and directory)."""
    raw = (json.dumps(value, sort_keys=True) + "\n").encode()
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--run-dir", type=Path, required=True)
    parser.add_argument("--runtime-sha256", required=True)
    parser.add_argument("--vm-name", required=True)
    parser.add_argument("--disk-root", type=Path, action="append", required=True)
    parser.add_argument("--skip-path", type=Path, action="append", default=[])
    parser.add_argument("--on-breach", choices=("stop-runtime", "report"), default="stop-runtime")
    for name, value in CEILINGS.items():
        parser.add_argument("--" + name.replace("_", "-"), type=int, default=value)
    for name, value in FLOORS.items():
        parser.add_argument("--" + name.replace("_", "-"), type=int, default=value)
    parser.add_argument("--swap-limit-bytes", type=int, default=0)
    parser.add_argument("--interval", type=float, default=1.0)
    parser.add_argument("--stop-grace", type=int, default=30)
    parser.add_argument("--observe-after-stop", type=int, default=300)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args(argv)
    if args.command[:1] == ["--"]:
        args.command = args.command[1:]
    return args


def check_limits(args):
    """Options may tighten the plan limits but never loosen them."""
    for name, ceiling in CEILINGS.items():
        require(0 < getattr(args, name) <= ceiling, name + " exceeds the plan limit")
    for name, floor in FLOORS.items():
        require(getattr(args, name) >= floor, name + " is below the plan floor")
    require(args.swap_limit_bytes == 0, "the plan admits no swap use")
    require(0.2 <= args.interval <= 5, "sampling interval 0.2-5 s")
    require(1 <= args.stop_grace <= 120 and 0 <= args.observe_after_stop <= 900,
            "bounded stop grace and post-stop observation")
    require(args.on_breach in ("stop-runtime", "report"), "breach policy")


def check_command(args, host):
    """Exactly the pinned runtime with one absolute --config argument."""
    argv = args.command
    require(len(argv) == 3 and argv[1] == "--config", "argv must be RUNTIME --config CONFIG")
    for path in (argv[0], argv[2]):
        require(os.path.isabs(path) and os.path.normpath(path) == path, "clean absolute paths")
    require(re.fullmatch(r"[0-9a-f]{64}", args.runtime_sha256) is not None, "runtime pin")
    try:
        actual = host.digest(argv[0])
    except OSError as failure:
        raise Refused("runtime unreadable: " + str(failure)) from failure
    require(actual == args.runtime_sha256, "runtime bytes differ from the pin")


def check_disk_roots(args, host):
    """Existing, distinct, non-nested root directories that are not mounts."""
    roots = [Path(os.path.normpath(root)) for root in args.disk_root]
    require(all(root.is_absolute() for root in roots), "absolute disk roots")
    # Nested roots would count the same bytes twice against the 4 GiB bound.
    require(not any(a != b and a in b.parents for a in roots for b in roots) and
            len(set(roots)) == len(roots), "distinct, non-nested disk roots")
    skips = [Path(os.path.normpath(path)) for path in args.skip_path]
    require(all(path.is_absolute() and any(root in path.parents for root in roots) for path in skips),
            "skip paths must lie strictly below a disk root")
    for root in roots:
        try:
            info = host.lstat(root)
        except OSError as failure:
            raise Refused("disk root unavailable: " + str(failure)) from failure
        require(stat.S_ISDIR(info.st_mode), "disk root must be a directory: " + str(root))
    try:
        host.mountpoints(roots)
    except OBSERVATION_ERRORS as failure:
        raise Refused("mount table refused: " + str(failure)) from failure
    args.disk_root, args.skip_path = roots, skips


def check_run_dir(args, host):
    """Fresh run directory in a root-private parent, inside a disk root."""
    run_dir = args.run_dir
    require(run_dir.is_absolute() and os.path.normpath(run_dir) == str(run_dir), "absolute run dir")
    require(VM_NAME.fullmatch(args.vm_name) is not None, "run-owned hfs-* VM name")
    check_disk_roots(args, host)
    require(any(root == run_dir.parent or root in run_dir.parents for root in args.disk_root),
            "guard records must count against a disk root")
    try:
        host.lstat(run_dir)
    except FileNotFoundError:
        pass
    else:
        raise Refused("run directory already exists; no replay")
    parent = host.lstat(run_dir.parent)
    require(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0 and not parent.st_mode & 0o077,
            "root-private run directory parent")


class Guard:
    """Samples the runtime tree and applies the single breach policy."""

    def __init__(self, args, host):
        self.args, self.host = args, host
        self.run_dir = args.run_dir
        self.known = {}
        self.child = None
        self.started = None
        self.breach = None
        self.stop_sent_at = None
        self.uncertain = False
        self.interrupted = None
        self.record_errors = []
        self.peaks = {"runtime_rss": 0, "exporter_rss": 0, "disk": 0}
        self.exporter = None

    def record(self, name, value):
        """Best-effort durable record: a write failure never blocks a stop."""
        try:
            persist(self.run_dir / name, value)
        except OSError as failure:
            self.record_errors.append(name + ": " + str(failure))

    def descendants(self, rows):
        """Child-rooted tree, following only recognised process incarnations."""
        owned = {self.child.pid} if self.child else set()
        owned |= {pid for pid, old in self.known.items() if pid in rows and old[2] == rows[pid][2]}
        while True:
            grown = owned | {pid for pid, row in rows.items() if row[0] in owned}
            if grown == owned:
                break
            owned = grown
        self.known.update({pid: rows[pid] for pid in owned if pid in rows})
        return {pid: rows[pid] for pid in owned if pid in rows}

    def is_exporter(self, args_text):
        # Anything without the VM-name token counts against the runtime envelope.
        return self.args.vm_name in title_tokens(args_text)

    def processes_sample(self):
        rows = self.host.processes()
        tree = self.descendants(rows)
        exporter = {pid: row for pid, row in tree.items() if self.is_exporter(row[3])}
        if exporter:
            pid = min(exporter)
            self.exporter = {"pid": pid, "lstart": exporter[pid][2]}
        # Rocky's vm-bhyve bhyve titles itself "bhyve: rocky (bhyve)"; a second
        # process carrying our VM name outside our tree is foreign as well.
        # The guard's own process and its ancestors (doas, the remote shell)
        # carry `--vm-name NAME` in their argv; zq3-r2 Phase 8 refused itself
        # for that before launch, so that lineage is never foreign.
        lineage = own_lineage(rows, os.getpid())
        foreign = sorted(pid for pid, row in rows.items()
                         if pid not in tree and pid not in lineage and (
                             row[3].startswith("bhyve: ") or self.is_exporter(row[3])))
        return {"runtime_rss": sum(row[1] for pid, row in tree.items() if pid not in exporter),
                "exporter_rss": sum(row[1] for row in exporter.values()),
                "tree": sorted(tree), "foreign_bhyve": foreign}

    def measure(self):
        sample = self.processes_sample()
        roots = self.args.disk_root
        skip = {str(path) for path in self.args.skip_path} | self.host.mountpoints(roots)
        sample.update({"vm_contexts": self.host.vm_contexts(), "memory": self.host.memory(),
                       "swap_used": self.host.swap_used(), "skipped": sorted(skip),
                       "disk": sum(self.host.allocated(root, skip) for root in roots),
                       "free_disk": min(self.host.free_bytes(root) for root in roots)})
        for key in self.peaks:
            self.peaks[key] = max(self.peaks[key], sample[key])
        return sample

    def violations(self, sample, elapsed):
        """Every limit the sample breaks, as (kind, detail) pairs."""
        a = self.args
        checks = [
            ("time", elapsed > a.max_seconds, elapsed),
            ("runtime-rss", sample["runtime_rss"] > a.runtime_rss_bytes, sample["runtime_rss"]),
            ("exporter-rss", sample["exporter_rss"] > a.exporter_rss_bytes, sample["exporter_rss"]),
            ("disk", sample["disk"] > a.disk_bytes, sample["disk"]),
            ("free-disk", sample["free_disk"] < a.free_disk_bytes, sample["free_disk"]),
            ("swap", sample["swap_used"] > a.swap_limit_bytes, sample["swap_used"]),
            ("memory-floor", sample["memory"]["available_bytes"] < a.memory_floor_bytes,
             sample["memory"]["available_bytes"]),
            ("foreign-vm-context", not set(sample["vm_contexts"]) <= {a.vm_name},
             sample["vm_contexts"]),
            ("foreign-bhyve", bool(sample["foreign_bhyve"]), sample["foreign_bhyve"]),
        ]
        return [(kind, detail) for kind, broken, detail in checks if broken]

    def observe(self):
        """One sample appended to observations.jsonl; returns violations."""
        elapsed = self.host.now() - self.started if self.started is not None else 0
        try:
            sample = self.measure()
            found = self.violations(sample, elapsed)
        except OBSERVATION_ERRORS as failure:
            sample, found = {"error": str(failure)}, [("observation-failed", str(failure))]
        try:
            with open(self.run_dir / "observations.jsonl", "a", encoding="utf-8") as stream:
                stream.write(json.dumps({"elapsed": elapsed, "sample": sample,
                                         "violations": found}, sort_keys=True) + "\n")
        except OSError as failure:
            # Unrecorded evidence is a breach too; the stop must not depend on it.
            found = found + [("record-failed", str(failure))]
        return found

    def on_breach(self, found):
        """Record the first breach; apply the policy exactly once."""
        if self.breach is not None or not found:
            return
        self.breach = {"violations": found, "policy": self.args.on_breach,
                       "elapsed": self.host.now() - self.started if self.started is not None else 0}
        self.record("breach.json", self.breach)
        if self.args.on_breach == "stop-runtime":
            self.request_stop("breach")

    def request_stop(self, reason):
        """One SIGTERM to the direct, unreaped runtime child; nothing else."""
        if self.child is None or self.stop_sent_at is not None or not self.host.running(self.child):
            return
        self.record("stop-intent.json", {"pid": self.child.pid, "signal": "SIGTERM", "reason": reason})
        self.host.terminate(self.child)
        self.stop_sent_at = self.host.now()
        self.record("stop-sent.json", {"pid": self.child.pid})

    def check_stop_progress(self):
        """Record once that the runtime outlived the grace; True when expired."""
        if self.stop_sent_at is None:
            return False
        waited = self.host.now() - self.stop_sent_at
        if waited > self.args.stop_grace and not self.uncertain:
            self.uncertain = True
            self.record("stop-uncertain.json", {"pid": self.child.pid,
                        "note": "runtime alive after SIGTERM grace; no further signal"})
        return waited > self.args.stop_grace + self.args.observe_after_stop


def preflight(guard):
    """Before launch: no VM context at all and every limit already satisfied."""
    try:
        sample = guard.measure()
    except OBSERVATION_ERRORS as failure:
        raise Refused("preflight observation failed: " + str(failure)) from failure
    require(sample["vm_contexts"] == [], "a VM context exists before launch (Rocky must be stopped)")
    found = guard.violations(sample, 0)
    require(not found, "limits already violated before launch: " + json.dumps(found))
    return sample


def launch(guard):
    """Durable intent, then the runtime in its own session with fixed env."""
    args, host = guard.args, guard.host
    persist(guard.run_dir / "intent.json", {
        "version": VERSION, "argv": args.command, "runtime_sha256": args.runtime_sha256,
        "environment": RUNTIME_ENVIRONMENT, "limits": limits(args),
        "on_breach": args.on_breach, "vm_name": args.vm_name,
        "disk_roots": [str(root) for root in args.disk_root],
        "skip_paths": [str(path) for path in args.skip_path]})
    with open(guard.run_dir / "runtime.stdout", "xb") as out, \
            open(guard.run_dir / "runtime.stderr", "xb") as err:
        guard.child = host.spawn(args.command, str(guard.run_dir), out, err)
    guard.started = host.now()
    guard.record("started.json", {"pid": guard.child.pid})


def limits(args):
    names = list(CEILINGS) + list(FLOORS) + ["swap_limit_bytes", "stop_grace", "observe_after_stop"]
    return {name: getattr(args, name) for name in names}


def supervise(guard):
    """Sample until the runtime exits or a stop request has expired."""
    host = guard.host
    while host.running(guard.child):
        # A guard signal (e.g. SSH hangup) is a breach: no runtime may be left
        # running unguarded. on_breach applies the policy once, as for limits.
        if guard.interrupted is not None:
            guard.on_breach([("guard-interrupted", guard.interrupted)])
        guard.on_breach(guard.observe())
        if guard.check_stop_progress():
            return None
        host.sleep(guard.args.interval)
    guard.observe()
    return host.reap(guard.child)


def emergency(guard, failure):
    """A guard bug is a breach: apply the policy, then wait one bounded grace.

    Observation is abandoned (it is what failed); only the exit of the direct
    child is awaited. Returns its exit code, or None if it still runs.
    """
    host = guard.host
    try:
        if guard.breach is None:
            guard.on_breach([("guard-failure", repr(failure))])
        elif guard.args.on_breach == "stop-runtime":
            guard.request_stop("guard-failure")
        deadline = host.now() + guard.args.stop_grace
        while host.running(guard.child):
            if host.now() > deadline:
                return None
            host.sleep(guard.args.interval)
        return host.reap(guard.child)
    except Exception as second:  # noqa: BLE001 - report, never raise past the receipt
        guard.record_errors.append("emergency: " + repr(second))
        return None


def finish(guard, code, failure=None):
    """Write the receipt (best effort) and return the guard exit status."""
    rows = {}
    try:
        rows = guard.host.processes()
    except OBSERVATION_ERRORS:
        pass
    exporter = guard.exporter
    alive = bool(exporter and exporter["pid"] in rows and rows[exporter["pid"]][2] == exporter["lstart"])
    running = guard.child is not None and code is None
    receipt = {"version": VERSION, "runtime_exit": code, "breach": guard.breach,
               "refused": failure, "stop_requested": guard.stop_sent_at is not None,
               "runtime_running_at_guard_exit": running,
               "exporter": exporter, "exporter_alive_at_guard_exit": alive,
               "peaks": guard.peaks, "interrupted": guard.interrupted,
               "record_errors": guard.record_errors,
               "seconds": guard.host.now() - guard.started if guard.started is not None else 0}
    guard.record("receipt.json", receipt)
    try:
        print(json.dumps(receipt, sort_keys=True))
    except OSError:
        pass  # A lost terminal must not hide the exit status.
    if running:
        return 3
    return 0 if code == 0 and guard.breach is None and failure is None else 1


def guarded(guard):
    """Preflight, launch and supervise inside the installed signal handlers."""
    try:
        preflight(guard)
        require(guard.interrupted is None, "guard interrupted before launch")
    except Refused as failure:
        return finish(guard, None, str(failure))
    launch(guard)
    try:
        code = supervise(guard)
    except Exception as failure:  # noqa: BLE001 - any guard bug is a breach
        code = emergency(guard, failure)
    return finish(guard, code)


def run(args, host):
    """Admission, launch and supervision; returns the process exit status."""
    require(host.admitted(), "actual FreeBSD root required")
    check_limits(args)
    check_command(args, host)
    check_run_dir(args, host)
    os.mkdir(args.run_dir, 0o700)
    guard = Guard(args, host)

    def interrupted(signum, frame):
        guard.interrupted = signal.Signals(signum).name

    previous = {number: signal.signal(number, interrupted)
                for number in (signal.SIGHUP, signal.SIGINT, signal.SIGTERM)}
    try:
        return guarded(guard)
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)


def isolated_interpreter():
    return bool(sys.flags.isolated)


def main(argv=None):
    args = parse_args(argv)
    try:
        # Same interpreter discipline as fdescfs-window.py: pinned python3.12 -I.
        require(isolated_interpreter(), "invoke the pinned Python interpreter with -I")
        return run(args, HostOS())
    except Refused as failure:
        print(json.dumps({"version": VERSION, "refused": str(failure)}), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
