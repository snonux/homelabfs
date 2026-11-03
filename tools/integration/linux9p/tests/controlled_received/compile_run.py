#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Bounded actual C6 portable consumer compile preflight, compile-only.

No provider, installed kernel, source authority or physical proof.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--source',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
p.add_argument('--budget-root',type=Path,required=True)
p.add_argument('--fixture',type=Path,required=True)

a=p.parse_args()
here=Path(__file__).resolve().parent
repo=here.parents[4]
budget=a.budget_root.resolve(strict=True)
out=a.output.absolute()
if not out.is_relative_to(budget):raise SystemExit('output must belong to budget root')
out.mkdir(mode=0o700,exist_ok=False)
source=a.source.resolve(strict=True)
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
short=Path(tempfile.mkdtemp(prefix='hfs04kernel-',dir='/tmp'))
env=dict(os.environ,TMPDIR=str(short),GOTMPDIR=str(short),HFS_CONTROLLED_TEST_ROOT=str(short),GOMAXPROCS='2',GOMEMLIMIT='1GiB',GOFLAGS='-p=1',HFS_MAPPED_CONSUMER=str(out/'build/consumer'))
os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
def resources():
    m={x.split(':')[0]:int(x.split()[1])*1024 for x in Path('/proc/meminfo').read_text().splitlines()}
    if m['MemAvailable']<max(8<<30,m['MemTotal']/4) or m['SwapTotal']-m['SwapFree']>m['SwapTotal']*.55:raise RuntimeError('memory/swap floor')
    disk=0
    for root in [budget,short]:
        for directory,_,files in os.walk(root):
            for name in files:
                try:disk+=(Path(directory)/name).stat().st_blocks*512
                except FileNotFoundError:pass
    if disk>8<<30:raise RuntimeError('owned disk over8GiB')
    return int(next(x.split()[1] for x in Path('/proc/vmstat').read_text().splitlines() if x.startswith('pswpout ')))
rows=[]
def run(name,argv):
    before=resources();time.sleep(3)
    if resources()!=before:raise RuntimeError('prelaunch swapout changed')
    start=time.monotonic();peak=0;reason=None
    with (out/(name+'.stdout')).open('wb') as stdout,(out/(name+'.stderr')).open('wb') as stderr:
        process=subprocess.Popen(argv,cwd=repo,env=env,stdout=stdout,stderr=stderr,start_new_session=True)
        try:
            while process.poll() is None:
                if resources()!=before:raise RuntimeError('swapout advanced')
                rss=0
                for entry in Path('/proc').iterdir():
                    if not entry.name.isdecimal():continue
                    try:
                        fields=(entry/'stat').read_text().rsplit(')',1)[1].split()
                        if int(fields[2])==process.pid:rss+=int(fields[21])*os.sysconf('SC_PAGE_SIZE')
                    except (OSError,ValueError,IndexError):pass
                peak=max(peak,rss)
                if rss>2<<30 or time.monotonic()-start>1200:raise RuntimeError('RSS/time bound')
                time.sleep(.1)
        except (RuntimeError,KeyboardInterrupt) as error:
            reason=str(error);os.killpg(process.pid,signal.SIGTERM)
        try:code=process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid,signal.SIGKILL);code=process.wait()
    rows.append(dict(name=name,argv=argv,exit=code,seconds=time.monotonic()-start,peak_group_rss=peak,limit_reason=reason,pswpout_before=before))
    (out/'receipt.json').write_text(json.dumps(dict(scope=__doc__,short_tmp=str(short),stages=rows),indent=2)+'\n')
    if code or reason:raise SystemExit('failed stage preserved: '+str(out))
inputs=[here/'build.py',here/'consumer.c',here/'compat.h',a.fixture.resolve(strict=True),*sorted((source/'fs/9p').rglob('*')),source/'net/9p/trans_virtio.c',source/'drivers/char/random.c',source/'include/linux/random.h',source/'include/net/9p/client.h']
(out/'source-binding.json').write_text(json.dumps({str(path):digest(path) for path in inputs if path.is_file()},indent=2)+'\n')
public=json.loads(a.fixture.read_bytes())['operator_public_key']
run('actual-c6-consumer-build',['python3',str(here/'build.py'),'--source',str(source),'--operator-key',public,'--output',str(out/'build')])
print((out/'actual-c6-consumer-build.stdout').read_text())
