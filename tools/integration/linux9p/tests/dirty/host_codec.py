#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Check a production C freeze vector through actual host codec, via Go overlay."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import time


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, required=True)
    parser.add_argument('--vectors', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--mode', choices=['freeze','bounds'], default='freeze')
    args = parser.parse_args()
    repo, vectors, output = (p.resolve() for p in (args.repo, args.vectors, args.output))
    output.mkdir(mode=0o700, exist_ok=False)
    here = Path(__file__).resolve().parent
    driver = output / 'codec_test.go'
    driver_source = here / 'host_codec_test.go.txt' if args.mode == 'freeze' else here.parent / 'leases/host_codec_test.go.txt'
    driver.write_bytes(driver_source.read_bytes())
    test = 'TestVZ3ActualCFreezeCodec' if args.mode == 'freeze' else 'TestVZ3MaximumViewUnions'
    overlay = output / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(repo / 'internal/guestbridge/vz3_c_freeze_test.go'): str(driver)}}))
    command = ['go', 'test', '-overlay', str(overlay), '-p', '1', '-run',
               '^'+test+'$', '-count=1', '-timeout=60s', './internal/guestbridge']
    env = dict(os.environ, GOMAXPROCS='2', GOMEMLIMIT='256MiB',
               HFS_C_FREEZE_COMMAND=str(vectors / 'c-command.json'),
               HFS_C_FREEZE_REPLY=str(vectors / 'reply.json'), HFS_VIEW_BOUNDS=str(vectors / 'unions.json'))
    cpus = sorted(os.sched_getaffinity(0))[:2]
    os.sched_setaffinity(0, cpus)
    sources = {str(p.relative_to(repo)): digest(p) for p in sorted((repo / 'internal').rglob('*.go'))}
    def limits():
        resource.setrlimit(resource.RLIMIT_CPU, (300, 300))
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    receipt = {'argv': command, 'source': sources, 'affinity': cpus,
               'vectors': {n: digest(vectors / n) for n in (['c-command.json', 'reply.json', 'binding.json'] if args.mode == 'freeze' else ['unions.json','sizes.json'])},
               'driver_sha256': digest(driver), 'script_sha256': digest(Path(__file__)),
               'scope': ('actual C freeze vector' if args.mode == 'freeze' else 'maximum closed resource unions') + ' through production host codec; no provider or runtime proof'}
    started = last = time.monotonic()
    peak = gap = 0
    reason = None
    with (output / 'codec.log').open('wb') as log:
        process = subprocess.Popen(command, cwd=repo, env=env, stdout=log, stderr=subprocess.STDOUT,
                                   start_new_session=True, preexec_fn=limits)
        while process.poll() is None:
            now = time.monotonic()
            gap, last = max(gap, now-last), now
            rss = 0
            for entry in Path('/proc').iterdir():
                if not entry.name.isdecimal():
                    continue
                try:
                    fields = (entry / 'stat').read_text().rsplit(')', 1)[1].split()
                    if int(fields[2]) == process.pid:
                        rss += int(fields[21]) * os.sysconf('SC_PAGE_SIZE')
                except (OSError, ValueError, IndexError):
                    pass
            peak = max(peak, rss)
            if rss > 1024**3 or now-started > 300:
                reason = 'sampled group RSS or wall time bound'
                os.killpg(process.pid, signal.SIGTERM)
                break
            time.sleep(.05)
        code = process.wait(timeout=5)
    receipt.update(exit=code, seconds=time.monotonic()-started,
                   sampled_peak_group_rss_bytes=peak, max_sample_gap_seconds=max(gap,time.monotonic()-last),
                   limit_reason=reason, source_unchanged=all(digest(repo/n)==h for n,h in sources.items()))
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')
    print((output / 'codec.log').read_text())
    print(json.dumps({k:v for k,v in receipt.items() if k not in ('source','argv')},indent=2))
    raise SystemExit(code or int(reason is not None))


if __name__ == '__main__':
    main()
