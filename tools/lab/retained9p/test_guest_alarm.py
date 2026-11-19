#!/usr/bin/env python3
"""Local tests of guest.py's SIGALRM-atomic descriptor ownership (y84).

hfs syscalls wait uninterruptibly, so an expired case/step alarm stays pending
and its handler raises Deadline as soon as os.open returns (zq3-r10 R10-1:
T1's root fd leaked until process exit). Here a fake os.open opens a real
local descriptor and queues SIGALRM before returning, which reproduces that
ordering without a guest: the descriptor must still reach a cleanup path and
the Deadline must still end the request. Also covers the admission-step
progress events. No guest, 9p, SSH or runtime.
"""
import contextlib
import io
import json
import os
import hashlib
import shutil
import signal
import sys
import tempfile
import types
import unittest
from unittest import mock

from test_lifecycle_steps import MOUNTS, boundary, mountinfo, step_request
from test_preparation import guest

REAL_OPEN = os.open


def is_open(fd):
    try:
        os.fstat(fd)
    except OSError:
        return False
    return True


class AlarmingOpen:
    """os.open stand-in: real open, then SIGALRM queued before it returns.

    trigger(path) selects which open delivers the alarm; every descriptor
    handed out is recorded so a test can prove none is left open.
    """

    def __init__(self, trigger=lambda path: True, redirect=None):
        self.trigger, self.redirect = trigger, redirect or (lambda path: path)
        self.opened = []

    def __call__(self, path, flags, mode=0o777, *, dir_fd=None):
        fd = REAL_OPEN(self.redirect(path), flags, mode, dir_fd=dir_fd)
        self.opened.append(fd)
        if self.trigger(path):
            os.kill(os.getpid(), signal.SIGALRM)
        return fd


class AlarmTestCase(unittest.TestCase):
    """Installs guest.deadline as the SIGALRM handler, as guest.main does."""

    def setUp(self):
        previous = signal.signal(signal.SIGALRM, guest.deadline)
        self.addCleanup(signal.signal, signal.SIGALRM, previous)
        self.addCleanup(signal.pthread_sigmask, signal.SIG_SETMASK,
                        signal.pthread_sigmask(signal.SIG_BLOCK, set()))
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.temp = temp.name

    def assert_all_closed(self, fake):
        self.assertTrue(fake.opened, "the fake open was never reached")
        self.assertEqual([fd for fd in fake.opened if is_open(fd)], [])

    def assert_alarm_unblocked(self):
        self.assertNotIn(signal.SIGALRM, signal.pthread_sigmask(signal.SIG_BLOCK, set()))


class OwnedDescriptorTests(AlarmTestCase):
    def test_open_registered_registers_before_deadline(self):
        fake, fds = AlarmingOpen(), []
        with mock.patch.object(guest.os, "open", fake), self.assertRaises(guest.Deadline):
            guest.open_registered(fds.append, self.temp, os.O_RDONLY | os.O_DIRECTORY)
        self.assertEqual(fds, fake.opened)
        self.assertTrue(is_open(fds[0]))
        os.close(fds[0])
        self.assert_alarm_unblocked()

    def test_owned_closes_descriptor_when_deadline_lands_on_open(self):
        fake = AlarmingOpen()
        with mock.patch.object(guest.os, "open", fake), self.assertRaises(guest.Deadline):
            with guest.owned(self.temp, os.O_RDONLY | os.O_DIRECTORY):
                self.fail("the body must not run after the deadline")
        self.assert_all_closed(fake)
        self.assert_alarm_unblocked()

    def test_open_failure_restores_mask_and_keeps_errno(self):
        fds = []
        with self.assertRaises(FileNotFoundError):
            guest.open_registered(fds.append, self.temp + "/missing", os.O_RDONLY)
        self.assertEqual(fds, [])
        self.assert_alarm_unblocked()

    def test_alarm_flagged_during_block_leaves_alarm_unblocked(self):
        """The handler raises from pthread_sigmask after the block took effect."""
        real = signal.pthread_sigmask

        def late(how, mask):
            previous = real(how, mask)
            if how == signal.SIG_BLOCK and mask == guest.ALARM:
                guest.deadline()
            return previous
        with mock.patch.object(guest.signal, "pthread_sigmask", late), \
                mock.patch.object(guest.os, "open") as opened, self.assertRaises(guest.Deadline):
            guest.open_registered([].append, self.temp, os.O_RDONLY)
        opened.assert_not_called()
        self.assert_alarm_unblocked()

    def test_owned_without_alarm_yields_and_closes(self):
        with guest.owned(self.temp, os.O_RDONLY | os.O_DIRECTORY) as fd:
            self.assertTrue(is_open(fd))
        self.assertFalse(is_open(fd))
        self.assert_alarm_unblocked()


def case_request(case="sync"):
    return {"version": guest.VERSION, "run_id": "test", "boot_id": "boot",
            "provider_nonce": "provider",
            "manifest_sha256": hashlib.sha256(b"manifest").hexdigest(),
            "mounts": MOUNTS, "case": case, "seconds": guest.REQUEST_SECONDS_MIN}


class AdmissionTests(AlarmTestCase):
    """open_domains/main with the guest kernel boundary substituted."""

    def setUp(self):
        super().setUp()
        for name in ["m0", "m1"]:
            os.mkdir(os.path.join(self.temp, name))
        lines = mountinfo([(row["path"], "9p", row["tag"], row["mount_id"]) for row in MOUNTS])
        ids = {}
        local = {row["path"]: os.path.join(self.temp, "m" + str(i)) for i, row in enumerate(MOUNTS)}
        self.redirect = lambda path: local.get(path, path)

        def fd_mount(fd):
            return ids[fd]

        def remember(fake):
            # The fake's descriptors map to the admitted mount IDs in order.
            original = fake.__call__

            def call(path, flags, mode=0o777, *, dir_fd=None):
                fd = original(path, flags, mode, dir_fd=dir_fd)
                ids[fd] = next(row["mount_id"] for row in MOUNTS if row["path"] == path)
                return fd
            return call

        self.remember = remember
        self.patches = [
            mock.patch.object(guest, "read", side_effect=lambda path, _: boundary(lines)[path]),
            mock.patch.object(guest.os, "geteuid", return_value=0),
            mock.patch.object(guest.os.path, "realpath", side_effect=lambda path: path),
            mock.patch.object(guest, "fd_mount", side_effect=fd_mount),
            mock.patch.object(guest.os, "fstat", side_effect=lambda fd: types.SimpleNamespace(st_dev=fd)),
            mock.patch.object(guest.os, "readlink", return_value="user:[1]"),
        ]

    def run_admit(self, fake):
        roots, printed = [], io.StringIO()
        with contextlib.ExitStack() as stack:
            for patch in self.patches:
                stack.enter_context(patch)
            stack.enter_context(mock.patch.object(guest.os, "open", self.remember(fake)))
            stack.enter_context(contextlib.redirect_stdout(printed))
            try:
                guest.admit(case_request(), roots)
                failure = None
            except guest.Deadline as raised:
                failure = raised
        return roots, failure, [json.loads(line) for line in printed.getvalue().splitlines()]

    def test_admission_emits_each_step_in_order(self):
        fake = AlarmingOpen(trigger=lambda path: False, redirect=self.redirect)
        roots, failure, events = self.run_admit(fake)
        self.assertIsNone(failure)
        self.assertEqual(roots, fake.opened)
        steps = [(e["event"], e.get("stage"), e.get("domain")) for e in events]
        self.assertEqual(steps, [
            ("admission-step", "identity-checked", None),
            ("admission-step", "open-root", 0), ("admission-step", "stat-root", 0),
            ("admission-step", "root-admitted", 0),
            ("admission-step", "open-root", 1), ("admission-step", "stat-root", 1),
            ("admission-step", "root-admitted", 1),
            ("admitted", None, None)])
        self.assertEqual([e["path"] for e in events if e.get("stage") == "open-root"],
                         [row["path"] for row in MOUNTS])
        self.assertTrue(all(e["version"] == guest.VERSION and isinstance(e["monotonic_ns"], int)
                            for e in events))
        for fd in roots:
            os.close(fd)

    def test_deadline_on_second_root_open_leaves_both_fds_owned(self):
        fake = AlarmingOpen(trigger=lambda path: path == MOUNTS[1]["path"], redirect=self.redirect)
        roots, failure, events = self.run_admit(fake)
        self.assertIsInstance(failure, guest.Deadline)
        # Both descriptors are in the caller's list, still open for main to close.
        self.assertEqual(roots, fake.opened)
        self.assertEqual(len(roots), 2)
        self.assertTrue(all(is_open(fd) for fd in roots))
        self.assertEqual((events[-1]["stage"], events[-1]["domain"]), ("open-root", 1))
        for fd in roots:
            os.close(fd)

    def test_main_closes_root_fd_when_deadline_lands_on_open(self):
        fake = AlarmingOpen(redirect=self.redirect)
        stdin = io.TextIOWrapper(io.BytesIO(json.dumps(case_request()).encode()))
        printed = io.StringIO()
        with contextlib.ExitStack() as stack:
            for patch in self.patches:
                stack.enter_context(patch)
            stack.enter_context(mock.patch.object(guest.os, "open", self.remember(fake)))
            stack.enter_context(mock.patch.object(sys, "argv", ["guest"]))
            stack.enter_context(mock.patch.object(sys, "stdin", stdin))
            stack.enter_context(contextlib.redirect_stdout(printed))
            code = guest.main()
        events = [json.loads(line) for line in printed.getvalue().splitlines()]
        self.assertEqual(code, 1)
        self.assertEqual((events[-2]["event"], events[-2]["error"]), ("failure", "Deadline"))
        self.assertEqual((events[-1]["event"], events[-1]["syscall_success"]), ("terminal", False))
        self.assertNotIn("admitted", [event["event"] for event in events])
        self.assertEqual(len(fake.opened), 1)
        self.assert_all_closed(fake)
        self.assert_alarm_unblocked()


class DeadlineBoundTests(unittest.TestCase):
    """be4 (zq3-r14 R14-1): admission alarm < request deadline < dispatch bound."""

    def test_guest_and_controller_bounds_agree_and_are_ordered(self):
        from test_preparation import controller
        self.assertEqual(guest.ADMISSION_SECONDS, controller.ADMISSION_SECONDS)
        self.assertEqual((guest.REQUEST_SECONDS_MIN, guest.REQUEST_SECONDS_MAX),
                         (controller.REQUEST_SECONDS_MIN, controller.REQUEST_SECONDS_MAX))
        # One worst-case hfs syscall is about 90-120 s (docs/Test-automation.md).
        self.assertGreaterEqual(guest.ADMISSION_SECONDS, 120 + 30)
        self.assertLess(guest.ADMISSION_SECONDS, guest.REQUEST_SECONDS_MIN)
        self.assertLessEqual(guest.REQUEST_SECONDS_MIN, controller.STEP_SECONDS)
        self.assertLessEqual(controller.STEP_SECONDS, guest.REQUEST_SECONDS_MAX)
        plan = {"seconds": controller.PLAN_SECONDS_MIN}
        for seconds in [guest.REQUEST_SECONDS_MIN, controller.STEP_SECONDS, guest.REQUEST_SECONDS_MAX]:
            self.assertEqual(controller.dispatch_seconds(plan, {"seconds": seconds}),
                             seconds + controller.DISPATCH_MARGIN)

    def test_limit_arms_what_is_left_of_the_whole_deadline(self):
        request = case_request()
        armed = []
        with mock.patch.object(guest.signal, "alarm", armed.append), \
                mock.patch.object(guest.resource, "setrlimit"), \
                mock.patch.object(guest.time, "monotonic", return_value=1000.0):
            guest.limit(request, 1000.0 - 14.2)  # zq3-r14's T1 admission took ~14 s.
            guest.limit(request, 1000.0 - request["seconds"] - 5)  # Overrun: floor.
        self.assertEqual(armed, [request["seconds"] - 14, 1])

    def test_admission_bound_refuses_short_or_long_deadlines(self):
        for seconds in [10, 60, guest.REQUEST_SECONDS_MIN - 1, guest.REQUEST_SECONDS_MAX + 1]:
            request = dict(case_request(), seconds=seconds)
            with self.subTest(seconds=seconds), self.assertRaisesRegex(guest.Refused, "case deadline bound"):
                guest.admit_identity(request)

    def test_main_arms_the_admission_alarm_first(self):
        armed = []
        stdin = io.TextIOWrapper(io.BytesIO(b"not json"))
        with mock.patch.object(guest.signal, "alarm", armed.append), \
                mock.patch.object(guest.signal, "signal"), \
                mock.patch.object(sys, "argv", ["guest"]), mock.patch.object(sys, "stdin", stdin), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(guest.main(), 1)
        self.assertEqual(armed, [guest.ADMISSION_SECONDS, 0])


class CaseDescriptorTests(AlarmTestCase):
    """Real local case syscalls; m0 and m1 are two fds on one local export.

    Both roots open the same directory, as both 9p domains see one export, so
    the cases' cross-domain reads through roots[1] find what m0 wrote.
    """

    def setUp(self):
        super().setUp()
        os.mkdir(os.path.join(self.temp, "m0"))
        self.roots = [REAL_OPEN(os.path.join(self.temp, "m0"), os.O_RDONLY | os.O_DIRECTORY)
                      for _ in range(2)]
        for fd in self.roots:
            self.addCleanup(os.close, fd)

    def run_case(self, case, trigger):
        fake = AlarmingOpen(trigger=trigger)
        probe = guest.Probe(case_request(case), self.roots)
        with mock.patch.object(guest.os, "open", fake), \
                contextlib.redirect_stdout(io.StringIO()), self.assertRaises(guest.Deadline):
            guest.CASES[case](probe)
        self.assert_all_closed(fake)
        self.assert_alarm_unblocked()
        return fake

    def test_deadline_on_leaf_open_closes_leaf(self):
        fake = self.run_case("sync", lambda path: path == "zq3-test-sync")
        self.assertEqual(len(fake.opened), 1)

    def test_deadline_on_payload_open_closes_payload_and_leaf(self):
        for case, name in [("credentials", "group"), ("sync", "dsync"), ("sync", "mapped"), ("metadata", "original"),
                           ("metadata", "unlinked"), ("namespace", "namespace"),
                           ("mapped-pressure", "full")]:
            with self.subTest(case=case, name=name):
                leaf = os.path.join(self.temp, "m0", "zq3-test-" + case)
                try:
                    fake = self.run_case(case, lambda path, n=name: path == n)
                    self.assertGreaterEqual(len(fake.opened), 2)
                finally:
                    # A failing subtest must not leave the leaf (EEXIST next time).
                    shutil.rmtree(leaf, ignore_errors=True)

    def test_deadline_on_cross_domain_canary_open_closes_all(self):
        fake = self.run_case("mapped-pressure", lambda path: path.endswith("/full"))
        self.assertEqual(len(fake.opened), 3)

    def test_deadline_on_quiesce_root_open_records_timeout_and_closes(self):
        fake = AlarmingOpen()
        probe = guest.Probe(step_request("quiesce"), [])
        printed = io.StringIO()
        mount = {"index": 0, "path": os.path.join(self.temp, "m0"), "present": True}
        with mock.patch.object(guest.os, "open", fake), contextlib.redirect_stdout(printed), \
                self.assertRaises(guest.Deadline):
            guest.sync_domain(probe, mount)
        last = json.loads(printed.getvalue().splitlines()[-1])
        self.assertEqual((last["event"], last["operation"], last["timeout"]),
                         ("reply", "open-root-quiesce", True))
        self.assert_all_closed(fake)

    def test_deadline_on_after_loss_create_closes_marker(self):
        fake = AlarmingOpen(trigger=lambda path: path.endswith("after-loss"))
        probe = guest.Probe(step_request("after-loss"), [])
        mounts = [{"index": 0, "path": "", "present": False},
                  {"index": 1, "path": os.path.join(self.temp, "m0"), "present": True}]
        with mock.patch.object(guest.os, "open", fake), \
                contextlib.redirect_stdout(io.StringIO()), self.assertRaises(guest.Deadline):
            guest.after_loss(probe, mounts)
        self.assertEqual(len(fake.opened), 1)
        self.assert_all_closed(fake)
        # The marker stays as evidence; only its descriptor is closed.
        self.assertTrue(os.path.exists(os.path.join(self.temp, "m0", "zq3-test-after-loss")))

    def test_deadline_on_retention_opens_closes_them(self):
        """Run directory, record create and record read each close on a deadline."""
        os.mkdir(os.path.join(self.temp, "local"))
        run = os.path.join(self.temp, "local", "hfs-zq3-test")
        os.mkdir(run, 0o700)
        for name, value in [("RUN_PARENT", os.path.join(self.temp, "local", "hfs-zq3-")),
                            ("TRUSTED_UID", os.geteuid())]:
            patcher = mock.patch.object(guest, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        # No roots: the stand-in run directory shares the temp filesystem with
        # m0, which run_directory would rightly refuse as a tested domain.
        retention = guest.Retention(guest.Probe(case_request("sync"), []))
        for target, action in [(run, lambda: retention.write_record({"version": "x"})),
                               ("release-sync.json", lambda: retention.write_record({"version": "x"}))]:
            with self.subTest(target=target):
                fake = AlarmingOpen(trigger=lambda path, t=target: path == t)
                with mock.patch.object(guest.os, "open", fake), self.assertRaises(guest.Deadline):
                    action()
                self.assert_all_closed(fake)
        with open(os.path.join(run, "release-sync.json"), "rb") as stream:
            self.assertEqual(stream.read(), b"")
        os.chmod(os.path.join(run, "release-sync.json"), 0o600)
        fake = AlarmingOpen(trigger=lambda path: path == "release-sync.json")

        def read_record():
            with retention.run_directory() as fd:
                return retention.read_record(fd)
        with mock.patch.object(guest.os, "open", fake), self.assertRaises(guest.Deadline):
            read_record()
        self.assertEqual(len(fake.opened), 2)
        self.assert_all_closed(fake)

    def test_quiesce_root_without_alarm_records_open_and_syncfs(self):
        probe = guest.Probe(step_request("quiesce"), [])
        mount = {"index": 0, "path": os.path.join(self.temp, "m0"), "present": True}
        with contextlib.redirect_stdout(io.StringIO()):
            row = guest.sync_domain(probe, mount)
        self.assertEqual(row, {"index": 0, "open_errno": 0, "syncfs_errno": 0})


if __name__ == "__main__":
    unittest.main()
