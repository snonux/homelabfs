#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Run actual v9fs_hfs_mount from enroll through the HELLO selection check.

The production mount, enrollment validators and controlled HELLO pre-send
check are copied verbatim from the owned source tree. Only kernel/VFS, p9
client creation, transport binding and the native exchange are substituted.
The substituted exchange echoes the sent HELLO as the runtime does
(internal/guestbridge/service.go). No kernel, VM or service is used.
"""
import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import sys


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def enrollment_struct(header):
    """Copy the real enrollment layout so the validators see actual fields."""
    limit = re.search(r'^#define HFS_CONTEXT_MAX .*$', header, re.M).group(0)
    start = header.index('enum hfs_view_scope {')
    end = header.index('\n};\n', header.index('struct hfs_enrollment {')) + 4
    return limit + '\n' + header[start:end]


def extract(source, output):
    """Write verbatim production definitions and return their source hashes."""
    here = Path(__file__).resolve().parent
    build = load('controlled_received_build', here.parent / 'controlled_received/build.py')
    texts = {name: (source / name).read_text()
             for name in ('fs/9p/hfs.h', 'fs/9p/hfs_session.c', 'fs/9p/hfs_controlled.c')}
    session = texts['fs/9p/hfs_session.c']
    (output / 'enrollment_struct.inc').write_text(enrollment_struct(texts['fs/9p/hfs.h']))
    (output / 'validate.inc').write_text(''.join(build.function(session, name) for name in (
        'hfs_uuid', 'hfs_enrollment_validate', 'hfs_enrollment_lanes')))
    (output / 'before_send.inc').write_text(
        build.function(texts['fs/9p/hfs_controlled.c'], 'hfs_controlled_before_send'))
    # The complete mount, not only its fail: tail (compare controlled_received).
    (output / 'mount.inc').write_text(build.function(session, 'v9fs_hfs_mount'))
    return {name: hashlib.sha256(text.encode()).hexdigest() for name, text in texts.items()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    args = parser.parse_args()
    sys.dont_write_bytecode = True
    here = Path(__file__).resolve().parent
    source, output = args.source.resolve(strict=True), args.output.absolute()
    output.mkdir(parents=True, exist_ok=False)
    runner = load('codec_runner', here.parent / 'codec/run.py')
    hashes = extract(source, output)
    log = output / 'mount-hello.log'
    log.write_text(''.join(f'{name}_sha256={value}\n' for name, value in sorted(hashes.items())))
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-Wno-sign-compare', '-Wno-unused-parameter', '-DHFS_CODEC_USERSPACE',
               '-include', str(here.parent / 'policy/compat.h'),
               '-I', str(source / 'fs/9p'), '-I', str(output),
               str(source / 'fs/9p/hfs_protocol.c'), str(source / 'fs/9p/hfs_policy.c'),
               str(here / 'test.c'), '-lcrypto', '-o', str(output / 'mount-hello-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    runner.run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16'
    runner.run([str(output / 'mount-hello-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
