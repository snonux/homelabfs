#!/usr/bin/env python3
"""zq3 gap G5: Rocky test-kernel install, one-shot selection, removal, admission.

Preparation tool for docs/lab/zq3-run-plan.md ("Phase 3", "Phase 5" and
"Boot selection and recovery"). It grants no authority: every Rocky-side
command still needs Paul's explicit approval (A3) of the filled plan.

Two halves:

* `plan` runs on the workstation. It reads the pinned xz3 candidate package,
  checks every Linux artifact against the admitted manifest, derives the GNU
  build IDs of `vmlinux` and each module, and writes one closed plan JSON.
* All other commands run on Rocky as root (`/usr/bin/python3 -I -B`, Python
  3.9) against that plan, pinned by its SHA256, plus a root-private state
  directory holding a journal. A journal intent without a result is
  *uncertain* and blocks the mutating commands install, select, remove,
  reboot-to-stock and admission (inspect, never replay). U1 `cancel` stays
  allowed, because unsetting `next_entry` is always the safe direction, and
  the read-only `check`/`status` and `reconcile` still run. After
  inspecting, root records the outcome with `reconcile` (counted as failed,
  never repeated). A torn journal line (interrupted write) blocks everything
  except `cancel`, `status` and the read-only `check` (which only appends)
  until root repairs the file.

Commands map to the plan as follows: `install` (Phase 3 steps 1-4),
`select` (Phase 5 step 2, the last Rocky action before the f3 stop; any
failure runs U1 automatically), `cancel` (U1), `remove` (U2), `check`
(U3 step 3, Phase 11 step 2, Phase 12 step 2), `reboot-to-stock` (U3 step 4)
and `admission` (the installed-admission-v1.json generator; it needs the
booted test kernel because the manifest must list exactly the loaded
modules, see `build_admission`). `status` prints the journal.

This is one file on purpose: Rocky gets exactly one staged, hashed script.

The tool never changes the grub default (`grubby --set-default` and
`--make-default` are never run) and verifies the default and `saved_entry`
before and after every mutation. The OS boundary (paths, identity, process
execution, free space) lives only in `System`, so local tests substitute a
simulated Rocky root, grubby and grubenv.
"""
import argparse
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import struct
import subprocess
import sys

PLAN_VERSION = "homelabfs-zq3-rocky-kernel-plan/1"
MANIFEST_VERSION = "homelabfs-installed-artifacts/1"  # internal/controlledruntime
PHYSICAL_CONTRACT = "homelabfs-stop-adoption/1"
ADMISSION_PATH = "/etc/homelabfs/installed-admission-v1.json"
# Same pins as controller.py (this file is staged alone on Rocky, so it
# cannot import them; a local test asserts they stay equal).
# z54 daemon rebuild (2026-11-09, source a20aca1; its guest kernel is the w54
# kernel, byte for byte). Superseded candidates refuse distinctly: e0cca0c
# cannot arm the d44 MKDIR-only lost reply, f44 (defd974) has the pre-r54 guest
# kernel whose hfs1.4 mounts return EPROTO, r54 (ca920a2) has the pre-w54 guest
# kernel that oopses in kill_sb after a failed hfs_fill_super, and w54
# (ff866c5) has the pre-z54 daemon that leaves a pre-acquire VIEW_POLL
# unanswered.
SUPERSEDED = {"3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1",
              "b203799e3f631888b2c2edc24e1d38b913249bea978e0d4d954886f589d04fa2",
              "5eb42c5066792b55e439373db48e8e8e88532efc61ff7b82f64db22a87798f99",
              "89c928d5072838bb509ec19e471109b549ce7b06c59451fab64a6d09250bec66"}
ADMITTED = {"2e0412a62d1c1f0aa0b29865c521bf3df7a925ea5ef293428fa9b38b647d8f2f"}
QUARANTINED = {"7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b"}
HASH = re.compile(r"[0-9a-f]{64}\Z")
BUILD_ID = re.compile(r"[0-9a-f]{16,128}\Z")  # Same bound as probe/installed.py.
RUN_ID = re.compile(r"[a-z0-9][a-z0-9-]{0,23}\Z")
RELEASE = re.compile(r"[0-9][0-9A-Za-z._+-]{0,63}\Z")
ENTRY_ID = re.compile(r"[A-Za-z0-9._+-]{1,200}\Z")
PART = re.compile(r"[A-Za-z0-9_-][A-Za-z0-9_.-]{0,63}\Z")
SAFE_PATH = re.compile(r"/[A-Za-z0-9_./-]+\Z")
DEPMOD_OUTPUT = re.compile(r"modules\.[a-z.]{1,40}\Z")
PARTIAL = ".hfs-zq3-partial"
BOOT_FILES = {"linux/bzImage": "vmlinuz-{}", "linux/System.map": "System.map-{}",
              "linux/config": "config-{}"}
MODULE_META = ("modules.order", "modules.builtin", "modules.builtin.modinfo")
ARTIFACT_LIMIT = 128 << 20  # Same per-artifact bound as probe/installed.py.
JSON_LIMIT = 1 << 20
# Absolute Rocky 9 tool paths; Phase 1 records their versions. depmod is a
# symlink to kmod, so the trust check follows links (see check_tool).
TOOLS = {"grubby": "/usr/sbin/grubby", "editenv": "/usr/bin/grub2-editenv",
         "reboot-once": "/usr/sbin/grub2-reboot", "depmod": "/usr/sbin/depmod",
         "dracut": "/usr/bin/dracut", "systemctl": "/usr/bin/systemctl",
         "udevadm": "/usr/bin/udevadm"}
TIMEOUTS = {"dracut": 600, "depmod": 120, "udevadm": 45}
UDEV_SETTLE = ["settle", "--timeout=30"]  # Module autoload done before admission.
TOOL_ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}
OUTPUT_LIMIT = 1 << 20
INITRAMFS_ESTIMATE = 64 << 20  # /boot headroom check before dracut runs.


class Refused(RuntimeError):
    """Precondition failed before any change (exit 2)."""


class Failed(RuntimeError):
    """A step ran and did not reach its verified result (exit 1)."""


# --------------------------------------------------------------------------
# JSON and file primitives


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Refused("duplicate JSON key")
        result[key] = value
    return result


def parse(raw):
    def nonfinite(_):
        raise Refused("nonfinite JSON")
    return json.loads(raw, object_pairs_hook=unique, parse_constant=nonfinite)


def canonical(value):
    """Deterministic bytes, no trailing newline, so the manifest hash is stable."""
    return json.dumps(value, sort_keys=True, separators=(",", ":"),
                      allow_nan=False).encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def read_bounded(path, bound):
    """Read one regular file without following a final symlink."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_size > bound:
            raise Refused("bounded regular file required: " + str(path))
        chunks, total = [], 0
        while True:
            chunk = os.read(fd, 1 << 20)
            if not chunk:
                break
            total += len(chunk)
            if total > bound:
                raise Refused("file grew beyond bound: " + str(path))
            chunks.append(chunk)
        return b"".join(chunks)
    finally:
        os.close(fd)


def clean_absolute(text):
    if (not SAFE_PATH.fullmatch(text) or os.path.normpath(text) != text or
            text == "/"):
        raise Refused("canonical absolute safe path required: " + text)
    return Path(text)


def fsync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write_exclusive(path, raw, mode):
    """Create a new file durably; never replaces an existing name."""
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, mode)
    try:
        os.fchmod(fd, mode)  # Independent of the caller's umask.
        view = memoryview(raw)
        while view:
            view = view[os.write(fd, view):]
        os.fsync(fd)
    finally:
        os.close(fd)
    fsync_directory(path.parent)


# --------------------------------------------------------------------------
# ELF GNU build IDs (same note walk as internal/controlledruntime/probe)


def note_build_ids(raw):
    """GNU build-id descriptors in one raw note blob, 4-byte aligned like the probe."""
    offset, found = 0, []
    while offset < len(raw):
        if len(raw) - offset < 12:
            raise Refused("truncated ELF note")
        namesz, descsz, kind = struct.unpack_from("<III", raw, offset)
        offset += 12
        name = raw[offset:offset + namesz]
        offset += (namesz + 3) & ~3
        desc = raw[offset:offset + descsz]
        offset += (descsz + 3) & ~3
        if offset > len(raw):
            raise Refused("invalid ELF note")
        if kind == 3 and name == b"GNU\0":
            found.append(desc.hex())
    return found


def single_build_id(found):
    if len(found) != 1 or not BUILD_ID.fullmatch(found[0]):
        raise Refused("missing or ambiguous GNU build ID")
    return found[0]


def elf_build_id(raw):
    """Build ID from every SHT_NOTE section of an ELF64 little-endian file."""
    if len(raw) < 64 or raw[:6] != b"\x7fELF\x02\x01":
        raise Refused("ELF64 little-endian file required")
    (shoff,) = struct.unpack_from("<Q", raw, 0x28)
    shentsize, shnum = struct.unpack_from("<HH", raw, 0x3A)
    if shentsize != 64 or shoff + shnum * 64 > len(raw):
        raise Refused("invalid ELF section table")
    found = []
    for index in range(shnum):
        header = shoff + index * 64
        (kind,) = struct.unpack_from("<I", raw, header + 4)
        if kind != 7:  # SHT_NOTE
            continue
        offset, size = struct.unpack_from("<QQ", raw, header + 0x18)
        if offset + size > len(raw):
            raise Refused("invalid ELF note section")
        found.extend(note_build_ids(raw[offset:offset + size]))
    return single_build_id(found)


# --------------------------------------------------------------------------
# Plan: derived targets and closed validation


def module_name(source):
    """Kernel module name as /proc/modules reports it."""
    return Path(source).name[:-len(".ko")].replace("-", "_")


def derived_target(source, release):
    """The only install location for one package source; plans cannot choose."""
    if source in BOOT_FILES:
        return "/boot/" + BOOT_FILES[source].format(release)
    name = source[len("linux/"):]
    if source.startswith("linux/") and name in MODULE_META:
        return "/lib/modules/{}/{}".format(release, name)
    parts = source.split("/")
    if (len(parts) >= 4 and parts[:2] == ["linux", "modules"] and
            all(PART.fullmatch(part) for part in parts[2:]) and
            source.endswith(".ko")):
        return "/lib/modules/{}/kernel/{}".format(release, "/".join(parts[2:]))
    raise Refused("unsupported package source: " + source)


def require_keys(value, keys, what):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise Refused(what + " must have exactly keys " + ",".join(sorted(keys)))


def check_plan_file(entry, release):
    require_keys(entry, {"source", "target", "sha256", "bytes"}, "plan file")
    if entry["target"] != derived_target(entry["source"], release):
        raise Refused("plan target is not the derived location: " + entry["target"])
    if not HASH.fullmatch(entry["sha256"]) or type(entry["bytes"]) is not int or \
            not 0 < entry["bytes"] <= ARTIFACT_LIMIT:
        raise Refused("plan file hash/size: " + entry["source"])


def check_plan_modules(plan, files):
    sources = {e["source"] for e in plan["files"] if e["source"].endswith(".ko")}
    if {m["source"] for m in plan["modules"].values()} != sources:
        raise Refused("plan modules must be exactly the staged .ko files")
    for name, module in plan["modules"].items():
        require_keys(module, {"source", "target", "sha256", "build_id"}, "plan module")
        entry = files[module["source"]]
        if (name != module_name(module["source"]) or
                not name.replace("_", "").isalnum() or
                module["target"] != entry["target"] or
                module["sha256"] != entry["sha256"] or
                not BUILD_ID.fullmatch(module["build_id"])):
            raise Refused("plan module inconsistent: " + name)


def check_plan_shape(plan):
    require_keys(plan, {"version", "run_id", "manifest_sha256", "kernel_release",
                        "title", "stock_default_kernel", "abi", "kernel", "files",
                        "modules", "boot_reserve_bytes"}, "plan")
    if plan["version"] != PLAN_VERSION or plan["abi"] != PHYSICAL_CONTRACT:
        raise Refused("plan version/abi")
    if not RUN_ID.fullmatch(plan["run_id"]) or \
            not RELEASE.fullmatch(plan["kernel_release"]):
        raise Refused("plan run ID/kernel release")
    if plan["manifest_sha256"] in SUPERSEDED:
        raise Refused("plan names a superseded xz3 candidate; use the z54 daemon rebuild")
    if plan["manifest_sha256"] in QUARANTINED or plan["manifest_sha256"] not in ADMITTED:
        raise Refused("plan names a candidate manifest that is not admitted")
    stock = plan["stock_default_kernel"]
    if not stock.startswith("/boot/vmlinuz-") or \
            not RELEASE.fullmatch(stock[len("/boot/vmlinuz-"):]) or \
            stock == "/boot/vmlinuz-" + plan["kernel_release"]:
        raise Refused("plan stock default kernel")
    if plan["title"] != "HomelabFS zq3 " + plan["kernel_release"]:
        raise Refused("plan title")
    if type(plan["boot_reserve_bytes"]) is not int or plan["boot_reserve_bytes"] < 0:
        raise Refused("plan boot reserve")
    if not isinstance(plan["files"], list) or not isinstance(plan["modules"], dict):
        raise Refused("plan files/modules")


def load_plan(raw, expected_sha256):
    """Exact pinned plan bytes; every target is recomputed, never trusted."""
    if not HASH.fullmatch(expected_sha256) or digest(raw) != expected_sha256:
        raise Refused("plan SHA256 mismatch")
    plan = parse(raw)
    check_plan_shape(plan)
    release = plan["kernel_release"]
    files = {}
    for entry in plan["files"]:
        check_plan_file(entry, release)
        if entry["source"] in files:
            raise Refused("duplicate plan source")
        files[entry["source"]] = entry
    required = set(BOOT_FILES) | {"linux/" + name for name in MODULE_META}
    if not required <= set(files):
        raise Refused("plan lacks a required kernel file")
    kernel = plan["kernel"]
    require_keys(kernel, {"target", "sha256", "build_id"}, "plan kernel")
    if (kernel["target"] != files["linux/bzImage"]["target"] or
            kernel["sha256"] != files["linux/bzImage"]["sha256"] or
            not BUILD_ID.fullmatch(kernel["build_id"])):
        raise Refused("plan kernel inconsistent")
    check_plan_modules(plan, files)
    return plan


def kernel_path(plan):
    return "/boot/vmlinuz-" + plan["kernel_release"]


def initramfs_path(plan):
    return "/boot/initramfs-{}.img".format(plan["kernel_release"])


def kdump_initramfs_path(plan):
    """Rocky's kdump service builds this per booted kernel (Phase 1 fact:
    kdump enabled and active); a test-kernel boot may create it, so U2
    removes it if present. The tool itself never creates it."""
    return "/boot/initramfs-{}kdump.img".format(plan["kernel_release"])


def stock_release(plan):
    return plan["stock_default_kernel"][len("/boot/vmlinuz-"):]


# --------------------------------------------------------------------------
# Workstation: build the plan from the pinned candidate package


def read_manifest(package, expected):
    if expected in SUPERSEDED:
        raise Refused("superseded xz3 candidate manifest; use the z54 daemon rebuild")
    if expected in QUARANTINED or expected not in ADMITTED:
        raise Refused("candidate manifest is not admitted")
    raw = read_bounded(package / "manifest.json", JSON_LIMIT)
    if digest(raw) != expected:
        raise Refused("candidate manifest SHA256 mismatch")
    manifest = parse(raw)
    if manifest.get("version") != "homelabfs-controlled-artifacts/1" or \
            not isinstance(manifest.get("files"), dict):
        raise Refused("candidate manifest shape")
    return manifest


def package_file(package, manifest, source, kind=None):
    """Exact package bytes for one manifest entry (size and SHA256)."""
    entry = manifest["files"].get(source)
    if not isinstance(entry, dict) or (kind and entry.get("kind") != kind):
        raise Refused("manifest lacks " + source)
    raw = read_bounded(package / source, ARTIFACT_LIMIT)
    if len(raw) != entry.get("bytes") or digest(raw) != entry.get("sha256"):
        raise Refused("package file does not match manifest: " + source)
    return raw, entry


def proposal_artifacts(package, manifest, release):
    raw, _ = package_file(package, manifest, "installation-proposal.json", "proposal")
    proposal = parse(raw)
    policy = proposal.get("policy")
    if (proposal.get("kernel_release") != release or not isinstance(policy, dict) or
            policy.get("physical_contract") != PHYSICAL_CONTRACT or
            not isinstance(proposal.get("artifacts"), dict)):
        raise Refused("installation proposal disagrees with kernel release/contract")
    return proposal["artifacts"]


def module_sources(package, manifest):
    """modules.order must name exactly the manifest's module artifacts."""
    raw, _ = package_file(package, manifest, "linux/modules.order")
    order = ["linux/modules/" + line for line in raw.decode("ascii").split("\n") if line]
    listed = sorted(source for source, entry in manifest["files"].items()
                    if source.startswith("linux/modules/") and
                    isinstance(entry, dict) and entry.get("kind") == "module")
    if sorted(order) != listed or len(set(order)) != len(order) or not order:
        raise Refused("modules.order differs from the manifest's modules")
    return order


def plan_entries(package, manifest, release, artifacts):
    sources = sorted(BOOT_FILES) + ["linux/" + name for name in MODULE_META]
    files, modules = [], {}
    for source in sources + module_sources(package, manifest):
        raw, entry = package_file(package, manifest, source)
        if artifacts.get(source) != entry["sha256"]:
            raise Refused("installation proposal disagrees on " + source)
        target = derived_target(source, release)
        files.append({"source": source, "target": target,
                      "sha256": entry["sha256"], "bytes": entry["bytes"]})
        if source.endswith(".ko"):
            modules[module_name(source)] = {"source": source, "target": target,
                                            "sha256": entry["sha256"],
                                            "build_id": elf_build_id(raw)}
    return files, modules


def build_plan(package, manifest_sha256, run_id, stock_default, reserve):
    """Closed plan bytes; validated with the same loader Rocky uses."""
    manifest = read_manifest(package, manifest_sha256)
    release = package_file(package, manifest, "linux/kernel.release")[0]
    release = release.decode("ascii").strip()
    artifacts = proposal_artifacts(package, manifest, release)
    files, modules = plan_entries(package, manifest, release, artifacts)
    vmlinux = package_file(package, manifest, "linux/vmlinux", "kernel")[0]
    bzimage = next(entry for entry in files if entry["source"] == "linux/bzImage")
    plan = {"version": PLAN_VERSION, "run_id": run_id,
            "manifest_sha256": manifest_sha256, "kernel_release": release,
            "title": "HomelabFS zq3 " + release, "stock_default_kernel": stock_default,
            "abi": PHYSICAL_CONTRACT, "files": files, "modules": modules,
            # bzImage carries vmlinux's notes; admission checks the running
            # /sys/kernel/notes against this ID, binding image and source.
            "kernel": {"target": bzimage["target"], "sha256": bzimage["sha256"],
                       "build_id": elf_build_id(vmlinux)},
            "boot_reserve_bytes": reserve}
    raw = canonical(plan) + b"\n"
    load_plan(raw, digest(raw))
    return raw


# --------------------------------------------------------------------------
# The OS boundary


def check_tool(executable, trusted_uid=0):
    """Root-owned, not group/world-writable executable file (symlinks followed)."""
    info = os.stat(executable)
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != trusted_uid or
            info.st_mode & 0o022 or not info.st_mode & 0o100):
        raise Refused("untrusted tool: " + executable)


class System:
    """Paths, identity, free space and tool execution on the real Rocky.

    Tests subclass it with a temporary root and simulated tools; nothing else
    in this file touches the OS outside the operator-given plan/stage/state.
    """

    trusted_uid = 0

    def __init__(self, root="/"):
        self.root = Path(root)

    def path(self, absolute):
        if not str(absolute).startswith("/"):
            raise Refused("absolute OS path required")
        return self.root / str(absolute).lstrip("/")

    def euid(self):
        return os.geteuid()

    def platform(self):
        return sys.platform

    def release(self):
        return os.uname().release

    def boot_id(self):
        raw = read_bounded(self.path("/proc/sys/kernel/random/boot_id"), 64)
        return raw.decode("ascii").strip()

    def free_bytes(self, absolute):
        info = os.statvfs(self.path(absolute))
        return info.f_bavail * info.f_frsize

    def run(self, tool, args):
        """Fixed argv, fixed environment, bounded output; never a shell."""
        executable = TOOLS[tool]
        check_tool(executable, self.trusted_uid)
        try:
            done = subprocess.run([executable] + list(args), stdin=subprocess.DEVNULL,
                                  capture_output=True, env=TOOL_ENV, check=False,
                                  timeout=TIMEOUTS.get(tool, 30))
        except subprocess.TimeoutExpired:
            raise Failed(tool + " timed out")
        if len(done.stdout) > OUTPUT_LIMIT or len(done.stderr) > OUTPUT_LIMIT:
            raise Failed(tool + " output bound")
        return (done.returncode, done.stdout.decode("utf-8", "replace"),
                done.stderr.decode("utf-8", "replace"))


def parse_info(raw):
    """grubby --info blocks as dicts; each block starts with index=N."""
    blocks = []
    for line in raw.splitlines():
        if not line.strip():
            continue
        key, sep, value = line.partition("=")
        if not sep:
            raise Failed("unparseable grubby line: " + line[:200])
        if key == "index":
            blocks.append({})
        elif not blocks:
            raise Failed("grubby output does not start with index=")
        if len(value) >= 2 and value[0] == value[-1] == '"':
            value = value[1:-1]
        blocks[-1][key] = value
    return blocks


def without_index(blocks):
    """Entries compared independently of position (a new entry shifts indices)."""
    return sorted(canonical({k: v for k, v in b.items() if k != "index"}) for b in blocks)


def run_ok(system, tool, args):
    """Run one fixed tool; a nonzero exit is a failed step, never ignored."""
    code, out, err = system.run(tool, args)
    if code != 0:
        raise Failed("{} {} exit {}: {}".format(tool, " ".join(args)[:80], code,
                                                err.strip()[:400]))
    return out


class Grub:
    """grubby and grubenv; it deliberately has no method that changes the default."""

    def __init__(self, system):
        self.system = system

    def _ok(self, tool, args):
        return run_ok(self.system, tool, args)

    def env(self):
        values = {}
        for line in self._ok("editenv", ["-", "list"]).splitlines():
            key, sep, value = line.partition("=")
            if sep:
                values[key] = value
        return values

    def default_kernel(self):
        return self._ok("grubby", ["--default-kernel"]).strip()

    def info_all(self):
        return self._ok("grubby", ["--info=ALL"])

    def add(self, plan):
        # --copy-default copies the stock arguments; never --make-default.
        self._ok("grubby", ["--add-kernel=" + kernel_path(plan),
                            "--initrd=" + initramfs_path(plan), "--copy-default",
                            "--title=" + plan["title"]])

    def remove(self, plan):
        self._ok("grubby", ["--remove-kernel=" + kernel_path(plan)])

    def set_once(self, entry_id):
        self._ok("reboot-once", [entry_id])

    def unset_once(self):
        self._ok("editenv", ["-", "unset", "next_entry"])

    def state(self):
        """Default kernel, saved_entry and next_entry (absent counts as empty:
        GRUB's own consumption saves next_entry as an empty value)."""
        env = self.env()
        return {"default_kernel": self.default_kernel(),
                "saved_entry": env.get("saved_entry", ""),
                "next_entry": env.get("next_entry", "")}


# --------------------------------------------------------------------------
# Durable state: pre-run record, install record and intent/result journal


def check_private_parent(directory, trusted_uid):
    info = os.lstat(directory.parent)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != trusted_uid or
            info.st_mode & 0o022):
        raise Refused("state parent must be a trusted, non-shared directory")


class Journal:
    """Root-private state directory, held under an exclusive lock per command.

    `journal.jsonl` gets one fsynced line per intent, result or check. An
    intent without a matching result means the step's outcome is unknown.
    """

    def __init__(self, directory, system):
        info = os.lstat(directory)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != system.trusted_uid or
                info.st_mode & 0o077):
            raise Refused("state directory must be a private 0700 directory")
        self.directory, self.system = directory, system
        self.path = directory / "journal.jsonl"
        self.allow_torn = False  # Set only by cancel and status (see records).
        self.lock = os.open(directory / "lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            os.close(self.lock)
            raise Refused("another command holds the state lock")

    def close(self):
        """Release the lock (closing the descriptor drops the flock)."""
        if self.lock >= 0:
            os.close(self.lock)
            self.lock = -1

    @classmethod
    def create(cls, directory, system):
        check_private_parent(directory, system.trusted_uid)
        directory.mkdir(mode=0o700)  # Fails if it exists: one state per run.
        os.chmod(directory, 0o700)
        fsync_directory(directory.parent)
        return cls(directory, system)

    def raw(self):
        if not os.path.lexists(self.path):
            return b""
        return read_bounded(self.path, 4 * JSON_LIMIT)

    def torn(self):
        """Every record is written as one line ending in a newline, so a file
        that does not end in one holds an interrupted (torn) write."""
        raw = self.raw()
        return bool(raw) and not raw.endswith(b"\n")

    def records(self):
        """Parsed records. A torn tail or an unparseable (formerly torn) line
        refuses unless `allow_torn` is set (only cancel and status set it);
        such lines are then skipped. Root repairs the file after inspection."""
        lines = self.raw().split(b"\n")
        complete, tail = lines[:-1], lines[-1]
        result = []
        for line in complete:
            if not line:
                continue
            try:
                result.append(parse(line))
            except (ValueError, Refused):
                if not self.allow_torn:
                    raise Refused("unparseable journal line: root must inspect and repair "
                                  "journal.jsonl; only cancel, status and check run")
        if tail and not self.allow_torn:
            raise Refused("torn journal tail (interrupted write): root must inspect "
                          "and repair journal.jsonl; only cancel, status and check run")
        return result

    def append(self, record):
        record = dict(record, time=datetime.datetime.now(datetime.timezone.utc).isoformat())
        line = canonical(record) + b"\n"
        if self.torn():
            line = b"\n" + line  # Keep the torn fragment on its own line.
        fd = os.open(self.path, os.O_CREAT | os.O_APPEND | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
        try:
            os.write(fd, line)
            os.fsync(fd)
        finally:
            os.close(fd)

    def begin(self, step, boot_id):
        # Strictly increasing, also when skipped torn lines shorten records().
        records = self.records()
        seq = 1 + max([r["seq"] for r in records if "seq" in r] + [len(records)])
        self.append({"seq": seq, "kind": "intent", "step": step, "boot_id": boot_id})
        return seq

    def finish(self, seq, step, ok, detail):
        self.append({"seq": seq, "kind": "result", "step": step, "ok": ok, "detail": detail})

    def uncertain(self, boot_id):
        """Dangling intents. A reboot intent from an earlier boot is resolved:
        the boot ID change proves the requested reboot happened."""
        records = self.records()
        done = {r["seq"] for r in records if r["kind"] == "result"}
        return [r for r in records if r["kind"] == "intent" and r["seq"] not in done and
                not (r["step"] == "reboot" and r["boot_id"] != boot_id)]

    def results(self, step):
        return [r for r in self.records() if r["kind"] == "result" and r["step"] == step]

    def read_json(self, name):
        return parse(read_bounded(self.directory / name, 4 * JSON_LIMIT))

    def write_json(self, name, value):
        write_exclusive(self.directory / name, canonical(value) + b"\n", 0o600)


class Context:
    """One command's plan, OS boundary and (once it exists) journal."""

    def __init__(self, system, plan, plan_sha256, journal):
        self.system, self.plan, self.plan_sha256 = system, plan, plan_sha256
        self.journal, self.grub = journal, Grub(system)


def open_context(args, system, create):
    raw = read_bounded(clean_absolute(args.plan), JSON_LIMIT)
    plan = load_plan(raw, args.plan_sha256)
    state = clean_absolute(args.state)
    if create:
        if os.path.lexists(state):
            raise Refused("state directory must not exist yet")
        return Context(system, plan, args.plan_sha256, None)
    ctx = Context(system, plan, args.plan_sha256, Journal(state, system))
    if ctx.journal.read_json("pre.json")["plan_sha256"] != args.plan_sha256:
        raise Refused("state directory belongs to a different plan")
    return ctx


# --------------------------------------------------------------------------
# Guards shared by the Rocky commands


def require_root(ctx):
    if ctx.system.euid() != 0 or ctx.system.platform() != "linux":
        raise Refused("Rocky root on Linux required")


def require_release(ctx, test, why):
    """Exactly the test kernel (test=True) or exactly the plan's stock default
    (test=False); any third kernel, such as an installed ELRepo one, refuses."""
    expected = ctx.plan["kernel_release"] if test else stock_release(ctx.plan)
    if ctx.system.release() != expected:
        raise Refused("{} (running {}, required {})".format(
            why, ctx.system.release(), expected))


def require_settled(ctx):
    pending = ctx.journal.uncertain(ctx.system.boot_id())
    if pending:
        raise Refused("uncertain step(s) {}: inspect, never replay".format(
            ",".join(r["step"] for r in pending)))


def require_succeeded(ctx, step):
    if not any(r["ok"] for r in ctx.journal.results(step)):
        raise Refused(step + " has not succeeded in this state directory")


def boot_problems(plan, pre, state, next_entry):
    problems = []
    if state["default_kernel"] != plan["stock_default_kernel"]:
        problems.append("grub default kernel is {!r}, plan pins {!r}".format(
            state["default_kernel"], plan["stock_default_kernel"]))
    if state["saved_entry"] != pre["saved_entry"]:
        problems.append("saved_entry is {!r}, was {!r}".format(
            state["saved_entry"], pre["saved_entry"]))
    if state["next_entry"] != next_entry:
        problems.append("next_entry is {!r}, expected {!r}".format(
            state["next_entry"], next_entry))
    return problems


def require_boot(ctx, next_entry, error=Failed):
    """Default and saved_entry unchanged and next_entry exactly as expected."""
    state = ctx.grub.state()
    problems = boot_problems(ctx.plan, ctx.journal.read_json("pre.json"), state, next_entry)
    if problems:
        raise error("; ".join(problems))
    return state


def run_step(ctx, step, action):
    """Durable intent, the action, then a durable result (failures included).

    Only an interruption that kills this process leaves an intent without a
    result, which later commands treat as uncertain.
    """
    seq = ctx.journal.begin(step, ctx.system.boot_id())
    try:
        detail = action()
    except Exception as error:
        ctx.journal.finish(seq, step, False, {"error": str(error)})
        if isinstance(error, Failed):
            raise
        raise Failed(step + ": " + str(error)) from error
    ctx.journal.finish(seq, step, True, detail)
    return detail


def verify_artifact(ctx, target, expected):
    """The checks probe/installed.py applies to an admitted artifact, plus its hash."""
    path = ctx.system.path(target)
    info = os.lstat(path)
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != ctx.system.trusted_uid or
            info.st_mode & 0o022 or info.st_size > ARTIFACT_LIMIT):
        raise Refused("artifact ownership/mode/bound: " + target)
    actual = digest(read_bounded(path, ARTIFACT_LIMIT))
    if expected is not None and actual != expected:
        raise Refused("artifact hash changed: " + target)
    return actual


# --------------------------------------------------------------------------
# install (Phase 3 steps 1-4)


def staged_artifacts(ctx, stage):
    """Read and verify every staged byte once; the step copies these bytes."""
    staged = {}
    for entry in ctx.plan["files"]:
        raw = read_bounded(stage / entry["source"], ARTIFACT_LIMIT)
        if len(raw) != entry["bytes"] or digest(raw) != entry["sha256"]:
            raise Refused("staged file does not match the plan: " + entry["source"])
        staged[entry["target"]] = raw
    return staged


def preflight_install(ctx, state):
    plan, system = ctx.plan, ctx.system
    if state["default_kernel"] != plan["stock_default_kernel"]:
        raise Refused("grub default is not the pinned stock kernel")
    if state["next_entry"]:
        raise Refused("next_entry is already set")
    if any(b.get("kernel") == kernel_path(plan) for b in parse_info(ctx.grub.info_all())):
        raise Refused("a boot entry for the test kernel already exists")
    targets = [e["target"] for e in plan["files"]] + [
        initramfs_path(plan), kdump_initramfs_path(plan), ADMISSION_PATH,
        "/lib/modules/" + plan["kernel_release"]]
    existing = [t for t in targets if os.path.lexists(system.path(t))]
    if existing:
        raise Refused("install targets already exist: " + ",".join(existing))
    need = sum(e["bytes"] for e in plan["files"] if e["target"].startswith("/boot/"))
    need += INITRAMFS_ESTIMATE + plan["boot_reserve_bytes"]
    if system.free_bytes("/boot") < need:
        raise Refused("/boot lacks {} free bytes".format(need))


def record_pre(ctx, state):
    """Pre-run boot facts every later verification compares against."""
    system = ctx.system
    pre = {"plan_sha256": ctx.plan_sha256, "boot_id": system.boot_id(),
           "release": system.release(), "default_kernel": state["default_kernel"],
           "saved_entry": state["saved_entry"], "grubby_info_all": ctx.grub.info_all(),
           "etc_homelabfs_existed": os.path.lexists(system.path("/etc/homelabfs"))}
    ctx.journal.write_json("pre.json", pre)
    return pre


def make_directory(path):
    path.mkdir(mode=0o755)
    os.chmod(path, 0o755)
    fsync_directory(path.parent)


def link_into_place(path, raw, mode):
    """Write beside the target, then link: never clobbers, never half-visible.
    A crash leaves only the `.NAME.hfs-zq3-partial` file, which U2 removes."""
    partial = path.with_name("." + path.name + PARTIAL)
    write_exclusive(partial, raw, mode)
    os.link(partial, path)
    os.unlink(partial)
    fsync_directory(path.parent)


def place(ctx, target, raw):
    """Install one plan file, creating its module directories first."""
    modules = "/lib/modules/" + ctx.plan["kernel_release"]
    if target.startswith(modules + "/"):
        current = ctx.system.path(modules)
        for part in target[len(modules) + 1:].split("/")[:-1]:
            current = current / part
            if not os.path.lexists(current):
                make_directory(current)
    link_into_place(ctx.system.path(target), raw, 0o644)


def verify_added(ctx, pre):
    """Exactly one new entry, all old entries unchanged, boot state unchanged."""
    plan = ctx.plan
    after = parse_info(ctx.grub.info_all())
    ours = [b for b in after if b.get("kernel") == kernel_path(plan)]
    others = [b for b in after if b.get("kernel") != kernel_path(plan)]
    if len(ours) != 1:
        raise Failed("expected exactly one new boot entry, found {}".format(len(ours)))
    if without_index(others) != without_index(parse_info(pre["grubby_info_all"])):
        raise Failed("existing boot entries changed")
    entry = ours[0]
    if (entry.get("title") != plan["title"] or not ENTRY_ID.fullmatch(entry.get("id", "")) or
            entry.get("initrd", "").split()[:1] != [initramfs_path(plan)]):
        raise Failed("new boot entry fields differ from the plan")
    require_boot(ctx, "")
    return entry


def install_steps(ctx, staged, pre):
    plan, system = ctx.plan, ctx.system
    release = plan["kernel_release"]
    make_directory(system.path("/lib/modules/" + release))
    for target, raw in staged.items():
        place(ctx, target, raw)
    installed = {t: verify_artifact(ctx, t, digest(raw)) for t, raw in staged.items()}
    run_ok(system, "depmod", [release])
    run_ok(system, "dracut", ["--kver", release, initramfs_path(plan)])
    ctx.grub.add(plan)
    entry = verify_added(ctx, pre)
    bls = "/boot/loader/entries/{}.conf".format(entry["id"])
    record = {"entry_id": entry["id"], "entry": entry, "bls_path": bls,
              "bls_sha256": verify_artifact(ctx, bls, None), "installed": installed,
              "initramfs_sha256": verify_artifact(ctx, initramfs_path(plan), None)}
    ctx.journal.write_json("install.json", record)
    return {"entry_id": entry["id"], "initramfs_sha256": record["initramfs_sha256"],
            "bls_sha256": record["bls_sha256"]}


def cmd_install(ctx, args):
    require_root(ctx)
    require_release(ctx, False, "install runs while Rocky runs its stock kernel")
    state = ctx.grub.state()
    preflight_install(ctx, state)
    staged = staged_artifacts(ctx, clean_absolute(args.stage))
    ctx.journal = Journal.create(clean_absolute(args.state), ctx.system)
    pre = record_pre(ctx, state)
    detail = run_step(ctx, "install", lambda: install_steps(ctx, staged, pre))
    return dict(detail, step="install", next_entry="")


# --------------------------------------------------------------------------
# select (Phase 5 step 2) and cancel (U1)


def verify_installed(ctx, record):
    """Installed bytes, initramfs and the BLS entry are still what install recorded."""
    for target, sha in record["installed"].items():
        verify_artifact(ctx, target, sha)
    verify_artifact(ctx, initramfs_path(ctx.plan), record["initramfs_sha256"])
    verify_artifact(ctx, record["bls_path"], record["bls_sha256"])
    ours = [b for b in parse_info(ctx.grub.info_all())
            if b.get("kernel") == kernel_path(ctx.plan)]
    if len(ours) != 1 or ours[0].get("id") != record["entry_id"]:
        raise Refused("the recorded test-kernel boot entry changed")


def cancel_once(ctx):
    ctx.grub.unset_once()
    return require_boot(ctx, "")


def cancel_after_failure(ctx):
    try:
        run_step(ctx, "cancel", lambda: cancel_once(ctx))
        return "U1 ran: next_entry unset and verified"
    except Exception as error:  # Reported; the operator must inspect grubenv.
        return "U1 FAILED, inspect grubenv now: " + str(error)


def select_once(ctx, entry_id):
    ctx.grub.set_once(entry_id)
    return require_boot(ctx, entry_id)


def cmd_select(ctx, args):
    """The last Rocky action before the f3 stop; any failure runs U1 at once."""
    require_root(ctx)
    require_release(ctx, False, "select runs while Rocky runs its stock kernel")
    require_settled(ctx)
    require_succeeded(ctx, "install")
    if any(r["step"] == "select" for r in ctx.journal.records()):
        raise Refused("select already ran; a retry needs a new run ID")
    record = ctx.journal.read_json("install.json")
    require_boot(ctx, "", Refused)
    verify_installed(ctx, record)
    try:
        state = run_step(ctx, "select", lambda: select_once(ctx, record["entry_id"]))
    except Failed as error:
        raise Failed("select failed ({}); {}".format(error, cancel_after_failure(ctx)))
    return {"step": "select", "state": state,
            "next_action": "f3, immediately: tools/lab/vms.sh --approved-action stop-rocky"}


def cmd_cancel(ctx, args):
    """U1. Allowed even with uncertain steps or a torn journal: unsetting is
    always safe. If even this cannot run, the documented manual fallback is
    `grub2-editenv - unset next_entry` plus `grub2-editenv - list`."""
    require_root(ctx)
    ctx.journal.allow_torn = True
    return {"step": "cancel", "state": run_step(ctx, "cancel", lambda: cancel_once(ctx))}


# --------------------------------------------------------------------------
# remove (U2)


def removal_files(ctx):
    """Existing /boot and admission files of the test kernel (created by this
    tool, or the kdump initramfs created by kdump in a test-kernel boot)."""
    plan = ctx.plan
    names = [e["target"] for e in plan["files"] if e["target"].startswith("/boot/")]
    names += [initramfs_path(plan), kdump_initramfs_path(plan), ADMISSION_PATH]
    names += [str(Path(n).with_name("." + Path(n).name + PARTIAL)) for n in list(names)]
    found = []
    for name in names:
        path = ctx.system.path(name)
        if os.path.lexists(path):
            if not stat.S_ISREG(os.lstat(path).st_mode):
                raise Refused("not a regular file, preserved: " + name)
            found.append(name)
    return found


def module_allowed(plan):
    base = "/lib/modules/" + plan["kernel_release"]
    files = {e["target"] for e in plan["files"] if e["target"].startswith(base + "/")}
    files |= {str(Path(f).with_name("." + Path(f).name + PARTIAL)) for f in list(files)}
    dirs = {base}
    for name in files:
        parent = Path(name).parent
        while str(parent) != base:
            dirs.add(str(parent))
            parent = parent.parent
    return base, files, dirs


def removal_modules(ctx):
    """The module tree, refusing (before deleting anything) on foreign content."""
    base, allowed_files, allowed_dirs = module_allowed(ctx.plan)
    root = ctx.system.path(base)
    if not os.path.lexists(root):
        return [], []
    files, dirs = [], []
    for current, dirnames, filenames in os.walk(root):
        logical = base + str(Path(current))[len(str(root)):]
        for name in dirnames + filenames:
            path, entry = Path(current) / name, logical + "/" + name
            mode = os.lstat(path).st_mode
            if stat.S_ISDIR(mode) and entry in allowed_dirs:
                dirs.append(entry)
            elif stat.S_ISREG(mode) and (entry in allowed_files or (
                    logical == base and DEPMOD_OUTPUT.fullmatch(name))):
                files.append(entry)
            else:
                raise Refused("unexpected entry, preserved: " + entry)
    dirs = sorted(dirs, key=lambda d: d.count("/"), reverse=True) + [base]
    return files, dirs


def remove_steps(ctx, files, dirs):
    plan, system = ctx.plan, ctx.system
    pre = ctx.journal.read_json("pre.json")
    if any(b.get("kernel") == kernel_path(plan) for b in parse_info(ctx.grub.info_all())):
        ctx.grub.remove(plan)
    removed = {}
    for name in files:
        # Hash first: evidence of exactly what was deleted (e.g. kdump's image).
        removed[name] = digest(read_bounded(system.path(name), ARTIFACT_LIMIT))
        os.unlink(system.path(name))
    for name in dirs:
        os.rmdir(system.path(name))
    kept = []
    etc = system.path("/etc/homelabfs")
    if not pre["etc_homelabfs_existed"] and os.path.isdir(etc):
        if os.listdir(etc):
            kept.append("/etc/homelabfs (not empty, preserved)")
        else:
            os.rmdir(etc)
    if ctx.grub.info_all() != pre["grubby_info_all"]:
        raise Failed("grubby --info=ALL differs from the pre-run listing")
    require_boot(ctx, "")
    return {"removed_files": removed, "removed_dirs": dirs, "kept": kept}


def cmd_remove(ctx, args):
    require_root(ctx)
    require_release(ctx, False, "remove runs only on the stock kernel, never the test kernel")
    require_settled(ctx)
    if any(r["ok"] for r in ctx.journal.results("remove")):
        raise Refused("the test kernel was already removed")
    if ctx.grub.state()["next_entry"]:
        raise Refused("next_entry is set; run cancel (U1) first")
    require_boot(ctx, "", Refused)
    files = removal_files(ctx)
    module_files, dirs = removal_modules(ctx)
    detail = run_step(ctx, "remove", lambda: remove_steps(ctx, files + module_files, dirs))
    return dict(detail, step="remove")


# --------------------------------------------------------------------------
# check (U3 step 3, Phase 11 step 2, Phase 12 step 2) and reboot-to-stock (U3 step 4)


def cmd_check(ctx, args):
    """Read-only boot-state check; the result is journaled for reboot-to-stock."""
    require_root(ctx)
    system, plan = ctx.system, ctx.plan
    state, release, boot = ctx.grub.state(), system.release(), system.boot_id()
    problems = boot_problems(plan, ctx.journal.read_json("pre.json"), state, "")
    if args.expect == "test" and release != plan["kernel_release"]:
        problems.append("running {}, expected the test kernel".format(release))
    if args.expect == "stock" and release != stock_release(plan):
        problems.append("running {}, expected stock {}".format(release, stock_release(plan)))
    result = {"step": "check", "expect": args.expect, "release": release, "boot_id": boot,
              "state": state, "problems": problems, "ok": not problems}
    ctx.journal.append(dict(result, kind="check"))
    return result


def cmd_reboot(ctx, args):
    """Clean reboot from the U3 test-kernel boot into the stock default.

    The tool cannot tell a U3 boot from the controlled Phase 8 boot by
    itself: the operator must know which boot they are in (the controlled
    boot ends with Phase 11's guest poweroff, never a reboot). The only
    safeguard is that the admission manifest, normally written only in the
    controlled boot, makes this refuse. A Phase 8 boot that failed before
    admission leaves no manifest, and a manifest written by mistake in a U3
    boot makes this refuse wrongly (U2 removes it after a manual reboot).
    """
    require_root(ctx)
    require_release(ctx, True, "only the test kernel needs the U3 reboot")
    require_settled(ctx)
    if os.path.lexists(ctx.system.path(ADMISSION_PATH)):
        raise Refused("admission manifest exists: refusing (the controlled boot ends "
                      "with Phase 11 poweroff; the operator must know which boot this is)")
    boot = ctx.system.boot_id()
    if not any(r["kind"] == "check" and r["ok"] and r["boot_id"] == boot and
               r["expect"] in ("test", "any") for r in ctx.journal.records()):
        raise Refused("run check --expect test in this boot first")
    require_boot(ctx, "", Refused)
    detail = run_step(ctx, "reboot", lambda: request_reboot(ctx, boot))
    return dict(detail, step="reboot-to-stock",
                next_action="wait for pinned SSH, then check --expect stock and remove")


def request_reboot(ctx, boot):
    # systemctl queues a clean reboot and normally returns first; if the
    # shutdown wins the race, the next boot ID resolves the dangling intent.
    run_ok(ctx.system, "systemctl", ["reboot"])
    return {"from_boot_id": boot}


def cmd_reconcile(ctx, args):
    """Root's explicit outcome for one inspected uncertain intent.

    It never repeats the step; it records that root inspected the leftovers
    and that the step counts as failed, so U1/U2 can clean up afterwards.
    """
    require_root(ctx)
    pending = {r["seq"]: r for r in ctx.journal.uncertain(ctx.system.boot_id())}
    if args.seq not in pending:
        raise Refused("no uncertain intent with that sequence number")
    if not args.note.strip() or len(args.note) > 1000:
        raise Refused("a non-empty inspection note (at most 1000 chars) is required")
    step = pending[args.seq]["step"]
    ctx.journal.finish(args.seq, step, False, {"reconciled": args.note})
    return {"step": "reconcile", "seq": args.seq, "reconciled_step": step}


# --------------------------------------------------------------------------
# admission: /etc/homelabfs/installed-admission-v1.json from the live state


def loaded_modules(system):
    raw = read_bounded(system.path("/proc/modules"), 65536)
    names = sorted(line.split()[0] for line in raw.decode("ascii").splitlines() if line.strip())
    if (len(names) > 256 or len(set(names)) != len(names) or
            any(not name.replace("_", "").isalnum() for name in names)):
        raise Refused("loaded module inventory bound/names")
    return names


def running_build_id(system, path, expected, what):
    found = single_build_id(note_build_ids(read_bounded(system.path(path), 65536)))
    if found != expected:
        raise Refused("running {} build ID {} differs from plan {}".format(what, found, expected))
    return found


def module_record(ctx, name):
    module, system = ctx.plan["modules"][name], ctx.system
    sha = verify_artifact(ctx, module["target"], module["sha256"])
    if elf_build_id(read_bounded(system.path(module["target"]), ARTIFACT_LIMIT)) != \
            module["build_id"]:
        raise Refused("installed module build ID differs from plan: " + name)
    running = running_build_id(system, "/sys/module/{}/notes/.note.gnu.build-id".format(name),
                               module["build_id"], "module " + name)
    return {"path": module["target"], "sha256": sha, "build_id": running}


def build_admission(ctx):
    """Closed manifest whose module set EQUALS /proc/modules.

    probe/installed.py refuses unless every loaded module is listed and every
    listed module is loaded, and it compares the running kernel's notes. Which
    of the five planned modules load depends on the booted hardware, so the
    manifest can only be generated under the booted test kernel.
    """
    system, plan = ctx.system, ctx.plan
    boot, names = system.boot_id(), loaded_modules(system)
    foreign = sorted(set(names) - set(plan["modules"]))
    if foreign:
        raise Refused("loaded modules not installed by the plan: " + ",".join(foreign))
    kernel = {"path": kernel_path(plan),
              "sha256": verify_artifact(ctx, kernel_path(plan), plan["kernel"]["sha256"]),
              "build_id": running_build_id(system, "/sys/kernel/notes",
                                           plan["kernel"]["build_id"], "kernel")}
    modules = {name: module_record(ctx, name) for name in names}
    # Repeat the observation, as the probe does, to refuse a concurrent change.
    if system.boot_id() != boot or loaded_modules(system) != names:
        raise Refused("unstable boot/module observation")
    raw = canonical({"version": MANIFEST_VERSION, "kernel": kernel, "modules": modules,
                     "abi": plan["abi"]})
    if len(raw) > 65536:
        raise Refused("manifest exceeds the probe's 64KiB bound")
    return raw, {"boot_id": boot, "modules": names, "kernel_build_id": kernel["build_id"]}


def write_admission(ctx, raw, summary):
    system = ctx.system
    directory = system.path("/etc/homelabfs")
    if not os.path.lexists(directory):
        make_directory(directory)
    info = os.lstat(directory)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != system.trusted_uid or
            info.st_mode & 0o022):
        raise Failed("/etc/homelabfs must be a trusted directory")
    link_into_place(system.path(ADMISSION_PATH), raw, 0o644)
    if read_bounded(system.path(ADMISSION_PATH), 65536) != raw:
        raise Failed("manifest readback differs")
    return dict(summary, path=ADMISSION_PATH, manifest_sha256=digest(raw))


def cmd_admission(ctx, args):
    require_root(ctx)
    require_release(ctx, True, "admission measures the booted test kernel")
    require_settled(ctx)
    require_succeeded(ctx, "install")
    if os.path.lexists(ctx.system.path(ADMISSION_PATH)):
        raise Refused("installed admission manifest exists; never overwritten")
    # udev loads hardware modules asynchronously after boot; snapshot the
    # inventory only once its queue is empty, or a late module load would
    # make the probe's exact /proc/modules equality fail later.
    try:
        run_ok(ctx.system, "udevadm", UDEV_SETTLE)
    except Failed as error:
        raise Refused("udev queue did not settle: " + str(error))
    raw, summary = build_admission(ctx)
    detail = run_step(ctx, "admission", lambda: write_admission(ctx, raw, summary))
    return dict(detail, step="admission")


def cmd_status(ctx, args):
    """Read-only journal view; skips (and reports) torn or unparseable lines."""
    ctx.journal.allow_torn = True
    records = ctx.journal.records()
    return {"step": "status", "records": records, "torn_tail": ctx.journal.torn(),
            "uncertain": ctx.journal.uncertain(ctx.system.boot_id())}


# --------------------------------------------------------------------------
# CLI


def cmd_plan(args):
    output = clean_absolute(args.output)
    raw = build_plan(clean_absolute(args.package), args.manifest_sha256, args.run_id,
                     args.stock_default_kernel, args.boot_reserve_bytes)
    write_exclusive(output, raw, 0o600)
    plan = parse(raw)
    return {"step": "plan", "plan": str(output), "plan_sha256": digest(raw),
            "kernel_release": plan["kernel_release"],
            "kernel_build_id": plan["kernel"]["build_id"],
            "modules": sorted(plan["modules"])}


COMMANDS = {"install": cmd_install, "select": cmd_select, "cancel": cmd_cancel,
            "remove": cmd_remove, "check": cmd_check, "reboot-to-stock": cmd_reboot,
            "admission": cmd_admission, "status": cmd_status, "reconcile": cmd_reconcile}


def parser():
    result = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = result.add_subparsers(dest="command", required=True)
    plan = sub.add_parser("plan", help="workstation: build the closed plan from the package")
    plan.add_argument("--package", required=True)
    plan.add_argument("--manifest-sha256", required=True)
    plan.add_argument("--run-id", required=True)
    plan.add_argument("--stock-default-kernel", required=True,
                      help="fresh Phase 1 `grubby --default-kernel` value")
    plan.add_argument("--boot-reserve-bytes", type=int, default=64 << 20)
    plan.add_argument("--output", required=True)
    for name in COMMANDS:
        command = sub.add_parser(name, help="Rocky root: " + name)
        command.add_argument("--plan", required=True)
        command.add_argument("--plan-sha256", required=True)
        command.add_argument("--state", required=True)
        if name == "install":
            command.add_argument("--stage", required=True)
        if name == "check":
            command.add_argument("--expect", required=True, choices=["test", "stock", "any"])
        if name == "reconcile":
            command.add_argument("--seq", required=True, type=int)
            command.add_argument("--note", required=True)
    return result


def run_command(args, system):
    if args.command == "plan":
        return cmd_plan(args)
    ctx = open_context(args, system or System(), create=args.command == "install")
    try:
        return COMMANDS[args.command](ctx, args)
    finally:
        if ctx.journal is not None:
            ctx.journal.close()


def main(argv=None, system=None):
    """Exit 0 success, 1 failed step or failed check, 2 refused before change."""
    args = parser().parse_args(argv)
    try:
        result = run_command(args, system)
    except (Refused, OSError, ValueError, KeyError) as error:
        print("refused: " + str(error), file=sys.stderr)
        return 2
    except Failed as error:
        print("failed: " + str(error), file=sys.stderr)
        return 1
    sys.stdout.write(json.dumps(result, sort_keys=True, indent=1) + "\n")
    return 0 if result.get("ok", True) else 1


if __name__ == "__main__":
    sys.exit(main())
