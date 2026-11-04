#!/usr/bin/env python3
"""Prepare a new owned pinned patch subset; never modify a preexisting tree."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import urllib.request

root = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("output", type=Path)
args = parser.parse_args()
manifest = json.loads((root / "upstream.json").read_text())
args.output.mkdir(parents=True, exist_ok=False)
for name, digest in manifest["files"].items():
    url = ("https://raw.githubusercontent.com/freebsd/freebsd-src/" +
           manifest["revision"] + "/" + name)
    data = urllib.request.urlopen(url, timeout=30).read()
    if hashlib.sha256(data).hexdigest() != digest:
        raise SystemExit("upstream SHA-256 mismatch: " + name)
    target = args.output / name
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
subprocess.run(["patch", "--batch", "--forward", "--fuzz=0", "-p1", "-i",
                str(root / "patches/0001-dedicated-backend.patch")],
               cwd=args.output, check=True)
dest = args.output / "contrib/lib9p/backend"
dest.mkdir(parents=True)
for name in ("hfs9p.c", "hfs9p.h", "hfs9p_json.c", "hfs9p_json.h",
             "hfs9p_ledger.c", "hfs9p_ledger.h",
             "hfs9p_supervisor.c", "hfs9p_supervisor.h", "hfs9p_recovery.inc"):
    shutil.copyfile(root / "src" / name, dest / name)
print("Prepared pinned subset:", args.output)
print("This is not a full FreeBSD source tree or a built/installed artifact.")
