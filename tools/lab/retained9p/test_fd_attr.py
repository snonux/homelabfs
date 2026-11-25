#!/usr/bin/env python3
"""Local tests of the bl4 hfs_fdattr gate and the peer-rename guest check.

s64 lets GETATTR and SETATTR go through the open file description when the
guest mounts with `hfs_fdattr`. Only a candidate whose guest kernel, lib9p
exporter and daemon all carry s64 may mount with it (controller.FD_ATTR,
empty until the next candidate built from main is admitted), so every test
of the enabled path patches FD_ATTR. Both directions are covered: a plan
without fd_attr keeps its exact pre-bl4 shape and refuses a mount that
shows the option, and a plan with it refuses an older candidate and a mount
that does not show it. nl4: the named candidate must be the one whose guest
kernel the run installs (the bootstrap's pinned kernel plan) and whose daemon
and exporter it runs (the candidate's package manifest), and the one the batch names
(fill_plan), and the retained `peer-renamed` is checked by retain and
unlinked by release before the leaf's rmdir.

The metadata case's peer rename (fd open on m0, rename through m1, then
stat(old name) ENOENT and fstat(fd) on the same file) runs here on one local
directory opened twice, as test_guest_alarm.py's cases do. That proves the
case's logic and its payload bookkeeping, not the guest kernel: the s64
fallback it guards only exists in a guest VM (zq3 T3).
No guest, 9p, SSH, VM or lab.
"""
import contextlib
import errno
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

from test_bootstrap_remote import BOOT, RemoteCase
from test_guest_alarm import TwoRootsTestCase, case_request
from test_lifecycle_steps import MOUNTS, boundary, step_request
from test_preparation import ADMITTED, SUPERSEDED, batch_fixture, controller, guest
import test_bootstrap
import test_bootstrap_remote
import test_fill_plan

rocky_kernel = test_bootstrap.module("rocky_kernel_fd_attr", "rocky_kernel.py")

ROW = "{} 1 0:{} / {} rw,relatime - 9p {} {}\n"
bootstrap_module = test_bootstrap.bootstrap
# Package digests of the s64 fixture candidates' daemon and exporters. DAEMON
# is plan_fixture's runtime.sha256; the R21 pair is the pre-s64 first r21
# rebuild 1428d4db (its evidence README), which an s64 plan must never run.
DAEMON, EXPORTER, EXPORTER_B = "5" * 64, "6" * 64, "8" * 64
R21_DAEMON = "013515a8731cde2bad643230a7822221f2ff086a6d72812778eb78d69c761200"
R21_EXPORTER = "368a021d2046bf9c2845a4d80345d5665f948bd6136a3a233827505d06a052db"
PLAIN = "rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4"
SHOWN = "rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4"


def fd_attr_candidates(module=controller):
    """FD_ATTR holding the admitted candidate, as for the next s64 build.

    Each test module loads its own controller instance (bootstrap.py and
    fill_plan.py load theirs by path), so the caller names the one in use.
    """
    return mock.patch.object(module, "FD_ATTR", frozenset({ADMITTED}))


def with_fd_attr(request):
    return {**request, "fd_attr": True}


class ControllerPlanTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def plan(self, fd_attr=True, requests=True):
        _, plan, _ = batch_fixture(self.root)
        if fd_attr is not None:
            plan["fd_attr"] = fd_attr
        if requests:
            plan["requests"] = [with_fd_attr(request) for request in plan["requests"]]
        return plan

    def test_only_the_admitted_candidate_carries_s64(self):
        # The second r21 rebuild carries s64 in all three parts and is the
        # one FD_ATTR digest; every superseded candidate (the first r21
        # rebuild 1428d4db included) predates s64 and may never be added.
        self.assertEqual(controller.FD_ATTR, frozenset({ADMITTED}))
        self.assertTrue(controller.FD_ATTR <= controller.ADMITTED)
        controller.require_fd_attr(ADMITTED)
        for value in SUPERSEDED:
            with self.subTest(candidate=value), self.assertRaises(controller.Refused):
                controller.require_fd_attr(value)

    def test_plan_without_fd_attr_keeps_its_shape(self):
        controller.validate_shape(self.plan(fd_attr=None, requests=False))

    def test_fd_attr_plan_refuses_a_pre_s64_candidate(self):
        # The fixture plan names ADMITTED, which is now in FD_ATTR; an empty
        # FD_ATTR stands for an admitted candidate that predates s64.
        with mock.patch.object(controller, "FD_ATTR", frozenset()), \
                self.assertRaisesRegex(controller.Refused, "predates s64"):
            controller.validate_shape(self.plan())

    def test_fd_attr_plan_validates_for_an_s64_candidate(self):
        with fd_attr_candidates():
            plan = self.plan()
            controller.validate_shape(plan)
            step = controller.step_request(plan, "quiesce")
        self.assertIs(step["fd_attr"], True)
        self.assertEqual(guest.request_kind(step), "step")

    def test_requests_must_agree_with_the_plan(self):
        with fd_attr_candidates():
            for plan, text in [(self.plan(requests=False), "closed input shape"),
                               (self.plan(fd_attr=None), "closed input shape"),
                               (self.plan(fd_attr=False), "fd_attr must be true"),
                               (self.plan(fd_attr="yes"), "fd_attr must be true")]:
                with self.subTest(text=text), self.assertRaisesRegex(controller.Refused, text):
                    controller.validate_shape(plan)
            plan = self.plan()
            plan["requests"][2]["fd_attr"] = 1
            with self.assertRaisesRegex(controller.Refused, "request fd_attr must be true"):
                controller.validate_shape(plan)


class GuestMountTupleTests(unittest.TestCase):
    """open_domains/admit_step's check_tuple with and without fd_attr."""

    def tuple(self, super_options):
        return (MOUNTS[0]["mount_id"], "9p", MOUNTS[0]["tag"], {"rw", "relatime"}, super_options.split(","))

    def test_option_must_match_the_request(self):
        row = MOUNTS[0]
        self.assertEqual(guest.check_tuple(row, self.tuple(PLAIN), False), row["mount_id"])
        self.assertEqual(guest.check_tuple(row, self.tuple(SHOWN), True), row["mount_id"])
        for options, expect in [(PLAIN, True), (SHOWN, False),
                                ("rw,hfs_fdattr,cache=mmap,msize=65536,version=9P2000.L.hfs1.4", True),
                                (SHOWN.replace("hfs_fdattr", "hfs_fdattr,hfs_fdattr"), True),
                                ("rw,cache=none,hfs_fdattr,msize=65536,version=9P2000.L.hfs1.4", True)]:
            with self.subTest(options=options, expect=expect), \
                    self.assertRaisesRegex(guest.Refused, "mount tuple differs"):
                guest.check_tuple(row, self.tuple(options), expect)

    def test_request_shapes_accept_only_a_true_fd_attr(self):
        for request, kind in [(case_request(), "case"), (step_request("quiesce"), "step")]:
            self.assertEqual(guest.request_kind(with_fd_attr(request)), kind)
            self.assertTrue(guest.fd_attr(with_fd_attr(request)))
            self.assertFalse(guest.fd_attr(request))
            for value in [False, 1, "true", None]:
                with self.subTest(kind=kind, value=value), self.assertRaises(guest.Refused):
                    guest.request_kind({**request, "fd_attr": value})

    def test_step_admission_checks_the_option(self):
        def admit(request, options):
            lines = "".join(ROW.format(m["mount_id"], m["mount_id"], m["path"], m["tag"], options)
                            for m in MOUNTS).encode()
            with mock.patch.object(guest, "read", side_effect=lambda path, _: boundary(lines)[path]), \
                    mock.patch.object(guest.os, "geteuid", return_value=0), \
                    mock.patch.object(guest.os.path, "realpath", side_effect=lambda path: path), \
                    contextlib.redirect_stdout(io.StringIO()):
                return guest.admit_step(request)
        request = step_request("quiesce")
        self.assertTrue(all(m["present"] for m in admit(with_fd_attr(request), SHOWN)))
        self.assertTrue(all(m["present"] for m in admit(request, PLAIN)))
        for changed, options in [(request, SHOWN), (with_fd_attr(request), PLAIN)]:
            with self.subTest(fd_attr="fd_attr" in changed), self.assertRaises(guest.Refused):
                admit(changed, options)


class PeerRenameTests(TwoRootsTestCase):
    """The metadata case's bl4 peer rename on a local stand-in for m0/m1."""

    def run_metadata(self, request, patches=()):
        leaf = os.path.join(self.temp, "m0", "zq3-test-metadata")
        self.addCleanup(shutil.rmtree, leaf, True)
        printed = io.StringIO()
        probe = guest.Probe(request, self.roots)
        with contextlib.ExitStack() as stack:
            for name, effect in patches:
                stack.enter_context(mock.patch.object(guest.os, name, side_effect=effect))
            stack.enter_context(contextlib.redirect_stdout(printed))
            try:
                guest.metadata(probe)
                failure = None
            except guest.Refused as refused:
                failure = refused
        return failure, [json.loads(line) for line in printed.getvalue().splitlines()], leaf

    def invoked(self, events):
        return [event["operation"] for event in events if event["event"] == "invoke"]

    def test_without_fd_attr_the_case_is_unchanged(self):
        failure, events, leaf = self.run_metadata(case_request("metadata"))
        self.assertIsNone(failure)
        self.assertNotIn("rename-cross-domain", self.invoked(events))
        self.assertEqual(sorted(os.listdir(leaf)), ["linked", "renamed"])
        self.assertEqual(guest.payload_files(case_request("metadata")), ["renamed", "linked"])

    def test_peer_rename_old_name_enoent_and_fstat_same_file(self):
        request = with_fd_attr(case_request("metadata"))
        failure, events, leaf = self.run_metadata(request)
        self.assertIsNone(failure)
        self.assertEqual(self.invoked(events)[-7:],
                         ["create-exclusive", "pwrite", "fstat-before-peer-rename", "rename-cross-domain",
                          "stat-old-name", "fstat-after-peer-rename", "stat-new-name"])
        old_name = [e for e in events if e["event"] == "reply" and e["operation"] == "stat-old-name"]
        self.assertEqual(old_name[0]["errno"], errno.ENOENT)
        self.assertEqual(sorted(os.listdir(leaf)), ["linked", "peer-renamed", "renamed"])
        self.assertEqual(guest.payload_files(request), ["renamed", "linked", "peer-renamed"])

    def test_old_name_answering_attributes_fails_the_case(self):
        """The hazard the check guards: the old name still resolves."""
        real = os.stat

        def stale(path, *args, **kwargs):
            if path == "peer-original":
                return real("peer-renamed", *args, **kwargs)
            return real(path, *args, **kwargs)
        failure, events, _ = self.run_metadata(with_fd_attr(case_request("metadata")), [("stat", stale)])
        self.assertRegex(str(failure), "must fail ENOENT.*errno 0")
        self.assertEqual(self.invoked(events)[-1], "stat-old-name")

    def test_fstat_of_another_file_fails_the_case(self):
        real, calls = os.fstat, []

        def moved(fd):
            calls.append(fd)
            seen = real(fd)
            return seen if len(calls) == 1 else os.stat_result((seen.st_mode, seen.st_ino + 1) + tuple(seen)[2:])
        failure, _, _ = self.run_metadata(with_fd_attr(case_request("metadata")), [("fstat", moved)])
        self.assertRegex(str(failure), "describe the same file")

    def test_fstat_estale_names_itself(self):
        real, calls = os.fstat, []

        def stale(fd):
            calls.append(fd)
            if len(calls) == 2:
                raise OSError(errno.ESTALE, "stale")
            return real(fd)
        probe = guest.Probe(with_fd_attr(case_request("metadata")), self.roots)
        printed = io.StringIO()
        self.addCleanup(shutil.rmtree, os.path.join(self.temp, "m0", "zq3-test-metadata"), True)
        with mock.patch.object(guest.os, "fstat", side_effect=stale), contextlib.redirect_stdout(printed), \
                self.assertRaises(OSError):
            guest.metadata(probe)
        last = json.loads(printed.getvalue().splitlines()[-1])
        self.assertEqual((last["operation"], last["errno"]), ("fstat-after-peer-rename", errno.ESTALE))


class RemoteMountTests(RemoteCase):
    def mount(self, options, argv_extra=()):
        mountpoint = "/var/tmp/hfs-zq3-zq3-r1/m0"
        (self.root / mountpoint.lstrip("/")).mkdir(parents=True, mode=0o700, exist_ok=True)

        def mounted(argv):
            with open(self.root / "proc/self/mountinfo", "a") as stream:
                stream.write(ROW.format(90, 55, argv[-1], "hfs0", options))
        self.system.on_mount, self.system.outcomes = mounted, [(0, b"", b"")]
        return self.act(["guest-mount", "--tag", "hfs0", "--mountpoint", mountpoint, "--boot-id", BOOT,
                         "--seconds", "25", *argv_extra])

    def test_fd_attr_on_adds_the_option_and_requires_it_shown(self):
        code, result = self.mount(SHOWN, ["--fd-attr", "on"])
        self.assertEqual(code, 0, result)
        self.assertEqual(self.system.children[0][0][4], test_bootstrap_remote.remote.MOUNT_OPTIONS_FD_ATTR)

    def test_option_not_shown_is_a_known_failure(self):
        code, result = self.mount(PLAIN, ["--fd-attr", "on"])
        self.assertEqual((code, result["changed"]), (2, True))
        self.assertIn("hfs_fdattr", result["error"])

    def test_default_is_off_and_refuses_a_shown_option(self):
        code, result = self.mount(SHOWN)
        self.assertEqual((code, result["changed"]), (2, True))
        self.assertEqual(self.system.children[0][0][4], test_bootstrap_remote.remote.MOUNT_OPTIONS)


def manifest_raw(daemon=DAEMON, exporter=EXPORTER):
    """A candidate package manifest.json (package.py shape, the entries the
    bootstrap reads); its SHA256 is the candidate digest."""
    files = {bootstrap_module.CANDIDATE_DAEMON: {"kind": "daemon", "bytes": 1, "sha256": daemon},
             bootstrap_module.CANDIDATE_EXPORTER: {"kind": "exporter", "bytes": 1, "sha256": exporter},
             "linux/bzImage": {"kind": "kernel", "bytes": 1, "sha256": "7" * 64}}
    return json.dumps({"version": "homelabfs-controlled-artifacts/1", "files": files}, sort_keys=True).encode()


# Two s64 candidates (patched into ADMITTED and FD_ATTR by s64_candidates),
# so a plan can name one while its run installs or runs parts of another.
CANDIDATE = hashlib.sha256(manifest_raw()).hexdigest()
CANDIDATE_B = hashlib.sha256(manifest_raw(exporter=EXPORTER_B)).hexdigest()
CANDIDATE_PARTS = {CANDIDATE: {}, CANDIDATE_B: {"exporter": EXPORTER_B}}


def write_private(path, raw):
    path.write_bytes(raw)
    path.chmod(0o600)
    return hashlib.sha256(raw).hexdigest()


def fd_attr_change(root, daemon=DAEMON, exporter=EXPORTER, runtime=None, config_exporter=None, installed=None,
                   named=None, **kernel_changes):
    """Bootstrap plan change: the three bl4/nl4 fields and their local inputs.

    The candidate package's manifest.json lists `daemon` and `exporter`; the
    plan runs `runtime` (runtime.sha256, default `daemon`) and the runtime
    config's Exporter.SHA256 is `config_exporter` (default `exporter`). The
    workstation copy of the G5 kernel plan installs `installed` (default the
    manifest's digest); `kernel.plan_sha256` and `prep.config_sha256` pin the
    written bytes, as Phase 0 does. `named` is fd_attr_candidate_sha256
    (default the manifest's digest); `kernel_changes` edit the kernel plan.
    """
    def change(plan):
        manifest = manifest_raw(daemon, exporter)
        digest = write_private(root / "candidate-manifest.json", manifest)
        config = json.loads(Path(plan["prep"]["config"]).read_bytes())
        config["Exporter"] = {"Path": "/var/tmp/run/bin/bhyve", "Args": [], "ControlledBootstrap": True,
                              "SHA256": list(bytes.fromhex(config_exporter or exporter))}
        plan["prep"]["config_sha256"] = write_private(Path(plan["prep"]["config"]), json.dumps(config).encode())
        plan["runtime"]["sha256"] = runtime or daemon
        kernel = {"version": rocky_kernel.PLAN_VERSION, "run_id": plan["run_id"],
                  "manifest_sha256": installed or digest, "kernel_release": plan["kernel"]["release"],
                  **kernel_changes}
        plan["kernel"]["plan_sha256"] = write_private(root / "kernel-plan.json", json.dumps(kernel).encode())
        plan.update(fd_attr_candidate_sha256=named or digest,
                    fd_attr_candidate_manifest=str(root / "candidate-manifest.json"),
                    fd_attr_kernel_plan=str(root / "kernel-plan.json"))
    return change


@contextlib.contextmanager
def s64_candidates(*modules):
    """ADMITTED and FD_ATTR of each controller instance hold CANDIDATE and
    CANDIDATE_B (plus the real ADMITTED in FD_ATTR, for batch templates)."""
    with contextlib.ExitStack() as stack:
        for module in modules:
            stack.enter_context(mock.patch.object(module, "ADMITTED", module.ADMITTED | set(CANDIDATE_PARTS)))
            stack.enter_context(mock.patch.object(module, "FD_ATTR", frozenset({ADMITTED, *CANDIDATE_PARTS})))
        yield


class BootstrapPlanTests(test_bootstrap.Case):
    def fd_attr_plan(self, **arguments):
        return fd_attr_change(self.root, **arguments)

    def mount_args(self):
        return [options for _, action, options, _ in self.world.calls if action == "guest-mount"]

    def assert_refused_before_launch(self, change, text):
        """Through run, on a fresh root and a fresh World per call: refused
        before the journal or any remote call. `change(root)` returns the
        plan change. The World is not self.world, so one subtest's calls
        cannot make the next one's `calls == []` fail (ol4)."""
        root = Path(tempfile.mkdtemp(dir=self.root))
        world = test_bootstrap.World(root)
        path, digest = test_bootstrap.plan_fixture(root, change(root))
        with self.assertRaisesRegex(test_bootstrap.bootstrap.Refused, text):
            test_bootstrap.bootstrap.run(path, digest, root / "out", boundaries=world.boundaries)
        self.assertEqual(world.calls, [])
        self.assertFalse((root / "out").exists())

    def test_plan_without_candidate_mounts_without_the_option(self):
        self.assertEqual(self.run_plan(), 0)
        self.assertTrue(all("--fd-attr" not in options for options in self.mount_args()))

    def test_pre_s64_candidate_refuses_before_launch(self):
        # ADMITTED is in FD_ATTR now; with FD_ATTR emptied it stands for an
        # admitted pre-s64 candidate, next to the superseded 1428d4db.
        with mock.patch.object(test_bootstrap.bootstrap.controller, "FD_ATTR", frozenset()):
            self.pre_s64_candidates_refuse()

    def pre_s64_candidates_refuse(self):
        for number, value in enumerate([ADMITTED, SUPERSEDED[-1], "a" * 64, "nothex"]):
            with self.subTest(value=value):
                root = self.root / str(number)
                root.mkdir()
                path, digest = test_bootstrap.plan_fixture(root, fd_attr_change(root, named=value))
                with self.assertRaisesRegex(test_bootstrap.bootstrap.Refused, "fd_attr_candidate_sha256"):
                    test_bootstrap.bootstrap.load_plan(path, digest)
        self.assert_refused_before_launch(lambda root: fd_attr_change(root, named=ADMITTED), "predates s64")

    def test_s64_candidate_mounts_with_the_option(self):
        with s64_candidates(test_bootstrap.bootstrap.controller):
            self.assertEqual(self.run_plan(self.fd_attr_plan()), 0)
        self.assertEqual([options.get("--fd-attr") for options in self.mount_args()], ["on", "on"])

    def test_named_candidate_must_be_the_one_the_kernel_plan_installs(self):
        """nl4: an s64 name on a run that installs another candidate refuses."""
        with s64_candidates(test_bootstrap.bootstrap.controller):
            self.assert_refused_before_launch(lambda root: fd_attr_change(root, installed=CANDIDATE_B),
                                              "not the candidate the kernel plan installs")
            # The cross-check binds the name, not just "some s64 candidate":
            # naming and installing CANDIDATE_B (its own parts) passes.
            self.assertEqual(self.run_plan(self.fd_attr_plan(exporter=EXPORTER_B)), 0)

    def test_daemon_and_exporter_must_be_the_candidates(self):
        """nl4 review: the r21 pre-s64 daemon or exporter under an s64 name
        refuses (runtime.sha256 and the config's Exporter.SHA256 against the
        named package manifest)."""
        for arguments, text in [({"runtime": R21_DAEMON}, "runtime.sha256 .* not the candidate's daemon"),
                                ({"config_exporter": R21_EXPORTER}, "Exporter.SHA256 .* not the candidate's exporter")]:
            with self.subTest(text=text), s64_candidates(test_bootstrap.bootstrap.controller):
                self.assert_refused_before_launch(lambda root: fd_attr_change(root, **arguments), text)

    def test_staged_f3_config_must_be_the_checked_one(self):
        """ol4: load_plan checks the workstation runtime config's exporter;
        the daemon launches with f3's staged runtime.config. A staged copy
        naming the r21 exporter differs from prep.config_sha256, so host
        preflight refuses it and the runtime never starts."""
        def change(plan):
            self.fd_attr_plan()(plan)  # the workstation copy names the s64 exporter
            config = json.loads(Path(plan["prep"]["config"]).read_bytes())
            config["Exporter"]["SHA256"] = list(bytes.fromhex(R21_EXPORTER))
            self.world.f3_config = json.dumps(config).encode()
        with s64_candidates(test_bootstrap.bootstrap.controller):
            self.assertEqual(self.run_plan(change), 1)
        self.assert_stopped("host-preflight", "failed", "pinned file bytes differ")
        self.assertFalse(self.world.launched)

    def test_candidate_manifest_must_be_the_named_one(self):
        def changed(root):
            def change(plan):
                fd_attr_change(root)(plan)
                with open(plan["fd_attr_candidate_manifest"], "ab") as stream:
                    stream.write(b" ")
            return change
        with s64_candidates(test_bootstrap.bootstrap.controller):
            self.assert_refused_before_launch(changed, "fd_attr_candidate_manifest: pinned input changed")

    def test_kernel_plan_copy_must_be_the_pinned_one(self):
        def changed_copy(root):
            def change(plan):
                fd_attr_change(root)(plan)
                with open(plan["fd_attr_kernel_plan"], "ab") as stream:
                    stream.write(b" ")
            return change
        for change, text in [(changed_copy, "fd_attr_kernel_plan: pinned input changed"),
                             (lambda root: fd_attr_change(root, kernel_release="6.1.190-hfs-other"),
                              "kernel_release"),
                             (lambda root: fd_attr_change(root, run_id="zq3-r9"), "run_id"),
                             (lambda root: fd_attr_change(root, version="other/1"), "not a G5 kernel plan")]:
            with self.subTest(text=text), s64_candidates(test_bootstrap.bootstrap.controller):
                self.assert_refused_before_launch(change, text)

    def test_all_fields_or_none(self):
        def without(name):
            def factory(root):
                def change(plan):
                    fd_attr_change(root)(plan)
                    del plan[name]
                return change
            return factory
        for name in test_bootstrap.bootstrap.FD_ATTR_FIELDS:
            with self.subTest(dropped=name), s64_candidates(test_bootstrap.bootstrap.controller):
                self.assert_refused_before_launch(without(name), "closed keys")

    def test_kernel_plan_version_is_rocky_kernels(self):
        self.assertEqual(test_bootstrap.bootstrap.KERNEL_PLAN_VERSION, rocky_kernel.PLAN_VERSION)

    def test_shared_options_agree(self):
        remote = test_bootstrap.module("bootstrap_remote_fd_attr", "bootstrap_remote.py")
        self.assertEqual(remote.MOUNT_OPTIONS_FD_ATTR, test_bootstrap.bootstrap.MOUNT_OPTIONS_FD_ATTR)
        self.assertEqual(remote.MOUNT_OPTIONS_FD_ATTR,
                         remote.MOUNT_OPTIONS.replace("cache=mmap", "cache=mmap,hfs_fdattr"))


class FillPlanTests(test_fill_plan.BatchCase):
    """fill_plan's batch fill: the bootstrap plan's request, the mounts and
    the template's fd_attr and candidate must all agree."""

    def run_with(self, shown, fd_attr_template, named=None, template_candidate=ADMITTED):
        """`named`: the bootstrap plan's fd_attr_candidate_sha256 (None: no
        fd_attr fields); `shown`: each mount's super options."""
        def rows(world):
            ids = [90, 91]

            def mount(options, payload):
                result = world.on_guest_mount(options, payload)
                result["mountinfo"] = ROW.format(ids.pop(0), 55, options["--mountpoint"], options["--tag"],
                                                 shown.pop(0)).rstrip("\n")
                return result
            world.overrides["guest-mount"] = mount
        plan_change = fd_attr_change(self.root, **CANDIDATE_PARTS[named]) if named else None
        with self.candidates():
            _, plan_sha256 = self.bootstrap_run(world_change=rows, plan_change=plan_change)

        def change(plan):
            plan["candidate_manifest_sha256"] = template_candidate
            if fd_attr_template:
                plan["fd_attr"] = True
                for request in plan["requests"]:
                    request["fd_attr"] = True
        template, template_sha256 = test_fill_plan.batch_template(self.root, change)
        with self.candidates():
            return self.fill(*self.argv(plan_sha256, template, template_sha256))

    def candidates(self):
        """Both controller instances (the bootstrap run's and fill_plan's)."""
        return s64_candidates(test_fill_plan.fill.controller, test_fill_plan.world_fixtures.bootstrap.controller)

    def test_plain_mounts_fill_a_plain_template(self):
        code, report = self.run_with([PLAIN, PLAIN], False)
        self.assertEqual((code, report["fd_attr"]), (0, False), report)

    def test_fd_attr_mounts_fill_an_fd_attr_template(self):
        code, report = self.run_with([SHOWN, SHOWN], True, named=CANDIDATE, template_candidate=CANDIDATE)
        self.assertEqual((code, report["fd_attr"]), (0, True), report)
        plan = json.loads(self.out.read_bytes())
        self.assertTrue(plan["fd_attr"] and all(request["fd_attr"] for request in plan["requests"]))

    def test_disagreement_refuses(self):
        for shown, template, named, candidate, text in [
                ([PLAIN, PLAIN], True, None, ADMITTED, "template fd_attr set"),
                ([SHOWN, SHOWN], False, CANDIDATE, CANDIDATE, "template fd_attr absent"),
                ([SHOWN, PLAIN], False, None, ADMITTED, "both show hfs_fdattr"),
                # nl4: the bootstrap plan's request and the mounts disagree.
                ([SHOWN, SHOWN], True, None, ADMITTED, "bootstrap plan names no fd_attr candidate"),
                ([PLAIN, PLAIN], False, CANDIDATE, ADMITTED, "bootstrap plan names fd_attr candidate"),
                # nl4: the batch would run another candidate than the bootstrap installed.
                ([SHOWN, SHOWN], True, CANDIDATE_B, CANDIDATE, "candidate_manifest_sha256"),
                ([SHOWN, SHOWN], True, CANDIDATE, ADMITTED, "candidate_manifest_sha256")]:
            with self.subTest(text=text, named=named, candidate=candidate):
                self.root = Path(tempfile.mkdtemp(dir=self.root))
                self.out = self.root / "filled.json"
                code, message = self.run_with(list(shown), template, named, candidate)
                self.assertEqual(code, 2, message)
                self.assertIn(text, message)
                self.assertFalse(os.path.lexists(self.out))


class FdAttrRetentionTests(unittest.TestCase):
    """Retention.retain and release of an fd_attr metadata payload.

    Real local syscalls as in test_preparation.GuestRetentionTests: the domain
    root on one filesystem, the root-private run directory on another. m0 and
    m1 are two fds of one directory so the case's peer rename through m1 is
    seen through m0, as both 9p domains see one export. Local logic only.
    """

    def setUp(self):
        domains = tempfile.TemporaryDirectory(dir="/dev/shm" if os.path.isdir("/dev/shm") else None)
        local = tempfile.TemporaryDirectory()
        self.addCleanup(domains.cleanup)
        self.addCleanup(local.cleanup)
        self.run_dir = Path(local.name) / "hfs-zq3-test"
        self.run_dir.mkdir(mode=0o700)
        self.domain = Path(domains.name) / "m0"
        self.domain.mkdir()
        if os.stat(self.domain).st_dev == os.stat(self.run_dir).st_dev:
            self.skipTest("needs two distinct local filesystems")
        self.roots = [os.open(self.domain, os.O_RDONLY | os.O_DIRECTORY) for _ in range(2)]
        for fd in self.roots:
            self.addCleanup(os.close, fd)
        for name, value in [("RUN_PARENT", str(Path(local.name) / "hfs-zq3-")), ("TRUSTED_UID", os.geteuid())]:
            patcher = mock.patch.object(guest, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        self.request = with_fd_attr(case_request("metadata"))
        self.leaf = self.domain / "zq3-test-metadata"

    def events(self, action):
        printed = io.StringIO()
        with contextlib.redirect_stdout(printed):
            action()
        return [json.loads(line) for line in printed.getvalue().splitlines()]

    def run_case(self, retain_request=None):
        """The fd_attr metadata case, then retain under `retain_request`."""
        self.events(lambda: guest.metadata(guest.Probe(self.request, self.roots)))
        events = self.events(lambda: guest.Retention(guest.Probe(retain_request or self.request, self.roots)).retain())
        return next(event for event in events if event["event"] == "payload-retained")

    def release(self, retained, **changes):
        request = {**self.request, "version": guest.RELEASE_VERSION, "token": retained["release_token"],
                   "inventory_sha256": retained["inventory_sha256"], "correlation_sha256": "c" * 64}
        request = {key: value for key, value in {**request, **changes}.items() if value is not None}
        guest.is_release(request)
        return self.events(lambda: guest.Retention(guest.Probe(request, self.roots)).release())

    def test_retain_lists_peer_renamed(self):
        retained = self.run_case()
        self.assertEqual(retained["inventory"]["listing"], ["linked", "peer-renamed", "renamed"])
        self.assertEqual([entry["name"] for entry in retained["inventory"]["entries"]],
                         ["renamed", "linked", "peer-renamed"])

    def test_retain_refuses_a_listing_that_differs_from_the_request(self):
        """A plain request does not expect peer-renamed; an extra entry
        beside it is refused on an fd_attr request too."""
        with self.assertRaisesRegex(guest.Refused, "unexpected payload entries"):
            self.run_case(retain_request=case_request("metadata"))
        (self.leaf / "stray").write_bytes(b"")
        with self.assertRaisesRegex(guest.Refused, "unexpected payload entries"):
            self.events(lambda: guest.Retention(guest.Probe(self.request, self.roots)).retain())
        self.assertFalse((self.run_dir / "release-metadata.json").exists())

    def test_release_unlinks_peer_renamed_before_the_leaf_rmdir(self):
        events = self.release(self.run_case())
        invoked = [(event["operation"], event["args"].get("name")) for event in events
                   if event["event"] == "invoke" and event["operation"] != "open-leaf"]
        self.assertEqual(invoked, [("unlink-released-payload", "renamed"), ("unlink-released-payload", "linked"),
                                   ("unlink-released-payload", "peer-renamed"),
                                   ("rmdir-released-leaf", "zq3-test-metadata")])
        self.assertEqual(events[-1]["event"], "payload-released")
        self.assertFalse(self.leaf.exists())
        self.assertTrue((self.run_dir / "released-metadata.json").exists())

    def test_release_without_fd_attr_refuses_and_preserves(self):
        """A release that drops the case's fd_attr would leave peer-renamed
        behind (and its rmdir fail after the record is consumed); the
        inventory check refuses it first and keeps everything."""
        retained = self.run_case()
        with self.assertRaisesRegex(guest.Refused, "inventory changed"):
            self.release(retained, fd_attr=None)
        self.assertEqual(sorted(os.listdir(self.leaf)), ["linked", "peer-renamed", "renamed"])
        self.assertTrue((self.run_dir / "release-metadata.json").exists())
        self.assertEqual(self.release(retained)[-1]["event"], "payload-released")


if __name__ == "__main__":
    unittest.main()
