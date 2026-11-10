#!/usr/bin/env python3
"""Local tests of the zq3 run-time plan fills (fill_plan.py).

No SSH, network, lab or key. The bootstrap evidence is produced by running
the real `bootstrap.run` against test_bootstrap.py's simulated world, so the
journal, summary, plan copy and work files have exactly the shape
bootstrap.py writes; only the guest's mount replies are made realistic
(`/proc/self/mountinfo` rows as bootstrap_remote.py reports them). Every
filled plan is checked again by the consumer's own validation.
"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
LAB = HERE.parents[2] / "docs" / "lab"


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


fill = module("fill_plan_under_test", "fill_plan.py")
# Only the simulated world and fixtures; its TestCase classes stay unbound
# here so discovery does not run them twice.
world_fixtures = module("test_bootstrap_world", "test_bootstrap.py")
controller = fill.controller
sha = controller.digest
BOOT, NONCE, MANIFEST = world_fixtures.BOOT, world_fixtures.NONCE, world_fixtures.MANIFEST
SESSIONS = [mount["session"] for mount in world_fixtures.REQUEST["Mounts"]]
RUN_DIR = "/var/tmp/hfs-zq3-zq3-r1"
SSH_HASH = "a1" * 32


def mountinfo(world, mount_ids):
    """The world's guest mount (it still emits READY after the second mount),
    replying with a real mountinfo row instead of its placeholder (IDs in
    call order)."""
    ids = list(mount_ids)

    def mount(options, payload):
        result = world.on_guest_mount(options, payload)
        result["mountinfo"] = "{} 22 0:55 / {} rw,relatime - 9p {} rw,trans=virtio".format(
            ids.pop(0), options["--mountpoint"], options["--tag"])
        return result
    return mount


def batch_template(root, change=None):
    """The zq3 controller batch template shape with Phase 8 placeholders."""
    ssh, host, guest = root / "ssh", root / "host-pin", root / "guest-pin"
    ssh.write_bytes(b"pinned ssh stand-in")
    host.write_bytes((LAB / "f3-host-known_hosts").read_bytes())
    guest.write_bytes((LAB / "rocky-known_hosts").read_bytes())
    mounts = [{"mount_id": fill.MOUNT_IDS[i], "path": "{}/m{}".format(RUN_DIR, i), "session": SESSIONS[i],
               "tag": "hfs{}".format(i)} for i in range(2)]
    requests = [{"boot_id": fill.BOOT, "case": case, "manifest_sha256": fill.MANIFEST, "mounts": mounts,
                 "provider_nonce": fill.NONCE, "run_id": "zq3-r1", "seconds": 60,
                 "version": "homelabfs-retained9p-syscalls/1"} for case in controller.CASES]
    plan = {"candidate_manifest_sha256": next(iter(controller.ADMITTED)), "installed_manifest_sha256": fill.MANIFEST,
            "probe_sha256": sha(controller.GUEST.read_bytes()), "requests": json.loads(json.dumps(requests)),
            "run_id": "zq3-r1", "seconds": 120, "version": controller.VERSION,
            "ssh": {"path": str(ssh), "sha256": sha(ssh.read_bytes())},
            "host": {"address": "10.20.0.133", "alias": "f3", "user": "paul", "known_hosts": str(host),
                     "known_hosts_sha256": sha(host.read_bytes())},
            "guest": {"address": "10.20.0.123", "alias": "rocky", "user": "root", "known_hosts": str(guest),
                      "known_hosts_sha256": sha(guest.read_bytes())}}
    if change:
        change(plan)
    path = root / "batch-plan.TEMPLATE.json"
    path.write_text(json.dumps(plan, indent=1, sort_keys=True) + "\n")
    return path, sha(path.read_bytes())


class Case(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.out = self.root / "filled.json"

    def fill(self, *argv):
        """(exit code, report or stderr) of one fill_plan.py main() call."""
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            code = fill.main([str(a) for a in argv])
        return code, (json.loads(stdout.getvalue()) if code == 0 else stderr.getvalue())

    def refuses(self, argv, text):
        code, message = self.fill(*argv)
        self.assertEqual(code, 2, message)
        self.assertIn(text, message)
        self.assertFalse(os.path.lexists(self.out))
        self.assertFalse(os.path.lexists(str(self.out) + ".next"))


class BatchCase(Case):
    def bootstrap_run(self, mount_ids=(90, 91), world_change=None):
        """A real bootstrap.run output directory from the simulated world."""
        world = world_fixtures.World(self.root)
        world.overrides["guest-mount"] = mountinfo(world, mount_ids)
        if world_change:
            world_change(world)
        plan, plan_sha256 = world_fixtures.plan_fixture(self.root)
        self.run_dir = self.root / "bootstrap-run"
        code = world_fixtures.bootstrap.run(plan, plan_sha256, self.run_dir, boundaries=world.boundaries)
        return code, plan_sha256

    def argv(self, plan_sha256, template=None, template_sha256=None):
        if template is None:
            template, template_sha256 = batch_template(self.root)
        return ["batch", "--template", template, "--template-sha256", template_sha256, "--bootstrap-run",
                self.run_dir, "--bootstrap-plan-sha256", plan_sha256, "--output", self.out]

    def rewrite_journal(self, step, change):
        """Edit one journaled result in place (evidence tampered after the run)."""
        path = self.run_dir / "journal.jsonl"
        records = [json.loads(line) for line in path.read_text().splitlines()]
        for record in records:
            if record.get("kind") == "result" and record.get("step") == step:
                change(record["detail"])
        path.write_bytes(b"".join(controller.canonical(record) for record in records))


class BatchTests(BatchCase):
    def test_fills_every_phase8_value_and_passes_the_controller(self):
        code, plan_sha256 = self.bootstrap_run()
        self.assertEqual(code, 0)
        code, report = self.fill(*self.argv(plan_sha256))
        self.assertEqual(code, 0, report)
        raw = self.out.read_bytes()
        self.assertEqual(report["sha256"], sha(raw))
        self.assertEqual((report["boot_id"], report["provider_nonce"], report["manifest_sha256"],
                          report["mount_ids"], report["execution_authorized"]), (BOOT, NONCE, MANIFEST, [90, 91], False))
        self.assertEqual(os.stat(self.out).st_mode & 0o777, 0o600)
        self.assertFalse(os.path.lexists(str(self.out) + ".next"))
        plan = json.loads(raw)
        self.assertEqual(plan["installed_manifest_sha256"], MANIFEST)
        for request in plan["requests"]:
            self.assertEqual((request["boot_id"], request["provider_nonce"], request["manifest_sha256"]),
                             (BOOT, NONCE, MANIFEST))
            self.assertEqual([m["mount_id"] for m in request["mounts"]], [90, 91])
        self.assertNotIn(b"PHASE", raw)
        # The controller's own validation, in process and as check-retained9p.sh runs it.
        controller.plan_snapshot(str(self.out), report["sha256"])
        done = subprocess.run([sys.executable, "-I", str(HERE / "controller.py"), "check", "--plan", str(self.out)],
                              capture_output=True, timeout=60)
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual(json.loads(done.stdout)["plan_sha256"], report["sha256"])

    def test_template_itself_is_refused_by_the_controller(self):
        template, _ = batch_template(self.root)
        with self.assertRaisesRegex(controller.Refused, "installed-manifest pin"):
            controller.plan_snapshot(str(template))

    def test_existing_output_is_never_overwritten(self):
        _, plan_sha256 = self.bootstrap_run()
        self.out.write_bytes(b"earlier plan\n")
        code, message = self.fill(*self.argv(plan_sha256))
        self.assertEqual((code, "never overwritten" in message), (2, True))
        self.assertEqual(self.out.read_bytes(), b"earlier plan\n")


class BatchEvidenceTests(BatchCase):
    def test_run_that_did_not_reach_ready_refuses(self):
        def fail_second_mount(world):
            replies = mountinfo(world, (90,))

            def mount(options, payload):
                if options["--tag"] == "hfs1":
                    return world_fixtures.reply({"ok": False, "uncertain": True, "error": "mount hung"}, 1)
                return replies(options, payload)
            world.overrides["guest-mount"] = mount
        code, plan_sha256 = self.bootstrap_run(world_change=fail_second_mount)
        self.assertEqual(code, 3)
        self.refuses(self.argv(plan_sha256), "incomplete run")

    def test_ready_run_whose_usable_gate_failed_refuses(self):
        """zq3-r7's shape: READY with every earlier step ok, then EACCES on
        the mount roots. The batch plan must never be filled from it."""
        denied = {"outcome": "errno", "errno": 13, "error": "Permission denied"}

        def refuse_roots(world):
            world.stat_outcomes = [[{}, {}], [denied, denied]]
        code, plan_sha256 = self.bootstrap_run(world_change=refuse_roots)
        self.assertEqual(code, 1)
        steps = {r["step"]: r["outcome"] for r in world_fixtures.records(self.run_dir) if r["kind"] == "result"}
        self.assertEqual((steps["ready"], steps["usable"]), ("ok", "failed"))
        self.refuses(self.argv(plan_sha256), "bootstrap steps did not end ok: usable=failed")

    def test_journal_of_the_previous_step_list_refuses_by_version(self):
        _, plan_sha256 = self.bootstrap_run()
        self.assertEqual(fill.bootstrap.JOURNAL_VERSION, "homelabfs-zq3-bootstrap-journal/2")
        path = self.run_dir / "journal.jsonl"
        records = [json.loads(line) for line in path.read_text().splitlines()]
        records[0]["version"] = "homelabfs-zq3-bootstrap-journal/1"
        path.write_bytes(b"".join(controller.canonical(record) for record in records))
        self.refuses(self.argv(plan_sha256), "journal version 'homelabfs-zq3-bootstrap-journal/1' is not")

    def test_summary_must_say_ready(self):
        _, plan_sha256 = self.bootstrap_run()
        summary = self.run_dir / "summary.json"
        value = json.loads(summary.read_text())
        summary.write_text(json.dumps(dict(value, outcome="failed", stopped_at="ready")))
        self.refuses(self.argv(plan_sha256), "not READY")

    def test_other_bootstrap_plan_refuses(self):
        _, plan_sha256 = self.bootstrap_run()
        self.refuses(self.argv("0" * 64), "not for the pinned bootstrap plan")
        (self.run_dir / "plan.json").chmod(0o600)
        (self.run_dir / "plan.json").write_bytes(b"{}")
        self.refuses(self.argv(plan_sha256), "plan.json differs")

    def test_torn_journal_refuses(self):
        _, plan_sha256 = self.bootstrap_run()
        with open(self.run_dir / "journal.jsonl", "ab") as stream:
            stream.write(b'{"kind":"intent"')
        self.refuses(self.argv(plan_sha256), "torn journal tail")

    def test_mountinfo_must_name_one_9p_mount_with_an_id(self):
        cases = [("row", "malformed"),
                 ("90 22 0:55 / /var/tmp/other rw - 9p hfs0 rw", "is not 9p hfs0"),
                 ("90 22 0:55 / /var/tmp/hfs-zq3-zq3-r1/m0 rw - ext4 hfs0 rw", "is not 9p hfs0"),
                 ("090 22 0:55 / /var/tmp/hfs-zq3-zq3-r1/m0 rw - 9p hfs0 rw", "positive decimal"),
                 ("0 22 0:55 / /var/tmp/hfs-zq3-zq3-r1/m0 rw - 9p hfs0 rw", "positive decimal"),
                 ("90 22 0:55 / /var/tmp/hfs-zq3-zq3-r1/m0 rw - 9p hfs0 rw\n91 22 0:56 / x rw - 9p hfs0 rw",
                  "exactly one mountinfo row"),
                 (None, "exactly one mountinfo row")]
        _, plan_sha256 = self.bootstrap_run()
        for row, text in cases:
            with self.subTest(row=row):
                self.rewrite_journal("mount-0", lambda detail: detail.update(mountinfo=row))
                self.refuses(self.argv(plan_sha256), text)

    def test_duplicate_mount_ids_refuse(self):
        _, plan_sha256 = self.bootstrap_run(mount_ids=(90, 90))
        self.refuses(self.argv(plan_sha256), "same mount ID")

    def test_admission_evidence_must_agree(self):
        _, plan_sha256 = self.bootstrap_run()
        self.rewrite_journal("admission", lambda detail: detail.update(boot_id=NONCE))
        self.refuses(self.argv(plan_sha256), "admission boot ID differs")
        self.rewrite_journal("admission", lambda detail: detail.update(boot_id=BOOT, provider_nonce=BOOT))
        self.refuses(self.argv(plan_sha256), "boot ID/provider nonce differ")
        self.rewrite_journal("admission", lambda detail: detail.update(provider_nonce=NONCE, manifest_sha256="0" * 64))
        self.refuses(self.argv(plan_sha256), "ManifestHash differs")
        self.rewrite_journal("admission", lambda detail: detail.update(manifest_sha256="x"))
        self.refuses(self.argv(plan_sha256), "not SHA256 hex")

    def test_changed_enroll_request_refuses(self):
        _, plan_sha256 = self.bootstrap_run()
        with open(self.run_dir / "work" / "enroll-request.json", "ab") as stream:
            stream.write(b" ")
        self.refuses(self.argv(plan_sha256), "differs from its journaled SHA256")


class BatchTemplateTests(BatchCase):
    def template_refuses(self, change, text):
        template, template_sha256 = batch_template(self.root, change)
        self.refuses(self.argv(self.plan_sha256, template, template_sha256), text)

    def setUp(self):
        super().setUp()
        _, self.plan_sha256 = self.bootstrap_run()

    def test_template_pin(self):
        template, _ = batch_template(self.root)
        self.refuses(self.argv(self.plan_sha256, template, "0" * 64), "template bytes differ")

    def test_already_filled_or_moved_placeholders_refuse(self):
        self.template_refuses(lambda p: p["requests"][2].update(boot_id=BOOT), "requests[2].boot_id must hold")
        self.template_refuses(lambda p: p.update(installed_manifest_sha256=MANIFEST),
                              "plan.installed_manifest_sha256 must hold")
        self.template_refuses(lambda p: p["requests"][0]["mounts"][1].update(mount_id=fill.MOUNT_IDS[0]),
                              "requests[0].mounts[1].mount_id must hold the placeholder PHASE8_M1_MOUNT_ID")
        self.template_refuses(lambda p: p["requests"][1].update(provider_nonce=fill.BOOT),
                              "provider_nonce must hold")

    def test_unknown_placeholder_refuses(self):
        self.template_refuses(lambda p: p["requests"][0]["mounts"][0].update(session="PHASE8_RUNTIME_UUID"),
                              "unknown run-time placeholder(s) in the template: PHASE8_RUNTIME_UUID")
        self.template_refuses(lambda p: p.update(seconds=fill.SSH_SHA256), "placeholder(s) left")

    def test_template_from_another_bundle_or_run_refuses(self):
        self.template_refuses(lambda p: p["requests"][3]["mounts"][0].update(session=SESSIONS[1]),
                              "path/tag/session differ")
        self.template_refuses(lambda p: p.update(run_id="zq3-r2"), "template run ID differs")

    def test_controller_validation_refusal_leaves_no_output(self):
        self.template_refuses(lambda p: p.update(candidate_manifest_sha256="3" * 64), "pinned admitted")
        self.template_refuses(lambda p: p["requests"].pop(), "exact five-case")


class BootstrapFillTests(Case):
    def template(self, change=None):
        def placeholder(plan):
            plan["observer"]["ssh_sha256"] = fill.SSH_SHA256
            if change:
                change(plan)
        # plan_fixture writes its inputs below a fresh root per template.
        return world_fixtures.plan_fixture(Path(tempfile.mkdtemp(dir=self.root)), placeholder)

    def fact(self, text):
        path = self.root / "p4-f3-ssh.sha256"
        path.write_text(text)
        return path

    def argv(self, fact_text, change=None):
        template, template_sha256 = self.template(change)
        return ["bootstrap", "--template", template, "--template-sha256", template_sha256,
                "--ssh-fact", self.fact(fact_text), "--output", self.out]

    def test_fills_observer_ssh_and_passes_bootstrap_check(self):
        code, report = self.fill(*self.argv(SSH_HASH + " /usr/bin/ssh\n"))
        self.assertEqual(code, 0, report)
        self.assertEqual((report["observer_ssh_sha256"], report["sha256"]), (SSH_HASH, sha(self.out.read_bytes())))
        self.assertEqual(json.loads(self.out.read_bytes())["observer"]["ssh_sha256"], SSH_HASH)
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            self.assertEqual(fill.bootstrap.main(["check", "--plan", str(self.out), "--plan-sha256", report["sha256"]]), 0)
        self.assertEqual(json.loads(stdout.getvalue())["plan_sha256"], report["sha256"])

    def test_template_itself_is_refused_by_bootstrap_check(self):
        template, template_sha256 = self.template()
        with self.assertRaisesRegex(fill.bootstrap.Refused, "observer.ssh_sha256: SHA256 hex"):
            fill.bootstrap.load_plan(str(template), template_sha256)

    def test_fact_file_must_be_one_exact_line_for_the_plan_ssh(self):
        for text, label in [(SSH_HASH + " /usr/local/bin/ssh\n", "not the plan's observer.ssh"),
                            (SSH_HASH + "  /usr/bin/ssh\n", "exactly one"),
                            (SSH_HASH + " /usr/bin/ssh", "exactly one"),
                            (SSH_HASH.upper() + " /usr/bin/ssh\n", "exactly one"),
                            (SSH_HASH + " /usr/bin/ssh\n" + SSH_HASH + " /usr/bin/ssh\n", "exactly one"),
                            ("SHA256 (/usr/bin/ssh) = " + SSH_HASH + "\n", "exactly one"),
                            ("", "exactly one")]:
            with self.subTest(text=text):
                self.refuses(self.argv(text), label)

    def test_template_must_hold_the_placeholder(self):
        self.refuses(self.argv(SSH_HASH + " /usr/bin/ssh\n", lambda p: p["observer"].update(ssh_sha256="b" * 64)),
                     "observer.ssh_sha256 must hold the placeholder")

    def test_bootstrap_validation_refusal_leaves_no_output(self):
        self.refuses(self.argv(SSH_HASH + " /usr/bin/ssh\n", lambda p: p.update(remote_sha256="0" * 64)),
                     "pinned input changed")


if __name__ == "__main__":
    unittest.main()
