#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Capture an actual production host ROOT result without modifying Go source."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    repo = here.parents[4]
    output = args.output.absolute()
    output.mkdir(mode=0o700, exist_ok=False)
    driver = output / 'capture_test.go'
    driver.write_bytes((here / 'capture_test.go.txt').read_bytes())
    overlay = output / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(repo / 'internal/guestbridge/vz3_fixture_export_test.go'): str(driver)}}))
    environment = dict(os.environ, GOMAXPROCS='2', GOMEMLIMIT='512MiB',
                       HFS_ROOT_FIXTURE_OUTPUT=str(output / 'host-result.json'))
    os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
    command = ['go', 'test', '-overlay', str(overlay), '-p', '2', '-run',
               '^TestVZ3ExportActualHostROOT$', '-count=1', '-timeout=60s',
               './internal/guestbridge']
    sources = {str(p.relative_to(repo)): hashlib.sha256(p.read_bytes()).hexdigest()
               for p in sorted((repo / 'internal').rglob('*.go'))}
    with (output / 'capture.log').open('wb') as log:
        result = subprocess.run(command, cwd=repo, env=environment,
                                stdout=log, stderr=subprocess.STDOUT, timeout=180)
    receipt = {'argv': command, 'exit': result.returncode, 'source': sources,
               'scope': 'actual production formatter via component fixture; no runtime provider'}
    if result.returncode == 0:
        receipt['fixture_sha256'] = hashlib.sha256((output / 'host-result.json').read_bytes()).hexdigest()
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
