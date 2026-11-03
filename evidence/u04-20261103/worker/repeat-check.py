import os, pathlib, subprocess, time, json, signal
root=pathlib.Path('/home/paul/tmp/codex/hfs-u04-20261103-jf4pucqm')
tmp=root/'tmp';tmp.mkdir(exist_ok=True)
env=dict(os.environ,GOMAXPROCS='2',GOMEMLIMIT='1GiB',TMPDIR=str(tmp))
def counters():
 d={}
 for x in pathlib.Path('/proc/vmstat').read_text().splitlines():
  k,v=x.split();d[k]=int(v)
 return d
initial=counters();records=[]
commands=[('lockfixed-repeat',['go','test','-race','-p','1','./internal/guestbridge','-run','^TestDurableTransportProcessLoss$','-count=100','-timeout=180s'])]
for name,args in commands:
 start=time.monotonic();peak=0
 with (root/(name+'.log')).open('w') as out:
  child=subprocess.Popen(args,cwd=root/'worktree',env=env,stdout=out,stderr=subprocess.STDOUT,start_new_session=True)
  while child.poll() is None:
   parents={};rss={}
   for entry in pathlib.Path('/proc').iterdir():
    if not entry.name.isdigit():continue
    try:
     stat=(entry/'stat').read_text().rsplit(')',1)[1].split();parents[int(entry.name)]=int(stat[1]);rss[int(entry.name)]=int(stat[21])*os.sysconf('SC_PAGE_SIZE')
    except (OSError,ValueError,IndexError):continue
   owned={child.pid}
   for unused in range(8):
    added={p for p,pp in parents.items() if pp in owned};owned|=added
   total=sum(rss.get(p,0) for p in owned);peak=max(total,peak)
   mem={x.split(':')[0]:int(x.split()[1])*1024 for x in pathlib.Path('/proc/meminfo').read_text().splitlines() if x.split(':')[0] in ('MemAvailable','MemTotal','SwapTotal','SwapFree')}
   why=None
   if total>2*(1<<30):why='owned RSS limit'
   if time.monotonic()-start>1200:why='stage deadline'
   if mem['MemAvailable']<max(8*(1<<30),mem['MemTotal']//4):why='available memory floor'
   if mem['SwapTotal']-mem['SwapFree']>mem['SwapTotal']*.55:why='swap ceiling'
   if counters()['pswpout']>initial['pswpout']:why='ongoing swapout'
   if why:
    os.killpg(child.pid,signal.SIGTERM);child.wait();raise RuntimeError(why)
   time.sleep(.25)
 records.append(dict(stage=name,exit=child.returncode,seconds=round(time.monotonic()-start,3),sampled_peak_rss=peak))
 (root/'repeat-metrics.json').write_text(json.dumps(records,indent=2)+'\n')
 print(json.dumps(records[-1]),flush=True)
 if child.returncode:break
