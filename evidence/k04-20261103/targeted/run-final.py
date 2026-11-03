import hashlib,json,os,pathlib,subprocess,time
root=pathlib.Path('/home/paul/tmp/codex/hfs-k04-20261103/worktree')
out=pathlib.Path('/home/paul/tmp/codex/hfs-k04-20261103/checks')
def listing(args):return subprocess.check_output(args,cwd=root,text=True).splitlines()
paths=sorted(set(listing(['git','diff','--name-only'])+listing(['git','ls-files','--others','--exclude-standard'])))
def hashes():return {p:hashlib.sha256((root/p).read_bytes()).hexdigest() for p in paths}
before=hashes();(out/'final-source-sha256.json').write_text(json.dumps(before,indent=2)+'\n');(out/'scoped-files.txt').write_text('\n'.join(paths)+'\n')
packages=['./internal/viewcontrol','./internal/controlledvalidation','./internal/fsservice','./internal/guestbridge']
commands=[('race',['go','test','-race','-coverprofile='+str(out/'frozen-coverage.out'),'./internal/viewcontrol','./internal/controlledvalidation','./internal/guestbridge','-count=1','-timeout=120s']),('fsservice-focused',['go','test','./internal/fsservice','-run','TestControlled|TestDelegated','-count=1','-timeout=90s']),('vet',['go','vet']+packages),('errcheck',['errcheck']+packages),('lint',['golangci-lint','run']+packages+['--timeout=120s'])]
env=dict(os.environ,GOMAXPROCS='2',GOMEMLIMIT='1GiB',GOFLAGS='-p=1')
receipts=[]
for name,args in commands:
 start=time.monotonic()
 with (out/(name+'.log')).open('w') as log:
  try:completed=subprocess.run(args,cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=150);code=completed.returncode
  except subprocess.TimeoutExpired:code='TIMEOUT'
 receipt={'name':name,'command':args,'exit':code,'seconds':round(time.monotonic()-start,3)};receipts.append(receipt)
 print(json.dumps(receipt),flush=True)
after=hashes();report={'base':listing(['git','rev-parse','HEAD'])[0],'environment':{k:env[k] for k in ('GOMAXPROCS','GOMEMLIMIT','GOFLAGS')},'checks':receipts,'source_changes':[p for p in paths if before[p]!=after[p]]}
(out/'frozen-checks.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'source_changes':report['source_changes'],'paths':len(paths)}),flush=True)
raise SystemExit(0 if all(r['exit']==0 for r in receipts) and not report['source_changes'] else 1)
