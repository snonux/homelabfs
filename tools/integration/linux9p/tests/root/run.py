#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Decode an actual captured host ROOT result using the production guest C."""
import argparse
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True)


def make_cases(fixture):
    original = fixture['result']
    cases = [('actual-host-root', original, False)]
    changes = [
        ('missing-domain', lambda r: r['capabilities'].pop()),
        ('duplicate-root', lambda r: r['capabilities'].__setitem__(1, copy.deepcopy(r['capabilities'][0]))),
        ('duplicate-domain', lambda r: r['capabilities'].__setitem__(0, copy.deepcopy(r['capabilities'][1]))),
        ('swapped-array', lambda r: r['capabilities'].reverse()),
        ('foreign-domain-object', lambda r: r['capabilities'][1].__setitem__('object_home', '00000000-0000-4000-8000-000000000099')),
        ('foreign-root-object', lambda r: r['capabilities'][0].__setitem__('object_home', '00000000-0000-4000-8000-000000000099')),
        ('foreign-domain-scope', lambda r: r['capabilities'][1]['scope'].__setitem__('session', '00000000-0000-4000-8000-000000000099')),
        ('root-rights', lambda r: r['capabilities'][0].__setitem__('rights', 1)),
        ('domain-rights', lambda r: r['capabilities'][1].__setitem__('rights', 63)),
        ('root-credentials', lambda r: r['capabilities'][0].__setitem__('credential_hash', '00' * 32)),
        ('domain-credentials', lambda r: r['capabilities'][1].__setitem__('credential_hash', '00' * 32)),
        ('root-ancestors', lambda r: r['capabilities'][0].__setitem__('ancestors_hash', '00' * 32)),
        ('domain-ancestors', lambda r: r['capabilities'][1].__setitem__('ancestors_hash', '00' * 32)),
        ('duplicate-token', lambda r: r['capabilities'][1].__setitem__('token', r['capabilities'][0]['token'])),
        ('domain-revision', lambda r: r['capabilities'][1].__setitem__('revision', 99)),
        ('root-revision', lambda r: r['capabilities'][0].__setitem__('revision', 99)),
        ('domain-description', lambda r: r['capabilities'][1].__setitem__('description', '00000000-0000-4000-8000-000000000099')),
    ]
    for name, change in changes:
        value = copy.deepcopy(original)
        change(value)
        del value['id']
        # Force the actual decoder to inspect capability semantics, not stop at
        # the outer wrapper digest when a validly encoded field is changed.
        value['id'] = hashlib.sha256(canonical(value).encode()).hexdigest()
        cases.append((name, value, True))
    return cases


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
        ('static int hfs_field_u64(', '\nint hfs_uuid('),
        ('int hfs_uuid(', '\nvoid hfs_cred_write('),
        ('static int hfs_copy_token(', '\nint hfs_result_code('),
        ('int hfs_result_code(', '\nstatic int hfs_result_validate('),
        ('void hfs_cap_free(', '\nint hfs_cap_parse('),
        ('int hfs_cap_parse(', '\nint hfs_reply_cap('),
        ('static int hfs_result_validate(', '\nstatic void hfs_request_result_put('),
    ]
    actual = [cap]
    for start, end in spans:
        actual.append(session[session.index(start):session.index(end, session.index(start))])
    (output / 'result.inc').write_text('\n'.join(actual))
    lines = ['/* Actual host literal and wrapper-rehashed negative controls. */']
    for key in ('context', 'credentials'):
        lines.append(f'static const char fixture_{key}[] = {json.dumps(canonical(fixture[key]))};')
    lines.append('static const struct { const char *name, *body; bool reject; } cases[] = {')
    for name, value, reject in make_cases(fixture):
        lines.append('{' + json.dumps(name) + ',' + json.dumps(canonical(value)) +
                     ',' + ('true' if reject else 'false') + '},')
    lines.append('};')
    (output / 'fixture.h').write_text('\n'.join(lines) + '\n')
    sys.dont_write_bytecode = True
    loader = importlib.util.spec_from_file_location('codec_runner', here.parent / 'codec/run.py')
    runner = importlib.util.module_from_spec(loader)
    loader.loader.exec_module(runner)
    log = output / 'root.log'
    bindings = {'session_sha256': hashlib.sha256(session.encode()).hexdigest(),
                'header_sha256': hashlib.sha256(header.encode()).hexdigest(),
                'fixture_sha256': hashlib.sha256(fixture_path.read_bytes()).hexdigest(),
                'cases': len(make_cases(fixture)),
                'scope': 'production formatter capture and guest decoder; no VFS/runtime/publication proof'}
    (output / 'bindings.json').write_text(json.dumps(bindings, indent=2) + '\n')
    log.write_text(json.dumps(bindings) + '\n')
    os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-Wno-sign-compare', '-DHFS_CODEC_USERSPACE', '-include',
               str(here.parent / 'policy/compat.h'), '-I', str(source / 'fs/9p'),
               '-I', str(output), str(source / 'fs/9p/hfs_protocol.c'),
               str(source / 'fs/9p/hfs_policy.c'), str(here / 'test.c'),
               '-lcrypto', '-o', str(output / 'root-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer', '-fno-pie', '-no-pie']
    runner.run(command, here, log)
    environment = dict(os.environ, ASAN_OPTIONS='detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16')
    runner.run([str(output / 'root-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
