#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Exercise production request retirement/result ownership at primitive boundaries."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    args.output.mkdir(parents=False, exist_ok=False, mode=0o700)
    names = ['fs/9p/hfs_session.c', 'fs/9p/hfs.h', 'fs/9p/hfs_view.h', 'fs/9p/hfs_lease.h']
    inputs = {name:(args.source/name).read_text() for name in names}
    body = ['#include "hfs_protocol.h"']
    for name in names[2:]:
        body.append('\n'.join(line for line in inputs[name].splitlines() if not line.startswith('#include')))
    header = inputs[names[1]]
    first = header.index('struct hfs_request {')
    last = header.index('\n};', first)+len('\n};')
    body.append(header[first:last])
    session = inputs[names[0]]
    for first, last in [('static int hfs_field_u64(', '\nint hfs_uuid('),
                        ('static void hfs_request_result_put(', '\nstatic int hfs_request_run('),
                        ('int hfs_retire(', '\nEXPORT_SYMBOL_GPL(hfs_retire);')]:
        start = session.index(first)
        body.append(session[start:session.index(last,start)])
    (args.output/'production.c').write_text('\n'.join(body)+'\n')
    for file in ['test.c','compat.h']:
        (args.output/file).write_bytes((here/file).read_bytes())
    for file in ['kernel_format.h','kernel_format.c']:
        (args.output/file).write_bytes((here.parent/'common'/file).read_bytes())
    primitive = (here.parent/'codec/compat.h').read_text().replace('typedef uint64_t u64;', 'typedef unsigned long long u64;')
    (args.output/'hfs_codec_compat.h').write_text(primitive)
    binding = {'source':{name:hashlib.sha256(data.encode()).hexdigest() for name,data in inputs.items()},
               'scope':'actual C retirement/result-owner code; primitive mutex/transport/proof fixtures; no VFS/runtime/source closure proof',
               'primitive_u64':'unsigned long long, matching selected kernel ABI'}
    (args.output/'binding.json').write_text(json.dumps(binding,indent=2)+'\n')
    sys.dont_write_bytecode=True
    spec=importlib.util.spec_from_file_location('codec_runner',here.parent/'codec/run.py')
    runner=importlib.util.module_from_spec(spec);spec.loader.exec_module(runner)
    os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
    log=args.output/'requests.log';log.write_text(json.dumps(binding)+'\n')
    runner.run(['cc','-std=gnu11','-O1','-g','-Wall','-Wextra','-Werror','-Wno-sign-compare',
                '-DHFS_CODEC_USERSPACE','-include',str(args.output/'hfs_codec_compat.h'),
                '-include',str(args.output/'kernel_format.h'),'-I',str(args.source/'fs/9p'),
                '-I',str(args.output),'-fsanitize=address,undefined','-fno-omit-frame-pointer',
                '-fno-pie','-no-pie',str(args.source/'fs/9p/hfs_protocol.c'),
                str(args.output/'kernel_format.c'),str(args.output/'test.c'),'-pthread',
                '-lcrypto','-o',str(args.output/'requests-test')],here,log)
    runner.run([str(args.output/'requests-test')],here,log)
    print(log.read_text())


if __name__=='__main__':
    main()
