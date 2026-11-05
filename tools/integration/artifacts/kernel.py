#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Owned full-kernel recipe. Run each stage under the approved resource guard.

All outputs remain in fresh private build/package directories. There is no
system installation, boot, module load, guest access or publication operation.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

from elf import identity
from package import canonical, digest, read_json

HERE = Path(__file__).resolve().parent
# Rootful/init_user_ns OCI fixture: VFS rootfs, private loopback-only network,
# separate mount/PID/network/UTS/IPC namespaces, memory and PID cgroup limits.
# Mount namespaces have no separate MNT_NS Kconfig symbol in selected Linux.
POD_REQUIRED = ('MULTIUSER', 'NAMESPACES', 'UTS_NS', 'IPC_NS', 'PID_NS', 'NET_NS',
                'USER_NS', 'SYSVIPC', 'POSIX_MQUEUE', 'NET', 'INET', 'UNIX',
                'CGROUPS', 'CGROUP_PIDS', 'MEMCG', 'PROC_FS', 'SYSFS', 'TMPFS',
                'TMPFS_XATTR', 'UNIX98_PTYS', 'SECCOMP', 'SECCOMP_FILTER',
                'SECURITY', 'SECURITY_NETWORK', 'SECURITYFS', 'SECURITY_SELINUX',
                'AUDIT', 'AUDITSYSCALL', 'FILE_LOCKING')
REQUIRED = ('64BIT', 'X86_64', 'FILE_LOCKING', 'MODULES', 'MODVERSIONS', 'SYSFS', 'SYSCTL',
            'TRACEPOINTS', 'NET_9P', 'NET_9P_VIRTIO', '9P_FS',
            '9P_FS_HOMELABFS', '9P_FS_HOMELABFS_CONTROLLED', 'CC_IS_GCC',
            'TRANSPARENT_HUGEPAGE', 'SHMEM', 'READ_ONLY_THP_FOR_FS',
            'EFI', 'EFI_STUB', 'BLK_DEV_NVME', 'BLK_DEV_DM', 'EXT4_FS', 'XFS_FS',
            'SECURITY_SELINUX', 'AUDIT', 'DEVTMPFS', 'BLK_DEV_INITRD') + POD_REQUIRED


def write_new(path, value):
    with path.open('xb') as output:
        output.write(canonical(value))
        output.flush()
        os.fsync(output.fileno())


def toolchain():
    gcc = Path('/usr/bin/gcc').resolve(strict=True)
    cc1 = Path(subprocess.check_output([str(gcc), '-print-prog-name=cc1'], text=True).strip()).resolve(strict=True)
    paths = {gcc, cc1}
    for name in ('make', 'as', 'ld', 'ar', 'nm', 'objcopy', 'objdump', 'readelf',
                 'bison', 'flex', 'bc', 'perl', 'openssl', 'pkg-config'):
        program = shutil.which(name, path='/usr/bin:/bin')
        if not program:
            raise ValueError('missing build tool: ' + name)
        paths.add(Path(program).resolve(strict=True))
    # Record the actual loader dependencies of build tools; these are binary
    # toolchain inputs, not invented source ancestry or a distribution ABI claim.
    for path in list(paths):
        result = subprocess.run(['/usr/bin/ldd', str(path)], text=True,
                                capture_output=True, check=False)
        for word in result.stdout.split():
            if word.startswith('/') and Path(word).is_file():
                paths.add(Path(word).resolve(strict=True))
    result = {}
    for path in sorted(paths):
        size, value = digest(path)
        result[str(path)] = {'bytes': size, 'sha256': value}
    return result


def config_values(path):
    # Admit a resolved .config, not a fragment with last-assignment-wins
    # ambiguity. Also count disabled entries when detecting duplicates.
    values = {}
    for line in path.read_text().splitlines():
        assignment = re.fullmatch(r'(CONFIG_[A-Za-z0-9_]+)=(.+)', line)
        disabled = re.fullmatch(r'# (CONFIG_[A-Za-z0-9_]+) is not set', line)
        if assignment:
            symbol, value = assignment.groups()
        elif disabled:
            symbol, value = disabled.group(1), 'n'
        elif line.startswith(('CONFIG_', '# CONFIG_')):
            raise ValueError('malformed kernel configuration entry')
        else:
            continue
        if symbol in values:
            raise ValueError('duplicate kernel configuration: ' + symbol)
        values[symbol] = value
    return values


def assert_config(path, key):
    values = config_values(path)
    for symbol in REQUIRED:
        if values.get('CONFIG_' + symbol) != 'y':
            raise ValueError('required full-kernel configuration: ' + symbol)
    if values.get('CONFIG_9P_FS_HOMELABFS_OPERATOR_KEY') != '"' + key + '"' or values.get('CONFIG_LOCALVERSION') != '"-hfs-xz3"' or values.get('CONFIG_VIRTIO_NET') != 'm':
        raise ValueError('candidate operator, kernel release or in-tree module selection')
    if values.get('CONFIG_DEBUG_INFO_BTF') == 'y' or values.get('CONFIG_LOCALVERSION_AUTO') == 'y':
        raise ValueError('unselected build configuration')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage', choices=['configure', 'vmlinux', 'image-modules', 'collect'])
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--operator-public', required=True)
    args = parser.parse_args()
    if len(args.operator_public) != 64 or any(c not in '0123456789abcdef' for c in args.operator_public) or int(args.operator_public, 16) == 0:
        raise ValueError('exact nonzero operator public key required')
    source = args.source.resolve(strict=True)
    output = args.output.absolute()
    package = args.package.absolute()
    for left, right in ((source, output), (source, package), (output, package)):
        if left == right or left in right.parents or right in left.parents:
            raise ValueError('disjoint source/build/package roots required')
    if any(p.is_symlink() for p in [output, package]):
        raise ValueError('real owned output roots required')
    env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR') if key in os.environ}
    env.update(PATH='/usr/bin:/bin', LC_ALL='C', TZ='UTC',
               KBUILD_BUILD_TIMESTAMP='2026-11-04 00:00:00 UTC',
               KBUILD_BUILD_USER='homelabfs', KBUILD_BUILD_HOST='owned-builder',
               KBUILD_BUILD_VERSION='1', SOURCE_DATE_EPOCH='1793750400',
               KCONFIG_NOTIMESTAMP='1',
               KCFLAGS=f'-fdebug-prefix-map={source}=/usr/src/linux '
                       f'-fdebug-prefix-map={output}=/usr/src/linux-build '
                       f'-fmacro-prefix-map={source}=/usr/src/linux '
                       f'-fmacro-prefix-map={output}=/usr/src/linux-build')
    make = ['/usr/bin/make', '-j2', f'O={output}', 'ARCH=x86_64',
            'CC=/usr/bin/gcc', 'HOSTCC=/usr/bin/gcc', 'LD=/usr/bin/ld',
            'AR=/usr/bin/ar', 'NM=/usr/bin/nm', 'OBJCOPY=/usr/bin/objcopy',
            'OBJDUMP=/usr/bin/objdump', 'READELF=/usr/bin/readelf']

    def run(argv):
        subprocess.run(argv, cwd=source, env=env, check=True, timeout=1190)

    def verify_source():
        run(['/usr/bin/python3', str(HERE.parent / 'linux9p/prepare.py'),
             '--destination', str(source), '--verify'])

    inventory = source / '.homelabfs-owned-source.json'
    if args.stage == 'configure':
        if output.exists() or package.exists():
            raise ValueError('fresh build and package roots required')
        verify_source()
        output.mkdir(mode=0o700)
        package.mkdir(mode=0o700)
        run(make + ['allnoconfig'])
        fragment = (HERE / 'kernel.config').read_text()
        with (output / '.config').open('a') as stream:
            stream.write('\n' + fragment + '\nCONFIG_9P_FS_HOMELABFS_OPERATOR_KEY="' + args.operator_public + '"\n')
        run(make + ['olddefconfig'])
        assert_config(output / '.config', args.operator_public)
        write_new(output / 'owned-inputs.json', {
            'source_inventory_sha256': digest(inventory)[1],
            'maintained_source_manifest_sha256': digest(HERE.parent / 'linux9p/source.json')[1],
            'recipe_sha256': digest(Path(__file__))[1],
            'fragment_sha256': digest(HERE / 'kernel.config')[1],
            'config_sha256': digest(output / '.config')[1],
            'operator_public': args.operator_public, 'toolchain': toolchain(),
            'environment': env, 'make': make})
        return
    verify_source()
    inputs = read_json(output / 'owned-inputs.json')
    assert_config(output / '.config', args.operator_public)
    if digest(inventory)[1] != inputs['source_inventory_sha256'] or digest(output / '.config')[1] != inputs['config_sha256'] or digest(Path(__file__))[1] != inputs['recipe_sha256'] or digest(HERE.parent / 'linux9p/source.json')[1] != inputs['maintained_source_manifest_sha256'] or toolchain() != inputs['toolchain']:
        raise ValueError('changed source, config, recipe or toolchain')
    if args.stage == 'vmlinux':
        run(make + ['vmlinux'])
        verify_source()
    elif args.stage == 'image-modules':
        run(make + ['bzImage', 'modules'])
        verify_source()
    else:
        destination = package / 'linux'
        destination.mkdir(mode=0o700)
        artifacts = {}
        for original, name in [('vmlinux', 'vmlinux'), ('arch/x86/boot/bzImage', 'bzImage'),
                               ('.config', 'config'), ('System.map', 'System.map'),
                               ('Module.symvers', 'Module.symvers'),
                               ('modules.order', 'modules.order'),
                               ('modules.builtin', 'modules.builtin'),
                               ('modules.builtin.modinfo', 'modules.builtin.modinfo')]:
            shutil.copyfile(output / original, destination / name)
            artifacts['linux/' + name] = digest(destination / name)[1]
        kernel = identity(destination / 'vmlinux')
        if kernel['type'] != 2 or not kernel['gnu_build_id']:
            raise ValueError('full linked kernel identity required')
        modules = {}
        for name in (output / 'modules.order').read_text().splitlines():
            path = Path(name)
            if path.is_absolute() or '..' in path.parts or path.suffix != '.ko':
                raise ValueError('in-tree module path')
            target = destination / 'modules' / path
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            shutil.copyfile(output / path, target)
            module = identity(target)
            if module['type'] != 1 or not module['gnu_build_id']:
                raise ValueError('linked in-tree module identity required')
            modules[name] = module
            artifacts['linux/modules/' + name] = digest(target)[1]
        if not modules:
            raise ValueError('selected in-tree module closure missing')
        write_new(destination / 'build.json', {'version': 'homelabfs-kernel-build/1',
            'inputs': inputs, 'kernel': kernel, 'modules': modules,
            'artifacts': artifacts, 'installed': False, 'booted': False})
        print('full kernel and matching in-tree modules staged; unbooted')


if __name__ == '__main__':
    main()
