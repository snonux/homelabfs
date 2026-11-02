#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Verify/fetch pinned inputs and apply the owned series to a NEW local tree."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import urllib.request

HERE = Path(__file__).resolve().parent
MAX_ARCHIVE = 256 * 1024 * 1024
MAX_UNPACKED = 3 * 1024 ** 3


def sha(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for data in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(data)
    return value.hexdigest()


def verify(source, manifest):
    state = json.loads((source / '.homelabfs-owned-source.json').read_text())
    if state['commit'] != manifest['commit'] or state['patches'] != manifest['patches']:
        raise SystemExit('refuse: source identity or patch series changed')
    for name, expected in state['files'].items():
        path = source / name
        if expected.startswith('link:'):
            if not path.is_symlink() or os.readlink(path) != expected[5:]:
                raise SystemExit(f'refuse: changed source symlink: {name}')
        elif not path.is_file() or path.is_symlink() or sha(path) != expected:
            raise SystemExit(f'refuse: changed source bytes: {name}')
    actual = {str(p.relative_to(source)) for p in source.rglob('*')
              if p.is_symlink() or p.is_file()}
    if actual != set(state['files']) | {'.homelabfs-owned-source.json'}:
        raise SystemExit('refuse: unexpected source files')
    for name, expected in manifest['patched_file_sha256'].items():
        if state['files'].get(name) != expected:
            raise SystemExit(f'refuse: changed maintained patch output: {name}')
    print(f'verified {len(state["files"])} source files/symlinks; no installed ABI claim')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--destination', type=Path, required=True)
    parser.add_argument('--archive', type=Path)
    parser.add_argument('--verify', action='store_true')
    args = parser.parse_args()
    manifest = json.loads((HERE / 'source.json').read_text())
    destination = args.destination.absolute()
    if args.verify:
        verify(destination.resolve(strict=True), manifest)
        return
    if destination.exists() or destination.is_symlink():
        raise SystemExit('refuse: destination must not exist; existing trees are never overwritten')
    destination.parent.mkdir(parents=True, exist_ok=True)
    if shutil.disk_usage(destination.parent).free < 4 * 1024 ** 3:
        raise SystemExit('refuse: less than 4GiB free before extraction/build preparation')
    for patch in manifest['patches']:
        if sha(HERE / 'patches' / patch['file']) != patch['sha256']:
            raise SystemExit('refuse: patch digest mismatch')
    archive = args.archive.absolute() if args.archive else destination.parent / 'linux-pinned.tar.gz'
    if not archive.exists():
        # A partial download remains evidence and is never treated as verified.
        with urllib.request.urlopen(manifest['archive_url'], timeout=60) as response, \
                archive.open('xb') as output:
            total = 0
            while data := response.read(1024 * 1024):
                total += len(data)
                if total > MAX_ARCHIVE:
                    raise SystemExit('refuse: archive exceeds 256MiB')
                output.write(data)
    if archive.stat().st_size != manifest['archive_bytes'] or sha(archive) != manifest['archive_sha256']:
        raise SystemExit('refuse: pinned source archive length/digest mismatch')
    destination.mkdir(mode=0o700)
    with tarfile.open(archive, 'r:gz') as tar:
        if tar.pax_headers.get('comment') != manifest['commit']:
            raise SystemExit('refuse: archive commit comment mismatch')
        prefix = f'linux-{manifest["commit"]}/'
        total = 0
        for member in tar:
            if member.name == prefix[:-1]:
                continue
            if not member.name.startswith(prefix):
                raise SystemExit('refuse: unexpected archive root')
            total += member.size
            if total > MAX_UNPACKED:
                raise SystemExit('refuse: unpacked source exceeds 3GiB')
            member.name = member.name[len(prefix):]
            if not member.name:
                continue
            tar.extract(member, destination, filter='data')
    for patch in manifest['patches']:
        subprocess.run(['patch', '--batch', '--forward', '--fuzz=0', '-p1', '-i',
                        str(HERE / 'patches' / patch['file'])],
                       cwd=destination, check=True, timeout=60)
    files = {}
    for path in sorted(destination.rglob('*')):
        name = str(path.relative_to(destination))
        if path.is_symlink():
            files[name] = 'link:' + os.readlink(path)
        elif path.is_file():
            files[name] = sha(path)
    for name, expected in manifest['patched_file_sha256'].items():
        if files.get(name) != expected:
            raise SystemExit(f'refuse: patch output mismatch: {name}')
    state = {'commit': manifest['commit'], 'patches': manifest['patches'], 'files': files}
    (destination / '.homelabfs-owned-source.json').write_text(json.dumps(state, sort_keys=True) + '\n')
    verify(destination, manifest)


if __name__ == '__main__':
    main()
