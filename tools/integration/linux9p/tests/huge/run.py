#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Exercise the actual common THP eligibility check with primitive fixtures."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    args.output.mkdir(parents=False, exist_ok=False, mode=0o700)
    source = (args.source / 'mm/huge_memory.c').read_text()
    first = source.index('bool hugepage_vma_check(')
    last = source.index('\nstatic bool get_huge_zero_page(', first)
    (args.output / 'production.inc').write_text(source[first:last])
    (args.output / 'test.c').write_bytes((here / 'test.c').read_bytes())
    receipt = {'source_sha256': hashlib.sha256(source.encode()).hexdigest(),
               'check_sha256': hashlib.sha256(source[first:last].encode()).hexdigest(),
               'scope': 'actual common eligibility C; primitive fixtures; no THP/VFS execution'}
    (args.output / 'binding.json').write_text(json.dumps(receipt, indent=2) + '\n')
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location('codec_runner', here.parent / 'codec/run.py')
    runner = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(runner)
    os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
    log = args.output / 'huge.log'
    log.write_text(json.dumps(receipt) + '\n')
    runner.run(['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
                '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
                '-fno-pie', '-no-pie', str(args.output / 'test.c'), '-o',
                str(args.output / 'huge-test')], here, log)
    runner.run([str(args.output / 'huge-test')], here, log)
    print(log.read_text())


if __name__ == '__main__':
    main()
