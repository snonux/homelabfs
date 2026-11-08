#!/usr/local/bin/python3.12 -I
"""f3 host facts, setup and post-exit cleanup for the zq3 direct bhyve launch.

Run only on f3 as root with `/usr/local/bin/python3.12 -I -B`, inside an
explicitly approved window (zq3 plan A4). Flags and pinned hashes record the
approved inputs; they never grant authority.

* `facts` is read-only (Phase 1). It never imports sibling files, so it can
  also run as `python3.12 -I -B - facts < host.py`.
* `setup` (Phase 7) copies the UEFI vars and creates one labelled tap in
  `vm-public`. It refuses on any launch-fact drift, a running VM, an existing
  vmm context, tap or nmdm node, or an existing launch directory (no replay).
* `cleanup` (Phase 11) runs only after the exporter child is positively gone
  (`--child-pid` from runtime evidence, or `--no-child` after an abort before
  bhyve started). It destroys only the run's own vmm context and tap, both
  proven absent before setup, and preserves the vars copy as evidence.

Never: VM lifecycle commands, signals to any process (no kill, even on a
command deadline), forced unmounts, or a lookup of /dev/nmdm* or /dev/tap*
paths (devfs cloning would create those devices); /dev is only listed.
Symlinks are refused everywhere except the one package-installed firmware
link (FIRMWARE), whose text and confined target are recorded as facts.
Every mutating command writes an intent first; an intent without a result is
uncertain and blocks every later run until root reconciles it.
"""
import argparse
import errno
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import selectors
import stat
import subprocess
import sys
import time

COMMANDS = {"ifconfig": "/sbin/ifconfig", "bhyvectl": "/usr/sbin/bhyvectl",
            "ps": "/bin/ps", "vm": "/usr/local/sbin/vm"}
FACTS_VERSION = "homelabfs-zq3-launch-facts/1"
ROCKY_DIR = "/zroot/bhyve/rocky"
HASHED = (ROCKY_DIR + "/rocky.conf", "/usr/local/lib/vm-bhyve/vm-run",
          ROCKY_DIR + "/uefi-vars.fd")
# pkg edk2-bhyve installs the bootrom path as a relative symlink
# (../edk2-bhyve/BHYVE_UEFI.fd) and bhyve follows it. It is the only symlink
# this tool follows: its target must resolve inside FIRMWARE_CONFINE and is
# digested like every other pinned file. The exact link text, target path and
# target hash are pins in argv.py (`facts` never imports it), re-checked by
# argv.py at generate time and again by `setup`.
FIRMWARE = "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"
FIRMWARE_CONFINE = "/usr/local/share"
DISK = ROCKY_DIR + "/disk0.img"
LOG = ROCKY_DIR + "/vm-bhyve.log"
BRIDGE = "vm-public"
COMMAND_SECONDS = 10
OUTPUT_LIMIT = 256 << 10
LOG_TAIL = 64 << 10
# Children whose command outcome is uncertain stay referenced for the process
# lifetime; their PID is in the uncertain record for root inspection.
RETAINED = []


class Uncertain(RuntimeError):
    """A dispatched command's outcome is unknown; it is never replayed."""

    def __init__(self, evidence):
        super().__init__(evidence["failure"])
        self.evidence = evidence


class Refused(RuntimeError):
    """A precondition failed; nothing further was dispatched."""


def require(ok, message):
    if not ok:
        raise Refused(message)


def bounded_command(argv):
    """Run one short host command with a deadline and output bound.

    On deadline or overflow the child is left alone (never signalled): its
    action may already have happened, so the step is recorded as uncertain.
    """
    child = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env={"PATH": "/sbin:/bin:/usr/sbin:/usr/bin",
                                                          "LC_ALL": "C"})
    streams = {"stdout": bytearray(), "stderr": bytearray()}
    deadline = time.monotonic() + COMMAND_SECONDS
    try:
        with selectors.DefaultSelector() as selector:
            for name, stream in (("stdout", child.stdout), ("stderr", child.stderr)):
                selector.register(stream, selectors.EVENT_READ, name)
            while selector.get_map():
                if time.monotonic() >= deadline:
                    raise TimeoutError("command deadline")
                for key, _ in selector.select(0.1):
                    chunk = os.read(key.fd, 4096)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    streams[key.data].extend(chunk)
                    if sum(map(len, streams.values())) > OUTPUT_LIMIT:
                        raise OverflowError("command output bound")
        child.wait(timeout=max(0.01, deadline - time.monotonic()))
    except (TimeoutError, OverflowError, subprocess.TimeoutExpired) as failure:
        RETAINED.append(child)  # referenced, never signalled or silently reaped
        raise Uncertain({"argv": argv, "pid": child.pid, "failure": str(failure),
                         "exit": child.poll()}) from failure
    finally:
        child.stdout.close()
        child.stderr.close()
    return {"argv": argv, "exit": child.returncode,
            **{name: raw.decode("utf-8", "replace") for name, raw in streams.items()}}


class Host:
    """The FreeBSD boundary. Production uses root "/" and owner uid 0.

    Tests substitute an explicit temporary root, their own uid and a command
    simulator; the checks themselves are unchanged.
    """

    def __init__(self, root="/", owner=0, runner=bounded_command):
        self.root = Path(root)
        self.owner = owner
        self.runner = runner

    def path(self, absolute):
        require(isinstance(absolute, str) and absolute.startswith("/") and
                os.path.normpath(absolute) == absolute, "clean absolute path required")
        return self.root / absolute.lstrip("/")

    def command(self, argv):
        return self.runner(argv)

    def listdir(self, absolute):
        # Listing never triggers devfs cloning; a name lookup could.
        try:
            return sorted(os.listdir(self.path(absolute)))
        except FileNotFoundError:
            return []

    def check_ancestors(self, absolute):
        target = self.path(absolute)
        for parent in target.parents:
            if len(parent.parts) < len(self.root.parts):
                break
            info = os.lstat(parent)
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == self.owner and
                    (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX),
                    "ancestors must be owner-controlled directories: " + str(parent))

    def open_checked(self, absolute):
        """Open a regular, owner-owned, not group/other-writable file.

        O_NOFOLLOW refuses a symlink at the final component; FreeBSD reports
        that as EMLINK ("Too many links"), Linux as ELOOP. Both become a clean
        refusal instead of a traceback.
        """
        self.check_ancestors(absolute)
        try:
            fd = os.open(self.path(absolute), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        except OSError as failure:
            if failure.errno in (errno.ELOOP, errno.EMLINK):
                raise Refused("symlink refused: " + absolute) from failure
            raise
        info = os.fstat(fd)
        if not (stat.S_ISREG(info.st_mode) and info.st_uid == self.owner and
                not info.st_mode & 0o022):
            os.close(fd)
            raise Refused("protected regular file required: " + absolute)
        return fd, info

    def resolve_link(self, absolute, confine):
        """Return (text, target) of one owner-owned symlink confined to `confine`.

        The target is resolved lexically, which is exact because both the
        link's and (via digest) the target's ancestors are checked with lstat
        and must be real directories, never symlinks. Only one hop is
        followed: the target is opened with O_NOFOLLOW later.
        """
        self.check_ancestors(absolute)
        info = os.lstat(self.path(absolute))
        require(stat.S_ISLNK(info.st_mode) and info.st_uid == self.owner,
                "owner-owned symlink required: " + absolute)
        text = os.readlink(self.path(absolute))
        target = os.path.normpath(os.path.join(os.path.dirname(absolute), text))
        require(target.startswith(confine + "/"),
                "symlink target outside %s: %s -> %s" % (confine, absolute, text))
        return text, target

    def digest(self, absolute):
        fd, info = self.open_checked(absolute)
        result = hashlib.sha256()
        try:
            while chunk := os.read(fd, 1 << 16):
                result.update(chunk)
        finally:
            os.close(fd)
        return {"sha256": result.hexdigest(), "bytes": info.st_size}

    def read(self, absolute, limit, tail=False):
        fd, info = self.open_checked(absolute)
        try:
            start = max(0, info.st_size - limit) if tail else 0
            require(tail or info.st_size <= limit, "oversized file: " + absolute)
            os.lseek(fd, start, os.SEEK_SET)
            raw = os.read(fd, limit)
        finally:
            os.close(fd)
        return raw, start > 0

    def size(self, absolute):
        self.check_ancestors(absolute)
        info = os.lstat(self.path(absolute))
        require(stat.S_ISREG(info.st_mode), "regular file required: " + absolute)
        return info.st_size

    def exists(self, absolute):
        return os.path.lexists(self.path(absolute))

    def check_private_dir(self, absolute):
        self.check_ancestors(absolute)
        info = os.lstat(self.path(absolute))
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == self.owner and
                not info.st_mode & 0o077, "private owner-only directory required: " + absolute)

    def mkdir_private(self, absolute):
        os.mkdir(self.path(absolute), 0o700)
        self.check_private_dir(absolute)

    def persist(self, absolute, value):
        raw = (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()
        self.write_exclusive(absolute, raw)

    def write_exclusive(self, absolute, raw):
        target = self.path(absolute)
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        parent = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)

    def load(self, absolute):
        raw, _ = self.read(absolute, 1 << 20)
        return json.loads(raw)

    def copy_exclusive(self, source, target):
        """Copy with hashing into a new 0600 file; the source is only read."""
        fd, _ = self.open_checked(source)
        result = hashlib.sha256()
        size = 0
        out = os.open(self.path(target), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        try:
            while chunk := os.read(fd, 1 << 16):
                os.write(out, chunk)
                result.update(chunk)
                size += len(chunk)
            os.fsync(out)
        finally:
            os.close(out)
            os.close(fd)
        return {"sha256": result.hexdigest(), "bytes": size}


def checked_stdout(host, argv):
    result = host.command(argv)
    require(result["exit"] == 0, "read-only command failed: %s" % (result,))
    return result["stdout"]


def bridge_members(text):
    return sorted(m[1] for m in re.finditer(r"^\s+member: (\S+) ", text, re.MULTILINE))


def firmware_fact(host):
    """Link text, confined target and the target's digest for the bootrom.

    The text is recorded rather than compared here so that Phase 1 evidence
    shows an unexpected (but confined) link; argv.check_facts refuses it.
    """
    text, target = host.resolve_link(FIRMWARE, FIRMWARE_CONFINE)
    return {"link": text, "target": target, **host.digest(target)}


def collect_facts(host):
    """Read-only launch facts; the same collector feeds Phase 1 and setup."""
    conf_raw, _ = host.read(HASHED[0], 64 << 10)
    tail, truncated = host.read(LOG, LOG_TAIL, tail=True)
    files = {path: host.digest(path) for path in HASHED}
    files[FIRMWARE] = firmware_fact(host)
    files[DISK] = {"bytes": host.size(DISK)}
    bridge = host.command([COMMANDS["ifconfig"], BRIDGE])
    return {"version": FACTS_VERSION, "collected": time.time(), "files": files,
            "rocky_conf": conf_raw.decode("utf-8", "strict"),
            "log": {"tail": tail.decode("utf-8", "replace"), "truncated": truncated},
            "vm_list": checked_stdout(host, [COMMANDS["vm"], "list"]),
            "vmm": host.listdir("/dev/vmm"),
            "interfaces": checked_stdout(host, [COMMANDS["ifconfig"], "-l"]).split(),
            "bridge_members": bridge_members(bridge["stdout"]) if bridge["exit"] == 0 else None,
            "dev_nmdm": [n for n in host.listdir("/dev") if n.startswith("nmdm")],
            "processes": checked_stdout(host, [COMMANDS["ps"], "-ax", "-ww", "-o", "pid=",
                                               "-o", "args="]).splitlines()}


def vm_rows(text):
    """`vm list` rows as {name: "Stopped" | other last-column state}."""
    lines = text.splitlines()
    require(lines and lines[0].split()[:1] == ["NAME"], "unrecognized vm list output")
    rows = {}
    for line in lines[1:]:
        if line.strip():
            fields = line.split()
            rows[fields[0]] = "Stopped" if fields[-1] == "Stopped" else line
    return rows


def process_names(lines, name):
    """Processes whose argv/title carries `name` as a token.

    The runtime execs the exporter as /dev/fd/4, so bhyve's setproctitle is
    expected to read `4: <vm>` rather than `bhyve: <vm>` (source reading, not
    yet observed natively); tokens split on whitespace and colons cover both.
    """
    return [line for line in lines if name in re.split(r"[\s:]+", line.strip())]


def load_argv_module():
    """Load the sibling pure module by explicit path (-I drops the script dir)."""
    source = Path(__file__).resolve().with_name("argv.py")
    spec = importlib.util.spec_from_file_location("directlaunch_argv", source)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_record(host, argv_module, record_path, record_sha256):
    raw, _ = host.read(record_path, 1 << 20)
    record = json.loads(raw)
    try:
        argv_module.validate_record(raw, record, record_sha256)
    except argv_module.Refused as refusal:
        raise Refused(str(refusal)) from refusal
    return raw, record


def uncertain_steps(host, launch):
    names = host.listdir(launch)
    return [n[:-len(".intent.json")] for n in names if n.endswith(".intent.json") and
            n[:-len(".intent.json")] + ".result.json" not in names]


def dispatch(host, launch, name, argv):
    """Intent, command, result. A failure or unknown outcome blocks replay."""
    host.persist("%s/%s.intent.json" % (launch, name), {"argv": argv, "time": time.time()})
    try:
        result = host.command(argv)
    except Uncertain as failure:
        host.persist("%s/%s.uncertain.json" % (launch, name), failure.evidence)
        raise
    if result["exit"] != 0:
        host.persist("%s/%s.failed.json" % (launch, name), result)
        raise Refused("%s failed; resources retained for inspection" % name)
    host.persist("%s/%s.result.json" % (launch, name), result)
    return result


def tap_label(text):
    match = re.search(r"^\s+description: (.*)$", text, re.MULTILINE)
    return match[1] if match else ""


def tap_flags(text):
    match = re.match(r"\S+: flags=[0-9a-f]+<([^>]*)>", text)
    return match[1].split(",") if match else []


def gate_facts(argv_module, facts, record):
    """Launch facts must still match the baseline and the record's vars pin."""
    try:
        pins = argv_module.check_facts(facts)
    except argv_module.Refused as refusal:
        raise Refused("launch facts drifted: %s" % refusal) from refusal
    rec = record["host"]
    require(pins["vars_sha256"] == rec["vars_source_sha256"] and
            pins["vars_bytes"] == rec["vars_bytes"], "UEFI vars changed since the record was pinned")


def check_quiescent(facts, record):
    """Nothing may run or exist that the launch would collide with."""
    rows = vm_rows(facts["vm_list"])
    require(rows and all(state == "Stopped" for state in rows.values()),
            "every vm-bhyve guest must be Stopped (Rocky stopped in Phase 5)")
    require(record["vm"] not in rows, "run vmm name collides with a vm-bhyve guest")
    # No context at all: also catches a retained child from an earlier attempt.
    require(facts["vmm"] == [], "existing vmm contexts: %s" % facts["vmm"])
    require(not process_names(facts["processes"], record["vm"]),
            "a process already names the run VM")
    rec = record["host"]
    require(rec["tap"] not in facts["interfaces"], "pinned tap already exists")
    require(facts["bridge_members"] is not None, "bridge %s missing" % BRIDGE)
    require(not {rec["nmdm"] + "A", rec["nmdm"] + "B"} & set(facts["dev_nmdm"]),
            "run nmdm nodes already exist")


def copy_vars(host, rec):
    """Copy the quiescent vars file; the original is only ever read."""
    copied = host.copy_exclusive(rec["vars_source"], rec["vars_copy"])
    source = host.digest(rec["vars_source"])
    host.persist(rec["launch"] + "/vars.json", {"copy": copied, "source_after": source})
    pinned = {"sha256": rec["vars_source_sha256"], "bytes": rec["vars_bytes"]}
    require(copied == pinned and source == pinned,
            "UEFI vars copy/source differ from the pin; copy retained")


def create_tap(host, rec, before):
    """Create, label, bridge and raise exactly the pinned tap unit."""
    launch, tap, ifconfig = rec["launch"], rec["tap"], COMMANDS["ifconfig"]
    dispatch(host, launch, "tap-create", [ifconfig, tap, "create"])
    after = checked_stdout(host, [ifconfig, "-l"]).split()
    require(set(after) - set(before) == {tap} and set(before) <= set(after),
            "unexpected interface change after tap create; retained")
    dispatch(host, launch, "tap-label", [ifconfig, tap, "description", rec["label"]])
    dispatch(host, launch, "tap-bridge", [ifconfig, rec["bridge"], "addm", tap])
    dispatch(host, launch, "tap-up", [ifconfig, tap, "up"])
    show = checked_stdout(host, [ifconfig, tap])
    require(tap_label(show) == rec["label"] and "UP" in tap_flags(show),
            "tap state not as configured; retained")
    members = bridge_members(checked_stdout(host, [ifconfig, rec["bridge"]]))
    require(tap in members, "tap is not a %s member; retained" % rec["bridge"])


def setup(host, argv_module, raw, record):
    """Phase 7 host setup. Refusals before mkdir leave nothing behind."""
    rec = record["host"]
    host.check_private_dir(rec["staging"])
    require(not host.exists(rec["launch"]), "launch directory exists; no replay")
    facts = collect_facts(host)
    gate_facts(argv_module, facts, record)
    check_quiescent(facts, record)
    host.mkdir_private(rec["launch"])
    host.write_exclusive(rec["launch"] + "/record.json", raw)
    host.persist(rec["launch"] + "/before.json", facts)
    copy_vars(host, rec)
    create_tap(host, rec, facts["interfaces"])
    owned = {"vm": record["vm"], "vmm_absent_before": True, "nmdm_absent_before": True,
             "tap": rec["tap"], "bridge": rec["bridge"], "label": rec["label"],
             "vars_copy": rec["vars_copy"], "args_sha256": record["args_sha256"]}
    host.persist(rec["launch"] + "/owned.json", owned)
    return owned


def require_child_gone(host, vm, child_pid):
    """Cleanup waits for a clean guest poweroff; it never ends the child.

    `child_pid=None` declares that the runtime never started bhyve (Phase 7/8
    abort). A started child creates the vmm context, so its presence refuses
    that declaration and the exact PID from runtime evidence is required.
    """
    if child_pid is None:
        require(vm not in host.listdir("/dev/vmm"),
                "vmm context %s exists, so a child started; give its exact PID" % vm)
    else:
        require(isinstance(child_pid, int) and child_pid > 1, "exact exporter child PID required")
        probe = host.command([COMMANDS["ps"], "-p", str(child_pid), "-o", "pid="])
        require(probe["exit"] != 0 and not probe["stdout"].strip(),
                "exporter child PID %d still exists; never killed, wait for its exit" % child_pid)
    lines = checked_stdout(host, [COMMANDS["ps"], "-ax", "-ww", "-o", "pid=", "-o", "args="])
    require(not process_names(lines.splitlines(), vm),
            "a process still names %s; never killed" % vm)


def release_vmm(host, record, before):
    """Destroy only the run's own context, proven absent before setup."""
    vm, launch = record["vm"], record["host"]["launch"]
    if host.exists(launch + "/vmm-destroy.result.json"):
        return "destroyed-earlier"
    current = host.listdir("/dev/vmm")
    if vm not in current:
        return "absent"
    require(before["vmm"] == [], "vmm context was not proven absent before setup; not ours")
    require(vm not in vm_rows(checked_stdout(host, [COMMANDS["vm"], "list"])),
            "vmm name is a vm-bhyve guest; never destroyed")
    require(re.fullmatch(r"hfs-zq3-[a-z0-9][a-z0-9-]{0,19}", vm) is not None,
            "vmm name outside the run namespace")
    foreign = [name for name in current if name != vm]
    dispatch(host, launch, "vmm-destroy", [COMMANDS["bhyvectl"], "--destroy", "--vm=" + vm])
    after = host.listdir("/dev/vmm")
    require(vm not in after and after == foreign,
            "vmm destroy did not remove exactly the run context; retained")
    return "destroyed"


def release_tap(host, rec, before):
    """Destroy only the pinned tap this run created, checked by its label."""
    launch, tap, ifconfig = rec["launch"], rec["tap"], COMMANDS["ifconfig"]
    if not host.exists(launch + "/tap-create.result.json"):
        return "never-created"
    if host.exists(launch + "/tap-destroy.result.json"):
        return "destroyed-earlier"
    require(tap not in before["interfaces"], "tap existed before setup; not ours")
    interfaces = checked_stdout(host, [ifconfig, "-l"]).split()
    if tap not in interfaces:
        return "absent"
    # Before the label step ran, our freshly created tap has no description.
    wanted = rec["label"] if host.exists(launch + "/tap-label.result.json") else ""
    require(tap_label(checked_stdout(host, [ifconfig, tap])) == wanted,
            "tap %s carries a foreign description; never destroyed" % tap)
    dispatch(host, launch, "tap-destroy", [ifconfig, tap, "destroy"])
    after = checked_stdout(host, [ifconfig, "-l"]).split()
    require(tap not in after and set(after) == set(interfaces) - {tap},
            "tap destroy changed other interfaces; retained")
    return "destroyed"


def vars_evidence(host, rec):
    """The copy is preserved as evidence; the original must be unchanged."""
    copy = host.digest(rec["vars_copy"]) if host.exists(rec["vars_copy"]) else None
    source = host.digest(rec["vars_source"])
    return {"copy_preserved": copy, "source": source,
            "original_unchanged": source == {"sha256": rec["vars_source_sha256"],
                                             "bytes": rec["vars_bytes"]}}


def cleanup(host, raw, record, child_pid):
    """Phase 11 post-exit cleanup; resumable only across clean refusals."""
    rec = record["host"]
    launch = rec["launch"]
    require(host.exists(launch + "/record.json") and host.exists(launch + "/before.json"),
            "setup never created this launch directory; nothing to clean")
    host.check_private_dir(launch)
    stored, _ = host.read(launch + "/record.json", 1 << 20)
    require(stored == raw, "launch directory belongs to a different record")
    require(not host.exists(launch + "/cleanup-summary.json"), "cleanup already completed")
    blocked = uncertain_steps(host, launch)
    require(not blocked, "uncertain or failed steps need root reconciliation: %s" % blocked)
    before = host.load(launch + "/before.json")
    require_child_gone(host, record["vm"], child_pid)
    summary = {"vmm": release_vmm(host, record, before), "tap": release_tap(host, rec, before),
               # nmdm pairs have no destroy command; they are inert once closed.
               "nmdm_present": sorted({rec["nmdm"] + "A", rec["nmdm"] + "B"} &
                                      set(host.listdir("/dev"))),
               "vars": vars_evidence(host, rec)}
    host.persist(launch + "/cleanup-summary.json", summary)
    require(summary["vars"]["original_unchanged"], "original UEFI vars changed; inspect")
    return summary


def run_locked(host, staging, action):
    """One setup/cleanup at a time per run (the staging dir is root-private)."""
    host.check_private_dir(staging)
    fd = os.open(host.path(staging + "/.directlaunch.lock"),
                 os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return action()
    finally:
        os.close(fd)


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("action", choices=["facts", "setup", "cleanup"])
    parser.add_argument("--record", help="pinned argv record on this host")
    parser.add_argument("--record-sha256", help="approved record SHA256")
    child = parser.add_mutually_exclusive_group()
    child.add_argument("--child-pid", type=int, help="exporter child PID (cleanup)")
    child.add_argument("--no-child", action="store_true",
                       help="cleanup after an abort before the runtime started bhyve")
    args = parser.parse_args(argv)
    if args.action != "facts" and not (args.record and args.record_sha256):
        parser.error("setup/cleanup need --record and --record-sha256")
    if args.action == "cleanup" and args.child_pid is None and not args.no_child:
        parser.error("cleanup needs --child-pid or --no-child")
    return args


def execute(host, args):
    if args.action == "facts":
        return collect_facts(host)
    module = load_argv_module()
    raw, record = load_record(host, module, args.record, args.record_sha256)
    if args.action == "setup":
        return run_locked(host, record["host"]["staging"],
                          lambda: setup(host, module, raw, record))
    return run_locked(host, record["host"]["staging"],
                      lambda: cleanup(host, raw, record, args.child_pid))


def main(argv=None):
    args = parse_args(argv)
    if not (sys.flags.isolated and sys.platform.startswith("freebsd") and os.geteuid() == 0):
        print(json.dumps({"refused": "run as FreeBSD root with python3.12 -I -B"}), file=sys.stderr)
        return 2
    try:
        result = execute(Host(), args)
    except Refused as refusal:
        print(json.dumps({"refused": str(refusal)}), file=sys.stderr)
        return 2
    except Uncertain as failure:
        print(json.dumps({"uncertain": failure.evidence}), file=sys.stderr)
        return 3
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
