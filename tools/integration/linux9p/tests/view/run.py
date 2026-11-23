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


def retirement_unlock_guard(source):
    """1h4 review round 2: every holder of session->retirement (view rounds,
    hfs_retire, hfs_session_destroy) must release it through
    hfs_retirement_unlock, which counts the release and wakes a BLOCKED
    observation waiting for the mutex. The pressure harness runs that helper
    but not hfs_retire or hfs_session_destroy, so the source is checked: the
    only mutex_unlock of a ->retirement mutex in fs/9p is the helper's.
    """
    unlock = re.compile(r'mutex_unlock\(\s*&[^;]*->retirement\s*\)')
    found = []
    for path in sorted((source / 'fs/9p').rglob('*')):
        if path.suffix not in ('.c', '.h', '.inc') or not path.is_file():
            continue
        text = path.read_text(errors='replace')
        for match in unlock.finditer(text):
            start = text.rfind('\nvoid hfs_retirement_unlock(', 0, match.start())
            inside = start >= 0 and text.find('\n}\n', start) > match.start()
            found.append((path.name, inside))
    if found != [('hfs_view.c', True)]:
        raise SystemExit(f'refuse: session->retirement released outside hfs_retirement_unlock: {found}')


def stop_wake_guard(source):
    """5f4: hfs_view_reopen_wait (and hfs_view_retire_wait,
    hfs_view_enter_until) wait for the session stops blocked, closing and
    transport_failed, whose writers have no view; they wake
    session->stop_wait instead (hfs_session_stop_wake). The harness models
    the wake-up (view_wait_wakes) but compiles only some writers, so the
    source is checked: every write of true to one of those session flags in
    fs/9p is followed by hfs_session_stop_wake within four lines. Any
    pointer name matches (5f4 review 1: `s->blocked = true` too), so the
    flags' names must stay unique to struct hfs_session in fs/9p.
    """
    write = re.compile(r'WRITE_ONCE\(\s*[^;]*->(blocked|closing|transport_failed)\s*,'
                       r'\s*true\s*\)|->(blocked|closing|transport_failed)\s*=\s*true\b')
    found = []
    for path in sorted((source / 'fs/9p').rglob('*')):
        if path.suffix not in ('.c', '.h', '.inc') or not path.is_file():
            continue
        text = path.read_text(errors='replace')
        for match in write.finditer(text):
            after = text[match.end():].split('\n', 5)[:5]
            if not any('hfs_session_stop_wake(' in line for line in after):
                found.append((path.name, text.count('\n', 0, match.start()) + 1))
    if found:
        raise SystemExit(f'refuse: session stop set without hfs_session_stop_wake: {found}')


def function_body(text, signature):
    """The text of the function that starts with signature, up to its end."""
    start = text.index(signature)
    return text[start:text.index('\n}\n', start)]


def mmap_install_guard(text):
    """wj4: in hfs_mmap_file, the mark's result is tested by the `if (!err) {`
    right after it, and hfs_mmap_ops is installed once, inside that block."""
    body = function_body(text, '\nint hfs_mmap_file(')
    install = 'vma->vm_ops = &hfs_mmap_ops;'
    mark = re.search(r'\n\terr = [^;]*hfs_view_mmap_inode\(inode\);\n\tif \(!err\) \{\n', body)
    if (not mark or 'hfs_view_register_inode' in text or
            body.count('hfs_view_mmap_inode(') != 1):
        raise SystemExit('refuse: hfs_mmap_file must mark the inode with hfs_view_mmap_inode, '
                         'then test its result, before installing hfs_mmap_ops')
    block = body[mark.end():body.index('\n\t}\n', mark.end())]
    if body.count(install) != 1 or install not in block:
        raise SystemExit('refuse: hfs_mmap_ops must be installed only after a successful mark')


def mmap_route_guard(files):
    """wj4: hfs_file_fops' .mmap is hfs_mmap, which sends a bridge session to
    hfs_mmap_file (the provider's own mmap serves only non-bridge sessions)."""
    route = function_body(files, '\nstatic int hfs_mmap(struct file *file, struct vm_area_struct *vma)')
    fops = files[files.index('const struct file_operations hfs_file_fops = {'):]
    fops = fops[:fops.index('\n};')]
    if ('return session->bridge_minor ? hfs_mmap_file(file, vma) :' not in route or
            not re.search(r'\n\t\.mmap = hfs_mmap,\n', fops)):
        raise SystemExit('refuse: hfs_file_fops must route a bridge session mmap to hfs_mmap_file')


def mmap_mark_guard(source):
    """tj4: hfs_mmap_file is not compiled by this harness, so the source is
    checked: it marks the inode mmapped (hfs_view_mmap_inode, which the view
    and pressure cases exercise) before it installs hfs_mmap_ops, and
    hfs_mmap.c never registers through the unmarked hfs_view_register_inode.
    Otherwise a mapped inode would leave the view cacheless and xf4 advance
    could adopt a new target under its page cache.

    wj4 (tj4 review): run with and without --pressure, and also require
    - hfs_mmap_ops installed only inside the `if (!err) {` block that
      directly follows the mark's result (a failed mark installs nothing);
    - exactly one `vm_ops = &hfs_mmap_ops` in fs/9p (that one), with
      hfs_mmap_ops static to hfs_mmap.c and `&hfs_mmap_ops` used nowhere
      else there (kn1 review 1: no indirect install through a variable);
    - hfs_file_fops' .mmap is hfs_mmap, which routes bridge sessions to
      hfs_mmap_file.
    """
    text = (source / 'fs/9p/hfs_mmap.c').read_text()
    mmap_install_guard(text)
    installs = []
    for path in sorted((source / 'fs/9p').rglob('*')):
        if path.suffix in ('.c', '.h', '.inc') and path.is_file():
            installs += [path.name] * len(re.findall(
                r'vm_ops\s*=\s*&\s*hfs_mmap_ops\b', path.read_text(errors='replace')))
    # kn1 review 1: hfs_mmap_ops is referenced once in hfs_mmap.c (that
    # install), so an indirect install through a variable is refused too.
    if installs != ['hfs_mmap.c'] or len(re.findall(r'&\s*hfs_mmap_ops\b', text)) != 1 or \
            'static const struct vm_operations_struct hfs_mmap_ops = {' not in text:
        raise SystemExit(f'refuse: hfs_mmap_ops installed outside hfs_mmap_file: {installs}')
    mmap_route_guard((source / 'fs/9p/hfs_file.c').read_text())


def struct_fields(text, name):
    """The member names of `struct name { ... };` in text, in order, with
    comments dropped (one name per declaration, as both copies write them)."""
    start = text.index(f'struct {name} {{')
    body = text[text.index('{', start) + 1:text.index('};', start)]
    body = re.sub(r'/\*.*?\*/', '', body, flags=re.S)
    return [re.search(r'(\w+)\s*(\[[^]]*\])?\s*$', decl).group(1)
            for decl in body.split(';') if decl.strip()]


def hfs_inode_layout_guard(source, compat):
    """wj4 (tj4 review): compat.h hand-copies struct hfs_inode with
    userspace member types; its members must stay those of fs/9p/hfs.h, by
    name and in order, so a new or moved member (as view_mmapped was in tj4)
    fails here instead of leaving the harness on a stale layout."""
    actual = struct_fields((source / 'fs/9p/hfs.h').read_text(), 'hfs_inode')
    copied = struct_fields(compat.read_text(), 'hfs_inode')
    if actual != copied:
        raise SystemExit(f'refuse: tests/view/compat.h struct hfs_inode {copied} '
                         f'differs from fs/9p/hfs.h {actual}')


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
                # ch4: hfs_cap_write, for hfs_lease_acquire_ex's payload.
                ('fs/9p/hfs_session.c', 'void hfs_cap_write(', '\nvoid hfs_args_object('),
                # 2f4: hfs_token_equal too, for hfs_view_exchange. ch4:
                # hfs_json_hash_fields and hfs_hex32, for hfs_lease_decode_check.
                ('fs/9p/hfs_lease.c', 'int hfs_json_hash_fields(', '\nstatic int hfs_normal_target('),
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
        # lease-grant settle helpers (since 5f4 also its round, bind,
        # send, terminal/pending and poll helpers, all in this range). The
        # transport, the ticket reply check, the session block/poll halt,
        # the mount fence and the 0005 source receipt are fixtures (pressure/test.c); the receipt's uncontrolled
        # header stub is redirected to the fixture.
        # xf4 (4h4): up to hfs_lease_decode_check, so the actual
        # hfs_view_grant_moved and hfs_lease_decode_target (the advance
        # classification) are compiled too; hfs_view_advance_target and
        # hfs_view_advance_supported are fixtures.
        # ch4 (4h4 round-2 review): on to hfs_lease_acquire_ex, so
        # xq_advance_unreadable runs through the actual acquisition (decode,
        # unadopted build); hfs_lease_own, the ticket allocation and
        # activation are fixtures. Its definition is renamed, since the
        # observation cases keep their host-model hfs_lease_acquire_ex.
        # 5f4: its helper hfs_lease_acquire_request lies in this range too.
        lease = inputs['fs/9p/hfs_lease.c']
        start = lease.index('struct hfs_grant_settle {')
        source.append('#define hfs_received_source_ex test_received_source_ex')
        # Only hfs_lease_access needs -Wformat off: it prints a u64 with
        # %llu (the kernel's u64 is unsigned long long, the harness's
        # uint64_t is unsigned long). kn1 review 1: just that function.
        access = lease.index('static void hfs_lease_access(', start)
        source.append(lease[start:access])
        source.append('#pragma GCC diagnostic push\n#pragma GCC diagnostic ignored "-Wformat"')
        source.append(lease[access:lease.index('\n}\n', access) + 3])
        source.append('#pragma GCC diagnostic pop')
        source.append(lease[lease.index('\n}\n', access) + 3:
                            lease.index('\nstatic int hfs_lease_own(', start)])
        start = lease.index('/* The arguments of hfs_lease_acquire_ex')
        source.append('#define hfs_lease_acquire_ex actual_hfs_lease_acquire_ex')
        source.append(lease[start:lease.index('\nint hfs_lease_acquire(', start)])
        source.append('#undef hfs_lease_acquire_ex')
        # 5f4: the actual stop wake-up (hfs_session_stop_wake), blocked
        # setter (hfs_session_mark_blocked) and hfs_session_block, renamed
        # actual_hfs_session_block as the exchange cases keep their
        # counting fixture; the stop-wake cases block through it.
        session_c = inputs['fs/9p/hfs_session.c']
        start = session_c.index('void hfs_session_stop_wake(')
        source.append('#define hfs_session_block actual_hfs_session_block')
        source.append(session_c[start:session_c.index('\n/* A provider stop/retain fence.', start)])
        source.append('#undef hfs_session_block')
    # wj4: the mmap mark and hfs_inode layout guards run in both modes (they
    # read only the source and compat.h); 5f4's stop wake-up guard too.
    mmap_mark_guard(args.source)
    hfs_inode_layout_guard(args.source, here / 'compat.h')
    stop_wake_guard(args.source)
    if args.pressure:
        retirement_unlock_guard(args.source)
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
