#!/usr/bin/env python3
"""Stage pinned FreeBSD test artifacts and run one bounded native stage.

Promoted on 2026-11-08 (task l34) from the evidence copy
docs/lab/results/20261108-native-fd-qualification/tools/drive.py. Additions:
--source-archive stages a bounded repository tarball (normally
`git archive --format=tar HEAD`) as <root>/src, and --cwd runs the test binary
from a directory inside it, so the package's cwd-relative inputs
(../../tools/integration/lib9p/*.c, testdata/*, process.go) resolve natively.

Targets: 'host' (f3 itself, via doas, Rocky pinned) or 'hfs-testN' (disposable
guest reached through f3 with the repository's pinned guest host keys). Never
starts/stops VMs, mounts, installs or touches production paths. The remote
run root is a fresh 0700 directory under /var/tmp and is preserved afterwards.
"""
import argparse
import base64
import hashlib
import inspect
import io
import json
from pathlib import Path, PurePosixPath
import re
import shlex
import subprocess
import sys
import tarfile

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
RUNNER_PROCESS = REPO / 'tools/integration/linux9p/tests/common/runner_process.py'
REMOTE_PYTHON = '/usr/local/bin/python3.12'
BASE = ['ssh', '-T', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
        '-o', 'ForwardAgent=no', '-o', 'ConnectTimeout=10',
        '-o', 'ServerAliveInterval=5', '-o', 'ServerAliveCountMax=2']
GUESTS = {'hfs-test0': '172.31.253.11', 'hfs-test1': '172.31.253.12', 'hfs-test2': '172.31.253.13'}
# Lower case only: the remote programs are filled in by replacing upper-case
# placeholders (ROOT, SOURCE, STAGE, MAX_*), so an upper-case run root could be
# re-matched by a later replacement and silently stage somewhere else.
ROOT_PATTERN = re.compile(r'/var/tmp/hfs-2026[a-z0-9-]*')
STAGE_PATTERN = re.compile(r'[A-Za-z0-9-]+')
PATH_PART = re.compile(r'[A-Za-z0-9._-]+')
ARTIFACTS = ('diagnostic-child', 'controlledruntime-freebsd.test')
# The guard admits at most 128 MiB below the run root (binaries, extracted
# source and test temporaries together); the archive is held well below that.
MAX_ARCHIVE_BYTES = 64 << 20
MAX_ARCHIVE_MEMBERS = 16384
SOURCE_DIRECTORY = 'src'
# Options this driver owns; repeating them in --guard-args would silently
# override the validated values because argparse keeps the last occurrence.
# The guard parser disables abbreviations, so exact names suffice here.
RESERVED_GUARD_OPTIONS = ('--root', '--stage', '--local-reserved-bytes', '--cwd')


def check_archive_members(archive, max_bytes, max_members):
    """Refuse anything but a bounded tree of plain files and directories.

    Returns the commit id `git archive` records in the pax global header (or
    None). This function is self-contained because its source is also sent to
    the remote stage program, which re-checks the transferred bytes before
    extraction; both sides therefore run the same reviewed rule.
    """
    from pathlib import PurePosixPath
    total, count = 0, 0
    for member in archive:
        count += 1
        name = PurePosixPath(member.name)
        if count > max_members:
            raise ValueError('source archive has too many members')
        if name.is_absolute() or not name.parts or '..' in name.parts:
            raise ValueError('source archive member path: ' + member.name)
        if not (member.isreg() or member.isdir()):
            raise ValueError('source archive member type: ' + member.name)
        total += member.size
        if total > max_bytes:
            raise ValueError('source archive content exceeds bound')
    return archive.pax_headers.get('comment')


def read_source_archive(path):
    """Read and validate a local source tarball; return (bytes, commit id)."""
    if not path.is_file() or path.stat().st_size > MAX_ARCHIVE_BYTES:
        raise ValueError('source archive must be a regular file of at most 64 MiB')
    raw = path.read_bytes()
    try:
        with tarfile.open(fileobj=io.BytesIO(raw), mode='r:') as archive:
            commit = check_archive_members(archive, MAX_ARCHIVE_BYTES, MAX_ARCHIVE_MEMBERS)
    except tarfile.TarError as exc:
        raise ValueError('source archive is not an uncompressed tar: ' + str(exc)) from exc
    return raw, commit


def check_cwd(cwd):
    """Validate --cwd as a canonical relative path below the staged source."""
    path = PurePosixPath(cwd)
    if path.is_absolute() or str(path) != cwd or not all(PATH_PART.fullmatch(p) and p not in ('.', '..') for p in path.parts):
        raise ValueError('--cwd must be a canonical relative path below the run root')
    if path.parts[0] != SOURCE_DIRECTORY:
        raise ValueError('--cwd must lie inside the staged source directory ' + SOURCE_DIRECTORY)
    return cwd


def check_args(a):
    """Reject unsafe or inconsistent options before any SSH or staging effect."""
    if not ROOT_PATTERN.fullmatch(a.root):
        raise ValueError('--root must be /var/tmp/hfs-2026...')
    if not STAGE_PATTERN.fullmatch(a.stage):
        raise ValueError('--stage must be alphanumeric with dashes')
    if not 1 <= a.outer_seconds <= 900:
        raise ValueError('--outer-seconds must be within 1..900')
    if any(word.split('=')[0] in RESERVED_GUARD_OPTIONS for word in shlex.split(a.guard_args)):
        raise ValueError('--guard-args must not repeat driver-owned guard options')
    if a.cwd is not None:
        check_cwd(a.cwd)
        if a.source_archive is None and not a.skip_stage:
            raise ValueError('--cwd needs --source-archive (or --skip-stage reusing a staged source)')
    if a.source_archive is not None and a.skip_stage:
        raise ValueError('--source-archive cannot be combined with --skip-stage')
    missing = [name for name in ARTIFACTS if not (Path(a.artifacts) / name).is_file()]
    if missing and not a.skip_stage:
        raise ValueError('missing artifacts: ' + ', '.join(missing))


def parse_args(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('--target', required=True, choices=['host', *GUESTS])
    p.add_argument('--root', required=True, help='/var/tmp/hfs-2026...')
    p.add_argument('--stage', required=True)
    p.add_argument('--artifacts', required=True, help='local directory with built FreeBSD artifacts')
    p.add_argument('--guard-args', default='', help='profile arguments for native-test-guard.py')
    p.add_argument('--out', required=True, help='local evidence directory')
    p.add_argument('--outer-seconds', type=int, default=110)
    p.add_argument('--skip-stage', action='store_true', help='reuse an already staged root')
    p.add_argument('--source-archive', type=Path,
                   help='uncompressed tar (git archive --format=tar HEAD) extracted to <root>/src')
    p.add_argument('--cwd', help='test working directory relative to the root, e.g. src/internal/controlledruntime')
    p.add_argument('test_argv', nargs=argparse.REMAINDER)
    a = p.parse_args(argv)
    try:
        check_args(a)
    except ValueError as exc:
        p.error(str(exc))
    return a


def ssh(target):
    if target == 'host':
        return BASE + ['-o', 'CheckHostIP=no', 'f3'], '/usr/local/bin/doas -n '
    return BASE + ['-o', 'UserKnownHostsFile=' + str(REPO / 'docs/lab/f3-known_hosts'),
                   '-J', 'paul@f3', 'root@' + GUESTS[target]], ''


def remote_python(prefix, code, seconds):
    encoded = 'import base64;exec(base64.b64decode(' + repr(base64.b64encode(code.encode()).decode()) + '))'
    return prefix + '/usr/bin/timeout -k 2 ' + str(seconds) + ' ' + REMOTE_PYTHON + ' -B -c ' + shlex.quote(encoded)


STAGE = '''import base64,hashlib,io,json,os,stat,sys,tarfile
from pathlib import Path
R=Path(ROOT)
assert os.geteuid()==0 and R.parent==Path('/var/tmp') and R.name.startswith('hfs-2026')
os.umask(0o077)
payload=json.load(sys.stdin)
R.mkdir(mode=0o700)
files=payload['files']
for name,item in files.items():
 raw=base64.b64decode(item['data'],validate=True)
 assert hashlib.sha256(raw).hexdigest()==item['sha256']
 with (R/name).open('xb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
 (R/name).chmod(0o400 if name.endswith('.py') else 0o500)
 st=(R/name).lstat();assert st.st_nlink==1 and st.st_uid==0 and stat.S_ISREG(st.st_mode)
bindings={n:v['sha256'] for n,v in files.items()}
(R/'staged-bindings.json').write_text(json.dumps(bindings,indent=2))
result={'root':str(R),'bindings':bindings}
source=payload.get('source')
if source:
 raw=base64.b64decode(source['data'],validate=True)
 assert hashlib.sha256(raw).hexdigest()==source['sha256']
 target=R/SOURCE;target.mkdir(mode=0o700)
 with tarfile.open(fileobj=io.BytesIO(raw),mode='r:') as t:
  commit=check_archive_members(t,MAX_BYTES,MAX_MEMBERS)
  t.extractall(target,filter='data')
 result['source']={'sha256':source['sha256'],'commit':commit,'directory':str(target)}
 (R/'staged-source.json').write_text(json.dumps(result['source'],indent=2))
print(json.dumps(result))
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


def stage_program(root):
    """Remote staging code: the shared member check followed by STAGE.

    The 'data' extraction filter (Python 3.12) is defence in depth; the
    explicit member check already refuses links, devices and path escapes.
    """
    substituted = (STAGE.replace('ROOT', repr(root)).replace('SOURCE', repr(SOURCE_DIRECTORY))
                   .replace('MAX_BYTES', str(MAX_ARCHIVE_BYTES)).replace('MAX_MEMBERS', str(MAX_ARCHIVE_MEMBERS)))
    return inspect.getsource(check_archive_members) + substituted


def staging_payload(a):
    """Build the JSON staging payload: pinned files plus the optional source tree."""
    art = Path(a.artifacts)
    files = {name: art / name for name in ARTIFACTS}
    files.update({'native-test-guard.py': HERE / 'native-test-guard.py', 'runner_process.py': RUNNER_PROCESS})
    payload = {'files': {}}
    for name, path in files.items():
        raw = path.read_bytes()
        payload['files'][name] = {'data': base64.b64encode(raw).decode(), 'sha256': hashlib.sha256(raw).hexdigest()}
    if a.source_archive is not None:
        raw, commit = read_source_archive(a.source_archive)
        payload['source'] = {'data': base64.b64encode(raw).decode(), 'sha256': hashlib.sha256(raw).hexdigest(),
                             'commit': commit}
    return payload


def guard_command(a):
    """Remote argv: the guard (with an optional source cwd) wrapping the test binary."""
    command = [REMOTE_PYTHON, '-B', a.root + '/native-test-guard.py', '--root', a.root,
               '--stage', a.stage, '--local-reserved-bytes', str(256 << 20)]
    if a.cwd is not None:
        command += ['--cwd', a.root + '/' + a.cwd]
    test = [x for x in a.test_argv if x != '--']
    return (command + shlex.split(a.guard_args) +
            ['/usr/bin/env', 'HFS_DIAGNOSTIC_CHILD=' + a.root + '/diagnostic-child',
             a.root + '/controlledruntime-freebsd.test'] + test)


def stage(a, argv, prefix, out):
    payload = staging_payload(a)
    r = subprocess.run(argv + [remote_python(prefix, stage_program(a.root), 110)],
                       input=json.dumps(payload), capture_output=True, text=True, timeout=130)
    (out / 'staging.json').write_text(json.dumps({'exit': r.returncode, 'stdout': r.stdout, 'stderr': r.stderr,
                                                  'source_commit': payload.get('source', {}).get('commit')}, indent=2))
    if r.returncode:
        print(r.stderr, file=sys.stderr)
    return r.returncode


def execute(a, argv, prefix, out):
    command = guard_command(a)
    r = subprocess.run(argv + [prefix + '/usr/bin/timeout -k 2 ' + str(a.outer_seconds) + ' ' + shlex.join(command)],
                       capture_output=True, text=True, timeout=a.outer_seconds + 20)
    (out / 'execution.json').write_text(json.dumps({'command': command, 'exit': r.returncode,
                                                    'stdout': r.stdout, 'stderr': r.stderr}, indent=2))
    return r


def collect(a, argv, prefix, out):
    code = COLLECT.replace('ROOT', repr(a.root)).replace('STAGE', a.stage)
    q = subprocess.run(argv + [remote_python(prefix, code, 30)], capture_output=True, text=True, timeout=50)
    (out / 'collection.json').write_text(json.dumps({'exit': q.returncode, 'stdout': q.stdout, 'stderr': q.stderr}, indent=2))
    if q.returncode == 0:
        for name, item in json.loads(q.stdout).items():
            (out / name.split('/')[-1]).write_text(item['text'])
    return q.returncode


def main(argv=None):
    a = parse_args(argv)
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    remote, prefix = ssh(a.target)
    if not a.skip_stage:
        code = stage(a, remote, prefix, out)
        if code:
            return code
    r = execute(a, remote, prefix, out)
    collected = collect(a, remote, prefix, out)
    print(json.dumps({'execution_exit': r.returncode, 'collection_exit': collected, 'guard': r.stdout.strip()[-2000:]}))
    return r.returncode or collected


if __name__ == '__main__':
    sys.exit(main())
