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
import sys
import subprocess
import time
import tempfile

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--source',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
p.add_argument('--budget-root',type=Path,required=True)
p.add_argument('--run',default='^TestControlledProductionGuestBackendHost$',help='Go test selection')
a=p.parse_args()
here=Path(__file__).resolve().parent
sys.dont_write_bytecode=True
sys.path.insert(0,str(here.parent/'common'))
from runner_process import process_running, finish_process_group
repo=here.parents[4]
out=a.output.absolute();out.mkdir(mode=0o700,exist_ok=False)
budget=a.budget_root.resolve(strict=True)
if not out.is_relative_to(budget):
    raise SystemExit('output must be inside owned budget root')
(out/'tmp').mkdir()
def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def swapout(): return int(next(x.split()[1] for x in Path('/proc/vmstat').read_text().splitlines() if x.startswith('pswpout ')))
initial_swap=swapout()
memory={x.split(':')[0]:int(x.split()[1])*1024 for x in Path('/proc/meminfo').read_text().splitlines()}
if memory['MemAvailable']<8*1024**3 or memory['MemAvailable']<memory['MemTotal']/4:
    raise SystemExit('memory admission refused')
affinity=sorted(os.sched_getaffinity(0))[:2];os.sched_setaffinity(0,affinity)
short_tmp=Path(tempfile.mkdtemp(prefix='hfo04-',dir='/tmp'))
env=dict(os.environ,GOMAXPROCS='2',GOMEMLIMIT='256MiB',GOGC='50',GOFLAGS='-p=1',TMPDIR=str(short_tmp),GOTMPDIR=str(short_tmp),HFS_CONTROLLED_TEST_ROOT=str(out/'tmp'))
owned_roots={budget,short_tmp}
for prior in budget.glob('*/receipt.json'):
    previous=json.loads(prior.read_text()).get('short_tmp')
    if previous:owned_roots.add(Path(previous))
owned_index=budget/'owned-temporaries.json'
if owned_index.exists():owned_roots.update(Path(path) for path in json.loads(owned_index.read_text()))
owned_index.write_text(json.dumps(sorted(str(path) for path in owned_roots if path != budget))+'\n')
def resources():
    memory={x.split(':')[0]:int(x.split()[1])*1024 for x in Path('/proc/meminfo').read_text().splitlines()}
    if memory['MemAvailable']<max(8<<30,memory['MemTotal']/4):raise RuntimeError('memory floor')
    allocated=0
    for parent in owned_roots:
        for directory,_,files in os.walk(parent):
            try:allocated+=Path(directory).lstat().st_blocks*512
            except FileNotFoundError:pass
            for name in files:
                try:allocated+=(Path(directory)/name).lstat().st_blocks*512
                except FileNotFoundError:pass
    if allocated>8<<30:raise RuntimeError('owned disk over8GiB')
    return swapout()
inputs=[here.parent/'common/runner_process.py',*sorted(a.source.rglob('*')),*sorted(here.glob('*')),*sorted((repo/'tools/integration/lib9p').rglob('*.c')),*sorted((repo/'tools/integration/lib9p').rglob('*.h')),*sorted((repo/'internal/controlledvalidation').glob('*.go'))]
frozen_binding={str(path):digest(path) for path in inputs if path.is_file()}
(out/'source-binding.json').write_text(json.dumps(frozen_binding,indent=2)+'\n')
stages=[]
def run(name,argv):
    before=resources();observations=[before]
    def limits():
        resource.setrlimit(resource.RLIMIT_CPU,(1200,1200))
        resource.setrlimit(resource.RLIMIT_CORE,(0,0))
    started=last=time.monotonic();peak=gap=0;reason=None
    with (out/(name+'.log')).open('wb') as log:
        process=subprocess.Popen(argv,cwd=repo,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,preexec_fn=limits)
        try:
            while process_running(process):
                now=time.monotonic();gap=max(gap,now-last);last=now;rss=0
                for proc in Path('/proc').iterdir():
                    if not proc.name.isdecimal(): continue
                    try:
                        values=(proc/'stat').read_text().rsplit(')',1)[1].split()
                        if int(values[2])==process.pid: rss+=int(values[21])*os.sysconf('SC_PAGE_SIZE')
                    except (OSError,ValueError,IndexError): pass
                peak=max(peak,rss)
                observations.append(resources())
                if rss>2*1024**3 or now-started>1200:reason=reason or 'sampled RSS/time bound'
                if reason:
                    break
                time.sleep(.1)
        except (Exception,KeyboardInterrupt) as error:
            reason=f'{type(error).__name__}: {error}'
        code=finish_process_group(process)
    try:after_resources=resources()
    except (Exception,KeyboardInterrupt) as error:
        after_resources=None
        reason=reason or f'{type(error).__name__}: {error}'
    try:
        swap_telemetry={key:int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith(key+':'))) for key in ['SwapTotal','SwapFree']}
    except (Exception,KeyboardInterrupt) as error:
        swap_telemetry=None
        reason=reason or f'{type(error).__name__}: {error}'
    stages.append({'argv':argv,'exit':code,'seconds':time.monotonic()-started,'peak_group_rss':peak,'max_sample_gap':gap,'limit_reason':reason,'pswpout_before':before,'pswpout_after':after_resources,'pswpout_observed':observations,'swap_telemetry':swap_telemetry})
    (out/'stages.json').write_text(json.dumps(stages,indent=2)+'\n')
    after={path:digest(Path(path)) for path in frozen_binding}
    (out/'source-binding-after.json').write_text(json.dumps(after,indent=2)+'\n')
    if after!=frozen_binding:raise SystemExit('consumed inputs changed during stage')
    if code or reason: raise SystemExit('FAIL: '+str(out/(name+'.log')))
run('build',['python3',str(here/'build.py'),'--source',str(a.source.resolve()),'--output',str(out/'consumer'),'--sanitize'])
overlay=out/'overlay.json'
overlay.write_text(json.dumps({'Replace':{str(repo/'internal/controlledvalidation/o04_production_guest_test.go'):str(here/'host_test.go.txt')}}))
env['HFS_CONTROLLED_CONSUMER']=str(out/'consumer/consumer')
run('host',['go','test','-overlay',str(overlay),'-p','1','-run',a.run,'-count=1','-timeout=90s','-v','./internal/controlledvalidation'])
(out/'receipt.json').write_text(json.dumps({'legacy_issuance':'minor0/1 ROOT claim explicitly issued through real legacy Provider.Allocate with nil enrollment Protocol; not guest pre-send allocation or native/source/publication authority','scope':'production guest issuance/framing and owned lib9p -> real host Transport/Service/provider/journal; kernel/native fd/resource observations and publication refusal are explicit primitives; no native/source/runtime acceptance','short_tmp':str(short_tmp),'affinity':affinity,'memory_before':memory,'source':{str(path.relative_to(repo)):digest(path) for path in [here/'consumer.c',here/'host_test.go.txt',repo/'tools/integration/lib9p/src/hfs9p.c']},'stages':stages},indent=2)+'\n')
print((out/'host.log').read_text())
