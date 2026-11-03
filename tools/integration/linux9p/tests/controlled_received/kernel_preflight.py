#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Actual C6 kernel translation units and owned-object link; compile-only."""
import argparse
import hashlib
import json
from pathlib import Path
import shlex
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--headers', type=Path, required=True)
p.add_argument('--fixture', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
source = a.source.resolve(strict=True)
headers = a.headers.resolve(strict=True)
out = a.output.absolute()
out.mkdir(mode=0o700, exist_ok=False)
public = json.loads(a.fixture.read_bytes())['operator_public_key']
raw = (headers/'fs/9p/.hfs_session.o.cmd').read_text().splitlines()[0]
original = shlex.split(raw.split(' := ', 1)[1].split(' ; ', 1)[0])
stock = Path(original[-1]).parents[2]
crypto = source/'fs/9p/hfs_crypto'
commands = []
objects = []
inputs = {}
for path in sorted((source/'fs/9p').rglob('*')):
    if path.is_file():
        inputs[str(path.relative_to(source))] = hashlib.sha256(path.read_bytes()).hexdigest()
for name in [*[str(path.relative_to(source)).removesuffix('.c')
               for path in sorted((source/'fs/9p').glob('hfs*.c'))],
             'net/9p/trans_virtio', 'net/9p/client', 'net/9p/protocol',
             'drivers/char/random']:
    command = []
    target = out/(Path(name).name+'.o')
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
            word = str(target)
        elif word.startswith('-DKBUILD_BASENAME='):
            word = '-DKBUILD_BASENAME="'+Path(name).name+'"'
        elif word.startswith('-DKBUILD_MODNAME='):
            word = '-DKBUILD_MODNAME="'+Path(name).name+'"'
        command.append(word)
    command[1:1] = ['-I'+str(source/'include'), '-I'+str(stock/'net/9p'),
                    '-I'+str(source/'fs/9p'), '-fstack-usage',
                    '-DCONFIG_9P_FS_HOMELABFS_CONTROLLED=1',
                    '-DCONFIG_9P_FS_HOMELABFS_OPERATOR_KEY="'+public+'"',
                    '-DKBUILD_MODFILE="'+name+'"']
    if name.startswith('fs/9p/hfs_crypto_'):
        command[1:1] = ['-fwhole-program', '-I'+str(crypto/'kernel_include'),
                        '-I'+str(crypto/'vendor/src')]
    commands.append(command)
    (out/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
    subprocess.run(command, cwd=headers, check=True, timeout=120)
    objects.append(target)
    inputs[name+'.c'] = hashlib.sha256((source/(name+'.c')).read_bytes()).hexdigest()
link = ['ld', '-r', '-o', str(out/'owned-c6.o'), *map(str, objects)]
commands.append(link)
subprocess.run(link, check=True, timeout=90)
undefined = subprocess.check_output(['nm', '-u', str(out/'owned-c6.o')], text=True)
(out/'undefined.txt').write_text(undefined)
unresolved = [line for line in undefined.splitlines() if line.split()[-1].startswith(('hfs_', 'v9fs_hfs_', 'p9_client_hfs_', 'p9pdu_hfs_'))]
if unresolved:
    raise SystemExit('unresolved owned integration symbols: '+repr(unresolved))
(out/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
(out/'source-binding.json').write_text(json.dumps(inputs, indent=2)+'\n')
frames = []
for path in sorted(out.glob('*.su')):
    for line in path.read_text().splitlines():
        fields = line.split('\t')
        if len(fields) == 3:
            frames.append({'function': fields[0], 'bytes': int(fields[1]), 'kind': fields[2]})
(out/'stack-frames.json').write_text(json.dumps(sorted(frames, key=lambda row: -row['bytes']), indent=2)+'\n')
print(json.dumps({'translation_units': len(objects), 'owned_link_symbols_resolved': True,
                  'scope': __doc__}, indent=2))
