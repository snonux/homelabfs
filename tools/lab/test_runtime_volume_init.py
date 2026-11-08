#!/usr/bin/env python3
"""Offline tests for runtime-volume-init.py (zq3 G3).

Root is an explicit boundary: tests pass the current UID as the trusted owner
instead of 0. A fake helper emulates cmd/homelabfs for failure paths. Set
HFS_HOMELABFS_TEST_BINARY to a locally built cmd/homelabfs to also run the
real node-init/volume-init/inspect sequence (still no host, VM or mount).
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("runtime_volume_init", Path(__file__).with_name("runtime-volume-init.py"))
INIT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(INIT)

VOLUME_ID = "3f2b8c1e-4d5a-4b6c-8d7e-9f0a1b2c3d4e"

# The fake helper writes the same files and JSON shapes as cmd/homelabfs.
FAKE = r'''#!{python}
import json, os, sys, time, uuid
MODE = {mode!r}
command = sys.argv[1]
if MODE == "hang" and command == "node-init":
    time.sleep(30)
if MODE == "flood" and command == "node-init":
    sys.stdout.write("x" * (200 << 10))
    sys.exit(0)
if command == "node-init":
    os.mkdir(sys.argv[3], 0o700)
    if MODE == "node-fail":
        sys.exit(1)
    node = {{"format_version": 1, "node_id": str(uuid.uuid4())}}
    with open(os.path.join(sys.argv[3], "node.json"), "w") as f:
        json.dump(node, f)
    print(json.dumps(node))
    sys.exit(0)
config = json.load(open(sys.argv[3]))
volume = {{"format_version": 1, "profile": "homelabfs-cli-disposable-self-client-v1",
          "node_id": config["node_id"], "volume_id": config["volume_id"]}}
for key in ("generation", "configuration", "root_allocation", "fixture_partner",
            "fixture_async", "owner_incarnation", "partner_incarnation"):
    volume[key] = "00000000-0000-4000-8000-%012d" % len(key)
if command == "volume-init":
    os.mkdir(config["volume_dir"], 0o700)
    if MODE == "wrong-volume":
        volume["volume_id"] = str(uuid.uuid4())
    with open(os.path.join(config["volume_dir"], "volume.json"), "w") as f:
        json.dump(volume, f)
if command == "inspect" and MODE == "inspect-differs":
    volume["generation"] = str(uuid.uuid4())
print(json.dumps(volume))
'''


class Fixture:
    def __init__(self, root, mode="ok", helper_bytes=None):
        self.root = Path(root)
        self.run = self.root / "run"
        self.run.mkdir(mode=0o700)
        self.bin = self.root / "bin"
        self.bin.mkdir(mode=0o700)
        self.helper = self.bin / "homelabfs"
        if helper_bytes is None:
            helper_bytes = FAKE.format(python=sys.executable, mode=mode).encode()
        self.helper.write_bytes(helper_bytes)
        self.helper.chmod(0o500)
        self.plan = {"version": INIT.PLAN_VERSION, "run_id": "zq3-test", "helper": str(self.helper),
                     "helper_sha256": hashlib.sha256(helper_bytes).hexdigest(),
                     "config": str(self.run / "volume-config.json"), "control_dir": str(self.run / "control"),
                     "volume_dir": str(self.run / "volume"), "mountpoint": str(self.run / "mnt"),
                     "volume_id": VOLUME_ID}
        self.record = str(self.root / "record.json")

    def initialise(self):
        raw = json.dumps(self.plan).encode()
        INIT.validate_plan(self.plan)
        return INIT.initialise(self.plan, hashlib.sha256(raw).hexdigest(), self.record, os.geteuid())

    def result(self):
        return json.loads(Path(self.record).read_text())


class RuntimeVolumeInitTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        os.chmod(self.tmp.name, 0o700)

    def tearDown(self):
        for path in Path(self.tmp.name).rglob("*"):
            if path.is_dir() and not path.is_symlink():
                path.chmod(0o700)
        self.tmp.cleanup()

    def fixture(self, mode="ok", **kwargs):
        return Fixture(tempfile.mkdtemp(dir=self.tmp.name), mode, **kwargs)

    def test_success_records_identities_and_private_mountpoint(self):
        f = self.fixture()
        result = f.initialise()
        self.assertEqual(result["status"], "initialised")
        self.assertTrue(Path(f.record + ".intent").exists())
        config = json.loads(Path(f.plan["config"]).read_text())
        self.assertEqual(config["volume_id"], VOLUME_ID)
        self.assertEqual(config["node_id"], result["node"]["node_id"])
        self.assertEqual(oct(os.stat(f.plan["config"]).st_mode & 0o777), "0o600")
        self.assertEqual(oct(os.stat(f.plan["mountpoint"]).st_mode & 0o777), "0o700")
        self.assertEqual(os.listdir(f.plan["mountpoint"]), [])
        self.assertEqual(f.result()["publication"], None)
        self.assertEqual(set(result["files"]), {"config", "node.json", "volume.json"})

    def test_plan_pin_and_schema_refusals(self):
        f = self.fixture()
        path = Path(f.root) / "plan.json"
        path.write_text(json.dumps(f.plan))
        with self.assertRaisesRegex(RuntimeError, "plan SHA256"):
            INIT.load_plan(str(path), "0" * 64)
        for mutate in (lambda p: p.update(extra=1), lambda p: p.update(volume_id="x"),
                       lambda p: p.update(run_id="Bad"), lambda p: p.update(config="relative"),
                       lambda p: p.update(mountpoint=p["volume_dir"] + "/mnt"),
                       lambda p: p.update(helper=p["control_dir"] + "/homelabfs"),
                       lambda p: p.update(volume_id="00000000-0000-0000-0000-000000000000")):
            plan = dict(f.plan)
            mutate(plan)
            with self.assertRaises(RuntimeError):
                INIT.validate_plan(plan)
        path.write_text('{"version": "a", "version": "b"}')
        with self.assertRaisesRegex(RuntimeError, "duplicate"):
            INIT.load_plan(str(path), hashlib.sha256(path.read_bytes()).hexdigest())

    def test_helper_pin_refusals_happen_before_any_effect(self):
        f = self.fixture()
        f.plan["helper_sha256"] = "1" * 64
        with self.assertRaisesRegex(RuntimeError, "helper SHA256"):
            f.initialise()
        g = self.fixture()
        g.helper.chmod(0o520)
        with self.assertRaisesRegex(RuntimeError, "protected helper"):
            g.initialise()
        for fixture in (f, g):
            self.assertFalse(Path(fixture.record + ".intent").exists())
            self.assertFalse(Path(fixture.plan["control_dir"]).exists())

    def test_existing_resources_are_preserved_and_refused(self):
        f = self.fixture()
        Path(f.plan["control_dir"]).mkdir(mode=0o700)
        with self.assertRaisesRegex(RuntimeError, "already exists"):
            f.initialise()
        self.assertTrue(Path(f.plan["control_dir"]).is_dir())
        g = self.fixture()
        Path(g.record).write_text("{}")
        with self.assertRaisesRegex(RuntimeError, "already exists"):
            g.initialise()
        h = self.fixture()
        os.chmod(h.run, 0o770)
        with self.assertRaisesRegex(RuntimeError, "parent"):
            h.initialise()

    def test_failed_steps_leave_records_and_partial_resources(self):
        for mode, stage in (("node-fail", "node-init"), ("wrong-volume", "volume-init"),
                            ("inspect-differs", "inspect")):
            with self.subTest(mode=mode):
                f = self.fixture(mode)
                with self.assertRaises(RuntimeError):
                    f.initialise()
                result = f.result()
                self.assertEqual((result["status"], result["stage"]), ("failed", stage))
                self.assertTrue(Path(f.plan["control_dir"]).is_dir())  # never deleted
                self.assertFalse(Path(f.plan["mountpoint"]).exists())

    def test_overflow_and_deadline_are_uncertain(self):
        f = self.fixture("flood")
        with self.assertRaises(INIT.CommandUncertain):
            f.initialise()
        self.assertEqual(f.result()["status"], "uncertain")
        g = self.fixture("hang")
        with patch.object(INIT, "DEADLINE", 1):
            with self.assertRaises(INIT.CommandUncertain):
                g.initialise()
        self.assertEqual(g.result()["status"], "uncertain")

    def test_main_requires_root(self):
        if os.geteuid() == 0:
            self.skipTest("running as root")
        with self.assertRaisesRegex(RuntimeError, "root"):
            INIT.main(["--plan", "/x", "--plan-sha256", "0" * 64, "--record", "/y"])

    @unittest.skipUnless(os.environ.get("HFS_HOMELABFS_TEST_BINARY"), "no locally built cmd/homelabfs")
    def test_real_helper_sequence(self):
        f = self.fixture(helper_bytes=Path(os.environ["HFS_HOMELABFS_TEST_BINARY"]).read_bytes())
        result = f.initialise()
        self.assertEqual(result["volume"]["volume_id"], VOLUME_ID)
        self.assertEqual(result["volume"]["profile"], INIT.PROFILE)
        self.assertTrue(Path(f.plan["volume_dir"], "volume.json").is_file())
        shutil.rmtree(f.root, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
