#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Bundle exact selected native sources and their complete license notices."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import tarfile

MAX_SOURCE = 192 << 20
MAX_ARCHIVE = 64 << 20
EPOCH = 1793750400


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


class HashedReader:
    def __init__(self, stream):
        self.stream = stream
        self.digest = hashlib.sha256()

    def read(self, count):
        block = self.stream.read(count)
        self.digest.update(block)
        return block


def source_path(source, name):
    path = PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or str(path) != name:
        raise ValueError('native source path')
    target = source
    for part in path.parts:
        target /= part
        if target.is_symlink():
            raise ValueError('native source symlink')
    if not stat.S_ISREG(target.stat().st_mode):
        raise ValueError('native source regular file required')
    return target


def bundle(root, expected_inventory):
    index = root / 'source.inventory.json'
    if sha(index) != expected_inventory:
        raise ValueError('native source inventory pin mismatch')
    record = json.loads(index.read_text())
    source = root / 'source'
    if source.resolve(strict=True) != source:
        raise ValueError('native source root alias')
    target = root / 'source-bundle'
    target.mkdir(mode=0o700)
    archive = target / 'freebsd-selected-source.tar.gz'
    total = 0
    with archive.open('xb') as output:
        with gzip.GzipFile(filename='', mode='wb', fileobj=output, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w|', format=tarfile.PAX_FORMAT) as tar:
                for name, expected in sorted(record['files'].items()):
                    path = source_path(source, name)
                    info = path.stat()
                    total += info.st_size
                    if total > MAX_SOURCE:
                        raise ValueError('selected native source size bound')
                    header = tarfile.TarInfo(name)
                    header.size = info.st_size
                    header.mode = 0o755 if info.st_mode & 0o111 else 0o644
                    header.mtime = EPOCH
                    with path.open('rb') as stream:
                        reader = HashedReader(stream)
                        tar.addfile(header, reader)
                    if reader.digest.hexdigest() != expected:
                        raise ValueError('native source changed: ' + name)
                    if output.tell() > MAX_ARCHIVE:
                        raise ValueError('native source archive bound')
        output.flush()
        os.fsync(output.fileno())
    if archive.stat().st_size > MAX_ARCHIVE or sha(index) != expected_inventory:
        raise ValueError('native source bundle closure changed')
    shutil.copyfile(index, target / 'freebsd-source-inventory.json')
    # Full original source retains every per-file notice, alongside these two
    # complete upstream distribution notices. Binary sysroot libs are external.
    licenses = {}
    for original, name in [('COPYRIGHT', 'COPYRIGHT'),
                           ('contrib/lib9p/COPYRIGHT', 'lib9p-COPYRIGHT')]:
        path = source_path(source, original)
        shutil.copyfile(path, target / name)
        licenses[name] = sha(target / name)
        if licenses[name] != record['files'][original]:
            raise ValueError('native notice changed')
    result = {'version': 'homelabfs-native-source-bundle/1',
              'source_inventory_sha256': expected_inventory,
              'source_archive_sha256': record['archive_sha256'],
              'selected_archive_sha256': sha(archive),
              'selected_archive_bytes': archive.stat().st_size,
              'selected_source_bytes': total, 'source_files': len(record['files']),
              'licenses': licenses, 'per_file_notices': 'retained in complete selected sources',
              'binary_sysroot_libraries': 'external hashed inputs, not redistributed',
              'installed': False}
    with (target / 'bundle.json').open('x') as output:
        json.dump(result, output, sort_keys=True, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    print(json.dumps(result, sort_keys=True))


def verify(archive, expected_files):
    if archive.stat().st_size > MAX_ARCHIVE:
        raise ValueError('native source archive bound')
    found, total = set(), 0
    with tarfile.open(archive, 'r|gz') as tar:
        for member in tar:
            name = member.name
            path = PurePosixPath(name)
            if not member.isfile() or path.is_absolute() or '..' in path.parts or str(path) != name or name in found or name not in expected_files:
                raise ValueError('native source archive path/type/inventory mismatch')
            total += member.size
            if member.size < 0 or total > MAX_SOURCE:
                raise ValueError('native source archive expanded bound')
            h = hashlib.sha256()
            with tar.extractfile(member) as stream:
                for block in iter(lambda: stream.read(1 << 20), b''):
                    h.update(block)
            if h.hexdigest() != expected_files[name]:
                raise ValueError('native source archive content mismatch: ' + name)
            found.add(name)
            tar.members.clear()
    if found != set(expected_files):
        raise ValueError('incomplete native source archive')
    return total


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-inventory-sha256', required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    if root.parent != Path('/var/tmp') or not root.name.startswith('hfs-xz3-'):
        raise ValueError('private native artifact root required')
    bundle(root, args.source_inventory_sha256)


if __name__ == '__main__':
    main()
