#!/usr/bin/env python3
"""Fail closed on absent/partial/skipped kernel and behavioral CI evidence."""

import json
from pathlib import Path
import sys
import uuid
import xml.etree.ElementTree as ET


CASES = {
    "bind": "infrastructure",
    "create-exclusive-rename-replace-hardlink-unlink-open-truncate": "P01",
    "umask-chmod-chown-setgid": "P02",
    "sync-open-fsync-fdatasync-directory-sync": "P04",
    "concurrent-append": "P01",
    "mmap-msync-readback": "P04",
    "multiuser-groups-sticky-permission-cache": "P02",
    "blocked-lock-process-interruption": "lifecycle",
    "process-fcntl-flock-contention-exit": "P03",
    "stop-mutating-peers": "infrastructure",
    "close-cleanup-lock": "infrastructure",
    "cleanup-data": "infrastructure",
    "stop-owned-helpers": "infrastructure",
    "normal-unmount": "lifecycle",
    "mount-loss-no-fallback": "lifecycle",
}
TESTS = {
    "TestLinuxPortableBehavior",
    "TestLinuxPortableBehavior/hfs-direct",
    "TestLinuxPortableBehavior/hfs-buffered",
    "TestLinuxBehaviorEINTRBoundary",
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def profile_report(directory, profile, identity):
    report = json.loads((directory / f"{profile}.json").read_text())
    require(report["profile"] == profile, "wrong profile")
    require(report["platform"] == "linux/amd64", "wrong platform")
    require("single-node local syscall semantics" in report["scope"], "missing scope")
    target = Path(report["target"])
    require(target.is_absolute(), "target is not absolute")
    require(Path(report["run_directory"]).parent == target, "run directory escaped mount")
    mount_identity = report["expected_identity"].splitlines()
    require(len(mount_identity) == 4, "missing mount identity components")
    require(mount_identity[0] == "homelabfs-validation-v1", "wrong mount identity format")
    for component in mount_identity[1:]:
        require(uuid.UUID(component).int != 0, "zero mount identity")
    buffered = profile == "hfs-buffered"
    expected_caps = {
        "direct_io": not buffered, "mmap": buffered,
        "default_permissions": True, "local_locks": True,
        "file_sync": True, "directory_sync": True,
        "normal_publication": False, "arbitrary_credentials": False,
        "writeback": False,
    }
    require(report["declared_capabilities"] == expected_caps, "capability contract changed")
    skips = {"concurrent-append" if buffered else "mmap-msync-readback"}
    if identity != "root" or not buffered:
        skips.add("multiuser-groups-sticky-permission-cache")
    results = report["results"]
    require(len(results) == len(CASES), "incomplete or duplicate semantic cases")
    by_name = {result["name"]: result for result in results}
    require(set(by_name) == set(CASES), "semantic case set changed")
    suite = ET.parse(directory / f"{profile}.xml").getroot()
    require(suite.tag == "testsuite", "JUnit is not a testsuite")
    require(suite.get("name") == f"homelabfs.behavior.{profile}", "wrong JUnit suite")
    require(suite.get("tests") == str(len(CASES)), "wrong JUnit test count")
    require(suite.get("failures") == "0", "JUnit reports failures")
    require(suite.get("skipped") == str(len(skips)), "wrong JUnit skip count")
    properties = {item.get("name"): item.get("value")
                  for item in suite.findall("properties/property")}
    for name in ("target", "profile", "platform", "scope", "run_directory", "expected_identity"):
        require(properties.get(name) == report[name], f"JUnit {name} differs from JSON")
    require(json.loads(properties["declared_capabilities"]) == expected_caps,
            "JUnit capability contract changed")
    xml_cases = suite.findall("testcase")
    require(len(xml_cases) == len(CASES), "incomplete or duplicate JUnit cases")
    xml_by_name = {case.get("name"): case for case in xml_cases}
    require(set(xml_by_name) == set(CASES), "JUnit case set changed")
    for name, family in CASES.items():
        result = by_name[name]
        expected_status = "skip" if name in skips else "pass"
        require(result["family"] == family, f"{name}: wrong family")
        require(result["status"] == expected_status, f"{name}: expected {expected_status}")
        xml_case = xml_by_name[name]
        require(xml_case.get("classname") == family, f"{name}: wrong JUnit family")
        require(xml_case.find("failure") is None and xml_case.find("error") is None,
                f"{name}: failed JUnit case")
        skipped = xml_case.findall("skipped")
        require(len(skipped) == (1 if name in skips else 0), f"{name}: JUnit skip differs")
        if name in skips:
            require(bool(result.get("detail")), f"{name}: no skip reason")
            require(skipped[0].text == result["detail"], f"{name}: skip reason differs")
    return mount_identity


def validate(directory, identity):
    require(identity in {"runner", "root"}, "identity must be runner or root")
    identities = [profile_report(directory, profile, identity)
                  for profile in ("hfs-direct", "hfs-buffered")]
    require(identities[0] != identities[1], "profiles reused the same mount identity")
    events = [json.loads(line) for line in
              (directory / "test-events.jsonl").read_text().splitlines()]
    terminals = [event for event in events if event["Action"] in {"pass", "fail", "skip"}]
    require(all(event["Action"] == "pass" for event in terminals), "kernel test failed/skipped")
    tests = [event["Test"] for event in terminals if "Test" in event]
    require(len(tests) == len(TESTS) and set(tests) == TESTS, "missing/duplicate kernel tests")
    require(any("Test" not in event for event in terminals), "package did not finish")
    require((directory / "wrapper-result.txt").read_text().startswith("exit_status=0\n"),
            "wrapper did not finish successfully")
    print(f"Validated {identity}: direct + buffered JSON/JUnit, P01–P04 and kernel EINTR")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 3:
            raise ValueError("usage: validate-linux-behavior.py REPORT_DIRECTORY runner|root")
        validate(Path(sys.argv[1]), sys.argv[2])
    except (OSError, ValueError, KeyError, TypeError, ET.ParseError) as error:
        sys.exit(f"Linux FUSE evidence rejected: {error}")
