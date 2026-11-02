#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Test actual production policy helpers with the production wire parser.

This verifies numeric credential-set capture and metadata validation. It does
not simulate Linux current_cred, user namespaces, LSM hooks, inode locking,
create syscalls or real server enforcement of creator/parent ownership policy.
"""
import argparse
import copy
import importlib.util
import json
import os
import sys
from pathlib import Path


def cases(target):
    base = {
        'atime': {'nanos': 999999999, 'seconds': -(1 << 63)},
        'ctime': {'nanos': 1, 'seconds': (1 << 63) - 1},
        'gid': 53000, 'kind': 'REGULAR', 'mode': 0o7777,
        'mtime': {'nanos': 0, 'seconds': 0}, 'nlink': 1,
        'object_allocation': '00000000-0000-0000-0000-000000000002',
        'object_home': '00000000-0000-0000-0000-000000000001',
        'revision': 1, 'size': 0, 'uid': 12001,
    }
    vector = []

    def add(name, body, valid=False, type_name='S_IFREG'):
        vector.append((name, body, valid, type_name))

    add('authoritative-full07777-asymmetric-owner', base, True)
    for kind, mode in [('DIRECTORY', 0o7777), ('SYMLINK', 0o777)]:
        body = dict(base, kind=kind, mode=mode)
        add(kind.lower(), body, True, {'DIRECTORY': 'S_IFDIR', 'SYMLINK': 'S_IFLNK'}[kind])
    add('maximum-metadata', dict(base, size=(1 << 63) - 1, revision=(1 << 64) - 1, nlink=(1 << 32) - 1), True)
    for key in base:
        body = dict(base)
        del body[key]
        add('missing-' + key, body)
    add('unknown-field', dict(base, unknown=0))
    for field, values in {
        'uid': [(1 << 32) - 1, 1 << 32, -1, True, None],
        'gid': [(1 << 32) - 1, 1 << 32, -1, False, None],
        'mode': [0o10000, 0o100777, -1, '07777', None],
        'size': [1 << 63, -1, None], 'nlink': [1 << 32, -1, None],
        'revision': [0, -1, None], 'kind': ['regular', 'BLOCK', None],
        'object_home': ['00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000000', None],
        'object_allocation': ['00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-00000000000A', None],
    }.items():
        for i, value in enumerate(values):
            add(f'invalid-{field}-{i}', dict(base, **{field: value}))
    add('symlink-invalid-mode', dict(base, kind='SYMLINK', mode=0o7777))
    for clock in ['atime', 'mtime', 'ctime']:
        for variant in [{'nanos': 1000000000, 'seconds': 0}, {'nanos': -1, 'seconds': 0},
                        {'nanos': 0}, {'seconds': 0}, {'nanos': 0, 'seconds': 1 << 63},
                        {'nanos': 0, 'seconds': 0, 'extra': 0}, None]:
            body = copy.deepcopy(base)
            body[clock] = variant
            add('invalid-' + clock, body)
    lines = ['/* Generated literal policy input vectors. */',
             'struct attr_case { const char *name; const char *body; bool valid; u32 mode; u16 type; u64 size; u64 nlink; u64 revision; };',
             'static const struct attr_case attribute_cases[] = {']
    for name, body, valid, type_name in vector:
        encoded = json.dumps(body, sort_keys=True, separators=(',', ':'))
        lines.append('{' + ','.join([json.dumps(name), json.dumps(encoded), 'true' if valid else 'false', str(body.get('mode', 0)) if valid else '0', type_name, str(body['size']) + 'ULL' if valid else '0', str(body['nlink']) + 'ULL' if valid else '0', str(body['revision']) + 'ULL' if valid else '0']) + '},')
    lines.extend(['};', f'#define POLICY_ATTR_COUNT {len(vector)}U'])
    target.write_text('\n'.join(lines) + '\n')
    return len(vector)


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
    count = cases(output / 'policycases.h')
    log = output / 'policy.log'
    log.write_text(f'literal_metadata_cases={count}\n')
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror', '-Wno-sign-compare',
               '-DHFS_CODEC_USERSPACE', '-include', str(here / 'compat.h'),
               '-I', str(source / 'fs/9p'), '-I', str(output),
               str(source / 'fs/9p/hfs_protocol.c'), str(source / 'fs/9p/hfs_policy.c'),
               str(here / 'test.c'), '-lcrypto', '-o', str(output / 'policy-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    runner.run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16'
    runner.run([str(output / 'policy-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
