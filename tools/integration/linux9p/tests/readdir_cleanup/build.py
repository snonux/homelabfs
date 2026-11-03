#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile production cleanup consumer for IPC with the real Go host provider."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
p.add_argument('--sanitize', action='store_true')
a = p.parse_args()
here = Path(__file__).resolve().parent
source = a.source.resolve(strict=True)
out = a.output.absolute()
out.mkdir(exist_ok=False)
session = (source / 'fs/9p/hfs_session.c').read_text()
start = session.index('static int hfs_readdir_cleanup(')
end = session.index('\nint hfs_readdir_forget(', start)
(out / 'consumer.inc').write_text(session[start:end])
command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
           '-Wno-sign-compare', '-DHFS_CODEC_USERSPACE', '-include',
           str(here.parent / 'codec/compat.h'), '-I', str(source / 'fs/9p'),
           '-I', str(out), str(source / 'fs/9p/hfs_protocol.c'),
           str(here / 'consumer.c'), '-lcrypto', '-o', str(out / 'consumer')]
if a.sanitize:
    command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer', '-fno-pie', '-no-pie']
with (out / 'build.log').open('w') as log:
    subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=60)
(out / 'bindings.json').write_text(json.dumps({
    'session_sha256': hashlib.sha256(session.encode()).hexdigest(),
    'scope': 'production cleanup consumer over live IPC; allocation/transport primitives only; no native/runtime acceptance'
}, indent=2) + '\n')
print(out / 'consumer')
