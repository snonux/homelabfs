#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Verify exact build-component linkage. Success grants no installed authority."""
import argparse
from pathlib import Path

from abi import observe as observe_abi
from elf import identity, strong_symbols
from kernel import assert_config
from package import canonical, check, file_path, read_json
from native_bundle import verify as verify_native_source
from native_build import dynamic_needed


def component(root, manifest, name, version):
    record = read_json(file_path(root, name))
    if record.get('version') != version or record.get('installed') is not False:
        raise ValueError('unsupported component build record')
    outputs = record.get('artifacts')
    if not isinstance(outputs, dict) or not outputs:
        raise ValueError('missing component outputs')
    for path, value in outputs.items():
        if path not in manifest['files'] or manifest['files'][path]['sha256'] != value:
            raise ValueError('component/package output mismatch: ' + path)
    return record


def admit(root, pin, operator, revision):
    manifest = check(root, pin, operator, revision)
    linux = component(root, manifest, 'linux/build.json', 'homelabfs-kernel-build/1')
    freebsd = component(root, manifest, 'freebsd/build.json', 'homelabfs-freebsd-build/1')
    daemon = component(root, manifest, 'freebsd/daemon-build.json', 'homelabfs-daemon-build/1')
    # The immutable source records are independently verified before building.
    # This checks their exact linkage, not an assertion about deployed sources.
    maintained = read_json(file_path(root, 'inputs/linux-source-manifest.json'))
    source = read_json(file_path(root, 'inputs/linux-source-inventory.json'))
    if maintained.get('commit') != manifest['source_revisions']['linux'] or source.get('commit') != maintained['commit'] or source.get('patches') != maintained.get('patches'):
        raise ValueError('Linux source/patch identity mismatch')
    for name, value in maintained['patched_file_sha256'].items():
        if source['files'].get(name) != value:
            raise ValueError('maintained Linux output mismatch')
    for name, key in [('inputs/linux-source-manifest.json', 'maintained_source_manifest_sha256'),
                      ('inputs/linux-source-inventory.json', 'source_inventory_sha256'),
                      ('linux/config', 'config_sha256')]:
        if linux['inputs'].get(key) != manifest['files'][name]['sha256']:
            raise ValueError('Linux build input linkage mismatch')
    for patch in maintained['patches']:
        item = manifest['files'].get('inputs/patches/' + patch['file'])
        if item is None or (item['bytes'], item['sha256']) != (patch['bytes'], patch['sha256']):
            raise ValueError('Linux packaged patch/source identity mismatch')
    if file_path(root, 'inputs/patches/series').read_text() != ''.join(p['file'] + '\n' for p in maintained['patches']):
        raise ValueError('Linux patch series order mismatch')
    assert_config(file_path(root, 'linux/config'), operator)
    if observe_abi(root, operator) != read_json(file_path(root, 'linux/abi.json')):
        raise ValueError('actual kernel/module ABI record mismatch')
    observed = identity(file_path(root, 'linux/vmlinux'))
    if observed != linux.get('kernel') or observed['type'] != 2 or not observed['gnu_build_id'] or linux.get('booted') is not False:
        raise ValueError('linked unbooted full kernel identity required')
    required_linux = {'linux/vmlinux', 'linux/bzImage', 'linux/config',
                      'linux/System.map', 'linux/Module.symvers',
                      'linux/modules.order', 'linux/modules.builtin',
                      'linux/modules.builtin.modinfo'}
    if not required_linux <= set(linux['artifacts']):
        raise ValueError('full kernel artifact closure missing')
    modules = linux.get('modules')
    ordered = file_path(root, 'linux/modules.order').read_text().splitlines()
    if not isinstance(modules, dict) or set(modules) != set(ordered) or not ordered:
        raise ValueError('configured in-tree module inventory mismatch')
    for name, expected in modules.items():
        path = 'linux/modules/' + name
        actual = identity(file_path(root, path))
        if actual != expected or actual['type'] != 1 or not actual['gnu_build_id'] or path not in linux['artifacts']:
            raise ValueError('matching in-tree module identity mismatch')
    packaged_modules = {name for name, item in manifest['files'].items() if item['kind'] == 'module'}
    if packaged_modules != {'linux/modules/' + name for name in modules}:
        raise ValueError('foreign or missing packaged module')
    if freebsd.get('revision') != manifest['source_revisions']['freebsd'] or not freebsd.get('source_files') or not freebsd.get('sysroot_files') or not freebsd.get('toolchain'):
        raise ValueError('FreeBSD owned source/binary sysroot/toolchain closure required')
    native_source = read_json(file_path(root, 'inputs/freebsd-source-inventory.json'))
    native_bundle = read_json(file_path(root, 'inputs/freebsd-source-bundle.json'))
    index_sha = manifest['files']['inputs/freebsd-source-inventory.json']['sha256']
    if native_bundle.get('version') != 'homelabfs-native-source-bundle/1' or native_bundle.get('installed') is not False or native_source.get('revision') != freebsd['revision'] or native_source.get('files') != freebsd['source_files'] or native_bundle.get('source_inventory_sha256') != index_sha or freebsd['preflight'].get('source_inventory_sha256') != index_sha or native_bundle.get('source_archive_sha256') != native_source.get('archive_sha256') or native_source.get('archive_sha256') != freebsd['source_capture'].get('archive_sha256'):
        raise ValueError('native source/build/bundle provenance mismatch')
    for name, value in native_source['seed']['files'].items():
        packaged = 'inputs/freebsd-owned/' + ('patches/' + name if name.endswith('.patch') else name)
        if packaged not in manifest['files'] or manifest['files'][packaged]['sha256'] != value:
            raise ValueError('native packaged seed/build source identity mismatch')
    archive = manifest['files']['inputs/freebsd-selected-source.tar.gz']
    if native_bundle.get('selected_archive_sha256') != archive['sha256'] or native_bundle.get('selected_archive_bytes') != archive['bytes'] or native_bundle.get('source_files') != len(native_source['files']):
        raise ValueError('native selected source archive linkage mismatch')
    expanded = verify_native_source(file_path(root, 'inputs/freebsd-selected-source.tar.gz'), native_source['files'])
    if expanded != native_bundle.get('selected_source_bytes'):
        raise ValueError('native expanded source size mismatch')
    for name, original in [('COPYRIGHT', 'COPYRIGHT'), ('lib9p-COPYRIGHT', 'contrib/lib9p/COPYRIGHT')]:
        digest = manifest['files']['licenses/freebsd/' + name]['sha256']
        if native_bundle.get('licenses', {}).get(name) != digest or native_source['files'].get(original) != digest:
            raise ValueError('native source license notice mismatch')
    exporter = identity(file_path(root, 'freebsd/bhyve'))
    if exporter != freebsd.get('exporter') or exporter['osabi'] != 9 or exporter['type'] not in (2, 3) or not exporter['gnu_build_id'] or 'freebsd/bhyve' not in freebsd['artifacts']:
        raise ValueError('full FreeBSD exporter identity required')
    needed, interpreters = dynamic_needed(file_path(root, 'freebsd/bhyve'))
    runtime = freebsd.get('runtime_libraries', {})
    if not runtime or not set(needed + interpreters) <= set(runtime) or any(name.startswith(('lib9p.so', 'libvmmapi.so')) for name in runtime):
        raise ValueError('native external runtime closure or owned static linkage mismatch')
    if any(item.get('redistributed') is not False for item in runtime.values()):
        raise ValueError('native binary sysroot libraries must remain external')
    strong_symbols(file_path(root, 'freebsd/bhyve'),
                   {'hfs9p_supervisor_start_tuple', 'hfs9p_supervisor_prepare',
                    'hfs9p_pending_exchange', 'hfs9p_controller_claim'})
    actual_daemon = identity(file_path(root, 'freebsd/hfs-controlled-runtime'))
    if daemon.get('repository') != revision or daemon.get('goos') != 'freebsd' or daemon.get('goarch') != 'amd64' or daemon.get('cgo_enabled') != '0' or not daemon.get('go_version') or not daemon.get('dependencies') or actual_daemon != daemon.get('identity') or actual_daemon['osabi'] != 9 or not actual_daemon['go_build_id'] or 'freebsd/hfs-controlled-runtime' not in daemon['artifacts']:
        raise ValueError('actual repository-native FreeBSD runtime required')
    for path, value in daemon.get('licenses', {}).items():
        if path not in manifest['files'] or manifest['files'][path]['sha256'] != value:
            raise ValueError('daemon license/package linkage mismatch')
    if not daemon.get('licenses'):
        raise ValueError('actual Go and dependency license closure missing')
    proposal = read_json(file_path(root, 'installation-proposal.json'))
    artifacts = {**linux['artifacts'], **freebsd['artifacts'], **daemon['artifacts']}
    if proposal.get('action') != 'PROPOSAL_ONLY' or proposal.get('execution_authorized') is not False or proposal.get('placement_admitted') is not False or proposal.get('publication') is not None or proposal.get('artifacts') != artifacts or proposal.get('operator_public') != operator or proposal.get('protocol') != manifest['protocol'] or proposal.get('policy') != manifest['policy']:
        raise ValueError('installation proposal artifact/policy/authority mismatch')
    kernel_repro = read_json(file_path(root, 'linux/clean-repro.json'))
    if kernel_repro.get('version') != 'homelabfs-kernel-clean-repro/1' or kernel_repro.get('equal_actual_artifacts') != linux['artifacts'] or kernel_repro.get('equal_compile_inputs') != linux['inputs'] or kernel_repro.get('first_build_record_sha256') != manifest['files']['linux/build.json']['sha256']:
        raise ValueError('kernel clean rebuild proof mismatch')
    native_repro = read_json(file_path(root, 'evidence/native-clean-repro.json'))
    if native_repro.get('version') != 'homelabfs-native-clean-repro/1' or native_repro.get('equal') is not True or native_repro.get('changed') != [] or native_repro.get('first') != native_repro.get('second') or native_repro.get('source_inventory_sha256') != index_sha or native_repro.get('first_preflight_sha256') != native_repro.get('second_preflight_sha256'):
        raise ValueError('native clean rebuild proof mismatch')
    for name, value in native_repro['first'].items():
        if name not in manifest['files'] or manifest['files'][name]['sha256'] != value:
            raise ValueError('native clean rebuild actual bytes mismatch')
    if not set(freebsd['artifacts']) <= set(native_repro['first']):
        raise ValueError('incomplete native clean rebuild proof')
    daemon_repro = read_json(file_path(root, 'evidence/daemon-clean-repro.json'))
    binary_sha = manifest['files']['freebsd/hfs-controlled-runtime']['sha256']
    if daemon_repro.get('version') != 'homelabfs-daemon-clean-repro/1' or daemon_repro.get('equal') is not True or daemon_repro.get('both_forced_rebuilds') is not True or daemon_repro.get('first_sha256') != binary_sha or daemon_repro.get('second_sha256') != binary_sha:
        raise ValueError('daemon forced rebuild proof mismatch')
    return {'container_verified': True, 'full_components_verified': True,
            'installed': False, 'booted': False, 'native_accepted': False,
            'publication': None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--manifest-sha256', required=True)
    parser.add_argument('--operator-public', required=True)
    parser.add_argument('--repository', required=True)
    args = parser.parse_args()
    print(canonical(admit(args.package.resolve(strict=True), args.manifest_sha256,
                          args.operator_public, args.repository)).decode(), end='')


if __name__ == '__main__':
    main()
