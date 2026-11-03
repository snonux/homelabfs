#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Reproduce only the maintained owned Linux files; never copy a full tree."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tarfile

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--archive',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
p.add_argument('--source',type=Path,required=True)
a=p.parse_args()
here=Path(__file__).resolve().parent
integration=here.parents[1]
manifest=json.loads((integration/'source.json').read_bytes())
out=a.output.absolute()
out.mkdir(mode=0o700,exist_ok=False)
source=a.source.resolve(strict=True)
archive=a.archive.resolve(strict=True)
state=hashlib.sha256()
with archive.open('rb') as stream:
    while data:=stream.read(1<<20):state.update(data)
if state.hexdigest()!=manifest['archive_sha256']:raise SystemExit('archive binding mismatch')
base=manifest['base_file_sha256']
originals={}
with tarfile.open(archive,'r:gz') as tar:
    if tar.pax_headers.get('comment')!=manifest['commit']:raise SystemExit('archive commit mismatch')
    prefix='linux-'+manifest['commit']+'/'
    for member in tar:
        name=member.name.removeprefix(prefix)
        if name not in base or base[name] is None or not member.isfile():continue
        data=tar.extractfile(member).read()
        if hashlib.sha256(data).hexdigest()!=base[name]:raise SystemExit('base hash mismatch: '+name)
        target=out/name
        target.parent.mkdir(parents=True,exist_ok=True)
        target.write_bytes(data)
        originals[name]=base[name]
if len(originals)!=sum(value is not None for value in base.values()):raise SystemExit('missing original')
commands=[]
for patch in manifest['patches']:
    path=integration/'patches'/patch['file']
    if hashlib.sha256(path.read_bytes()).hexdigest()!=patch['sha256']:raise SystemExit('patch changed')
    command=['patch','--batch','--fuzz=0','-p1','-i',str(path)]
    commands.append(command)
    subprocess.run(command,cwd=out,check=True,timeout=90)
outputs={}
for name,expected in manifest['patched_file_sha256'].items():
    actual=hashlib.sha256((out/name).read_bytes()).hexdigest()
    if actual!=expected or hashlib.sha256((source/name).read_bytes()).hexdigest()!=expected:raise SystemExit('reproduced source mismatch: '+name)
    outputs[name]=actual
(out/'reproduction.json').write_text(json.dumps(dict(commands=commands,files=outputs,scope=__doc__),indent=2)+'\n')
print(json.dumps(dict(owned_files=len(outputs),full_tree_copied=False)))
