#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Capture two actual Service/journal READDIR results from the lifetime regression."""
import argparse
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    output = args.output.resolve()
    repo = Path(__file__).resolve().parents[5]
    environment = dict(os.environ, GOMAXPROCS='2', GOFLAGS='-p=1',
                       GOMEMLIMIT='1GiB', TMPDIR='/tmp', HOMELABFS_FUSE_INTEGRATION='0',
                       HOMELABFS_READDIR_FIXTURE=str(output / 'host-result.json'))
    command = ['go', 'test', './internal/controlledvalidation', '-count=1',
               '-timeout=60s', '-run', '^TestBridgeReclaimsExactCapabilityLifetimes$']
    with (output / 'capture.log').open('wb') as log:
        result = subprocess.run(command, cwd=repo, env=environment, stdout=log,
                                stderr=subprocess.STDOUT, timeout=120)
    (output / 'receipt.json').write_text(json.dumps({'argv': command, 'exit': result.returncode,
        'scope': 'actual Service/controlled provider/local journal; component credentials; no runtime provider'}, indent=2) + '\n')
    raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
