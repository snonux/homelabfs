#!/usr/bin/env python3
"""Local tests of the offline host correlator and host snapshot (zq3 G7, G10, G11).

No runtime, f3, Rocky or SSH: lifecycle replies are synthetic records with the
daemon's JSON field names (lifecycle_evidence.go, lost_reply.go), the guest
is a stand-in writing guest-shaped logs, and host snapshots are taken by the
real host_snapshot.py against temporary directories with the prefix patched.
The produced case record is then fed to the real controller `release`.
"""
import contextlib
import copy
import errno
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from test_lifecycle_steps import quiet, step_stdout
from test_preparation import batch_fixture, controller, guest, guest_stdout, module

correlate = module("correlate")
host_snapshot = module("host_snapshot")


def uuid(byte):
    return [byte] * 16  # Go marshals fsapi.UUID ([16]byte) as a number array.


def evidence(applied, cut="cut-1", epoch=1, runtime="runtime-1"):
    """One complete SERVING OBSERVE evidence record of the plan's guest."""
    domains = [{"session": "session" + str(i), "phase": "ADMITTED", "revision": applied, "fence": 0}
               for i in range(2)]
    return {"version": correlate.EVIDENCE_VERSION, "runtime": runtime, "epoch": epoch, "cut": cut,
            "stage": "SERVING", "publication": None, "complete": True,
            "supervisor": {"stage": "SERVING", "process": "p", "controller": "c", "endpoint": {}, "live": True},
            "admissions": [{"fenced": False, "boot_id": "boot", "provider_nonce": "provider",
                            "kernel_build_id": "k", "physical": "h", "domains": domains}],
            "service": service(applied),
            "providers": [{"session": "session" + str(i),
                           "ledger": {"Ledger": "l" + str(i), "Digest": "d" + str(applied), "Device": 1,
                                      "Inode": 10 + i, "Bytes": applied}} for i in range(2)],
            "controller": {"ledger": {"Digest": "v", "Bytes": 1, "Matches": True, "Device": 1, "Inode": 20,
                                      "Sources": 1, "Leases": 0, "Reservations": 0, "Domains": 2,
                                      "Tickets": 0, "Terminal": 0, "Recall": False}},
            "transport": {"controller": "tc", "fuse_session": "fs", "high": applied, "cells": 1,
                          "ordinary": 1, "control": 0, "workspace": False, "terminal": 0, "hellos": 2,
                          "digest": "t" * 64}}


def service(applied):
    return {"state": "SERVING", "journal": "journal-1", "owner": "owner-1", "generation": "gen-1",
            "configuration": "conf-1", "applied": {"Volume": uuid(1), "Generation": uuid(2),
                                                   "Configuration": uuid(3), "Epoch": 1, "Sequence": applied},
            "visibility": "v", "state_hash": "s" * 64, "state_bytes": 10}


def observed(sequence, applied, **changes):
    return {"status": "OBSERVED", "sequence": sequence, "evidence": {**evidence(applied), **changes}}


def armed(sequence, applied, cut="cut-1", epoch=1, session="session0", **exposure):
    return {"status": "ARMED", "sequence": sequence, "lost_reply": {
        "fixture": "fixture-1", "cut": cut, "epoch": epoch, "session": session, "funded_bytes": 65536,
        "baseline": service(applied),
        "exposure": {"state": "ARMED", "session": session, "operation": "CALL", "armed_high": 40,
                     **exposure}}}


def fired(**changes):
    return {"state": "FIRED", "session": "session0", "operation": "CALL", "armed_high": 40, "cell": 41,
            "claim": {"realm": "r", "subject": "s", "anchor": "a", "nonce": "n", "number": 7,
                      "original_hash": "o"},
            "command_hash": "c" * 64, "response_hash": "e" * 64, "response_bytes": 200,
            "wire_bytes": 264, "unread_bytes": 264, "dropped_offset": 0, "dropped_bytes": 264,
            "dropped_hash": "f" * 64, "job_phase": "RESPONDED", "outcome": "UNCERTAIN", **changes}


def case_stdout(request):
    """Guest-shaped case log including the admission event the real guest emits."""
    text = guest_stdout(request)
    if request["version"] == guest.VERSION:
        admitted = {"event": "admitted", "boot_id": request["boot_id"],
                    "provider_nonce": request["provider_nonce"]}
        text = json.dumps(admitted) + "\n" + text
    return text


class CorrelatorBase(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.path, self.plan, probe = batch_fixture(self.root)
        self.hash = controller.digest(self.path.read_bytes())
        self.output = self.root / "run"
        self.observations, self.count = {}, 0
        for patcher in [mock.patch.object(controller, "GUEST", probe),
                        mock.patch.object(controller, "bounded_child", side_effect=self.fake_dispatch)]:
            patcher.start()
            self.addCleanup(patcher.stop)

    def fake_dispatch(self, argv, request, folder, seconds):
        if request["version"] == guest.STEP_VERSION:
            text = step_stdout(request, self.observations[request["step"]])
        else:
            text = case_stdout(request)
        (folder / "stdout.log").write_text(text)
        (folder / "stderr.log").write_text("")
        return {"returncode": 0}

    def reply(self, value, raw=None):
        """Write one raw lifecycle reply as the client prints it; return the pinned entry."""
        self.count += 1
        path = self.root / ("reply-" + str(self.count) + ".json")
        path.write_bytes(raw if raw is not None else json.dumps(value).encode() + b"\n")
        return str(path), controller.digest(path.read_bytes())

    def entry(self, value):
        return correlate.load_reply(*self.reply(value))

    def state(self):
        return json.loads((self.output / "state.json").read_bytes())

    def correlate_case(self, case, baseline, retained):
        return correlate.case_record(str(self.output), self.hash, case,
                                     self.entry(baseline), self.entry(retained))

    def write(self, record, name):
        path = self.root / name
        return str(path), correlate.write_record(str(path), record)

    def release(self, case, record):
        path, sha = self.write(record, "correlation-" + case + ".json")
        return quiet(controller.release, self.output, self.hash, path, sha)

    def release_all(self):
        """Five cases, each correlated by the real correlator and released."""
        quiet(controller.probe, self.path, self.hash, self.output)
        sequence = 0
        for index, case in enumerate(controller.CASES):
            if index:
                quiet(controller.next_case, self.output, self.hash)
            base = 2 * index + 1
            record = self.correlate_case(case, observed(sequence + 1, base), observed(sequence + 2, base + 1))
            self.assertEqual(self.release(case, record), 0)
            sequence += 2
        return sequence


class CaseCorrelationTests(CorrelatorBase):
    def test_every_case_record_is_bound_and_accepted_by_release(self):
        self.release_all()
        state = self.state()
        self.assertTrue(state["syscall_batch_success"])
        self.assertFalse(state["zq3_acceptance_complete"])
        record = json.loads((self.output / "sync" / "correlation.json").read_bytes())
        self.assertEqual(record["version"], controller.CORRELATION_VERSION)
        self.assertFalse(record["acceptance"])
        host = record["host_observe"]
        self.assertEqual(host["format"], correlate.OBSERVE_FORMAT)
        self.assertEqual((host["baseline"]["sequence"], host["retained"]["sequence"]), (3, 4))
        self.assertEqual(host["previous_correlation_sha256"], state["cases"][0]["correlation_sha256"])
        self.assertTrue(host["summary"]["advanced"])
        self.assertEqual(record["case_stdout_sha256"], state["cases"][1]["stdout_sha256"])

    def assert_refused(self, message, baseline, retained, case="credentials"):
        with self.assertRaisesRegex(correlate.Refused, message):
            self.correlate_case(case, baseline, retained)

    def test_binding_negatives_refuse_before_any_record(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        good = observed(1, 1)
        fenced = copy.deepcopy(observed(2, 2))
        fenced["evidence"]["admissions"][0]["fenced"] = True
        moved = copy.deepcopy(observed(2, 2))
        moved["evidence"]["providers"][0]["ledger"]["Inode"] = 99
        rotated_journal = copy.deepcopy(observed(2, 2))
        rotated_journal["evidence"]["service"]["journal"] = "journal-2"
        wrong_boot = copy.deepcopy(observed(2, 2))
        wrong_boot["evidence"]["admissions"][0]["boot_id"] = "rebooted"
        foreign = copy.deepcopy(observed(2, 2))
        foreign["evidence"]["admissions"][0]["domains"][1]["session"] = "foreign"
        for message, retained in [
                ("stale or rotated cut/epoch", observed(2, 2, cut="cut-2")),
                ("stale or rotated cut/epoch", observed(2, 2, epoch=2)),
                ("runtime differs", observed(2, 2, runtime="runtime-2")),
                ("did not advance", observed(2, 1)),
                ("sequence must increase", observed(1, 2)),
                ("fenced or refresh", fenced),
                ("provider ledger identity", moved),
                ("journal/owner/generation/configuration changed", rotated_journal),
                ("boot/provider differs", wrong_boot),
                ("domains differ", foreign),
                ("incomplete or not SERVING", observed(2, 2, complete=False)),
                ("incomplete or not SERVING", observed(2, 2, stage="FENCED")),
                ("publication differs", observed(2, 2, publication={"generation": 1})),
                ("follows a RESTART", observed(2, 2, restart={"status": "FENCED"})),
                ("armed lost-reply", observed(2, 2, lost_reply={"fixture": "x"})),
                ("not an OBSERVED", {"status": "REFUSED", "sequence": 2, "reason": "x"})]:
            with self.subTest(message=message):
                self.assert_refused(message, good, retained)
        self.assert_refused("not the retained case", good, observed(2, 2), case="sync")
        self.assertEqual(list(self.root.glob("correlation-*")), [])

    def test_reply_pinning_and_duplicate_keys_refuse(self):
        path, sha = self.reply(None, raw=b'{"status":"OBSERVED","sequence":1,"sequence":2}\n')
        with self.assertRaisesRegex(correlate.Refused, "duplicate JSON key"):
            correlate.load_reply(path, sha)
        path, _ = self.reply(observed(1, 1))
        with self.assertRaisesRegex(correlate.Refused, "pinned input changed"):
            correlate.load_reply(path, "0" * 64)
        with self.assertRaisesRegex(correlate.Refused, "digest required"):
            correlate.load_reply(path, "short")

    def test_changed_case_log_wrong_plan_or_error_state_refuse(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        with self.assertRaisesRegex(correlate.Refused, "plan digest differs"):
            correlate.case_record(str(self.output), "0" * 64, "credentials",
                                  self.entry(observed(1, 1)), self.entry(observed(2, 2)))
        log = self.output / "credentials" / "stdout.log"
        original = log.read_bytes()
        log.write_bytes(original.replace(b'"leaf_ino": 7', b'"leaf_ino": 8'))
        self.assert_refused("evidence changed", observed(1, 1), observed(2, 2))
        log.write_bytes(original)
        state = self.state()
        state["error"] = "Refused: earlier failure"
        (self.output / "state.json").write_text(json.dumps(state))
        self.assert_refused("recorded an error", observed(1, 1), observed(2, 2))

    def test_chain_requires_baseline_after_previous_release(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        record = self.correlate_case("credentials", observed(1, 1), observed(2, 2))
        self.assertEqual(self.release("credentials", record), 0)
        quiet(controller.next_case, self.output, self.hash)
        self.assert_refused("predates the previous", observed(2, 3), observed(3, 4), case="sync")
        self.assert_refused("no applied effect of the previous release", observed(3, 2), observed(4, 4),
                            case="sync")
        self.assert_refused("since the previous case", observed(3, 3, cut="cut-2"),
                            observed(4, 4, cut="cut-2"), case="sync")
        self.assertIsNotNone(self.correlate_case("sync", observed(3, 3), observed(4, 4)))

    def test_mismatched_record_digest_refuses_release_and_record_is_never_overwritten(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        record = self.correlate_case("credentials", observed(1, 1), observed(2, 2))
        path, sha = self.write(record, "correlation.json")
        with self.assertRaisesRegex(controller.Refused, "pinned input changed"):
            controller.release(self.output, self.hash, path, "0" * 64)
        with self.assertRaises(FileExistsError):
            correlate.write_record(path, record)
        self.assertEqual(controller.digest(Path(path).read_bytes()), sha)
        self.assertEqual(quiet(controller.release, self.output, self.hash, path, sha), 0)

    def test_cli_writes_record_and_prints_its_digest(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        baseline, baseline_sha = self.reply(observed(1, 1))
        retained, retained_sha = self.reply(observed(2, 2))
        record = str(self.root / "cli-record.json")
        printed = io.StringIO()
        with contextlib.redirect_stdout(printed):
            self.assertEqual(correlate.main(["case", "--output", str(self.output), "--sha256", self.hash,
                                             "--case", "credentials", "--record", record,
                                             "--baseline", baseline, "--baseline-sha256", baseline_sha,
                                             "--retained", retained, "--retained-sha256", retained_sha]), 0)
        result = json.loads(printed.getvalue())
        self.assertEqual(result["record_sha256"], controller.digest(Path(record).read_bytes()))
        self.assertEqual(Path(record).stat().st_mode & 0o777, 0o600)


class LostReplyCorrelationTests(CorrelatorBase):
    def setUp(self):
        super().setUp()
        self.sequence = self.release_all()
        self.observations.update({
            "lost-reply-call": {"armed_domain": 0, "leaf": "zq3-test-lost-reply", "mkdir_errno": errno.EIO,
                                "retried": False},
            "lost-reply-reread": {"leaf": "zq3-test-lost-reply", "mkdir_retried": False, "domains": [
                {"index": 1, "present": True, "lstat_errno": 0, "type": "dir", "listing_errno": 0,
                 "listing_matches": 1},
                {"index": 0, "present": True, "lstat_errno": errno.EHOSTDOWN, "type": None,
                 "listing_errno": errno.EHOSTDOWN, "listing_matches": None}]}})

    def steps(self, names=("lost-reply-call", "lost-reply-reread")):
        for name in names:
            self.assertEqual(quiet(controller.lifecycle, self.output, self.hash, name), 0)

    def record(self, before=None, arm=None, after=None):
        s = self.sequence
        return correlate.lost_reply_record(
            str(self.output), self.hash, self.entry(before or observed(s + 1, 11)),
            self.entry(arm or armed(s + 2, 11)),
            self.entry(after or observed(s + 3, 12, lost_reply={**armed(0, 11)["lost_reply"],
                                                               "exposure": fired()})))

    def test_committed_dropped_reply_visible_through_unarmed_domain(self):
        self.steps()
        record = self.record()
        self.assertEqual(record["classification"], "dropped-reply-effect-committed-and-visible")
        self.assertTrue(record["committed_since_arm"])
        self.assertFalse(record["acceptance"])
        self.assertEqual(record["exposure"]["outcome"], "UNCERTAIN")
        self.assertEqual(record["guest"]["call"]["observation"]["mkdir_errno"], errno.EIO)
        path, sha = self.write(record, "lost-reply.json")
        self.assertEqual(controller.digest(Path(path).read_bytes()), sha)

    def test_dropped_before_mutation_and_not_fired_are_observations(self):
        self.observations["lost-reply-reread"]["domains"][0].update(
            {"lstat_errno": errno.ENOENT, "type": None, "listing_matches": 0})
        self.observations["lost-reply-reread"]["domains"][1]["lstat_errno"] = errno.ENOENT
        self.steps()
        s = self.sequence
        unchanged = observed(s + 3, 11, lost_reply={**armed(0, 11)["lost_reply"], "exposure": fired()})
        self.assertEqual(self.record(after=unchanged)["classification"], "dropped-before-mutation")
        idle = observed(s + 3, 11, lost_reply=armed(0, 11)["lost_reply"])
        record = self.record(after=idle)
        self.assertEqual(record["classification"], "fixture-not-fired")
        self.assertIsNone(record["exposure"])

    def test_binding_negatives_refuse(self):
        self.steps()
        s = self.sequence
        post = armed(0, 11)["lost_reply"]
        for message, kwargs in [
                ("stale cut/epoch", {"arm": armed(s + 2, 11, cut="cut-0")}),
                ("unfired CALL fixture on m0", {"arm": armed(s + 2, 11, session="session1")}),
                ("follow the pre-arm", {"arm": armed(s + 1, 11)}),
                ("already armed", {"before": observed(s + 1, 11, lost_reply=post)}),
                ("does not carry the armed fixture",
                 {"after": observed(s + 3, 12, lost_reply={**post, "fixture": "other", "exposure": fired()})}),
                ("stale or rotated cut/epoch",
                 {"after": observed(s + 3, 12, cut="cut-2", lost_reply={**post, "exposure": fired()})}),
                ("above the armed high-water",
                 {"after": observed(s + 3, 12, lost_reply={**post, "exposure": fired(cell=40)})}),
                ("above the armed high-water",
                 {"after": observed(s + 3, 12, lost_reply={**post, "exposure": fired(outcome="OK")})}),
                ("must follow the ARM",
                 {"arm": armed(s + 5, 11),
                  "after": observed(s + 3, 12, lost_reply={**post, "exposure": fired()})})]:
            with self.subTest(message=message), self.assertRaisesRegex(correlate.Refused, message):
                self.record(**kwargs)

    def test_missing_reread_step_refuses(self):
        self.steps(["lost-reply-call"])
        with self.assertRaisesRegex(correlate.Refused, "lost-reply-reread"):
            self.record()


class K04CorrelationTests(CorrelatorBase):
    def setUp(self):
        super().setUp()
        self.release_all()
        prefix = self.root / "staging"
        (prefix / "test").mkdir(parents=True)
        self.mountpoint = prefix / "test" / "fuse"
        self.mountpoint.mkdir()
        patcher = mock.patch.object(host_snapshot, "PREFIX", str(prefix) + "/")
        patcher.start()
        self.addCleanup(patcher.stop)
        refused = [{"operation": name, "errno": errno.ENOTCONN}
                   for name in ["lstat-root-after-loss", "listdir-after-loss", "create-after-loss"]]
        self.observations["after-loss"] = {"marker": "zq3-test-after-loss", "attempts": refused,
                                           "io_refused": True}

    def snapshot(self, label, run_id="test", path=None, dead=False):
        target = str(path or self.mountpoint)
        real_lstat = os.lstat

        def lstat(name, *args, **kwargs):
            if dead and name == target:
                raise OSError(errno.ENOTCONN, "dead FUSE mount")
            return real_lstat(name, *args, **kwargs)

        with mock.patch.object(host_snapshot.os, "lstat", side_effect=lstat):
            value = host_snapshot.snapshot("test", target, label)
        value["run_id"] = run_id  # A snapshot of another run, relabelled after the fact.
        path, sha = self.reply(value)
        return path, sha

    def k04(self, **snapshots):
        quiet(controller.lifecycle, self.output, self.hash, "after-loss")
        return correlate.k04_record(str(self.output), self.hash, snapshots)

    def test_dead_mount_and_unchanged_raw_directory_record_refusal(self):
        record = self.k04(before=self.snapshot("before", dead=True), after=self.snapshot("after", dead=True))
        self.assertTrue(record["k04_refusal_observed"])
        self.assertFalse(record["checks"]["raw_directory_reachable"])
        self.assertFalse(record["acceptance"])

    def test_raw_directory_with_guest_marker_is_recorded_as_not_holding(self):
        baseline = self.snapshot("baseline")
        before = self.snapshot("before")
        (self.mountpoint / "zq3-test-after-loss").write_text("")
        self.observations["after-loss"]["attempts"][2]["errno"] = 0
        record = self.k04(baseline=baseline, before=before, after=self.snapshot("after"))
        self.assertFalse(record["k04_refusal_observed"])
        self.assertEqual({key: record["checks"][key] for key in
                          ["guest_io_refused", "marker_absent", "raw_directory_reachable",
                           "raw_directory_unchanged"]},
                         {"guest_io_refused": False, "marker_absent": False,
                          "raw_directory_reachable": True, "raw_directory_unchanged": False})

    def test_unchanged_raw_directory_holds_against_baseline(self):
        record = self.k04(baseline=self.snapshot("baseline"), before=self.snapshot("before"),
                          after=self.snapshot("after"))
        self.assertTrue(record["k04_refusal_observed"])
        self.assertTrue(record["checks"]["raw_directory_reachable"])

    def test_snapshot_binding_negatives_refuse(self):
        other = self.mountpoint.parent / "other"
        other.mkdir()
        quiet(controller.lifecycle, self.output, self.hash, "after-loss")
        for message, snapshots in [
                ("version/run/label", {"before": self.snapshot("after"), "after": self.snapshot("after")}),
                ("version/run/label", {"before": self.snapshot("before", run_id="other"),
                                       "after": self.snapshot("after")}),
                ("different paths", {"before": self.snapshot("before", path=other),
                                     "after": self.snapshot("after")}),
                ("pinned input changed", {"before": (self.snapshot("before")[0], "0" * 64),
                                          "after": self.snapshot("after")}),
                ("raw pre-run directory", {"baseline": self.snapshot("baseline", dead=True),
                                           "before": self.snapshot("before"), "after": self.snapshot("after")})]:
            with self.subTest(message=message), self.assertRaisesRegex(correlate.Refused, message):
                correlate.k04_record(str(self.output), self.hash, snapshots)

    def test_missing_after_loss_step_or_malformed_observation_refuses(self):
        with self.assertRaisesRegex(correlate.Refused, "after-loss"):
            correlate.k04_record(str(self.output), self.hash,
                                 {"before": self.snapshot("before"), "after": self.snapshot("after")})
        self.observations["after-loss"]["marker"] = "zq3-other-after-loss"
        with self.assertRaisesRegex(correlate.Refused, "observation malformed"):
            self.k04(before=self.snapshot("before"), after=self.snapshot("after"))


class CommandTests(CorrelatorBase):
    def command(self, operation, observe, last=None, control="/var/tmp/hfs/owner/control.sock"):
        path, sha = self.reply(observe)
        argv = ["command", "--output", str(self.output), "--sha256", self.hash, "--operation", operation,
                "--runtime-binary", "/var/tmp/hfs-validation-xz3/test/bin/hfs-controlled-runtime",
                "--control", control, "--observe", path, "--observe-sha256", sha]
        if last is not None:
            last_path, last_sha = self.reply(last)
            argv += ["--last", last_path, "--last-sha256", last_sha]
        return correlate.client_command(correlate.arguments(argv))

    def test_arm_argv_binds_current_cut_epoch_and_m0_session(self):
        sequence = self.release_all()
        result = self.command("arm-lost-reply", observed(sequence + 1, 11))
        self.assertEqual(result["argv"][1:], [
            "--control", "/var/tmp/hfs/owner/control.sock", "--operation", "arm-lost-reply",
            "--runtime", "runtime-1", "--sequence", str(sequence + 2), "--cut", "cut-1", "--epoch", "1",
            "--session", "session0", "--lost-operation", "CALL"])
        self.assertFalse(result["execution_authorized"])

    def test_arm_refuses_before_batch_after_steps_or_when_armed(self):
        quiet(controller.probe, self.path, self.hash, self.output)
        with self.assertRaisesRegex(correlate.Refused, "ARM only once"):
            self.command("arm-lost-reply", observed(3, 3))
        self.release_all_from_probe()
        with self.assertRaisesRegex(correlate.Refused, "ARM only once"):
            self.command("arm-lost-reply", observed(20, 11, lost_reply=armed(0, 11)["lost_reply"]))
        self.observations["after-loss"] = {"marker": "zq3-test-after-loss", "attempts": [], "io_refused": True}
        quiet(controller.lifecycle, self.output, self.hash, "after-loss")
        with self.assertRaisesRegex(correlate.Refused, "ARM only once"):
            self.command("arm-lost-reply", observed(21, 11))

    def release_all_from_probe(self):
        """Release the already probed first case and the remaining four."""
        sequence = 0
        for index, case in enumerate(controller.CASES):
            if index:
                quiet(controller.next_case, self.output, self.hash)
            base = 2 * index + 1
            record = self.correlate_case(case, observed(sequence + 1, base), observed(sequence + 2, base + 1))
            self.assertEqual(self.release(case, record), 0)
            sequence += 2

    def test_restart_requires_quiesce_step_and_observed_quiesce(self):
        sequence = self.release_all()
        with self.assertRaisesRegex(correlate.Refused, "directly after the guest quiesce"):
            self.command("restart", observed(sequence + 1, 11))
        self.observations["quiesce"] = {"domains": [], "drop_caches_errno": [0, 0], "settle_seconds": 2}
        quiet(controller.lifecycle, self.output, self.hash, "quiesce")
        for field, value in [("Leases", 1), ("Reservations", 2), ("Recall", True), ("Matches", False)]:
            busy = observed(sequence + 1, 11)
            busy["evidence"]["controller"]["ledger"][field] = value
            with self.subTest(field=field), self.assertRaisesRegex(correlate.Refused, "RESTART not issued"):
                self.command("restart", busy)
        result = self.command("restart", observed(sequence + 1, 11))
        self.assertEqual(result["argv"][3:], ["--operation", "restart", "--runtime", "runtime-1",
                                              "--sequence", str(sequence + 2), "--cut", "cut-1", "--epoch", "1"])
        self.assertEqual(result["quiesce"]["Leases"], 0)

    def test_observe_uses_latest_sequence_and_unsafe_paths_refuse(self):
        sequence = self.release_all()
        result = self.command("observe", observed(sequence + 1, 11), last=armed(sequence + 2, 11))
        self.assertEqual(result["argv"][3:], ["--operation", "observe", "--runtime", "runtime-1",
                                              "--sequence", str(sequence + 3)])
        with self.assertRaisesRegex(correlate.Refused, "predates"):
            self.command("observe", observed(sequence + 3, 11), last=armed(sequence + 2, 11))
        with self.assertRaisesRegex(correlate.Refused, "safe ASCII path"):
            self.command("observe", observed(sequence + 1, 11), control="/var/tmp/$HOME/control.sock")
        with self.assertRaisesRegex(correlate.Refused, "OBSERVED reply"):
            self.command("observe", armed(sequence + 1, 11))


class HostSnapshotTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.prefix = Path(temporary.name) / "staging"
        (self.prefix / "test").mkdir(parents=True)
        patcher = mock.patch.object(host_snapshot, "PREFIX", str(self.prefix) + "/")
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_paths_outside_the_run_staging_directory_refuse(self):
        base = str(self.prefix / "test")
        for run_id, path in [("test", base), ("test", base + "/../other"), ("test", "/etc"),
                             ("test", base + "/a b"), ("Test", base + "/fuse"), ("other", base + "/fuse")]:
            with self.subTest(path=path), self.assertRaises(host_snapshot.Refused):
                host_snapshot.snapshot(run_id, path, "before")

    def test_plain_directory_listing_is_bounded_and_symlinks_are_not_followed(self):
        directory = self.prefix / "test" / "fuse"
        directory.mkdir()
        for index in range(host_snapshot.LISTING_LIMIT + 1):
            (directory / ("n" + str(index))).write_text("")
        value = host_snapshot.snapshot("test", str(directory), "after")
        self.assertFalse(value["mountpoint"])
        self.assertTrue(value["listing"]["truncated"])
        self.assertEqual(len(value["listing"]["names"]), host_snapshot.LISTING_LIMIT)
        link = self.prefix / "test" / "link"
        link.symlink_to(directory)
        value = host_snapshot.snapshot("test", str(link), "after")
        self.assertEqual((value["target"]["type"], value["listing"]["errno"]), ("symlink", errno.ENOTDIR))

    def test_main_prints_canonical_snapshot(self):
        directory = self.prefix / "test" / "fuse"
        directory.mkdir()
        printed = io.StringIO()
        with contextlib.redirect_stdout(printed):
            self.assertEqual(host_snapshot.main(["--run-id", "test", "--path", str(directory),
                                                 "--label", "baseline"]), 0)
        value = json.loads(printed.getvalue())
        self.assertEqual((value["version"], value["listing"]["names"]), (correlate.SNAPSHOT_VERSION, []))


if __name__ == "__main__":
    unittest.main()
