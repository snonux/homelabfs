#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile actual physical-admission C with only kernel primitive shims."""
import argparse
import hashlib
import json
import os
import re
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
        names += ('fs/9p/hfs_mmap.c', 'fs/9p/hfs_lease.c', 'fs/9p/hfs_session.c',
                  'fs/9p/hfs_inode.c')
    hashes = {}
    inputs = {}
    for name in names:
        data = (args.source / name).read_bytes()
        hashes[name] = hashlib.sha256(data).hexdigest()
        inputs[name] = data.decode()
    source = ['#include "hfs_protocol.h"']
    if args.pressure:
        # hfs_lease.c's hfs_copy_json uses the received-copy helper; without
        # CONFIG_9P_FS_HOMELABFS_CONTROLLED the header supplies its plain stub.
        source.append('#include "hfs_received.h"')
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
                ('fs/9p/hfs_mmap.c', 'static bool hfs_mmap_fault_final(', '\nstatic vm_fault_t hfs_mmap_fault('),
                ('fs/9p/hfs_mmap.c', 'static vm_fault_t hfs_mmap_mkwrite(', '\nstatic const struct vm_operations_struct'),
                ('fs/9p/hfs_session.c', 'int hfs_result_code(', '\nstatic int hfs_result_validate('),
                # 2f4: hfs_token_equal too, for hfs_view_exchange.
                ('fs/9p/hfs_lease.c', 'static int hfs_copy_json(', '\nstatic int hfs_hex32('),
                ('fs/9p/hfs_lease.c', 'static int hfs_u64_field(', '\nstatic int hfs_normal_target('),
                ('fs/9p/hfs_lease.c', 'int hfs_dirty_drain(', None)):
            start = inputs[filename].index(first)
            source.append(inputs[filename][start:inputs[filename].index(last, start)] if last else inputs[filename][start:])
        # 1a4: hfs_mmap_fail's once-per-session line, with its control
        # struct, its once bit and the event executor's record helpers.
        mmap = inputs['fs/9p/hfs_mmap.c']
        bit = re.search(r'^#define HFS_REPORTED_VIEW_STOP .*$', inputs['fs/9p/hfs.h'], re.M)
        source.append(bit.group(0))
        start = mmap.index('struct hfs_mmap_control {')
        source.append(mmap[start:mmap.index('\n};\n', start) + 4])
        for first, last in (('static void hfs_mmap_word(', '\nstatic int hfs_mmap_ack('),
                            ('struct hfs_mmap_where {', '\nstatic void hfs_mmap_drains(')):
            start = mmap.index(first)
            source.append(mmap[start:mmap.index(last, start)])
        # ie4: hfs_mmap_begin with its re-check retry, hfs_mmap_end and the
        # cleanup packet they queue (the worker itself is a fixture).
        start = mmap.index('struct hfs_cache_cleanup {')
        source.append(mmap[start:mmap.index('\nstatic void hfs_mmap_cleanup(', start)])
        source.append(mmap[mmap.index('static void hfs_mmap_recheck_note('):])
        # 1a4 review: the committed-result timeout line of hfs_attr_install,
        # and since ie4 hfs_attr_restamp and hfs_reissue, which sit between
        # it and hfs_attr_install, with their internal error code.
        moved = re.search(r'^#define HFS_ETARGET_MOVED .*$', inputs['fs/9p/hfs.h'], re.M)
        source.append(moved.group(0))
        inode = inputs['fs/9p/hfs_inode.c']
        start = inode.index('static void hfs_attr_timeout_warn(')
        source.append(inode[start:inode.index('\nint hfs_attr_install(', start)])
        # ie4 final review: a moved LOOKUP's forget record (hfs_inode_get_fail)
        # and the eviction that sends its FORGET.
        hfs_h = inputs['fs/9p/hfs.h']
        start = hfs_h.index('struct hfs_forget {')
        source.append(hfs_h[start:hfs_h.index('\n};', start) + 3])
        for first, last in (('struct hfs_inode_seed {', '\nstatic int hfs_test_inode('),
                            ('static void hfs_inode_link_forget(', '\nstruct inode *hfs_inode_get('),
                            ('static void hfs_evict_inode(', '\nstatic int hfs_d_revalidate(')):
            start = inode.index(first)
            source.append(inode[start:inode.index(last, start)])
        # 2f4 (zq3-r17 R17-1): the actual hfs_view_exchange with its
        # lease-grant settle helpers. The transport, the ticket reply check,
        # the session block/poll halt, the mount fence and the 0005 source
        # receipt are fixtures (pressure/test.c); the receipt's uncontrolled
        # header stub is redirected to the fixture.
        lease = inputs['fs/9p/hfs_lease.c']
        start = lease.index('struct hfs_grant_settle {')
        source.append('#define hfs_received_source_ex test_received_source_ex')
        source.append(lease[start:lease.index('\nstatic int hfs_lease_decode(', start)])
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
