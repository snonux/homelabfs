#!/usr/bin/env python3
"""Local tests of the controller's post-migration targets and host-key pins (G8).

No SSH, no network and no key scanning: pin files are the reviewed repository
copies or local edits of them; refusals must happen before any process starts.
"""
import base64
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from test_preparation import GUEST_PIN, HOST_PIN, batch_fixture, controller


class TargetTests(unittest.TestCase):
    def snapshot(self, root, plan, pins=None):
        """Write the (changed) plan with fresh pin hashes, then validate it."""
        for name, raw in (pins or {}).items():
            Path(plan[name]["known_hosts"]).write_bytes(raw)
            plan[name]["known_hosts_sha256"] = controller.digest(raw)
        path = root / "changed-plan.json"
        path.write_text(json.dumps(plan))
        with mock.patch.object(controller.subprocess, "Popen") as spawn:
            try:
                return controller.plan_snapshot(path)
            finally:
                spawn.assert_not_called()

    def fixture(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        _, plan, probe = batch_fixture(root)
        patch = mock.patch.object(controller, "GUEST", probe)
        patch.start()
        self.addCleanup(patch.stop)
        return root, plan

    def test_primary_targets_and_reviewed_pins_pass(self):
        root, plan = self.fixture()
        _, _, inputs = self.snapshot(root, plan)
        self.assertEqual(inputs["guest-known_hosts"], GUEST_PIN)
        self.assertEqual(controller.TARGETS["host"][0], "10.20.0.133")
        self.assertEqual(controller.TARGETS["guest"][0], "10.20.0.123")

    def test_retired_aliases_refuse_with_reason(self):
        for name, old, primary in [("host", "192.168.1.133", "10.20.0.133"),
                                   ("guest", "192.168.1.123", "10.20.0.123")]:
            with self.subTest(target=name):
                root, plan = self.fixture()
                plan[name]["address"] = old
                with self.assertRaisesRegex(controller.Refused,
                                            "pre-migration address " + old + ".*primary " + primary):
                    self.snapshot(root, plan)

    def test_other_targets_refuse(self):
        for name, field, value in [("host", "address", "10.20.0.123"),
                                   ("guest", "address", "10.20.0.133"),
                                   ("guest", "address", "10.20.0.124"),
                                   ("host", "alias", "rocky"), ("guest", "alias", "f3"),
                                   ("host", "user", "root"), ("guest", "user", "paul")]:
            with self.subTest(target=name, field=field, value=value):
                root, plan = self.fixture()
                plan[name][field] = value
                with self.assertRaises(controller.Refused):
                    self.snapshot(root, plan)

    def test_pin_content_must_be_the_reviewed_key_for_the_primary(self):
        rocky_key = GUEST_PIN.split()[2]
        f3_key = HOST_PIN.split()[2]
        rsa = base64.b64encode(b"\0\0\0\x07ssh-rsa" + b"\0" * 40)
        short = base64.b64encode(b"\0\0\0\x0bssh-ed25519\0\0\0\x20" + b"\0" * 31)
        bad = {
            "retired address in pin": b"rocky,192.168.1.123 ssh-ed25519 " + rocky_key + b"\n",
            "both addresses": b"rocky,10.20.0.123,192.168.1.123 ssh-ed25519 " + rocky_key + b"\n",
            "alias only": b"rocky ssh-ed25519 " + rocky_key + b"\n",
            "f3 key for rocky": b"rocky,10.20.0.123 ssh-ed25519 " + f3_key + b"\n",
            "second line": GUEST_PIN + b"rocky,10.20.0.123 ssh-ed25519 " + rocky_key + b"\n",
            "rsa type": b"rocky,10.20.0.123 ssh-rsa " + rsa + b"\n",
            "rsa blob": b"rocky,10.20.0.123 ssh-ed25519 " + rsa + b"\n",
            "short blob": b"rocky,10.20.0.123 ssh-ed25519 " + short + b"\n",
            "not base64": b"rocky,10.20.0.123 ssh-ed25519 !!!!\n",
            "trailing comment": GUEST_PIN.rstrip(b"\n") + b" comment\n",
            "empty": b"",
            "non-ascii": "rocky,10.20.0.123 ssh-ed25519 é\n".encode(),
        }
        for label, raw in bad.items():
            with self.subTest(pin=label):
                root, plan = self.fixture()
                with self.assertRaises(controller.Refused):
                    self.snapshot(root, plan, {"guest": raw})

    def test_comment_lines_in_pin_are_ignored(self):
        root, plan = self.fixture()
        self.snapshot(root, plan, {"host": b"# reviewed 2026-11-08\n" + HOST_PIN})

    def test_repository_pins_match_reviewed_fingerprints(self):
        for raw, target in [(HOST_PIN, {"alias": "f3", "address": "10.20.0.133"}),
                            (GUEST_PIN, {"alias": "rocky", "address": "10.20.0.123"})]:
            controller.check_pin(raw, target)
            self.assertNotIn(b"192.168.", raw)
            self.assertTrue(raw.endswith(b"\n") and raw.count(b"\n") == 1)

    def test_plan_with_unchanged_hash_but_swapped_pin_files_refuses(self):
        # The hashes match the swapped bytes, so only the content binding refuses.
        root, plan = self.fixture()
        with self.assertRaisesRegex(controller.Refused, "exactly one line"):
            self.snapshot(root, plan, {"host": GUEST_PIN, "guest": HOST_PIN})


if __name__ == "__main__":
    unittest.main()
