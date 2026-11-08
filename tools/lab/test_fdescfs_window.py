#!/usr/bin/env python3
"""Offline failure/ownership regressions; every host operation is substituted."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("fdescfs_window", Path(__file__).with_name("fdescfs-window.py"))
WINDOW = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WINDOW)

MODULES = """Id Refs Address Size Name
 1 92 0xffffffff80200000 2351f30 kernel (/boot/kernel/kernel)
    Contains modules:
         Id Name
        402 cubic
        387 if_lo
"""
ADDED = """ 2 1 0xffffffff82000000 100 fdescfs.ko (/boot/kernel/fdescfs.ko)
    Contains modules:
         Id Name
         20 fdescfs
"""
# The plan-declared window module, as loaded by zq3 Phase 4 step 4 (j44).
FUSEFS = """ 3 1 0xffffffff82100000 200 fusefs.ko (/boot/kernel/fusefs.ko)
    Contains modules:
         Id Name
         21 fusefs
"""
OTHER = """ 4 1 0xffffffff82200000 300 if_wg.ko (/boot/kernel/if_wg.ko)
    Contains modules:
         Id Name
         22 wg
"""
FUSEFS_PATH = "/boot/kernel/fusefs.ko"
FUSEFS_SHA = "b" * 64


def descriptor(supported=False, identity="00000000:00000001"):
    return {"mount_type": "fdescfs" if supported else "devfs",
            "mount_id": identity, "supported": supported}


def baseline():
    return {"module": {"exit": 0}, "modules": MODULES + ADDED,
            "descriptor": descriptor(), "mounts": "devfs /dev devfs rw 0 0\n",
            "boot": "same-boot"}


class WindowTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name) / "run"
        self.directory.mkdir()
        self.plan = {"before": baseline(), "namespace_helper": "/pinned/helper",
                     "module_policy": "preserve-existing-or-retain-new",
                     "window_module": {"path": FUSEFS_PATH, "sha256": FUSEFS_SHA},
                     "sha256": {"/boot/kernel/fdescfs.ko": "a" * 64, FUSEFS_PATH: FUSEFS_SHA}}
        WINDOW.persist(self.directory / "plan.json", self.plan)

    def own(self):
        owned = descriptor(True)
        WINDOW.persist(self.directory / "owned.json", owned)
        WINDOW.persist(self.directory / "module-baseline.json", self.plan["before"])
        return owned

    def test_changed_fsid_never_unmounts(self):
        self.own()
        with patch.object(WINDOW, "inspect", return_value=descriptor(True, "changed")), patch.object(WINDOW, "command") as command:
            with self.assertRaisesRegex(RuntimeError, "FSID/profile changed"):
                WINDOW.restore(self.plan, self.directory, "/inspector")
            command.assert_not_called()
        self.assertFalse((self.directory / "restore.intent.json").exists())

    def test_uncertain_unmount_cannot_be_replayed(self):
        owned = self.own()
        uncertain = WINDOW.CommandUncertain({"failure": "lost result", "pid": 1234, "stdout_base64": ""})
        with patch.object(WINDOW, "inspect", return_value=owned), patch.object(WINDOW, "command", side_effect=uncertain) as command:
            with self.assertRaises(WINDOW.CommandUncertain):
                WINDOW.restore(self.plan, self.directory, "/inspector")
            with self.assertRaisesRegex(RuntimeError, "already dispatched"):
                WINDOW.restore(self.plan, self.directory, "/inspector")
            self.assertEqual(command.call_count, 1)
        self.assertTrue((self.directory / "restore.intent.json").exists())
        self.assertEqual(WINDOW.load(self.directory / "restore.uncertain.json")["pid"], 1234)
        self.assertFalse((self.directory / "restore.result.json").exists())

    def test_busy_normal_unmount_retains_ownership(self):
        owned = self.own()
        with patch.object(WINDOW, "inspect", return_value=owned), patch.object(WINDOW, "facts", return_value=self.plan["before"]), patch.object(WINDOW, "command", return_value={"exit": 1, "stdout": "", "stderr": "busy"}):
            with self.assertRaisesRegex(RuntimeError, "restore incomplete"):
                WINDOW.restore(self.plan, self.directory, "/inspector")
        self.assertTrue((self.directory / "owned.json").exists())
        self.assertFalse((self.directory / "restoration-summary.json").exists())

    def test_retained_module_is_not_full_restoration(self):
        owned = self.own()
        WINDOW.persist(self.directory / "module-retained.json", {"retained": True})
        with patch.object(WINDOW, "inspect", return_value=owned), patch.object(WINDOW, "facts", return_value=self.plan["before"]), patch.object(WINDOW, "command", return_value={"exit": 0, "stdout": json.dumps({"unmounted_fsid": owned["mount_id"]})}):
            WINDOW.restore(self.plan, self.directory, "/inspector")
        summary = WINDOW.load(self.directory / "restoration-summary.json")
        self.assertTrue(summary["mount_restored"])
        self.assertTrue(summary["module_retained_by_reviewed_plan"])
        self.assertFalse(summary["full_original_system_state_restored"])

    def restore_after(self, modules, file_sha=FUSEFS_SHA, plan=None, **changes):
        """Restore with substituted post-unmount facts; return the refusal or None.

        Only the inspector, facts, helper command and file digest boundaries
        are substituted; intent/result/summary evidence is real.
        """
        plan = plan or self.plan
        owned = self.own()
        after = dict(plan["before"], modules=modules, **changes)
        reply = {"exit": 0, "stdout": json.dumps({"unmounted_fsid": owned["mount_id"]})}
        with patch.object(WINDOW, "inspect", return_value=owned), \
                patch.object(WINDOW, "facts", return_value=after), \
                patch.object(WINDOW, "command", return_value=reply), \
                patch.object(WINDOW, "digest", return_value=file_sha):
            try:
                WINDOW.restore(plan, self.directory, "/inspector")
            except RuntimeError as failure:
                self.assertTrue((self.directory / "owned.json").exists())
                self.assertFalse((self.directory / "restoration-summary.json").exists())
                return failure
        return None

    def test_declared_window_module_is_accepted(self):
        self.assertIsNone(self.restore_after(MODULES + ADDED + FUSEFS))
        summary = WINDOW.load(self.directory / "restoration-summary.json")
        self.assertTrue(summary["mount_restored"])
        self.assertEqual(summary["window_module"], self.plan["window_module"])
        self.assertEqual(summary["window_module_loaded"][0][3:], ["fusefs.ko", FUSEFS_PATH])
        self.assertFalse(summary["full_original_system_state_restored"])

    def test_declared_window_module_may_stay_unloaded(self):
        self.assertIsNone(self.restore_after(MODULES + ADDED))
        summary = WINDOW.load(self.directory / "restoration-summary.json")
        self.assertIsNone(summary["window_module_loaded"])
        self.assertTrue(summary["full_original_system_state_restored"])

    def test_window_module_other_differences_refuse(self):
        cases = {
            "other_module": (MODULES + ADDED + OTHER, {}, "unexpected module"),
            "extra_unexpected_module": (MODULES + ADDED + FUSEFS + OTHER, {}, "unexpected module"),
            "wrong_hash": (MODULES + ADDED + FUSEFS, {"file_sha": "c" * 64}, "file hash changed"),
            "missing_fdescfs": (MODULES + FUSEFS, {}, "changed or missing"),
            "foreign_pathname": (MODULES + ADDED + FUSEFS.replace("(/boot/kernel/", "(/tmp/"),
                                 {}, "pathname changed"),
            "unregistered": (MODULES + ADDED + FUSEFS.replace(" 21 fusefs", " 21 other"),
                             {}, "not registered"),
            "changed_mounts": (MODULES + ADDED + FUSEFS,
                               {"mounts": "devfs /dev devfs rw 0 0\n/dev/fuse /mnt fusefs rw 0 0\n"},
                               "restore incomplete"),
        }
        for name, (modules, changes, pattern) in cases.items():
            with self.subTest(name=name):
                self.setUp()
                self.assertRegex(str(self.restore_after(modules, **changes)), pattern)

    def test_undeclared_window_module_refuses(self):
        plan = dict(self.plan, window_module=None)
        WINDOW.persist(self.directory / "plan.json.tmp", plan)
        (self.directory / "plan.json.tmp").replace(self.directory / "plan.json")
        self.assertRegex(str(self.restore_after(MODULES + ADDED + FUSEFS, plan=plan)),
                         "unexpected module")

    def test_window_module_declaration_is_checked(self):
        WINDOW.check_window_module(self.plan)
        WINDOW.check_window_module(dict(self.plan, window_module=None))
        cases = {
            "fdescfs": {"path": "/boot/kernel/fdescfs.ko", "sha256": "a" * 64},
            "foreign": {"path": "/tmp/fusefs.ko", "sha256": FUSEFS_SHA},
            "unpinned_hash": {"path": FUSEFS_PATH, "sha256": "d" * 64},
            "extra_key": {"path": FUSEFS_PATH, "sha256": FUSEFS_SHA, "name": "fusefs"},
            "not_object": [FUSEFS_PATH, FUSEFS_SHA],
        }
        for name, declared in cases.items():
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                WINDOW.check_window_module(dict(self.plan, window_module=declared))

    def test_existing_mount_preserved(self):
        original = descriptor(True)
        WINDOW.persist(self.directory / "existing.json", original)
        with patch.object(WINDOW, "inspect", return_value=original), patch.object(WINDOW, "command") as command:
            WINDOW.restore(self.plan, self.directory, "/inspector")
            command.assert_not_called()

    def test_module_load_rejects_unrelated_change(self):
        before = baseline()
        before["module"] = {"exit": 1}
        before["modules"] = MODULES
        after = copy.deepcopy(before)
        after.update(module={"exit": 0}, modules=MODULES + ADDED, boot="changed")
        with patch.object(WINDOW, "command", return_value={"exit": 0}), patch.object(WINDOW, "facts", return_value=after):
            with self.assertRaisesRegex(RuntimeError, "unrelated state changed"):
                WINDOW.load_module(self.plan, self.directory, "/inspector", before)
        self.assertFalse((self.directory / "module-baseline.json").exists())

    def test_module_load_records_retention(self):
        before = baseline()
        before["module"] = {"exit": 1}
        before["modules"] = MODULES
        after = copy.deepcopy(before)
        after.update(module={"exit": 0}, modules=MODULES + ADDED)
        with patch.object(WINDOW, "command", return_value={"exit": 0}), patch.object(WINDOW, "facts", return_value=after):
            self.assertEqual(WINDOW.load_module(self.plan, self.directory, "/inspector", before), after)
        self.assertTrue((self.directory / "module-retained.json").exists())

    def test_module_load_rejects_foreign_pathname(self):
        before = baseline()
        before["module"] = {"exit": 1}
        before["modules"] = MODULES
        after = copy.deepcopy(before)
        after.update(module={"exit": 0}, modules=MODULES + ADDED.replace("/boot/kernel/fdescfs.ko", "/foreign/fdescfs.ko"))
        with patch.object(WINDOW, "command", return_value={"exit": 0}), patch.object(WINDOW, "facts", return_value=after):
            with self.assertRaisesRegex(RuntimeError, "pathname changed"):
                WINDOW.load_module(self.plan, self.directory, "/inspector", before)
        self.assertFalse((self.directory / "module-retained.json").exists())

    def test_live_module_refs_do_not_change_identity(self):
        self.assertEqual(WINDOW.module_files(MODULES), WINDOW.module_files(MODULES.replace(" 1 92 ", " 1 93 ")))

    def test_actual_selected_host_inventory_preserves_pathnames(self):
        actual = Path(__file__).with_name("testdata") / "fdescfs-kldstat-f3-20261105.txt"
        groups = WINDOW.module_files(actual.read_text())
        self.assertEqual(len(groups), 24)
        self.assertEqual(groups["kernel"][0], ["1", "0xffffffff80200000", "2351f30", "kernel", "/boot/kernel/kernel"])
        self.assertEqual(groups["vmm.ko"][0][-1], "/boot/kernel/vmm.ko")
        changed = actual.read_text().replace("(/boot/kernel/vmm.ko)", "(/foreign/vmm.ko)")
        self.assertNotEqual(groups, WINDOW.module_files(changed))

    def test_malformed_module_inventory_refuses(self):
        cases = {
            "missing_path": MODULES.replace(" (/boot/kernel/kernel)", ""),
            "relative_path": MODULES.replace("/boot/kernel/kernel", "kernel"),
            "unclosed_path": MODULES.replace("kernel)", "kernel"),
            "wrong_columns": MODULES.replace("Address Size", "Address Bytes"),
            "missing_block_heading": MODULES.replace("    Contains modules:\n", ""),
            "missing_module_columns": MODULES.replace("         Id Name\n", ""),
            "invalid_entry": MODULES + "not a module row\n",
            "malformed_later_file": MODULES + ADDED.replace(" (/boot/kernel/fdescfs.ko)", ""),
            "duplicate_file": MODULES + MODULES.split("\n", 1)[1],
            "duplicate_file_id": MODULES + ADDED.replace(" 2 1 ", " 1 1 "),
            "incomplete_block": MODULES.split("    Contains modules:")[0],
        }
        for name, text in cases.items():
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                WINDOW.module_files(text)

    def setup_attempt(self, before, after, helper_fsid="00000000:00000001"):
        # Only the root-UID admission boundary is substituted. Real private
        # directories and durable evidence are exercised; no host command runs.
        directory = Path(self.temp.name) / "new-run"
        plan = dict(self.plan, before=before)
        original_stat = Path.stat

        def root_stat(path, *args, **kwargs):
            values = list(original_stat(path, *args, **kwargs))
            values[4] = 0
            return os.stat_result(values)

        with patch.object(Path, "stat", root_stat), patch.object(WINDOW, "facts", side_effect=[before, after]), patch.object(WINDOW, "command", return_value={"exit": 0, "stdout": json.dumps({"mounted_fsid": helper_fsid})}) as command:
            try:
                WINDOW.execute(plan, directory, "/inspector")
            except RuntimeError as failure:
                return directory, command.call_count, failure
        return directory, command.call_count, None

    def test_only_completed_matching_mount_gets_ownership(self):
        before = baseline()
        after = copy.deepcopy(before)
        after.update(descriptor=descriptor(True), mounts=before["mounts"] + "fdescfs /dev/fd fdescfs rw,nosuid 0 0\n")
        directory, calls, failure = self.setup_attempt(before, after)
        self.assertIsNone(failure)
        self.assertEqual(calls, 1)
        self.assertEqual(WINDOW.load(directory / "owned.json"), after["descriptor"])

    def test_mismatched_helper_fsid_preserves_without_claiming_ownership(self):
        before = baseline()
        after = copy.deepcopy(before)
        after.update(descriptor=descriptor(True), mounts=before["mounts"] + "fdescfs /dev/fd fdescfs rw,nosuid 0 0\n")
        directory, calls, failure = self.setup_attempt(before, after, "foreign")
        self.assertIn("FSID mismatch", str(failure))
        self.assertEqual(calls, 1)
        self.assertFalse((directory / "owned.json").exists())
        self.assertTrue((directory / "mount.result.json").exists())

    def test_existing_foreign_mount_refuses_before_module_load(self):
        before = baseline()
        before["module"] = {"exit": 1}
        before["mounts"] += "foreign /dev/fd/child tmpfs rw 0 0\n"
        directory, calls, failure = self.setup_attempt(before, before)
        self.assertIn("existing /dev/fd mount", str(failure))
        self.assertEqual(calls, 0)
        self.assertFalse((directory / "module-load.intent.json").exists())


if __name__ == "__main__":
    unittest.main()
