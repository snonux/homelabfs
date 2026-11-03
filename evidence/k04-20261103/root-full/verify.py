import hashlib,json,os,signal,subprocess,time
from pathlib import Path
r=Path(__file__).parent;root=Path('/home/paul/tmp/codex/hfs-k04-20261103/worktree');plan=json.loads((r/'plan.json').read_text())
env=dict(os.environ,TMPDIR=plan['tmp'],GOMAXPROCS='2',GOMEMLIMIT='1GiB',GOFLAGS='-p=1',HOMELABFS_FUSE_INTEGRATION='0')
meta=subprocess.run(['go','list','-deps','-test','-f','{{if not .Standard}}{{.Dir}}{{end}}',*plan['packages']],cwd=root,env=env,text=True,capture_output=True)
(r/'metadata.stdout').write_text(meta.stdout);(r/'metadata.stderr').write_text(meta.stderr)
if meta.returncode:raise SystemExit(meta.returncode)
paths=['go.mod','go.sum']
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

for item in plan['commands']:
 name=item['name'];argv=[v.replace('@EVIDENCE@',str(r)) for v in item['argv']];start=time.monotonic();expired=False
 with (r/(name+'.stdout')).open('wb') as out,(r/(name+'.stderr')).open('wb') as err:
  p=subprocess.Popen(argv,cwd=root,env=env|item.get('env',{}),stdout=out,stderr=err,start_new_session=True)
  (r/'pid').write_text(str(p.pid)+'\n')
  try:code=p.wait(timeout=item.get('timeout',300))
  except subprocess.TimeoutExpired:
   expired=True;os.killpg(p.pid,signal.SIGTERM)
   try:p.wait(timeout=5)
   except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);p.wait()
   code=p.returncode
 row=dict(check=name,argv=argv,exit=code,deadline=expired,seconds=round(time.monotonic()-start,3));rows.append(row)
 (r/'receipt.json').write_text(json.dumps(rows,indent=2)+'\n');print(json.dumps(row),flush=True)
 if code or expired:raise SystemExit(1)
changed=[p for p,h in binding.items() if hashlib.sha256((root/p).read_bytes()).hexdigest()!=h]
(r/'source-changes.json').write_text(json.dumps(changed)+'\n')
if changed:raise SystemExit('Source changed during check')
