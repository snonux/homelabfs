#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile actual physical-admission C with only kernel primitive shims."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    parser.add_argument('--pressure', action='store_true')
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    names = ('fs/9p/hfs_view.h', 'fs/9p/hfs_view.c',
             'fs/9p/hfs_protocol.h', 'fs/9p/hfs_protocol.c', 'fs/9p/hfs_lease.h',
             'fs/9p/hfs.h', 'mm/page-writeback.c', 'mm/folio-compat.c')
    if args.pressure:
        names += ('fs/9p/hfs_mmap.c', 'fs/9p/hfs_lease.c', 'fs/9p/hfs_session.c')
    hashes = {}
    inputs = {}
    for name in names:
        data = (args.source / name).read_bytes()
        hashes[name] = hashlib.sha256(data).hexdigest()
        inputs[name] = data.decode()
    source = ['#include "hfs_protocol.h"']
    declarations = inputs['fs/9p/hfs.h']
    first = declarations.index('struct hfs_request {')
    last = declarations.index('\n};', first) + len('\n};')
    source.append('\n'.join(line for line in inputs['fs/9p/hfs_lease.h'].splitlines()
                            if not line.startswith('#include')))
    for name in names[:2]:
        if name.endswith('hfs_view.c'):
            source.append(declarations[first:last])
        source.append('\n'.join(line for line in inputs[name].splitlines()
                                if not line.startswith('#include')))
    # Actual Linux dirty marking/dispatch, with only page-bit/mapping primitives.
    for filename, first, last in (
            ('mm/page-writeback.c', 'bool noop_dirty_folio(', '\nEXPORT_SYMBOL(noop_dirty_folio);'),
            ('mm/page-writeback.c', 'bool folio_mark_dirty(', '\nEXPORT_SYMBOL(folio_mark_dirty);'),
            ('mm/folio-compat.c', 'bool set_page_dirty(', '\nEXPORT_SYMBOL(set_page_dirty);')):
        start = inputs[filename].index(first)
        source.append(inputs[filename][start:inputs[filename].index(last, start)])
    if args.pressure:
        for filename, first, last in (
                ('fs/9p/hfs_mmap.c', 'static int hfs_mmap_invalidate_stopped(', '\n/* Independent poll,'),
                ('fs/9p/hfs_mmap.c', 'static int hfs_mmap_pressure(', '\nstatic void hfs_mmap_pressure_work('),
                ('fs/9p/hfs_mmap.c', 'static vm_fault_t hfs_mmap_fault_error(', '\nstatic vm_fault_t hfs_mmap_fault('),
                ('fs/9p/hfs_mmap.c', 'static vm_fault_t hfs_mmap_mkwrite(', '\nstatic const struct vm_operations_struct'),
                ('fs/9p/hfs_session.c', 'int hfs_result_code(', '\nstatic int hfs_result_validate('),
                ('fs/9p/hfs_lease.c', 'static int hfs_copy_json(', '\nstatic bool hfs_token_equal('),
                ('fs/9p/hfs_lease.c', 'static int hfs_u64_field(', '\nstatic int hfs_normal_target('),
                ('fs/9p/hfs_lease.c', 'int hfs_dirty_drain(', None)):
            start = inputs[filename].index(first)
            source.append(inputs[filename][start:inputs[filename].index(last, start)] if last else inputs[filename][start:])
    (args.output / 'production.c').write_text('\n'.join(source) + '\n')
    (args.output / 'test.c').write_bytes(b'#include \"compat.h\"\n' + ((here.parent / 'pressure' if args.pressure else here) / 'test.c').read_bytes())
    (args.output / 'compat.h').write_bytes((here / 'compat.h').read_bytes() + ((here.parent / 'pressure/compat_extra.h').read_bytes() if args.pressure else b''))
    (args.output / 'hfs_codec_compat.h').write_bytes((here.parent / 'codec/compat.h').read_bytes())
    command = ['gcc', '-std=gnu11', '-Wall', '-Wextra', '-Werror', '-g', '-O1',
               '-pthread', '-Wno-unused-parameter', '-Wno-sign-compare', '-DHFS_CODEC_USERSPACE', '-I', str(args.source / 'fs/9p'),
               '-include', str(args.output / 'hfs_codec_compat.h'),
               str(args.source / 'fs/9p/hfs_protocol.c'),
               str(args.output / 'test.c'), '-lcrypto', '-o', str(args.output / 'view-test')]
    if args.sanitize:
        command += ['-fsanitize=address,undefined', '-fno-omit-frame-pointer',
                    '-fno-pie', '-no-pie']
    # ASan reserves a large virtual shadow; RSS and stage limits are external.
    resource.setrlimit(resource.RLIMIT_CPU, (120, 120))
    result = {'source': hashes, 'commands': [], 'scope':
              'actual production C; kernel primitives mocked; no VFS/PTE execution'}
    cpus = sorted(os.sched_getaffinity(0))[:2]
    os.sched_setaffinity(0, cpus)
    result['affinity'] = cpus
    for index, cmd in enumerate((command, [str(args.output / 'view-test')])):
        started = time.monotonic()
        peak = 0
        max_gap = 0
        previous = started
        reason = None
        with (args.output / f'stage{index}.log').open('wb') as log:
            process = subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT,
                                       start_new_session=True)
            while process.poll() is None:
                now = time.monotonic()
                max_gap = max(max_gap, now - previous)
                previous = now
                rss = 0
                for directory in Path('/proc').iterdir():
                    if not directory.name.isdecimal():
                        continue
                    try:
                        stat = (directory / 'stat').read_text().rsplit(')', 1)[1].split()
                        if int(stat[2]) == process.pid:
                            rss += int(stat[21]) * os.sysconf('SC_PAGE_SIZE')
                    except (OSError, ValueError, IndexError):
                        continue
                peak = max(peak, rss)
                if rss > 1024 ** 3:
                    reason = 'sampled process-group RSS exceeds 1GiB'
                if now - started > 300:
                    reason = 'stage exceeds 5min'
                if reason:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                    break
                time.sleep(0.02)
            code = process.wait()
        record = {'argv': cmd, 'exit': code, 'limit_reason': reason,
                  'seconds': time.monotonic() - started,
                  'peak_group_rss_bytes': peak,
                  'max_sample_gap_seconds': max(max_gap, time.monotonic() - previous)}
        result['commands'].append(record)
        (args.output / 'receipt.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(record), flush=True)
        print((args.output / f'stage{index}.log').read_text(), end='')
        if code or reason:
            raise SystemExit(code or 1)



if __name__ == '__main__':
    main()
