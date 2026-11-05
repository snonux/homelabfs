#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Compare two actual full clean kernel builds and their measured ABI records."""
import argparse
from pathlib import Path

from abi import observe
from kernel import write_new
from package import digest, read_json


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--first', type=Path, required=True)
    parser.add_argument('--second', type=Path, required=True)
    parser.add_argument('--operator-public', required=True)
    parser.add_argument('--record', type=Path, required=True)
    args = parser.parse_args()
    first = args.first.resolve(strict=True)
    second = args.second.resolve(strict=True)
    original = read_json(first / 'linux/build.json')
    repeated = read_json(second / 'linux/build.json')
    if original['inputs'] != repeated['inputs']:
        raise ValueError('different source/config/compiler/recipe/environment inputs')
    if original['artifacts'] != repeated['artifacts']:
        changed = sorted(key for key in original['artifacts']
                         if original['artifacts'][key] != repeated['artifacts'].get(key))
        raise ValueError('actual clean rebuild outputs differ: ' + repr(changed))
    for directory, record in [(first, original), (second, repeated)]:
        for name, expected in record['artifacts'].items():
            if digest(directory / name)[1] != expected:
                raise ValueError('actual bytes differ from build record: ' + name)
    first_abi = observe(first, args.operator_public)
    second_abi = observe(second, args.operator_public)
    if first_abi != second_abi or first_abi != read_json(first / 'linux/abi.json') or second_abi != read_json(second / 'linux/abi.json'):
        raise ValueError('actual full kernel/module ABI differs')
    write_new(args.record, {'version': 'homelabfs-kernel-clean-repro/1',
        'scope': 'full linked kernel/bzImage/all configured in-tree modules/config/symvers',
        'equal_actual_artifacts': original['artifacts'],
        'equal_actual_abi': first_abi, 'equal_compile_inputs': original['inputs'],
        'first_build_record_sha256': digest(first / 'linux/build.json')[1],
        'second_build_record_sha256': digest(second / 'linux/build.json')[1],
        'installed': False, 'booted': False, 'native_accepted': False})
    print('actual clean full kernel, bzImage, all modules/config/symvers and ABI byte-identical; unbooted')


if __name__ == '__main__':
    main()
