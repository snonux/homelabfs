#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Run actual v9fs_hfs_mount from its cache= check through the HELLO selection.

The production mount and its helpers, the enrollment validators, the
controlled HELLO pre-send check, hfs_session_block, hfs_show_options and
p9_show_client_options are copied verbatim from the owned source tree. Only kernel/VFS, p9 client
creation, transport binding and the native exchange are substituted. The
substituted exchange echoes the sent HELLO as the runtime does
(internal/guestbridge/service.go). No kernel, VM or service is used.

Since m64 it also copies hfs_go_bounds (the signed enrollment Bounds check),
v9fs_hfs_kill_super, hfs_session_fence, hfs_controlled_stop and
hfs_controlled_retain verbatim. The Bounds cases use the daemon's own
Sources floor constants from internal/viewcontrol/source_floor.go and the
real Bounds of the zq3 spec and the controlledruntime guest artifacts.
"""
import argparse
import hashlib
import importlib.util
import json
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


def go_source_floor(repo):
    """The daemon's Sources floor constants, read from viewcontrol (m64).

    The kernel test compares hfs_go_bounds against these, not against a copy
    of the kernel macro, so a change on either side fails the harness.
    """
    text = (repo / 'internal/viewcontrol/source_floor.go').read_text()
    if not re.search(r'return baselineSources \+ 2\*domains \+ admittedSources \+ sourceReserve\n',
                     text):
        raise ValueError('viewcontrol.MinSources formula changed; update the m64 oracle')
    names = ('baselineSources', 'admittedSources', 'sourceReserve', 'MaxSources')
    return {name: int(re.search(r'^\t' + name + r' = (\d+)$', text, re.M).group(1))
            for name in names}


def bounds_fixtures(repo, output):
    """Real Bounds the daemon accepts: the zq3 spec Views (two sessions) and the
    regenerated controlledruntime guest artifacts (3-domain groups)."""
    floor = go_source_floor(repo)
    spec = json.loads((repo / 'internal/runtimeprep/testdata/zq3-spec.json').read_text())
    artifacts = json.loads(
        (repo / 'internal/controlledruntime/testdata/guest-artifacts.json').read_text())
    sessions = sum(1 for arg in spec['Exporter']['Args'] if 'hfs-controller={{session:' in arg)
    groups = [json.loads(bytes.fromhex(group['enrollment_bytes']))
              for group in artifacts['provision']['payload']['groups']]

    def literal(bounds):
        text = json.dumps(bounds, separators=(',', ':'))
        return '"' + text.replace('\\', '\\\\').replace('"', '\\"') + '"'

    rows = [('zq3-spec', sessions, spec['Views'])]
    rows += [(f'guest-artifacts-{i}', len(group['Domains']), group['Bounds'])
             for i, group in enumerate(groups)]
    lines = [f'#define GO_{name.upper()} {value}' for name, value in floor.items()]
    lines.append('static const struct { const char *name; u32 domains; const char *bounds; } '
                 'real_bounds[] = {')
    lines += [f'\t{{ "{name}", {domains}, {literal(bounds)} }},' for name, domains, bounds in rows]
    lines.append('};')
    (output / 'bounds_fixtures.inc').write_text('\n'.join(lines) + '\n')


def extract(source, output):
    """Write verbatim production definitions and return their source hashes."""
    here = Path(__file__).resolve().parent
    build = load('controlled_received_build', here.parent / 'controlled_received/build.py')
    texts = {name: (source / name).read_text()
             for name in ('fs/9p/hfs.h', 'fs/9p/hfs_session.c', 'fs/9p/hfs_controlled.c',
                          'fs/9p/hfs_inode.c', 'fs/9p/hfs_enrollment.c', 'net/9p/client.c')}
    session = texts['fs/9p/hfs_session.c']
    (output / 'enrollment_struct.inc').write_text(enrollment_struct(texts['fs/9p/hfs.h']))
    (output / 'validate.inc').write_text(''.join(build.function(session, name) for name in (
        'hfs_uuid', 'hfs_enrollment_validate', 'hfs_enrollment_lanes')))
    (output / 'before_send.inc').write_text(
        build.function(texts['fs/9p/hfs_controlled.c'], 'hfs_controlled_before_send'))
    # The complete mount with every helper it calls (g64 split it), not only
    # its failure tail (compare controlled_received).
    (output / 'mount.inc').write_text(''.join(build.function(session, name) for name in (
        'hfs_session_block', 'hfs_session_fence', 'hfs_cache_option', 'hfs_session_new', 'hfs_mount_enroll', 'hfs_trace_register',
        'hfs_mount_client', 'hfs_mount_hello', 'hfs_mount_fail', 'v9fs_hfs_mount')))
    # g64: the mountinfo super options of an hfs superblock.
    client = texts['net/9p/client.c']
    (output / 'show_options.inc').write_text(
        build.function(client, 'p9_client_is_hfs') +
        build.function(client, 'p9_show_client_options') +
        build.function(texts['fs/9p/hfs_inode.c'], 'hfs_show_options'))
    # m64: the enrollment Bounds check with its Sources floor macros, and the
    # unmount path: kill_sb with the real controlled stop and retain fences.
    enrollment = texts['fs/9p/hfs_enrollment.c']
    macros = re.findall(r'^#define HFS_GO_SOURCES_\w+.*$', enrollment, re.M)
    (output / 'bounds.inc').write_text('\n'.join(macros) + '\n' +
                                       build.function(enrollment, 'hfs_go_bounds'))
    (output / 'stop.inc').write_text(''.join(
        build.function(texts['fs/9p/hfs_controlled.c'], name)
        for name in ('hfs_controlled_stop', 'hfs_controlled_retain')))
    (output / 'kill_sb.inc').write_text(build.function(session, 'v9fs_hfs_kill_super'))
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
    bounds_fixtures(here.parents[4], output)
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
