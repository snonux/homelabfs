#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile the actual owned kernel codec against a userspace allocation shim.

No kernel, VM or production service is installed or exercised. The generated
corpus header is derived from the committed protocol fixtures, never a second
codec implementation. Schema/session state is tested by its separate owner.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import subprocess


def byte_array(value):
    return '{' + ','.join(str(v) for v in value) + '}'


def corpus(fixtures, target):
    doc = json.loads(fixtures.read_text())
    out = ['/* Generated from committed cases.json; do not edit. */',
           'struct golden { const u8 *body; u32 body_len; u64 cell; const u8 **frames; const u32 *sizes; u32 nframes; };',
           'struct raw_case { const u8 *bytes; u32 len; };',
           'struct bad_frame { u32 offset; u8 byte; bool hash_only; };']
    for index, case in enumerate(doc['golden']):
        body = json.dumps(case['body'], ensure_ascii=True, sort_keys=True, separators=(',', ':')).encode('ascii')
        out.append(f'static const u8 body{index}[] = {byte_array(body)};')
        frames = [bytes.fromhex(v) for v in case['frame_hex']]
        for part, frame in enumerate(frames):
            out.append(f'static const u8 frame{index}_{part}[] = {byte_array(frame)};')
        out.append(f'static const u8 *frames{index}[] = {{' + ','.join(f'frame{index}_{p}' for p in range(len(frames))) + '};')
        out.append(f'static const u32 sizes{index}[] = {{' + ','.join(str(len(v)) for v in frames) + '};')
    out.append('static const struct golden corpus_golden[] = {')
    for index, case in enumerate(doc['golden']):
        out.append(f'{{body{index},sizeof(body{index}),{case["body"]["cell"]},frames{index},sizes{index},{len(case["frame_hex"])}}},')
    out.append('};')
    for index, case in enumerate(doc['raw_malformed']):
        out.append(f'static const u8 raw{index}[] = {byte_array(bytes.fromhex(case["hex"]))};')
    out.append('static const struct raw_case corpus_raw[] = {' + ','.join(f'{{raw{i},sizeof(raw{i})}}' for i in range(len(doc['raw_malformed']))) + '};')
    out.append('static const struct bad_frame corpus_frames[] = {')
    for case in doc['frame_malformed']:
        out.append(f'{{{case["offset"]},{case["byte"]},{"true" if case["error"] == "body-hash" else "false"}}},')
    out += ['};', f'#define CORPUS_GOLDEN_COUNT {len(doc["golden"])}U',
            f'#define CORPUS_RAW_COUNT {len(doc["raw_malformed"])}U',
            f'#define CORPUS_FRAME_COUNT {len(doc["frame_malformed"])}U']
    target.write_text('\n'.join(out) + '\n')


def limits():
    # ASan reserves virtual address space. RSS limits are advisory on Linux;
    # record and check actual child peak RSS after each bounded command.
    resource.setrlimit(resource.RLIMIT_CPU, (60, 60))
    resource.setrlimit(resource.RLIMIT_RSS, (512 * 1024 * 1024,) * 2)
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def run(argv, cwd, log, env=None):
    with log.open('a') as output:
        output.write('argv=' + json.dumps([str(a) for a in argv]) + '\n')
        output.flush()
        result = subprocess.run(argv, cwd=cwd, stdout=output, stderr=subprocess.STDOUT,
                                timeout=60, preexec_fn=limits, env=env)
        usage = resource.getrusage(resource.RUSAGE_CHILDREN)
        output.write(f'exit={result.returncode} peak_child_rss_kib={usage.ru_maxrss}\n')
        if usage.ru_maxrss > 512 * 1024:
            raise SystemExit(f'RSS budget exceeded: see {log}')
    if result.returncode:
        raise SystemExit(f'FAIL: see {log}')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    parser.add_argument('--fixtures', type=Path)
    args = parser.parse_args()
    source = args.source.resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    here = Path(__file__).resolve().parent
    repo = here.parents[4]
    fixtures = (args.fixtures or repo / 'docs/spec/retained9p-fixtures/cases.json').resolve()
    fixture_hash = hashlib.sha256(fixtures.read_bytes()).hexdigest()
    corpus(fixtures, output / 'corpus.h')
    log = output / 'codec.log'
    log.write_text(f'fixture_path={fixtures}\nfixture_sha256={fixture_hash}\n')
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror', '-Wno-sign-compare',
               '-DHFS_CODEC_USERSPACE', '-include', str(here / 'compat.h'),
               '-I', str(source / 'fs/9p'), '-I', str(output),
               str(source / 'fs/9p/hfs_protocol.c'), str(here / 'test.c'),
               '-lcrypto', '-o', str(output / 'codec-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16'
    run([str(output / 'codec-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
