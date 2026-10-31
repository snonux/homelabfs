#!/usr/bin/env python3
"""Run the bounded transaction composition profiles sequentially; retain exact TLC receipts."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time

SHA256 = "fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e"
ROOT = Path(__file__).resolve().parents[2]
MODELS = ROOT / "spec/tla"
CACHE = ROOT / ".cache/tla"


def tree_bytes(root):
    total = 0
    for base, _, names in os.walk(root):
        for name in names:
            try:
                total += Path(base, name).stat().st_size
            except FileNotFoundError:
                pass  # TLC may rotate checkpoint files while the monitor scans.
    return total


def execute(command, output, run, seconds):
    """Deadlines/resource limits are failures; keep all partial evidence."""
    start = time.monotonic()
    with subprocess.Popen(command, cwd=ROOT, stdout=output, stderr=subprocess.STDOUT) as proc:
        while True:
            try:
                return proc.wait(timeout=1), None
            except subprocess.TimeoutExpired:
                reason = None
                if time.monotonic() - start >= seconds:
                    reason = "wall deadline"
                elif tree_bytes(run) > 2 * 1024**3:
                    reason = "2 GiB profile evidence limit"
                elif sum(tree_bytes(p) for p in CACHE.glob("OriginTransactions-*") if p.is_dir()) > 4 * 1024**3:
                    reason = "4 GiB aggregate evidence limit"
                if reason:
                    proc.terminate()
                    try:
                        proc.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait()
                    return proc.returncode, reason


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profiles", nargs="*", help="OriginTransactions configuration names without .cfg")
    parser.add_argument("--group", choices=("safety", "negative", "witness", "all"))
    parser.add_argument("--seconds", type=int, default=180, help="per-check wall limit; expiry is a failure")
    args = parser.parse_args()
    if args.seconds <= 0:
        parser.error("--seconds must be positive")
    profiles = args.profiles
    if args.group:
        if profiles:
            parser.error("select profiles or --group")
        profiles = sorted(p.stem for p in MODELS.glob("OriginTransactions*.cfg")
                          if args.group == "all"
                          or (args.group == "negative" and "Negative" in p.stem)
                          or (args.group == "witness" and "Witness" in p.stem)
                          or (args.group == "safety" and "Negative" not in p.stem and "Witness" not in p.stem))
    if not profiles:
        parser.error("select at least one profile or --group")
    for profile in profiles:
        if not re.fullmatch(r"OriginTransactions[A-Za-z]+", profile) or not (MODELS / (profile + ".cfg")).is_file():
            parser.error("unknown profile: " + profile)
    jar = CACHE / "tla2tools-1.8.0.jar"
    if not jar.is_file() or hashlib.sha256(jar.read_bytes()).hexdigest() != SHA256:
        sys.exit("Pinned TLC 1.8.0 jar absent or hash mismatch; use the existing Mage download/pin setup first")
    if not shutil.which("java"):
        sys.exit("Java is required")
    if sum(tree_bytes(p) for p in CACHE.glob("OriginTransactions-*") if p.is_dir()) >= 4 * 1024**3:
        sys.exit("Existing OriginTransactions evidence reaches the 4 GiB limit; preserve it for review")
    evidence = Path(tempfile.mkdtemp(prefix="OriginTransactions-", dir=CACHE))
    shutil.copyfile(Path(__file__), evidence / "runner.py")
    print("Evidence:", evidence, flush=True)
    receipts = []
    failed = False
    for profile in profiles:
        run = evidence / profile
        run.mkdir()
        for filename in ("OriginTransactions.tla", profile + ".cfg"):
            shutil.copyfile(MODELS / filename, run / filename)
        command = ["java", "-Xmx1024m", "-XX:ActiveProcessorCount=1", "-XX:+UseParallelGC",
                   "-cp", str(jar), "tlc2.TLC", "-workers", "1", "-noGenerateSpecTE",
                   "-lncheck", "final", "-metadir", str(run / "states"), "-config",
                   str(run / (profile + ".cfg")), str(run / "OriginTransactions.tla")]
        (run / "argv.json").write_text(json.dumps(command, indent=2) + "\n")
        meta = dict(profile=profile, jar_sha256=SHA256,
                    runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), heap_mib=1024, workers=1,
                    timeout_seconds=args.seconds, state_cutoff=None, depth_cutoff=None,
                    profile_bytes_limit=2 * 1024**3, aggregate_bytes_limit=4 * 1024**3,
                    source_sha256=hashlib.sha256((run / "OriginTransactions.tla").read_bytes()).hexdigest(),
                    config_sha256=hashlib.sha256((run / (profile + ".cfg")).read_bytes()).hexdigest())
        started = time.monotonic()
        with (run / "output.log").open("w") as output:
            code, limit_reason = execute(command, output, run, args.seconds)
        (run / "exit").write_text(str(code) + "\n")
        log = (run / "output.log").read_text()
        expected = "WitnessAbsent" if "Witness" in profile else "Safety" if "Negative" in profile else None
        if expected:
            passed = code == 12 and ("Invariant " + expected + " is violated") in log
        else:
            passed = code == 0 and "Model checking completed. No error has been found." in log and re.search(r"0 states left on queue", log) is not None
        passed = passed and limit_reason is None
        counts = re.findall(r"([\d,]+) states generated, ([\d,]+) distinct states found, ([\d,]+) states left on queue", log)
        if counts:
            meta.update(zip(("generated", "distinct", "queued"), (int(x.replace(",", "")) for x in counts[-1])))
        depths = re.findall(r"depth of the complete state graph search is (\d+)", log)
        if depths:
            meta["depth"] = int(depths[-1])
        meta.update(limit_reason=limit_reason, elapsed_seconds=round(time.monotonic()-started, 3), exit=code, expected_invariant=expected, passed=passed)
        (run / "receipt.json").write_text(json.dumps(meta, indent=2) + "\n")
        receipts.append(meta)
        (evidence / "receipts.json").write_text(json.dumps(receipts, indent=2) + "\n")
        print(profile, "PASS" if passed else "FAIL", "exit=" + str(code),
              "distinct=" + str(meta.get("distinct", "unknown")), flush=True)
        failed |= not passed
        if not passed:
            print(log[-5000:], flush=True)
            break
    return int(failed)


if __name__ == "__main__":
    sys.exit(main())
