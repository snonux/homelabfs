#!/usr/local/bin/python3.12
# SPDX-License-Identifier: GPL-2.0-only
"""Bounded native test guard (f3 host or disposable hfs-test guest profile).

Derived on 2026-11-08 from the reviewed S24 guard; the host profile now takes
the current Rocky PID because the migration reboot made PID 6826 historical.

Original: Reviewed unprivileged f3 build guard. Never starts/stops a VM or installs.

The coordinator first creates one fresh0700 private root and transfers this
file there. Every stage receives a conservative reservation for ALL local disk
owners, held fixed while this sole native heavy job runs. New local heavy jobs
or unbudgeted output transfers are forbidden until the receipt is collected.
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
    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / 'linux9p/tests/common'))
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
    return free_pages(3 * GIB + 512 * MIB)


def guest_invariant(hostname):
    """Disposable hfs-test guest profile: no nested VMs, pinned guest name."""
    actual = subprocess.check_output(['/bin/hostname'], text=True, timeout=5).strip()
    if not hostname.startswith('hfs-test') or actual.split('.')[0] != hostname:
        raise ValueError('guest identity changed')
    if Path('/dev/vmm').exists() and any(Path('/dev/vmm').iterdir()):
        raise ValueError('unexpected nested VM context')
    return free_pages(512 * MIB)


def free_pages(floor):
    values = subprocess.check_output(['/sbin/sysctl', '-n', 'hw.pagesize',
        'vm.stats.vm.v_free_count', 'vm.stats.vm.v_inactive_count'],
        text=True, timeout=5).split()
    page, free, inactive = map(int, values)
    if page != 4096 or free * page < floor:
        raise ValueError('current physical free-page reserve below floor')
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
    size = allocated(root)
    if size > limit:
        raise ValueError("native fixture root exceeds admitted128MiB")
    return size


def main():
    os.environ.update(LC_ALL='C', TZ='UTC')
    time.tzset()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--stage', required=True)
    parser.add_argument('--local-reserved-bytes', type=int, required=True)
    parser.add_argument('--profile', choices=('host', 'guest'), required=True)
    parser.add_argument('--rocky-pid', type=int)
    parser.add_argument('--rocky-birth')
    parser.add_argument('--rocky-config-sha256')
    parser.add_argument('--guest-hostname')
    parser.add_argument('--max-seconds', type=int, default=90, choices=range(1, 601))
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    root = args.root.absolute()
    info = root.lstat()
    if root.resolve(strict=True) != root or root.parent != Path('/var/tmp') or not root.name.startswith('hfs-2026') or not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError('fresh private native build root required')
    if not args.stage.replace('-', '').isalnum() or not args.command or not 0 < args.local_reserved_bytes < 12 * GIB:
        raise ValueError('stage, command and complete local disk reservation required')
    stage = root / 'checks' / args.stage
    stage.mkdir(mode=0o700, parents=True, exist_ok=False)
    environment = {key: os.environ[key] for key in ('HOME', 'USER', 'LOGNAME') if key in os.environ}
    environment.update(PATH='/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/bin',
                       TMPDIR=str(root / 'tmp'), LC_ALL='C', TZ='UTC',
                       PYTHONDONTWRITEBYTECODE='1', GOMAXPROCS='1', GOMEMLIMIT='128MiB')
    (root / 'tmp').mkdir(mode=0o700, exist_ok=True)
    # Keep the build and guard on exactly one CPU. No root permission is needed
    # to restrict this process and its inherited descendants.
    subprocess.run(['/bin/cpuset', '-l', '3' if args.profile == 'host' else '1', '-p', str(os.getpid())],
                   check=True, timeout=5)
    known, observations = {}, []
    peak_rss, peak_disk = 0, 0
    child, failure, code = None, None, None
    started = time.monotonic()

    def sample():
        nonlocal peak_rss, peak_disk
        if args.profile == 'host':
            physical = host_invariant(args.rocky_pid, args.rocky_birth, args.rocky_config_sha256)
        else:
            physical = guest_invariant(args.guest_hostname)
        rows = processes()
        owned = {os.getpid()}
        if child:
            owned.add(child.pid)
        owned.update(pid for pid, old in known.items()
                     if pid in rows and old[3] == rows[pid][3])
        while True:
            grown = owned | {pid for pid, row in rows.items() if row[0] in owned}
            if grown == owned:
                break
            owned = grown
        known.update({pid: rows[pid] for pid in owned if pid in rows})
        # Includes guard, compiler/make/linker and ps helper plus64MiB reserved
        # for this stage's SSH/shell parent and short-lived observation helpers.
        rss = sum(rows[pid][2] for pid in owned if pid in rows) + 64 * MIB
        build_output_bytes = check_build_outputs(root)
        disk = allocated(root) + args.local_reserved_bytes
        peak_rss, peak_disk = max(peak_rss, rss), max(peak_disk, disk)
        swap = subprocess.check_output(['/usr/sbin/swapinfo', '-k'],
                                       text=True, timeout=5)
        observations.append({'elapsed': time.monotonic() - started,
                             'rss': rss, 'global_allocated_disk': disk,
                             'build_output_bytes': build_output_bytes,
                             'physical_observation': physical,
                             'swap_telemetry': swap})
        if rss > 512 * MIB or disk > 12 * GIB:
            raise ValueError('native aggregate RSS or global disk bound')
        space = os.statvfs(root)
        if space.f_bavail * space.f_frsize < (8 * GIB if args.profile == 'host' else 2 * GIB):
            raise ValueError('native unreserved filesystem disk headroom')
        if time.monotonic() - started > args.max_seconds:
            raise ValueError('native stage time bound exceeded')

    def interrupted(signum, frame):
        raise RuntimeError('native guard interrupted: ' + str(signum))

    signal.signal(signal.SIGHUP, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    if not hasattr(os, 'waitid') or not hasattr(os, 'WNOWAIT'):
        raise ValueError('nonreaping waitid WNOWAIT capability required')
    try:
        sample()
        with (stage / 'stdout').open('xb') as output, (stage / 'stderr').open('xb') as errors:
            child = subprocess.Popen(args.command, cwd=root, env=environment,
                stdout=output, stderr=errors, start_new_session=True)
            while process_running(child):
                sample()
                time.sleep(0.25)
            sample()
    except BaseException as exc:
        failure = str(exc)
    finally:
        # The session leader remains unreaped until TERM, bounded grace and
        # unconditional group KILL complete. Its PID/PGID cannot be reused to
        # target any pre-existing process, including the running VM.
        if child is not None:
            code = finish_process_group(child)
        receipt = {'argv': args.command, 'local_reserved_bytes': args.local_reserved_bytes,
                   'error': failure, 'exit': code, 'peak_rss': peak_rss,
                   'peak_global_allocated_disk': peak_disk,
                   'seconds': time.monotonic() - started,
                   'observations': observations, 'installed': False,
                   'booted': False, 'native_accepted': False}
        (stage / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
        print(json.dumps({key: value for key, value in receipt.items() if key != 'observations'}))
    if failure or code:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
