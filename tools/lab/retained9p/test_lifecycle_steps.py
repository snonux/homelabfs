#!/usr/bin/env python3
"""Local tests of the zq3 T9-T11 guest lifecycle steps and their dispatch (G10, G11).

No guest, 9p, SSH or runtime: guest steps run real local syscalls on temporary
directories standing in for the two domains, while the guest kernel boundary
(identity files, mountinfo, a dead transport's errno, drop_caches) is
substituted explicitly. Controller dispatch uses a guest-shaped stand-in.
"""
import contextlib
import errno
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

from test_preparation import batch_fixture, controller, guest, guest_stdout

MOUNTS = [{"path": "/var/tmp/hfs-zq3-test/m" + str(i), "tag": "hfs" + str(i),
           "mount_id": i + 10, "session": "session" + str(i)} for i in range(2)]


def step_request(step, mounts=None):
    return {"version": guest.STEP_VERSION, "run_id": "test", "boot_id": "boot",
            "provider_nonce": "provider", "manifest_sha256": controller.digest(b"manifest"),
            "mounts": mounts or MOUNTS, "step": step, "seconds": 30}


def mountinfo(rows):
    """mountinfo lines for (path, fstype, tag, mount_id) tuples."""
    return "".join("{} 1 0:{} / {} rw,relatime - {} {} rw,cache=mmap,version=9P2000.L.hfs1.4,msize=65536\n"
                   .format(mount_id, mount_id, path, kind, tag)
                   for path, kind, tag, mount_id in rows).encode()


def boundary(mount_lines):
    """Substituted guest kernel/identity files read by step admission."""
    admission = {"boot_id": "boot", "provider_nonce": "provider",
                 "domains": [{"session": "session0"}, {"session": "session1"}]}
    return {"/proc/sys/kernel/random/boot_id": b"boot\n",
            "/etc/homelabfs/installed-admission-v1.json": b"manifest",
            "/sys/fs/selinux/enforce": b"1\n",
            "/sys/kernel/homelabfs/admission_v1": json.dumps(admission).encode(),
            "/proc/self/mountinfo": mount_lines}


def step_stdout(request, observation, success=True, **terminal):
    """Guest-shaped step evidence for local controller/correlator tests."""
    events = [{"event": "admitted-step", "step": request["step"], "boot_id": request["boot_id"]}]
    if success:
        events.append({"event": "step-observed", "step": request["step"], "observation": observation,
                       "acceptance": False, "publication": None})
    events.append({"event": "terminal", "action": "step", "step": request["step"],
                   "syscall_success": success, "payload_retained": False, "payload_released": False,
                   "uncertain_children": [], "acceptance": False, "publication": None, **terminal})
    return "".join(json.dumps(event) + "\n" for event in events)


def quiet(function, *args):
    with contextlib.redirect_stdout(io.StringIO()):
        return function(*args)


def captured(function, *args):
    printed = io.StringIO()
    with contextlib.redirect_stdout(printed):
        value = function(*args)
    return value, [json.loads(line) for line in printed.getvalue().splitlines()]


class StepAdmissionTests(unittest.TestCase):
    def admit(self, rows, step="quiesce"):
        with mock.patch.object(guest, "read", side_effect=lambda path, _: boundary(mountinfo(rows))[path]), \
                mock.patch.object(guest.os, "geteuid", return_value=0), \
                mock.patch.object(guest.os, "open") as opened, \
                mock.patch.object(guest.os.path, "realpath", side_effect=lambda path: path):
            value, events = captured(guest.admit_step, step_request(step))
            opened.assert_not_called()
        return value, events

    def test_step_shape_is_closed_and_separate_from_cases(self):
        request = step_request("quiesce")
        self.assertEqual(guest.request_kind(request), "step")
        self.assertFalse(guest.is_release(request))
        for changed in [{**request, "case": "sync"}, {**request, "version": guest.VERSION},
                        {key: value for key, value in request.items() if key != "step"}]:
            with self.subTest(changed=sorted(changed)), self.assertRaises(guest.Refused):
                guest.request_kind(changed)
        with self.assertRaisesRegex(guest.Refused, "step admission"):
            guest.admit(request)
        case_shaped = {**{key: value for key, value in request.items() if key != "step"},
                       "case": "sync", "version": guest.VERSION}
        with self.assertRaisesRegex(guest.Refused, "step admission requires"):
            guest.admit_step(case_shaped)

    def test_unknown_step_refuses_at_identity_admission(self):
        with mock.patch.object(guest.os, "geteuid", return_value=0):
            with self.assertRaisesRegex(guest.Refused, "case/two-domain inventory"):
                guest.admit_identity(step_request("restart"))

    def test_m1_absent_after_k04_is_recorded_without_any_open(self):
        mounts, events = self.admit([(MOUNTS[0]["path"], "9p", "hfs0", 10)])
        self.assertEqual([row["present"] for row in mounts], [True, False])
        self.assertEqual(events[-1]["event"], "admitted-step")
        self.assertFalse(events[-1]["acceptance"])
        both, _ = self.admit([(MOUNTS[0]["path"], "9p", "hfs0", 10), (MOUNTS[1]["path"], "9p", "hfs1", 11)])
        self.assertEqual([row["mount_id"] for row in both], [10, 11])

    def test_after_loss_needs_m1_and_tolerates_m0_absent(self):
        m0, m1 = (MOUNTS[0]["path"], "9p", "hfs0", 10), (MOUNTS[1]["path"], "9p", "hfs1", 11)
        with self.assertRaisesRegex(guest.Refused, "exact mountpoint"):
            self.admit([m0], step="after-loss")
        mounts, _ = self.admit([m1], step="after-loss")
        self.assertEqual([row["present"] for row in mounts], [False, True])
        self.assertEqual(guest.STEP_DOMAIN, {"lost-reply-call": 0, "lost-reply-reread": 0,
                                             "quiesce": 0, "after-loss": 1})

    def test_missing_m0_or_raw_fallback_filesystem_refuses(self):
        for rows, message in [([], "exact mountpoint"),
                              ([(MOUNTS[0]["path"], "ext4", "hfs0", 10)], "tuple differs"),
                              ([(MOUNTS[0]["path"], "9p", "hfs0", 10), (MOUNTS[1]["path"], "tmpfs", "hfs1", 11)],
                               "tuple differs"),
                              ([(MOUNTS[0]["path"], "9p", "hfs0", 99)], "tuple differs"),
                              ([(MOUNTS[0]["path"], "9p", "hfs0", 10)] * 2, "not present once")]:
            with self.subTest(rows=rows), self.assertRaisesRegex(guest.Refused, message):
                self.admit(rows)


class GuestStepTests(unittest.TestCase):
    """Real local syscalls on temporary stand-ins for m0/m1."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.mounts = []
        for index in range(2):
            path = self.root / ("m" + str(index))
            path.mkdir()
            self.mounts.append({"index": index, "path": str(path), "present": True, "mount_id": 10 + index})
        self.probe = guest.Probe(step_request("lost-reply-call"), [])

    def run_step(self, function, mounts=None):
        return captured(function, self.probe, mounts or self.mounts)

    def test_lost_reply_call_is_one_unretried_mkdir(self):
        observation, events = self.run_step(guest.lost_reply_call)
        self.assertEqual(observation["mkdir_errno"], 0)
        self.assertFalse(observation["retried"])
        self.assertTrue((self.root / "m0" / "zq3-test-lost-reply").is_dir())
        self.assertFalse((self.root / "m1" / "zq3-test-lost-reply").exists())
        self.assertEqual([event["operation"] for event in events if event["event"] == "invoke"],
                         ["mkdir-lost-reply-call"])
        again, _ = self.run_step(guest.lost_reply_call)
        self.assertEqual(again["mkdir_errno"], errno.EEXIST)

    def test_lost_reply_error_is_an_observation_not_a_failure(self):
        with mock.patch.object(guest.os, "mkdir", side_effect=OSError(errno.EIO, "dropped reply")):
            observation, events = self.run_step(guest.lost_reply_call)
        self.assertEqual(observation["mkdir_errno"], errno.EIO)
        self.assertEqual(events[-1]["errno"], errno.EIO)

    def test_reread_reads_unarmed_domain_first_and_never_creates(self):
        (self.root / "m0" / "zq3-test-lost-reply").mkdir()
        observation, events = self.run_step(guest.lost_reply_reread)
        rows = observation["domains"]
        self.assertEqual([row["index"] for row in rows], [1, 0])
        self.assertEqual((rows[0]["lstat_errno"], rows[0]["listing_matches"]), (errno.ENOENT, 0))
        self.assertEqual((rows[1]["lstat_errno"], rows[1]["type"], rows[1]["listing_matches"]), (0, "dir", 1))
        self.assertFalse(observation["mkdir_retried"])
        self.assertNotIn("mkdir", " ".join(event.get("operation", "") for event in events))
        absent = [self.mounts[0], {**self.mounts[1], "present": False}]
        observation, _ = self.run_step(guest.lost_reply_reread, absent)
        self.assertEqual(observation["domains"][0], {"index": 1, "present": False})

    def test_reread_records_blocked_session_errno(self):
        real_lstat = os.lstat

        def blocked(path, *args, **kwargs):
            if str(path).startswith(str(self.root / "m0")):
                raise OSError(errno.EHOSTDOWN, "blocked session")
            return real_lstat(path, *args, **kwargs)

        with mock.patch.object(guest.os, "lstat", side_effect=blocked):
            observation, _ = self.run_step(guest.lost_reply_reread)
        self.assertEqual(observation["domains"][1]["lstat_errno"], errno.EHOSTDOWN)

    def test_quiesce_syncs_present_domains_and_drops_caches_twice(self):
        caches = self.root / "drop_caches"
        caches.write_text("")
        writes = []
        real_open = open

        def recorded(path, mode="r", *args, **kwargs):
            if path == str(caches):
                writes.append(mode)
            return real_open(path, mode, *args, **kwargs)

        with mock.patch.object(guest, "DROP_CACHES", str(caches)), \
                mock.patch.object(guest, "QUIESCE_SETTLE", 0), \
                mock.patch("builtins.open", side_effect=recorded):
            observation, events = self.run_step(guest.quiesce, [self.mounts[0], {**self.mounts[1], "present": False}])
        self.assertEqual(observation["domains"], [{"index": 0, "open_errno": 0, "syncfs_errno": 0}])
        self.assertEqual(observation["drop_caches_errno"], [0, 0])
        self.assertEqual(writes, ["w", "w"])
        self.assertEqual(caches.read_text(), "3\n")
        operations = [event["operation"] for event in events if event["event"] == "invoke"]
        self.assertEqual(operations, ["open-root-quiesce", "syncfs", "drop-caches-first", "drop-caches-second"])

    def test_quiesce_records_refusals_instead_of_raising(self):
        with mock.patch.object(guest, "DROP_CACHES", str(self.root / "missing" / "drop_caches")), \
                mock.patch.object(guest, "QUIESCE_SETTLE", 0), \
                mock.patch.object(guest, "syncfs", side_effect=OSError(errno.EHOSTDOWN, "blocked")):
            observation, _ = self.run_step(guest.quiesce)
        self.assertEqual([row["syncfs_errno"] for row in observation["domains"]], [errno.EHOSTDOWN] * 2)
        self.assertEqual(observation["drop_caches_errno"], [errno.ENOENT, errno.ENOENT])

    def test_after_loss_dead_transport_refuses_all_io(self):
        dead = OSError(errno.ENOTCONN, "transport endpoint is not connected")
        with mock.patch.object(guest.os, "lstat", side_effect=dead), \
                mock.patch.object(guest.os, "listdir", side_effect=dead), \
                mock.patch.object(guest.os, "open", side_effect=dead):
            observation, _ = self.run_step(guest.after_loss)
        self.assertTrue(observation["io_refused"])
        self.assertEqual([item["errno"] for item in observation["attempts"]], [errno.ENOTCONN] * 3)
        self.assertEqual(observation["marker"], "zq3-test-after-loss")

    def test_after_loss_reaching_a_directory_is_recorded_and_preserved(self):
        observation, _ = self.run_step(guest.after_loss)
        self.assertFalse(observation["io_refused"])
        # m1, the domain T9 did not block, is probed; m0 is left alone.
        self.assertEqual(observation["domain"], 1)
        self.assertTrue((self.root / "m1" / "zq3-test-after-loss").exists())
        self.assertFalse((self.root / "m0" / "zq3-test-after-loss").exists())

    def test_deadline_is_never_recorded_as_an_errno(self):
        # The SIGALRM handler's exception is not an OSError: a hung, interrupted
        # call ends the step (uncertain) instead of becoming a refusal.
        self.assertFalse(issubclass(guest.Deadline, OSError))
        for step in [guest.after_loss, guest.lost_reply_call, guest.lost_reply_reread]:
            with self.subTest(step=step.__name__), \
                    mock.patch.object(guest.os, "lstat", side_effect=guest.deadline), \
                    mock.patch.object(guest.os, "mkdir", side_effect=guest.deadline):
                printed = io.StringIO()
                with contextlib.redirect_stdout(printed), self.assertRaises(guest.Deadline):
                    step(self.probe, self.mounts)
                last = json.loads(printed.getvalue().splitlines()[-1])
                self.assertEqual((last["event"], last["errno"], last["timeout"]), ("reply", None, True))

    def test_oserror_without_errno_ends_the_step(self):
        with mock.patch.object(guest.os, "mkdir", side_effect=OSError("no errno")):
            with contextlib.redirect_stdout(io.StringIO()), self.assertRaisesRegex(OSError, "no errno"):
                guest.lost_reply_call(self.probe, self.mounts)


class GuestStepMainTests(unittest.TestCase):
    def main(self, request, admitted=None, failure=None):
        stdin = io.TextIOWrapper(io.BytesIO(json.dumps(request).encode()))
        admit = mock.patch.object(guest, "admit_step", side_effect=failure, return_value=admitted)
        with admit, mock.patch.object(guest.resource, "setrlimit"), \
                mock.patch.dict(guest.STEP_ACTIONS, {"quiesce": lambda p, mounts: {"rows": len(mounts)}}), \
                mock.patch.object(sys, "argv", ["guest"]), mock.patch.object(sys, "stdin", stdin):
            return captured(guest.main)

    def test_step_terminal_names_step_and_never_payload(self):
        code, events = self.main(step_request("quiesce"), admitted=[{}, {}])
        self.assertEqual(code, 0)
        self.assertEqual(events[-2], {**events[-2], "event": "step-observed", "observation": {"rows": 2}})
        terminal = events[-1]
        self.assertEqual({key: terminal[key] for key in ["action", "step", "syscall_success",
                                                         "payload_retained", "payload_released",
                                                         "acceptance", "publication"]},
                         {"action": "step", "step": "quiesce", "syscall_success": True,
                          "payload_retained": False, "payload_released": False,
                          "acceptance": False, "publication": None})

    def test_step_deadline_reports_failed_terminal(self):
        def hung(p, mounts):
            p.call("lstat-root-after-loss", lambda: guest.deadline())

        stdin = io.TextIOWrapper(io.BytesIO(json.dumps(step_request("after-loss")).encode()))
        with mock.patch.object(guest, "admit_step", return_value=[{}, {}]), \
                mock.patch.object(guest.resource, "setrlimit"), \
                mock.patch.dict(guest.STEP_ACTIONS, {"after-loss": hung}), \
                mock.patch.object(sys, "argv", ["guest"]), mock.patch.object(sys, "stdin", stdin):
            code, events = captured(guest.main)
        self.assertEqual(code, 1)
        self.assertEqual((events[-2]["event"], events[-2]["error"]), ("failure", "Deadline"))
        self.assertEqual((events[-1]["step"], events[-1]["syscall_success"]), ("after-loss", False))
        self.assertNotIn("step-observed", [event["event"] for event in events])

    def test_step_refusal_reports_failed_terminal(self):
        code, events = self.main(step_request("quiesce"), failure=guest.Refused("exact mountpoint not present once"))
        self.assertEqual(code, 1)
        self.assertEqual(events[-2]["event"], "failure")
        self.assertEqual((events[-1]["action"], events[-1]["syscall_success"]), ("step", False))


class LifecycleDispatchTests(unittest.TestCase):
    """Controller lifecycle state machine; dispatch is a guest-shaped stand-in."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.path, self.plan, probe = batch_fixture(self.root)
        self.hash = controller.digest(self.path.read_bytes())
        self.output = self.root / "run"
        self.dispatched, self.step_success = [], True
        for patcher in [mock.patch.object(controller, "GUEST", probe),
                        mock.patch.object(controller, "bounded_child", side_effect=self.fake_dispatch)]:
            patcher.start()
            self.addCleanup(patcher.stop)

    def fake_dispatch(self, argv, request, folder, seconds):
        self.dispatched.append(request)
        if request["version"] == guest.STEP_VERSION:
            text = step_stdout(request, {"fixture": request["step"]}, self.step_success)
        else:
            text = guest_stdout(request)
        (folder / "stdout.log").write_text(text)
        (folder / "stderr.log").write_text("")
        return {"returncode": 0 if self.step_success else 1}

    def state(self):
        return json.loads((self.output / "state.json").read_bytes())

    def release_all(self):
        from test_preparation import correlation_for
        quiet(controller.probe, self.path, self.hash, self.output)
        for index, case in enumerate(controller.CASES):
            if index:
                quiet(controller.next_case, self.output, self.hash)
            correlation, sha = correlation_for(self.root, self.output, case)
            self.assertEqual(quiet(controller.release, self.output, self.hash, correlation, sha), 0)

    def lifecycle(self, step):
        return quiet(controller.lifecycle, self.output, self.hash, step)

    def test_steps_refuse_until_all_payloads_released(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        before = (self.output / "state.json").read_bytes()
        with self.assertRaisesRegex(controller.Refused, "five case payloads released"):
            self.lifecycle("lost-reply-call")
        self.assertEqual((self.output / "state.json").read_bytes(), before)
        self.assertEqual(len(self.dispatched), 1)

    def test_steps_run_once_in_order_with_plan_identity(self):
        self.release_all()
        for step in controller.STEPS:
            self.assertEqual(self.lifecycle(step), 0)
            request = self.dispatched[-1]
            self.assertEqual(guest.request_kind(request), "step")
            identity = {key: value for key, value in self.plan["requests"][0].items()
                        if key not in {"version", "case", "seconds"}}
            self.assertEqual(request, {**identity, "version": guest.STEP_VERSION, "step": step,
                                       "seconds": controller.STEP_SECONDS})
            record = self.state()["lifecycle"][-1]
            self.assertEqual((record["step"], record["state"]), (step, "completed"))
            self.assertEqual(record["observation"], {"fixture": step})
            self.assertTrue((self.output / ("lifecycle-" + step) / "stdout.log").exists())
        for step in controller.STEPS:
            with self.subTest(repeat=step), self.assertRaisesRegex(controller.Refused, "repeated or out of order"):
                self.lifecycle(step)
        self.assertFalse(self.state()["zq3_acceptance_complete"])

    def test_out_of_order_and_reread_without_call_refuse(self):
        self.release_all()
        with self.assertRaisesRegex(controller.Refused, "preceding lost-reply CALL"):
            self.lifecycle("lost-reply-reread")
        self.assertEqual(self.lifecycle("after-loss"), 0)
        for step in ["lost-reply-call", "quiesce"]:
            with self.subTest(step=step), self.assertRaisesRegex(controller.Refused, "out of order"):
                self.lifecycle(step)
        with self.assertRaisesRegex(controller.Refused, "unknown lifecycle step"):
            controller.check_step_order(self.state(), "restart")

    def test_failed_step_is_recorded_and_never_replayed(self):
        self.release_all()
        self.step_success = False
        self.assertEqual(self.lifecycle("lost-reply-call"), 1)
        state = self.state()
        self.assertEqual(state["lifecycle"][-1]["state"], "dispatching-uncertain")
        self.assertIn("did not complete", state["error"])
        self.step_success = True
        with self.assertRaisesRegex(controller.Refused, "never replay"):
            self.lifecycle("lost-reply-reread")

    def test_step_terminal_negatives(self):
        path = self.root / "stdout.log"
        request = step_request("quiesce")
        path.write_text(step_stdout(request, {"ok": 1}))
        self.assertEqual(controller.check_step_terminal(path, "quiesce")["observation"], {"ok": 1})
        events = [json.loads(line) for line in step_stdout(request, {"ok": 1}).splitlines()]
        for index, changes in [(-1, {"acceptance": True}), (-1, {"publication": {}}), (-1, {"step": "after-loss"}),
                               (-1, {"action": "case"}), (-1, {"payload_retained": True}),
                               (-1, {"uncertain_children": [7]}), (-2, {"step": "after-loss"}),
                               (-2, {"observation": "text"}), (-2, {"acceptance": True}),
                               (-2, {"event": "reply"})]:
            changed = [dict(event) for event in events]
            changed[index].update(changes)
            path.write_text("".join(json.dumps(event) + "\n" for event in changed))
            with self.subTest(index=index, changes=changes), self.assertRaises(controller.Refused):
                controller.check_step_terminal(path, "quiesce")
        path.write_text("".join(json.dumps(event) + "\n" for event in [events[0], events[1], events[1], events[2]]))
        with self.assertRaisesRegex(controller.Refused, "exactly one observation"):
            controller.check_step_terminal(path, "quiesce")


if __name__ == "__main__":
    unittest.main()
