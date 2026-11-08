#!/usr/bin/env python3
"""Forced-command target for a new run-only, read-only guest observer SSH key.

Does not provision keys or change authorized_keys. Root must separately approve
installation of these exact bytes and a restrictive run-owned public-key entry;
observer_key.py stages these bytes and enrols/removes that one entry.
Accepts only the actual controlled-runtime fixed probe command, with optional
literal `phase`; arbitrary SSH commands and other arguments refuse.
"""
import argparse
import hashlib
import os
from pathlib import Path
import stat
import sys


def trusted_file(path, expected, bound):
    if not path.startswith("/") or str(Path(path)) != os.path.normpath(path):
        raise ValueError("canonical absolute path required")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        value = os.fstat(fd)
        if (not stat.S_ISREG(value.st_mode) or value.st_uid != 0 or
                value.st_mode & 0o022 or value.st_size > bound):
            raise ValueError("root-owned immutable bounded regular input required")
        h, data, total = hashlib.sha256(), [], 0
        while True:
            chunk = os.read(fd, 65536)
            if not chunk:
                break
            total += len(chunk)
            if total > bound:
                raise ValueError("observer input grew beyond bound")
            h.update(chunk)
            data.append(chunk)
        if h.hexdigest() != expected:
            raise ValueError("pinned observer input changed")
        os.lseek(fd, 0, os.SEEK_SET)
        return fd, b"".join(data)
    except BaseException:
        os.close(fd)
        raise


def permitted_phase(source, original):
    exact = "python3 -I -c '" + source.replace("'", "'\\''") + "'"
    if original == exact:
        return []
    if original == exact + " phase":
        return ["phase"]
    raise ValueError("only exact runtime installed/phase observations allowed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--probe", required=True)
    parser.add_argument("--probe-sha256", required=True)
    parser.add_argument("--python", required=True)
    parser.add_argument("--python-sha256", required=True)
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise ValueError("trusted guest-root observer required")
    fd, raw = trusted_file(args.probe, args.probe_sha256, 65536)
    os.close(fd)
    source = raw.decode("utf-8")
    original = os.environ.get("SSH_ORIGINAL_COMMAND", "")
    phase = permitted_phase(source, original)
    fd, _ = trusted_file(args.python, args.python_sha256, 32 << 20)
    try:
        # Python's fd exec uses fexecve; retain the actually hashed executable.
        os.execve(fd, [args.python, "-I", "-c", source, *phase],
                  {"PATH": "/usr/bin:/bin", "LC_ALL": "C"})
    finally:
        os.close(fd)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError) as failure:
        print("observer refused: " + str(failure), file=sys.stderr)
        sys.exit(1)
