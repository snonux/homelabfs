#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Fill the exact f3 fdescfs window plan from reviewed pins and fresh facts.

Workstation-only (task v34, zq3 gap G12). No SSH, host, VM or mount action.
The controller (fdescfs-window.py) requires its plan's `before` to equal the
actual `facts` output and `expires` to be at most one hour ahead, so the exact
plan JSON can only exist after root has collected, read-only:

  1. `facts --inspector STAGED_INSPECTOR` stdout (zq3 Phase 4 step 0),
  2. `date -u +%s` on f3 (passed as --host-now; the controller checks the
     expiry against f3's clock), and
  3. `sha256 -r` lines ("HASH PATH") for every path in the pins' `collect`
     list, every staged artifact and the base library closure, plus optional
     extra library closure lines (e.g. from `ldd /usr/local/bin/python3.12`)
     under /lib, /usr/lib, /usr/local/lib or /libexec.

This tool refuses unless those facts match every reviewed pin: the f3 host
identity and 15.1-RELEASE-p2 kernel/base, fdescfs absent from the module
inventory, /dev/fd still devfs and unsupported, no /dev/fd mount, the rebuilt
artifact hashes, the stock module hash and the repository controller bytes.
The pins' `window_module` (task j44: fusefs.ko, loaded by zq3 Phase 4 step 4
after setup) is copied into the plan so restore tolerates exactly that file;
it must be pinned with the same hash and still be absent at fill time.
It writes the canonical plan.json and a fill-record.json and prints the plan
SHA256. Neither file grants authority; the filled plan is reviewed, and the
SYSTEM host setup still needs Paul's explicit approval (zq3 class A5).
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import time

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("fdescfs_window", HERE / "fdescfs-window.py")
WINDOW = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WINDOW)

PINS_VERSION = "homelabfs-fdescfs-window-pins/1"
FILL_VERSION = "homelabfs-fdescfs-window-fill/1"
QUALIFICATION_FLAGS = ["-test.run=^TestDescriptorExecNativeQualification$",
                       "-test.v", "-test.timeout=5s"]
FACT_KEYS = {"hostname", "base", "kernel", "boot", "module", "descriptor",
             "modules", "mounts", "fd_directory"}
CLOSURE_PREFIXES = ("/lib/", "/usr/lib/", "/usr/local/lib/", "/libexec/")
SHA = re.compile(r"[0-9a-f]{64}")


class FillRefused(RuntimeError):
    """A fact or input does not match the reviewed pins."""


def require(ok, message):
    if not ok:
        raise FillRefused(message)


def read_json(path):
    """Duplicate-key-refusing, size-bounded JSON (the controller's loader)."""
    try:
        return WINDOW.load(path)
    except RuntimeError as failure:
        raise FillRefused(str(failure)) from failure


def sha256_file(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def parse_host_sha256(text):
    """FreeBSD `sha256 -r` lines: exactly one 'HASH /absolute/path' per path."""
    result = {}
    for line in text.splitlines():
        if not line.strip():
            continue
        words = line.split()
        require(len(words) == 2 and SHA.fullmatch(words[0]) is not None and
                words[1].startswith("/"), "malformed host sha256 line: " + line)
        require(words[1] not in result, "duplicate host sha256 path: " + words[1])
        result[words[1]] = words[0]
    return result


def check_pins(pins):
    require(set(pins) == {"version", "run_id", "directory", "inspector", "namespace_helper",
                          "qualification_test", "controller", "module_policy", "pinned",
                          "collect", "expected", "window_module"}, "closed pins schema")
    require(pins["version"] == PINS_VERSION, "pins version")
    require(pins["directory"] == str(WINDOW.PARENT / pins["run_id"]), "pins run directory")
    require(pins["module_policy"] == "preserve-existing-or-retain-new", "pins module policy")
    staged = [pins["inspector"], pins["namespace_helper"], pins["qualification_test"]]
    require(all(path in pins["pinned"] for path in staged), "staged artifacts must be pinned")
    require(pins["controller"] not in pins["pinned"], "controller hash comes from the repository")
    require(all(SHA.fullmatch(value) for value in pins["pinned"].values()), "pinned hashes")
    required = set(WINDOW.COMMANDS.values()) | {"/boot/kernel/kernel", "/usr/local/bin/python3.12"}
    require(set(pins["collect"]) == required, "collect list must be the controller's fixed inputs")
    check_window_module(pins)


def check_window_module(pins):
    """The declared window module is a pinned stock file, absent at fill time.

    Pinning it in `pinned` makes artifact_pins() require the f3 hash to match
    and puts it into the plan's `sha256`, which the controller re-digests on
    every action. Requiring it in `modules_forbidden` keeps the plan honest:
    restore tolerates it only because the run itself loads it after setup.
    """
    declared = pins["window_module"]
    require(isinstance(declared, dict) and set(declared) == {"path", "sha256"},
            "closed window module pins")
    path = declared["path"]
    require(isinstance(path, str) and WINDOW.WINDOW_MODULE_PATH.fullmatch(path) is not None and
            path != "/boot/kernel/fdescfs.ko", "window module must be a stock /boot/kernel module")
    require(pins["pinned"].get(path) == declared["sha256"], "window module must be pinned with its hash")
    require(Path(path).name in pins["expected"]["modules_forbidden"],
            "window module must be expected absent at fill time")


def check_identity(facts, expected):
    require(set(facts) == FACT_KEYS, "facts are not the controller's facts output")
    require(facts["hostname"] == expected["hostname"], "host identity changed")
    require(facts["base"] == expected["base"], "kernel/userland version changed")
    require(facts["kernel"].startswith(expected["kernel_prefix"]), "running kernel changed")
    directory = facts["fd_directory"]
    require(set(directory) == {"mode", "uid", "gid", "dev", "ino"} and directory["uid"] == 0 and
            directory["mode"] & 0o170000 == 0o040000 and not directory["mode"] & 0o022,
            "protected /dev/fd directory")


def check_descriptor_state(facts, expected):
    """The reviewed plan loads the absent module and mounts over devfs."""
    require(expected["fdescfs_module"] == "absent", "pins expect fdescfs absent")
    require(facts["module"].get("exit") not in (None, 0), "fdescfs module present; plan is for absent")
    report = facts["descriptor"]
    require(report.get("version") == "homelabfs-descriptor-exec/1" and
            report.get("platform") == "freebsd/amd64", "descriptor report identity")
    require(report.get("supported") is False and
            report.get("mount_type") == expected["descriptor_mount_type"],
            "/dev/fd is not the expected unsupported devfs")
    for row in facts["mounts"].splitlines():
        fields = row.split()
        require(len(fields) < 3 or fields[2] not in ("fdescfs", "fusefs"), "fdescfs/fuse mount present")
        require(len(fields) < 2 or not (fields[1] == "/dev/fd" or fields[1].startswith("/dev/fd/")),
                "a /dev/fd mount exists")
    try:
        groups = WINDOW.module_files(facts["modules"])
    except RuntimeError as failure:
        raise FillRefused(str(failure)) from failure
    require(groups["kernel"][0][4] == "/boot/kernel/kernel", "kernel pathname")
    require(set(expected["modules_required"]) <= set(groups), "expected loaded modules missing")
    require(not set(expected["modules_forbidden"]) & set(groups), "unexpected module loaded")


def artifact_pins(pins, host, controller_sha256):
    """Merge reviewed pins, collected values and optional library closure."""
    result = {}
    for path, wanted in pins["pinned"].items():
        require(host.get(path) == wanted, "pinned artifact differs or missing: " + path)
        result[path] = wanted
    for path in pins["collect"]:
        require(path in host, "collected fact missing: " + path)
        result[path] = host[path]
    require(host.get(pins["controller"]) == controller_sha256,
            "staged controller differs from the repository controller")
    result[pins["controller"]] = controller_sha256
    for path, value in host.items():
        if path not in result:
            require(path.startswith(CLOSURE_PREFIXES), "unexpected host sha256 path: " + path)
            result[path] = value
    return dict(sorted(result.items()))


def check_expiry(expires, now):
    require(isinstance(expires, int) and now < expires <= now + 3600,
            "expiry must be in the future and at most one hour ahead")


def check_host_clock(expires, host_now, workstation_now):
    """The controller checks expiry against f3's clock, not the workstation's.

    `host_now` is f3's `date -u +%s`, collected with the facts. Expiry must be
    valid on both clocks, and a skew above five minutes refuses: it would make
    the reviewed one-hour bound mean something different on f3.
    """
    require(abs(host_now - workstation_now) <= 300, "f3/workstation clock skew above 300 s")
    check_expiry(expires, host_now)


def build_plan(pins, facts, host, controller_sha256, expires, now):
    """Return the exact plan the controller's validate() accepts."""
    check_pins(pins)
    check_identity(facts, pins["expected"])
    check_descriptor_state(facts, pins["expected"])
    check_expiry(expires, now)
    plan = {"version": WINDOW.VERSION, "run_id": pins["run_id"], "expires": expires,
            "directory": pins["directory"], "inspector": pins["inspector"],
            "namespace_helper": pins["namespace_helper"],
            "module_policy": pins["module_policy"],
            "window_module": dict(pins["window_module"]),
            "sha256": artifact_pins(pins, host, controller_sha256), "before": facts,
            "qualification": [[pins["qualification_test"], *QUALIFICATION_FLAGS]]}
    require(re.fullmatch(r"[a-z0-9][a-z0-9-]{1,63}", plan["run_id"]) is not None, "run id")
    return plan


def plan_bytes(plan):
    return (json.dumps(plan, sort_keys=True, indent=2) + "\n").encode()


def write_outputs(out, plan, record):
    """Write both files exclusively; an existing output directory refuses."""
    out.mkdir(mode=0o700)
    for name, raw in (("plan.json", plan_bytes(plan)),
                      ("fill-record.json", plan_bytes(record))):
        with open(out / name, "xb") as stream:
            stream.write(raw)


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--pins", type=Path, required=True)
    parser.add_argument("--facts", type=Path, required=True)
    parser.add_argument("--host-sha256", type=Path, required=True)
    parser.add_argument("--expires", type=int, required=True)
    parser.add_argument("--host-now", type=int, required=True,
                        help="f3 `date -u +%%s` collected with the facts")
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--controller", type=Path, default=HERE / "fdescfs-window.py")
    return parser.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    pins, facts = read_json(args.pins), read_json(args.facts)
    host_text = args.host_sha256.read_text(encoding="utf-8")
    controller = sha256_file(args.controller)
    now = int(time.time())
    check_host_clock(args.expires, args.host_now, now)
    plan = build_plan(pins, facts, parse_host_sha256(host_text), controller, args.expires, now)
    digest = hashlib.sha256(plan_bytes(plan)).hexdigest()
    record = {"version": FILL_VERSION, "plan_sha256": digest,
              "pins_sha256": sha256_file(args.pins), "facts_sha256": sha256_file(args.facts),
              "host_sha256_sha256": hashlib.sha256(host_text.encode()).hexdigest(),
              "controller_sha256": controller, "expires": args.expires,
              "host_now": args.host_now, "workstation_now": now,
              "authority": "none: review input only; host setup needs explicit approval"}
    write_outputs(args.out, plan, record)
    print(digest)


if __name__ == "__main__":
    main()
