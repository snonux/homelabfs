#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Mutate a private copy of actual artifacts; originals remain immutable."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import shutil
import tarfile

from admit import admit
from elf import identity
from kernel import write_new
from package import canonical, digest, read_json


def controls(package, pin, operator, baseline, output):
    positive = admit(package, pin, operator, baseline)
    if package == output or package in output.parents or output in package.parents:
        raise ValueError('disjoint candidate and control-output roots required')
    output.mkdir(mode=0o700)
    fixture = output / 'candidate'
    shutil.copytree(package, fixture)
    original_manifest = (fixture / 'manifest.json').read_bytes()
    manifest = read_json(fixture / 'manifest.json')
    saved, results = {}, []

    def put(name, data):
        path = fixture / name
        saved.setdefault(name, path.read_bytes())
        path.write_bytes(data)
        manifest['files'][name]['bytes'], manifest['files'][name]['sha256'] = digest(path)

    def metadata(name, action):
        value = read_json(fixture / name)
        action(value)
        put(name, canonical(value))

    def reject(name, change, expected, key=operator, revision=baseline):
        change()
        raw = canonical(manifest)
        (fixture / 'manifest.json').write_bytes(raw)
        changed_pin = hashlib.sha256(raw).hexdigest()
        try:
            admit(fixture, changed_pin, key, revision)
        except ValueError as exc:
            if expected not in str(exc):
                raise AssertionError('wrong refusal for ' + name + ': ' + str(exc)) from exc
            results.append({'case': name, 'refused': True, 'reason': str(exc)})
        else:
            raise AssertionError('unsupported actual candidate accepted: ' + name)
        for path, data in saved.items():
            (fixture / path).write_bytes(data)
        saved.clear()
        (fixture / 'manifest.json').write_bytes(original_manifest)
        manifest.clear()
        manifest.update(json.loads(original_manifest))

    reject('external operator mismatch', lambda: None, 'unexpected operator', key='ab' * 32)
    reject('external repository mismatch', lambda: None, 'unexpected operator or repository', revision='ab' * 20)
    reject('old negotiated features', lambda: manifest['protocol'].update(Features=16383), 'unsupported version, protocol or policy')
    reject('old source revision', lambda: metadata('inputs/linux-source-manifest.json', lambda v: v.update(commit='ab' * 20)), 'Linux source/patch identity mismatch')
    reject('different packaged Linux patch', lambda: put('inputs/patches/0004-hfs1-physical-views.patch', b'foreign maintained patch'), 'Linux packaged patch/source identity mismatch')
    reject('different native owned source', lambda: metadata('inputs/freebsd-source-inventory.json', lambda v: v['files'].update({'contrib/lib9p/backend/hfs9p.c': 'ab' * 32})), 'native source/build/bundle provenance mismatch')

    def foreign_source():
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w:gz') as tar:
            info = tarfile.TarInfo('contrib/lib9p/backend/hfs9p.c')
            body = b'foreign maintained source'
            info.size = len(body)
            tar.addfile(info, io.BytesIO(body))
        put('inputs/freebsd-selected-source.tar.gz', data.getvalue())
        item = manifest['files']['inputs/freebsd-selected-source.tar.gz']
        metadata('inputs/freebsd-source-bundle.json', lambda v: v.update(selected_archive_sha256=item['sha256'], selected_archive_bytes=item['bytes']))
    reject('wrong actual bundled source with consistent container hashes', foreign_source, 'native source archive content mismatch')

    def foreign_exporter():
        path = 'freebsd/bhyve'
        put(path, (fixture / 'freebsd/hfs-controlled-runtime').read_bytes())
        value = manifest['files'][path]['sha256']
        metadata('freebsd/build.json', lambda v: (v['artifacts'].update({path: value}), v.update(exporter=identity(fixture / path))))
    reject('Go ELF labelled as full exporter', foreign_exporter, 'dynamic native exporter expected')
    def helper_exporter():
        path = 'freebsd/bhyve'
        put(path, (fixture / 'freebsd/bhyve-slirp-helper').read_bytes())
        value = manifest['files'][path]['sha256']
        metadata('freebsd/build.json', lambda v: (v['artifacts'].update({path: value}), v.update(exporter=identity(fixture / path))))
    reject('native helper ELF lacks production controllers', helper_exporter, 'missing strong production exporter symbols')
    reject('invented stock shared9p runtime', lambda: metadata('freebsd/build.json', lambda v: v['runtime_libraries'].update({'lib9p.so.1': next(iter(v['runtime_libraries'].values()))})), 'native external runtime closure or owned static linkage mismatch')
    reject('installed success claim', lambda: metadata('freebsd/build.json', lambda v: v.update(installed=True)), 'unsupported component build record')
    reject('missing actual Go build version', lambda: metadata('freebsd/daemon-build.json', lambda v: v.update(go_version='')), 'actual repository-native FreeBSD runtime required')
    reject('installation authority claim', lambda: metadata('installation-proposal.json', lambda v: v.update(execution_authorized=True)), 'installation proposal artifact/policy/authority mismatch')
    reject('false forced rebuild witness', lambda: metadata('evidence/daemon-clean-repro.json', lambda v: v.update(second_sha256='ab' * 32)), 'daemon forced rebuild proof mismatch')
    assert admit(fixture, pin, operator, baseline) == positive
    assert admit(package, pin, operator, baseline) == positive
    write_new(output / 'controls.json', {'actual_full_candidate_positive': positive,
        'cases': results, 'original_manifest_sha256': pin,
        'restored_fixture_passed': True, 'original_passed_after_controls': True})
    print(json.dumps({'actual_full_candidate_controls': len(results), 'all_refused_for_expected_reason': True}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--manifest-sha256', required=True)
    parser.add_argument('--operator-public', required=True)
    parser.add_argument('--repository', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    controls(args.package.resolve(strict=True), args.manifest_sha256,
             args.operator_public, args.repository, args.output.absolute())


if __name__ == '__main__':
    main()
