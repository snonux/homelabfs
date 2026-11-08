#!/usr/local/bin/python3.12 -I
"""Temporary SYSTEM /dev/fd window; flags record, never grant, authority.

Run on the exact FreeBSD host only after review/explicit host-setup authority.
No SSH, VM lifecycle, package/policy edits, forced unmount or module unload.
Interrupted dispatches are uncertain and cannot be automatically replayed.

The plan may declare one `window_module` (exact /boot/kernel path and pinned
SHA256) that the outer reviewed run loads itself between setup and restore,
e.g. fusefs.ko in zq3 Phase 4 step 4. This controller never loads it; restore
only tolerates exactly that loaded file beyond the post-load baseline.
"""
import argparse
import base64
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import stat
import subprocess
import sys
import time

VERSION = "homelabfs-fdescfs-window/1"
PARENT = Path("/var/tmp/hfs-fdescfs-windows")
# A declarable window module is a stock kernel module file; fdescfs.ko is the
# controller's own module and is handled by load_module(), never declared.
WINDOW_MODULE_PATH = re.compile(r"/boot/kernel/([a-z0-9_]+)\.ko")
COMMANDS = {"mount": "/sbin/mount", "kldload": "/sbin/kldload",
            "kldstat": "/sbin/kldstat", "sysctl": "/sbin/sysctl",
            "version": "/bin/freebsd-version", "hostname": "/bin/hostname"}


class CommandUncertain(RuntimeError):
    def __init__(self, evidence):
        super().__init__(evidence["failure"])
        self.evidence = evidence


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def unique(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "duplicate JSON key")
        value[key] = item
    return value


def load(path):
    with open(path, "rb") as stream:
        raw = stream.read((512 << 10) + 1)
    require(len(raw) <= 512 << 10, "oversized JSON")
    return json.loads(raw, object_pairs_hook=unique)


def digest(path):
    path = Path(path)
    require(path.is_absolute() and str(path) == os.path.normpath(path), "clean absolute artifact path")
    for parent in path.parents:
        info = os.lstat(parent)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX),
                "artifact ancestors must be root-controlled directories")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_size <= 128 << 20,
                "bounded regular artifact required")
        require(info.st_uid == 0 and not info.st_mode & 0o022,
                "root-owned non-writable artifact required")
        result = hashlib.sha256()
        while True:
            chunk = os.read(fd, 65536)
            if not chunk:
                return result.hexdigest()
            result.update(chunk)
    finally:
        os.close(fd)


def persist(path, value):
    raw = (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()
    require(len(raw) <= 512 << 10, "evidence bound")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)


def command(argv, started=None, tmpdir=None):
    environment = {"PATH": "/sbin:/bin:/usr/sbin:/usr/bin", "LC_ALL": "C",
                   "GOMAXPROCS": "1", "GOMEMLIMIT": "256MiB", "GOGC": "50"}
    if tmpdir is not None:
        environment["TMPDIR"] = str(tmpdir)
    child = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env=environment)
    streams = {"stdout": bytearray(), "stderr": bytearray()}
    deadline = time.monotonic() + 10
    with selectors.DefaultSelector() as selector:
        for name, stream in (("stdout", child.stdout), ("stderr", child.stderr)):
            selector.register(stream, selectors.EVENT_READ, name)
        try:
            if started is not None:
                started(child.pid)
            while selector.get_map():
                require(time.monotonic() < deadline, "command deadline; dispatch uncertain")
                for key, _ in selector.select(min(0.1, max(0, deadline - time.monotonic()))):
                    chunk = os.read(key.fd, 4096)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                        continue
                    streams[key.data].extend(chunk)
                    require(sum(map(len, streams.values())) <= 256 << 10,
                            "command evidence overflow; dispatch uncertain")
            child.wait(timeout=max(0.01, deadline - time.monotonic()))
        except BaseException as failure:
            # Only the exact short command child, never a VM or process group.
            # Its action may already have occurred; intent remains uncertain.
            try:
                child.kill()
            except ProcessLookupError:
                pass
            try:
                child.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass  # Retain uncertain child/action; never dispatch dependent work.
            raise CommandUncertain({"argv": argv, "pid": child.pid,
                "failure": str(failure), "exit": child.poll(),
                **{name + "_base64": base64.b64encode(raw).decode("ascii")
                   for name, raw in streams.items()}}) from failure
        finally:
            child.stdout.close()
            child.stderr.close()
    return {"argv": argv, "exit": child.returncode,
            **{name: raw.decode("utf-8", "strict") for name, raw in streams.items()}}


def checked(argv):
    result = command(argv)
    require(result["exit"] == 0, "read-only facts command failed: " + str(result))
    return result["stdout"]


def inspect(executable):
    result = command([executable, "--check-descriptor-exec"])
    report = json.loads(result["stdout"], object_pairs_hook=unique)
    require(report["version"] == "homelabfs-descriptor-exec/1" and
            report["platform"] == "freebsd/amd64" and
            report["supported"] == (result["exit"] == 0), "invalid inspector")
    return report


def facts(executable):
    require(sys.platform.startswith("freebsd") and os.geteuid() == 0,
            "actual FreeBSD root required")
    module = command([COMMANDS["kldstat"], "-m", "fdescfs"])
    directory = os.lstat("/dev/fd")
    require(stat.S_ISDIR(directory.st_mode) and directory.st_uid == 0 and
            not directory.st_mode & 0o022, "protected existing /dev/fd directory required")
    return {"hostname": checked([COMMANDS["hostname"]]).strip(),
            "base": checked([COMMANDS["version"], "-ku"]),
            "kernel": checked([COMMANDS["sysctl"], "-n", "kern.version"]),
            "boot": checked([COMMANDS["sysctl"], "-n", "kern.boottime"]),
            "module": module, "descriptor": inspect(executable),
            "modules": checked([COMMANDS["kldstat"], "-v"]),
            "mounts": checked([COMMANDS["mount"], "-p"]),
            "fd_directory": {name: getattr(directory, "st_" + name)
                             for name in ("mode", "uid", "gid", "dev", "ino")}}


def check_window_module(plan):
    """Validate the optional module the outer run may load during the window.

    `None` means restore requires the exact post-load baseline. Otherwise the
    declared path must also be an artifact pin with the same hash, so the
    plan-hash-bound pin set covers it and validate() re-digests the file on
    every action, including restore, before anything is dispatched.
    """
    declared = plan["window_module"]
    if declared is None:
        return
    require(isinstance(declared, dict) and set(declared) == {"path", "sha256"},
            "closed window module schema")
    match = (WINDOW_MODULE_PATH.fullmatch(declared["path"])
             if isinstance(declared["path"], str) else None)
    require(match is not None and match[1] != "fdescfs",
            "window module must be a stock /boot/kernel module other than fdescfs")
    require(plan["sha256"].get(declared["path"]) == declared["sha256"],
            "window module hash must equal its artifact pin")


def validate(plan, executable, action):
    require(set(plan) == {"version", "run_id", "expires", "directory", "inspector",
                          "sha256", "before", "qualification", "namespace_helper",
                          "module_policy", "window_module"}, "closed plan schema")
    require(plan["module_policy"] == "preserve-existing-or-retain-new",
            "explicit approved module retention policy required")
    require(plan["version"] == VERSION and
            re.fullmatch(r"[a-z0-9][a-z0-9-]{1,63}", plan["run_id"]), "plan identity")
    if action != "restore":
        require(plan["expires"] >= time.time() and plan["expires"] - time.time() <= 3600,
                "expired or unbounded plan")
    require(plan["inspector"] == executable and Path(executable).is_absolute(), "inspector path")
    check_window_module(plan)
    expected = set(COMMANDS.values()) | {executable, "/boot/kernel/kernel",
                                       "/boot/kernel/fdescfs.ko", plan["namespace_helper"],
                                       str(Path(__file__).resolve()), sys.executable}
    require(1 <= len(plan["qualification"]) <= 4, "bounded qualification inventory")
    for argv in plan["qualification"]:
        require(len(argv) == 4 and Path(argv[0]).is_absolute() and
                argv[1] == "-test.run=^TestDescriptorExecNativeQualification$" and
                argv[2:] == ["-test.v", "-test.timeout=5s"], "fixed qualification test required")
        expected.add(argv[0])
    require(set(plan["sha256"]) >= expected, "incomplete artifact pins")
    for path, wanted in plan["sha256"].items():
        require(Path(path).is_absolute() and re.fullmatch(r"[0-9a-f]{64}", wanted), "artifact pin")
        require(digest(path) == wanted, "artifact changed: " + path)


def dispatch(directory, name, argv):
    persist(directory / (name + ".intent.json"), {"argv": argv, "time": time.time()})
    # Exception, timeout, lost SSH, or process death leaves intent without result.
    # A fresh controller refuses replay. Independent inspection is required.
    try:
        result = command(argv, tmpdir=directory / "tmp",
                         started=lambda pid: persist(directory / (name + ".started.json"), {"pid": pid, "argv": argv}))
    except CommandUncertain as failure:
        persist(directory / (name + ".uncertain.json"), failure.evidence)
        raise
    persist(directory / (name + ".result.json"), result)
    return result


def module_files(text):
    """Checked-format kldstat -v inventory, ignoring only live reference counts."""
    groups = {}
    current = None
    state = None
    file_ids = set()
    lines = text.splitlines()
    require(lines and lines[0].split() == ["Id", "Refs", "Address", "Size", "Name"],
            "unrecognized kldstat columns")
    for line in lines[1:]:
        if not line.strip():
            continue
        match = re.fullmatch(
            r"\s*([1-9]\d*)\s+\d+\s+(0x[0-9a-f]+)\s+([0-9a-f]+)"
            r"\s+([^\s()]+) \((/[^()\x00\r\n]+)\)", line)
        if match:
            require(current is None or state == "entries", "incomplete kldstat module block")
            current = match[4]
            require(current not in groups and match[1] not in file_ids, "duplicate module file")
            file_ids.add(match[1])
            # The pathname is part of loaded-file identity, not discarded
            # decoration. Selected kldstat -v always prints this suffix.
            groups[current] = [list(match.group(1, 2, 3, 4, 5))]
            state = "contains"
        elif state == "contains" and line.strip() == "Contains modules:":
            state = "columns"
            groups[current].append(line.strip())
        elif state == "columns" and line.split() == ["Id", "Name"]:
            state = "entries"
            groups[current].append("Id Name")
        else:
            entry = re.fullmatch(r"\s*([1-9]\d*)\s+(\S+)", line)
            require(state == "entries" and entry is not None, "unrecognized kldstat format")
            groups[current].append(entry[1] + " " + entry[2])
    require("kernel" in groups and state == "entries", "missing/incomplete kernel module inventory")
    return groups


def load_module(plan, directory, executable, before):
    if before["module"]["exit"] == 0:
        persist(directory / "module-baseline.json", before)
        return before
    result = dispatch(directory, "module-load", [COMMANDS["kldload"], "/boot/kernel/fdescfs.ko"])
    after = facts(executable)
    persist(directory / "module-load.after.json", after)
    require(result["exit"] == 0 and after["module"]["exit"] == 0,
            "module load uncertain/failed; retain for inspection")
    for key in before.keys() - {"module", "modules"}:
        require(after[key] == before[key], "unrelated state changed during module load")
    old = module_files(before["modules"])
    new = module_files(after["modules"])
    require(new.keys() - old.keys() == {"fdescfs.ko"} and
            all(new.get(key) == value for key, value in old.items()),
            "unexpected loaded module change")
    require(new["fdescfs.ko"][0][4] == "/boot/kernel/fdescfs.ko",
            "loaded module pathname changed")
    require(any(re.fullmatch(r"\d+\s+fdescfs", line) for line in new["fdescfs.ko"][1:]),
            "fdescfs module not registered by loaded file")
    persist(directory / "module-retained.json", {
        "policy": plan["module_policy"], "original_module_absent": True,
        "loaded": new["fdescfs.ko"], "sha256": plan["sha256"]["/boot/kernel/fdescfs.ko"]})
    persist(directory / "module-baseline.json", after)
    return after


def execute(plan, directory, executable):
    require(not directory.exists(), "run directory already exists; no replay")
    require(directory.parent.is_dir() and not directory.parent.is_symlink(), "private staging parent")
    parent = directory.parent.stat()
    require(parent.st_uid == 0 and not parent.st_mode & 0o077, "root-private parent")
    directory.mkdir(mode=0o700)
    (directory / "tmp").mkdir(mode=0o700)
    persist(directory / "plan.json", plan)
    before = facts(executable)
    persist(directory / "before.json", before)
    require(before == plan["before"], "actual facts changed")
    if before["descriptor"]["mount_type"] == "fdescfs":
        require(before["descriptor"]["supported"], "existing unsupported mount: preserve unchanged")
        persist(directory / "existing.json", before["descriptor"])
        return
    require(not any(len(line.split()) > 1 and
                    (line.split()[1] == "/dev/fd" or line.split()[1].startswith("/dev/fd/"))
                    for line in before["mounts"].splitlines()),
            "existing /dev/fd mount must be preserved")
    before = load_module(plan, directory, executable, before)
    result = dispatch(directory, "mount", [plan["namespace_helper"], "mount"])
    after = facts(executable)
    persist(directory / "after.json", after)
    require(result["exit"] == 0 and after["descriptor"]["supported"], "mount failed; resources retained")
    require(after["module"] == before["module"] and module_files(after["modules"]) == module_files(before["modules"]),
            "module state changed; retain for inspection")
    old_rows = before["mounts"].splitlines()
    new_rows = after["mounts"].splitlines()
    added = [row for row in new_rows if row not in old_rows]
    require(all(row in new_rows for row in old_rows) and len(added) == 1,
            "unexpected concurrent mount change; retain for inspection")
    fields = added[0].split()
    require(len(fields) == 6 and fields[1:3] == ["/dev/fd", "fdescfs"] and
            "nosuid" in fields[3].split(",") and "noexec" not in fields[3].split(","),
            "unexpected mounted profile; retain for inspection")
    # Private nodup flags are not exported by statfs/mount -p. The semantic
    # same-regular-vnode check is independent of guessed printable flags.
    helper = json.loads(result["stdout"], object_pairs_hook=unique)
    require(helper == {"mounted_fsid": after["descriptor"]["mount_id"]},
            "helper/inspector FSID mismatch")
    persist(directory / "owned.json", after["descriptor"])


def qualify(plan, directory, executable):
    require(load(directory / "plan.json") == plan, "plan changed")
    require((directory / "owned.json").exists() or (directory / "existing.json").exists(),
            "completed setup required")
    expected = load(directory / ("owned.json" if (directory / "owned.json").exists() else "existing.json"))
    require(inspect(executable) == expected, "mount FSID/profile changed")
    for index, argv in enumerate(plan["qualification"]):
        result = dispatch(directory, "qualify-" + str(index), argv)
        require(result["exit"] == 0 and "--- PASS:" in result["stdout"] and
                "--- SKIP:" not in result["stdout"], "native qualification failed/skipped")


def window_module_group(plan, groups):
    """Check the loaded file of the declared window module; return its group.

    kldstat cannot hash loaded kernel text, so identity is the exact loaded
    pathname, the registered module name and the pinned on-disk file hash,
    re-digested here after the unmount, next to the facts it is compared with.
    """
    declared = plan["window_module"]
    path = Path(declared["path"])
    group = groups[path.name]
    require(group[0][4] == declared["path"], "window module pathname changed; retain resources")
    require(any(re.fullmatch(r"\d+\s+" + re.escape(path.stem), line) for line in group[1:]),
            "window module not registered by loaded file; retain resources")
    require(digest(declared["path"]) == declared["sha256"],
            "window module file hash changed; retain resources")
    return group


def compare_restored(plan, baseline, after):
    """Accept the post-load baseline plus at most the declared window module.

    Every non-module fact (boot, base, fdescfs module query, mounts, /dev/fd,
    descriptor report) must equal the baseline exactly. Every baseline file,
    including fdescfs.ko, must still be loaded unchanged (reference counts
    aside, as in module_files). The only tolerated addition is the declared
    window module file; it may be absent when the run stopped before loading
    it. Returns the loaded window module group, or None.
    """
    require(after.keys() == baseline.keys() and
            all(after[key] == baseline[key] for key in baseline.keys() - {"modules"}),
            "normal mount restore incomplete; retain resources")
    old = module_files(baseline["modules"])
    new = module_files(after["modules"])
    require(all(new.get(name) == group for name, group in old.items()),
            "baseline module changed or missing; retain resources")
    added = new.keys() - old.keys()
    if not added:
        return None
    declared = plan["window_module"]
    require(declared is not None and added == {Path(declared["path"]).name},
            "unexpected module loaded during window; retain resources")
    return window_module_group(plan, new)


def restore(plan, directory, executable):
    require(load(directory / "plan.json") == plan, "plan changed")
    require(not (directory / "restore.intent.json").exists(), "restore already dispatched; inspect uncertain/completed state")
    if (directory / "existing.json").exists():
        require(inspect(executable) == load(directory / "existing.json"), "existing mount changed")
        persist(directory / "preserved.json", {"existing_mount_unchanged": True})
        return
    owned = load(directory / "owned.json")
    require(inspect(executable) == owned, "owned FSID/profile changed; no unmount")
    result = dispatch(directory, "restore", [plan["namespace_helper"], "unmount", owned["mount_id"]])
    after = facts(executable)
    persist(directory / "restored.json", after)
    require(result["exit"] == 0, "normal mount restore incomplete; retain resources")
    # Any refusal below leaves owned.json and no summary: resources retained.
    loaded = compare_restored(plan, load(directory / "module-baseline.json"), after)
    require(json.loads(result["stdout"], object_pairs_hook=unique) == {"unmounted_fsid": owned["mount_id"]},
            "helper unmount FSID report mismatch")
    retained = (directory / "module-retained.json").exists()
    persist(directory / "restoration-summary.json", {
        "mount_restored": True,
        "module_retained_by_reviewed_plan": retained,
        "window_module": plan["window_module"],
        "window_module_loaded": loaded,
        "full_original_system_state_restored": not retained and loaded is None})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["facts", "setup", "qualify", "restore"])
    parser.add_argument("--inspector", required=True)
    parser.add_argument("--plan")
    parser.add_argument("--plan-sha256")
    parser.add_argument("--directory")
    args = parser.parse_args()
    require(sys.flags.isolated, "invoke the pinned Python interpreter with -I")
    require(sys.platform.startswith("freebsd") and os.geteuid() == 0,
            "actual FreeBSD root required")
    if args.action == "facts":
        print(json.dumps(facts(args.inspector), sort_keys=True))
        return
    require(digest(args.plan) == args.plan_sha256,
            "exact reviewed plan hash required; this does not grant authority")
    plan = load(args.plan)
    validate(plan, args.inspector, args.action)
    directory = Path(args.directory)
    require(str(directory) == plan["directory"] and directory.is_absolute() and
            directory.name == plan["run_id"] and directory.parent == PARENT,
            "run-owned absolute directory under fixed staging parent")
    parent = os.lstat(PARENT)
    require(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0 and
            not parent.st_mode & 0o077, "root-private fixed staging parent")
    if args.action != "setup":
        run = os.lstat(directory)
        require(stat.S_ISDIR(run.st_mode) and run.st_uid == 0 and
                not run.st_mode & 0o077, "root-private existing run directory")
    # The staging parent is precreated privately during approved preparation.
    lock = os.open(directory.parent / ".fdescfs-window.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        {"setup": execute, "qualify": qualify, "restore": restore}[args.action](plan, directory, args.inspector)
    finally:
        os.close(lock)


if __name__ == "__main__":
    main()
