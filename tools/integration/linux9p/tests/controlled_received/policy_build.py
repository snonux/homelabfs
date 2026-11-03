#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Focused inert complete SOURCE policy vectors; no provider/receipt authority."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
sys.dont_write_bytecode=True
from policy_cases import canonical,cases

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source',type=Path,required=True)
parser.add_argument('--fixture',type=Path,required=True)
parser.add_argument('--output',type=Path,required=True)
args=parser.parse_args()
here=Path(__file__).resolve().parent
out=args.output.absolute()
out.mkdir(mode=0o700,exist_ok=False)
source=args.source.resolve(strict=True)
paths=[]
for case in cases(json.loads(args.fixture.read_bytes())):
    path=out/(case.pop('name')+'.json')
    path.write_bytes(canonical(case))
    paths.append(path)
(out/'hfs_codec_compat.h').write_text((here.parent/'codec/compat.h').read_text().replace('typedef uint64_t u64;','typedef unsigned long long u64;'))
command=['cc','-std=gnu11','-O1','-g','-Wall','-Wextra','-Werror','-Wno-sign-compare',
         '-DHFS_CODEC_USERSPACE','-fsanitize=address,undefined','-fno-omit-frame-pointer',
         '-fno-pie','-no-pie','-include',str(out/'hfs_codec_compat.h'),
         '-I'+str(source/'fs/9p'),str(here/'policy_test.c'),str(source/'fs/9p/hfs_protocol.c'),
         '-lcrypto','-o',str(out/'policy-test')]
inputs=[here/'policy_cases.py',here/'policy_test.c',here/'policy_build.py',
        here.parent/'codec/compat.h',source/'fs/9p/hfs_protocol.c',source/'fs/9p/hfs_protocol.h',args.fixture,*paths]
(out/'source-binding.json').write_text(json.dumps({str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs},indent=2)+'\n')
(out/'commands.json').write_text(json.dumps([command,[str(out/'policy-test'),*map(str,paths)]],indent=2)+'\n')
subprocess.run(command,check=True,timeout=90)
subprocess.run([str(out/'policy-test'),*map(str,paths)],check=True,timeout=45)
