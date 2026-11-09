#!/usr/bin/env python3
"""Pinned direct bhyve argv for the zq3 controlled Rocky launch (gap G4).

Pure local logic: no SSH, no VM, no host command. `generate` reconstructs the
controlled exporter argv from read-only launch facts (collected by
`host.py facts`) and pins it in a canonical record; `verify` refuses any
runtime config whose Exporter argv/path/digest drifted from that record.

The reconstruction is anchored to reviewed baseline constants (rocky.conf,
vm-bhyve's vm-run, the firmware symlink text and target digest, and the
hardware tokens vm-bhyve logged for the current Rocky launch). Any difference refuses: a changed launch profile needs a
reviewed code/plan revision, never a silent re-derivation.
"""
import argparse
import hashlib
import json
import os
import re
import sys

FACTS_VERSION = "homelabfs-zq3-launch-facts/1"
RECORD_VERSION = "homelabfs-zq3-bhyve-argv/1"

# Reviewed read-only facts (2026-11-05 and 2026-11-08 batches, see README).
ROCKY_CONF = "/zroot/bhyve/rocky/rocky.conf"
ROCKY_CONF_SHA256 = "c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581"
VM_RUN = "/usr/local/lib/vm-bhyve/vm-run"
VM_RUN_SHA256 = "43753bc83263c26a25f49e7ccd3c1da79dca7a6361442b3c61a1831bd2782db0"
VM_BHYVE_LOG = "/zroot/bhyve/rocky/vm-bhyve.log"
BOOTROM = "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"
# BOOTROM is a symlink installed by pkg edk2-bhyve (g202508_3, seen 2026-11-08)
# and bhyve follows it. The argv keeps the link path vm-bhyve logged; the
# link text and resolved target are pinned so a relinked firmware refuses.
BOOTROM_LINK = "../edk2-bhyve/BHYVE_UEFI.fd"
BOOTROM_TARGET = "/usr/local/share/edk2-bhyve/BHYVE_UEFI.fd"
# Digest of the 3,653,632 target bytes (2026-11-05 batch, sha256(1) follows
# the link), so the pin carries over to the target unchanged.
BOOTROM_SHA256 = "6b48e9673915110037a54835dca217bcb262c1de3606a482f0ff3890687499e2"
VARS = "/zroot/bhyve/rocky/uefi-vars.fd"
DISK = "/zroot/bhyve/rocky/disk0.img"
BRIDGE = "vm-public"
UUID = "e10b6225-44d0-11f1-882a-e8ff1ed7f3d7"
MAC = "58:9c:fc:4c:38:84"
POC_DEVICE = "0:7:0,virtio-9p,homelabfs_poc=/var/tmp/homelabfs-9p-poc/share"
FBUF_DEVICE = "0:6:0,fbuf,tcp=127.0.0.1:5900,vga=io"
# Exactly what vm-run logged for the normal 12 GiB Rocky launch. `TAP` stands
# for vm-bhyve's dynamically allocated tap; every other token is pinned.
BASELINE = {
    "options": ["-c", "4", "-m", "12G", "-AHPw",
                "-l", "bootrom,%s,%s" % (BOOTROM, VARS),
                "-s", POC_DEVICE, "-U", UUID, "-u"],
    "devices": ["-s", "0,hostbridge", "-s", "31,lpc",
                "-s", "0:4:0,nvme," + DISK,
                "-s", "0:5:0,virtio-net,TAP,mac=" + MAC,
                "-s", FBUF_DEVICE],
    "console": ["-l", "com1,/dev/nmdm-rocky.1A"],
}
# Candidate exporter (xz3 w54 kernel rebuild, manifest 89c928d5...; the bhyve
# bytes are unchanged since the 3f770a88... package because every later
# candidate (f44 b203799e..., r54 5eb42c50..., then w54) reused the native build).
EXPORTER_SHA256 = "36423b6d4f68dc1b05299b1c379a164035e7db5b06ca64b47f2a8571d823fb5c"
STAGING_PARENT = "/var/tmp/hfs-validation-xz3"
GUEST_MEMORY = "6G"
SHARES = ("hfs0", "hfs1")
NINEP_SLOTS = ("0:7:0", "0:8:0")

RUN_ID = re.compile(r"zq3-[a-z0-9][a-z0-9-]{0,19}")
# hfs9p_supervisor_prepare accepts only canonical lowercase UUID text.
SESSION = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
# High units stay clear of vm-bhyve's lowest-free `ifconfig tap create`.
TAP = re.compile(r"tap[1-9][0-9]{2}")
HEX64 = re.compile(r"[0-9a-f]{64}")
LOG_LINE = re.compile(r"(\S+): (.*)")
RECORD_KEYS = {"version", "run_id", "vm", "facts_sha256", "launch_logged",
               "sessions", "host", "exporter", "changes", "args", "args_sha256"}


class Refused(RuntimeError):
    """A precondition failed; nothing was or may be derived from the input."""


def require(ok, message):
    if not ok:
        raise Refused(message)


def unique(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "duplicate JSON key " + key)
        value[key] = item
    return value


def load_json(path, limit=1 << 20):
    with open(path, "rb") as stream:
        raw = stream.read(limit + 1)
    require(len(raw) <= limit, "oversized JSON input")
    return raw, json.loads(raw, object_pairs_hook=unique)


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def sha256(raw):
    return hashlib.sha256(raw).hexdigest()


def run_names(run_id):
    """Every host name derives from the run ID so a retry never reuses one.

    The fixed `zq3-` prefix keeps the vmm name `hfs-zq3-*` disjoint from the
    vm-bhyve guests (`rocky`, `freebsd`, `hfs-test0..2`).
    """
    require(isinstance(run_id, str) and RUN_ID.fullmatch(run_id) is not None,
            "run ID must match zq3-[a-z0-9][a-z0-9-]{0,19}")
    vm = "hfs-" + run_id
    staging = "%s/%s" % (STAGING_PARENT, run_id)
    return {"vm": vm, "staging": staging, "launch": staging + "/launch",
            "vars_copy": staging + "/launch/uefi-vars.fd",
            "exporter": staging + "/bin/bhyve", "nmdm": "nmdm-%s.1" % vm,
            "label": "homelabfs-" + vm}


def last_launch_block(tail, truncated):
    """Return the hardware tokens of the last vm-run `booting` iteration.

    vm-run logs options/devices/console on consecutive lines, then
    `starting bhyve (run N)`. A later `initialising` without its own block,
    an install ISO line or a malformed block refuses.
    """
    lines = tail.splitlines()
    if truncated:
        lines = lines[1:]  # The first line of a byte-bounded tail may be partial.
    parsed = [LOG_LINE.fullmatch(line) for line in lines]
    require(all(parsed), "unrecognized vm-bhyve log line")
    starts = [i for i, m in enumerate(parsed) if m[2].startswith(" [bhyve options: ")]
    require(starts, "no vm-run launch block in the log tail")
    i = starts[-1]
    block = [m[2] for m in parsed[i:i + 4]]
    require(len(block) == 4 and len({m[1] for m in parsed[i:i + 3]}) == 1,
            "incomplete vm-run launch block")
    require(re.fullmatch(r"starting bhyve \(run [1-9][0-9]*\)", block[3]) is not None,
            "launch block not followed by starting bhyve (ISO or changed vm-run)")
    require("initialising" not in [m[2] for m in parsed[i + 4:]],
            "a later launch attempt has no launch block")
    result = {"timestamp": parsed[i][1]}
    for key, text in zip(("options", "devices", "console"), block[:3]):
        prefix = " [bhyve %s: " % key
        require(text.startswith(prefix) and text.endswith("]"), "malformed " + key)
        result[key] = text[len(prefix):-1].split()
    return result


def match_baseline(block):
    """Refuse unless the logged launch equals the pinned hardware profile."""
    for key, expected in BASELINE.items():
        actual = block[key]
        require(len(actual) == len(expected), "launch %s drifted from baseline" % key)
        for want, got in zip(expected, actual):
            if "TAP" in want:
                pattern = re.escape(want).replace("TAP", r"tap[0-9]+")
                require(re.fullmatch(pattern, got) is not None,
                        "launch %s drifted: %s" % (key, got))
            else:
                require(got == want, "launch %s drifted: %s != %s" % (key, got, want))


def check_bootrom(fact):
    """The firmware fact must be the pinned package link and target bytes."""
    require(fact.get("link") == BOOTROM_LINK,
            "firmware symlink text drifted: %r" % (fact.get("link"),))
    require(fact.get("target") == BOOTROM_TARGET,
            "firmware symlink target drifted: %r" % (fact.get("target"),))
    require(fact.get("sha256") == BOOTROM_SHA256, "pinned file drifted: " + BOOTROM_TARGET)


def check_facts(facts):
    """Gate the reviewed hardware subset of read-only facts; return mutable pins."""
    require(isinstance(facts, dict) and facts.get("version") == FACTS_VERSION,
            "launch facts version")
    require(isinstance(facts.get("files"), dict) and isinstance(facts.get("rocky_conf"), str) and
            isinstance(facts.get("interfaces"), list) and isinstance(facts.get("log"), dict) and
            isinstance(facts["log"].get("tail"), str) and
            isinstance(facts["log"].get("truncated"), bool), "launch facts shape")
    files = facts["files"]
    require(set(files) == {ROCKY_CONF, VM_RUN, BOOTROM, VARS, DISK} and
            all(isinstance(item, dict) for item in files.values()), "closed file facts")
    pinned = {ROCKY_CONF: ROCKY_CONF_SHA256, VM_RUN: VM_RUN_SHA256}
    for path, want in pinned.items():
        require(files[path].get("sha256") == want, "pinned file drifted: " + path)
    check_bootrom(files[BOOTROM])
    require(sha256(facts["rocky_conf"].encode()) == ROCKY_CONF_SHA256,
            "rocky.conf text does not match its pinned hash")
    vars_fact = files[VARS]
    require(HEX64.fullmatch(str(vars_fact.get("sha256"))) is not None and
            isinstance(vars_fact.get("bytes"), int) and vars_fact["bytes"] > 0,
            "UEFI vars facts")
    require(isinstance(files[DISK].get("bytes"), int) and files[DISK]["bytes"] > 0,
            "disk facts")
    require(BRIDGE in facts["interfaces"], "bridge %s absent" % BRIDGE)
    block = last_launch_block(facts["log"]["tail"], facts["log"]["truncated"])
    match_baseline(block)
    return {"vars_sha256": vars_fact["sha256"], "vars_bytes": vars_fact["bytes"],
            "timestamp": block["timestamp"]}


def replace_once(tokens, old, new, changes, why):
    """Apply one listed change; refuse unless `old` occurs exactly once."""
    hits = [i for i in range(len(tokens) - len(old) + 1) if tokens[i:i + len(old)] == old]
    require(len(hits) == 1, "baseline change not applicable exactly once: " + why)
    changes.append(why)
    return tokens[:hits[0]] + new + tokens[hits[0] + len(old):]


def dedicated_devices(sessions):
    """Two controlled virtio-9p devices.

    bhyve's legacy `-s` virtio-9p parser maps a `name=value` token to
    sharename/path and refuses a second `=`, so `hfs-controller=` cannot be
    spelled inside `-s`. Each device is declared bare and configured by `-o`
    after its own `-s` (bhyve requires `-s` before `-o` for one device).
    """
    tokens = []
    for share, slot, session in zip(SHARES, NINEP_SLOTS, sessions):
        node = "pci." + slot.replace(":", ".")
        tokens += ["-s", slot + ",virtio-9p",
                   "-o", "%s.sharename=%s" % (node, share),
                   "-o", "%s.hfs-controller=%s" % (node, session)]
    return tokens


def controlled_args(names, sessions, tap):
    """Derive the exporter argv from the baseline by explicit listed changes.

    Order follows vm-run (devices, options, console, VM name). The NIC keeps
    slot 0:5:0 and its MAC, so the guest interface name and address persist.
    """
    changes = []
    devices = replace_once(BASELINE["devices"], ["-s", "0:5:0,virtio-net,TAP,mac=" + MAC],
                           ["-s", "0:5:0,virtio-net,%s,mac=%s" % (tap, MAC)],
                           changes, "pinned transient tap %s on %s" % (tap, BRIDGE))
    devices = replace_once(devices, ["-s", FBUF_DEVICE], [], changes,
                           "drop fbuf/VNC device; com1 nmdm is the console")
    options = replace_once(BASELINE["options"], ["-m", "12G"], ["-m", GUEST_MEMORY],
                           changes, "guest memory 12G -> 6G (plan decision)")
    options = replace_once(options, ["-l", "bootrom,%s,%s" % (BOOTROM, VARS)],
                           ["-l", "bootrom,%s,%s" % (BOOTROM, names["vars_copy"])],
                           changes, "run-owned copy of the UEFI vars; original never written")
    options = replace_once(options, ["-s", POC_DEVICE], dedicated_devices(sessions),
                           changes, "replace PoC 9p share with two hfs-controller devices")
    console = replace_once(BASELINE["console"], ["-l", "com1,/dev/nmdm-rocky.1A"],
                           ["-l", "com1,/dev/%sA" % names["nmdm"]],
                           changes, "run-owned nmdm console pair")
    changes.append("vmm context name rocky -> " + names["vm"])
    return devices + options + console + [names["vm"]], changes


def check_run_inputs(run_id, sessions, tap):
    names = run_names(run_id)
    require(isinstance(sessions, list) and len(sessions) == len(SHARES) and
            len(set(sessions)) == len(SHARES) and
            all(isinstance(s, str) and SESSION.fullmatch(s) for s in sessions),
            "exactly two distinct canonical lowercase session UUIDs required")
    require(isinstance(tap, str) and TAP.fullmatch(tap) is not None,
            "tap must be a pinned unit tap100..tap999")
    return names


def build_record(run_id, sessions, tap, pins, facts_sha256):
    """The record is a pure function of its inputs, so it can be rebuilt."""
    names = check_run_inputs(run_id, sessions, tap)
    args, changes = controlled_args(names, sessions, tap)
    host = {"tap": tap, "bridge": BRIDGE, "label": names["label"], "nmdm": names["nmdm"],
            "launch": names["launch"], "staging": names["staging"],
            "vars_source": VARS, "vars_source_sha256": pins["vars_sha256"],
            "vars_bytes": pins["vars_bytes"], "vars_copy": names["vars_copy"],
            "bootrom": BOOTROM, "bootrom_link": BOOTROM_LINK,
            "bootrom_target": BOOTROM_TARGET, "bootrom_sha256": BOOTROM_SHA256, "disk": DISK,
            "rocky_conf": ROCKY_CONF, "rocky_conf_sha256": ROCKY_CONF_SHA256,
            "vm_run_sha256": VM_RUN_SHA256}
    return {"version": RECORD_VERSION, "run_id": run_id, "vm": names["vm"],
            "facts_sha256": facts_sha256, "launch_logged": pins["timestamp"],
            "sessions": dict(zip(SHARES, sessions)), "host": host,
            "exporter": {"path": names["exporter"], "sha256": EXPORTER_SHA256},
            "changes": changes, "args": args, "args_sha256": sha256(canonical(args))}


def validate_record(raw, record, expected_sha256):
    """Refuse a record that is not the approved bytes or not self-consistent."""
    require(HEX64.fullmatch(str(expected_sha256)) is not None and
            sha256(raw) == expected_sha256, "record SHA256 is not the approved pin")
    require(raw == canonical(record), "record is not canonical JSON")
    require(isinstance(record, dict) and set(record) == RECORD_KEYS and
            record["version"] == RECORD_VERSION and
            HEX64.fullmatch(str(record["facts_sha256"])) is not None, "closed record schema")
    host = record["host"]
    require(isinstance(host, dict) and isinstance(record["sessions"], dict) and
            list(record["sessions"]) == sorted(SHARES), "record host/session shape")
    pins = {"vars_sha256": host.get("vars_source_sha256"),
            "vars_bytes": host.get("vars_bytes"), "timestamp": record["launch_logged"]}
    require(HEX64.fullmatch(str(pins["vars_sha256"])) is not None and
            isinstance(pins["vars_bytes"], int) and pins["vars_bytes"] > 0, "vars pins")
    rebuilt = build_record(record["run_id"], [record["sessions"][s] for s in SHARES],
                           host.get("tap"), pins, record["facts_sha256"])
    require(rebuilt == record, "record drifted from its pinned derivation")
    return record


def exporter_digest(value):
    """Go encodes RuntimeConfig.Exporter.SHA256 ([32]byte) as 32 numbers."""
    require(isinstance(value, list) and len(value) == 32 and
            all(isinstance(b, int) and not isinstance(b, bool) and 0 <= b <= 255
                for b in value), "Exporter.SHA256 must be 32 bytes")
    return bytes(value).hex()


def verify_runtime(record, runtime):
    """Refuse any Exporter argv/path/digest drift in a runtime config."""
    require(isinstance(runtime, dict) and isinstance(runtime.get("Exporter"), dict),
            "runtime config without Exporter")
    exporter = runtime["Exporter"]
    require(exporter.get("Path") == record["exporter"]["path"], "Exporter.Path drifted")
    require(exporter_digest(exporter.get("SHA256")) == record["exporter"]["sha256"],
            "Exporter.SHA256 is not the candidate bhyve")
    args = exporter.get("Args")
    require(isinstance(args, list) and all(isinstance(a, str) for a in args),
            "Exporter.Args must be a string list")
    require(args == record["args"], "Exporter.Args drifted from the pinned argv")
    return {"verified": True, "args_sha256": record["args_sha256"]}


def write_exclusive(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())


def command_generate(args):
    raw, facts = load_json(args.facts)
    pins = check_facts(facts)
    record = build_record(args.run_id, args.session, args.tap, pins, sha256(raw))
    raw_record = canonical(record)
    write_exclusive(args.output, raw_record)
    return {"record": args.output, "record_sha256": sha256(raw_record),
            "args_sha256": record["args_sha256"]}


def command_verify(args):
    raw, record = load_json(args.record)
    validate_record(raw, record, args.record_sha256)
    _, runtime = load_json(args.runtime_config)
    return verify_runtime(record, runtime)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="action", required=True)
    gen = sub.add_parser("generate", help="pin the argv from read-only launch facts")
    gen.add_argument("--facts", required=True)
    gen.add_argument("--run-id", required=True)
    gen.add_argument("--session", action="append", required=True,
                     help="session UUID for hfs0, then hfs1")
    gen.add_argument("--tap", required=True)
    gen.add_argument("--output", required=True)
    ver = sub.add_parser("verify", help="refuse runtime Exporter drift")
    ver.add_argument("--record", required=True)
    ver.add_argument("--record-sha256", required=True)
    ver.add_argument("--runtime-config", required=True)
    args = parser.parse_args(argv)
    try:
        result = {"generate": command_generate, "verify": command_verify}[args.action](args)
    except Refused as refusal:
        print(json.dumps({"refused": str(refusal)}), file=sys.stderr)
        return 2
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
