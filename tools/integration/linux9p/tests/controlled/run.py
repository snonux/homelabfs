#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Run the production C guest/backend against the real local host using an overlay."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import time
import tempfile

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--source',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
p.add_argument('--budget-root',type=Path,required=True)
a=p.parse_args()
here=Path(__file__).resolve().parent
repo=here.parents[4]
out=a.output.absolute();out.mkdir(mode=0o700,exist_ok=False)
budget=a.budget_root.resolve(strict=True)
if not out.is_relative_to(budget):
    raise SystemExit('output must be inside owned budget root')
(out/'tmp').mkdir()
def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def swapout(): return int(next(x.split()[1] for x in Path('/proc/vmstat').read_text().splitlines() if x.startswith('pswpout ')))
initial_swap=swapout();time.sleep(1)
memory={x.split(':')[0]:int(x.split()[1])*1024 for x in Path('/proc/meminfo').read_text().splitlines()}
if memory['MemAvailable']<8*1024**3 or memory['MemAvailable']<memory['MemTotal']/4 or memory['SwapTotal']-memory['SwapFree']>memory['SwapTotal']*.55 or swapout()!=initial_swap:
    raise SystemExit('memory/swap admission refused')
affinity=sorted(os.sched_getaffinity(0))[:2];os.sched_setaffinity(0,affinity)
short_tmp=Path(tempfile.mkdtemp(prefix='hfo04-',dir='/tmp'))
env=dict(os.environ,GOMAXPROCS='2',GOMEMLIMIT='1GiB',TMPDIR=str(short_tmp),GOTMPDIR=str(short_tmp),HFS_CONTROLLED_TEST_ROOT=str(out/'tmp'))
stages=[]
def run(name,argv):
    def limits():
        resource.setrlimit(resource.RLIMIT_CPU,(1200,1200))
        resource.setrlimit(resource.RLIMIT_CORE,(0,0))
    started=last=time.monotonic();peak=gap=0;reason=None
    with (out/(name+'.log')).open('wb') as log:
        process=subprocess.Popen(argv,cwd=repo,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,preexec_fn=limits)
        while process.poll() is None:
            now=time.monotonic();gap=max(gap,now-last);last=now;rss=0
            for proc in Path('/proc').iterdir():
                if not proc.name.isdecimal(): continue
                try:
                    values=(proc/'stat').read_text().rsplit(')',1)[1].split()
                    if int(values[2])==process.pid: rss+=int(values[21])*os.sysconf('SC_PAGE_SIZE')
                except (OSError,ValueError,IndexError): pass
            peak=max(peak,rss)
            owned=0
            for parent in [budget,short_tmp]:
                for path in [parent,*parent.rglob('*')]:
                    try: owned+=path.lstat().st_blocks*512
                    except FileNotFoundError: pass  # successful owned fixture cleanup raced this sample
            if rss>2*1024**3 or owned>8*1024**3 or now-started>1200 or swapout()>initial_swap:
                reason='sampled RSS/disk/time/swap bound';os.killpg(process.pid,signal.SIGTERM);break
            time.sleep(.1)
        code=process.wait(timeout=5)
    stages.append({'argv':argv,'exit':code,'seconds':time.monotonic()-started,'peak_group_rss':peak,'max_sample_gap':gap,'limit_reason':reason})
    (out/'stages.json').write_text(json.dumps(stages,indent=2)+'\n')
    if code or reason: raise SystemExit('FAIL: '+str(out/(name+'.log')))
run('build',['python3',str(here/'build.py'),'--source',str(a.source.resolve()),'--output',str(out/'consumer'),'--sanitize'])
overlay=out/'overlay.json'
overlay.write_text(json.dumps({'Replace':{str(repo/'internal/controlledvalidation/o04_production_guest_test.go'):str(here/'host_test.go.txt')}}))
env['HFS_CONTROLLED_CONSUMER']=str(out/'consumer/consumer')
run('host',['go','test','-overlay',str(overlay),'-p','1','-run','^TestControlledProductionGuestBackendHost$','-count=1','-timeout=90s','-v','./internal/controlledvalidation'])
(out/'receipt.json').write_text(json.dumps({'scope':'production guest issuance/framing and owned lib9p -> real host Transport/Service/provider/journal; kernel/native fd/resource observations and publication refusal are explicit primitives; no native/source/runtime acceptance','short_tmp':str(short_tmp),'affinity':affinity,'memory_before':memory,'source':{str(path.relative_to(repo)):digest(path) for path in [here/'consumer.c',here/'host_test.go.txt',repo/'tools/integration/lib9p/src/hfs9p.c']},'stages':stages},indent=2)+'\n')
print((out/'host.log').read_text())
