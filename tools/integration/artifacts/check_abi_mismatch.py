#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Negative controls against the actual built in-tree module bytes."""
import argparse
from pathlib import Path

from abi import RELEASE, module, observe, symbol_versions
from kernel import write_new
from package import read_json


def refused(name, operation, results):
    try:
        operation()
    except ValueError as exc:
        results[name] = str(exc)
        return
    raise ValueError('mismatched ABI unexpectedly admitted: ' + name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--operator-public', required=True)
    args = parser.parse_args()
    package = args.package.resolve(strict=True)
    expected = read_json(package / 'linux/abi.json')
    if observe(package, args.operator_public) != expected:
        raise ValueError('positive actual artifact check failed')
    path = package / 'linux/modules' / next(iter(expected['modules']))
    symbols = symbol_versions(package / 'linux/Module.symvers')
    results = {}
    refused('stock-kernel-release', lambda: module(path, '6.1.190-stock', symbols), results)
    wrong = dict(symbols)
    wrong['module_layout'] ^= 1
    refused('different-kernel-module-layout-crc', lambda: module(path, RELEASE, wrong), results)
    missing = dict(symbols)
    del missing['module_layout']
    refused('missing-kernel-module-layout-crc', lambda: module(path, RELEASE, missing), results)
    key = ('00' if args.operator_public[:2] != '00' else '01') + args.operator_public[2:]
    refused('different-operator-config-identity', lambda: observe(package, key), results)
    if observe(package, args.operator_public) != expected:
        raise ValueError('negative controls altered original artifacts')
    write_new(package / 'linux/abi-negative.json', {
        'version': 'homelabfs-abi-negative-controls/1', 'actual_module': str(path.relative_to(package)),
        'refusals': results, 'original_unchanged': True, 'booted': False})
    print('actual module release/CRC/config mismatch controls refused; no runtime claim')


if __name__ == '__main__':
    main()
