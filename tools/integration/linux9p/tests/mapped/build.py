#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile actual owned carrier/guest verifier; OS boundaries remain primitives."""
import argparse
import json
import os
from pathlib import Path
import subprocess
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
here = Path(__file__).resolve().parent
repo = here.parents[4]
out = a.output.absolute()
out.mkdir(mode=0o700, exist_ok=False)
src = a.source.resolve(strict=True)
os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
session=(src/'fs/9p/hfs_session.c').read_text()
start=session.index('hfs_buf_printf(&command, "{')
end=session.index('mutex_unlock(&session->lock);',start)
(out/'builder.inc').write_text('struct builder_session {\n struct { const char *context; u32 context_len; } enrollment;\n u16 bridge_minor;\n};\nstruct builder_slot { bool ack_ready; u64 number; u8 hash[32]; };\nstatic void production_build(struct builder_session *session,\n struct builder_slot *slot,u64 cell,const char *operation,\n struct hfs_buf *payload,struct hfs_buf *out) {\n struct hfs_buf command = *out;\n'+session[start:end]+'\n *out = command;\n}\n')
compat=(here.parent/'codec/compat.h').read_text().replace('typedef uint64_t u64;', 'typedef unsigned long long u64;').replace(
    'hfs_test_last_allocation_size = n;',
    'if (n > hfs_test_last_allocation_size) hfs_test_last_allocation_size = n;').replace(
    'hfs_test_last_allocation_size = n * s;',
    'if (n*s > hfs_test_last_allocation_size) hfs_test_last_allocation_size = n*s;')
(out/'compat.h').write_text(compat)
command = ['cc' , '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
           '-Wno-sign-compare', '-D_GNU_SOURCE', '-D_DEFAULT_SOURCE',
           '-D_POSIX_C_SOURCE=200809L', '-DHFS_CODEC_USERSPACE',
           '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
           '-fno-pie', '-no-pie', '-include', str(out/'compat.h'),
           '-I', str(src/'fs/9p'), '-I',str(out), '-I', str(repo), str(here/'consumer.c'),
           str(src/'fs/9p/hfs_protocol.c'),
           str(repo/'tools/integration/lib9p/src/hfs9p_json.c'),
           str(repo/'tools/integration/lib9p/src/hfs9p_ledger.c'),
           str(repo/'tools/integration/lib9p/src/hfs9p_supervisor.c'),
           '-pthread', '-lcrypto', '-o', str(out/'consumer')]
(out/'command.json').write_text(json.dumps(command, indent=2)+'\n')
subprocess.run(command, check=True, timeout=90)
