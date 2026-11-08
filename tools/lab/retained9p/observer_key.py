#!/usr/bin/env python3
"""Run-only observer SSH key on Rocky: stage, enrol, status and remove (zq3 G6).

Runs ON ROCKY AS ROOT inside the approved zq3 window (approval A6), started by
the root-orchestrated run over the pinned SSH channel. It generates no key,
opens no network connection and never touches a line it did not write.

- `stage` copies observer.py and the runtime's installed probe into the
  root-private run directory as root-owned 0400 files after checking their
  SHA256 and the pinned interpreter, creating the run directory if absent.
- `enrol` appends exactly ONE line to root's authorized_keys: the f3-generated
  run-only public key, restricted to f3's primary address and forced to run
  observer.py (which itself admits only the runtime's exact read-only probe).
  The file's SHA256 must equal the caller's pre-run value; the new SHA256 must
  equal SHA256(before + line). A durable receipt is written first.
- `remove` deletes exactly that line (found by its exact bytes, once) and
  preserves every other line. With the receipt's post-enrol hash it proves
  restoration to the pre-run bytes; with an explicitly acknowledged different
  current hash (unrelated keys changed meanwhile) it still removes only its own
  line and reports `restored_pre_run: false`. After an interrupted enrol whose
  line never landed (file still at the pre-run hash) it only retires the receipt.
- `status` is read-only; it also reports a stale temporary file left by a kill
  between writing it and the rename (`stale_pending`).

The file is replaced atomically (exclusive temporary file, same owner, mode and
SELinux label, fsync, rename, directory fsync), so a crash leaves either the
old or the new complete file and never locks root out. Python 3.9 compatible.
"""
import argparse
import base64
import binascii
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys

VERSION = "homelabfs-retained9p-observer-key/1"
RUN_PARENT = "/var/tmp/hfs-zq3-"  # Same root-private run directory as guest.py.
AUTHORIZED_KEYS = "/root/.ssh/authorized_keys"
TRUSTED_UID = 0
# f3's IQ-verified primary, identical to controller.TARGETS["host"][0]. The
# runtime's observer connects from f3 to Rocky's primary on the same subnet, so
# this is its source address; the pre-migration alias is deliberately not
# accepted (fail closed rather than one key valid from two routes).
OBSERVER_FROM = "10.20.0.133"
# internal/controlledruntime/probe/installed.py, unchanged since source e0cca0c
# of the pinned xz3 candidate: the runtime embeds and sends exactly these bytes,
# and observer.py admits only that command.
PROBE_SHA256 = "c824b183288fb7b1c9e550aad785c147446748d1a6aa39ded613cc621dfcc321"
STAGED_OBSERVER, STAGED_PROBE = "observer.py", "installed-probe.py"
RECEIPT, REMOVED = "observer-key.json", "observer-key-removed.json"
RUN_ID = re.compile(r"[a-z0-9][a-z0-9-]{0,23}\Z")
HASH = re.compile(r"[0-9a-f]{64}\Z")
SAFE_PATH = re.compile(r"/[A-Za-z0-9_./-]+\Z")  # No shell or sshd option syntax.
KEYS_BOUND = 65536
LABEL = "security.selinux"


class Refused(RuntimeError):
    pass


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def safe_path(text):
    """Canonical absolute path usable verbatim inside the forced command."""
    if not SAFE_PATH.match(text) or os.path.normpath(text) != text:
        raise Refused("canonical absolute safe path required: " + repr(text))
    return text


def require_hash(value, what):
    if not HASH.match(value):
        raise Refused(what + " must be a lowercase SHA256")
    return value


def read_bounded(path, bound, trusted):
    """One no-follow read of a regular file; trusted adds root-owned, not 022."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_size > bound:
            raise Refused("bounded regular file required: " + str(path))
        if trusted and (info.st_uid != TRUSTED_UID or info.st_mode & 0o022):
            raise Refused("root-owned file without group/other write required: " + str(path))
        chunks, total = [], 0
        while total <= bound:
            chunk = os.read(fd, 65536)
            if not chunk:
                break
            chunks.append(chunk)
            total += len(chunk)
        if total > bound or total != info.st_size:
            raise Refused("file changed size while read: " + str(path))
        return b"".join(chunks), info
    finally:
        os.close(fd)


def check_hash(raw, expected, what):
    if digest(raw) != expected:
        raise Refused(what + " SHA256 differs from its pin")


def require_root():
    if os.geteuid() != TRUSTED_UID:
        raise Refused("trusted guest root required")


def fsync_directory(path):
    fd = os.open(str(path), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write_exclusive(path, raw, mode):
    """New file only (an existing one refuses and is preserved), then fsync."""
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        os.fchmod(fd, mode)  # Exact mode regardless of the caller's umask.
        if os.write(fd, raw) != len(raw):
            raise OSError("short write: " + str(path))
        os.fsync(fd)
    finally:
        os.close(fd)
    fsync_directory(path.parent)


def run_directory(run_id, create=False):
    """The root-owned 0700 run directory /var/tmp/hfs-zq3-RUN (m0/m1 inside)."""
    if not RUN_ID.match(run_id):
        raise Refused("run ID must match [a-z0-9][a-z0-9-]{0,23}")
    run = Path(safe_path(RUN_PARENT + run_id))
    if create and not os.path.lexists(str(run)):
        os.mkdir(str(run), 0o700)
        os.chmod(str(run), 0o700)
        for mount in ["m0", "m1"]:
            os.mkdir(str(run / mount), 0o700)
        fsync_directory(run)
        fsync_directory(run.parent)
    info = os.lstat(str(run))
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != TRUSTED_UID or
            stat.S_IMODE(info.st_mode) != 0o700):
        raise Refused("root-owned 0700 run directory required: " + str(run))
    return run


def locked(run):
    """Exclusive flock on the run directory; the fd keeps it until exit."""
    fd = os.open(str(run), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(fd)
        raise Refused("another observer_key.py holds this run") from None
    return fd


def interpreter(path, expected):
    """The pinned interpreter: the exact root-owned bytes observer.py will exec."""
    raw, _ = read_bounded(safe_path(path), 32 << 20, trusted=True)
    check_hash(raw, require_hash(expected, "interpreter"), "interpreter " + path)


def staged_files(run, observer_sha256):
    """Verify the staged observer and probe still hold their pinned bytes."""
    raw, _ = read_bounded(run / STAGED_OBSERVER, 65536, trusted=True)
    check_hash(raw, require_hash(observer_sha256, "observer.py"), "staged observer.py")
    raw, _ = read_bounded(run / STAGED_PROBE, 65536, trusted=True)
    check_hash(raw, PROBE_SHA256, "staged installed probe")


def key_blob(encoded):
    """Validate one base64 ssh-ed25519 public key blob (type string + 32 bytes)."""
    try:
        blob = base64.b64decode(encoded, validate=True)
    except binascii.Error:
        raise Refused("public key is not base64") from None
    if len(blob) != 51 or not blob.startswith(b"\0\0\0\x0bssh-ed25519\0\0\0\x20"):
        raise Refused("public key is not one ssh-ed25519 key")
    return encoded


def public_key(raw, run_id):
    """The f3-generated `ssh-ed25519 KEY hfs-zq3-RUN-observer` line, nothing else."""
    try:
        text = raw.decode("ascii")
    except UnicodeDecodeError:
        raise Refused("public key file must be ASCII") from None
    fields = text[:-1].split(" ") if text.endswith("\n") and text.count("\n") == 1 else []
    if len(fields) != 3 or fields[0] != "ssh-ed25519" or fields[2] != comment(run_id):
        raise Refused("public key must be one line: ssh-ed25519 KEY " + comment(run_id))
    return key_blob(fields[1])


def comment(run_id):
    return "hfs-zq3-" + run_id + "-observer"


def forced_line(run, run_id, encoded, python, python_sha256):
    """The one authorized_keys line: restricted, source-pinned, forced observer."""
    command = " ".join([python, "-I", str(run / STAGED_OBSERVER),
                        "--probe", str(run / STAGED_PROBE), "--probe-sha256", PROBE_SHA256,
                        "--python", python, "--python-sha256", python_sha256])
    # `restrict` disables PTY, agent/port/X11 forwarding and ~/.ssh/rc.
    return ('restrict,from="' + OBSERVER_FROM + '",command="' + command + '" ssh-ed25519 ' +
            encoded + " " + comment(run_id) + "\n").encode("ascii")


def read_keys():
    """authorized_keys bytes and stat; sshd StrictModes-compatible ownership."""
    path = Path(AUTHORIZED_KEYS)
    parent = os.lstat(str(path.parent))
    if (not stat.S_ISDIR(parent.st_mode) or parent.st_uid != TRUSTED_UID or
            parent.st_mode & 0o022):
        raise Refused("root-owned .ssh directory without group/other write required")
    raw, info = read_bounded(path, KEYS_BOUND, trusted=True)
    if info.st_nlink != 1:
        raise Refused("authorized_keys must have exactly one link")
    return raw, info


def selinux_label(target):
    """The file's SELinux label, or None when the filesystem carries none."""
    try:
        return os.getxattr(target, LABEL, follow_symlinks=False)
    except OSError as failure:
        if failure.errno in (errno.ENODATA, errno.ENOTSUP):
            return None
        raise


def write_pending(pending, raw, info, label):
    """Exclusive temporary file with the original owner, mode and label."""
    fd = os.open(str(pending), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        os.fchown(fd, info.st_uid, info.st_gid)
        os.fchmod(fd, stat.S_IMODE(info.st_mode))
        if label is not None:
            os.setxattr(fd, LABEL, label)
        if os.write(fd, raw) != len(raw):
            raise OSError("short authorized_keys write")
        os.fsync(fd)
    except BaseException:
        os.close(fd)
        os.unlink(str(pending))
        raise
    os.close(fd)


def pending_path():
    """The fixed temporary name used for every atomic replacement."""
    return Path(AUTHORIZED_KEYS).with_name(".authorized_keys.hfs-observer-next")


def replace_keys(raw, expected_current):
    """Atomically replace authorized_keys if it still has the expected bytes."""
    path = Path(AUTHORIZED_KEYS)
    current, info = read_keys()
    label = selinux_label(str(path))
    pending = pending_path()
    write_pending(pending, raw, info, label)
    # Last check before the rename: a concurrent change seen here refuses and
    # is preserved. Other tools take no lock, so an edit landing between this
    # check and os.replace (an append to the old file, or another tool's own
    # rename) is silently overwritten and NOT detected: the post-rename check
    # below only catches writes after our rename. Nothing else may edit
    # authorized_keys during the window.
    if digest(read_keys()[0]) != expected_current or digest(current) != expected_current:
        os.unlink(str(pending))
        raise Refused("authorized_keys changed during the update; nothing replaced")
    os.replace(str(pending), str(path))
    fsync_directory(path.parent)
    final, _ = read_keys()
    if digest(final) != digest(raw) or selinux_label(str(path)) != label:
        raise Refused("authorized_keys differs after replacement; inspect manually")
    return digest(final)


def stage(args):
    """Copy observer.py and the installed probe into the run directory, pinned."""
    require_root()
    interpreter(args.python, args.python_sha256)
    observer, _ = read_bounded(safe_path(args.observer), 65536, trusted=False)
    check_hash(observer, require_hash(args.observer_sha256, "observer.py"), "observer.py")
    probe, _ = read_bounded(safe_path(args.probe), 65536, trusted=False)
    check_hash(probe, PROBE_SHA256, "installed probe")
    run = run_directory(args.run_id, create=True)
    lock = locked(run)
    try:
        write_exclusive(run / STAGED_OBSERVER, observer, 0o400)
        write_exclusive(run / STAGED_PROBE, probe, 0o400)
        staged_files(run, args.observer_sha256)
    finally:
        os.close(lock)
    return {"action": "stage", "run_directory": str(run), "observer_sha256": args.observer_sha256,
            "probe_sha256": PROBE_SHA256, "python": args.python,
            "python_sha256": args.python_sha256}


def enrolment_plan(args, run):
    """All checks before any write; returns (before bytes, line, receipt)."""
    interpreter(args.python, args.python_sha256)
    staged_files(run, args.observer_sha256)
    raw, _ = read_bounded(safe_path(args.public_key), 4096, trusted=False)
    encoded = public_key(raw, args.run_id)
    before, _ = read_keys()
    if digest(before) != require_hash(args.expect_sha256, "expected authorized_keys"):
        raise Refused("authorized_keys SHA256 differs from the recorded pre-run value")
    if before and not before.endswith(b"\n"):
        raise Refused("authorized_keys must end with a newline (exact restoration)")
    if encoded.encode() in before or comment(args.run_id).encode() in before:
        raise Refused("this observer key or run comment is already present")
    if os.path.lexists(str(run / RECEIPT)) or os.path.lexists(str(run / REMOVED)):
        raise Refused("this run already enrolled an observer key; use a new run ID")
    line = forced_line(run, args.run_id, encoded, args.python, args.python_sha256)
    receipt = {"version": VERSION, "run_id": args.run_id, "line": line.decode("ascii"),
               "line_sha256": digest(line), "before_sha256": digest(before),
               "after_sha256": digest(before + line), "observer_sha256": args.observer_sha256,
               "python": args.python, "python_sha256": args.python_sha256}
    return before, line, receipt


def enrol(args):
    """Append the one forced-command line; the receipt is durable beforehand."""
    require_root()
    run = run_directory(args.run_id)
    lock = locked(run)
    try:
        before, line, receipt = enrolment_plan(args, run)
        # Intent first: if the replacement fails or is uncertain, `remove` and
        # `status` still know the exact line to look for.
        write_exclusive(run / RECEIPT, canonical(receipt), 0o600)
        after = replace_keys(before + line, receipt["before_sha256"])
    finally:
        os.close(lock)
    return {"action": "enrol", "before_sha256": receipt["before_sha256"],
            "after_sha256": after, "line_sha256": receipt["line_sha256"],
            "receipt": str(run / RECEIPT)}


def load_receipt(run, run_id):
    if not os.path.lexists(str(run / RECEIPT)):
        state = "already removed" if os.path.lexists(str(run / REMOVED)) else "never enrolled"
        raise Refused("no active observer-key receipt for this run (" + state + ")")
    raw, _ = read_bounded(run / RECEIPT, 65536, trusted=True)
    receipt = json.loads(raw)
    fields = ["line", "line_sha256", "before_sha256", "after_sha256"]
    # Type checks first, so a malformed receipt refuses cleanly instead of
    # failing later with an AttributeError or KeyError.
    if (not isinstance(receipt, dict) or receipt.get("version") != VERSION or
            receipt.get("run_id") != run_id or
            not all(isinstance(receipt.get(name), str) for name in fields) or
            not receipt["line"].isascii() or
            digest(receipt["line"].encode("ascii")) != receipt["line_sha256"]):
        raise Refused("observer-key receipt does not describe this run")
    return receipt


def without_line(current, line):
    """Bytes with exactly one occurrence of the exact line removed."""
    lines = current.splitlines(keepends=True)
    if lines.count(line) != 1:
        raise Refused("enrolled line present " + str(lines.count(line)) +
                      " times (expected once); nothing changed")
    lines.remove(line)
    return b"".join(lines)


def removal(receipt, current, expected):
    """New bytes, or None when an interrupted enrol never applied the line.

    Unchanged since enrol (expected == receipt after hash): the result must be
    exactly the pre-run bytes. Line absent and file still at the pre-run hash:
    nothing to remove. Anything else removes only the exact line, once.
    """
    line = receipt["line"].encode("ascii")
    if expected == receipt["before_sha256"] and line not in current.splitlines(keepends=True):
        return None
    result = without_line(current, line)
    if expected == receipt["after_sha256"] and digest(result) != receipt["before_sha256"]:
        raise Refused("removal would not restore the pre-run bytes; nothing changed")
    return result


def remove(args):
    """Delete only the receipt's line; prove pre-run bytes when unchanged."""
    require_root()
    run = run_directory(args.run_id)
    lock = locked(run)
    try:
        receipt = load_receipt(run, args.run_id)
        current, _ = read_keys()
        expected = require_hash(args.expect_sha256, "expected authorized_keys")
        if digest(current) != expected:
            raise Refused("authorized_keys SHA256 differs from the expected current value")
        result = removal(receipt, current, expected)
        final = expected if result is None else replace_keys(result, expected)
        unchanged = expected in (receipt["after_sha256"], receipt["before_sha256"])
        # Retiring the receipt makes a repeated remove refuse instead of acting.
        os.replace(str(run / RECEIPT), str(run / REMOVED))
        fsync_directory(run)
    finally:
        os.close(lock)
    return {"action": "remove", "final_sha256": final, "before_sha256": receipt["before_sha256"],
            "restored_pre_run": final == receipt["before_sha256"],
            "unrelated_changes_acknowledged": not unchanged}


def status(args):
    """Read-only: current hash, line count and whether the run's line is present."""
    current, _ = read_keys()
    # A kill between write_pending and the rename leaves the temporary file;
    # its exclusive create then refuses every later replacement until root
    # inspects and deletes it.
    result = {"action": "status", "sha256": digest(current),
              "lines": len(current.splitlines()), "receipt": None, "enrolled_line_count": 0,
              "stale_pending": os.path.lexists(str(pending_path()))}
    run = run_directory(args.run_id)
    if os.path.lexists(str(run / RECEIPT)):
        receipt = load_receipt(run, args.run_id)
        result["receipt"] = {key: receipt[key] for key in ["before_sha256", "after_sha256"]}
        result["enrolled_line_count"] = current.splitlines(keepends=True).count(
            receipt["line"].encode("ascii"))
    return result


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="action", required=True)
    staging = sub.add_parser("stage")
    enrolling = sub.add_parser("enrol")
    for command in [staging, enrolling]:
        command.add_argument("--run-id", required=True)
        command.add_argument("--observer-sha256", required=True)
        command.add_argument("--python", required=True)
        command.add_argument("--python-sha256", required=True)
    staging.add_argument("--observer", required=True)
    staging.add_argument("--probe", required=True)
    enrolling.add_argument("--public-key", required=True)
    enrolling.add_argument("--expect-sha256", required=True)
    removing = sub.add_parser("remove")
    removing.add_argument("--run-id", required=True)
    removing.add_argument("--expect-sha256", required=True)
    sub.add_parser("status").add_argument("--run-id", required=True)
    return parser.parse_args(argv)


def main(argv=None):
    args = arguments(argv)
    action = {"stage": stage, "enrol": enrol, "remove": remove, "status": status}[args.action]
    result = action(args)
    result.update({"version": VERSION, "run_id": args.run_id, "acceptance": False})
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, Refused) as failure:
        print("observer-key refused: " + str(failure), file=sys.stderr)
        sys.exit(1)
