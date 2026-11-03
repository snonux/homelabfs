#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile strict production crypto and pinned-kernel TUs; never install."""
import argparse
import hashlib
import json
from pathlib import Path
import shlex
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--headers', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
source = a.source.resolve(strict=True)
headers = a.headers.resolve(strict=True)
out = a.output.absolute()
out.mkdir(mode=0o700, exist_ok=False)
here = Path(__file__).resolve().parent
crypto = source/'fs/9p/hfs_crypto'
vendor = json.loads((crypto/'provenance.json').read_text())
for name, row in vendor['files'].items():
    data = (crypto/'vendor'/name).read_bytes()
    if len(data) != row['bytes'] or hashlib.sha256(data).hexdigest() != row['sha256']:
        raise SystemExit('modified upstream crypto: '+name)
commands = []
command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
           '-fwhole-program',
           '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
           '-fno-pie', '-no-pie', '-I'+str(source/'fs/9p'),
           '-I'+str(crypto/'vendor/src'), str(here/'test.c'),
           str(source/'fs/9p/hfs_crypto_core.c'),
           str(source/'fs/9p/hfs_crypto_ed25519.c'), '-lcrypto',
           '-o', str(out/'crypto-test')]
commands.append(command)
subprocess.run(command, check=True, timeout=90)
subprocess.run([str(out/'crypto-test')], check=True, timeout=90)
signer = out/'sign.go'
signer.write_bytes((here/'sign.go.txt').read_bytes())
signatures = subprocess.check_output(['go', 'run', '-p=1', str(signer)], timeout=120)
(out/'actual-go-signatures.json').write_bytes(signatures)
for fields in json.loads(signatures):
    subprocess.run([str(out/'crypto-test'), '--verify-go'],
                   input=('\n'.join(fields)+'\n').encode(), check=True, timeout=90)
raw = (headers/'fs/9p/.hfs_session.o.cmd').read_text().splitlines()[0]
original = shlex.split(raw.split(' := ', 1)[1].split(' ; ', 1)[0])
for name in ['fs/9p/hfs_crypto_core', 'fs/9p/hfs_crypto_ed25519', 'drivers/char/random']:
    command = []
    for word in original:
        if word == '-DMODULE':
            continue
        if word.startswith('-Wp,-MMD,'):
            word = '-Wp,-MMD,'+str(out/(Path(name).name+'.d'))
        elif word.startswith('-I./'):
            word = '-I'+str(headers/word[2:])
        elif word.startswith('./'):
            word = str(headers/word)
        elif word == original[-1]:
            word = str(source/(name+'.c'))
        elif word == 'fs/9p/hfs_session.o':
            word = str(out/(Path(name).name+'.o'))
        elif word.startswith('-DKBUILD_BASENAME='):
            word = '-DKBUILD_BASENAME="'+Path(name).name+'"'
        elif word.startswith('-DKBUILD_MODNAME='):
            word = '-DKBUILD_MODNAME="'+Path(name).name+'"'
        command.append(word)
    command[1:1] = ['-I'+str(crypto/'kernel_include'), '-I'+str(source/'include'),
                    '-I'+str(crypto/'vendor/src'), '-fstack-usage']
    if name.startswith('fs/9p/hfs_crypto_'):
        command.insert(1, '-fwhole-program')
    commands.append(command)
    subprocess.run(command, cwd=headers, check=True, timeout=90)
(out/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
rows = {}
for f in out.glob('*.su'):
    frames = []
    for line in f.read_text().splitlines():
        fields = line.split('\t')
        if len(fields) == 3:
            frames.append(dict(function=fields[0], bytes=int(fields[1]), kind=fields[2]))
    rows[f.name] = sorted(frames, key=lambda x: -x['bytes'])
(out/'stack-frames.json').write_text(json.dumps(rows, indent=2)+'\n')
print(json.dumps({name: rows[name][:5] for name in rows}, indent=2))
