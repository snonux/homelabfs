#!/usr/local/bin/python3.12
# SPDX-License-Identifier: BSD-2-Clause
"""Bounded original source capture and owned subset preparation; never compile."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tarfile
import urllib.parse
import urllib.request

REVISION = 'aadd58dddcbc78f4d5594827b46b5633552b15ce'
URL = 'https://codeload.github.com/freebsd/freebsd-src/tar.gz/' + REVISION
MAX_ARCHIVE = 512 << 20
MAX_SELECTED = 192 << 20
TREES = ('usr.sbin/bhyve/', 'contrib/lib9p/', 'lib/lib9p/', 'lib/libvmmapi/',
         'share/mk/', 'tools/build/', 'share/vt/keymaps/')
EXTRA = {'sys/conf/newvers.sh', 'sys/libkern/crc16.c', 'sys/cam/ctl/ctl_nvme_all.c',
         'sys/cam/ctl/ctl_scsi_all.c', 'sys/cam/ctl/ctl_util.c',
         'sys/amd64/vmm/vmm_instruction_emul.c', 'COPYRIGHT', 'Makefile.inc1', 'Makefile'}
BASE = {'lib/lib9p/Makefile': '27eb4e47734fe07faa3e8899cfd9b2a68ca4af715f5a22316c57f1ef9c2f6b13',
        'usr.sbin/bhyve/pci_virtio_9p.c': '2d0ba25704c2acc8c26befb6d40e9407184fe7e73d6700bb6045110fee214aa5'}
OWNED = ('hfs9p.c', 'hfs9p.h', 'hfs9p_json.c', 'hfs9p_json.h',
         'hfs9p_ledger.c', 'hfs9p_ledger.h', 'hfs9p_supervisor.c',
         'hfs9p_supervisor.h', 'hfs9p_recovery.inc', 'hfs9p_bootstrap.inc')


def sha(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for data in iter(lambda: stream.read(1 << 20), b''):
            value.update(data)
    return value.hexdigest()


def new_json(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def capture(root):
    archive = root / 'freebsd-pinned.tar.gz'
    partial = root / 'freebsd-pinned.tar.gz.partial'
    if archive.exists() or partial.exists():
        raise ValueError('fresh archive capture required; preserve failed original')
    count, digest = 0, hashlib.sha256()
    with urllib.request.urlopen(URL, timeout=30) as response, partial.open('xb') as output:
        if urllib.parse.urlparse(response.url).hostname != 'codeload.github.com':
            raise ValueError('unselected source redirect')
        headers = {key: response.headers.get(key) for key in
                   ('ETag', 'Last-Modified', 'Content-Length')}
        while data := response.read(1 << 20):
            count += len(data)
            if count > MAX_ARCHIVE:
                raise ValueError('original source archive exceeds512MiB')
            digest.update(data)
            output.write(data)
        output.flush()
        os.fsync(output.fileno())
    with tarfile.open(partial, 'r:gz') as tar:
        commit = tar.pax_headers.get('comment')
        if commit != REVISION:
            raise ValueError('original source PAX commit mismatch')
    partial.rename(archive)
    record = {'version': 'homelabfs-freebsd-source-capture/1', 'url': URL,
              'revision': REVISION, 'pax_commit': commit, 'bytes': count,
              'sha256': digest.hexdigest(), 'expected_sha256_before_capture': None,
              'trust': 'TOFU via exact pinned HTTPS URL; captured hash requires coordinator review before compile',
              'response_headers': headers, 'compiled': False, 'installed': False}
    new_json(root / 'archive.capture.json', record)
    print(json.dumps(record, sort_keys=True))


def selected(name):
    return name in EXTRA or name.startswith(TREES) or name.startswith('sys/') and name.endswith('.h')


def extract(root):
    archive = root / 'freebsd-pinned.tar.gz'
    capture = json.loads((root / 'archive.capture.json').read_text())
    if capture.get('version') != 'homelabfs-freebsd-source-capture/1' or capture.get('url') != URL or capture.get('pax_commit') != REVISION or capture.get('revision') != REVISION or not 0 < capture.get('bytes', 0) <= MAX_ARCHIVE or archive.stat().st_size != capture['bytes'] or sha(archive) != capture['sha256']:
        raise ValueError('changed captured original archive')
    source = root / 'source'
    if source.exists():
        raise ValueError('fresh selected source required')
    prefix = 'freebsd-src-' + REVISION + '/'
    planned_bytes, planned_files = 0, 0
    with tarfile.open(archive, 'r|gz') as tar:
        if tar.pax_headers.get('comment') != REVISION:
            raise ValueError('selected source PAX commit mismatch')
        for member in tar:
            if member.name.startswith(prefix):
                name = member.name[len(prefix):]
                if selected(name) and not member.isdir():
                    if not member.isfile() or member.size < 0:
                        raise ValueError('selected nonregular source dependency: ' + name)
                    planned_bytes += member.size
                    planned_files += 1
            tar.members.clear()
    new_json(root / 'source.selection.json', {'selected_bytes': planned_bytes,
        'selected_files': planned_files, 'ceiling': MAX_SELECTED,
        'within_ceiling': planned_bytes <= MAX_SELECTED})
    if planned_bytes > MAX_SELECTED:
        raise ValueError('source selection exceeds192MiB before extraction; preserve archive/selection and report')
    source.mkdir(mode=0o700)
    total, before, count = 0, {}, 0
    with tarfile.open(archive, 'r|gz') as tar:
        if tar.pax_headers.get('comment') != REVISION:
            raise ValueError('selected source PAX commit mismatch')
        for member in tar:
            if member.name == prefix[:-1] and member.isdir():
                continue
            if not member.name.startswith(prefix):
                raise ValueError('unexpected source archive root')
            name = member.name[len(prefix):]
            path = PurePosixPath(name)
            if not name or path.is_absolute() or '..' in path.parts or str(path) != name:
                raise ValueError('unsafe archive path')
            if selected(name) and not member.isdir():
                if not member.isfile():
                    raise ValueError('selected nonregular source dependency: ' + name)
                if member.size < 0 or total + member.size > MAX_SELECTED:
                    raise ValueError('selected source exceeds192MiB')
                target = source / name
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with tar.extractfile(member) as incoming, target.open('xb') as output:
                    copied = 0
                    while data := incoming.read(1 << 20):
                        copied += len(data)
                        if copied > member.size:
                            raise ValueError('archive member length changed')
                        output.write(data)
                    if copied != member.size:
                        raise ValueError('truncated source member')
                target.chmod(0o600 | (member.mode & 0o155))
                total += member.size
                count += 1
                before[name] = sha(target)
            tar.members.clear()
    if any(before.get(name) != value for name, value in BASE.items()):
        raise ValueError('known base source identity mismatch')
    seed = json.loads((root / 'inputs/seed.json').read_text())
    if set(seed) != {'files'} or set(seed['files']) != {'0001-dedicated-backend.patch'} | {'src/' + name for name in OWNED}:
        raise ValueError('closed owned source seed required')
    for name, expected in seed['files'].items():
        path = root / 'inputs' / name
        if path.is_symlink() or not path.is_file() or sha(path) != expected:
            raise ValueError('owned input mismatch: ' + name)
    patch = root / 'inputs/0001-dedicated-backend.patch'
    subprocess.run(['/usr/bin/patch', '-t', '-N', '-F', '0', '-p1', '-i', str(patch)],
                   cwd=source, check=True, timeout=30)
    destination = source / 'contrib/lib9p/backend'
    for name in OWNED:
        target = destination / name
        if target.exists():
            raise ValueError('unexpected upstream owned-backend filename')
        shutil.copyfile(root / 'inputs/src' / name, target)
    after = {str(path.relative_to(source)): sha(path)
             for path in sorted(source.rglob('*')) if path.is_file()}
    new_json(root / 'source.inventory.json', {
        'version': 'homelabfs-freebsd-owned-source/1', 'revision': REVISION,
        'archive_sha256': capture['sha256'], 'selected_original_bytes': total,
        'selected_original_files': count, 'original_files': before,
        'files': after, 'seed': seed, 'compiled': False, 'installed': False})
    print(json.dumps({'source_files': len(after), 'selected_original_bytes': total,
                      'source_inventory_sha256': sha(root / 'source.inventory.json'),
                      'compiled': False, 'installed': False}, sort_keys=True))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['capture', 'extract'])
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    if root.parent != Path('/var/tmp') or not root.name.startswith('hfs-xz3-'):
        raise ValueError('approved private native root required')
    if args.action == 'capture':
        capture(root)
    else:
        extract(root)


if __name__ == '__main__':
    main()
