#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Export only the maintained guest files from a task-owned pinned source tree."""
import argparse
import difflib
import hashlib
import json
from pathlib import Path
import tarfile

COMMIT = '79807e970724c1f90cee1316361d61b80db4eef6'
ARCHIVE_SHA = '4f3c00db43ece26e00b83da8cee938e544ba89e10dc10aecc45209ab03554a17'
GROUPS = [
    ('0001-9p-hfs1-transport.patch', '9p: negotiate exact hfs1 over virtio without stock fallback',
     ['include/net/9p/9p.h', 'include/net/9p/client.h', 'net/9p/client.c', 'net/9p/protocol.c']),
    ('0002-hfs1-canonical-codec-policy.patch', '9p: bound canonical bridge framing and identity/attribute decoding',
     ['fs/9p/hfs_protocol.h', 'fs/9p/hfs_protocol.c', 'fs/9p/hfs_policy.h', 'fs/9p/hfs_policy.c',
      'fs/9p/hfs_cursor.h', 'fs/9p/hfs_cursor.c']),
    ('0003-hfs1-guest-vfs.patch', '9p: bind guest VFS requests to immutable credentials, claims and grants',
     ['fs/9p/Kconfig', 'fs/9p/Makefile', 'fs/9p/vfs_super.c', 'fs/9p/hfs.h',
      'fs/9p/hfs_session.c', 'fs/9p/hfs_inode.c', 'fs/9p/hfs_file.c']),
    ('0004-hfs1-physical-views.patch', 'mm/9p: retain physical cache installation tokens and exact view leases',
     ['include/linux/mm.h', 'include/linux/page-flags.h', 'mm/memory.c', 'mm/gup.c',
      'mm/mremap.c', 'mm/huge_memory.c', 'fs/9p/hfs_view.h', 'fs/9p/hfs_view.c',
      'fs/9p/hfs_lease.h', 'fs/9p/hfs_lease.c', 'fs/9p/hfs_mmap.c']),
]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--development', action='store_true')
    parser.add_argument('--task', default='vz3', help='owning project task identity')
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    archive = args.archive.resolve(strict=True)
    here = Path(__file__).resolve().parent
    if digest(archive.read_bytes()) != ARCHIVE_SHA:
        raise SystemExit('refuse: pinned archive digest mismatch')
    wanted = {name for _, _, names in GROUPS for name in names}
    originals = {}
    with tarfile.open(archive, 'r:gz') as tar:
        if tar.pax_headers.get('comment') != COMMIT:
            raise SystemExit('refuse: archive commit identity mismatch')
        prefix = f'linux-{COMMIT}/'
        for member in tar:
            name = member.name.removeprefix(prefix)
            if name in wanted and member.isfile():
                originals[name] = tar.extractfile(member).read()
    patches = []
    base_hashes = {}
    patched_hashes = {}
    for filename, subject, names in GROUPS:
        lines = [f'Subject: {subject}\n\n', f'Base-commit: {COMMIT}\n',
                 'Owned integration patch; no installation or runtime admission.\n\n']
        for name in names:
            original = originals.get(name, b'')
            modified = (source / name).read_bytes()
            if original == modified:
                raise SystemExit(f'refuse: owned path has no change: {name}')
            base_hashes[name] = digest(original) if name in originals else None
            patched_hashes[name] = digest(modified)
            lines.append(f'diff --git a/{name} b/{name}\n')
            if name not in originals:
                lines.append('new file mode 100644\n')
            lines.extend(difflib.unified_diff(
                original.decode().splitlines(keepends=True),
                modified.decode().splitlines(keepends=True),
                fromfile=f'a/{name}' if name in originals else '/dev/null',
                tofile=f'b/{name}', n=3))
        data = ''.join(lines).encode()
        (here / 'patches' / filename).write_bytes(data)
        patches.append({'file': filename, 'sha256': digest(data), 'bytes': len(data)})
    (here / 'patches/series').write_text(''.join(p['file'] + '\n' for p in patches))
    manifest = {
        'format': 1, 'project_task': args.task + '-development' if args.development else args.task, 'source_kind': 'upstream Linux reference', 'unfinished_mmap_development': args.development,
        'version': '6.1.190', 'commit': COMMIT,
        'tag': 'v6.1.190', 'tag_object': '9ad12a4c0c7a36f365c13ea17d88499618440a7b',
        'git_url': 'https://github.com/gregkh/linux.git',
        'archive_url': f'https://codeload.github.com/gregkh/linux/tar.gz/{COMMIT}',
        'archive_sha256': ARCHIVE_SHA, 'archive_bytes': archive.stat().st_size,
        'patches': patches, 'base_file_sha256': base_hashes,
        'patched_file_sha256': patched_hashes,
        'installed_elrepo_source_binding': 'unproven',
        'module_abi_runtime_and_normal_publication': 'unproven; not admitted',
    }
    (here / 'source.json').write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'patches': len(patches), 'owned_files': len(wanted)}))


if __name__ == '__main__':
    main()
