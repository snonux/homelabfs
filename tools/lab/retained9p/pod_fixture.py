#!/usr/bin/env python3
"""zq3 T7 ordinary pod namespace fixture on Rocky (gap G9, decision A8).

Runs ON ROCKY AS ROOT inside the approved zq3 window, after Phase 8 mounted m0.
It never pulls an image, installs a package, mounts, unmounts, kills or forces
anything. Python 3.9 compatible; the staged file is pinned by SHA256.

Runtime choice (A8): the podman 5.8.2 / crun 1.27 already installed on Rocky,
pinned by NEVRA and SHA256, rootful, VFS storage in fresh run-owned roots. The
rootfs is not an image: it is built from Rocky's own installed files, and each
file must equal the signed RPM database digest. Why: buildah/skopeo are absent,
any registry pull would add a network dependency and unpinned bytes, and
`--rootfs` lets the exact tree be re-verified in place before and after the run.

- `manifest` (read-only) runs pod_workload.py's `closure` under the pinned
  /usr/bin/python3.9 to learn exactly which module and mapped files the workload
  interpreter loads, attests each against the RPM database and prints the
  canonical manifest and its SHA256. Same Rocky install, same digest.
- `build` recomputes the manifest, refuses unless it equals the pinned digest,
  and materialises it as RUN/pod/rootfs (container_file_t labels, root-owned).
- `run` admits the guest (root, SELinux enforcing, exact m0 9p mount, driver in
  the initial namespaces), checks the runtime pins and the rootfs, starts two
  containers (see pod_workload.py), inspects both from outside while they wait
  on a stdin barrier, collects their reports, audit AVC delta and post-run
  rootfs state, and judges everything. Pass removes only the exact payload and
  the two exited containers normally; any failure preserves everything.

A pass is T7 evidence only, never zq3 acceptance (`acceptance: false`).
"""
import argparse
import fcntl
import hashlib
import json
import os
import re
import select
import stat
import subprocess
import sys
import time

VERSION = "homelabfs-retained9p-pod/1"
MANIFEST_VERSION = "homelabfs-retained9p-pod-rootfs/1"
RUN_PARENT = "/var/tmp/hfs-zq3-"  # Same root-private run directory as guest.py.
TRUSTED_UID, TRUSTED_GID = 0, 0
SOURCE_ROOT = ""  # Prefix for reading Rocky's installed files; tests use a mirror.
PYTHON = "/usr/bin/python3.9"
STDLIB = "/usr/lib64/python3.9/"  # Rocky's platlibdir; -I -S keeps site-packages out.
WORKLOAD = "pod_workload.py"
# SHA256 of the sibling pod_workload.py; pinning this file pins the workload.
WORKLOAD_SHA256 = "a35449c6762772d57309ad8639e57ea9ae09b78cd2af94fcc2ee0adec2d54b75"
WORKLOAD_PATH = "/hfs/pod_workload.py"  # Inside the rootfs.
# Rocky facts 2026-11-08: podman/crun NEVRAs are pinned here; every SHA256
# (and conmon/seccomp, not in that fact list) comes from the Phase 1 request.
RUNTIME = {
    "podman": ("/usr/bin/podman", "podman-5.8.2-8.el9_8.x86_64"),
    "crun": ("/usr/bin/crun", "crun-1.27-1.el9_7.x86_64"),
    "conmon": ("/usr/bin/conmon", None),
    "seccomp": ("/usr/share/containers/seccomp.json", None),
}
LABEL = b"system_u:object_r:container_file_t:s0"  # podman's documented --rootfs label.
LEVEL = "s0:c731,c914"  # Fixed MCS pair: a numeric, reviewable container context.
CAPS = ("CHOWN", "FOWNER", "SETGID", "SETUID")  # Matrix setup and ID switching only.
CAP_HEX = "%016x" % ((1 << 0) | (1 << 3) | (1 << 6) | (1 << 7))
MEMORY, PIDS, WORKLOAD_SECONDS, RUN_SECONDS = 256 << 20, 64, 45, 120
NAMESPACES = ("mnt", "pid", "net", "uts", "ipc", "user", "cgroup")
ISOLATED = ("mnt", "pid", "net", "uts", "ipc")
SHARED_IN_POD = ("pid", "net", "uts", "ipc")
INITIAL_MAP = ["0", "0", "4294967295"]  # init_user_ns identity map.
MOUNT_OPTIONS = {"rw", "cache=mmap", "version=9P2000.L.hfs1.4", "msize=65536"}
# Mount points the runtime needs; pre-created so it never writes the pinned tree.
ROOTFS_DIRS = ("/", "/dev", "/etc", "/hfs", "/hfs/m0", "/proc", "/run", "/sys",
               "/tmp", "/usr", "/var", "/var/tmp")
GENERATED = {"/etc/passwd": b"root:x:0:0:root:/:/usr/bin/python3.9\n",
             "/etc/group": b"root:x:0:\n", "/etc/hosts": b"", "/etc/hostname": b"",
             "/etc/resolv.conf": b""}
TOP_LINKS = ("bin", "lib", "lib64", "sbin")  # usrmerge links, e.g. PT_INTERP /lib64.
SONAME = re.compile(r"(lib[A-Za-z0-9_.+-]*|ld-linux-x86-64)\.so\.[0-9][0-9.]*\Z")
HASH = re.compile(r"[0-9a-f]{64}\Z")
RUN_ID = re.compile(r"[a-z0-9][a-z0-9-]{0,23}\Z")
AUDIT_LOG = "/var/log/audit/audit.log"
STREAM_LIMIT, AUDIT_LIMIT, MANIFEST_LIMIT = 256 << 10, 256 << 10, 4 << 20
# TZ/LC_ALL fixed: rpm prints signature dates in local time and locale, which
# would otherwise make the same install yield different manifest digests.
CLEAN_ENV = {"PATH": "/usr/sbin:/usr/bin", "LC_ALL": "C", "TZ": "UTC"}
RPM_FILES = "[%{FILENAMES}\t%{FILEMODES}\t%{FILEDIGESTS}\t%{FILELINKTOS}\n]"
RPM_HEADER = "%{FILEDIGESTALGO}\t%{VENDOR}\t%{RSAHEADER:pgpsig}\n"


class Refused(RuntimeError):
    """Admission, a pin or a required observation refused; nothing was forced."""


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def require(condition, message):
    if not condition:
        raise Refused(message)


def require_hash(value, what):
    require(isinstance(value, str) and HASH.match(value), what + " must be a lowercase SHA256")
    return value


class Host:
    """Every OS boundary the driver crosses besides plain local file I/O.

    Tests substitute a fake to simulate rpm, podman, /proc and SELinux.
    """

    def run(self, argv, timeout, env=None):
        """Bounded short command; a timeout kills only this client process."""
        done = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True,
                              timeout=timeout, env=env or CLEAN_ENV, check=False)
        require(len(done.stdout) <= MANIFEST_LIMIT and len(done.stderr) <= STREAM_LIMIT,
                "command output bound: " + argv[0])
        return done.returncode, done.stdout, done.stderr

    def spawn(self, argv, env):
        return subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, env=env, bufsize=0)

    def read(self, path, limit=65536, offset=0):
        with open(path, "rb") as stream:
            stream.seek(offset)
            raw = stream.read(limit + 1)
        require(len(raw) <= limit, "read bound: " + path)
        return raw

    def readlink(self, path):
        return os.readlink(path)

    def stat(self, path):
        return os.stat(path)

    def hash_file(self, path, limit=256 << 20):
        """SHA256 of a root-owned, not group/other-writable regular file."""
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            require(stat.S_ISREG(info.st_mode) and info.st_size <= limit and
                    info.st_uid == TRUSTED_UID and not info.st_mode & 0o022,
                    "root-owned bounded regular file required: " + path)
            value = hashlib.sha256()
            for chunk in iter(lambda: os.read(fd, 1 << 20), b""):
                value.update(chunk)
            return value.hexdigest()
        finally:
            os.close(fd)

    def get_label(self, path):
        return os.getxattr(path, "security.selinux", follow_symlinks=False).rstrip(b"\0")

    def set_label(self, path, label):
        os.setxattr(path, "security.selinux", label, follow_symlinks=False)


# --- the pinned workload -----------------------------------------------------

def workload_source():
    """Exact bytes of the sibling workload, checked against WORKLOAD_SHA256."""
    path = os.path.join(os.path.dirname(os.path.realpath(__file__)), WORKLOAD)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        raw = os.read(fd, 1 << 20)
    finally:
        os.close(fd)
    require(digest(raw) == WORKLOAD_SHA256, "pod_workload.py differs from its pin")
    return path, raw


def workload_module():
    """Execute exactly the verified bytes: one source for names and the matrix."""
    path, raw = workload_source()
    namespace = {"__name__": "pod_workload", "__file__": path}
    exec(compile(raw, path, "exec"), namespace)
    return namespace


# --- rootfs manifest -----------------------------------------------------------

def source(path):
    return SOURCE_ROOT + path


def closure_paths(host):
    """Files the workload interpreter loads, from its own closure run."""
    path, _ = workload_source()
    code, out, err = host.run([PYTHON, "-I", "-B", "-S", path, "closure"], timeout=30)
    require(code == 0, "closure probe failed: " + err.decode("ascii", "replace")[:512])
    report = json.loads(out)
    require(report.get("executable") == PYTHON, "closure probe used another interpreter")
    files = {PYTHON}
    for name in report["modules"]:
        if name == path:
            continue  # The workload itself goes to WORKLOAD_PATH.
        # -I -S never imports site-packages; seeing one means another interpreter setup.
        require(name.startswith(STDLIB) and "/site-packages/" not in name and
                name.endswith((".py", ".so")),
                "module outside the Rocky python3.9 stdlib: " + name)
        files.add(name)
    for name in report["maps"]:
        if not name.startswith("/") or name.startswith(("/memfd:", "/dev/")):
            continue
        require(name.startswith("/usr/") and not name.endswith(" (deleted)"),
                "mapped file outside /usr: " + name)
        files.add(name)
    for name in files:
        require(os.path.normpath(name) == name, "noncanonical closure path: " + name)
    return files


def link_paths(files):
    """usrmerge top-level links and versioned SONAME links into included libs."""
    links = {"/" + name for name in TOP_LINKS if os.path.islink(source("/" + name))}
    real = {os.path.realpath(source(name)) for name in files}
    for directory in sorted({os.path.dirname(name) for name in files if ".so" in name}):
        with os.scandir(source(directory)) as entries:
            for entry in entries:
                if (entry.is_symlink() and SONAME.match(entry.name) and
                        os.path.realpath(entry.path) in real):
                    links.add(directory + "/" + entry.name)
    return links


def rpm_table(host, paths):
    """RPM database rows (mode, digest, link) for every package owning paths."""
    code, out, err = host.run(["rpm", "-qf", "--qf", "%{NEVRA}\n", "--", *sorted(paths)], 120)
    require(code == 0, "file not owned by an installed package: " +
            (out + err).decode("ascii", "replace")[:512])
    table, packages = {}, []
    for nevra in sorted(set(out.decode().split())):
        code, header, _ = host.run(["rpm", "-q", "--qf", RPM_HEADER, nevra], 60)
        algo, vendor, signature = (header.decode().rstrip("\n").split("\t") + ["", "", ""])[:3]
        require(code == 0 and algo == "8", "package without SHA256 file digests: " + nevra)
        packages.append({"nevra": nevra, "vendor": vendor, "signature": signature})
        code, rows, _ = host.run(["rpm", "-q", "--qf", RPM_FILES, nevra], 60)
        require(code == 0, "rpm file list failed: " + nevra)
        for row in rows.decode().splitlines():
            fields = row.split("\t")
            require(len(fields) == 4, "unexpected rpm row: " + nevra)
            name, mode, sha, target = fields
            if name in paths:
                record = (int(mode), sha, target)
                require(table.setdefault(name, (nevra, record))[1] == record,
                        "packages disagree about " + name)
    return table, packages


def attest(path, table):
    """Manifest entry for one installed path, equal to its RPM database row."""
    require(path in table, "not listed by its owning package: " + path)
    nevra, (mode, sha, target) = table[path]
    info = os.lstat(source(path))
    require(info.st_mode == mode, "mode differs from rpm database: " + path)
    if stat.S_ISLNK(info.st_mode):
        require(os.readlink(source(path)) == target, "link differs from rpm: " + path)
        return {"path": path, "type": "link", "target": target, "origin": "rpm:" + nevra}
    require(stat.S_ISREG(info.st_mode) and not info.st_mode & 0o7022,
            "plain regular file required: " + path)
    raw = read_source(path)
    require(digest(raw) == sha, "content differs from rpm database: " + path)
    return {"path": path, "type": "file", "mode": stat.S_IMODE(info.st_mode),
            "size": len(raw), "sha256": sha, "origin": "rpm:" + nevra}


def read_source(path, limit=64 << 20):
    fd = os.open(source(path), os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        raw = os.read(fd, limit + 1)
        while len(raw) <= limit:
            chunk = os.read(fd, limit + 1 - len(raw))
            if not chunk:
                break
            raw += chunk
    finally:
        os.close(fd)
    require(len(raw) <= limit, "source file bound: " + path)
    return raw


def fixture_entries(paths):
    """Directories, generated /etc files and the workload; never from rpm."""
    entries = [{"path": name, "type": "file", "mode": 0o644, "size": len(raw),
                "sha256": digest(raw), "origin": "fixture"} for name, raw in GENERATED.items()]
    _, raw = workload_source()
    entries.append({"path": WORKLOAD_PATH, "type": "file", "mode": 0o444, "size": len(raw),
                    "sha256": digest(raw), "origin": "fixture"})
    directories = set(ROOTFS_DIRS)
    for name in list(paths) + [entry["path"] for entry in entries]:
        parent = os.path.dirname(name)
        while parent not in directories:
            directories.add(parent)
            parent = os.path.dirname(parent)
    entries += [{"path": name, "type": "dir", "mode": 0o755, "origin": "fixture"}
                for name in directories]
    return entries


def compute_manifest(host):
    files = closure_paths(host)
    paths = files | link_paths(files)
    table, packages = rpm_table(host, paths)
    entries = [attest(path, table) for path in sorted(paths)] + fixture_entries(paths)
    names = [entry["path"] for entry in entries]
    require(len(names) == len(set(names)), "duplicate rootfs path")
    return {"version": MANIFEST_VERSION, "python": PYTHON, "label": LABEL.decode(),
            "workload_sha256": WORKLOAD_SHA256, "packages": packages,
            "entries": sorted(entries, key=lambda entry: entry["path"])}


# --- rootfs tree ---------------------------------------------------------------

def tree_entries(host, root):
    """Walk the built tree without following links: path -> observed entry."""
    found, pending = {}, ["/"]
    while pending:
        name = pending.pop()
        full = root + ("" if name == "/" else name)
        info = os.lstat(full)
        require(info.st_uid == TRUSTED_UID and info.st_gid == TRUSTED_GID,
                "rootfs entry not root-owned: " + name)
        entry = {"path": name, "label": host.get_label(full).decode("ascii", "replace")}
        if stat.S_ISDIR(info.st_mode):
            entry.update(type="dir", mode=stat.S_IMODE(info.st_mode))
            pending += [os.path.join(name, child) for child in os.listdir(full)]
        elif stat.S_ISLNK(info.st_mode):
            entry.update(type="link", target=os.readlink(full))
        elif stat.S_ISREG(info.st_mode):
            with open(full, "rb") as stream:
                raw = stream.read()
            entry.update(type="file", mode=stat.S_IMODE(info.st_mode), size=len(raw),
                         sha256=digest(raw))
        else:
            entry.update(type="other")
        found[name] = entry
    return found


def verify_tree(host, root, manifest):
    """Changed/missing pinned entries, and extras the runtime may have added."""
    found = tree_entries(host, root)
    changed = []
    for entry in manifest["entries"]:
        seen = found.pop(entry["path"], None)
        wanted = {key: value for key, value in entry.items() if key != "origin"}
        if seen is None or seen.pop("label") != manifest["label"] or seen != wanted:
            changed.append(entry["path"])
    extras = [{"path": name, "type": entry["type"], "size": entry.get("size")}
              for name, entry in sorted(found.items())]
    return {"changed": changed, "extras": extras}


def materialise(host, rootfs, entry):
    full = rootfs + ("" if entry["path"] == "/" else entry["path"])
    if entry["type"] == "dir":
        if entry["path"] != "/":
            os.mkdir(full, 0o755)
        os.chmod(full, entry["mode"])
    elif entry["type"] == "link":
        os.symlink(entry["target"], full)
    else:
        raw = entry_bytes(entry)
        require(digest(raw) == entry["sha256"], "source changed since manifest: " + entry["path"])
        fd = os.open(full, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
        try:
            require(os.write(fd, raw) == len(raw), "short rootfs write")
            os.fchmod(fd, entry["mode"])
            os.fsync(fd)
        finally:
            os.close(fd)
    os.lchown(full, TRUSTED_UID, TRUSTED_GID)
    host.set_label(full, LABEL)


def entry_bytes(entry):
    if entry["path"] == WORKLOAD_PATH:
        return workload_source()[1]
    if entry["path"] in GENERATED:
        return GENERATED[entry["path"]]
    return read_source(entry["path"])


def run_directory(run_id, m0_dev=None):
    require(RUN_ID.match(run_id), "run ID bound")
    path = RUN_PARENT + run_id
    info = os.lstat(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == TRUSTED_UID and
            not info.st_mode & 0o077 and info.st_dev != m0_dev,
            "root-private local run directory required: " + path)
    return path


def write_exclusive(path, raw, mode=0o600):
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, mode)
    try:
        require(os.write(fd, raw) == len(raw), "short write: " + path)
        os.fsync(fd)
    finally:
        os.close(fd)


def fsync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def build(host, run_id, expected):
    """Materialise the pinned manifest; the run directory gains RUN/pod only."""
    require(os.geteuid() == TRUSTED_UID, "trusted guest root required")
    run = run_directory(run_id)
    manifest = compute_manifest(host)
    raw = canonical(manifest)
    require(digest(raw) == require_hash(expected, "manifest"),
            "computed rootfs manifest differs from the pinned digest " + digest(raw))
    pod, rootfs = run + "/pod", run + "/pod/rootfs"
    os.mkdir(pod, 0o700)
    os.mkdir(rootfs, 0o755)
    for entry in manifest["entries"]:  # Sorted: parents precede children.
        materialise(host, rootfs, entry)
    write_exclusive(pod + "/rootfs-manifest.json", raw)
    for directory in (rootfs, pod, run):
        fsync_directory(directory)
    state = verify_tree(host, rootfs, manifest)
    require(state == {"changed": [], "extras": []}, "built rootfs differs: " + json.dumps(state))
    return {"version": VERSION, "action": "build", "rootfs": rootfs,
            "manifest_sha256": digest(raw), "entries": len(manifest["entries"])}


# --- guest admission and runtime pins ---------------------------------------

def decode_mount(value):
    return re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), value)


def m0_mount(host, path, request):
    """The exact controlled 9p tuple of m0, as guest.py admits it."""
    rows = []
    for line in host.read("/proc/self/mountinfo", 1 << 20).decode().splitlines():
        left, _, right = line.partition(" - ")
        fields, fs = left.split(), right.split()
        if decode_mount(fields[4]) == path:
            rows.append((int(fields[0]), fs[0], decode_mount(fs[1]),
                         set(fields[5].split(",")) | set(fs[2].split(","))))
    require(len(rows) == 1, "exact m0 mountpoint not present once")
    mount_id, kind, tag, options = rows[0]
    require(mount_id == request["m0"]["mount_id"] and kind == "9p" and
            tag == request["m0"]["tag"] and MOUNT_OPTIONS <= options,
            "exact controlled m0 mount tuple differs")
    return {"mount_id": mount_id, "tag": tag, "dev": host.stat(path).st_dev}


def guest_baseline(host, request):
    """Guest init's namespaces (the driver must share them), map, UTS, SELinux."""
    require(os.geteuid() == TRUSTED_UID and sys.platform == "linux", "trusted Linux guest root required")
    require(host.read("/sys/fs/selinux/enforce", 2).strip() == b"1", "SELinux enforcing required")
    namespaces = {}
    for name in NAMESPACES:
        own, init = host.readlink("/proc/self/ns/" + name), host.readlink("/proc/1/ns/" + name)
        require(own == init, "driver is not in the guest's initial " + name + " namespace")
        namespaces[name] = init
    for name in ("uid_map", "gid_map"):
        require(host.read("/proc/1/" + name).decode().split() == INITIAL_MAP,
                "guest init is not in the initial user namespace map")
    m0 = RUN_PARENT + request["run_id"] + "/m0"
    mount = m0_mount(host, m0, request)
    run_directory(request["run_id"], mount["dev"])
    return {"namespaces": namespaces, "m0": mount, "m0_label": label_or_error(host, m0),
            "hostname": host.read("/proc/sys/kernel/hostname").decode().strip()}


def label_or_error(host, path):
    """Evidence only: whether container_t may use this label is a run-time fact.

    The fixture never relabels m0 (no :z/:Z, which would write xattrs into the
    tested volume) and never disables labeling; a denial fails T7 with AVCs.
    """
    try:
        return host.get_label(path).decode("ascii", "replace")
    except OSError as failure:
        return "unavailable: errno %d" % failure.errno


def check_runtime(host, request):
    """podman/crun/conmon/seccomp bytes and package identities equal their pins."""
    record = {}
    for name, (path, nevra) in sorted(RUNTIME.items()):
        sha = host.hash_file(path)
        require(sha == request["runtime_sha256"][name], name + " SHA256 differs from its pin")
        code, out, _ = host.run(["rpm", "-qf", "--qf", "%{NEVRA}\n", "--", path], 30)
        owners = out.decode().split()
        require(code == 0 and (nevra is None or owners == [nevra]),
                name + " package identity differs: " + " ".join(owners))
        record[name] = {"path": path, "sha256": sha, "packages": owners}
    return record


def check_rootfs(host, request, pod):
    with open(pod + "/rootfs-manifest.json", "rb") as stream:
        raw = stream.read(MANIFEST_LIMIT + 1)
    require(digest(raw) == request["rootfs_manifest_sha256"], "rootfs manifest differs from its pin")
    manifest = json.loads(raw)
    require(manifest["workload_sha256"] == WORKLOAD_SHA256, "rootfs holds another workload")
    state = verify_tree(host, pod + "/rootfs", manifest)
    require(state == {"changed": [], "extras": []}, "rootfs differs before run: " + json.dumps(state))
    return manifest


def parse_request(raw):
    require(len(raw) <= 16384, "request bound")
    request = json.loads(raw)
    require(isinstance(request, dict) and request.get("version") == VERSION and
            set(request) == {"version", "run_id", "fixture_sha256", "rootfs_manifest_sha256",
                             "runtime_sha256", "m0"}, "request shape/version")
    require(isinstance(request["run_id"], str) and RUN_ID.match(request["run_id"]), "run ID bound")
    for key in ("fixture_sha256", "rootfs_manifest_sha256"):
        require_hash(request[key], key)
    require(isinstance(request["runtime_sha256"], dict) and
            set(request["runtime_sha256"]) == set(RUNTIME), "runtime pin set")
    for name, value in request["runtime_sha256"].items():
        require_hash(value, name)
    m0 = request["m0"]
    require(isinstance(m0, dict) and set(m0) == {"tag", "mount_id"} and
            m0["tag"] in ("hfs0",) and type(m0["mount_id"]) is int, "m0 pin shape")
    with open(os.path.realpath(__file__), "rb") as stream:
        require(digest(stream.read()) == request["fixture_sha256"], "pod_fixture.py differs from its pin")
    return request


# --- podman --------------------------------------------------------------------

def container_name(run_id, role):
    return "hfs-zq3-" + run_id + "-" + role


def hostname(run_id):
    return "hfs-zq3-" + run_id + "-pod"


def podman_files(pod):
    """Run-owned config: no system/user overrides, no registries, VFS roots."""
    return {
        "containers.conf": (
            "[containers]\nlog_driver = \"k8s-file\"\nseccomp_profile = \"%s\"\n"
            "userns = \"host\"\nnetns = \"none\"\nipcns = \"private\"\nutsns = \"private\"\n"
            "pidns = \"private\"\ncgroupns = \"private\"\nlabel = true\n"
            "[engine]\ncgroup_manager = \"systemd\"\nevents_logger = \"file\"\n"
            "events_logfile_path = \"%s/events.log\"\nruntime = \"crun\"\n"
            "static_dir = \"%s/storage/libpod\"\ntmp_dir = \"%s/tmp\"\n"
            "volume_path = \"%s/storage/volumes\"\n[engine.runtimes]\ncrun = [\"%s\"]\n"
            % (RUNTIME["seccomp"][0], pod, pod, pod, pod, RUNTIME["crun"][0])),
        "storage.conf": ("[storage]\ndriver = \"vfs\"\ngraphroot = \"%s/storage\"\n"
                         "runroot = \"%s/runroot\"\n" % (pod, pod)),
        "registries.conf": "unqualified-search-registries = []\n",
    }


def podman_env(pod):
    return {**CLEAN_ENV, "HOME": pod + "/home", "XDG_CONFIG_HOME": pod + "/home",
            "TMPDIR": pod + "/tmp", "CONTAINERS_CONF": pod + "/containers.conf",
            "CONTAINERS_STORAGE_CONF": pod + "/storage.conf",
            "CONTAINERS_REGISTRIES_CONF": pod + "/registries.conf"}


def prepare_podman(pod):
    for name in ("home", "tmp", "evidence"):
        os.mkdir(pod + "/" + name, 0o700)
    for name, text in podman_files(pod).items():
        write_exclusive(pod + "/" + name, text.encode())


def global_args(pod):
    return [RUNTIME["podman"][0], "--root", pod + "/storage", "--runroot", pod + "/runroot",
            "--storage-driver", "vfs", "--runtime", RUNTIME["crun"][0],
            "--conmon", RUNTIME["conmon"][0], "--cgroup-manager", "systemd",
            "--events-backend", "file", "--tmpdir", pod + "/tmp", "--log-level", "warn"]


def workload_params(run_id, role, version):
    return json.dumps({"version": version, "run_id": run_id, "role": role,
                       "seconds": WORKLOAD_SECONDS}, sort_keys=True, separators=(",", ":"))


def run_args(pod, run_id, role, version):
    """Exact `podman run` argv. Never privileged, never label=disable, no :z."""
    name, peer = container_name(run_id, role), container_name(run_id, "w0")
    args = global_args(pod) + [
        "run", "--interactive", "--name", name, "--rootfs", pod + "/rootfs",
        "--read-only", "--read-only-tmpfs=true", "--userns=host", "--user=0:0",
        "--passwd=false", "--cgroupns=private", "--cap-drop=all"]
    args += ["--cap-add=" + cap for cap in CAPS]
    args += ["--security-opt=no-new-privileges", "--security-opt=seccomp=" + RUNTIME["seccomp"][0],
             "--security-opt=label=level:" + LEVEL, "--memory=%d" % MEMORY,
             "--memory-swap=%d" % MEMORY, "--pids-limit=%d" % PIDS,
             "--mount=type=bind,source=%s/m0,destination=/hfs/m0,bind-propagation=rprivate"
             % os.path.dirname(pod),
             "--log-driver=k8s-file", "--log-opt=path=%s/%s.log" % (pod, name),
             # Same locale as the closure probe, so the same files get loaded.
             "--env=LC_ALL=C", "--workdir=/"]
    if role == "w0":
        args += ["--network=none", "--ipc=private", "--uts=private", "--pid=private",
                 "--hostname=" + hostname(run_id), "--no-hosts", "--dns=none"]
    else:
        args += ["--%s=container:%s" % (kind, peer) for kind in ("network", "ipc", "uts", "pid")]
    return args + [PYTHON, "-I", "-B", "-S", WORKLOAD_PATH, "workload",
                   workload_params(run_id, role, version)]


# --- orchestration ---------------------------------------------------------------

class Workload:
    """One attached `podman run` client: bounded stdout/stderr and a barrier."""

    def __init__(self, host, role, argv, env):
        self.role, self.process = role, host.spawn(argv, env)
        self.streams = {self.process.stdout.fileno(): bytearray(),
                        self.process.stderr.fileno(): bytearray()}
        self.open, self.overflow = set(self.streams), False

    def stdout(self):
        return bytes(self.streams[self.process.stdout.fileno()])

    def stderr(self):
        return bytes(self.streams[self.process.stderr.fileno()])

    def events(self):
        parsed = []
        for line in self.stdout().split(b"\n")[:-1]:
            try:
                parsed.append(json.loads(line))
            except ValueError:
                parsed.append({"event": "unparsed", "line": line[:256].decode("ascii", "replace")})
        return parsed

    def has(self, kind):
        return any(event.get("event") == kind for event in self.events())

    def release(self):
        """After outside inspection: one 'observed' line, then stdin EOF."""
        self.process.stdin.write(b"observed\n")
        self.process.stdin.close()

    def finished(self):
        return not self.open and self.process.poll() is not None


def pump(workloads, timeout):
    readable = {fd: item for item in workloads for fd in item.open}
    if not readable:
        time.sleep(min(timeout, 0.05))
        return
    for fd in select.select(list(readable), [], [], timeout)[0]:
        item, chunk = readable[fd], os.read(fd, 65536)
        if not chunk:
            item.open.discard(fd)
            continue
        buffer = item.streams[fd]
        room = STREAM_LIMIT - len(buffer)
        # Overflow is recorded, never silently truncated into a pass.
        item.overflow = item.overflow or len(chunk) > room
        buffer += chunk[:max(0, room)]


def wait_until(workloads, predicate, deadline, what):
    while not predicate():
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise Refused("uncertain: timed out waiting for " + what + "; resources preserved")
        pump(workloads, min(remaining, 0.2))


def cgroup_limits(host, relative):
    """Nearest limiting value of each control on the container's cgroup path."""
    limits = {}
    for control in ("memory.max", "memory.swap.max", "pids.max"):
        path = relative
        while True:
            value = host.read("/sys/fs/cgroup" + path.rstrip("/") + "/" + control, 64).decode().strip()
            if value != "max" or path in ("", "/"):
                limits[control] = {"value": value, "cgroup": path}
                break
            path = os.path.dirname(path.rstrip("/")) or "/"
    return limits


def observe(host, pod, name, env):
    """Outside view of a running container's init process, from guest root."""
    code, out, err = host.run(global_args(pod) + ["inspect", "--format", "{{.State.Pid}}", name],
                              30, env)
    require(code == 0 and out.strip().isdigit(), "podman inspect failed: " + err.decode()[:256])
    pid = int(out)
    proc = "/proc/%d/" % pid
    status = {}
    for line in host.read(proc + "status").decode().splitlines():
        key, _, value = line.partition(":")
        status[key] = value.split()
    cgroup = host.read(proc + "cgroup").decode().strip()
    require(cgroup.startswith("0::"), "cgroup v2 single hierarchy required")
    return {"pid": pid, "namespaces": {n: host.readlink(proc + "ns/" + n) for n in NAMESPACES},
            "uid_map": host.read(proc + "uid_map").decode().split(),
            "gid_map": host.read(proc + "gid_map").decode().split(),
            "status": {key: status.get(key) for key in ("Uid", "Gid", "Groups", "CapEff",
                                                         "CapPrm", "CapBnd", "CapAmb",
                                                         "NoNewPrivs", "Seccomp")},
            "selinux": host.read(proc + "attr/current").decode().rstrip("\0\n"),
            "cgroup": cgroup[3:], "limits": cgroup_limits(host, cgroup[3:])}


def start_and_observe(host, pod, run_id, role, version, env, workloads, deadline):
    item = Workload(host, role, run_args(pod, run_id, role, version), env)
    workloads.append(item)
    wait_until(workloads, lambda: item.has("ready") or item.finished(), deadline, role + " ready")
    require(item.has("ready"), role + " exited before ready")
    outside = observe(host, pod, container_name(run_id, role), env)
    item.release()
    return item, outside


def run_containers(host, request, pod, version):
    """w0, then w1 joined to it; every partial result is kept for evidence."""
    env, workloads, outside = podman_env(pod), [], {}
    deadline = time.monotonic() + RUN_SECONDS
    error = None
    try:
        for role in ("w0", "w1"):
            outside[role] = start_and_observe(host, pod, request["run_id"], role, version,
                                              env, workloads, deadline)[1]
        wait_until(workloads, lambda: all(item.finished() for item in workloads),
                   deadline, "both workloads to exit")
    except (Refused, OSError, ValueError) as failure:
        # Never kill: the containers stay for inspection, the error is evidence.
        error = type(failure).__name__ + ": " + str(failure)
    return workloads, outside, error


# --- audit ----------------------------------------------------------------------

def audit_mark(host):
    info = host.stat(AUDIT_LOG)
    return {"ino": info.st_ino, "size": info.st_size}


def audit_delta(host, mark):
    """AVC lines appended during the run; rotation or overflow is uncertain."""
    info = host.stat(AUDIT_LOG)
    if info.st_ino != mark["ino"] or info.st_size < mark["size"]:
        return None
    if info.st_size - mark["size"] > AUDIT_LIMIT:
        return None
    raw = host.read(AUDIT_LOG, AUDIT_LIMIT, mark["size"]).decode("ascii", "replace")
    return [line for line in raw.splitlines() if "avc:" in line and "denied" in line]


# --- judgement -------------------------------------------------------------------

def single_event(events, kind):
    found = [event for event in events if event.get("event") == kind]
    return found[0] if len(found) == 1 else None


def check_status(role, status):
    """Container root: numeric 0/0, no foreign groups, exact caps, NNP, seccomp."""
    expected = {"Uid": ["0"] * 4, "Gid": ["0"] * 4, "CapEff": [CAP_HEX], "CapPrm": [CAP_HEX],
                "CapBnd": [CAP_HEX], "CapAmb": ["0" * 16], "NoNewPrivs": ["1"], "Seccomp": ["2"]}
    failures = [role + ": " + key + " is " + str(status.get(key)) + ", expected " + str(value)
                for key, value in expected.items() if status.get(key) != value]
    if not set(status.get("Groups") or []) <= {"0"}:
        failures.append(role + ": supplementary groups beyond 0: " + str(status.get("Groups")))
    return failures


def check_label(role, context):
    parts = context.split(":", 3)
    if len(parts) != 4 or parts[2] != "container_t" or parts[3] != LEVEL:
        return [role + ": SELinux context " + context + " is not container_t:" + LEVEL]
    return []


def check_namespaces(role, baseline, outside, ident):
    failures = []
    if outside["namespaces"] != ident["namespaces"]:
        failures.append(role + ": outside and self-reported namespaces differ")
    for name in ISOLATED:
        if ident["namespaces"][name] == baseline["namespaces"][name]:
            failures.append(role + ": " + name + " namespace not separated from the guest")
    if ident["namespaces"]["user"] != baseline["namespaces"]["user"]:
        failures.append(role + ": user namespace is not the guest's initial user namespace")
    for key in ("uid_map", "gid_map"):
        if ident[key] != INITIAL_MAP or outside[key] != INITIAL_MAP:
            failures.append(role + ": " + key + " is not the initial identity map")
    return failures


def check_environment(role, run_id, baseline, outside, ident):
    """Loopback-only netns, pod hostname, one bind of m0, cgroup limits."""
    failures = []
    if ident["links"] != ["lo"]:
        failures.append(role + ": network links are " + str(ident["links"]) + ", expected only lo")
    if ident["hostname"] != hostname(run_id) or ident["hostname"] == baseline["hostname"]:
        failures.append(role + ": UTS hostname " + ident["hostname"] + " is not the pod's own")
    mounts = ident.get("mounts") or []
    if (len(mounts) != 1 or mounts[0]["fstype"] != "9p" or
            mounts[0]["source"] != baseline["m0"]["tag"] or
            ident.get("mount_dev") != baseline["m0"]["dev"]):
        failures.append(role + ": /hfs/m0 is not exactly one bind of the guest m0 9p mount")
    wanted = {"memory.max": str(MEMORY), "memory.swap.max": "0", "pids.max": str(PIDS)}
    for control, value in wanted.items():
        if outside["limits"][control]["value"] != value:
            failures.append(role + ": " + control + " is " + outside["limits"][control]["value"])
    return failures


def check_workload(role, run_id, baseline, outside, ident):
    if ident is None or outside is None:
        return [role + ": identity report or outside observation missing"]
    return (check_namespaces(role, baseline, outside, ident) +
            check_status(role + " (self)", ident["status"]) +
            check_status(role + " (outside)", outside["status"]) +
            check_label(role + " (self)", ident["selinux"]) +
            check_label(role + " (outside)", outside["selinux"]) +
            check_environment(role, run_id, baseline, outside, ident))


def check_pod(outside):
    if set(outside) != {"w0", "w1"}:
        return ["pod: both workloads were not observed"]
    w0, w1 = outside["w0"]["namespaces"], outside["w1"]["namespaces"]
    failures = ["pod: w1 does not share w0's " + name + " namespace"
                for name in SHARED_IN_POD if w0[name] != w1[name]]
    if w0["mnt"] == w1["mnt"]:
        failures.append("pod: w0 and w1 share one mount namespace")
    return failures


def check_matrix(w, matrix):
    """Every numeric ID/full-group case allows or denies exactly as declared."""
    cases = (matrix or {}).get("cases") or []
    names = [case[0] for case in w["MATRIX"]] + ["changing-groups-fsuid"]
    if [case.get("case") for case in cases] != names:
        return ["matrix: case list incomplete or reordered"]
    failures = []
    for declared, case in zip(w["MATRIX"], cases):
        name, filename, uid, gid, groups, allow = declared
        result = case["reply"].get("result") or {}
        if (case["status"] != 0 or not case["reply"].get("ok") or
                [result.get("uid"), result.get("gid"), result.get("groups")] != [uid, gid, groups]):
            failures.append("matrix: " + name + " did not run with exact numeric IDs/groups")
        elif (result["errno"], result.get("read")) != ((0, filename) if allow else (w["DENIED"][0], None)):
            failures.append("matrix: " + name + " expected " + ("allow" if allow else "EACCES") +
                            ", observed errno " + str(result["errno"]))
    return failures + check_changing(w, cases[-1])


def check_changing(w, case):
    steps = case["reply"].get("result") or []
    if case["status"] != 0 or len(steps) != len(w["CHANGING"]):
        return ["matrix: changing-groups-fsuid did not complete"]
    failures = []
    for (groups, allow), step in zip(w["CHANGING"], steps):
        if [step["groups"], step["fsuid"], step["fsgid"]] != [groups, 41001, 42001]:
            failures.append("matrix: changing groups ran with other fsuid/fsgid/groups")
        elif step["errno"] != (0 if allow else w["DENIED"][0]):
            failures.append("matrix: changing groups " + str(groups) + " observed errno " +
                            str(step["errno"]))
    return failures


def check_locks(w, baseline, idents, locks):
    """Same inode via two binds, GETLK owner, conflicts, waits, wakeups, exit release."""
    if None in (idents.get("w0"), idents.get("w1"), locks.get("w0"), locks.get("w1")):
        return ["locks: lock reports missing"]
    held, contender = locks["w0"], locks["w1"]
    w0_pid = idents["w0"]["pid"]
    failures = []
    same = [{"dev": baseline["m0"]["dev"], "ino": item["ino"]} for item in held["identities"]]
    if contender["identities"] != held["identities"] or held["identities"] != same:
        failures.append("locks: workloads did not open the same m0 inodes")
    if contender["held"]["pid"] != w0_pid or held["peer_pid"] != idents["w1"]["pid"]:
        failures.append("locks: pod PID namespace does not show consistent peer PIDs")
    if contender["getlk"] != {"type": fcntl.F_WRLCK, "whence": 0, "start": 0, "length": 1,
                              "pid": w0_pid}:
        failures.append("locks: GETLK did not report w0's write lock: " + str(contender["getlk"]))
    if any(code not in w["CONFLICT"] for code in contender["nonblocking"]):
        failures.append("locks: nonblocking conflict errnos " + str(contender["nonblocking"]))
    for family in ("posix", "flock"):
        if not held.get(family + "_waiter"):
            failures.append("locks: no kernel blocked-waiter evidence for " + family)
        if contender[family]["acquired_ns"] < held[family + "_released_ns"]:
            failures.append("locks: " + family + " acquired before w0 released it")
        if held["after_peer_exit"][family]["errno"] != 0:
            failures.append("locks: w1's " + family + " lock survived its exit")
    return failures


def check_run(statuses, workloads, error, audit, after):
    failures = [] if error is None else [error]
    for item in workloads:
        terminal = single_event(item.events(), "terminal")
        if statuses.get(item.role) != 0 or not terminal or terminal.get("success") is not True:
            failures.append(item.role + ": workload did not exit successfully")
        if item.overflow:
            failures.append(item.role + ": output exceeded its bound; evidence incomplete")
    if audit is None:
        failures.append("audit: AVC evidence incomplete (rotated, unreadable or oversized)")
    else:
        failures += ["audit: SELinux denial in the pod: " + line[:256] for line in audit
                     if "container_t" in line or LEVEL.split(":", 1)[1] in line]
    if not after["enforcing"]:
        failures.append("selinux: not enforcing after the run")
    failures += ["rootfs: pinned entry changed: " + name for name in after["rootfs"]["changed"]]
    failures += ["rootfs: runtime added nonempty or special " + extra["path"]
                 for extra in after["rootfs"]["extras"]
                 if extra["type"] not in ("dir", "file") or extra["size"]]
    return failures


def evaluate(w, run_id, baseline, outside, workloads, error, audit, after):
    statuses = {item.role: item.process.poll() for item in workloads}
    events = {item.role: item.events() for item in workloads}
    idents = {role: single_event(events.get(role, []), "identity") for role in ("w0", "w1")}
    locks = {role: single_event(events.get(role, []), "locks") for role in ("w0", "w1")}
    failures = check_run(statuses, workloads, error, audit, after)
    for role in ("w0", "w1"):
        failures += check_workload(role, run_id, baseline, outside.get(role), idents[role])
    failures += check_pod(outside)
    failures += check_matrix(w, single_event(events.get("w0", []), "matrix"))
    failures += check_locks(w, baseline, idents, locks)
    return failures


# --- run, evidence and cleanup -----------------------------------------------

def persist(pod, name, value):
    raw = value if isinstance(value, bytes) else canonical(value)
    write_exclusive(pod + "/evidence/" + name, raw)


def cleanup(host, pod, run_id, w):
    """After a pass only: normal `podman rm` of the exited pair, exact payload."""
    env = podman_env(pod)
    for role in ("w1", "w0"):
        code, _, err = host.run(global_args(pod) + ["rm", container_name(run_id, role)], 60, env)
        require(code == 0, "podman rm " + role + " failed: " + err.decode()[:256])
    leaf, _ = w["names"](run_id)
    expected = sorted(list(w["FILES"]) + list(w["LOCK_FILES"]))
    root = os.open(RUN_PARENT + run_id + "/m0", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        fd = os.open(leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root)
        try:
            require(sorted(os.listdir(fd)) == expected, "pod payload listing changed; preserved")
            for name in expected:
                os.unlink(name, dir_fd=fd)
        finally:
            os.close(fd)
        os.rmdir(leaf, dir_fd=root)
    finally:
        os.close(root)
    return {"containers_removed": True, "payload_removed": expected}


def run(host, request_raw):
    request = parse_request(request_raw)
    w = workload_module()
    baseline = guest_baseline(host, request)
    runtime = check_runtime(host, request)
    pod = RUN_PARENT + request["run_id"] + "/pod"
    manifest = check_rootfs(host, request, pod)
    mark = audit_mark(host)  # Before any write: no audit log refuses cleanly.
    prepare_podman(pod)  # Exclusive: a second run in this pod directory refuses.
    persist(pod, "request.json", request_raw)
    workloads, outside, error = run_containers(host, request, pod, w["VERSION"])
    audit = audit_delta(host, mark)
    after = {"enforcing": host.read("/sys/fs/selinux/enforce", 2).strip() == b"1",
             "rootfs": verify_tree(host, pod + "/rootfs", manifest)}
    try:
        failures = evaluate(w, request["run_id"], baseline, outside, workloads, error, audit, after)
    except (KeyError, TypeError, IndexError, AttributeError) as failure:
        # A malformed report is a failed observation; its evidence still persists.
        failures = (["malformed workload report: " + type(failure).__name__ + " " + str(failure)] +
                    ([error] if error else []))
    for item in workloads:
        persist(pod, item.role + ".stdout", item.stdout())
        persist(pod, item.role + ".stderr", item.stderr())
    verdict = {"version": VERSION, "action": "run", "run_id": request["run_id"],
               "t7_pass": not failures, "failures": failures, "baseline": baseline,
               "runtime": runtime, "outside": outside, "audit_avc": audit, "after": after,
               "acceptance": False, "publication": None}
    persist(pod, "verdict.json", verdict)
    if not failures:
        try:
            verdict["cleanup"] = cleanup(host, pod, request["run_id"], w)
        except (OSError, Refused) as failure:  # Reported apart from the T7 result.
            verdict["cleanup"] = {"error": str(failure)}
        persist(pod, "cleanup.json", verdict["cleanup"])
    return verdict


def manifest_report(host):
    manifest = compute_manifest(host)
    return {"version": VERSION, "action": "manifest",
            "manifest_sha256": digest(canonical(manifest)), "manifest": manifest}


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("manifest", help="read-only: print the rootfs manifest and SHA256")
    built = commands.add_parser("build", help="materialise the pinned rootfs in RUN/pod")
    built.add_argument("--run-id", required=True)
    built.add_argument("--expect-manifest-sha256", required=True)
    ran = commands.add_parser("run", help="run the T7 pod fixture from a request file")
    ran.add_argument("--request", required=True)
    return parser.parse_args(argv)


def main(argv=None, host=None):
    args, host = parse_args(argv), host or Host()
    try:
        if args.command == "manifest":
            result = manifest_report(host)
        elif args.command == "build":
            result = build(host, args.run_id, args.expect_manifest_sha256)
        else:
            require(os.path.isabs(args.request), "absolute request path required")
            with open(args.request, "rb") as stream:
                result = run(host, stream.read(16385))
    except (Refused, OSError, ValueError, KeyError, subprocess.SubprocessError) as failure:
        print(canonical({"version": VERSION, "action": args.command, "refused": True,
                         "error": type(failure).__name__, "detail": str(failure)[:1024]}).decode(),
              end="")
        return 2
    print(canonical(result).decode(), end="")
    return 0 if result.get("t7_pass", True) else 1


if __name__ == "__main__":
    sys.exit(main())
