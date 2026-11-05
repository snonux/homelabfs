#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compile actual mm/debug.o with the owned flag disabled; no runtime claim."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess

from kernel import config_values, write_new
from package import digest

HERE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    output = args.output.absolute()
    if output.exists() or source == output or source in output.parents:
        raise ValueError('fresh disjoint conditional-check output required')
    verify = ['/usr/bin/python3', str(HERE.parent / 'linux9p/prepare.py'),
              '--destination', str(source), '--verify']
    subprocess.run(verify, check=True, timeout=180)
    output.mkdir(mode=0o700)
    shutil.copyfile(args.config, output / '.config')
    subprocess.run([str(source / 'scripts/config'), '--file', str(output / '.config'),
                    '--disable', '9P_FS_HOMELABFS', '--disable',
                    '9P_FS_HOMELABFS_CONTROLLED'], check=True, timeout=10)
    make = ['/usr/bin/make', '-j2', f'O={output}', 'ARCH=x86_64',
            'CC=/usr/bin/gcc', 'HOSTCC=/usr/bin/gcc']
    env = dict(os.environ, KBUILD_BUILD_TIMESTAMP='2026-11-04 00:00:00 UTC',
               KBUILD_BUILD_USER='homelabfs', KBUILD_BUILD_HOST='owned-builder',
               KBUILD_BUILD_VERSION='1', LC_ALL='C', TZ='UTC')
    subprocess.run(make + ['olddefconfig'], cwd=source, env=env, check=True, timeout=180)
    config = config_values(output / '.config')
    if any(config.get('CONFIG_' + key) in ('y', 'm') for key in
           ['9P_FS_HOMELABFS', '9P_FS_HOMELABFS_CONTROLLED']):
        raise ValueError('conditional negative must disable the owned page flag')
    subprocess.run(make + ['mm/debug.o'], cwd=source, env=env, check=True, timeout=600)
    subprocess.run(verify, check=True, timeout=180)
    write_new(output / 'evidence.json', {'scope': 'actual mm/debug.o Hfs-off compile only',
        'source_inventory_sha256': digest(source / '.homelabfs-owned-source.json')[1],
        'config_sha256': digest(output / '.config')[1],
        'object_sha256': digest(output / 'mm/debug.o')[1],
        'hfs': False, 'full_kernel': False, 'booted': False})


if __name__ == '__main__':
    main()
