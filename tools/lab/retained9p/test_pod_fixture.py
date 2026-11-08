#!/usr/bin/env python3
"""Local tests of the zq3 T7 pod fixture (G9); no podman, SSH, root or lab.

OS boundaries are simulated explicitly. FakeHost answers rpm, `podman inspect`
and `podman rm`, the guest's /proc and /sys files and SELinux labels. The
"containers" are local Python processes that replay canned workload events
through real pipes and wait on the real stdin barrier, so the driver's
orchestration, bounds and evidence handling run unchanged. Rocky's installed
files are a small mirror directory with a fake RPM database.

Real local behaviour, where it is possible without privileges: the workload's
lock protocol runs between two local processes (real kernel POSIX/flock locks
and /proc/locks waiters on the local filesystem, not 9p), and optional tests
read this workstation's real RPM database read-only and start the built
rootfs's interpreter in an unprivileged user-namespace chroot.
"""
import ast
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import textwrap
import time
import types
import unittest
from unittest import mock

from test_preparation import module

pod = module("pod_fixture")
workload = module("pod_workload")
HERE = Path(__file__).resolve().parent
DEV = 0xBEEF  # Fake st_dev of the guest m0 9p mount.
GUEST_NS = {name: "%s:[40265%02d]" % (name, index) for index, name in enumerate(pod.NAMESPACES)}
CONTEXT = "system_u:system_r:container_t:" + pod.LEVEL
ROCKY = "Rocky Enterprise Software Foundation"


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def namespaces(role):
    """w0 owns new namespaces; w1 joins its pid/net/uts/ipc; both keep the user ns."""
    own = {name: "%s:[5%s%02d]" % (name, role[1], index) for index, name in enumerate(pod.NAMESPACES)}
    own["user"] = GUEST_NS["user"]
    if role == "w1":
        for name in pod.SHARED_IN_POD:
            own[name] = "%s:[50%02d]" % (name, pod.NAMESPACES.index(name))
    else:
        for name in pod.SHARED_IN_POD:
            own[name] = "%s:[50%02d]" % (name, pod.NAMESPACES.index(name))
        own["mnt"] = "mnt:[5000]"
    return own


def good_status():
    return {"Uid": ["0"] * 4, "Gid": ["0"] * 4, "Groups": ["0"], "CapEff": [pod.CAP_HEX],
            "CapPrm": [pod.CAP_HEX], "CapBnd": [pod.CAP_HEX], "CapAmb": ["0" * 16],
            "NoNewPrivs": ["1"], "Seccomp": ["2"]}


def identity_event(role):
    return {"version": workload.VERSION, "event": "identity", "pid": 1 if role == "w0" else 7,
            "namespaces": namespaces(role), "uid_map": list(pod.INITIAL_MAP),
            "gid_map": list(pod.INITIAL_MAP), "status": good_status(), "selinux": CONTEXT,
            "hostname": pod.hostname("r1"), "links": ["lo"], "link_flags": {"lo": "0x9"},
            "mounts": [{"mount_id": 77, "root": "/", "fstype": "9p", "source": "hfs0"}],
            "mount_dev": DEV}


def outside_view(role):
    scope = "/machine.slice/libpod-%s.scope" % role
    return {"pid": 4242 if role == "w0" else 4343, "namespaces": namespaces(role),
            "uid_map": list(pod.INITIAL_MAP), "gid_map": list(pod.INITIAL_MAP),
            "status": good_status(), "selinux": CONTEXT, "cgroup": scope + "/container",
            "limits": {"memory.max": {"value": str(pod.MEMORY), "cgroup": scope},
                       "memory.swap.max": {"value": "0", "cgroup": scope},
                       "pids.max": {"value": str(pod.PIDS), "cgroup": scope}}}


def matrix_event():
    cases = []
    for name, filename, uid, gid, groups, allow in workload.MATRIX:
        result = {"errno": 0, "read": filename} if allow else {"errno": 13}
        result.update(uid=uid, gid=gid, groups=groups)
        cases.append({"case": name, "file": filename, "uid": uid, "gid": gid, "groups": groups,
                      "expected_allow": allow, "status": 0, "reply": {"ok": True, "result": result}})
    steps = [dict({"groups": groups, "expected_allow": allow, "fsuid": 41001, "fsgid": 42001,
                   "errno": 0 if allow else 13}, **({"read": "group"} if allow else {}))
             for groups, allow in workload.CHANGING]
    cases.append({"case": "changing-groups-fsuid", "status": 0, "reply": {"ok": True, "result": steps}})
    return {"version": workload.VERSION, "event": "matrix", "cases": cases}


def lock_events():
    idents = [{"dev": DEV, "ino": 11}, {"dev": DEV, "ino": 12}]
    held = {"kind": "held", "pid": 1, "posix": idents[0], "flock": idents[1]}
    w0 = {"event": "locks", "peer_pid": 7, "identities": idents, "contended": {},
          "posix_waiter": "1: -> POSIX ADVISORY WRITE 7 00:be:11 0 0", "posix_released_ns": 150,
          "posix_acquired": {}, "flock_waiter": "2: -> POSIX ADVISORY WRITE 7 00:be:12 0 EOF",
          "flock_released_ns": 350, "flock_acquired": {},
          "after_peer_exit": {"posix": {"errno": 0, "attempts": 1},
                              "flock": {"errno": 0, "attempts": 2}}}
    w1 = {"event": "locks", "held": held, "identities": copy.deepcopy(idents),
          "getlk": {"type": 1, "whence": 0, "start": 0, "length": 1, "pid": 1},
          "nonblocking": [11, 11], "posix": {"wait_started_ns": 100, "acquired_ns": 200},
          "flock": {"wait_started_ns": 300, "acquired_ns": 400}}
    return w0, w1


def scenario():
    w0_locks, w1_locks = lock_events()
    events = {
        "w0": [identity_event("w0"), {"event": "ready"}, matrix_event(), w0_locks,
               {"event": "terminal", "success": True, "acceptance": False}],
        "w1": [identity_event("w1"), {"event": "ready"}, w1_locks,
               {"event": "terminal", "success": True, "acceptance": False}],
    }
    return {"baseline": {"namespaces": dict(GUEST_NS), "hostname": "rocky",
                         "m0": {"mount_id": 77, "tag": "hfs0", "dev": DEV}},
            "outside": {"w0": outside_view("w0"), "w1": outside_view("w1")},
            "events": events, "statuses": {"w0": 0, "w1": 0}, "error": None, "audit": [],
            "after": {"enforcing": True, "rootfs": {"changed": [], "extras": []}}}


def fake_workloads(case):
    return [types.SimpleNamespace(role=role, events=lambda role=role: case["events"][role],
                                  process=types.SimpleNamespace(poll=lambda role=role: case["statuses"][role]),
                                  overflow=False)
            for role in ("w0", "w1")]


def judge(case):
    return pod.evaluate(pod.workload_module(), "r1", case["baseline"], case["outside"],
                        fake_workloads(case), case["error"], case["audit"], case["after"])


def find(case, role, kind):
    return next(event for event in case["events"][role] if event.get("event") == kind)


class FakeHost(pod.Host):
    """Simulated OS boundary: commands, /proc and /sys files, links, labels."""

    def __init__(self):
        self.files, self.links, self.labels = {}, {}, {}
        self.hashes, self.stats, self.handlers = {}, {}, []
        self.calls, self.spawned, self.spawner = [], [], None

    def run(self, argv, timeout, env=None):
        self.calls.append(list(argv))
        for handler in self.handlers:
            result = handler(list(argv))
            if result is not None:
                return result
        raise AssertionError("unexpected command: " + " ".join(argv[:6]))

    def spawn(self, argv, env):
        process = self.spawner(list(argv), env)
        self.spawned.append(process)
        return process

    def read(self, path, limit=65536, offset=0):
        if path in self.files:
            return self.files[path][offset:offset + limit]
        return super().read(path, limit, offset)

    def readlink(self, path):
        return self.links[path] if path in self.links else os.readlink(path)

    def stat(self, path):
        return self.stats[path] if path in self.stats else os.stat(path)

    def hash_file(self, path, limit=0):
        return self.hashes[path]

    def get_label(self, path):
        return self.labels.get(path, b"unlabeled")

    def set_label(self, path, label):
        self.labels[path] = label


class LabelHost(pod.Host):
    """Real commands and files; only SELinux labels are simulated (non-root)."""

    def __init__(self):
        self.labels = {}

    def get_label(self, path):
        return self.labels.get(path, b"unlabeled")

    def set_label(self, path, label):
        self.labels[path] = label


class Mirror:
    """Rocky-like installed files plus a fake RPM database captured at creation."""

    FILES = {"/usr/bin/python3.9": b"\x7fELF python3.9", "/usr/lib64/libc.so.6": b"libc",
             "/usr/lib64/ld-linux-x86-64.so.2": b"ld.so", "/usr/lib64/libffi.so.8.1.0": b"ffi",
             "/usr/lib64/python3.9/os.py": b"# os\n",
             "/usr/lib64/python3.9/lib-dynload/_ctypes.cpython-39-x86_64-linux-gnu.so": b"ct"}
    LINKS = {"/lib64": "usr/lib64", "/usr/lib64/libffi.so.8": "libffi.so.8.1.0",
             "/usr/lib64/libffi.so": "libffi.so.8.1.0"}  # Unversioned devel link: excluded.

    def __init__(self, root):
        self.src = str(root / "src")
        for name, raw in self.FILES.items():
            Path(self.src + name).parent.mkdir(parents=True, exist_ok=True)
            Path(self.src + name).write_bytes(raw)
            os.chmod(self.src + name, 0o755 if name.endswith(("python3.9", ".so", ".so.6", ".so.2", ".so.8.1.0")) else 0o644)
        for name, target in self.LINKS.items():
            os.symlink(target, self.src + name)
        self.owner = {name: ("python3-3.9.25-3.el9_8.x86_64" if "python" in name else
                             "filesystem-3.16-5.el9.x86_64" if name == "/lib64" else
                             "libffi-3.4.2-8.el9.x86_64" if "ffi" in name else
                             "glibc-2.34-231.el9_8.x86_64")
                      for name in list(self.FILES) + list(self.LINKS)}
        self.db = {name: self.row(name) for name in self.owner}
        self.algo, self.signature = "8", "RSA/SHA256, Key ID 702d426d350d275d"
        self.maps = ["/usr/bin/python3.9", "/usr/lib64/ld-linux-x86-64.so.2", "/usr/lib64/libc.so.6",
                     "/usr/lib64/libffi.so.8.1.0",
                     "/usr/lib64/python3.9/lib-dynload/_ctypes.cpython-39-x86_64-linux-gnu.so",
                     "/memfd:hfs-closure (deleted)", "/dev/zero", "[heap]"]
        self.modules = ["/usr/lib64/python3.9/os.py"]
        self.executable = pod.PYTHON

    def row(self, name):
        info = os.lstat(self.src + name)
        if stat.S_ISLNK(info.st_mode):
            return (info.st_mode, "", os.readlink(self.src + name))
        return (info.st_mode, sha(self.src + name), "")

    def handler(self, argv):
        if argv[0] == pod.PYTHON and argv[-1] == "closure":
            report = {"executable": self.executable, "modules": self.modules + [argv[-2]],
                      "maps": self.maps, "version": workload.VERSION}
            return 0, json.dumps(report).encode(), b""
        if argv[:3] == ["rpm", "-qf", "--qf"] and argv[4] == "--":
            missing = [name for name in argv[5:] if name not in self.owner]
            if missing:
                return 1, b"", ("file %s is not owned by any package\n" % missing[0]).encode()
            return 0, "".join(self.owner[name] + "\n" for name in argv[5:]).encode(), b""
        if argv[:3] == ["rpm", "-q", "--qf"] and argv[3] == pod.RPM_HEADER:
            return 0, ("%s\t%s\t%s\n" % (self.algo, ROCKY, self.signature)).encode(), b""
        if argv[:3] == ["rpm", "-q", "--qf"] and argv[3] == pod.RPM_FILES:
            rows = ["%s\t%d\t%s\t%s\n" % (name, *self.db[name]) for name, nevra in
                    sorted(self.owner.items()) if nevra == argv[4]]
            return 0, "".join(rows).encode(), b""
        return None


class Sandbox(unittest.TestCase):
    """Temporary run parent, trusted IDs = the test user, mirror and fake host."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.mirror = Mirror(self.root)
        self.run_dir = self.root / "hfs-zq3-r1"
        self.run_dir.mkdir(mode=0o700)
        (self.run_dir / "m0").mkdir()
        for name, value in [("RUN_PARENT", str(self.root / "hfs-zq3-")), ("SOURCE_ROOT", self.mirror.src),
                            ("TRUSTED_UID", os.getuid()), ("TRUSTED_GID", os.getgid()),
                            ("AUDIT_LOG", str(self.root / "audit.log"))]:
            patcher = mock.patch.object(pod, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        (self.root / "audit.log").write_text("type=DAEMON_START msg=audit(1.0:1): start\n")
        self.host = FakeHost()
        self.host.handlers.append(self.mirror.handler)
        self.addCleanup(self.reap)

    def reap(self):
        for process in self.host.spawned:  # Local fakes only; never a real container.
            if process.poll() is None:
                process.kill()
            process.wait()
            for stream in (process.stdin, process.stdout, process.stderr):
                if stream and not stream.closed:
                    stream.close()

    def refuses(self, text, function, *args):
        with self.assertRaises(pod.Refused) as caught:
            function(*args)
        self.assertIn(text, str(caught.exception))

    def built(self):
        manifest = pod.compute_manifest(self.host)
        expected = pod.digest(pod.canonical(manifest))
        result = pod.build(self.host, "r1", expected)
        return manifest, result


class PinTests(unittest.TestCase):
    def test_workload_pin_python39_syntax_and_runtime_nevras(self):
        self.assertEqual(pod.WORKLOAD_SHA256, sha(HERE / "pod_workload.py"))
        for name in ("pod_fixture.py", "pod_workload.py"):
            ast.parse((HERE / name).read_text(), feature_version=(3, 9))
        facts = "podman-5.8.2-8.el9_8.x86_64\ncrun-1.27-1.el9_7.x86_64\n"  # zq3-facts/rocky.txt
        self.assertIn(pod.RUNTIME["podman"][1], facts)
        self.assertIn(pod.RUNTIME["crun"][1], facts)

    def test_workload_imports_avoid_openssl(self):
        tree = ast.parse((HERE / "pod_workload.py").read_text())
        imported = {alias.name for node in ast.walk(tree) if isinstance(node, ast.Import)
                    for alias in node.names}
        self.assertFalse(imported & {"hashlib", "ssl", "subprocess"}, imported)

    def test_matrix_full_group_vector(self):
        self.assertEqual(len(workload.FULL_GROUPS), 1024)
        self.assertEqual(workload.FULL_GROUPS[-1], workload.FILES["group-tail"][1])
        rows = {row[0]: row for row in workload.MATRIX}
        self.assertEqual(rows["full-groups-without-tail"][4], workload.FULL_GROUPS[:-1])
        self.assertNotIn(workload.FILES["group"][1], rows["supplementary-missing"][4])


class EvaluateTests(unittest.TestCase):
    def assertFails(self, text, mutate):
        case = scenario()
        mutate(case)
        failures = judge(case)
        self.assertTrue(any(text in failure for failure in failures), failures)

    def test_good_observations_pass(self):
        self.assertEqual(judge(scenario()), [])

    def test_namespace_not_separated(self):
        for name in pod.ISOLATED:
            with self.subTest(namespace=name):
                def mutate(case, name=name):
                    find(case, "w0", "identity")["namespaces"][name] = GUEST_NS[name]
                    case["outside"]["w0"]["namespaces"][name] = GUEST_NS[name]
                self.assertFails(name + " namespace not separated", mutate)

    def test_user_namespace_must_be_initial(self):
        def mutate(case):
            for view in (find(case, "w1", "identity"), case["outside"]["w1"]):
                view["namespaces"]["user"] = "user:[4026599999]"
                view["uid_map"] = ["0", "100000", "65536"]
        self.assertFails("user namespace is not the guest's initial", mutate)
        self.assertFails("uid_map is not the initial identity map",
                         lambda case: case["outside"]["w0"].update(uid_map=["0", "1000", "1"]))

    def test_outside_view_must_match_self_report(self):
        self.assertFails("outside and self-reported namespaces differ",
                         lambda case: case["outside"]["w0"]["namespaces"].update(mnt="mnt:[1]"))

    def test_selinux_not_enforcing_or_wrong_context(self):
        self.assertFails("not enforcing after the run",
                         lambda case: case["after"].update(enforcing=False))
        for context in ("system_u:system_r:spc_t:s0", "unconfined_u:unconfined_r:unconfined_t:s0",
                        "system_u:system_r:container_t:s0:c1,c2"):
            with self.subTest(context=context):
                self.assertFails("is not container_t",
                                 lambda case, c=context: find(case, "w0", "identity").update(selinux=c))
        self.assertFails("SELinux denial in the pod", lambda case: case.update(audit=[
            "type=AVC msg=audit(2.0:9): avc:  denied  { read } scontext=system_u:system_r:container_t:"
            + pod.LEVEL]))
        self.assertFails("AVC evidence incomplete", lambda case: case.update(audit=None))

    def test_wrong_ids_groups_and_caps(self):
        self.assertFails("Uid is", lambda case: find(case, "w0", "identity")["status"].update(
            Uid=["1000"] * 4))
        self.assertFails("supplementary groups beyond 0",
                         lambda case: case["outside"]["w1"]["status"].update(Groups=["0", "10"]))
        self.assertFails("CapEff is", lambda case: case["outside"]["w0"]["status"].update(
            CapEff=["00000000a80425fb"]))
        self.assertFails("NoNewPrivs is", lambda case: find(case, "w1", "identity")["status"].update(
            NoNewPrivs=["0"]))

    def test_matrix_wrong_ids_or_outcomes(self):
        def truncated(case):
            row = find(case, "w0", "matrix")["cases"][4]["reply"]["result"]
            row["groups"] = row["groups"][:-1]
        self.assertFails("full-groups-tail did not run with exact numeric IDs", truncated)

        def allowed(case):
            find(case, "w0", "matrix")["cases"][1]["reply"]["result"].update(errno=0, read="uid-only")
        self.assertFails("two-uid-other expected EACCES", allowed)

        def changing(case):
            find(case, "w0", "matrix")["cases"][-1]["reply"]["result"][1]["errno"] = 0
        self.assertFails("changing groups [42001] observed errno 0", changing)
        self.assertFails("case list incomplete", lambda case: find(case, "w0", "matrix")["cases"].pop())

    def test_missing_shared_bind(self):
        self.assertFails("not exactly one bind", lambda case: find(case, "w1", "identity").update(mounts=[]))
        self.assertFails("not exactly one bind",
                         lambda case: find(case, "w1", "identity").update(mount_dev=DEV + 1))

        def other_inodes(case):
            find(case, "w1", "locks")["identities"][0]["ino"] = 99
        self.assertFails("did not open the same m0 inodes", other_inodes)

    def test_pod_sharing_and_environment(self):
        self.assertFails("w1 does not share w0's net",
                         lambda case: case["outside"]["w1"]["namespaces"].update(net="net:[9]"))
        self.assertFails("share one mount namespace",
                         lambda case: case["outside"]["w1"]["namespaces"].update(mnt="mnt:[5000]"))
        self.assertFails("expected only lo",
                         lambda case: find(case, "w0", "identity").update(links=["eth0", "lo"]))
        self.assertFails("loopback is not UP",  # IFF_LOOPBACK without IFF_UP.
                         lambda case: find(case, "w1", "identity").update(link_flags={"lo": "0x8"}))
        self.assertFails("is not the pod's own",
                         lambda case: find(case, "w0", "identity").update(hostname="rocky"))
        self.assertFails("memory.max is max", lambda case: case["outside"]["w0"]["limits"].update(
            {"memory.max": {"value": "max", "cgroup": "/"}}))

    def test_lock_and_exit_failures(self):
        self.assertFails("GETLK did not report",
                         lambda case: find(case, "w1", "locks")["getlk"].update(pid=0))
        self.assertFails("acquired before w0 released",
                         lambda case: find(case, "w1", "locks")["posix"].update(acquired_ns=10))
        self.assertFails("survived its exit", lambda case: find(case, "w0", "locks")[
            "after_peer_exit"]["flock"].update(errno=11))
        self.assertFails("no kernel blocked-waiter evidence",
                         lambda case: find(case, "w0", "locks").update(posix_waiter=None))
        self.assertFails("w1: workload did not exit", lambda case: case["statuses"].update(w1=137))
        self.assertFails("lock reports missing", lambda case: case["events"]["w1"].pop(2))

    def test_rootfs_changes(self):
        self.assertFails("pinned entry changed", lambda case: case["after"]["rootfs"].update(
            changed=["/usr/lib64/libc.so.6"]))
        self.assertFails("added nonempty or special", lambda case: case["after"]["rootfs"].update(
            extras=[{"path": "/etc/x", "type": "file", "size": 3}]))
        case = scenario()
        case["after"]["rootfs"]["extras"] = [{"path": "/etc/mtab", "type": "file", "size": 0}]
        self.assertEqual(judge(case), [])


class LockProtocolTests(unittest.TestCase):
    """The workload's w0/w1 lock protocol against real kernel locks (local fs)."""

    def run_protocol(self, leaf_fd):
        near, far = socket.socketpair()
        receive, send = os.pipe()
        pid = os.fork()
        if pid == 0:  # Plays w1; reports through the pipe, exits holding its locks.
            try:
                near.close()
                channel = workload.Channel(far)
                channel.send("hello", pid=os.getpid())
                held = channel.receive("held", time.monotonic() + 10)
                report = workload.contend(channel, leaf_fd, held, time.monotonic() + 10)
                os.write(send, json.dumps(report).encode())
            finally:
                os._exit(0)
        far.close()
        os.close(send)
        try:
            result = workload.hold_and_release(workload.Channel(near), leaf_fd, time.monotonic() + 10)
            raw = workload.read_until_eof(receive, time.monotonic() + 10)
        except BaseException:
            os.kill(pid, 9)  # Local test child only; it may be blocked on our lock.
            raise
        finally:
            near.close()
            os.close(receive)
            os.waitpid(pid, 0)
        return pid, result, json.loads(raw)

    @unittest.skipUnless(os.path.isdir("/dev/shm"), "needs tmpfs /dev/shm")
    def test_real_contention_waits_wakeups_and_exit_release(self):
        # tmpfs, not $TMPDIR: on btrfs stat().st_dev is a per-subvolume device
        # while /proc/locks prints the superblock's, so no waiter would match.
        # Linux 9p (the real target) reports one device in both places.
        with tempfile.TemporaryDirectory(dir="/dev/shm") as directory:
            leaf_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                pid, held, contender = self.run_protocol(leaf_fd)
                baseline = {"m0": {"dev": os.fstat(leaf_fd).st_dev}}
            finally:
                os.close(leaf_fd)
        idents = {"w0": {"pid": os.getpid()}, "w1": {"pid": pid}}
        w = pod.workload_module()
        locks = {"w0": held, "w1": contender}
        self.assertEqual(pod.check_locks(w, baseline, idents, locks), [])
        self.assertIn(" -> ", held["posix_waiter"])
        self.assertEqual(contender["getlk"]["pid"], os.getpid())
        tampered = copy.deepcopy(locks)
        tampered["w1"]["getlk"]["pid"] = pid
        self.assertTrue(pod.check_locks(w, baseline, idents, tampered))
        self.assertTrue(pod.check_locks(w, {"m0": {"dev": 1}}, idents, locks))

    def test_waiter_line_selects_only_the_peer_on_that_inode(self):
        ident = {"dev": os.makedev(0, 0x2B), "ino": 4711}
        raw = ("1: POSIX  ADVISORY  WRITE 1 00:2b:4711 0 0\n"
               "1: -> POSIX  ADVISORY  WRITE 9 00:2b:4711 0 0\n"
               "1: -> POSIX  ADVISORY  WRITE 7 00:2b:4712 0 0\n"
               "2: -> POSIX  ADVISORY  WRITE 7 00:2b:4711 0 EOF\n")
        self.assertIsNone(workload.waiter_line(raw, 7, ident, "posix"))
        self.assertIn("EOF", workload.waiter_line(raw, 7, ident, "flock"))
        self.assertIn(" 9 ", workload.waiter_line(raw, 9, ident, "posix"))

    def test_workload_params_and_local_identity(self):
        for raw in ("{}", json.dumps({"version": workload.VERSION, "run_id": "r1", "role": "w2",
                                      "seconds": 30}),
                    json.dumps({"version": workload.VERSION, "run_id": "r1", "role": "w0",
                                "seconds": 600})):
            with self.subTest(raw=raw):
                with self.assertRaises(workload.Refused):
                    workload.parse_params(raw)
        report = workload.identity(with_mount=False)
        self.assertEqual(set(report["namespaces"]), set(workload.NAMESPACES))
        root = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
        try:
            reply, status = workload.child(lambda: workload.try_read(root, "absent"))
        finally:
            os.close(root)
        self.assertEqual((status, reply["result"]["errno"]), (0, 2))


class ManifestTests(Sandbox):
    def test_manifest_attests_every_installed_entry(self):
        manifest = pod.compute_manifest(self.host)
        again = pod.compute_manifest(self.host)
        self.assertEqual(pod.canonical(manifest), pod.canonical(again))
        entries = {entry["path"]: entry for entry in manifest["entries"]}
        for name in list(Mirror.FILES) + ["/lib64", "/usr/lib64/libffi.so.8"]:
            self.assertTrue(entries[name]["origin"].startswith("rpm:"), name)
        self.assertNotIn("/usr/lib64/libffi.so", entries)  # Devel link is not a SONAME.
        self.assertEqual(entries[pod.WORKLOAD_PATH]["sha256"], pod.WORKLOAD_SHA256)
        for name in ("/proc", "/sys", "/dev", "/run", "/tmp", "/hfs/m0", "/etc/hosts"):
            self.assertIn(name, entries)
        self.assertEqual({p["vendor"] for p in manifest["packages"]}, {ROCKY})

    def test_refuses_unattested_or_unexpected_inputs(self):
        cases = [
            ("content differs from rpm database",
             lambda: Path(self.mirror.src + "/usr/lib64/libc.so.6").write_bytes(b"evil")),
            ("not owned by an installed package", lambda: self.mirror.owner.pop("/usr/lib64/libc.so.6")),
            ("mapped file outside /usr", lambda: self.mirror.maps.append("/opt/evil.so")),
            ("module outside the Rocky python3.9 stdlib",
             lambda: self.mirror.modules.append("/home/paul/evil.py")),
            ("module outside the Rocky python3.9 stdlib",
             lambda: self.mirror.modules.append(pod.STDLIB + "site-packages/evil.py")),
            ("without SHA256 file digests", lambda: setattr(self.mirror, "algo", "1")),
            ("unsigned package", lambda: setattr(self.mirror, "signature", "(none)")),
            ("unsigned package", lambda: setattr(self.mirror, "signature", "")),
            ("closure probe used another interpreter",
             lambda: setattr(self.mirror, "executable", "/usr/bin/python3")),
            ("mode differs from rpm database",
             lambda: os.chmod(self.mirror.src + "/usr/lib64/python3.9/os.py", 0o600)),
        ]
        for text, mutate in cases:
            with self.subTest(text=text):
                saved = copy.deepcopy((self.mirror.owner, self.mirror.maps, self.mirror.modules))
                try:
                    mutate()
                    self.refuses(text, pod.compute_manifest, self.host)
                finally:  # Restore even when this subtest failed.
                    self.mirror.owner, self.mirror.maps, self.mirror.modules = saved
                    self.mirror.algo, self.mirror.executable = "8", pod.PYTHON
                    self.mirror.signature = "RSA/SHA256, Key ID 702d426d350d275d"
                    libc = self.mirror.src + "/usr/lib64/libc.so.6"
                    Path(libc).write_bytes(Mirror.FILES["/usr/lib64/libc.so.6"])
                    os.chmod(libc, 0o755)
                    os.chmod(self.mirror.src + "/usr/lib64/python3.9/os.py", 0o644)

    def test_build_materialises_and_verifies(self):
        manifest, result = self.built()
        rootfs = self.run_dir / "pod" / "rootfs"
        self.assertEqual(result["rootfs"], str(rootfs))
        self.assertEqual(os.readlink(rootfs / "lib64"), "usr/lib64")
        self.assertEqual((rootfs / "hfs/pod_workload.py").read_bytes(),
                         (HERE / "pod_workload.py").read_bytes())
        self.assertEqual(len(self.host.labels), len(manifest["entries"]))
        self.assertEqual(set(self.host.labels.values()), {pod.LABEL})
        self.assertEqual(pod.digest((self.run_dir / "pod/rootfs-manifest.json").read_bytes()),
                         result["manifest_sha256"])
        with self.assertRaises(FileExistsError):  # One build per run directory.
            pod.build(self.host, "r1", result["manifest_sha256"])

    def test_build_refuses_other_digest_before_writing(self):
        self.refuses("differs from the pinned digest", pod.build, self.host, "r1", "0" * 64)
        self.assertFalse((self.run_dir / "pod").exists())
        os.chmod(self.run_dir, 0o750)
        self.refuses("root-private local run directory", pod.build, self.host, "r1", "0" * 64)

    def test_verify_tree_reports_changes_and_extras(self):
        manifest, _ = self.built()
        rootfs = str(self.run_dir / "pod" / "rootfs")
        self.assertEqual(pod.verify_tree(self.host, rootfs, manifest), {"changed": [], "extras": []})
        libc = rootfs + "/usr/lib64/libc.so.6"
        os.chmod(libc, 0o644)
        self.host.labels[rootfs + "/usr/bin/python3.9"] = b"system_u:object_r:tmp_t:s0"
        os.mkdir(rootfs + "/etc/extra")
        state = pod.verify_tree(self.host, rootfs, manifest)
        self.assertEqual(state["changed"], ["/usr/bin/python3.9", "/usr/lib64/libc.so.6"])
        self.assertEqual(state["extras"], [{"path": "/etc/extra", "type": "dir", "size": None}])


def request_for(manifest_sha256):
    return {"version": pod.VERSION, "run_id": "r1", "fixture_sha256": sha(HERE / "pod_fixture.py"),
            "rootfs_manifest_sha256": manifest_sha256,
            "runtime_sha256": {name: hashlib.sha256(name.encode()).hexdigest() for name in pod.RUNTIME},
            "m0": {"tag": "hfs0", "mount_id": 77}}


FAKE_CONTAINER = textwrap.dedent("""
    import json, select, sys
    data = json.load(open(sys.argv[1]))
    for event in data["pre"]:
        print(json.dumps(event), flush=True)
    if not select.select([sys.stdin], [], [], 5)[0]:
        sys.exit(3)
    if sys.stdin.readline().strip() != "observed":
        sys.exit(4)
    for event in data["post"]:
        print(json.dumps(event), flush=True)
    sys.exit(data["code"])
""")


class AdmissionTests(Sandbox):
    def guest(self, mountinfo=None):
        m0 = str(self.run_dir / "m0")
        self.host.files.update({
            "/sys/fs/selinux/enforce": b"1", "/proc/sys/kernel/hostname": b"rocky\n",
            "/proc/1/uid_map": b"         0          0 4294967295\n",
            "/proc/1/gid_map": b"         0          0 4294967295\n",
            "/proc/self/mountinfo": (mountinfo or
                                     "22 1 253:0 / / rw - xfs /dev/root rw\n"
                                     "77 22 0:190 / %s rw,relatime - 9p hfs0 rw,cache=mmap,"
                                     "version=9P2000.L.hfs1.4,msize=65536\n" % m0).encode()})
        for name in pod.NAMESPACES:
            self.host.links["/proc/self/ns/" + name] = GUEST_NS[name]
            self.host.links["/proc/1/ns/" + name] = GUEST_NS[name]
        self.host.stats[m0] = types.SimpleNamespace(st_dev=DEV)
        for name, (path, nevra) in pod.RUNTIME.items():
            self.host.hashes[path] = hashlib.sha256(name.encode()).hexdigest()

    def runtime_rpm(self, argv):
        owners = {path: nevra or name + "-1.0-1.el9.x86_64" for name, (path, nevra) in pod.RUNTIME.items()}
        if argv[:3] == ["rpm", "-qf", "--qf"] and len(argv) == 6 and argv[5] in owners:
            return 0, (owners[argv[5]] + "\n").encode(), b""
        return None

    def test_baseline_records_guest_init_and_m0(self):
        self.guest()
        baseline = pod.guest_baseline(self.host, request_for("0" * 64))
        self.assertEqual(baseline["m0"], {"mount_id": 77, "tag": "hfs0", "dev": DEV})
        self.assertEqual(baseline["namespaces"], GUEST_NS)

    def test_admission_refusals(self):
        m0 = str(self.run_dir / "m0")
        line = "77 22 0:190 / %s rw - 9p hfs0 rw,cache=mmap,version=9P2000.L.hfs1.4,msize=65536\n"
        cases = [
            ("SELinux enforcing required", lambda: self.host.files.update(
                {"/sys/fs/selinux/enforce": b"0"})),
            ("initial net namespace", lambda: self.host.links.update({"/proc/self/ns/net": "net:[1]"})),
            ("initial user namespace map", lambda: self.host.files.update(
                {"/proc/1/uid_map": b"0 100000 65536\n"})),
            ("not present once", lambda: self.host.files.update(
                {"/proc/self/mountinfo": (line % m0 * 2).encode()})),
            ("mount tuple differs", lambda: self.host.files.update(
                {"/proc/self/mountinfo": (line % m0).replace("cache=mmap", "cache=none").encode()})),
            ("mount tuple differs", lambda: self.host.files.update(
                {"/proc/self/mountinfo": (line % m0).replace(" 9p hfs0", " 9p hfs1").encode()})),
            ("mount tuple differs", lambda: self.host.files.update(
                {"/proc/self/mountinfo": (line % m0).replace("77 ", "78 ", 1).encode()})),
            ("root-private local run directory", lambda: self.host.stats.update(
                {m0: types.SimpleNamespace(st_dev=os.stat(self.run_dir).st_dev)})),
        ]
        for text, mutate in cases:
            with self.subTest(text=text):
                self.guest()
                mutate()
                self.refuses(text, pod.guest_baseline, self.host, request_for("0" * 64))

    def test_runtime_pins(self):
        self.guest()
        self.host.handlers.insert(0, self.runtime_rpm)
        record = pod.check_runtime(self.host, request_for("0" * 64))
        self.assertEqual(record["podman"]["packages"], ["podman-5.8.2-8.el9_8.x86_64"])
        self.host.hashes["/usr/bin/crun"] = "1" * 64
        self.refuses("crun SHA256 differs", pod.check_runtime, self.host, request_for("0" * 64))
        self.guest()
        self.host.handlers.insert(0, lambda argv: (0, b"podman-5.6.0-1.el9.x86_64\n", b"")
                                  if argv[-1] == "/usr/bin/podman" else None)
        self.refuses("podman package identity differs", pod.check_runtime, self.host,
                     request_for("0" * 64))

    def test_request_shape(self):
        good = request_for("0" * 64)
        self.assertEqual(pod.parse_request(json.dumps(good).encode())["run_id"], "r1")
        for label, change in [("extra key", {"seconds": 5}), ("fixture pin", {"fixture_sha256": "1" * 64}),
                              ("tag", {"m0": {"tag": "hfs1", "mount_id": 77}}),
                              ("run id", {"run_id": "R1"}),
                              ("runtime set", {"runtime_sha256": {"podman": "0" * 64}})]:
            with self.subTest(label=label):
                with self.assertRaises(pod.Refused):
                    pod.parse_request(json.dumps({**good, **change}).encode())


class PodmanArgvTests(unittest.TestCase):
    def test_exact_isolation_flags_and_no_weakening(self):
        pod_dir = "/var/tmp/hfs-zq3-r1/pod"
        w0 = pod.run_args(pod_dir, "r1", "w0", workload.VERSION)
        w1 = pod.run_args(pod_dir, "r1", "w1", workload.VERSION)
        for argv in (w0, w1):
            text = " ".join(argv)
            for wanted in ("--userns=host", "--rootfs /var/tmp/hfs-zq3-r1/pod/rootfs", "--read-only",
                           "--cap-drop=all", "--security-opt=no-new-privileges",
                           "--security-opt=label=level:" + pod.LEVEL, "--storage-driver vfs",
                           "--runtime /usr/bin/crun", "--pids-limit=64",
                           "source=/var/tmp/hfs-zq3-r1/m0,destination=/hfs/m0", "--passwd=false"):
                self.assertIn(wanted, text)
            for banned in ("--privileged", "label=disable", ":z", ":Z", "=host,", "--network=host",
                           "--pid=host", "--ipc=host", "--uts=host", "--userns=auto", "unconfined",
                           "docker.io", "--pull"):
                self.assertNotIn(banned, text)
            self.assertEqual(argv.count("--cap-add=SETUID"), 1)
        self.assertIn("--network=none", w0)
        self.assertIn("--hostname=" + pod.hostname("r1"), w0)
        for kind in ("network", "ipc", "uts", "pid"):
            self.assertIn("--%s=container:hfs-zq3-r1-w0" % kind, w1)
        params = json.loads(w1[-1])
        self.assertEqual(workload.parse_params(w1[-1]), params)
        files = pod.podman_files(pod_dir)
        self.assertIn('driver = "vfs"', files["storage.conf"])
        self.assertEqual(files["registries.conf"], "unqualified-search-registries = []\n")
        self.assertEqual(pod.podman_env(pod_dir)["CONTAINERS_CONF"], pod_dir + "/containers.conf")


class RunTests(AdmissionTests):
    """End to end through main(): build, admission, fake containers, verdict."""

    def setUp(self):
        super().setUp()
        self.guest()
        self.host.handlers.insert(0, self.runtime_rpm)
        self.host.handlers.append(self.podman)
        self.host.spawner = self.spawn
        self.case = scenario()
        self.pre = {"w0": 2, "w1": 2}  # identity + ready before the barrier.
        for role, pid in (("w0", 4242), ("w1", 4343)):
            self.proc(role, pid)
        for name in sorted(list(workload.FILES) + list(workload.LOCK_FILES)):
            (self.run_dir / "m0" / "zq3-r1-pod").mkdir(exist_ok=True)
            (self.run_dir / "m0" / "zq3-r1-pod" / name).write_text(name)

    def proc(self, role, pid):
        view, base = outside_view(role), "/proc/%d/" % pid
        status = "".join("%s:\t%s\n" % (key, "\t".join(value)) for key, value in view["status"].items())
        self.host.files.update({base + "status": status.encode(), base + "uid_map": b"0 0 4294967295\n",
                                base + "gid_map": b"0 0 4294967295\n",
                                base + "attr/current": (CONTEXT + "\0").encode(),
                                base + "cgroup": ("0::" + view["cgroup"] + "\n").encode()})
        scope = "/sys/fs/cgroup" + os.path.dirname(view["cgroup"])
        for control, value in (("memory.max", str(pod.MEMORY)), ("memory.swap.max", "0"),
                               ("pids.max", str(pod.PIDS))):
            self.host.files[scope + "/container/" + control] = b"max\n"
            self.host.files[scope + "/" + control] = (value + "\n").encode()
        for name, value in view["namespaces"].items():
            self.host.links[base + "ns/" + name] = value

    def podman(self, argv):
        if argv[0] != "/usr/bin/podman":
            return None
        if "inspect" in argv:
            if getattr(self, "inspect_timeout", None) == argv[-1][-2:]:
                # What Host.run raises after killing a stuck podman client.
                raise subprocess.TimeoutExpired(argv, 30)
            return 0, (b"4242\n" if argv[-1].endswith("w0") else b"4343\n"), b""
        if "rm" in argv:
            return 0, argv[-1].encode() + b"\n", b""
        raise AssertionError(argv)

    def spawn(self, argv, env):
        self.assertEqual(env["CONTAINERS_REGISTRIES_CONF"], str(self.run_dir / "pod/registries.conf"))
        role = argv[argv.index("--name") + 1].rsplit("-", 1)[1]
        events = self.case["events"][role]
        data = self.root / (role + ".json")
        data.write_text(json.dumps({"pre": events[:self.pre[role]], "post": events[self.pre[role]:],
                                    "code": self.case["statuses"][role]}))
        script = self.root / "fake_container.py"
        script.write_text(FAKE_CONTAINER)
        return subprocess.Popen([sys.executable, "-I", str(script), str(data)], stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)

    def main(self):
        _, built = self.built()
        path = self.root / "request.json"
        path.write_text(json.dumps(request_for(built["manifest_sha256"])))
        with mock.patch("sys.stdout") as out:
            code = pod.main(["run", "--request", str(path)], self.host)
        return code, json.loads("".join(call.args[0] for call in out.write.call_args_list
                                        if call.args) or "{}")

    def test_pass_persists_evidence_and_removes_exact_payload(self):
        code, verdict = self.main()
        self.assertEqual((code, verdict["failures"], verdict["t7_pass"]), (0, [], True))
        self.assertFalse(verdict["acceptance"])
        evidence = self.run_dir / "pod" / "evidence"
        self.assertEqual(sorted(os.listdir(evidence)),
                         ["cleanup.json", "request.json", "verdict.json", "w0.stderr", "w0.stdout",
                          "w1.stderr", "w1.stdout"])
        self.assertFalse((self.run_dir / "m0" / "zq3-r1-pod").exists())
        removed = [call[-1] for call in self.host.calls if call[0] == "/usr/bin/podman" and "rm" in call]
        self.assertEqual(removed, ["hfs-zq3-r1-w1", "hfs-zq3-r1-w0"])

    def test_failure_preserves_payload_and_containers(self):
        find(self.case, "w1", "identity")["namespaces"]["user"] = "user:[4026599999]"
        code, verdict = self.main()
        self.assertEqual(code, 1)
        self.assertTrue(any("user namespace" in failure for failure in verdict["failures"]))
        self.assertTrue((self.run_dir / "m0" / "zq3-r1-pod" / "lock-posix").exists())
        self.assertFalse([call for call in self.host.calls if "rm" in call])
        self.assertTrue((self.run_dir / "pod" / "evidence" / "verdict.json").exists())

    def test_no_ready_is_uncertain_and_never_killed(self):
        self.pre["w0"] = 0
        self.case["events"]["w0"] = []
        with mock.patch.object(pod, "RUN_SECONDS", 1):
            code, verdict = self.main()
        self.assertEqual(code, 1)
        self.assertTrue(verdict["failures"][0].startswith("Refused: uncertain: timed out"))
        self.assertIsNone(self.host.spawned[0].poll())  # The driver did not kill it.

    def test_client_timeout_still_writes_verdict_and_keeps_containers(self):
        self.inspect_timeout = "w1"
        code, verdict = self.main()
        self.assertEqual(code, 1)
        self.assertTrue(verdict["failures"][0].startswith("TimeoutExpired"), verdict["failures"])
        evidence = self.run_dir / "pod" / "evidence"
        for name in ("verdict.json", "w0.stdout", "w0.stderr", "w1.stdout", "w1.stderr"):
            self.assertTrue((evidence / name).exists(), name)
        self.assertFalse([call for call in self.host.calls if "rm" in call])
        self.assertTrue((self.run_dir / "m0" / "zq3-r1-pod" / "lock-posix").exists())

    @unittest.skipIf(os.geteuid() == 0, "root reads mode 000 files")
    def test_unreadable_audit_log_refuses_before_any_write(self):
        os.chmod(self.root / "audit.log", 0)
        code, verdict = self.main()
        self.assertEqual((code, verdict["refused"]), (2, True))
        self.assertFalse((self.run_dir / "pod" / "evidence").exists())

    def test_missing_audit_log_refuses_before_any_write(self):
        os.unlink(self.root / "audit.log")
        code, verdict = self.main()
        self.assertEqual((code, verdict["refused"]), (2, True))
        self.assertFalse((self.run_dir / "pod" / "evidence").exists())
        self.assertEqual(self.host.spawned, [])

    def test_second_run_in_same_pod_refuses(self):
        self.main()
        path = self.root / "request.json"
        with mock.patch("sys.stdout"):
            self.assertEqual(pod.main(["run", "--request", str(path)], self.host), 2)


def system_python():
    """This workstation's RPM-installed interpreter and stdlib, if any."""
    if not shutil.which("rpm") or not os.path.exists("/usr/bin/python3"):
        return None
    python = os.path.realpath("/usr/bin/python3")
    owned = subprocess.run(["rpm", "-qf", python], capture_output=True, check=False)
    if owned.returncode != 0:
        return None
    stdlib = subprocess.run([python, "-I", "-c", "import sysconfig;print(sysconfig.get_path('stdlib'))"],
                            capture_output=True, text=True, check=False).stdout.strip()
    return python, stdlib + "/"


@unittest.skipUnless(system_python(), "needs an RPM-installed /usr/bin/python3")
class RealRpmTests(unittest.TestCase):
    """Real read-only RPM database and interpreter of this workstation (not Rocky)."""

    def test_manifest_build_and_chroot_start(self):
        python, stdlib = system_python()
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory) / "hfs-zq3-r1"
            run_dir.mkdir(mode=0o700)
            patches = [mock.patch.object(pod, name, value) for name, value in
                       [("PYTHON", python), ("STDLIB", stdlib), ("SOURCE_ROOT", ""),
                        ("RUN_PARENT", str(Path(directory) / "hfs-zq3-")),
                        ("TRUSTED_UID", os.getuid()), ("TRUSTED_GID", os.getgid())]]
            for patcher in patches:
                patcher.start()
                self.addCleanup(patcher.stop)
            host = LabelHost()
            first, second = pod.manifest_report(host), pod.manifest_report(host)
            self.assertEqual(first["manifest_sha256"], second["manifest_sha256"])
            origins = {entry["path"]: entry["origin"] for entry in first["manifest"]["entries"]}
            self.assertTrue(origins[python].startswith("rpm:"))
            pod.build(host, "r1", first["manifest_sha256"])
            self.start_in_chroot(str(run_dir / "pod" / "rootfs"), python)

    def start_in_chroot(self, rootfs, python):
        """The interpreter and all workload imports load from the rootfs alone."""
        if not shutil.which("unshare"):
            self.skipTest("unshare not available")
        done = subprocess.run(["unshare", "-r", "chroot", rootfs, python, "-I", "-B", "-S",
                               pod.WORKLOAD_PATH, "workload", "{}"], capture_output=True,
                              timeout=60, env=pod.CLEAN_ENV, check=False)
        if done.returncode != 1 and b"unshare" in done.stderr:
            self.skipTest("unprivileged user namespaces unavailable: " + done.stderr.decode()[:200])
        lines = [json.loads(line) for line in done.stdout.decode().splitlines()]
        self.assertEqual(done.returncode, 1, done.stderr.decode())
        self.assertEqual(lines[0]["detail"], "params shape")
        self.assertEqual(lines[-1], {**lines[-1], "event": "terminal", "success": False})


if __name__ == "__main__":
    unittest.main()
