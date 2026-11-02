#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Run actual hfs_transport at a deterministic p9/mutex/clock boundary."""
import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    source, output = args.source.resolve(strict=True), args.output.absolute()
    output.mkdir(parents=True, exist_ok=False)
    sys.dont_write_bytecode = True
    loader = importlib.util.spec_from_file_location('codec_runner', here.parent / 'codec/run.py')
    runner = importlib.util.module_from_spec(loader)
    loader.loader.exec_module(runner)
    session = (source / 'fs/9p/hfs_session.c').read_text()
    # Compile the complete production function verbatim, never a modeled loop.
    transport = session[session.index('static int hfs_transport('):session.index('\nstatic bool hfs_operation(')]
    (output / 'transport.inc').write_text(transport)
    log = output / 'transport.log'
    log.write_text('hfs_session_sha256=' + hashlib.sha256(session.encode()).hexdigest() + '\n')
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-Wno-sign-compare', '-DHFS_CODEC_USERSPACE', '-include',
               str(here.parent / 'policy/compat.h'), '-I', str(source / 'fs/9p'),
               '-I', str(output), str(source / 'fs/9p/hfs_protocol.c'),
               str(source / 'fs/9p/hfs_policy.c'), str(here / 'test.c'),
               '-lcrypto', '-o', str(output / 'transport-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    runner.run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16'
    runner.run([str(output / 'transport-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
