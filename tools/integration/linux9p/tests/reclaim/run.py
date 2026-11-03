#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Exercise production READDIR decoder and matching transient FORGET consumer."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    parser.add_argument('--fixture', type=Path)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    fixture_path = args.fixture or here / 'host-result.json'
    fixture = json.loads(fixture_path.read_text())
    source, output = args.source.resolve(strict=True), args.output.absolute()
    output.mkdir(parents=True, exist_ok=False)
    session = (source / 'fs/9p/hfs_session.c').read_text()
    header = (source / 'fs/9p/hfs.h').read_text()
    cap = header[header.index('struct hfs_cap {'):header.index('\nstruct hfs_cred {')]
    spans = [
        ('static bool hfs_result_needs_adoption(', '\nstatic bool hfs_operation('),
        ('static int hfs_field_u64(', '\nint hfs_uuid('),
        ('int hfs_uuid(', '\nvoid hfs_cred_write('),
        ('static int hfs_copy_token(', '\nint hfs_result_code('),
        ('int hfs_result_code(', '\nstatic int hfs_result_validate('),
        ('void hfs_cap_free(', '\nint hfs_cap_parse('),
        ('int hfs_cap_parse(', '\nint hfs_reply_cap('),
        ('static int hfs_result_validate(', '\nstatic void hfs_request_result_put('),
        ('int hfs_args_init(', '\nvoid hfs_args_object('),
    ]
    actual = [cap]
    for start, end in spans:
        actual.append(session[session.index(start):session.index(end, session.index(start))])
    (output / 'result.inc').write_text('\n'.join(actual))
    lines = ['/* Actual temporary service/journal READDIR result. */']
    for key in ('context', 'credentials'):
        lines.append(f'static const char fixture_{key}[] = {json.dumps(canonical(fixture[key]))};')
    lines.append('static const char fixture_result[] = ' + json.dumps(canonical(fixture['result'])) + ';')
    helper = session[session.index('int hfs_readdir_forget('):session.index('\nvoid hfs_cap_free(', session.index('int hfs_readdir_forget('))]
    (output / 'consumer.inc').write_text(helper)
    lines.append('static const char fixture_second_result[] = ' + json.dumps(canonical(fixture['next_result'])) + ';')
    inode = (source / 'fs/9p/hfs_inode.c').read_text()
    inode_parts = []
    for start, end in [('static void hfs_inode_link_forget(', '\nstruct inode *hfs_inode_get('),
                       ('static void hfs_forget_work(', '\nstatic int hfs_d_revalidate(')]:
        offset = inode.index(start)
        inode_parts.append(inode[offset:inode.index(end, offset)])
    (output / 'inode.inc').write_text('\n'.join(inode_parts))
    offset = header.index('struct hfs_forget {')
    (output / 'forget.inc').write_text(header[offset:header.index('\nstruct hfs_object_number {', offset)])

    (output / 'fixture.h').write_text('\n'.join(lines) + '\n')
    sys.dont_write_bytecode = True
    loader = importlib.util.spec_from_file_location('codec_runner', here.parent / 'codec/run.py')
    runner = importlib.util.module_from_spec(loader)
    loader.loader.exec_module(runner)
    log = output / 'reclaim.log'
    bindings = {'session_sha256': hashlib.sha256(session.encode()).hexdigest(),
                'header_sha256': hashlib.sha256(header.encode()).hexdigest(),
                'fixture_sha256': hashlib.sha256(fixture_path.read_bytes()).hexdigest(),
                'entries': len(fixture['result']['entries']),
                'inode_sha256': hashlib.sha256(inode.encode()).hexdigest(),
                'scope': 'actual service/journal host literal, production C decoder/consumer; transport/credential primitives, no VFS/runtime/publication proof'}
    (output / 'bindings.json').write_text(json.dumps(bindings, indent=2) + '\n')
    log.write_text(json.dumps(bindings) + '\n')
    os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
    (output / 'codec_compat.h').write_text((here.parent / 'codec/compat.h').read_text().replace('typedef uint64_t u64;', 'typedef unsigned long long u64;'))
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-Wno-sign-compare', '-DHFS_CODEC_USERSPACE', '-include',
               str(output / 'codec_compat.h'), '-include', str(here.parent / 'policy/compat.h'), '-I', str(source / 'fs/9p'),
               '-I', str(output), str(source / 'fs/9p/hfs_protocol.c'),
               str(source / 'fs/9p/hfs_policy.c'), str(here / 'test.c'),
               '-lcrypto', '-o', str(output / 'reclaim-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer', '-fno-pie', '-no-pie']
    runner.run(command, here, log)
    environment = dict(os.environ, ASAN_OPTIONS='detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16')
    runner.run([str(output / 'reclaim-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
