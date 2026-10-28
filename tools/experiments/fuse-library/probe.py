#!/usr/bin/env python3
"""Bounded Linux binding experiment. Default is a read-only printed plan.

Execution needs authorization under the current session's user instructions;
--run selects execution and is never evidence of approval by itself.
"""
import argparse
import ctypes
import errno
import fcntl
import json
import mmap
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


def emit(value):
    print(json.dumps(value), flush=True)


def client(directory, operation):
    mount = Path(directory) / "mnt"
    if operation == "cancel":
        received = []
        signal.signal(signal.SIGUSR1, lambda *_: received.append("SIGUSR1"))
        signal.siginterrupt(signal.SIGUSR1, True)
        libc = ctypes.CDLL(None, use_errno=True)
        with (mount / "cancel").open("rb", buffering=0) as source:
            buf = ctypes.create_string_buffer(1)
            started = time.monotonic()
            ctypes.set_errno(0)
            count = libc.read(source.fileno(), buf, 1)
            emit({"operation": operation, "count": count, "errno": ctypes.get_errno(),
                  "seconds": time.monotonic() - started, "pid": os.getpid(), "signals": received})
        return
    if operation == "after-invalidation":
        data = (mount / "data").read_bytes()
        emit({"operation": operation, "first_byte": data[:1].decode(), "length": len(data),
              "stat_size": (mount / "data").stat().st_size})
        return
    identity = {"pid": os.getpid(), "uid": os.getuid(), "gid": os.getgid(), "groups": os.getgroups()}
    assert (mount / "credentials").read_bytes() == b"A" * 4096
    assert (mount / "data").read_bytes() == b"A" * 4096
    started = time.monotonic()
    assert (mount / "delayed").read_bytes() == b"A" * 4096
    delayed = time.monotonic() - started
    mm_result = {}
    with (mount / "credentials").open("r+b", buffering=0) as source:
        try:
            with mmap.mmap(source.fileno(), 4096) as memory:
                memory[0:1] = b"M"
                memory.flush()
            os.fsync(source.fileno())
            source.seek(0)
            assert source.read(1) == b"M"
            mm_result = {"mmap": "passed"}
        except OSError as error:
            mm_result = {"mmap": "rejected", "errno": error.errno}
    locks = []
    # Two independently opened descriptions establish same-mount contention.
    # Go daemon logs additionally establish actual backend forwarding.
    for method in (fcntl.flock, fcntl.lockf):
        with (mount / "lock").open("r+b", buffering=0) as first, (mount / "lock").open("r+b", buffering=0) as second:
            method(first, fcntl.LOCK_EX | fcntl.LOCK_NB)
            contender = subprocess.run(
                [sys.executable, __file__, "--lock-contender", str(mount / "lock"), method.__name__],
                capture_output=True, text=True, timeout=3)
            locks.append({"method": method.__name__, "returncode": contender.returncode,
                          "stdout": contender.stdout, "stderr": contender.stderr})
            assert contender.returncode == 0
            method(first, fcntl.LOCK_UN)
    emit({"operation": operation, "identity": identity, "delayed_seconds": delayed, **mm_result, "locks": locks})


def wait_for(predicate, seconds, description):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.05)
    raise RuntimeError("timed out: " + description)


def invoke_client(directory, operation):
    result = subprocess.run([sys.executable, __file__, "--client", str(directory), operation],
                            capture_output=True, text=True, timeout=12)
    (directory / (operation + ".json")).write_text(result.stdout)
    (directory / (operation + ".stderr")).write_text(result.stderr)
    if result.returncode:
        raise RuntimeError(f"{operation} client failed: {result.stderr}")
    return json.loads(result.stdout)


def one_mount(root, binary, library, direct):
    directory = root / (library + ("-direct" if direct else "-buffered"))
    directory.mkdir(mode=0o700)
    (directory / "mnt").mkdir(mode=0o700)
    (directory / "lock-backing").write_bytes(b"A" * 4096)
    command = [str(binary), "-run-dir", str(directory)] + (["-direct"] if direct else [])
    log_path = directory / "daemon.log"
    with log_path.open("w") as log:
        daemon = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
        try:
            wait_for(lambda: os.path.ismount(directory / "mnt"), 5, "mount readiness")
            initial = invoke_client(directory, "initial")
            if not direct:
                assert initial["mmap"] == "passed"
            reader = subprocess.Popen([sys.executable, __file__, "--client", str(directory), "cancel"],
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                wait_for(lambda: "event=read_wait name=cancel" in log_path.read_text(), 3, "blocked read callback")
                reader.send_signal(signal.SIGUSR1)
                stdout, stderr = reader.communicate(timeout=5)
                (directory / "cancel.json").write_text(stdout)
                (directory / "cancel.stderr").write_text(stderr)
                if reader.returncode:
                    raise RuntimeError("cancel client failed")
                interrupted = json.loads(stdout)
                assert interrupted["signals"] == ["SIGUSR1"]
                if library == "gofuse":
                    assert interrupted["count"] == -1 and interrupted["errno"] == errno.EINTR
            finally:
                if reader.poll() is None:
                    reader.terminate()
                    reader.communicate(timeout=5)
            os.kill(daemon.pid, signal.SIGUSR1)
            wait_for(lambda: "event=invalidate" in log_path.read_text(), 3, "invalidation response")
            after = invoke_client(directory, "after-invalidation")
            if library == "gofuse":
                assert after["first_byte"] == "B" and after["length"] == 2048 and after["stat_size"] == 2048
                assert "event=cancelled name=cancel" in log_path.read_text()
                assert "event=setlk" in log_path.read_text()
            emit({"mount": directory.name, "initial": initial, "after": after, "status": "observed"})
        finally:
            # No lazy/forced unmount, no deleting fixtures, no killing daemons
            # while mounted. A failed ordinary unmount preserves everything.
            if os.path.ismount(directory / "mnt"):
                cleanup = subprocess.run(["fusermount3", "-u", str(directory / "mnt")],
                                         capture_output=True, text=True, timeout=5)
                (directory / "unmount.json").write_text(json.dumps({"returncode": cleanup.returncode,
                    "stdout": cleanup.stdout, "stderr": cleanup.stderr}))
                if cleanup.returncode:
                    raise RuntimeError(f"normal unmount failed; preserve daemon PID {daemon.pid} and {directory}")
            try:
                daemon.wait(timeout=5)
            except subprocess.TimeoutExpired:
                # Mount is gone; terminate only this disposable userspace daemon.
                daemon.terminate()
                daemon.wait(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true")
    parser.add_argument("--root", default="/tmp/hfs-fuse-library-20261028-hq3-linux-r6")
    parser.add_argument("--bin-dir", default="/tmp/hfs-fuse-builddeps-20261028-Hl5Smo")
    parser.add_argument("--client", nargs=2, metavar=("DIRECTORY", "OPERATION"))
    parser.add_argument("--lock-contender", nargs=2)
    args = parser.parse_args()
    if args.client:
        client(*args.client)
        return
    if args.lock_contender:
        path, method = args.lock_contender
        with open(path, "r+b", buffering=0) as source:
            try:
                getattr(fcntl, method)(source, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError as error:
                if error.errno in (errno.EAGAIN, errno.EACCES):
                    emit({"contention": "denied", "errno": error.errno})
                    return
                raise
        raise RuntimeError("contending lock unexpectedly succeeded")
    root = Path(args.root)
    if root.parent != Path("/tmp") or not root.name.startswith("hfs-fuse-library-"):
        parser.error("root must be a new /tmp/hfs-fuse-library-* directory")
    emit({"root": str(root), "mounts": [f"{lib}-{mode}" for lib in ("gofuse", "cgofuse")
                                        for mode in ("buffered", "direct")],
          "max_file_bytes": 4096, "max_files_per_mount": 7,
          "client_timeout_seconds": 12, "cleanup": "ordinary fusermount3 -u; preserve all artifacts"})
    if not args.run:
        return
    root.mkdir(mode=0o700)  # Refuse an existing run and never delete its data.
    (root / "environment.json").write_text(json.dumps({"uname": list(os.uname()),
        "uid": os.getuid(), "gid": os.getgid(), "groups": os.getgroups()}))
    for library in ("gofuse", "cgofuse"):
        for direct in (False, True):
            one_mount(root, Path(args.bin_dir) / library, library, direct)


if __name__ == "__main__":
    main()
