#!/usr/local/bin/python3.12
# SPDX-License-Identifier: GPL-2.0-only
"""Bounded native test guard (f3 host or disposable hfs-test guest profile).

Promoted on 2026-11-08 (task l34) from the evidence copy
docs/lab/results/20261108-native-fd-qualification/tools/native_guard.py, which
was derived from the reviewed S24 guard. The bounds and profiles are unchanged.
The promotion splits the former single main function, adds --cwd so a staged
source tree (see native-test-drive.py --source-archive) can serve the test
binary's cwd-relative repository inputs, records that cwd in the receipt, and
updates the runner_process fallback import path for the tools/lab/ location.
The elapsed-time clock now starts after the cpuset call (no practical effect).

Never starts/stops a VM, mounts or installs. The coordinator first creates one
fresh 0700 private root and transfers this file there. Every stage receives a
conservative reservation for ALL local disk owners, held fixed while this sole
native heavy job runs. New local heavy jobs or unbudgeted output transfers are
forbidden until the receipt is collected.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import time

try:
    from runner_process import process_running, finish_process_group
except ModuleNotFoundError:
    # Repository checkout: the reviewed helper lives with the linux9p runners.
    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / 'integration/linux9p/tests/common'))
    from runner_process import process_running, finish_process_group

GIB = 1 << 30
MIB = 1 << 20


def processes():
    raw = subprocess.check_output(['/bin/ps', '-ax', '-o', 'pid=',
        '-o', 'ppid=', '-o', 'pgid=', '-o', 'rss=', '-o', 'lstart='],
        text=True, timeout=5)
    result = {}
    for line in raw.splitlines():
        words = line.split()
        if len(words) != 9:
            raise ValueError('native process observation shape')
        pid, parent, group, rss = map(int, words[:4])
        result[pid] = (parent, group, rss * 1024, tuple(words[4:]))
    return result


def host_invariant(rocky_pid, birth, config_hash):
    """f3 host profile: only the pinned current Rocky incarnation may run."""
    if {p.name for p in Path('/dev/vmm').iterdir()} != {'rocky'}:
        raise ValueError('VM context inventory changed')
    pid = str(rocky_pid)
    actual = subprocess.check_output(['/bin/ps', '-p', pid, '-o',
                                      'pid=', '-o', 'command='], text=True, timeout=5).strip()
    if actual.split(maxsplit=1) != [pid, 'bhyve: rocky (bhyve)']:
        raise ValueError('pinned Rocky process changed')
    actual_birth = subprocess.check_output(['/bin/ps', '-p', pid, '-o',
                                           'lstart='], text=True, timeout=5).strip()
    if actual_birth != birth:
        raise ValueError('pinned Rocky incarnation changed')
    config = Path('/zroot/bhyve/rocky/rocky.conf').read_bytes()
    if len(config) > 65536 or hashlib.sha256(config).hexdigest() != config_hash or [line for line in config.decode().splitlines() if line.startswith('memory=')] != ['memory=12G']:
        raise ValueError('configured12GiB VM allocation changed')
    # Host profile (zq3-r2): Rocky's guest pages count as Active on f3, so free
    # pages alone fell to 2.87 GB with 2.6 GB inactive while the host had ample
    # reclaimable memory. Keep a hard 1 GiB free floor and require 3.5 GiB of
    # free plus inactive (reclaimable) pages, the same accounting the zq3 Phase
    # 6 bar uses. The test itself stays bounded to 512 MiB RSS.
    return free_pages(1 * GIB, reclaimable_floor=3 * GIB + 512 * MIB)


def guest_invariant(hostname):
    """Disposable hfs-test guest profile: no nested VMs, pinned guest name."""
    actual = subprocess.check_output(['/bin/hostname'], text=True, timeout=5).strip()
    if not hostname.startswith('hfs-test') or actual.split('.')[0] != hostname:
        raise ValueError('guest identity changed')
    if Path('/dev/vmm').exists() and any(Path('/dev/vmm').iterdir()):
        raise ValueError('unexpected nested VM context')
    return free_pages(512 * MIB)


def free_pages(floor, reclaimable_floor=None):
    """Check the free-page floor and, if given, the free+inactive floor.

    Inactive pages are reclaimable without swapping out active memory, so the
    optional second floor accepts them; the first floor always counts only
    free pages.
    """
    values = subprocess.check_output(['/sbin/sysctl', '-n', 'hw.pagesize',
        'vm.stats.vm.v_free_count', 'vm.stats.vm.v_inactive_count'],
        text=True, timeout=5).split()
    page, free, inactive = map(int, values)
    if page != 4096 or free * page < floor:
        raise ValueError('current physical free-page reserve below floor')
    if reclaimable_floor is not None and (free + inactive) * page < reclaimable_floor:
        raise ValueError('current free+inactive reserve below floor')
    return {'page_size': page, 'free_pages': free, 'inactive_pages': inactive,
            'physical_free_bytes': page * free,
            'free_plus_inactive_bytes': page * (free + inactive)}


def allocated(root):
    total = 0
    for directory, directories, files in os.walk(root, followlinks=False):
        total += Path(directory).lstat().st_blocks * 512
        for name in files:
            total += (Path(directory) / name).lstat().st_blocks * 512
        # os.walk omits symlink-directory bodies but they still own dirent blocks.
        for name in directories:
            path = Path(directory) / name
            if path.is_symlink():
                total += path.lstat().st_blocks * 512
    return total


def check_build_outputs(root, limit=128 * MIB):
    # The staged source tree lives under the root, so it counts against this
    # admitted bound together with the binaries and test temporaries.
    size = allocated(root)
    if size > limit:
        raise ValueError("native fixture root exceeds admitted128MiB")
    return size


def parse_args(argv=None):
    # No abbreviations: the driver refuses --guard-args that repeat its own
    # options by exact name, and a prefix such as --cw must not alias --cwd.
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--stage', required=True)
    parser.add_argument('--local-reserved-bytes', type=int, required=True)
    parser.add_argument('--profile', choices=('host', 'guest'), required=True)
    parser.add_argument('--rocky-pid', type=int)
    parser.add_argument('--rocky-birth')
    parser.add_argument('--rocky-config-sha256')
    parser.add_argument('--guest-hostname')
    parser.add_argument('--max-seconds', type=int, default=90, choices=range(1, 601))
    parser.add_argument('--cwd', type=Path,
                        help='absolute child working directory strictly inside --root '
                             '(default: the root itself)')
    parser.add_argument('command', nargs=argparse.REMAINDER)
    return parser.parse_args(argv)


def check_root(root):
    """Return the absolute run root after proving it is fresh, private and ours."""
    root = root.absolute()
    info = root.lstat()
    if root.resolve(strict=True) != root or root.parent != Path('/var/tmp') or not root.name.startswith('hfs-2026') or not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError('fresh private native build root required')
    return root


def check_request(args):
    if not args.stage.replace('-', '').isalnum() or not args.command or not 0 < args.local_reserved_bytes < 12 * GIB:
        raise ValueError('stage, command and complete local disk reservation required')


def resolve_cwd(root, cwd):
    """Return the child's working directory: the root or a real directory below it.

    Go test binaries read repository inputs relative to their package directory
    (../../tools/integration/lib9p, testdata/, process.go), so a staged source
    tree must become the cwd. The path must already be canonical (no symlink or
    '..' component, which resolve() would change), owned by this user and
    strictly inside the root, so every relative child effect stays run-owned.
    """
    if cwd is None:
        return root
    if not cwd.is_absolute():
        raise ValueError('child working directory must be absolute')
    try:
        resolved = cwd.resolve(strict=True)
        info = resolved.lstat()
    except OSError as exc:
        raise ValueError('child working directory missing: ' + str(exc)) from exc
    if resolved != cwd or root not in resolved.parents or not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid():
        raise ValueError('child working directory must be an owned real directory inside the run root')
    return resolved


def child_environment(root):
    environment = {key: os.environ[key] for key in ('HOME', 'USER', 'LOGNAME') if key in os.environ}
    environment.update(PATH='/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/bin',
                       TMPDIR=str(root / 'tmp'), LC_ALL='C', TZ='UTC',
                       PYTHONDONTWRITEBYTECODE='1', GOMAXPROCS='1', GOMEMLIMIT='128MiB')
    (root / 'tmp').mkdir(mode=0o700, exist_ok=True)
    return environment


class Sampler:
    """Owned-process, memory, disk and time accounting for one guarded stage."""

    def __init__(self, args, root):
        self.args, self.root = args, root
        self.known, self.observations = {}, []
        self.peak_rss, self.peak_disk = 0, 0
        self.child = None
        self.started = time.monotonic()

    def physical(self):
        if self.args.profile == 'host':
            return host_invariant(self.args.rocky_pid, self.args.rocky_birth, self.args.rocky_config_sha256)
        return guest_invariant(self.args.guest_hostname)

    def owned_rss(self):
        """Sum RSS of the guard's descendants, tracking recognised incarnations."""
        rows = processes()
        owned = {os.getpid()}
        if self.child:
            owned.add(self.child.pid)
        owned.update(pid for pid, old in self.known.items()
                     if pid in rows and old[3] == rows[pid][3])
        while True:
            grown = owned | {pid for pid, row in rows.items() if row[0] in owned}
            if grown == owned:
                break
            owned = grown
        self.known.update({pid: rows[pid] for pid in owned if pid in rows})
        # Includes guard, compiler/make/linker and ps helper plus 64MiB reserved
        # for this stage's SSH/shell parent and short-lived observation helpers.
        return sum(rows[pid][2] for pid in owned if pid in rows) + 64 * MIB

    def sample(self):
        physical = self.physical()
        rss = self.owned_rss()
        build_output_bytes = check_build_outputs(self.root)
        disk = allocated(self.root) + self.args.local_reserved_bytes
        self.peak_rss, self.peak_disk = max(self.peak_rss, rss), max(self.peak_disk, disk)
        swap = subprocess.check_output(['/usr/sbin/swapinfo', '-k'], text=True, timeout=5)
        self.observations.append({'elapsed': time.monotonic() - self.started,
                                  'rss': rss, 'global_allocated_disk': disk,
                                  'build_output_bytes': build_output_bytes,
                                  'physical_observation': physical,
                                  'swap_telemetry': swap})
        if rss > 512 * MIB or disk > 12 * GIB:
            raise ValueError('native aggregate RSS or global disk bound')
        space = os.statvfs(self.root)
        if space.f_bavail * space.f_frsize < (8 * GIB if self.args.profile == 'host' else 2 * GIB):
            raise ValueError('native unreserved filesystem disk headroom')
        if time.monotonic() - self.started > self.args.max_seconds:
            raise ValueError('native stage time bound exceeded')


def interrupted(signum, frame):
    raise RuntimeError('native guard interrupted: ' + str(signum))


def run_child(args, sampler, stage, cwd, environment):
    """Start the guarded command in its own session and sample until it exits."""
    sampler.sample()
    with (stage / 'stdout').open('xb') as output, (stage / 'stderr').open('xb') as errors:
        sampler.child = subprocess.Popen(args.command, cwd=cwd, env=environment,
            stdout=output, stderr=errors, start_new_session=True)
        while process_running(sampler.child):
            sampler.sample()
            time.sleep(0.25)
        sampler.sample()


def write_receipt(stage, args, sampler, cwd, failure, code):
    receipt = {'argv': args.command, 'cwd': str(cwd),
               'local_reserved_bytes': args.local_reserved_bytes,
               'error': failure, 'exit': code, 'peak_rss': sampler.peak_rss,
               'peak_global_allocated_disk': sampler.peak_disk,
               'seconds': time.monotonic() - sampler.started,
               'observations': sampler.observations, 'installed': False,
               'booted': False, 'native_accepted': False}
    (stage / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps({key: value for key, value in receipt.items() if key != 'observations'}))


def main(argv=None):
    os.environ.update(LC_ALL='C', TZ='UTC')
    time.tzset()
    args = parse_args(argv)
    root = check_root(args.root)
    check_request(args)
    # Refuse a bad working directory before creating any stage effect.
    cwd = resolve_cwd(root, args.cwd)
    stage = root / 'checks' / args.stage
    stage.mkdir(mode=0o700, parents=True, exist_ok=False)
    environment = child_environment(root)
    # Keep the build and guard on exactly one CPU. No root permission is needed
    # to restrict this process and its inherited descendants.
    subprocess.run(['/bin/cpuset', '-l', '3' if args.profile == 'host' else '1', '-p', str(os.getpid())],
                   check=True, timeout=5)
    sampler = Sampler(args, root)
    signal.signal(signal.SIGHUP, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    if not hasattr(os, 'waitid') or not hasattr(os, 'WNOWAIT'):
        raise ValueError('nonreaping waitid WNOWAIT capability required')
    failure, code = None, None
    try:
        run_child(args, sampler, stage, cwd, environment)
    except BaseException as exc:
        failure = str(exc)
    finally:
        # The session leader remains unreaped until TERM, bounded grace and
        # unconditional group KILL complete. Its PID/PGID cannot be reused to
        # target any pre-existing process, including the running VM.
        if sampler.child is not None:
            code = finish_process_group(sampler.child)
        write_receipt(stage, args, sampler, cwd, failure, code)
    if failure or code:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
