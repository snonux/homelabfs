#!/usr/bin/env python3
"""zq3 gap G1: Phase 8 bootstrap orchestrator (workstation side).

Drives the controlled runtime's staged bootstrap in the order the code
requires (internal/controlledruntime/driver_phases.go): launch under
runtime-guard.py, the proposal (AWAITING_OPERATOR_ENROLLMENT), the guest
admission manifest (rocky_kernel.py admission) with boot ID, provider nonce
and 9p device names in one SSH round trip, the enrollment request
(`hfs-runtime-prep enroll-request`, carrying Observer.ManifestHash), the
operator's signed enrollment, the provision sysfs write, the enrollment
handoff to the runtime's EnrollmentFile (the runtime then runs
ObserveAdmission), AWAITING_GUEST_BIND, the bind request, the operator's
signed bind, the bind sysfs write, both guest mounts and READY, all inside
`bootstrap_seconds` (at most 120) from launch. Then the post-READY usability
gate (`usable`, k64 + b84): both mount roots must still answer an lstat after
the guest's 30 s QUERY timeout has passed for the mounts' own claims, and the
runtime's OBSERVE must show every provider claim settled and the final
probe's GETATTR and PROCESS_EXIT claims TERMINAL, under its own bound counted
from mount-1. The lstat writes nothing into the mounts, but each one adds
GETATTR and PROCESS_EXIT claims to the runtime's provider ledgers. See the
run plan docs/lab/zq3-run-plan.md, Phase 8.

It never reads the operator private key and never signs. At each signing
point it prints the exact `hfs-retained9p-operator` command for the operator
(approval A7) and waits for that separate invocation's fresh output
directory, then checks the signed files against what it measured.

Every step writes a durable intent record before it starts and a result
after it (`journal.jsonl`). A step whose effect may have happened without
being observed (an SSH timeout during a sysfs write, say) ends `uncertain`:
the run stops, nothing dependent runs, and nothing is ever replayed (a run
directory cannot be reused; `status` reports open or uncertain steps). A
timeout stops dependent steps and preserves every remote resource. The
orchestrator never signals, kills, unmounts or deletes anything remote: on
failure the runtime's own bootstrap deadline fences it and the guard records
its exit. Read-only polls are the only retried calls.

Every SSH/OS/runtime boundary is injected (`Boundaries`): local tests run the
whole sequence and its refusals against simulated f3, Rocky, runtime and
operator. Nothing here has run against the lab; running it needs Paul's
explicit approval of the filled plan (A3, A4, A7).
"""
import argparse
import base64
import copy
import datetime
import errno
import importlib.util
import json
import os
from pathlib import Path
import re
import selectors
import shlex
import signal
import stat
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent


def _sibling(name, filename):
    """Load a sibling module by path: `python3 -I` drops the script directory."""
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# Shared, reviewed primitives: strict JSON, durable files, target and host-key
# pins (G8) and the pinned SSH option set of the probe controller.
controller = _sibling("retained9p_controller", "controller.py")
digest, canonical, parse, read = controller.digest, controller.canonical, controller.parse, controller.read

VERSION = "homelabfs-zq3-bootstrap-plan/1"
# /2: the step list gained `usable` (k64); fill_plan.py refuses /1 journals.
# /3: `usable` also requires the probes' claims TERMINAL via OBSERVE (b84) and
# the run consumes lifecycle sequences (summary `observe_sequence_last`), so a
# /2 journal's `ok` usable proves less; fill_plan.py refuses /2 by version.
JOURNAL_VERSION = "homelabfs-zq3-bootstrap-journal/3"
EVIDENCE_VERSION = "homelabfs-controlled-evidence/2"  # OBSERVE with provider Claims (b84)
OBSERVER_VERSION = "homelabfs-runtime-observer-input/1"  # internal/runtimeprep
REMOTE = HERE / "bootstrap_remote.py"
MOUNT_OPTIONS = "trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536"
STAGES = ("AWAITING_OPERATOR_ENROLLMENT", "AWAITING_GUEST_BIND", "READY")
BOOTSTRAP_LIMIT = 120          # base gate: launch to READY
POLL_SECONDS = 1.0             # host/guest polls (one SSH round trip each)
SIGNER_POLL_SECONDS = 0.25     # local signer output directory
MIN_DISPATCH = 3.0             # never start a mutating call with less left
# Steps with a remote child need a realistic floor, not MIN_DISPATCH: admission
# waits up to 30 s for `udevadm settle` (+5 s remote margin); a 9p mount must
# have time to complete its HELLO. Below the floor the step fails undispatched.
MIN_REMOTE_SECONDS = {"admission": 40.0, "mount-0": 10.0, "mount-1": 10.0}
# Post-READY usability gate (k64). In zq3-r6/r7 READY came with every step ok
# while the mount(8) PROCESS_EXIT claims stayed RESERVED/UNPREPARED: the guest
# QUERYs such a claim for GUEST_QUERY_SECONDS (its `jiffies + 30 * HZ`
# deadline), then blocks the session, and every later operation, even
# getattr on the root, returns EACCES. A root stat right after READY can
# still pass, so the final probe waits until that window has closed for the
# later mount, plus a margin. The guest checks its deadline only after each
# QUERY reply, and each QUERY has its own 30 s transport deadline, so the
# block lands at 30 s plus one QUERY round trip. r7's round trips took
# milliseconds, so 10 s of margin covers it. A QUERY stalled for longer than
# the margin would let the final probe run before the block; such a stall
# would most likely also stall the probe's own GETATTR past STAT_SECONDS,
# which fails the gate as a timeout. The gate narrows the r7 failure, it is
# not a proof.
#
# Host-side claim check (b84). zq3-r8 showed the lstat alone is not enough:
# m0's first-probe lstat returned, but the probe child's own PROCESS_EXIT
# claim stayed RESERVED/UNPREPARED and the guest blocked m0 about 30 s
# later. So the gate also reads the runtime's OBSERVE (one pinned
# `hfs-controlled-runtime --operation observe` client on f3 per poll), whose
# provider ledgers now carry a claim summary (evidence /2):
#   - at the settle point, before the final probe: no open claim in either
#     ledger (the mounts' and the first probe's claims all settled), the
#     `baseline`;
#   - after the final probe, polled until the gate's deadline: per provider
#     at least CLAIMS_PER_PROBE numbers allocated since the baseline, no open
#     claim, and both a GETATTR and a PROCESS_EXIT newer than the baseline
#     TERMINAL (or already retired, which implies TERMINAL), the `claims`
#     phase.
# Claim numbers are per principal; the guest uses one principal per session
# (the enrolled realm/subject/anchor/nonce), so the gate requires exactly one
# and compares numbers against the baseline's Allocated.
# OBSERVE is not free: it refreshes each Admission through the guest
# observer (a failed refresh fences that Admission, as any VIEW_ACK refresh
# would) and consumes one lifecycle sequence per attempt. Phase 9 must use a
# sequence above summary.json's `observe_sequence_last`.
GUEST_QUERY_SECONDS = 30
USABLE_SETTLE_SECONDS = GUEST_QUERY_SECONDS + 10   # final probe, counted from mount-1's end
STAT_SECONDS = 8               # one guest lstat child per root; a healthy root answers in ms
USABLE_PROBE_SECONDS = 30      # per-call cap, and the slack after the settle point
# After the final probe the claims get up to the guest's own QUERY window to
# reach TERMINAL: a claim that takes longer blocks the session anyway.
USABLE_CLAIM_SECONDS = GUEST_QUERY_SECONDS
CLAIM_POLL_SECONDS = 2.0       # between OBSERVEs; each one also SSH-observes the guest
CLAIMS_PER_PROBE = 2           # the lstat child's GETATTR and its PROCESS_EXIT, per root
# Ending of the local guard SSH process says little about f3: the guard may
# have finished (its receipt says why) or only the connection was lost.
TRANSPORT_LOST = ("The local guard SSH transport ended. If the guard itself finished, its receipt.json on f3 "
                  "says why. After a lost connection the guard and the runtime KEEP RUNNING on f3 (no tty, so "
                  "no SIGHUP) under the guard's own bounds; the runtime's bootstrap timeout or the guard limits "
                  "end the runtime. Dependent steps were stopped; reconnect to f3 and inspect the guard run "
                  "directory before anything else.")
USABLE_FAILED = ("The runtime reached READY, but a mount root failed the usable gate (see the journal's usable "
                 "result for each root and the errno). The runtime, both mounts and any stuck lstat child were "
                 "left as they are: go to Phase 11 with inspection first; never remount or retry.")
USABLE_CLAIMS_FAILED = ("The runtime reached READY and the mount roots answered, but a provider claim did not reach "
                        "TERMINAL in time (see the journal's usable result: `claims` per mount with the open claims' "
                        "number, opcode, status and stage). A stranded PROCESS_EXIT or GETATTR blocks its session "
                        "once the guest's 30 s QUERY window ends, as in zq3-r8. The runtime and both mounts were left "
                        "as they are: go to Phase 11 with inspection first; never remount or retry. Any later "
                        "OBSERVE needs a sequence above summary.json's observe_sequence_last.")
STREAM_LIMIT = 1 << 20         # per stream of one SSH/local child
SAFE_TOKEN = re.compile(r"[A-Za-z0-9_./=:,+-]+\Z")
SAFE_PATH = re.compile(r"/[A-Za-z0-9_./-]+\Z")
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")
HASH = re.compile(r"[0-9a-f]{64}\Z")
RUN_ID = re.compile(r"[a-z0-9][a-z0-9-]{0,23}\Z")
# The loader is the only program text on the remote command line: no `$`,
# backslash or backquote, so sh, bash, csh and fish all pass it unchanged.
LOADER = ("import sys,hashlib;s=sys.stdin.buffer.read({size});"
          "hashlib.sha256(s).hexdigest()=='{sha}' or sys.exit(97);"
          "exec(compile(s,'bootstrap_remote','exec'))")
# (step, kind, per-call cap in seconds). Kinds: read (retry-safe, never
# uncertain), launch, local (workstation subprocess), remote (mutating SSH).
STEPS = (("host-preflight", "read", 30), ("launch", "launch", 0), ("proposal", "read", 10),
         ("guest-ready", "read", 8), ("admission", "remote", 80), ("enroll-request", "local", 15),
         ("signed-enrollment", "read", 0), ("provision", "remote", 15),
         ("enrollment-handoff", "remote", 15), ("awaiting-bind", "read", 10),
         ("bind-request", "local", 15), ("signed-bind", "read", 0), ("bind", "remote", 15),
         ("mount-0", "remote", 35), ("mount-1", "remote", 35), ("ready", "read", 10),
         ("usable", "read", USABLE_PROBE_SECONDS))
STEP_NAMES = tuple(name for name, _, _ in STEPS)


class Refused(RuntimeError):
    """Refused before a step could run (plan, output, journal or step order)."""


class StepFailed(RuntimeError):
    """The step ended with a known state; dependent steps do not run.

    `detail` adds journal fields beside `error`, such as the usable gate's
    errno and per-root probe results.
    """

    def __init__(self, message, detail=None):
        super().__init__(message)
        self.detail = detail or {}


class StepUncertain(RuntimeError):
    """The step's effect may have happened unobserved: inspect, never replay."""


class Retry(RuntimeError):
    """A read-only attempt that may be repeated until the deadline."""


# --------------------------------------------------------------------------
# Plan


def require(ok, message):
    if not ok:
        raise Refused(message)


def closed(value, names, what):
    require(isinstance(value, dict) and set(value) == set(names), what + ": closed keys " + ",".join(sorted(names)))


def safe_path(value, what):
    require(isinstance(value, str) and SAFE_PATH.match(value) is not None and
            os.path.normpath(value) == value, what + ": clean absolute safe ASCII path")


def safe_hash(value, what):
    require(isinstance(value, str) and HASH.match(value) is not None, what + ": SHA256 hex")


PLAN_SHAPE = {
    "ssh": ["path", "sha256"],
    "host": ["address", "alias", "user", "known_hosts", "known_hosts_sha256", "doas", "python"],
    "guest": ["address", "alias", "user", "known_hosts", "known_hosts_sha256", "python"],
    "guard": ["script", "script_sha256", "run_dir", "vm_name", "disk_roots", "skip_paths"],
    "runtime": ["executable", "sha256", "config", "owner_directory", "enrollment_file"],
    "kernel": ["script", "script_sha256", "plan", "plan_sha256", "state", "release"],
    "prep": ["path", "sha256", "template", "template_sha256", "config", "config_sha256"],
    "observer": ["ssh", "ssh_sha256", "known_hosts", "known_hosts_sha256", "identity", "host"],
}


def validate_plan(plan):
    """Closed schema; every remote token is a safe path, hash or fixed name."""
    closed(plan, ["version", "run_id", "bootstrap_seconds", "signer", "mounts", "remote_sha256"] +
           list(PLAN_SHAPE), "plan")
    safe_hash(plan["remote_sha256"], "remote_sha256")
    require(plan["version"] == VERSION and isinstance(plan["run_id"], str) and
            RUN_ID.match(plan["run_id"]) is not None, "plan version/run ID")
    seconds = plan["bootstrap_seconds"]
    # At least 60 s: Rocky's boot consumes part of the window and admission
    # needs a 40 s floor (udevadm settle), so a shorter plan could only fail
    # after launch had already used the one-shot test-kernel boot.
    require(type(seconds) is int and 60 <= seconds <= BOOTSTRAP_LIMIT, "bootstrap_seconds 60..120 (launch to READY)")
    safe_path(plan["signer"], "signer")
    for section, names in PLAN_SHAPE.items():
        closed(plan[section], names, section)
        for name in names:
            check_field(section, name, plan[section][name])
    validate_targets(plan)
    validate_mounts(plan)
    require(re.fullmatch(r"hfs-[a-z0-9][a-z0-9-]{0,22}", plan["guard"]["vm_name"]) is not None, "guard vm_name")
    require(re.fullmatch(r"[0-9][0-9A-Za-z._+-]{0,63}", plan["kernel"]["release"]) is not None, "kernel release")


def check_field(section, name, value):
    what = section + "." + name
    if name.endswith("sha256"):
        safe_hash(value, what)
    elif name in ("disk_roots", "skip_paths"):
        require(isinstance(value, list) and (name == "skip_paths" or value), what + ": list")
        for item in value:
            safe_path(item, what)
    elif name in ("address", "alias", "user", "host", "vm_name", "release"):
        require(isinstance(value, str) and SAFE_TOKEN.match(value) is not None, what + ": safe token")
    else:
        safe_path(value, what)


def validate_targets(plan):
    """Exactly the G8 primaries and aliases the controller pins (no aliases)."""
    for name in ("host", "guest"):
        target = plan[name]
        require(target["address"] not in controller.RETIRED,
                name + ": retired pre-migration address; plans name the primary only")
        require((target["address"], target["alias"], target["user"]) == controller.TARGETS[name],
                name + ": target must be exactly " + "/".join(controller.TARGETS[name]))
    require(plan["observer"]["host"] == plan["guest"]["address"], "observer host must be the guest primary")


def validate_mounts(plan):
    """hfs0 on m0 and hfs1 on m1 in the run directory observer_key.py stages."""
    mounts = plan["mounts"]
    require(isinstance(mounts, list) and len(mounts) == 2, "exactly two mounts")
    for index, mount in enumerate(mounts):
        closed(mount, ["tag", "mountpoint"], "mount")
        expected = "/var/tmp/hfs-zq3-{}/m{}".format(plan["run_id"], index)
        require(mount == {"tag": "hfs{}".format(index), "mountpoint": expected},
                "mount {} must be hfs{} on {}".format(index, index, expected))


def load_plan(path, expected_sha256):
    """Plan bytes pinned by SHA256, then one verified read of local inputs."""
    raw = read(path, 64 << 10)
    require(digest(raw) == expected_sha256, "plan bytes differ from the pinned SHA256")
    plan = parse(raw)
    validate_plan(plan)
    inputs = {"ssh": controller.verified_bytes(plan["ssh"]["path"], plan["ssh"]["sha256"], 16 << 20),
              "template": private_input(plan["prep"]["template"], plan["prep"]["template_sha256"])}
    for name in ("host", "guest"):
        pins = controller.verified_bytes(plan[name]["known_hosts"], plan[name]["known_hosts_sha256"], 65536)
        controller.check_pin(pins, plan[name])
        inputs[name + "-known_hosts"] = pins
    inputs["template-tags"] = template_tags(inputs["template"], plan)
    # Local misconfiguration must refuse here, before the launch consumes
    # Rocky's one-shot boot; each fill step re-checks the prep pin anyway.
    controller.verified_bytes(plan["prep"]["path"], plan["prep"]["sha256"], 256 << 20)
    check_runtime_config(private_input(plan["prep"]["config"], plan["prep"]["config_sha256"]), plan)
    inputs["remote"] = controller.verified_bytes(str(REMOTE), plan["remote_sha256"], 256 << 10)
    return raw, plan, inputs


def private_input(path, expected):
    """A pinned input that hfs-runtime-prep will read: private, ours, one link."""
    raw = controller.verified_bytes(path, expected, 512 << 10)
    info = os.lstat(path)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and not info.st_mode & 0o077 and
            info.st_uid == os.geteuid(), path + ": must be a private single-link file of this user")
    return raw


def check_runtime_config(raw, plan):
    """The bundle's runtime-config.json must match the plan's bootstrap bound
    and runtime paths. It also holds the run's host receipt private keys: only
    these three fields are read, nothing of it is kept or logged."""
    config = parse(raw)
    require(config.get("BootstrapSeconds") == plan["bootstrap_seconds"],
            "plan bootstrap_seconds differs from the runtime config's BootstrapSeconds")
    require((config.get("OwnerDirectory"), config.get("EnrollmentFile")) ==
            (plan["runtime"]["owner_directory"], plan["runtime"]["enrollment_file"]),
            "plan OwnerDirectory/EnrollmentFile differ from the runtime config")


def template_tags(raw, plan):
    """The generated template's device tags, which must match the plan mounts."""
    template = parse(raw)
    tags = [mount.get("device_tag") for mount in template.get("Mounts", [])]
    require(tags == [mount["tag"] for mount in plan["mounts"]], "template device tags differ from plan mounts")
    return tags


# --------------------------------------------------------------------------
# Boundaries (all injectable)


class Reply:
    """One bounded child: exit code (None if it did not end), streams, flags."""

    def __init__(self, returncode, stdout=b"", stderr=b"", incomplete=False, reason=""):
        self.returncode, self.stdout, self.stderr = returncode, stdout, stderr
        self.incomplete, self.reason = incomplete, reason


def run_bounded(argv, payload, seconds, env):
    """Run a LOCAL child with stdin payload, capped streams and one deadline.

    On a deadline or overflow only this local child (SSH or hfs-runtime-prep)
    is stopped; a remote effect is never inferred from that.
    """
    child = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env=env, start_new_session=True)
    streams = {"stdout": bytearray(), "stderr": bytearray()}
    try:
        reason = pump(child, payload, streams, time.monotonic() + seconds)
        if not reason:
            return Reply(child.wait(timeout=5), bytes(streams["stdout"]), bytes(streams["stderr"]))
    except subprocess.TimeoutExpired:
        reason = "child did not exit after closing its streams"
    finally:
        stop_local(child)
    return Reply(None, bytes(streams["stdout"]), bytes(streams["stderr"]), True, reason)


def pump(child, payload, streams, deadline):
    """Nonblocking stdin feed and stream drain; returns a failure reason or ''."""
    offset = 0
    with selectors.DefaultSelector() as selector:
        for name, stream in (("stdin", child.stdin), ("stdout", child.stdout), ("stderr", child.stderr)):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_WRITE if name == "stdin" else selectors.EVENT_READ, name)
        if not payload:
            selector.unregister(child.stdin)
            child.stdin.close()
        while selector.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return "deadline"
            for key, _ in selector.select(min(remaining, 0.25)):
                if key.data == "stdin":
                    try:
                        offset += os.write(key.fd, payload[offset:offset + 65536])
                    except BlockingIOError:
                        continue
                    except BrokenPipeError:
                        offset = len(payload)  # The child stopped reading; its exit tells.
                    if offset >= len(payload):
                        selector.unregister(child.stdin)
                        child.stdin.close()
                    continue
                chunk = os.read(key.fd, 65536)
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                streams[key.data] += chunk
                if len(streams[key.data]) > STREAM_LIMIT:
                    return "stream overflow"
    return ""


def stop_local(child):
    """TERM then KILL the local child's own session; close its pipes."""
    if child.poll() is None:
        for number in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.killpg(child.pid, number)
                child.wait(timeout=2)
                break
            except ProcessLookupError:
                break
            except subprocess.TimeoutExpired:
                continue
    for stream in (child.stdin, child.stdout, child.stderr):
        if not stream.closed:
            stream.close()


def local_env(ssh=False):
    env = {"PATH": "/usr/bin:/bin", "LC_ALL": "C", "HOME": os.environ.get("HOME", "/")}
    if ssh and "SSH_AUTH_SOCK" in os.environ:
        env["SSH_AUTH_SOCK"] = os.environ["SSH_AUTH_SOCK"]
    return env


class SshRemote:
    """Pinned SSH to f3 (paul + doas -n) and to Rocky (root, jumped via f3)."""

    def __init__(self, plan, inputs_dir, program):
        self.plan, self.inputs, self.program = plan, inputs_dir, program

    def base(self, name, keepalive=False):
        argv = controller.ssh_base(self.plan[name], self.inputs, name)
        if keepalive:
            # The guard transport lives for the whole run window: tolerate a
            # short network stall. A lost session does NOT stop the guard: -T
            # gives it no tty, so sshd sends no SIGHUP and the guard keeps
            # supervising the runtime under its own bounds (time, RSS, disk,
            # VM context). Only this orchestrator loses its view (see
            # TRANSPORT_LOST).
            argv = [a for a in argv if not a.startswith("-oServerAlive")]
            argv += ["-oServerAliveInterval=15", "-oServerAliveCountMax=8"]
        return argv

    def destination(self, name):
        return self.plan[name]["user"] + "@" + self.plan[name]["address"]

    def argv(self, target, command):
        if target == "host":
            return self.base("host") + ["--", self.destination("host"), command]
        jump = self.base("host") + ["-W", self.plan["guest"]["address"] + ":22", "--", self.destination("host")]
        return self.base("guest") + ["-oProxyCommand=" + shlex.join(jump), "--", self.destination("guest"), command]

    def remote_command(self, target, action, args):
        prefix = ([self.plan["host"]["doas"], "-n", self.plan["host"]["python"]] if target == "host"
                  else [self.plan["guest"]["python"]])
        loader = LOADER.format(size=len(self.program), sha=digest(self.program))
        return " ".join(safe_tokens(prefix + ["-I", "-B", "-c"]) + ['"' + loader + '"'] +
                        safe_tokens([action] + list(args)))

    def call(self, target, action, args, payload, seconds):
        command = self.remote_command(target, action, args)
        return run_bounded(self.argv(target, command), self.program + payload, seconds, local_env(ssh=True))

    def guard_argv(self):
        """runtime-guard.py in the foreground of one long-lived f3 session."""
        g, r, h = self.plan["guard"], self.plan["runtime"], self.plan["host"]
        command = [h["doas"], "-n", h["python"], "-I", "-B", g["script"], "--run-dir", g["run_dir"],
                   "--runtime-sha256", r["sha256"], "--vm-name", g["vm_name"]]
        for root in g["disk_roots"]:
            command += ["--disk-root", root]
        for path in g["skip_paths"]:
            command += ["--skip-path", path]
        # The runtime refuses to start without --disposable-validation, and
        # runtime-guard.py accepts exactly this argv shape (zq3-r2 Phase 8).
        command += ["--", r["executable"], "--config", r["config"], "--disposable-validation"]
        return self.base("host", keepalive=True) + ["--", self.destination("host"), " ".join(safe_tokens(command))]


def safe_tokens(tokens):
    for token in tokens:
        if not isinstance(token, str) or SAFE_TOKEN.match(token) is None:
            raise Refused("unsafe remote command token: " + repr(token))
    return list(tokens)


class GuardLauncher:
    """Starts the guard transport detached from this terminal and process.

    Its own session (a Ctrl-C here never reaches it), stdin from /dev/null and
    streams to files, so it keeps running if the orchestrator exits. It is
    never signalled by this tool. If this local SSH process ends while the
    guard still runs on f3 (a lost connection), the guard and the runtime
    continue under the guard's own bounds; see TRANSPORT_LOST.
    """

    def start(self, argv, stdout_path, stderr_path):
        with open(stdout_path, "xb") as out, open(stderr_path, "xb") as err:
            return subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=out, stderr=err,
                                    env=local_env(ssh=True), start_new_session=True)


class LocalRunner:
    def run(self, argv, seconds):
        return run_bounded(argv, b"", seconds, local_env())


class Clock:
    def now(self):
        return time.monotonic()

    def sleep(self, seconds):
        time.sleep(seconds)


def announce(record):
    print(json.dumps(record, sort_keys=True), flush=True)


class Boundaries:
    """Every effect the orchestrator has outside its own output directory."""

    def __init__(self, remote, launcher=None, local=None, clock=None, notify=None):
        self.remote = remote
        self.launcher = launcher or GuardLauncher()
        self.local = local or LocalRunner()
        self.clock = clock or Clock()
        self.notify = notify or announce


# --------------------------------------------------------------------------
# Journal


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def append_durable(path, record):
    line = canonical(record)
    fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
    try:
        if os.write(fd, line) != len(line):
            raise OSError("short journal write")
        os.fsync(fd)
    finally:
        os.close(fd)


class Journal:
    """Durable intent/result records in the fixed STEPS order.

    begin() refuses a step out of order, after any non-ok result and while any
    intent is open: an orchestrator killed (or Ctrl-C'd) mid-step leaves an
    intent without a result, and that step stays uncertain. There is no
    resume, so no step is ever replayed.
    """

    def __init__(self, path, records=()):
        self.path, self.records = path, list(records)

    @classmethod
    def create(cls, path, header):
        journal = cls(path)
        journal.append(dict(header, kind="run"))
        controller.fsync_directory(path.parent)
        return journal

    @classmethod
    def open(cls, path):
        lines = read(path, 4 << 20).split(b"\n")
        require(lines[-1] == b"", "torn journal tail: inspect journal.jsonl")
        return cls(path, [parse(line) for line in lines[:-1]])

    def append(self, record):
        record = dict(record, version=JOURNAL_VERSION, seq=len(self.records), utc=utc_now())
        append_durable(self.path, record)
        self.records.append(record)

    def steps(self):
        """step -> open | ok | failed | uncertain, in record order."""
        state = {}
        for record in self.records:
            if record["kind"] == "intent":
                state[record["step"]] = "open"
            elif record["kind"] == "result":
                state[record["step"]] = record["outcome"]
        return state

    def next_step(self):
        for step, outcome in self.steps().items():
            if outcome == "open":
                raise Refused("step {} has an intent without a result: uncertain, never replayed".format(step))
            if outcome != "ok":
                raise Refused("run stopped at step {} ({}); no further step".format(step, outcome))
        done = len(self.steps())
        require(done < len(STEP_NAMES), "bootstrap already complete")
        return STEP_NAMES[done]

    def begin(self, step):
        expected = self.next_step()
        require(step == expected, "step order violation: {} before {}".format(step, expected))
        self.append({"kind": "intent", "step": step})

    def finish(self, step, outcome, detail):
        require(self.steps().get(step) == "open", "result without an open intent: " + step)
        self.append({"kind": "result", "step": step, "outcome": outcome, "detail": detail})


# --------------------------------------------------------------------------
# Record checks (pure functions; StepFailed on any mismatch)


def strict(raw, what):
    try:
        return parse(raw)
    except (ValueError, controller.Refused) as failure:
        raise StepFailed("{} is not strict JSON: {}".format(what, failure)) from None


def is_uuid(value):
    return isinstance(value, str) and UUID.match(value) is not None


def is_hash(value):
    return isinstance(value, str) and HASH.match(value) is not None


def uuid_text(value):
    """fsapi.UUID marshals as 16 numbers; return its canonical text (or None)."""
    if not (isinstance(value, list) and len(value) == 16 and
            all(type(item) is int and 0 <= item <= 255 for item in value)):
        return None
    text = bytes(value).hex()
    return "-".join((text[:8], text[8:12], text[12:16], text[16:20], text[20:]))


def last_json(raw):
    lines = [line for line in raw.splitlines() if line.strip()]
    try:
        value = parse(lines[-1]) if lines else None
    except (ValueError, controller.Refused):
        return None
    return value if isinstance(value, dict) else None


def tail(raw, size=2000):
    return raw[-size:].decode("utf-8", "replace").strip()


def classify(reply, action, mutating):
    """Map one remote reply to its result, a known failure, or the unknown.

    0 + ok JSON: done. 97: the loader refused the helper bytes, nothing ran.
    2 + JSON without `uncertain`: a known state. Anything else (timeout, lost
    transport, overflow, malformed output, exit 1): uncertain for a mutating
    call, a retryable miss for a read-only one.
    """
    result = last_json(reply.stdout)
    if reply.incomplete:
        outcome = "transport " + reply.reason
    elif reply.returncode == 0 and result and result.get("ok") is True and result.get("action") == action:
        return result
    elif reply.returncode == 97:
        raise StepFailed(action + ": remote loader rejected the helper bytes; nothing ran")
    elif reply.returncode == 2 and result and result.get("ok") is False and not result.get("uncertain"):
        raise StepFailed("{}: {} (changed: {})".format(action, result.get("error"), result.get("changed")))
    else:
        outcome = "exit {} {}".format(reply.returncode, (result or {}).get("error") or tail(reply.stderr))
    if mutating:
        raise StepUncertain("{}: {}; remote state unknown, inspect before anything else".format(action, outcome))
    raise Retry("{}: {}".format(action, outcome))


def stage_records(text):
    """Complete runtime stdout lines; they must be a prefix of STAGES."""
    records = []
    for index, line in enumerate((text or "").split("\n")[:-1]):  # last: partial or ''
        record = strict(line.encode(), "runtime stdout line {}".format(index))
        stage = record.get("stage") if isinstance(record, dict) else None
        expected = STAGES[index] if index < len(STAGES) else "nothing after READY"
        if stage != expected:
            raise StepFailed("stage order violation: runtime record {} is {!r}, expected {}".format(index, stage, expected))
        if "publication" not in record or record["publication"] is not None:
            raise StepFailed("runtime record {} claims a publication".format(stage))
        records.append((record, line))
    return records


def guard_records(result):
    """A guard receipt or breach means the runtime ended or is being stopped."""
    files = result.get("files") or {}
    if files.get("receipt.json"):
        receipt = strict(files["receipt.json"].encode(), "guard receipt")
        raise StepFailed("runtime guard finished: refused={!r} breach={!r} runtime_exit={!r}".format(
            receipt.get("refused"), receipt.get("breach"), receipt.get("runtime_exit")))
    if files.get("breach.json"):
        raise StepFailed("runtime guard breach: " + files["breach.json"].strip()[:2000])


def observer_manifest(observer):
    """Go marshals [32]byte as 32 numbers; return them as lowercase hex."""
    value = observer.get("ManifestHash") if isinstance(observer, dict) else None
    if not (isinstance(value, list) and len(value) == 32 and
            all(type(item) is int and 0 <= item <= 255 for item in value)):
        return None
    return bytes(value).hex()


def expected_enrollment(facts):
    """The runtime's exact proposal with only the empty GuestBoot filled."""
    proposal = copy.deepcopy(facts["proposal"])
    for domain in proposal["Domains"]:
        if not domain["Scope"].get("guest_boot"):
            domain["Scope"]["guest_boot"] = facts["boot_id"]
    return proposal


def check_enroll_request(request, facts):
    names = [mount.get("device_name") for mount in request.get("Mounts", [])]
    if (request.get("Proposal") != facts["proposal_path"] or request.get("BootID") != facts["boot_id"] or
            request.get("ProviderNonce") != facts["provider_nonce"] or names != facts["device_names"] or
            observer_manifest(request.get("Observer")) != facts["manifest_sha256"]):
        raise StepFailed("enroll request does not carry the measured proposal/boot/nonce/devices/manifest")


def check_signed_enrollment(files, facts):
    enrollment = strict(files["enrollment.json"], "signed enrollment")
    provision = strict(files["provision.json"], "signed provision")
    if not isinstance(enrollment, dict) or set(enrollment) != {"Authorization", "Providers", "Provision", "Observer"}:
        raise StepFailed("signed enrollment is not a RuntimeEnrollment")
    try:
        problems = enrollment_problems(enrollment, provision, files["provision.json"], facts)
    except (KeyError, TypeError, AttributeError, ValueError) as failure:
        problems = ["malformed signer output: " + repr(failure)]
    if problems:
        raise StepFailed("signer output mismatch: " + "; ".join(problems))


def enrollment_problems(enrollment, provision, provision_raw, facts):
    problems = []
    if base64.b64decode(enrollment["Provision"], validate=True) != provision_raw:
        problems.append("enrollment.json carries a different provision than provision.json")
    if enrollment["Observer"] != facts["enroll_request"]["Observer"]:
        problems.append("signed Observer differs from the enrollment request")
    if observer_manifest(enrollment["Observer"]) != facts["manifest_sha256"]:
        problems.append("signed Observer.ManifestHash is not the measured manifest SHA256")
    payload = provision["payload"]
    if (payload.get("version") != "homelabfs-guest-provision/1" or payload.get("boot_id") != facts["boot_id"] or
            payload.get("provider_nonce") != facts["provider_nonce"]):
        problems.append("provision is not for the measured boot ID and provider nonce")
    problems += provision_mount_problems(payload, facts)
    if enrollment["Authorization"]["Enrollment"] != expected_enrollment(facts):
        problems.append("signed enrollment is not the runtime's exact proposal for the measured boot")
    if not isinstance(enrollment["Providers"], list) or len(enrollment["Providers"]) != 2:
        problems.append("signed enrollment must carry two provider authorizations")
    return problems


def provision_mount_problems(payload, facts):
    """The single provision group's local mounts must name, in domain order,
    the request's sessions and tags and the MEASURED virtio device names: the
    guest provider binds each session to that device."""
    groups = payload.get("groups")
    if not isinstance(groups, list) or len(groups) != 1:
        return ["provision must hold exactly one group"]
    found = [(m.get("domain_index"), m.get("session"), m.get("device_tag"), m.get("device_name"))
             for m in groups[0]["local_mounts"]]
    expected = [(i, m["session"], m["device_tag"], facts["device_names"][i])
                for i, m in enumerate(facts["enroll_request"]["Mounts"])]
    if found != expected:
        return ["provision local mounts {!r} are not the measured devices/request sessions {!r}".format(found, expected)]
    return []


def check_signed_bind(raw, facts):
    bind = strict(raw, "signed bind")
    payload = bind.get("payload") if isinstance(bind, dict) else None
    awaiting = facts["awaiting"]
    expected = {"version": "homelabfs-guest-bind/1", "boot_id": facts["boot_id"],
                "provider_nonce": facts["provider_nonce"], "provision_hash": awaiting["provision_hash"],
                "controller": awaiting["controller"], "group_id": facts["bind_request"]["GroupID"]}
    if not isinstance(payload, dict) or payload != expected:
        raise StepFailed("signer output mismatch: bind payload {!r} is not {!r}".format(payload, expected))


def root_problem(root):
    """One guest-stat root result as (text, errno or None); None when ok."""
    outcome = root.get("outcome")
    if outcome == "ok":
        return None
    if outcome == "errno":
        number = root.get("errno")
        name = errno.errorcode.get(number, "?") if type(number) is int else "?"
        return "lstat failed with errno {} ({}, {})".format(number, name, root.get("error")), number
    reasons = {"timeout": "lstat did not return within {} s (left running, never killed)".format(root.get("seconds")),
               "not-mounted": "no longer exactly one 9p mount of its tag (root not stat'ed)",
               "not-directory": "root is not a directory", "error": "lstat child failed: {}".format(root.get("stderr"))}
    return reasons.get(outcome, "unknown probe outcome {!r}".format(outcome)), None


def check_usable(result, mounts, probe):
    """Both plan mount roots, in plan order, must have answered the lstat.

    Raises StepFailed whose journal detail keeps every root's result and the
    first failing root's errno (`errno`, null for a timeout or lost mount).
    """
    roots = result.get("roots")
    pairs = [(r.get("tag"), r.get("mountpoint")) for r in roots if isinstance(r, dict)] \
        if isinstance(roots, list) else None
    if pairs != [(m["tag"], m["mountpoint"]) for m in mounts]:
        raise StepFailed("{} probe: guest-stat did not report exactly the plan's mount roots".format(probe),
                         {"probe": probe, "roots": roots, "errno": None})
    problems = [(root, root_problem(root)) for root in roots]
    problems = [(root, found) for root, found in problems if found]
    if problems:
        text = "; ".join("{} {}: {}".format(root["tag"], root["mountpoint"], found[0]) for root, found in problems)
        raise StepFailed("{} probe: mount root unusable: {}".format(probe, text),
                         {"probe": probe, "roots": roots, "errno": problems[0][1][1]})


def check_control(control, owner_directory):
    """READY's control binding as OBSERVE needs it: a UUID runtime and a
    socket directly inside the plan's private owner directory."""
    socket, runtime = control.get("socket"), control.get("runtime")
    if (not isinstance(socket, str) or SAFE_PATH.match(socket) is None or os.path.normpath(socket) != socket or
            os.path.dirname(socket) != owner_directory or not is_uuid(runtime)):
        raise StepFailed("READY control binding is not a socket in {} with a runtime UUID: {!r}".format(
            owner_directory, control))
    return socket, runtime


def observed_evidence(result, sequence):
    """The evidence of one OBSERVED reply to `sequence`: complete, SERVING,
    no publication and the b84 evidence version. Anything else fails the
    gate: an incomplete or fenced runtime is not usable."""
    reply = result.get("reply")
    evidence = reply.get("evidence") if isinstance(reply, dict) else None
    if not isinstance(reply, dict) or reply.get("status") != "OBSERVED" or reply.get("sequence") != sequence or \
            not isinstance(evidence, dict):
        raise StepFailed("OBSERVE {} did not answer OBSERVED with its sequence: {!r}".format(
            sequence, tail(json.dumps(reply).encode(), 2000)))
    if evidence.get("version") != EVIDENCE_VERSION:
        raise StepFailed("OBSERVE evidence version {!r} is not {} (a runtime without the b84 claim summary)".format(
            evidence.get("version"), EVIDENCE_VERSION))
    if evidence.get("complete") is not True or evidence.get("stage") != "SERVING" or \
            evidence.get("publication", "missing") is not None:
        raise StepFailed("OBSERVE {} is not complete, SERVING and unpublished (stage {!r}, complete {!r})".format(
            sequence, evidence.get("stage"), evidence.get("complete")))
    return evidence


def provider_claims(evidence, sessions):
    """tag -> that provider ledger's Claims, for exactly the enrolled sessions."""
    rows = evidence.get("providers")
    found = {row.get("session"): row for row in rows if isinstance(row, dict)} if isinstance(rows, list) else {}
    if set(found) != set(sessions) or len(rows) != len(sessions):
        raise StepFailed("OBSERVE providers {} are not the enrolled sessions {}".format(
            sorted(found), sorted(sessions)))
    claims = {}
    for session, tag in sessions.items():
        row = found[session]
        summary = (row.get("ledger") or {}).get("Claims") if not row.get("error") else None
        if not isinstance(summary, dict) or not all(type(summary.get(k)) is int for k in CLAIM_COUNTS) or \
                not all(isinstance(summary.get(k), list) for k in ("Pending", "Recent")):
            raise StepFailed("OBSERVE provider {} ({}) has no claim summary: {!r}".format(
                tag, session, row.get("error") or row.get("ledger")))
        claims[tag] = summary
    return claims


CLAIM_COUNTS = ("Principals", "Allocated", "Retired", "Entries", "Open")


def claim_text(claim):
    return "#{} {} {}/{}".format(claim.get("Number"), claim.get("Opcode") or "(no CALL yet)", claim.get("Status"),
                                 claim.get("Stage") or "-")


def claims_problems(claims, base):
    """Why the claims are not settled yet; [] when they are.

    Without `base` (the baseline at the settle point): no open claim. With
    it (after the final probe), per mount also: CLAIMS_PER_PROBE numbers
    allocated since the baseline, and a TERMINAL GETATTR and PROCESS_EXIT
    among them, unless retirement already covered those numbers (a retired
    number was TERMINAL first; its entry may be recycled).
    """
    problems = []
    for tag, now in sorted(claims.items()):
        if now["Principals"] != 1:
            problems.append("{}: {} claim principals, the gate compares numbers of exactly one".format(
                tag, now["Principals"]))
            continue
        if now["Open"]:
            pending = ", ".join(claim_text(c) for c in now["Pending"])
            problems.append("{}: {} claim(s) not TERMINAL: {}".format(tag, now["Open"], pending))
        if base is not None:
            problems.extend(probe_claim_problems(tag, now, base[tag]))
    return problems


def probe_claim_problems(tag, now, base):
    """The final probe's own claims on one mount, numbered above the baseline."""
    first, needed = base["Allocated"] + 1, base["Allocated"] + CLAIMS_PER_PROBE
    if now["Allocated"] < needed:
        return ["{}: the final probe's GETATTR and PROCESS_EXIT are not both allocated yet ({} of {} numbers "
                "after #{})".format(tag, now["Allocated"] - base["Allocated"], CLAIMS_PER_PROBE, base["Allocated"])]
    if now["Retired"] >= now["Allocated"]:
        return []  # every number, the probe's included, was retired after it was TERMINAL
    newer = [c for c in now["Recent"] if isinstance(c, dict) and type(c.get("Number")) is int and
             c["Number"] >= first]
    missing = [op for op in ("GETATTR", "PROCESS_EXIT")
               if not any(c.get("Opcode") == op and c.get("Status") == "TERMINAL" for c in newer)]
    if missing:
        return ["{}: no TERMINAL {} among the final probe's claims ({})".format(
            tag, " or ".join(missing), ", ".join(claim_text(c) for c in newer) or "none listed")]
    return []


def remote_seconds(seconds):
    """Remote child deadline, ending before the SSH deadline so it can report."""
    return str(max(1, int(seconds) - 5))


def observe_signed(directory, bounds):
    """name -> bytes once the signer's fresh directory holds exactly `bounds`.

    None while the directory or a file is still missing. Anything else in it,
    or a file that is not a private single-link regular file of ours, refuses.
    """
    try:
        names = set(os.listdir(directory))
    except FileNotFoundError:
        return None
    if names - set(bounds):
        raise StepFailed("signer output mismatch: unexpected files " + ",".join(sorted(names - set(bounds))))
    if names != set(bounds):
        return None
    files = {}
    for name, bound in bounds.items():
        info = os.lstat(directory / name)
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_mode & 0o077 or
                info.st_uid != os.geteuid()):
            raise StepFailed("signer output mismatch: {} is not a private regular file of ours".format(name))
        files[name] = read(directory / name, bound)
    return files


# --------------------------------------------------------------------------
# The bootstrap sequence


class Bootstrap:
    """One run: the STEPS in order, each journaled, stopping at the first miss."""

    def __init__(self, plan, inputs, output, journal, io):
        self.plan, self.inputs, self.output, self.journal, self.io = plan, inputs, output, journal, io
        self.work, self.calls = output / "work", output / "calls"
        self.facts, self.stage_lines = {}, []
        self.deadline = self.launched = self.guard = self.mounted_at = None
        self.step_name, self.cap, self.calls_made, self.last_retry = "", 0, 0, ""
        # What the current deadline is, for its refusal text: the launch-to-
        # READY bootstrap deadline, then the usable gate's own bound.
        self.deadline_text = ""
        # Lifecycle sequences this run consumed (b84): one per OBSERVE
        # attempt, sent or not answered. Phase 9 must start above it.
        self.observe_sequence = 0
        self.usable_phase = self.last_claims = None  # where `usable` stopped, for its note

    def run(self):
        """(exit status, step that stopped the run): 0 READY with both mount
        roots usable, 1 failed (including READY with an unusable root),
        3 uncertain."""
        for name, kind, cap in STEPS:
            outcome = self.step(name, kind, cap)
            if outcome != "ok":
                return {"failed": 1, "uncertain": 3}[outcome], name
        return 0, None

    def step(self, name, kind, cap):
        self.journal.begin(name)
        self.step_name, self.cap = name, cap
        try:
            detail, outcome = getattr(self, "step_" + name.replace("-", "_"))(), "ok"
        except StepFailed as failure:
            detail, outcome = dict(failure.detail, error=str(failure)), "failed"
        except StepUncertain as failure:
            detail, outcome = {"error": str(failure)}, "uncertain"
        except Exception as failure:  # noqa: BLE001 - a bug or local I/O error
            # It may have struck after a remote/local effect: claim no known state.
            detail = {"error": repr(failure)}
            outcome = "uncertain" if kind in ("remote", "launch", "local") else "failed"
        self.journal.finish(name, outcome, detail)
        self.io.notify({"step": name, "outcome": outcome, "detail": detail})
        return outcome

    # -- time, calls, evidence

    def seconds(self, cap):
        """Seconds the next call may take: its cap within the bootstrap deadline."""
        if self.deadline is None:
            return cap
        remaining = self.deadline - self.io.clock.now()
        if remaining <= 0:
            raise StepFailed("{} passed during {}{}".format(
                self.deadline_text, self.step_name, "; last: " + self.last_retry if self.last_retry else ""))
        return min(cap, remaining) if cap else remaining

    def remote(self, target, action, args, payload=b"", mutating=False, seconds=None):
        seconds = self.seconds(self.cap) if seconds is None else seconds
        floor = max(MIN_DISPATCH, MIN_REMOTE_SECONDS.get(self.step_name, 0.0))
        if mutating and seconds < floor:
            raise StepFailed("not dispatched: {:.1f} s left for {} (needs {:.0f} s) before the bootstrap "
                             "deadline".format(seconds, self.step_name, floor))
        reply = self.io.remote.call(target, action, args, payload, seconds)
        self.evidence(action, {"target": target, "args": args, "seconds": seconds,
                               "payload_sha256": digest(payload) if payload else None}, reply)
        return classify(reply, action, mutating)

    def read_once(self, target, action, args):
        try:
            return self.remote(target, action, args)
        except Retry as failure:
            raise StepFailed(str(failure)) from None

    def evidence(self, action, request, reply):
        self.calls_made += 1
        name = "{:04d}-{}-{}.json".format(self.calls_made, self.step_name, action)
        controller.persist(self.calls / name, dict(request, returncode=reply.returncode, incomplete=reply.incomplete,
                                                   reason=reply.reason, stdout=tail(reply.stdout, STREAM_LIMIT),
                                                   stderr=tail(reply.stderr, STREAM_LIMIT)))

    def poll(self, attempt, interval):
        """Repeat a read-only attempt until it yields a value or the deadline."""
        self.last_retry = ""
        while True:
            self.seconds(interval)
            try:
                value = attempt()
            except Retry as retry:
                value, self.last_retry = None, str(retry)
            if value is not None:
                return value
            self.io.clock.sleep(interval)

    def write_work(self, name, raw):
        path = self.work / name
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        controller.fsync_directory(self.work)
        return path

    # -- runtime and guard observation

    def guard_alive(self):
        code = self.guard.poll() if self.guard is not None else None
        if code is not None:
            try:
                with open(self.output / "guard-transport.stderr", "rb") as stream:
                    detail = tail(stream.read()[-4096:])
            except OSError:
                detail = ""
            raise StepFailed("runtime guard transport exited with {} during {}: {}. {}".format(
                code, self.step_name, detail, TRANSPORT_LOST))

    def runtime_records(self, expected):
        """The runtime's records once exactly `expected` exist; None before."""
        result = self.remote("host", "host-poll", ["--guard-dir", self.plan["guard"]["run_dir"]])
        guard_records(result)
        self.guard_alive()
        records = stage_records(result.get("stdout"))
        if len(records) > expected:
            raise StepFailed("stage order violation: runtime emitted {} before {} completed".format(
                records[expected][0]["stage"], self.step_name))
        self.stage_lines = [line for _, line in records]
        return [record for record, _ in records] if len(records) == expected else None

    def wait_signed(self, directory, bounds):
        """The operator's signed files, once two polls see identical bytes."""
        seen = {"files": None}

        def attempt():
            self.guard_alive()
            files = observe_signed(directory, bounds)
            stable, seen["files"] = files is not None and files == seen["files"], files
            if not stable:
                raise Retry("waiting for the operator's signer output in " + str(directory))
            return files
        return self.poll(attempt, SIGNER_POLL_SECONDS)

    def operator_action(self, verb, request, directory):
        command = [self.plan["signer"], verb, "--request", str(request), "--output", str(directory)]
        self.io.notify({"operator_action": "sign-" + verb, "command": shlex.join(command),
                        "request_sha256": digest(read(request)), "seconds_left": round(self.seconds(0), 1),
                        "note": "run this with the operator key (A7); this tool never reads it"})

    def prep(self, args, output):
        """One pinned hfs-runtime-prep fill step; returns the request bytes."""
        p = self.plan["prep"]
        try:
            controller.verified_bytes(p["path"], p["sha256"], 256 << 20)
        except controller.Refused as failure:
            raise StepFailed("hfs-runtime-prep pin: " + str(failure)) from None
        reply = self.io.local.run([p["path"]] + args, self.seconds(self.cap))
        self.evidence(args[0], {"local": True, "args": args}, reply)
        if reply.incomplete:
            raise StepUncertain("hfs-runtime-prep {}: {}; {} may be partial".format(args[0], reply.reason, output))
        if reply.returncode != 0:
            raise StepFailed("hfs-runtime-prep {} refused: {}".format(args[0], tail(reply.stderr)))
        report = strict(reply.stdout, "hfs-runtime-prep report")
        raw = read(output, 512 << 10)
        if report.get("output") != str(output) or report.get("sha256") != digest(raw):
            raise StepFailed("request file differs from the hfs-runtime-prep report")
        return raw

    def sysfs_write(self, target, raw):
        result = self.remote("guest", "guest-write", ["--target", target, "--sha256", digest(raw), "--size",
                                                      str(len(raw)), "--boot-id", self.facts["boot_id"]],
                             payload=raw, mutating=True)
        if result.get("bytes") != len(raw) or result.get("sha256") != digest(raw):
            raise StepUncertain("guest {} write report differs from the bytes sent".format(target))
        return {"target": target, "sha256": digest(raw), "bytes": len(raw)}

    # -- the steps, in STEPS order

    def step_host_preflight(self):
        g, r = self.plan["guard"], self.plan["runtime"]
        return self.read_once("host", "host-preflight", [
            "--guard-script", g["script"], "--guard-sha256", g["script_sha256"], "--runtime", r["executable"],
            "--runtime-sha256", r["sha256"], "--guard-dir", g["run_dir"], "--owner-dir", r["owner_directory"],
            "--enrollment-file", r["enrollment_file"]])

    def step_launch(self):
        try:
            argv = self.io.remote.guard_argv()
        except Refused as failure:
            raise StepFailed("guard command refused before launch: " + str(failure)) from None
        self.launched = self.io.clock.now()
        self.deadline = self.launched + self.plan["bootstrap_seconds"]
        self.deadline_text = "bootstrap deadline ({} s from launch)".format(self.plan["bootstrap_seconds"])
        try:
            self.guard = self.io.launcher.start(argv, self.output / "guard-transport.stdout",
                                                self.output / "guard-transport.stderr")
        except OSError as failure:
            raise StepFailed("guard transport did not start: " + str(failure)) from None
        return {"pid": self.guard.pid, "command": argv[-1], "bootstrap_seconds": self.plan["bootstrap_seconds"]}

    def step_proposal(self):
        records = self.poll(lambda: self.runtime_records(1), POLL_SECONDS)
        expected = self.plan["runtime"]["owner_directory"] + "/proposal.json"
        if records[0].get("proposal") != expected:
            raise StepFailed("proposal record names {!r}, not {}".format(records[0].get("proposal"), expected))
        result = self.read_once("host", "host-proposal", ["--path", expected])
        raw = base64.b64decode(result.get("base64", ""), validate=True)
        proposal = strict(raw, "proposal")
        if digest(raw) != result.get("sha256") or len(proposal.get("Domains") or []) != 2:
            raise StepFailed("proposal transfer differs or does not hold two domains")
        path = self.write_work("proposal.json", raw)
        self.facts.update(proposal=proposal, proposal_path=str(path))
        return {"record": self.stage_lines[0], "proposal_sha256": digest(raw), "copy": str(path)}

    def step_guest_ready(self):
        def attempt():
            self.guard_alive()
            return self.remote("guest", "guest-ping", [])
        result = self.poll(attempt, POLL_SECONDS)
        if result.get("release") != self.plan["kernel"]["release"]:
            raise StepFailed("guest runs {!r}, not the one-shot test kernel {}".format(
                result.get("release"), self.plan["kernel"]["release"]))
        if not is_uuid(result.get("boot_id")):
            raise StepFailed("guest boot ID is not a UUID")
        self.facts["boot_id"] = result["boot_id"]
        return {"release": result["release"], "boot_id": result["boot_id"]}

    def step_admission(self):
        k, seconds = self.plan["kernel"], self.seconds(self.cap)
        result = self.remote("guest", "guest-admission", [
            "--release", k["release"], "--boot-id", self.facts["boot_id"], "--python", self.plan["guest"]["python"],
            "--script", k["script"], "--script-sha256", k["script_sha256"], "--plan", k["plan"],
            "--plan-sha256", k["plan_sha256"], "--state", k["state"], "--tags", ",".join(self.inputs["template-tags"]),
            "--seconds", remote_seconds(seconds)], mutating=True, seconds=seconds)
        return self.admitted(result)

    def admitted(self, result):
        """Cross-check the step 0 round trip. The manifest exists either way."""
        admission, devices = result.get("admission") or {}, result.get("devices") or {}
        manifest, boot = result.get("manifest_sha256"), self.facts["boot_id"]
        names = [devices.get(tag) for tag in self.inputs["template-tags"]]
        problems = []
        if not is_hash(manifest) or admission.get("manifest_sha256") != manifest:
            problems.append("manifest SHA256 readback differs from rocky_kernel.py admission")
        if result.get("boot_id") != boot or admission.get("boot_id") != boot:
            problems.append("guest boot ID changed")
        if not is_uuid(result.get("provider_nonce")):
            problems.append("provider nonce is not a UUID")
        if not all(isinstance(n, str) and re.fullmatch(r"virtio(0|[1-9][0-9]{0,3})", n) for n in names) or \
                len(set(names)) != len(names):
            problems.append("9p device names are not distinct virtioN names")
        if problems:
            raise StepFailed("; ".join(problems) + " (the manifest is written: U2 removes it)")
        self.facts.update(manifest_sha256=manifest, provider_nonce=result["provider_nonce"], device_names=names)
        return {"manifest_sha256": manifest, "boot_id": boot, "provider_nonce": result["provider_nonce"],
                "devices": devices, "modules": admission.get("modules")}

    def step_enroll_request(self):
        o, f = self.plan["observer"], self.facts
        observer = {"Version": OBSERVER_VERSION, "SSH": o["ssh"], "KnownHosts": o["known_hosts"],
                    "Identity": o["identity"], "Host": o["host"], "SSHSHA256": o["ssh_sha256"],
                    "KnownHostsSHA256": o["known_hosts_sha256"], "ManifestSHA256": f["manifest_sha256"]}
        observer_path = self.write_work("observer.json", canonical(observer))
        output = self.work / "enroll-request.json"
        raw = self.prep(["enroll-request", "--template", self.plan["prep"]["template"], "--proposal",
                         f["proposal_path"], "--boot-id", f["boot_id"], "--provider-nonce", f["provider_nonce"],
                         "--device-names", ",".join(f["device_names"]), "--observer", str(observer_path),
                         "--output", str(output)], output)
        f["enroll_request"] = strict(raw, "enroll request")
        check_enroll_request(f["enroll_request"], f)
        return {"request": str(output), "sha256": digest(raw), "observer_sha256": digest(canonical(observer))}

    def step_signed_enrollment(self):
        directory = self.work / "signer-enrollment"
        self.operator_action("enroll", self.work / "enroll-request.json", directory)
        files = self.wait_signed(directory, {"enrollment.json": 512 << 10, "provision.json": 128 << 10})
        check_signed_enrollment(files, self.facts)
        self.facts.update(enrollment=files["enrollment.json"], provision=files["provision.json"],
                          provision_path=str(directory / "provision.json"))
        return {name: digest(raw) for name, raw in files.items()}

    def step_provision(self):
        return self.sysfs_write("provision", self.facts["provision"])

    def step_enrollment_handoff(self):
        raw, path = self.facts["enrollment"], self.plan["runtime"]["enrollment_file"]
        result = self.remote("host", "host-install", ["--path", path, "--sha256", digest(raw), "--size", str(len(raw))],
                             payload=raw, mutating=True)
        if result.get("path") != path or result.get("sha256") != digest(raw):
            raise StepUncertain("enrollment handoff report differs from the bytes sent")
        return {"path": path, "sha256": digest(raw)}

    def step_awaiting_bind(self):
        record = self.poll(lambda: self.runtime_records(2), POLL_SECONDS)[1]
        if not is_uuid(record.get("controller")) or not is_hash(record.get("provision_hash")):
            raise StepFailed("AWAITING_GUEST_BIND lacks a controller UUID or provision hash")
        path = self.write_work("awaiting-guest-bind.json", self.stage_lines[1].encode())
        self.facts.update(awaiting=record, awaiting_path=str(path))
        return {"record": self.stage_lines[1]}

    def step_bind_request(self):
        f, output = self.facts, self.work / "bind-request.json"
        raw = self.prep(["bind-request", "--template", self.plan["prep"]["template"], "--provision",
                         f["provision_path"], "--awaiting", f["awaiting_path"], "--output", str(output)], output)
        f["bind_request"] = strict(raw, "bind request")
        if (f["bind_request"].get("Controller") != f["awaiting"]["controller"] or
                f["bind_request"].get("Provision") != f["provision_path"] or
                f["bind_request"].get("GroupID") != uuid_text(f["proposal"].get("ID"))):
            raise StepFailed("bind request differs from the runtime controller, the provision or the proposal group")
        return {"request": str(output), "sha256": digest(raw)}

    def step_signed_bind(self):
        directory = self.work / "signer-binding"
        self.operator_action("bind", self.work / "bind-request.json", directory)
        files = self.wait_signed(directory, {"bind.json": 64 << 10})
        check_signed_bind(files["bind.json"], self.facts)
        self.facts["bind"] = files["bind.json"]
        return {"bind.json": digest(files["bind.json"])}

    def step_bind(self):
        return self.sysfs_write("bind", self.facts["bind"])

    def mount(self, index):
        mount, seconds = self.plan["mounts"][index], self.seconds(self.cap)
        result = self.remote("guest", "guest-mount", ["--tag", mount["tag"], "--mountpoint", mount["mountpoint"],
                                                      "--boot-id", self.facts["boot_id"],
                                                      "--seconds", remote_seconds(seconds)],
                             mutating=True, seconds=seconds)
        if result.get("tag") != mount["tag"] or result.get("mountpoint") != mount["mountpoint"]:
            raise StepUncertain("guest mount report differs from the requested mount")
        # mount(8) has exited by now, so its PROCESS_EXIT claim exists; the
        # usable gate counts the guest's QUERY window from the later mount.
        self.mounted_at = self.io.clock.now()
        return {"tag": mount["tag"], "mountpoint": mount["mountpoint"], "mountinfo": result.get("mountinfo")}

    def step_mount_0(self):
        return self.mount(0)

    def step_mount_1(self):
        return self.mount(1)

    def step_ready(self):
        ready = self.poll(lambda: self.runtime_records(3), POLL_SECONDS)[2]
        control = ready.get("control")
        if (ready.get("profile") != "CONTROLLED_LOCAL" or ready.get("domains") != 2 or
                not isinstance(control, dict) or set(control) != {"socket", "runtime", "cut", "epoch"}):
            raise StepFailed("READY record is not CONTROLLED_LOCAL with two domains and a control binding")
        self.facts["control"] = control
        return {"record": self.stage_lines[2], "seconds_from_launch": round(self.io.clock.now() - self.launched, 3)}

    def step_usable(self):
        """Post-READY usability gate (k64, b84): never retried, never mounts,
        writes into or unmounts anything; a failure preserves everything.

        Read-only for the mounts' contents, but NOT for the runtime: each
        guest lstat is an hfs operation. hfs_capture registers the lstat
        child as an hfs process and sends a GETATTR claim, and the child's
        exit queues its own PROCESS_EXIT claim (patch 0003). The two probes
        therefore add about four durable claims to each provider ledger
        before T1. Each OBSERVE refreshes the Admissions through the guest
        observer and consumes a lifecycle sequence.

        READY ends the runtime's launch-to-READY bootstrap timeout, so this
        step replaces the driver's deadline with its own fixed bound through
        the same `seconds()` machinery: the settle point (mount-1's end plus
        USABLE_SETTLE_SECONDS, or now if READY came later) plus
        USABLE_PROBE_SECONDS and USABLE_CLAIM_SECONDS. Phases, in order:
        `first` probe right away (a root already refused fails fast);
        `settle` until the guest's QUERY window has closed for the mounts'
        claims; `baseline` OBSERVE with no open claim; `final` probe; `claims`
        OBSERVE polls until the final probe's GETATTR and PROCESS_EXIT are
        TERMINAL on every mount (zq3-r8: an answered lstat whose
        PROCESS_EXIT stayed RESERVED blocked the session 30 s later).
        Every failure's journal detail carries `probe` (the phase), `roots`
        and `errno` (null where no lstat reported one), `claims` (the last
        per-mount claim summary, null before any OBSERVE answered) and
        `observe_sequence_last`.
        """
        settle_at = self.mounted_at + USABLE_SETTLE_SECONDS
        self.deadline = max(settle_at, self.io.clock.now()) + USABLE_PROBE_SECONDS + USABLE_CLAIM_SECONDS
        self.deadline_text = "usable gate deadline ({:.0f} s after mount-1)".format(self.deadline - self.mounted_at)
        try:
            return self.usable_phases(settle_at)
        except StepFailed as failure:
            # Transport, deadline, guard, runtime and OBSERVE failures carry no
            # probe result of their own: name where the gate stopped.
            if "probe" not in failure.detail:
                failure.detail = dict(failure.detail, probe=self.usable_phase, roots=None, errno=None)
            failure.detail = dict(failure.detail, claims=self.last_claims,
                                  observe_sequence_last=self.observe_sequence)
            raise

    def usable_phases(self, settle_at):
        """The gate's phases in order; `usable_phase` names the current one."""
        self.usable_phase = "first"
        first = self.stat_roots("first")
        self.usable_phase = "settle"
        self.settle(settle_at)
        self.runtime_still_ready()
        self.usable_phase = "baseline"
        base = self.wait_claims(None)
        self.usable_phase = "final"
        final = self.stat_roots("final")
        final_at = self.io.clock.now()
        self.usable_phase = "claims"
        claims = self.wait_claims(base)
        self.usable_phase = None
        return {"first": first, "final": final, "settle_seconds": USABLE_SETTLE_SECONDS,
                "final_seconds_after_mount": round(final_at - self.mounted_at, 3), "baseline_claims": base,
                "claims": claims, "claims_seconds_after_final": round(self.io.clock.now() - final_at, 3),
                "observe_sequence_last": self.observe_sequence}

    def usable_note(self):
        """summary.json's note for a failed `usable`: claims or roots."""
        return USABLE_CLAIMS_FAILED if self.usable_phase in ("baseline", "claims") else USABLE_FAILED

    def wait_claims(self, base):
        """OBSERVE until claims_problems() is empty; the per-mount claims.

        Polled every CLAIM_POLL_SECONDS within the gate's deadline. A
        transport miss retries with a fresh sequence; an OBSERVE that is not
        complete and SERVING, or lacks the claim summary, fails at once. At
        the deadline the failure names the open claims of the last OBSERVE.
        """
        sessions = {m["session"]: m["device_tag"] for m in self.facts["enroll_request"]["Mounts"]}
        problems = []

        def attempt():
            claims = provider_claims(self.observe(), sessions)
            self.last_claims = claims
            problems[:] = claims_problems(claims, base)
            if problems:
                raise Retry("; ".join(problems))
            return claims
        try:
            return self.poll(attempt, CLAIM_POLL_SECONDS)
        except StepFailed:
            if not problems:
                raise
            raise StepFailed("{} OBSERVE: provider claims not TERMINAL by the {}: {}".format(
                self.usable_phase, self.deadline_text, "; ".join(problems))) from None

    def observe(self):
        """One OBSERVE through the plan-pinned runtime client on f3."""
        socket, runtime = check_control(self.facts["control"], self.plan["runtime"]["owner_directory"])
        r, seconds = self.plan["runtime"], self.seconds(self.cap)
        self.observe_sequence += 1  # consumed even if the reply is lost
        result = self.remote("host", "host-observe", [
            "--executable", r["executable"], "--executable-sha256", r["sha256"], "--socket", socket,
            "--runtime-id", runtime, "--sequence", str(self.observe_sequence),
            "--seconds", remote_seconds(seconds)], seconds=seconds)
        return observed_evidence(result, self.observe_sequence)

    def stat_roots(self, probe):
        """One guest-stat round trip over both plan mounts; their roots."""
        mounts = self.plan["mounts"]
        result = self.read_once("guest", "guest-stat", [
            "--tags", ",".join(m["tag"] for m in mounts), "--mountpoints", ",".join(m["mountpoint"] for m in mounts),
            "--boot-id", self.facts["boot_id"], "--python", self.plan["guest"]["python"],
            "--seconds", str(STAT_SECONDS)])
        check_usable(result, mounts, probe)
        return result["roots"]

    def settle(self, until):
        """Wait for the settle point, watching only the local guard transport."""
        while self.io.clock.now() < until:
            self.seconds(0)
            self.guard_alive()
            self.io.clock.sleep(min(POLL_SECONDS, until - self.io.clock.now()))

    def runtime_still_ready(self):
        """The guard reports no breach or exit and READY is still the last record."""
        try:
            records = self.runtime_records(len(STAGES))
        except Retry as failure:
            raise StepFailed(str(failure)) from None
        if records is None:
            raise StepFailed("runtime stdout no longer holds its READY record")

    def final_evidence(self):
        """One read-only poll after the run for the record; never a step."""
        if self.guard is None:
            return
        self.deadline, self.step_name, self.cap = None, "final", 10
        try:
            self.remote("host", "host-poll", ["--guard-dir", self.plan["guard"]["run_dir"]])
        except (Retry, StepFailed, OSError):
            pass


# --------------------------------------------------------------------------
# Commands


def prepare_output(output, plan_raw, inputs, program):
    """Fresh private run directory: no reuse, so no replay of any step."""
    output = Path(output)
    controller.check_output_path(output)
    controller.check_output_parent(output)
    try:
        output.mkdir(mode=0o700)
    except FileExistsError:
        raise Refused("output exists: a run directory is never reused (no replay)") from None
    controller.fsync_directory(output.parent)
    controller.persist_bytes(output / "plan.json", plan_raw)
    controller.stage_inputs(output, {"ssh": inputs["ssh"], "host-known_hosts": inputs["host-known_hosts"],
                                     "guest-known_hosts": inputs["guest-known_hosts"],
                                     "bootstrap_remote.py": program})
    for name in ("work", "calls"):
        controller.mkdir_durable(output / name)
    return output


def real_boundaries(plan, inputs_dir, program):
    return Boundaries(SshRemote(plan, inputs_dir, program))


def run(plan_path, expected_sha256, output, boundaries=real_boundaries):
    raw, plan, inputs = load_plan(plan_path, expected_sha256)
    program = inputs["remote"]  # the plan-pinned bootstrap_remote.py bytes
    output = prepare_output(output, raw, inputs, program)
    io = boundaries(plan, output / "inputs", program)
    journal = Journal.create(output / "journal.jsonl", {"plan_sha256": digest(raw), "run_id": plan["run_id"],
                                                        "remote_sha256": digest(program)})
    bootstrap = Bootstrap(plan, inputs, output, journal, io)
    code, stopped = bootstrap.run()
    bootstrap.final_evidence()
    guard = bootstrap.guard
    summary = {"outcome": {0: "ready", 1: "failed", 3: "uncertain"}[code], "stopped_at": stopped,
               "guard_transport_pid": guard.pid if guard else None,
               "guard_transport_running": bool(guard) and guard.poll() is None,
               "acceptance": False, "note": "READY is not gate acceptance; Phases 9-11 follow"}
    if guard and guard.poll() is not None:
        summary["guard_transport_note"] = TRANSPORT_LOST
    if stopped == "usable":
        summary["usable_note"] = bootstrap.usable_note()
    # Lifecycle sequences are strictly increasing per runtime: the next
    # OBSERVE (Phase 9, or Phase 11 inspection) must use a larger one.
    summary["observe_sequence_last"] = bootstrap.observe_sequence
    controller.persist(output / "summary.json", summary)
    io.notify(summary)
    return code


def status(output):
    """Read-only: journal step states, open (uncertain) intents, guard pid."""
    journal = Journal.open(Path(output) / "journal.jsonl")
    states = journal.steps()
    launch = next((r["detail"] for r in journal.records if r["kind"] == "result" and r["step"] == "launch"
                   and r["outcome"] == "ok"), None)
    alive = None
    if launch:
        try:
            os.kill(launch["pid"], 0)  # Existence probe only; never a signal.
            alive = True
        except ProcessLookupError:
            alive = False
        except PermissionError:
            alive = None  # The PID now belongs to someone else: unknown.
    return {"steps": states, "uncertain": [s for s, o in states.items() if o in ("open", "uncertain")],
            "guard_transport_pid": launch and launch["pid"], "guard_transport_running": alive}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="action", required=True)
    check = sub.add_parser("check", help="validate the plan and pinned local inputs; no contact")
    run_parser = sub.add_parser("run", help="Phase 8 bootstrap (needs A3/A4/A7 approval)")
    for command in (check, run_parser):
        command.add_argument("--plan", required=True)
        command.add_argument("--plan-sha256", required=True)
    run_parser.add_argument("--output", required=True)
    sub.add_parser("status").add_argument("--output", required=True)
    args = parser.parse_args(argv)
    try:
        if args.action == "check":
            raw, plan, _ = load_plan(args.plan, args.plan_sha256)
            announce({"plan_sha256": digest(raw), "run_id": plan["run_id"], "execution_authorized": False})
            return 0
        if args.action == "status":
            announce(status(args.output))
            return 0
        return run(args.plan, args.plan_sha256, args.output)
    except (Refused, controller.Refused, OSError, ValueError) as failure:
        print("refused: " + str(failure), file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
