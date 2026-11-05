#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Seal/check a closed disposable build package; never install or admit runtime."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat

VERSION = 'homelabfs-controlled-artifacts/1'
PROTOCOL = {'BodySchema': 5, 'BridgeMajor': 1, 'BridgeMinor': 4,
            'Features': 32767, 'Msize': 65536, 'NormalMajor': 3,
            'NormalMinor': 0}
POLICY = {'profile': 'CONTROLLED_LOCAL', 'publication': None,
          'physical_contract': 'homelabfs-stop-adoption/1', 'page_size': 4096,
          'runtime_config': 'homelabfs-controlled-runtime/1',
          'view_enrollment': 'homelabfs-private-viewcontrol/1',
          'transport_checkpoint': 1, 'session_limit': 16,
          'topology': 'one-vm-boot-process-endpoint',
          'recovery': 'same-surviving-owner-current-cut-only',
          'replacement_resume': 'BLOCKED', 'logical_bytes': 4 << 20,
          'guest_ceiling_bytes': 2 << 30}
KINDS = {'source', 'patch', 'license', 'config', 'toolchain', 'sysroot',
         'recipe', 'build-record', 'kernel', 'module', 'exporter', 'daemon',
         'runtime-library', 'proposal'}
REQUIRED = {'source', 'patch', 'license', 'config', 'toolchain', 'sysroot',
            'recipe', 'build-record', 'kernel', 'exporter', 'daemon', 'proposal'}
LIMIT = 8 << 30
HASH = re.compile(r'[0-9a-f]{64}\Z')


def closed(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError('closed manifest shape')


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON key')
        result[key] = value
    return result


def read_json(path, limit=16 << 20):
    with path.open('rb') as stream:
        raw = stream.read(limit + 1)
    if len(raw) > limit:
        raise ValueError('JSON size limit')
    return json.loads(raw.decode('utf-8'), object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(
                          ValueError('nonfinite JSON number')))


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'),
                       ensure_ascii=True, allow_nan=False) + '\n').encode()


def file_path(root, name):
    if not isinstance(name, str) or not name or '\\' in name:
        raise ValueError('package path')
    path = PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or str(path) != name:
        raise ValueError('package path')
    actual = root
    for part in path.parts:
        actual = actual / part
        mode = actual.lstat().st_mode
        if stat.S_ISLNK(mode):
            raise ValueError('package symlink')
    if not stat.S_ISREG(actual.stat().st_mode):
        raise ValueError('package regular file required')
    return actual


def digest(path):
    total, value = 0, hashlib.sha256()
    with path.open('rb') as stream:
        for data in iter(lambda: stream.read(1 << 20), b''):
            total += len(data)
            if total > LIMIT:
                raise ValueError('artifact size limit')
            value.update(data)
    return total, value.hexdigest()


def validate_metadata(manifest):
    closed(manifest, ['version', 'protocol', 'policy', 'operator_public',
                      'source_revisions', 'files', 'evidence'])
    if manifest['version'] != VERSION or canonical(manifest['protocol']) != canonical(PROTOCOL) or canonical(manifest['policy']) != canonical(POLICY):
        raise ValueError('unsupported version, protocol or policy')
    # Public identity pins bytes only. This package cannot create consent or
    # assign deployed authority, and contains no operator private key.
    key = manifest['operator_public']
    if not isinstance(key, str) or not HASH.fullmatch(key) or int(key, 16) == 0:
        raise ValueError('operator public identity')
    revisions = manifest['source_revisions']
    closed(revisions, ['repository', 'linux', 'freebsd'])
    for revision in revisions.values():
        if not isinstance(revision, str) or not re.fullmatch('[0-9a-f]{40}', revision):
            raise ValueError('exact source revision required')
    if revisions['linux'] != '79807e970724c1f90cee1316361d61b80db4eef6' or revisions['freebsd'] != 'aadd58dddcbc78f4d5594827b46b5633552b15ce':
        raise ValueError('unselected upstream source')
    if canonical(manifest['evidence']) != canonical({'built': True, 'installed': False, 'booted': False,
                                'native_accepted': False, 'publication': None}):
        raise ValueError('build package cannot certify runtime')
    files = manifest['files']
    if not isinstance(files, dict) or not 1 <= len(files) <= 65536:
        raise ValueError('file inventory bound')
    kinds, total = set(), 0
    for name, item in files.items():
        closed(item, ['kind', 'bytes', 'sha256'])
        if item['kind'] not in KINDS or type(item['bytes']) is not int or not 0 < item['bytes'] <= LIMIT or not isinstance(item['sha256'], str) or not HASH.fullmatch(item['sha256']):
            raise ValueError('file inventory entry')
        kinds.add(item['kind'])
        total += item['bytes']
    if total > LIMIT or not REQUIRED <= kinds:
        raise ValueError('incomplete artifact closure or size bound')


def check(root, expected_hash, expected_operator, expected_repository):
    root = root.resolve(strict=True)
    manifest_path = file_path(root, 'manifest.json')
    if not HASH.fullmatch(expected_hash) or digest(manifest_path)[1] != expected_hash:
        raise ValueError('externally pinned manifest mismatch')
    manifest = read_json(manifest_path)
    validate_metadata(manifest)
    if manifest['operator_public'] != expected_operator or manifest['source_revisions']['repository'] != expected_repository:
        raise ValueError('unexpected operator or repository source')
    found = set()
    for path in root.rglob('*'):
        mode = path.lstat().st_mode
        if stat.S_ISDIR(mode):
            continue
        if not stat.S_ISREG(mode):
            raise ValueError('package contains symlink or special file')
        found.add(path.relative_to(root).as_posix())
    if found != set(manifest['files']) | {'manifest.json'}:
        raise ValueError('incomplete or extra package files')
    for name, item in manifest['files'].items():
        size, value = digest(file_path(root, name))
        if (size, value) != (item['bytes'], item['sha256']):
            raise ValueError('artifact mismatch: ' + name)
    return manifest


def seal(root, inventory, operator, repository):
    """Seal actual files after builders succeed; inventory is path -> kind."""
    if (root / 'manifest.json').exists():
        raise ValueError('manifest already exists; preserve candidate')
    files = {}
    for name, kind in inventory.items():
        size, value = digest(file_path(root, name))
        files[name] = {'kind': kind, 'bytes': size, 'sha256': value}
    manifest = {'version': VERSION, 'protocol': PROTOCOL, 'policy': POLICY,
                'operator_public': operator,
                'source_revisions': {'repository': repository,
                    'linux': '79807e970724c1f90cee1316361d61b80db4eef6',
                    'freebsd': 'aadd58dddcbc78f4d5594827b46b5633552b15ce'},
                'files': files, 'evidence': {'built': True, 'installed': False,
                    'booted': False, 'native_accepted': False, 'publication': None}}
    validate_metadata(manifest)
    raw = canonical(manifest)
    with (root / 'manifest.json').open('xb') as output:
        output.write(raw)
        output.flush()
        os.fsync(output.fileno())
    pinned = hashlib.sha256(raw).hexdigest()
    check(root, pinned, operator, repository)
    return pinned


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    for action in ['seal', 'check']:
        child = sub.add_parser(action)
        child.add_argument('--package', type=Path, required=True)
        child.add_argument('--operator-public', required=True)
        child.add_argument('--repository', required=True)
        if action == 'seal':
            child.add_argument('--inventory', type=Path, required=True)
        else:
            child.add_argument('--manifest-sha256', required=True)
    args = parser.parse_args()
    if args.action == 'seal':
        print(seal(args.package.resolve(strict=True), read_json(args.inventory),
                   args.operator_public, args.repository))
    else:
        check(args.package, args.manifest_sha256, args.operator_public, args.repository)
        print('exact offline container verified; component and runtime admission separate')


if __name__ == '__main__':
    main()
