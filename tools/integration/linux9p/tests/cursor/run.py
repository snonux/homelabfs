#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Execute actual allocation-free kernel cursor helpers in userspace.

This does not exercise Linux dir_emit, getdents, llseek, inode lifetimes, provider
credits, a mounted filesystem, or a remote directory service.
"""
import argparse
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
    source, output = args.source.resolve(), args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    here = Path(__file__).resolve().parent
    sys.dont_write_bytecode = True
    loader = importlib.util.spec_from_file_location('codec_runner', here.parent / 'codec/run.py')
    runner = importlib.util.module_from_spec(loader)
    loader.loader.exec_module(runner)
    log = output / 'cursor.log'
    log.write_text('')
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror', '-Wno-sign-compare',
               '-DHFS_CODEC_USERSPACE', '-include', str(here.parent / 'codec/compat.h'),
               '-I', str(source / 'fs/9p'), str(source / 'fs/9p/hfs_cursor.c'),
               str(here / 'test.c'), '-lcrypto', '-o', str(output / 'cursor-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    runner.run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16'
    runner.run([str(output / 'cursor-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
