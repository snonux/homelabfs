#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Actual production capture and ticket-family tests at credential/pid/map primitives."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys


def struct(source, name):
    first=source.index('struct '+name+' {')
    return source[first:source.index('\n};',first)+3]


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source',required=True,type=Path)
    p.add_argument('--output',required=True,type=Path)
    args=p.parse_args();here=Path(__file__).resolve().parent
    args.output.mkdir(mode=0o700,exist_ok=False)
    names=['fs/9p/hfs_session.c','fs/9p/hfs.h','fs/9p/hfs_lease.c','fs/9p/hfs_lease.h','fs/9p/hfs_policy.c','fs/9p/hfs_policy.h']
    source={n:(args.source/n).read_text() for n in names}
    body=['#include "hfs_protocol.h"']
    for name in ['hfs_cred','hfs_process']:
        body.append(struct(source[names[1]],name))
    for name in ['hfs_view_family','hfs_view_ticket']:
        body.append(struct(source[names[3]],name))
    body+=['#define HFS_VIEW_TICKETS_MAX 256U','#define HFS_VIEW_CONTROL_TICKETS_MAX 256U','#define HFS_VIEW_FAMILIES_MAX 4096U']
    session=source[names[0]]
    first=session.index('void hfs_cred_write(')
    body.append(session[first:session.index('\n\nstatic void hfs_process_exit_work',first)])
    first=session.index('static bool hfs_cred_equal(')
    body.append(session[first:session.index('\nstatic int hfs_reply_scope',first)])
    lease=source[names[2]]
    for first,last in [('void hfs_view_ticket_put(', '\nbool hfs_view_tickets_busy('),
                       ('void hfs_view_tickets_destroy(', '\nint hfs_view_ticket_prepare('),
                       ('int hfs_view_ticket_prepare(', '\nint hfs_view_ticket_activate(')]:
        start=lease.index(first);body.append(lease[start:lease.index(last,start)])
    (args.output/'production.c').write_text('\n'.join(body)+'\n')
    codec=(here.parent/'codec/compat.h').read_text().replace('typedef uint64_t u64;','typedef unsigned long long u64;')
    codec+='\n#ifndef HFS_CAPTURE_SORT_COMPAT\n#define HFS_CAPTURE_SORT_COMPAT\n#include <sys/stat.h>\n#define MAX_NON_LFS 2147483647UL\nstatic inline void sort(void *b,size_t n,size_t s,int (*cmp)(const void *,const void *),void (*swap)(void *,void *,int)) { (void)swap;qsort(b,n,s,cmp); }\n#endif\n'
    (args.output/'hfs_codec_compat.h').write_text(codec)
    for n in ['test.c','compat.h']:(args.output/n).write_bytes((here/n).read_bytes())
    for n in ['kernel_format.h','kernel_format.c']:(args.output/n).write_bytes((here.parent/'common'/n).read_bytes())
    binding={'source':{n:hashlib.sha256(v.encode()).hexdigest() for n,v in source.items()},'scope':'actual capture, complete numeric policy and ticket-family code; current/cred/pid/map primitives are fixtures, no running-kernel or identity authority proof'}
    (args.output/'binding.json').write_text(json.dumps(binding,indent=2)+'\n')
    sys.dont_write_bytecode=True
    spec=importlib.util.spec_from_file_location('codec_runner',here.parent/'codec/run.py');runner=importlib.util.module_from_spec(spec);spec.loader.exec_module(runner)
    os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
    log=args.output/'capture.log';log.write_text(json.dumps(binding)+'\n')
    runner.run(['cc','-std=gnu11','-O1','-g','-Wall','-Wextra','-Werror','-Wno-sign-compare',
                '-DHFS_CODEC_USERSPACE','-include',str(args.output/'hfs_codec_compat.h'),'-include',str(args.output/'kernel_format.h'),
                '-I',str(args.source/'fs/9p'),'-I',str(args.output),'-fsanitize=address,undefined','-fno-omit-frame-pointer','-fno-pie','-no-pie',
                str(args.source/'fs/9p/hfs_protocol.c'),str(args.source/'fs/9p/hfs_policy.c'),str(args.output/'kernel_format.c'),str(args.output/'test.c'),
                '-pthread','-lcrypto','-o',str(args.output/'capture-test')],here,log)
    runner.run([str(args.output/'capture-test')],here,log)
    print(log.read_text())


if __name__=='__main__':main()
