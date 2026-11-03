#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Bounded upstream object compilation only; never install, mount or boot."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import shutil
import signal
import subprocess
import time

HERE = Path(__file__).resolve().parent
MEMORY_MAX = 2 * 1024 ** 3
DISK_MAX = 8 * 1024 ** 3


def sha(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for data in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(data)
    return value.hexdigest()


def group_rss(group):
    total = 0
    for process in Path('/proc').iterdir():
        if not process.name.isdigit():
            continue
        try:
            text = (process / 'stat').read_text()
            fields = text[text.rfind(')') + 2:].split()
            if int(fields[2]) == group:
                total += int(fields[21]) * os.sysconf('SC_PAGE_SIZE')
        except (OSError, ValueError, IndexError):
            pass
    return total


def tree_bytes(root):
    return root.lstat().st_blocks * 512 + sum(path.lstat().st_blocks * 512
                                                for path in root.rglob('*'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--budget-root', type=Path, required=True)
    parser.add_argument('--module-objects', action='store_true')
    parser.add_argument('--mm-objects', action='store_true')
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    output = args.output.absolute()
    budget = args.budget_root.resolve(strict=True)
    if not source.is_relative_to(budget) or not output.is_relative_to(budget):
        raise SystemExit('refuse: source/output must be inside the allocated budget root')
    if source == output or source in output.parents or output in source.parents:
        raise SystemExit('refuse: source and output must be disjoint')
    if output.exists():
        raise SystemExit('refuse: output must be fresh; failed builds remain for inspection')
    for program in ('make', 'gcc', 'as', 'ld', 'bison', 'flex', 'bc', 'openssl', 'pkg-config', 'perl'):
        if not shutil.which(program):
            raise SystemExit(f'refuse: missing build prerequisite: {program}')
    if tree_bytes(budget) >= DISK_MAX or shutil.disk_usage(budget).free < 2 * 1024 ** 3:
        raise SystemExit('refuse: disk budget/preflight failed')
    # Verification precedes every build; no incidental vendor checkout is used.
    subprocess.run(['python3', str(HERE / 'prepare.py'), '--destination', str(source), '--verify'],
                   check=True, timeout=300)
    output.mkdir(mode=0o700)
    compiler_paths = {'gcc': Path(shutil.which('gcc')).resolve(),
                      'cc1': Path(subprocess.check_output(['gcc', '-print-prog-name=cc1'], text=True).strip()).resolve(),
                      'as': Path(shutil.which('as')).resolve(),
                      'ld': Path(shutil.which('ld')).resolve()}
    compiler_bindings = {name: {'path': str(path), 'sha256': sha(path)}
                         for name, path in compiler_paths.items()}
    (output / 'inputs.json').write_text(json.dumps({
        'compiler_files': compiler_bindings,
        'source_inventory_sha256': sha(source / '.homelabfs-owned-source.json'),
        'manifest_sha256': sha(HERE / 'source.json'),
        'builder_sha256': sha(Path(__file__)),
        'disk_measure': 'sum owned entry st_blocks*512, excluding filesystem metadata',
        'unfinished_mmap_development': json.loads((HERE / 'source.json').read_text()).get('unfinished_mmap_development', False),
    }, indent=2, sort_keys=True) + '\n')
    cpus = sorted(os.sched_getaffinity(0))[:2]
    stages = []
    environment = dict(os.environ)
    environment.update(KBUILD_BUILD_TIMESTAMP='2026-11-02 00:00:00 UTC',
                       KBUILD_BUILD_USER='homelabfs', KBUILD_BUILD_HOST='owned-builder',
                       KBUILD_BUILD_VERSION='1', CC='gcc', HOSTCC='gcc',
                       KCFLAGS=f'-fdebug-prefix-map={source}=/usr/src/linux '
                               f'-fdebug-prefix-map={output}=/usr/src/linux-build '
                               f'-fmacro-prefix-map={source}=/usr/src/linux')

    def limits():
        os.sched_setaffinity(0, cpus)
        # Per-process AS limit is additional; aggregate RSS is checked below.
        resource.setrlimit(resource.RLIMIT_AS, (MEMORY_MAX, MEMORY_MAX))
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))

    def run(argv, name):
        started = time.monotonic()
        peak = 0
        max_gap = 0
        last_sample = started
        reason = None
        next_disk = started
        with (output / (name + '.log')).open('wb') as log:
            process = subprocess.Popen(argv, cwd=source, stdout=log,
                                       stderr=subprocess.STDOUT, env=environment,
                                       start_new_session=True, preexec_fn=limits)
            while process.poll() is None:
                now = time.monotonic()
                max_gap = max(max_gap, now - last_sample)
                last_sample = now
                peak = max(peak, group_rss(process.pid))
                if peak > MEMORY_MAX:
                    reason = 'aggregate process-group RSS exceeds 2GiB'
                if now - started > 1200:
                    reason = 'stage exceeds 20 minutes'
                if now >= next_disk:
                    next_disk = now + 5
                    if tree_bytes(budget) > DISK_MAX:
                        reason = 'allocated root exceeds 8GiB'
                if reason:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                    break
                time.sleep(0.25)
            code = process.wait()
        max_gap = max(max_gap, time.monotonic() - last_sample)
        record = {'name': name, 'argv': argv, 'exit': code,
                  'seconds': round(time.monotonic() - started, 3),
                  'peak_group_rss_bytes': peak, 'limit_reason': reason,
                  'max_rss_sample_gap_seconds': round(max_gap, 3)}
        stages.append(record)
        (output / 'stages.json').write_text(json.dumps(stages, indent=2) + '\n')
        print(json.dumps(record), flush=True)
        if code or reason:
            raise SystemExit(f'failed {name}; retained evidence at {output}')

    make = ['make', '-j2', f'O={output}', 'ARCH=x86_64']
    run(make + ['x86_64_defconfig'], 'defconfig')
    config = [str(source / 'scripts/config'), '--file', str(output / '.config')]
    state = '--module' if args.module_objects else '--enable'
    config += [state, 'NET_9P', state, 'NET_9P_VIRTIO', state, '9P_FS',
               '--enable', '9P_FS_HOMELABFS', '--enable', 'CRYPTO_LIB_SHA256',
               '--disable', '9P_FSCACHE', '--disable', '9P_FS_POSIX_ACL']
    # huge_memory.c is normally selected only under this symbol. Explicitly
    # requesting its object with THP disabled compiles against stub headers.
    if args.mm_objects:
        config += ['--enable', 'TRANSPARENT_HUGEPAGE', '--enable', 'SHMEM',
                   '--enable', 'READ_ONLY_THP_FOR_FS']
    run(config, 'config')
    run(make + ['olddefconfig'], 'olddefconfig')
    if 'CONFIG_9P_FS_HOMELABFS=y\n' not in (output / '.config').read_text():
        raise SystemExit('refuse: selected guest code was not enabled')
    mm_config = {}
    if args.mm_objects:
        for symbol in ('TRANSPARENT_HUGEPAGE', 'SHMEM', 'READ_ONLY_THP_FOR_FS'):
            if 'CONFIG_' + symbol + '=y\n' not in (output / '.config').read_text():
                raise SystemExit('refuse: MM observer check requires CONFIG_' + symbol + '=y')
            mm_config[symbol] = 'y'
    targets = ['fs/9p/', 'net/9p/']
    if args.mm_objects:
        targets += ['mm/memory.o', 'mm/gup.o', 'mm/mremap.o', 'mm/huge_memory.o']
    (output / 'config-binding.json').write_text(json.dumps({'config_sha256': sha(output / '.config'), 'targets': targets, 'mm_check_symbols': mm_config}, indent=2) + '\n')
    run(make + targets, 'objects')
    artifacts = {str(path.relative_to(output)): sha(path)
                 for part in ('fs/9p', 'net/9p')
                 for path in (output / part).glob('*.o')}
    if args.mm_objects:
        for name in ('memory.o', 'gup.o', 'mremap.o', 'huge_memory.o'):
            artifacts['mm/' + name] = sha(output / 'mm' / name)
    receipt = {
        'development_unfinished_mmap': json.loads((HERE / 'source.json').read_text()).get('unfinished_mmap_development', False),
        'source_inventory_sha256': sha(source / '.homelabfs-owned-source.json'),
        'compiler_files': compiler_bindings,
        'scope': 'upstream kernel object compilation; no module link, installed ELRepo ABI or runtime proof',
        'manifest_sha256': sha(HERE / 'source.json'), 'config_sha256': sha(output / '.config'),
        'artifacts': artifacts, 'cpus': cpus, 'rss_nominal_poll_ms': 250, 'disk_poll_seconds': 5,
        'memory_limit_bytes': MEMORY_MAX, 'disk_limit_bytes': DISK_MAX,
        'limits_note': 'RSS/disk polling records observed bounds; disk walks can delay RSS sampling; actual maximum gaps are recorded per stage; transient overshoot is possible',
        'environment': {key: environment[key] for key in environment
                        if key.startswith('KBUILD_') or key in ('CC', 'HOSTCC', 'KCFLAGS')},
        'tools': {tool: subprocess.check_output([tool, '--version'], text=True).splitlines()[0]
                  for tool in ('gcc', 'make', 'ld', 'bison', 'flex')},
        'stages': stages,
    }
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    print(f'object checks complete: {output / "receipt.json"}')


if __name__ == '__main__':
    main()
