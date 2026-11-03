#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile owned translation units using read-only historical generated headers.

Compile-only evidence, never a complete custom kernel or runtime admission.
"""
import argparse
import json
from pathlib import Path
import shlex
import subprocess
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source',type=Path,required=True)
p.add_argument('--headers',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
a = p.parse_args()
out=a.output.absolute();out.mkdir(mode=0o700,exist_ok=False)
source=a.source.resolve(strict=True);headers=a.headers.resolve(strict=True)
commands=[]
for name in ['fs/9p/hfs_protocol','fs/9p/hfs_session','net/9p/client']:
    raw=(headers/Path(name).parent/('.'+Path(name).name+'.o.cmd')).read_text().splitlines()[0]
    command=shlex.split(raw.split(' := ',1)[1].split(' ; ',1)[0])
    original_source=Path(command[-1]);oldsource=original_source
    for _ in Path(name).parts:oldsource=oldsource.parent
    revised=[]
    for word in command:
        if word.startswith('-Wp,-MMD,'):word='-Wp,-MMD,'+str(out/(Path(name).name+'.d'))
        elif word.startswith('-I./'):word='-I'+str(headers/word[2:])
        elif word.startswith('./'):word=str(headers/word)
        elif word==str(original_source):word=str(source/(name+'.c'))
        elif word==name+'.o':word=str(out/(Path(name).name+'.o'))
        revised.append(word)
    revised[1:1]=['-I'+str(source/'include')]
    commands.append(revised)
    subprocess.run(revised,cwd=headers,check=True,timeout=90)
(out/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
