#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Bounded actual C6 component integration with real host Producer and source.

Declared OS primitives; no installed kernel, native PTE/TLB, deployment or C7 proof.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import sys
import subprocess
import tempfile
import time

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--source',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
p.add_argument('--budget-root',type=Path,required=True)
p.add_argument('--host-tree',type=Path,required=True)
p.add_argument('--final-focus',action='store_true')
p.add_argument('--receipt-tests',action='store_true')
p.add_argument('--scenario',choices=['provider','corruption','dirty','gap','mixed','effected','readdir','interleaving','allocate','terminal','terminal-clean','terminal-smoke','terminal-handoff','terminal-dirty','terminal-owners','terminal-backend','terminal-kernel80','terminal-exhaustion','both'],default='provider')

a=p.parse_args()
here=Path(__file__).resolve().parent
sys.dont_write_bytecode=True
sys.path.insert(0,str(here.parent/'common'))
from runner_process import process_running, finish_process_group
repo=here.parents[4]
budget=a.budget_root.resolve(strict=True)
out=a.output.absolute()
if not out.is_relative_to(budget):raise SystemExit('output must belong to budget root')
out.mkdir(mode=0o700,exist_ok=False)
source=a.source.resolve(strict=True)
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
short=Path(tempfile.mkdtemp(prefix='hfs04kernel-',dir='/tmp'))
env=dict(os.environ,TMPDIR=str(short),GOTMPDIR=str(short),HFS_CONTROLLED_TEST_ROOT=str(short),GOMAXPROCS='2',GOMEMLIMIT='256MiB',GOGC='50',GOFLAGS='-p=1',HFS_MAPPED_CONSUMER=str(out/'build/consumer'))
os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
# Include retained temporary outputs from every earlier task-owned stage.
owned_roots = {budget, short}
for prior in budget.glob('*/receipt.json'):
    prior_tmp = json.loads(prior.read_text()).get('short_tmp')
    if prior_tmp:
        owned_roots.add(Path(prior_tmp))
owned_index = budget/'owned-temporaries.json'
if owned_index.exists():
    owned_roots.update(Path(path) for path in json.loads(owned_index.read_text()))
owned_index.write_text(json.dumps(sorted(str(path) for path in owned_roots if path != budget))+'\n')
def resources():
    m={x.split(':')[0]:int(x.split()[1])*1024 for x in Path('/proc/meminfo').read_text().splitlines()}
    if m['MemAvailable']<max(8<<30,m['MemTotal']/4):raise RuntimeError('memory floor')
    disk=0
    for root in owned_roots:
        for directory,_,files in os.walk(root):
            try:disk += Path(directory).lstat().st_blocks*512
            except FileNotFoundError:pass
            for name in files:
                try:disk+=(Path(directory)/name).lstat().st_blocks*512
                except FileNotFoundError:pass
    if disk>8<<30:raise RuntimeError('owned disk over8GiB')
    return int(next(x.split()[1] for x in Path('/proc/vmstat').read_text().splitlines() if x.startswith('pswpout ')))
rows=[]
def run(name,argv):
    before=resources();observations=[before]
    start=time.monotonic();peak=0;reason=None
    with (out/(name+'.stdout')).open('wb') as stdout,(out/(name+'.stderr')).open('wb') as stderr:
        process=subprocess.Popen(argv,cwd=repo,env=env,stdout=stdout,stderr=stderr,start_new_session=True)
        try:
            while process_running(process):
                observations.append(resources())
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
    rows.append(dict(name=name,argv=argv,exit=code,seconds=time.monotonic()-start,peak_group_rss=peak,limit_reason=reason,pswpout_before=before,pswpout_after=after_resources,pswpout_observed=observations,swap_telemetry=swap_telemetry))
    (out/'receipt.json').write_text(json.dumps(dict(scope=__doc__,short_tmp=str(short),stages=rows),indent=2)+'\n')
    after={path:digest(Path(path)) for path in frozen_binding}
    (out/'source-binding-after.json').write_text(json.dumps(after,indent=2)+'\n')
    if after!=frozen_binding:raise SystemExit('consumed inputs changed during stage')
    if code or reason:raise SystemExit('failed stage preserved: '+str(out))
host_tree=a.host_tree.resolve(strict=True)
frozen=out/'frozen-host'
frozen.mkdir(mode=0o700)
replacements={}
for relative in ['internal/controlledruntime/producer_test.go', 'tools/integration/linux9p/tests/controlled_received/host_test.go.txt']:
    path=host_tree/relative
    before=digest(path)
    target=frozen/path.name
    target.write_bytes(path.read_bytes())
    if digest(path)!=before or digest(target)!=before:raise SystemExit('host inputs changed during freeze copy')
    destination=repo/relative if relative.endswith('.go') else repo/'internal/controlledruntime/c6_actual_received_test.go'
    replacements[str(destination)]=str(target)
overlay=out/'overlay.json'
overlay.write_text(json.dumps({'Replace':replacements},indent=2)+'\n')
inputs=[here.parent/'common/runner_process.py',here/'build.py',here/'consumer.c',here/'compat.h',here/'README.md',*sorted(frozen.iterdir()),*sorted((source/'fs/9p').rglob('*')),source/'net/9p/trans_virtio.c',source/'net/9p/client.c',
        here/'run.py',here/'bootstrap_test.inc',here.parent/'controlled_provider/compat.h',here.parent/'codec/compat.h',
        here.parent/'common/kernel_format.h',here.parent/'common/kernel_format.c',
        *sorted((repo/'tools/integration/lib9p').rglob('*.c')),
        *sorted((repo/'tools/integration/lib9p').rglob('*.h')),
        *sorted((repo/'internal/guestbridge').glob('*.go')),
        *sorted((repo/'internal/controlledruntime').glob('*.go')),
        *sorted((repo/'internal/viewcontrol').glob('*.go')),*sorted((repo/'internal/controlledvalidation').glob('*.go'))]
frozen_binding={str(path):digest(path) for path in inputs if path.is_file()}
(out/'source-binding.json').write_text(json.dumps(frozen_binding,indent=2)+'\n')
env.update(HFS_C6_BUILD=str(here/'build.py'),HFS_C6_SOURCE=str(source),GOFLAGS='-p=1 -overlay='+str(overlay))
pattern={'provider':'^TestControlledReceivedActualProviderProducer$','corruption':'^TestControlledReceivedActualSourceCorruption$','dirty':'^TestControlledReceivedActual(FundedDirty|RetainedResultPressure)$','gap':'^TestControlledReceivedActual(ProtectedGapQuery|GapLostReplies)$','mixed':'^TestControlledReceivedActualMixedIssuerFloors$','effected':'^TestControlledReceivedActualEffectedGapRefusal$','readdir':'^TestControlledReceivedActualClosingReaddir$','interleaving':'^TestControlledReceivedActual(RetirementInterleaving|LateBorrowRetirement)$','allocate':'^TestControlledReceivedActualAllocateRetryProfile$','terminal':'^TestControlledReceivedActualTerminal','terminal-handoff':'^TestControlledReceivedActualTerminalHandoffIdempotent$','terminal-smoke':'^TestControlledReceivedActualTerminalClean$','terminal-clean':'^TestControlledReceivedActualTerminal(Clean|DomainAlias|ExactRetry)$','terminal-dirty':'^TestControlledReceivedActualTerminalDirtyActor$','terminal-owners':'^TestControlledReceivedActualTerminal(ReservationOwner|HandoffIdempotent)$','terminal-backend':'^TestControlledReceivedActualTerminalBackend80$','terminal-kernel80':'^TestControlledReceivedActualTerminalKernel80$','terminal-exhaustion':'^TestControlledReceivedActualTerminalPermanentExhaustion$','both':'^TestControlledReceivedActual'}[a.scenario]
if a.receipt_tests:
    run('view-floor-policy',['go','test','-p','1','-run','^Test(MappedViewFloorBinding|ProtectedGapQueryEncoding)$','-count=1','./internal/guestbridge'])
    run('release-query-continuation',['go','test','-race','-p','1','-run','^TestPhysicalReleaseQueryContinuation$','-count=1','./internal/viewcontrol'])
run('actual-c6-host',['go','test','-race','-overlay',str(overlay),'-p','1','-run',pattern,'-count=1','-timeout=180s','-v','./internal/controlledruntime'])
if a.final_focus:
    run('old-producer-fixtures',['go','test','-race','-p','1','-run','^TestRealControlledProducer','-count=1','-timeout=180s','./internal/controlledruntime'])
    run('vet',['go','vet','./internal/controlledruntime'])
    run('errcheck',['errcheck','./internal/controlledruntime'])
    run('lint',['golangci-lint','run','./internal/controlledruntime'])
print((out/'actual-c6-host.stdout').read_text())
