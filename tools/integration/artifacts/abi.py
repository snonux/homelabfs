#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Check actual in-tree ELF module versions against the complete kernel build."""
import argparse
from pathlib import Path
import struct

from elf import identity
from kernel import assert_config, config_values, write_new
from package import digest, read_json

RELEASE = '6.1.190-hfs-xz3'


def section(path, wanted):
    with path.open('rb') as stream:
        size = stream.seek(0, 2)
        if not 64 <= size <= 128 << 20:
            raise ValueError('ELF bound')
        stream.seek(0)
        raw = stream.read(64)
        if raw[:7] != b'\x7fELF\x02\x01\x01':
            raise ValueError('selected ELF64 required')
        h = struct.unpack('<HHIQQQIHHHHHH', raw[16:])
        start, stride, count, names_index = h[5], h[10], h[11], h[12]
        if stride != 64 or not 1 <= count <= 32768 or names_index >= count or start + stride * count > size:
            raise ValueError('ELF section table bound')
        stream.seek(start)
        rows = [struct.unpack('<IIQQQQIIQQ', stream.read(stride)) for _ in range(count)]

        def contents(row):
            if row[4] + row[5] > size or row[5] > 8 << 20:
                raise ValueError('ELF section content bound')
            stream.seek(row[4])
            return stream.read(row[5])

        names = contents(rows[names_index])
        found = []
        for row in rows:
            if row[0] >= len(names):
                raise ValueError('ELF section name offset')
            end = names.find(b'\0', row[0])
            if end < 0:
                raise ValueError('ELF section name termination')
            if names[row[0]:end].decode('ascii') == wanted:
                found.append(contents(row))
        if len(found) != 1:
            raise ValueError('missing/ambiguous ELF section: ' + wanted)
        return found[0]


def symbol_versions(path):
    result = {}
    for line in path.read_text().splitlines():
        words = line.split()
        if len(words) < 4 or words[1] in result:
            raise ValueError('invalid/duplicate Module.symvers entry')
        value = int(words[0], 16)
        if not 0 <= value <= 0xffffffff:
            raise ValueError('symbol CRC bound')
        result[words[1]] = value
    if not result or 'module_layout' not in result:
        raise ValueError('complete kernel symbol versions required')
    return result


def module(path, release, symbols):
    fields = {}
    for item in section(path, '.modinfo').split(b'\0'):
        if item:
            key, value = item.decode('utf-8').split('=', 1)
            fields.setdefault(key, []).append(value)
    for key in ['name', 'vermagic', 'depends', 'intree']:
        if len(fields.get(key, [])) != 1:
            raise ValueError('exact module metadata required: ' + key)
    magic = fields['vermagic'][0]
    if not magic.startswith(release + ' ') or fields['intree'] != ['Y']:
        raise ValueError('foreign kernel release/out-of-tree module')
    raw = section(path, '__versions')
    if not raw or len(raw) % 64:
        raise ValueError('kernel module version table required')
    imports = {}
    for offset in range(0, len(raw), 64):
        value = struct.unpack_from('<Q', raw, offset)[0]
        name = raw[offset + 8:offset + 64].split(b'\0', 1)[0].decode('ascii')
        if not name or name in imports or symbols.get(name) != value:
            raise ValueError('kernel/module symbol CRC mismatch: ' + name)
        imports[name] = f'0x{value:08x}'
    if 'module_layout' not in imports:
        raise ValueError('module_layout ABI witness required')
    return {'identity': identity(path), 'name': fields['name'][0],
            'vermagic': magic, 'depends': fields['depends'][0], 'imports': imports}


def observe(package, operator):
    linux = package / 'linux'
    release = (linux / 'kernel.release').read_text().strip()
    if release != RELEASE:
        raise ValueError('unselected complete kernel release')
    assert_config(linux / 'config', operator)
    config = config_values(linux / 'config')
    expected_magic = release + ' SMP '
    if config.get('CONFIG_PREEMPT_BUILD') == 'y':
        expected_magic += 'preempt '
    expected_magic += 'mod_unload modversions '
    symbols = symbol_versions(linux / 'Module.symvers')
    record = read_json(linux / 'build.json')
    result = {}
    for name in (linux / 'modules.order').read_text().splitlines():
        relative = Path(name)
        if relative.is_absolute() or '..' in relative.parts:
            raise ValueError('in-tree module path')
        path = linux / 'modules' / relative
        if record['artifacts'].get('linux/modules/' + name) != digest(path)[1]:
            raise ValueError('module differs from actual build record')
        actual = module(path, release, symbols)
        if actual['vermagic'] != expected_magic or actual['identity'] != record['modules'].get(name):
            raise ValueError('kernel configuration/module identity mismatch')
        result[name] = actual
    names = {value['name'] for value in result.values()}
    if len(names) != len(result) or not names:
        raise ValueError('complete unique module names required')
    for value in result.values():
        deps = set(value['depends'].split(',')) - {''}
        if not deps <= names:
            raise ValueError('missing configured in-tree dependency module')
    return {'version': 'homelabfs-kernel-module-abi/1', 'release': release,
            'config_sha256': digest(linux / 'config')[1],
            'symvers_sha256': digest(linux / 'Module.symvers')[1],
            'kernel_identity': identity(linux / 'vmlinux'), 'modules': result,
            'installed': False, 'booted': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['collect', 'verify'])
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--operator-public', required=True)
    args = parser.parse_args()
    package = args.package.resolve(strict=True)
    if args.action == 'collect':
        if args.output is None:
            raise ValueError('actual complete kernel build output required')
        raw = (args.output / 'include/config/kernel.release').read_bytes()
        with (package / 'linux/kernel.release').open('xb') as target:
            target.write(raw)
        write_new(package / 'linux/abi.json', observe(package, args.operator_public))
    elif observe(package, args.operator_public) != read_json(package / 'linux/abi.json'):
        raise ValueError('changed kernel/module ABI record')
    print('exact release/config/Module.symvers/import CRC/vermagic matched; unbooted')


if __name__ == '__main__':
    main()
