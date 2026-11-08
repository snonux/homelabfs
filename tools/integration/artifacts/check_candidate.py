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

from admit import CC_WRAPPER, DAEMON, SYSROOT_CAPTURE, admit
import daemon_cgo
from elf import identity
from kernel import write_new
from package import canonical, digest, read_json


class Mutator:
    """Apply one mutation to the private copy, require refusal, then restore."""

    def __init__(self, fixture, operator, baseline):
        self.fixture, self.operator, self.baseline = fixture, operator, baseline
        self.original_manifest = (fixture / 'manifest.json').read_bytes()
        self.manifest = json.loads(self.original_manifest)
        self.saved, self.results = {}, []

    def put(self, name, data):
        path = self.fixture / name
        self.saved.setdefault(name, path.read_bytes())
        path.write_bytes(data)
        item = self.manifest['files'][name]
        item['bytes'], item['sha256'] = digest(path)

    def metadata(self, name, action):
        value = read_json(self.fixture / name)
        action(value)
        self.put(name, canonical(value))

    def replace(self, name, old, new, count=None):
        """Same-length byte substitution in an actual artifact."""
        raw = (self.fixture / name).read_bytes()
        if len(old) != len(new) or (count is not None and raw.count(old) != count) or old not in raw:
            raise AssertionError('unexpected mutation target in ' + name)
        self.put(name, raw.replace(old, new))

    def reject(self, case, change, expected, key=None, revision=None):
        change()
        raw = canonical(self.manifest)
        (self.fixture / 'manifest.json').write_bytes(raw)
        try:
            admit(self.fixture, hashlib.sha256(raw).hexdigest(), key or self.operator, revision or self.baseline)
        except ValueError as exc:
            if expected not in str(exc):
                raise AssertionError('wrong refusal for ' + case + ': ' + str(exc)) from exc
            self.results.append({'case': case, 'refused': True, 'reason': str(exc)})
        else:
            raise AssertionError('unsupported actual candidate accepted: ' + case)
        for path, data in self.saved.items():
            (self.fixture / path).write_bytes(data)
        self.saved.clear()
        (self.fixture / 'manifest.json').write_bytes(self.original_manifest)
        self.manifest = json.loads(self.original_manifest)


def source_controls(m):
    m.reject('external operator mismatch', lambda: None, 'unexpected operator', key='ab' * 32)
    m.reject('external repository mismatch', lambda: None, 'unexpected operator or repository', revision='ab' * 20)
    m.reject('old negotiated features', lambda: m.manifest['protocol'].update(Features=16383), 'unsupported version, protocol or policy')
    m.reject('old source revision', lambda: m.metadata('inputs/linux-source-manifest.json', lambda v: v.update(commit='ab' * 20)), 'Linux source/patch identity mismatch')
    m.reject('different packaged Linux patch', lambda: m.put('inputs/patches/0004-hfs1-physical-views.patch', b'foreign maintained patch'), 'Linux packaged patch/source identity mismatch')
    m.reject('different native owned source', lambda: m.metadata('inputs/freebsd-source-inventory.json', lambda v: v['files'].update({'contrib/lib9p/backend/hfs9p.c': 'ab' * 32})), 'native source/build/bundle provenance mismatch')

    def foreign_source():
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w:gz') as tar:
            info = tarfile.TarInfo('contrib/lib9p/backend/hfs9p.c')
            body = b'foreign maintained source'
            info.size = len(body)
            tar.addfile(info, io.BytesIO(body))
        m.put('inputs/freebsd-selected-source.tar.gz', data.getvalue())
        item = m.manifest['files']['inputs/freebsd-selected-source.tar.gz']
        m.metadata('inputs/freebsd-source-bundle.json', lambda v: v.update(selected_archive_sha256=item['sha256'], selected_archive_bytes=item['bytes']))
    m.reject('wrong actual bundled source with consistent container hashes', foreign_source, 'native source archive content mismatch')


def exporter_controls(m):
    def relabel(source):
        path = 'freebsd/bhyve'
        m.put(path, (m.fixture / source).read_bytes())
        value = m.manifest['files'][path]['sha256']
        m.metadata('freebsd/build.json', lambda v: (v['artifacts'].update({path: value}), v.update(exporter=identity(m.fixture / path))))
    m.reject('daemon ELF labelled as full exporter', lambda: relabel(DAEMON), 'missing strong production exporter symbols')
    m.reject('native helper ELF lacks production controllers', lambda: relabel('freebsd/bhyve-slirp-helper'), 'missing strong production exporter symbols')
    m.reject('invented stock shared9p runtime', lambda: m.metadata('freebsd/build.json', lambda v: v['runtime_libraries'].update({'lib9p.so.1': next(iter(v['runtime_libraries'].values()))})), 'native external runtime closure or owned static linkage mismatch')
    m.reject('installed success claim', lambda: m.metadata('freebsd/build.json', lambda v: v.update(installed=True)), 'unsupported component build record')


def replace_daemon(m, data):
    """Swap the daemon bytes, keeping record artifact/identity consistent so
    the refusal comes from the cgo/closure checks rather than linkage."""
    m.put(DAEMON, data)
    value = m.manifest['files'][DAEMON]['sha256']
    m.metadata('freebsd/daemon-build.json', lambda v: (v['artifacts'].update({DAEMON: value}), v.update(identity=identity(m.fixture / DAEMON))))


def daemon_controls(m, cgo0_daemon):
    record = 'freebsd/daemon-build.json'
    m.reject('missing actual Go build version', lambda: m.metadata(record, lambda v: v.update(go_version='')), 'actual repository-native FreeBSD runtime required')
    m.reject('real CGO_ENABLED=0 daemon with nocgo observer stub', lambda: replace_daemon(m, cgo0_daemon.read_bytes()), 'CGO-enabled FreeBSD daemon required')
    m.reject('record claims CGO0 build', lambda: m.metadata(record, lambda v: v.update(cgo_enabled='0')), 'CGO-enabled FreeBSD daemon required')
    m.reject('nocgo stub in recorded source closure', lambda: m.metadata(record, lambda v: v['dependencies'][daemon_cgo.CONTROLLED_RUNTIME]['files'].update({daemon_cgo.NOCGO_SOURCE: 'ab' * 32})), 'lacks the cgo process observer')
    m.reject('renamed cgo process observer symbols', lambda: (m.replace(DAEMON, b'_Cfunc_hfs_process', b'_Cfunc_hfs_procesX'), replace_daemon(m, (m.fixture / DAEMON).read_bytes())), 'cgo process observer symbols missing')
    m.reject('foreign dynamic loader', lambda: (m.replace(DAEMON, b'/libexec/ld-elf.so.1\0', b'/usr/hfs/ld-elf.so.1\0', count=1), replace_daemon(m, (m.fixture / DAEMON).read_bytes())), 'foreign dynamic loader')
    m.reject('extra recorded DT_NEEDED', lambda: m.metadata(record, lambda v: v['runtime_closure']['needed'].append('libhfs.so.1')), 'DT_NEEDED does not match recorded closure')
    m.reject('missing recorded DT_NEEDED', lambda: m.metadata(record, lambda v: v['runtime_closure']['needed'].pop()), 'DT_NEEDED does not match recorded closure')

    def foreign_library_hash():
        m.metadata(SYSROOT_CAPTURE, lambda v: v['files']['lib/libc.so.7'].update(sha256='ab' * 32))
        value = m.manifest['files'][SYSROOT_CAPTURE]['sha256']
        m.metadata(record, lambda v: v['sysroot'].update(capture_sha256=value))
    m.reject('sysroot library hash differs from recorded closure', foreign_library_hash, 'daemon runtime library sysroot hash mismatch: libc.so.7')
    m.reject('unlinked sysroot capture', lambda: m.metadata(SYSROOT_CAPTURE, lambda v: v.update(scope='foreign')), 'daemon sysroot capture linkage mismatch')
    m.reject('unrecorded clang resource headers', lambda: m.metadata(record, lambda v: v['cgo_toolchain']['resource_dir'].update(files={})), 'resource directory not recorded')
    m.reject('unrecorded clang shared-library closure', lambda: m.metadata(record, lambda v: v['cgo_toolchain']['clang'].update(libraries={})), 'host toolchain library closure not recorded')
    m.reject('foreign cgo compiler wrapper', lambda: m.put(CC_WRAPPER, b'#!/bin/sh\nexec /usr/bin/gcc "$@"\n'), 'daemon cgo compiler wrapper/toolchain linkage mismatch')


def proof_controls(m):
    m.reject('installation authority claim', lambda: m.metadata('installation-proposal.json', lambda v: v.update(execution_authorized=True)), 'installation proposal artifact/policy/authority mismatch')
    m.reject('false forced rebuild witness', lambda: m.metadata('evidence/daemon-clean-repro.json', lambda v: v.update(second_sha256='ab' * 32)), 'daemon forced rebuild proof mismatch')


def controls(package, pin, operator, baseline, output, cgo0_daemon):
    # The CGO0 control must use a real nocgo build, never a relabelled copy.
    if daemon_cgo.go_buildinfo(cgo0_daemon)['settings'].get('CGO_ENABLED') != '0':
        raise ValueError('--cgo0-daemon must be an actual CGO_ENABLED=0 FreeBSD daemon')
    positive = admit(package, pin, operator, baseline)
    if package == output or package in output.parents or output in package.parents:
        raise ValueError('disjoint candidate and control-output roots required')
    output.mkdir(mode=0o700)
    fixture = output / 'candidate'
    shutil.copytree(package, fixture)
    m = Mutator(fixture, operator, baseline)
    source_controls(m)
    exporter_controls(m)
    daemon_controls(m, cgo0_daemon)
    proof_controls(m)
    assert admit(fixture, pin, operator, baseline) == positive
    assert admit(package, pin, operator, baseline) == positive
    write_new(output / 'controls.json', {'actual_full_candidate_positive': positive,
        'cases': m.results, 'original_manifest_sha256': pin,
        'cgo0_daemon_sha256': digest(cgo0_daemon)[1],
        'restored_fixture_passed': True, 'original_passed_after_controls': True})
    print(json.dumps({'actual_full_candidate_controls': len(m.results), 'all_refused_for_expected_reason': True}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--manifest-sha256', required=True)
    parser.add_argument('--operator-public', required=True)
    parser.add_argument('--repository', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cgo0-daemon', type=Path, required=True,
                        help='actual CGO_ENABLED=0 FreeBSD daemon used read-only as a refusal control')
    args = parser.parse_args()
    controls(args.package.resolve(strict=True), args.manifest_sha256,
             args.operator_public, args.repository, args.output.absolute(),
             args.cgo0_daemon.resolve(strict=True))


if __name__ == '__main__':
    main()
