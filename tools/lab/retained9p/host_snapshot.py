#!/usr/bin/env python3
"""Read-only f3 snapshot of one run-owned host path for the K04 check (zq3 G11).

Runs on f3 as root inside the approved window, with the pinned interpreter
(`/usr/local/bin/python3.12 -I -B host_snapshot.py ...`). It changes nothing:
it lstat()s the path and its parent and lists at most 256 entries through an
O_NOFOLLOW directory descriptor, then prints one canonical JSON object. The
operator saves stdout privately, hashes it and hands it to `correlate.py k04`.

Labels: `baseline` (the empty raw mount point before the runtime starts),
`before` (after runtime exit, immediately before the guest after-loss step)
and `after` (immediately after it). A dead FUSE mount still covering the path
reports its errno (typically ENOTCONN) instead of a listing: nothing reaches
the raw directory through it. A path that is no longer a mount point is the
raw directory, and its listing must not change.

Bounds: a 10 s alarm. Its expiry refuses the snapshot (exit 1, nothing
printed): a hang is uncertain, never a dead mount's errno. A call stuck in an
uninterruptible kernel sleep cannot be interrupted at all, so the operator's
outer timeout must treat it as uncertain too.
"""
import argparse
import datetime
import errno
import json
import os
import re
import signal
import stat
import sys

VERSION = "homelabfs-retained9p-host-path-snapshot/1"
# Staging prefix from the zq3 installation proposal; the snapshot path must be
# a run-owned entry below PREFIX/RUN/ (for example the runtime FUSE mount point).
PREFIX = "/var/tmp/hfs-validation-xz3/"
LISTING_LIMIT = 256
SECONDS = 10


class Refused(RuntimeError):
    pass


class Deadline(Exception):
    """The snapshot alarm expired. Not an OSError (unlike TimeoutError), so the
    lstat/listing handlers can never record a hung path as a dead mount's
    errno: a deadline refuses the whole snapshot as uncertain instead."""


def deadline(*_):
    raise Deadline("snapshot deadline; host path state uncertain")


def errno_of(failure):
    """The kernel's errno; an OSError without one is not an answer and propagates."""
    if not failure.errno:
        raise failure
    return failure.errno


def checked_path(run_id, path):
    """Canonical safe ASCII path strictly below the run's staging directory."""
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,23}", run_id):
        raise Refused("run ID bound")
    base = PREFIX + run_id + "/"
    if (not re.fullmatch(r"/[A-Za-z0-9_./-]+", path) or os.path.normpath(path) != path or
            not path.startswith(base) or len(path) <= len(base)):
        raise Refused("path must be a canonical entry below " + base)
    return path


def lstat_row(path):
    """lstat without following a final symlink; errno instead of an exception."""
    try:
        seen = os.lstat(path)
    except OSError as failure:
        return {"errno": errno_of(failure), "dev": None, "ino": None, "type": None}
    kind = ("dir" if stat.S_ISDIR(seen.st_mode) else
            "symlink" if stat.S_ISLNK(seen.st_mode) else "other")
    return {"errno": None, "dev": seen.st_dev, "ino": seen.st_ino, "type": kind}


def listing(path):
    """At most LISTING_LIMIT names through an O_NOFOLLOW directory descriptor."""
    names = []
    try:
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            with os.scandir(fd) as entries:
                for entry in entries:
                    if len(names) == LISTING_LIMIT:
                        return {"errno": None, "names": sorted(names), "truncated": True}
                    names.append(entry.name)
        finally:
            os.close(fd)
    except OSError as failure:
        return {"errno": errno_of(failure), "names": None, "truncated": False}
    return {"errno": None, "names": sorted(names), "truncated": False}


def snapshot(run_id, path, label):
    path = checked_path(run_id, path)
    parent = lstat_row(os.path.dirname(path))
    if parent["errno"] is not None or parent["type"] != "dir":
        raise Refused("parent of the snapshot path must be an existing directory")
    target = lstat_row(path)
    mountpoint = None if target["errno"] is not None else target["dev"] != parent["dev"]
    found = (listing(path) if target["type"] == "dir" else
             {"errno": target["errno"] or errno.ENOTDIR, "names": None, "truncated": False})
    return {"version": VERSION, "run_id": run_id, "label": label, "path": path,
            "taken_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "parent": {"dev": parent["dev"], "ino": parent["ino"]},
            "target": target, "mountpoint": mountpoint, "listing": found}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--path", required=True)
    parser.add_argument("--label", required=True, choices=["baseline", "before", "after"])
    args = parser.parse_args(argv)
    signal.signal(signal.SIGALRM, deadline)
    signal.alarm(SECONDS)
    try:
        value = snapshot(args.run_id, args.path, args.label)
    finally:
        signal.alarm(0)
    print(json.dumps(value, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, Refused, Deadline) as failure:
        print("refuse: " + str(failure), file=sys.stderr)
        sys.exit(1)
