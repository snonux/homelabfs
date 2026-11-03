import hashlib,json,os,signal,subprocess,time
from pathlib import Path
r=Path(__file__).parent;root=Path('/home/paul/git/homelabfs');plan=json.loads((r/'plan.json').read_text())
env=dict(os.environ,TMPDIR=plan['tmp'],GOMAXPROCS='2',GOMEMLIMIT='1GiB',GOFLAGS='-p=1',HOMELABFS_FUSE_INTEGRATION='0')
env.update(plan.get('environment',{}))
os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
meta=subprocess.run(['go','list','-deps','-test','-f','{{if not .Standard}}{{.Dir}}{{end}}',*plan['packages']],cwd=root,env=env,text=True,capture_output=True)
(r/'metadata.stdout').write_text(meta.stdout);(r/'metadata.stderr').write_text(meta.stderr)
if meta.returncode:raise SystemExit(meta.returncode)
paths=['go.mod','go.sum','internal/controlledruntime/probe/installed.py', 'internal/controlledvalidation/o04_production_guest_test.go', 'tools/integration/linux9p/README.md', 'tools/integration/linux9p/source.json', 'tools/integration/linux9p/patches/0003-hfs1-guest-vfs.patch', 'tools/integration/linux9p/tests/controlled/build.py', 'tools/integration/linux9p/tests/controlled/consumer.c', 'tools/integration/linux9p/tests/controlled/host_test.go.txt', 'tools/integration/linux9p/tests/controlled/run.py']
for raw in meta.stdout.splitlines():
 d=Path(raw)
 if raw and d.is_relative_to(root):paths.extend(str(p.relative_to(root)) for p in d.glob('*.go'))
binding={p:hashlib.sha256((root/p).read_bytes()).hexdigest() for p in sorted(set(paths))}
(r/'source-binding.json').write_text(json.dumps(binding,indent=2)+'\n');rows=[]

def interrupted(signum, frame):
 child=globals().get('p'); code=None
 if child is not None:
  code=child.poll()
  if code is None:
   os.killpg(child.pid,signal.SIGTERM)
   try: code=child.wait(timeout=5)
   except subprocess.TimeoutExpired: os.killpg(child.pid,signal.SIGKILL);code=child.wait()
 (r/'external-interruption.json').write_text(json.dumps(dict(signal=signum,child_exit=code,check=globals().get('name')),indent=2)+'\n')
 raise SystemExit(128+signum)
signal.signal(signal.SIGTERM,interrupted)

def resource():
 m={line.split(':')[0]:int(line.split()[1])*1024 for line in Path('/proc/meminfo').read_text().splitlines()}
 out=int(next(x.split()[1] for x in Path('/proc/vmstat').read_text().splitlines() if x.startswith('pswpout ')))
 if m['MemAvailable']<max(8<<30,m['MemTotal']/4) or m['SwapTotal']-m['SwapFree']>m['SwapTotal']*.55:raise RuntimeError('resource floor')
 return out

for item in plan['commands']:
 name=item['name'];argv=[v.replace('@EVIDENCE@',str(r)) for v in item['argv']];start=time.monotonic();expired=False
 with (r/(name+'.stdout')).open('wb') as out,(r/(name+'.stderr')).open('wb') as err:
  before=resource();time.sleep(3)
  if resource()!=before:raise RuntimeError('prelaunch swapout changed')
  peak=0;guard_error=None
  p=subprocess.Popen(argv,cwd=root,env=env|item.get('env',{}),stdout=out,stderr=err,start_new_session=True)
  (r/'pid').write_text(str(p.pid)+'\n')
  try:
   while p.poll() is None:
    try:
     if resource()!=before:raise RuntimeError('swapout advanced')
     rss=0
     for entry in Path('/proc').iterdir():
      if not entry.name.isdigit():continue
      try:
       st=(entry/'stat').read_text().split()
       if int(st[5])==p.pid:rss+=next((int(x.split()[1]) for x in (entry/'status').read_text().splitlines() if x.startswith('VmRSS:')),0)
      except (OSError,ValueError,IndexError):pass
     peak=max(peak,rss)
     if rss>2*1024*1024:raise RuntimeError('group RSS over 2GiB')
    except RuntimeError as e:
     guard_error=str(e);os.killpg(p.pid,signal.SIGTERM);break
    if time.monotonic()-start>item.get('timeout',300):raise subprocess.TimeoutExpired(argv,item.get('timeout',300))
    time.sleep(.25)
   code=p.wait(timeout=5)
  except subprocess.TimeoutExpired:
   expired=True;os.killpg(p.pid,signal.SIGTERM)
   try:p.wait(timeout=5)
   except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);p.wait()
   code=p.returncode
 row=dict(check=name,argv=argv,exit=code,deadline=expired,seconds=round(time.monotonic()-start,3),peak_group_rss_kib=peak,guard=guard_error,pswpout_before=before,pswpout_after=resource());rows.append(row)
 (r/'receipt.json').write_text(json.dumps(rows,indent=2)+'\n');print(json.dumps(row),flush=True)
 if code or expired or guard_error:raise SystemExit(1)
changed=[p for p,h in binding.items() if hashlib.sha256((root/p).read_bytes()).hexdigest()!=h]
(r/'source-changes.json').write_text(json.dumps(changed)+'\n')
if changed:raise SystemExit('Source changed during check')
