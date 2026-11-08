#!/usr/bin/env python3
"""Stage pinned FreeBSD test artifacts and run one bounded native stage.

Targets: 'host' (f3 itself, via doas, Rocky pinned) or 'hfs-testN' (disposable
guest reached through f3 with the repository's pinned guest host keys). Never
starts/stops VMs, mounts, installs or touches production paths. The remote
run root is a fresh 0700 directory under /var/tmp and is preserved afterwards.
"""
import argparse
import base64
import hashlib
import json
import shlex
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = Path('/home/paul/git/homelabfs')
BASE = ['ssh', '-T', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
        '-o', 'ForwardAgent=no', '-o', 'ConnectTimeout=10',
        '-o', 'ServerAliveInterval=5', '-o', 'ServerAliveCountMax=2']
GUESTS = {'hfs-test0': '172.31.253.11', 'hfs-test1': '172.31.253.12', 'hfs-test2': '172.31.253.13'}


def ssh(target):
    if target == 'host':
        return BASE + ['-o', 'CheckHostIP=no', 'f3'], '/usr/local/bin/doas -n '
    return BASE + ['-o', 'UserKnownHostsFile=' + str(REPO / 'docs/lab/f3-known_hosts'),
                   '-J', 'paul@f3', 'root@' + GUESTS[target]], ''


def remote_python(prefix, code, seconds):
    encoded = 'import base64;exec(base64.b64decode(' + repr(base64.b64encode(code.encode()).decode()) + '))'
    return prefix + '/usr/bin/timeout -k 2 ' + str(seconds) + ' /usr/local/bin/python3.12 -B -c ' + shlex.quote(encoded)


STAGE = '''import base64,hashlib,json,os,stat,sys
from pathlib import Path
R=Path(ROOT)
assert os.geteuid()==0 and R.parent==Path('/var/tmp') and R.name.startswith('hfs-2026')
os.umask(0o077)
payload=json.load(sys.stdin)
R.mkdir(mode=0o700)
for name,item in payload.items():
 raw=base64.b64decode(item['data'],validate=True)
 assert hashlib.sha256(raw).hexdigest()==item['sha256']
 with (R/name).open('xb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
 (R/name).chmod(0o400 if name.endswith('.py') else 0o500)
 st=(R/name).lstat();assert st.st_nlink==1 and st.st_uid==0 and stat.S_ISREG(st.st_mode)
(R/'staged-bindings.json').write_text(json.dumps({n:v['sha256'] for n,v in payload.items()},indent=2))
print(json.dumps({'root':str(R),'bindings':{n:v['sha256'] for n,v in payload.items()}}))
'''

COLLECT = '''import hashlib,json
from pathlib import Path
R=Path(ROOT);out={}
for name in ['checks/STAGE/receipt.json','checks/STAGE/stdout','checks/STAGE/stderr']:
 p=R/name
 if p.exists():raw=p.read_bytes()[:4<<20];out[name]={'sha256':hashlib.sha256(raw).hexdigest(),'text':raw.decode(errors='replace')}
for name,digest in json.loads((R/'staged-bindings.json').read_text()).items():assert hashlib.sha256((R/name).read_bytes()).hexdigest()==digest,name
print(json.dumps(out))
'''


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--target', required=True)
    p.add_argument('--root', required=True, help='/var/tmp/hfs-2026...')
    p.add_argument('--stage', required=True)
    p.add_argument('--artifacts', required=True, help='local directory with built FreeBSD artifacts')
    p.add_argument('--guard-args', default='', help='profile arguments for native_guard.py')
    p.add_argument('--out', required=True, help='local evidence directory')
    p.add_argument('--outer-seconds', type=int, default=110)
    p.add_argument('--skip-stage', action='store_true', help='reuse an already staged root')
    p.add_argument('test_argv', nargs=argparse.REMAINDER)
    a = p.parse_args()
    out = Path(a.out); out.mkdir(parents=True, exist_ok=True)
    argv, prefix = ssh(a.target)
    art = Path(a.artifacts)
    files = {'diagnostic-child': art / 'diagnostic-child',
             'controlledruntime-freebsd.test': art / 'controlledruntime-freebsd.test',
             'native_guard.py': HERE / 'native_guard.py', 'runner_process.py': HERE / 'runner_process.py'}
    if not a.skip_stage:
        payload = {n: {'data': base64.b64encode(f.read_bytes()).decode(),
                       'sha256': hashlib.sha256(f.read_bytes()).hexdigest()} for n, f in files.items()}
        r = subprocess.run(argv + [remote_python(prefix, STAGE.replace('ROOT', repr(a.root)), 110)],
                           input=json.dumps(payload), capture_output=True, text=True, timeout=130)
        (out / 'staging.json').write_text(json.dumps({'exit': r.returncode, 'stdout': r.stdout, 'stderr': r.stderr}, indent=2))
        if r.returncode:
            print(r.stderr, file=sys.stderr); return r.returncode
    test = [x for x in a.test_argv if x != '--']
    command = (['/usr/local/bin/python3.12', '-B', a.root + '/native_guard.py', '--root', a.root,
                '--stage', a.stage, '--local-reserved-bytes', str(256 << 20)] + shlex.split(a.guard_args) +
               ['/usr/bin/env', 'HFS_DIAGNOSTIC_CHILD=' + a.root + '/diagnostic-child',
                a.root + '/controlledruntime-freebsd.test'] + test)
    r = subprocess.run(argv + [prefix + '/usr/bin/timeout -k 2 ' + str(a.outer_seconds) + ' ' + shlex.join(command)],
                       capture_output=True, text=True, timeout=a.outer_seconds + 20)
    (out / 'execution.json').write_text(json.dumps({'command': command, 'exit': r.returncode,
                                                    'stdout': r.stdout, 'stderr': r.stderr}, indent=2))
    code = COLLECT.replace('ROOT', repr(a.root)).replace('STAGE', a.stage)
    q = subprocess.run(argv + [remote_python(prefix, code, 30)], capture_output=True, text=True, timeout=50)
    (out / 'collection.json').write_text(json.dumps({'exit': q.returncode, 'stdout': q.stdout, 'stderr': q.stderr}, indent=2))
    if q.returncode == 0:
        for name, item in json.loads(q.stdout).items():
            (out / name.split('/')[-1]).write_text(item['text'])
    print(json.dumps({'execution_exit': r.returncode, 'collection_exit': q.returncode, 'guard': r.stdout.strip()[-2000:]}))
    return r.returncode or q.returncode


if __name__ == '__main__':
    sys.exit(main())
