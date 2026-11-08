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
than its own direct, unreaped runtime child. On the first breach it records
`breach.json` and, with the default `--on-breach stop-runtime`, sends exactly
one SIGTERM to that runtime. The runtime handles SIGTERM as its documented
retained stop (`controlled-runtime-stopping-retained`): it closes the
lifecycle endpoint, blocks admission and exits WITHOUT killing bhyve,
unmounting or deleting anything (internal/controlledruntime/runtime.md).
`--on-breach report` only records. If the runtime does not exit within the
grace period the guard records `stop-uncertain.json`, keeps observing for a
bounded time and then exits with status 3, leaving the runtime running for
root inspection. Guest shutdown and vmm/tap cleanup stay Phase 11 steps.
The runtime runs in its own session, so a terminal Ctrl-C or SSH hangup
reaches only the guard; the guard treats SIGHUP/SIGINT/SIGTERM to itself as a
`guard-interrupted` breach, so no runtime is left running unguarded and the
operator's Ctrl-C becomes the same orderly retained stop.

Plan limits (defaults; options may only tighten them, never loosen):
runtime window 3600 s from launch to runtime exit; host runtime envelope
6 GiB RSS for the runtime and its non-exporter descendants (e.g. the SSH
observer); exporter (bhyve) 6 GiB guest memory plus 512 MiB process overhead;
run-owned disk 4 GiB over every --disk-root; 8 GiB free on each disk-root
filesystem; no swap use; at least 512 MiB available memory (free + inactive
pages + ARC above its minimum). Only VM context `--vm-name` may exist under
/dev/vmm while the runtime runs; a `rocky` context there means vm-bhyve
started Rocky on the same disk (run plan R3) and is reported as a critical
breach, never acted upon. Failure to observe is itself a breach: what cannot
be measured cannot be bounded.

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
CEILINGS = {"max_seconds": 3600, "runtime_rss_bytes": 6 * GIB,
            "exporter_rss_bytes": 6 * GIB + 512 * MIB, "disk_bytes": 4 * GIB}
FLOORS = {"free_disk_bytes": 8 * GIB, "memory_floor_bytes": 512 * MIB}
VM_NAME = re.compile(r"hfs-[a-z0-9][a-z0-9-]{0,22}")
RUNTIME_ENVIRONMENT = {"PATH": "/sbin:/bin:/usr/sbin:/usr/bin", "LC_ALL": "C",
                       "TZ": "UTC", "HOME": "/root"}


class Refused(RuntimeError):
    """A precondition failed before the runtime was launched."""


def require(ok, message):
    if not ok:
        raise Refused(message)


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
        """pid -> (ppid, rss_bytes, lstart, args) from one bounded ps call."""
        raw = subprocess.check_output(["/bin/ps", "-ax", "-o", "pid=", "-o", "ppid=",
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

    def allocated(self, root):
        return allocated_bytes(Path(root))

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


def allocated_bytes(root, device=None):
    """Allocated bytes below root without crossing into another filesystem.

    The native FUSE mount (and any other mount) below a staging root must not
    be walked: touching it from the guard could block on or perturb the very
    filesystem under test, and its bytes are not run-owned host disk.
    """
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
            with os.scandir(root) as entries:
                names = [entry.path for entry in entries]
        except FileNotFoundError:
            return total
        for name in names:
            total += allocated_bytes(Path(name), device)
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


def check_run_dir(args, host):
    """Fresh run directory in a root-private parent, inside a disk root."""
    run_dir = args.run_dir
    require(run_dir.is_absolute() and os.path.normpath(run_dir) == str(run_dir), "absolute run dir")
    require(VM_NAME.fullmatch(args.vm_name) is not None, "run-owned hfs-* VM name")
    roots = [Path(os.path.normpath(root)) for root in args.disk_root]
    require(all(root.is_absolute() for root in roots), "absolute disk roots")
    # Nested roots would count the same bytes twice against the 4 GiB bound.
    require(not any(a != b and a in b.parents for a in roots for b in roots) and
            len(set(roots)) == len(roots), "distinct, non-nested disk roots")
    require(any(root == run_dir.parent or root in run_dir.parents for root in roots),
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
        self.interrupted = None
        self.peaks = {"runtime_rss": 0, "exporter_rss": 0, "disk": 0}
        self.exporter = None

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
        # bhyve retitles itself "bhyve: VM (bhyve)"; before that its argv ends
        # with the VM name. Anything else counts against the runtime envelope.
        words = args_text.split()
        return args_text.startswith("bhyve: " + self.args.vm_name + " ") or (
            bool(words) and words[-1] == self.args.vm_name)

    def measure(self):
        rows = self.host.processes()
        tree = self.descendants(rows)
        exporter = {pid: row for pid, row in tree.items() if self.is_exporter(row[3])}
        if exporter:
            pid = min(exporter)
            self.exporter = {"pid": pid, "lstart": exporter[pid][2]}
        foreign = sorted(pid for pid, row in rows.items()
                         if row[3].startswith("bhyve: ") and pid not in tree)
        sample = {"runtime_rss": sum(row[1] for pid, row in tree.items() if pid not in exporter),
                  "exporter_rss": sum(row[1] for row in exporter.values()),
                  "tree": sorted(tree), "foreign_bhyve": foreign,
                  "vm_contexts": self.host.vm_contexts(), "memory": self.host.memory(),
                  "swap_used": self.host.swap_used(),
                  "disk": sum(self.host.allocated(root) for root in self.args.disk_root),
                  "free_disk": min(self.host.free_bytes(root) for root in self.args.disk_root)}
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
        except (OSError, ValueError, subprocess.SubprocessError) as failure:
            sample, found = {"error": str(failure)}, [("observation-failed", str(failure))]
        record = {"elapsed": elapsed, "sample": sample, "violations": found}
        with open(self.run_dir / "observations.jsonl", "a", encoding="utf-8") as stream:
            stream.write(json.dumps(record, sort_keys=True) + "\n")
        return found

    def on_breach(self, found):
        """Record the first breach; apply the policy exactly once."""
        if self.breach is not None or not found:
            return
        self.breach = {"violations": found, "policy": self.args.on_breach,
                       "elapsed": self.host.now() - self.started}
        persist(self.run_dir / "breach.json", self.breach)
        if self.args.on_breach == "stop-runtime":
            self.request_stop("breach")

    def request_stop(self, reason):
        """One SIGTERM to the direct, unreaped runtime child; nothing else."""
        if self.stop_sent_at is not None or not self.host.running(self.child):
            return
        persist(self.run_dir / "stop-intent.json", {"pid": self.child.pid, "signal": "SIGTERM",
                                                    "reason": reason})
        self.host.terminate(self.child)
        self.stop_sent_at = self.host.now()
        persist(self.run_dir / "stop-sent.json", {"pid": self.child.pid})

    def stop_expired(self):
        if self.stop_sent_at is None:
            return False
        return self.host.now() - self.stop_sent_at > self.args.stop_grace + self.args.observe_after_stop


def preflight(guard):
    """Before launch: no VM context at all and every limit already satisfied."""
    sample = guard.measure()
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
        "on_breach": args.on_breach, "vm_name": args.vm_name})
    with open(guard.run_dir / "runtime.stdout", "xb") as out, \
            open(guard.run_dir / "runtime.stderr", "xb") as err:
        guard.child = host.spawn(args.command, str(guard.run_dir), out, err)
    guard.started = host.now()
    persist(guard.run_dir / "started.json", {"pid": guard.child.pid})


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
        if guard.stop_sent_at is not None and host.now() - guard.stop_sent_at > guard.args.stop_grace:
            if not (guard.run_dir / "stop-uncertain.json").exists():
                persist(guard.run_dir / "stop-uncertain.json", {"pid": guard.child.pid,
                        "note": "runtime alive after SIGTERM grace; no further signal"})
        if guard.stop_expired():
            return None
        host.sleep(guard.args.interval)
    guard.observe()
    return host.reap(guard.child)


def finish(guard, code, failure=None):
    """Write the receipt and return the guard exit status."""
    rows = {}
    try:
        rows = guard.host.processes()
    except (OSError, ValueError, subprocess.SubprocessError):
        pass
    exporter = guard.exporter
    alive = bool(exporter and exporter["pid"] in rows and rows[exporter["pid"]][2] == exporter["lstart"])
    receipt = {"version": VERSION, "runtime_exit": code, "breach": guard.breach,
               "refused": failure, "stop_requested": guard.stop_sent_at is not None,
               "runtime_running_at_guard_exit": guard.child is not None and code is None,
               "exporter": exporter, "exporter_alive_at_guard_exit": alive,
               "peaks": guard.peaks, "interrupted": guard.interrupted,
               "seconds": guard.host.now() - guard.started if guard.started is not None else 0}
    persist(guard.run_dir / "receipt.json", receipt)
    try:
        print(json.dumps(receipt, sort_keys=True))
    except OSError:
        pass  # A lost terminal must not hide the exit status; receipt.json is durable.
    if guard.child is not None and code is None:
        return 3
    return 0 if code == 0 and guard.breach is None and failure is None else 1


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
        try:
            preflight(guard)
            require(guard.interrupted is None, "guard interrupted before launch")
        except Refused as failure:
            return finish(guard, None, str(failure))
        launch(guard)
        return finish(guard, supervise(guard))
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
