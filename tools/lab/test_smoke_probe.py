"""Local-only tests: temporary files, no mounts, guests or remote transports."""
import importlib.util
import os
import pathlib
import tempfile
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("smoke_probe", pathlib.Path(__file__).with_name("smoke-probe.py"))
probe_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe_module)


class SmokeProbeTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.previous = os.open(".", os.O_RDONLY | os.O_DIRECTORY)
        os.chdir(self.directory.name)
        request = {"protocol": "hfs-live-v1", "volume": "hfs-smoke-unit-fake", "operation": "L02", "plan_hash": "a" * 64,
                   "plan": {"live": {"volumes": [{"id": "hfs-smoke-unit-fake"}],
                                     "max_operations": 1000, "uid": 21001, "gid": 21001, "denied_uid": 21002},
                            "budget": {"io_bytes": 16 << 20}}}
        self.probe = probe_module.Probe(request)

    def tearDown(self):
        os.fchdir(self.previous)
        os.close(self.previous)
        self.directory.cleanup()

    def test_write_sync_reopen(self):
        self.probe.bytes_io()
        self.assertEqual(self.probe.checks, ["write-fsync-reopen-checksum"])
        self.assertEqual(self.probe.history[0]["length"], 4096)

    def test_metadata_semantics(self):
        self.probe.semantics()
        self.assertEqual(self.probe.checks, ["mkdir-rmdir", "rename-replace", "truncate", "hardlink", "unlink-open"])

    def test_two_process_locks_and_mmap(self):
        self.probe.locking()
        self.assertEqual(self.probe.checks, ["flock-contention", "fcntl-contention", "mmap-flush-sync"])

    def test_sqlite_modes_reopen_integrity(self):
        self.probe.databases()
        self.assertEqual(self.probe.checks, ["sqlite-delete-full", "sqlite-wal-full", "sqlite-reopen-integrity"])

    def test_bounded_independent_file_mix(self):
        self.probe.mix()
        self.assertEqual(self.probe.checks, ["bounded-mix"])
        self.assertEqual(len(os.listdir(".")), 8)

    def test_budget_rejected_before_payload(self):
        self.probe.byte_limit = 1
        with self.assertRaisesRegex(RuntimeError, "reservation exhausted"):
            self.probe.bytes_io()
        self.assertEqual(os.listdir("."), [])

    def test_raw_fallback_mount_rejected_before_directory_creation(self):
        self.probe.plan.update({"stage": "stage2", "profile": "live-smoke", "run_id": "unit", "disruptions": []})
        mount = "/mnt/hfs-smoke/hfs-smoke-unit-fake"
        self.probe.volume.update({"guest_mount": mount, "export": "hfs-smoke-unit-fake"})
        record = "23 1 0:1 / " + mount + " rw - ext4 /dev/fake rw\n"
        with mock.patch("builtins.open", mock.mock_open(read_data=record)), mock.patch.object(
                os.path, "realpath", side_effect=lambda path: path), mock.patch.object(os, "mkdir") as create:
            with self.assertRaisesRegex(RuntimeError, "exact deployed 9p mount/export"):
                self.probe.enter()
            create.assert_not_called()

    @unittest.skipUnless(os.geteuid() == 0, "numeric identity switching requires root")
    def test_numeric_identities(self):
        self.probe.permissions()
        self.assertEqual(self.probe.checks, ["uid-gid-mode", "allowed-access", "denied-access"])


if __name__ == "__main__":
    unittest.main()
