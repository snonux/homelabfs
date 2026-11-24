#!/usr/bin/env python3
"""Local tests of the bl4 hfs_fdattr gate and the peer-rename guest check.

s64 lets GETATTR and SETATTR go through the open file description when the
guest mounts with `hfs_fdattr`. Only a candidate whose guest kernel, lib9p
exporter and daemon all carry s64 may mount with it (controller.FD_ATTR,
empty until the next candidate built from main is admitted), so every test
of the enabled path patches FD_ATTR. Both directions are covered: a plan
without fd_attr keeps its exact pre-bl4 shape and refuses a mount that
shows the option, and a plan with it refuses an older candidate and a mount
that does not show it.

The metadata case's peer rename (fd open on m0, rename through m1, then
stat(old name) ENOENT and fstat(fd) on the same file) runs here on one local
directory opened twice, as test_guest_alarm.py's cases do. That proves the
case's logic and its payload bookkeeping, not the guest kernel: the s64
fallback it guards only exists in a guest VM (zq3 T3).
No guest, 9p, SSH, VM or lab.
"""
import contextlib
import errno
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

ROW = "{} 1 0:{} / {} rw,relatime - 9p {} {}\n"
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

    def test_no_candidate_carries_s64_yet(self):
        # The r21 candidate predates s64; nothing superseded may ever be added.
        self.assertEqual(controller.FD_ATTR, frozenset())
        self.assertTrue(controller.FD_ATTR <= controller.ADMITTED)
        for value in [ADMITTED, *SUPERSEDED]:
            with self.subTest(candidate=value), self.assertRaises(controller.Refused):
                controller.require_fd_attr(value)

    def test_plan_without_fd_attr_keeps_its_shape(self):
        controller.validate_shape(self.plan(fd_attr=None, requests=False))

    def test_fd_attr_plan_refuses_a_pre_s64_candidate(self):
        with self.assertRaisesRegex(controller.Refused, "predates s64"):
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


class BootstrapPlanTests(test_bootstrap.Case):
    def fd_attr_plan(self, value=ADMITTED):
        return lambda plan: plan.update(fd_attr_candidate_sha256=value)

    def mount_args(self):
        return [options for _, action, options, _ in self.world.calls if action == "guest-mount"]

    def test_plan_without_candidate_mounts_without_the_option(self):
        self.assertEqual(self.run_plan(), 0)
        self.assertTrue(all("--fd-attr" not in options for options in self.mount_args()))

    def test_pre_s64_candidate_refuses_before_launch(self):
        for number, value in enumerate([ADMITTED, SUPERSEDED[-1], "a" * 64, "nothex"]):
            with self.subTest(value=value):
                root = self.root / str(number)
                root.mkdir()
                path, digest = test_bootstrap.plan_fixture(root, self.fd_attr_plan(value))
                with self.assertRaisesRegex(test_bootstrap.bootstrap.Refused, "fd_attr_candidate_sha256"):
                    test_bootstrap.bootstrap.load_plan(path, digest)
        # Through run: refused before the journal or any remote call.
        with self.assertRaisesRegex(test_bootstrap.bootstrap.Refused, "predates s64"):
            self.run_plan(self.fd_attr_plan())
        self.assertEqual(self.world.calls, [])
        self.assertFalse(self.output.exists())

    def test_s64_candidate_mounts_with_the_option(self):
        with fd_attr_candidates(test_bootstrap.bootstrap.controller):
            self.assertEqual(self.run_plan(self.fd_attr_plan()), 0)
        self.assertEqual([options.get("--fd-attr") for options in self.mount_args()], ["on", "on"])

    def test_shared_options_agree(self):
        remote = test_bootstrap.module("bootstrap_remote_fd_attr", "bootstrap_remote.py")
        self.assertEqual(remote.MOUNT_OPTIONS_FD_ATTR, test_bootstrap.bootstrap.MOUNT_OPTIONS_FD_ATTR)
        self.assertEqual(remote.MOUNT_OPTIONS_FD_ATTR,
                         remote.MOUNT_OPTIONS.replace("cache=mmap", "cache=mmap,hfs_fdattr"))


class FillPlanTests(test_fill_plan.BatchCase):
    """fill_plan's batch fill: template fd_attr must match the mounts."""

    def run_with(self, shown, fd_attr_template):
        def rows(world):
            ids = [90, 91]

            def mount(options, payload):
                result = world.on_guest_mount(options, payload)
                result["mountinfo"] = ROW.format(ids.pop(0), 55, options["--mountpoint"], options["--tag"],
                                                 shown.pop(0)).rstrip("\n")
                return result
            world.overrides["guest-mount"] = mount
        _, plan_sha256 = self.bootstrap_run(world_change=rows)

        def change(plan):
            if fd_attr_template:
                plan["fd_attr"] = True
                for request in plan["requests"]:
                    request["fd_attr"] = True
        template, template_sha256 = test_fill_plan.batch_template(self.root, change)
        return self.fill(*self.argv(plan_sha256, template, template_sha256))

    def test_plain_mounts_fill_a_plain_template(self):
        code, report = self.run_with([PLAIN, PLAIN], False)
        self.assertEqual((code, report["fd_attr"]), (0, False), report)

    def test_fd_attr_mounts_fill_an_fd_attr_template(self):
        with fd_attr_candidates(test_fill_plan.fill.controller):
            code, report = self.run_with([SHOWN, SHOWN], True)
        self.assertEqual((code, report["fd_attr"]), (0, True), report)
        plan = json.loads(self.out.read_bytes())
        self.assertTrue(plan["fd_attr"] and all(request["fd_attr"] for request in plan["requests"]))

    def test_disagreement_refuses(self):
        for shown, template, text in [([PLAIN, PLAIN], True, "template fd_attr set"),
                                      ([SHOWN, SHOWN], False, "template fd_attr absent"),
                                      ([SHOWN, PLAIN], False, "both show hfs_fdattr")]:
            with self.subTest(text=text):
                self.root = Path(tempfile.mkdtemp(dir=self.root))
                self.out = self.root / "filled.json"
                code, message = self.run_with(list(shown), template)
                self.assertEqual(code, 2, message)
                self.assertIn(text, message)
                self.assertFalse(os.path.lexists(self.out))


if __name__ == "__main__":
    unittest.main()
