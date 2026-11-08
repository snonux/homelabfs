#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Seal actual built components and retained proofs. No install/run operations."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess

from admit import CC_WRAPPER, SYSROOT_CAPTURE, admit
from kernel import write_new
from native_source import OWNED
from package import digest, read_json, seal
from proposal import pinned_proposal

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[2]
# daemon.py imports daemon_cgo.py for its cgo/closure evidence, so both are
# daemon build recipes and both must match the retained builder bindings.
DAEMON_RECIPES = ('daemon.py', 'daemon_cgo.py')


def copy(source, destination):
    if source.is_symlink() or not source.is_file():
        raise ValueError('regular retained input required')
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if destination.exists():
        if digest(source) != digest(destination):
            raise ValueError('existing retained input differs: ' + str(destination))
        return
    shutil.copyfile(source, destination)


def copy_native_inputs(package, evidence, native, linux, bindings):
    """Native/kernel recipes, owned native sources and patch, as built."""
    actual = {'native_build_v3.py': 'native_build.py', 'native_source_v2.py': 'native_source.py',
              'native_guard_v3.py': 'native_guard.py', 'elf.py': 'elf.py'}
    for name, local in actual.items():
        if digest(HERE / local)[1] != bindings[name]:
            raise ValueError('actual native recipe differs from candidate recipe')
        copy(HERE / local, package / 'inputs/build-recipes' / local)
    if digest(HERE / 'kernel.py')[1] != linux['inputs']['recipe_sha256']:
        raise ValueError('actual kernel recipe differs')
    copy(HERE.parent / 'linux9p/tests/common/runner_process.py', package / 'inputs/build-recipes/runner_process.py')
    if digest(package / 'inputs/build-recipes/runner_process.py')[1] != bindings['runner_process.py']:
        raise ValueError('native process helper differs')
    bundle_binding = read_json(evidence / 'native-bundle-helper-transfer.json')['sha256']
    if digest(HERE / 'native_bundle.py')[1] != bundle_binding:
        raise ValueError('actual native bundler differs')
    copy(HERE / 'native_bundle.py', package / 'inputs/build-recipes/native_bundle.py')
    seed = native['source_capture']['seed']['files']
    integration = HERE.parent / 'lib9p'
    for name in OWNED:
        source = integration / 'src' / name
        if digest(source)[1] != seed['src/' + name]:
            raise ValueError('actual owned native source differs')
        copy(source, package / 'inputs/freebsd-owned/src' / name)
    patch = integration / 'patches/0001-dedicated-backend.patch'
    if digest(patch)[1] != seed['0001-dedicated-backend.patch']:
        raise ValueError('actual native patch differs')
    copy(patch, package / 'inputs/freebsd-owned/patches' / patch.name)
    return seed, bundle_binding


def copy_daemon_inputs(package, evidence, daemon):
    """Daemon recipes plus the exact cgo wrapper and sysroot capture manifest.

    The wrapper and capture manifest come from the paths recorded by
    daemon.py and must still hash to the recorded values. Sysroot libraries
    themselves stay external: only their capture inventory is packaged so
    admission can compare the daemon's runtime closure against it.
    """
    first = read_json(evidence / 'checks/daemon-build-private-first/binding.json')
    second = read_json(evidence / 'checks/daemon-build-private-clean-repro/binding.json')
    recipes = {}
    for name in DAEMON_RECIPES:
        path = str(HERE / name)
        if first[path] != digest(HERE / name)[1] or second[path] != first[path]:
            raise ValueError('actual daemon recipe differs: ' + name)
        copy(HERE / name, package / 'inputs/build-recipes' / name)
        recipes[name] = first[path]
    wrapper = Path(daemon['cgo_toolchain']['wrapper']['path'])
    if digest(wrapper)[1] != daemon['cgo_toolchain']['wrapper']['sha256']:
        raise ValueError('daemon cgo compiler wrapper differs from build record')
    copy(wrapper, package / CC_WRAPPER)
    capture = Path(daemon['sysroot']['root']) / 'capture.json'
    if digest(capture)[1] != daemon['sysroot']['capture_sha256']:
        raise ValueError('daemon sysroot capture differs from build record')
    copy(capture, package / SYSROOT_CAPTURE)
    return recipes


def write_repository_source(package, checkout, baseline, seed, daemon):
    copy(REPOSITORY / 'LICENSE', package / 'licenses/HomelabFS-LICENSE')
    new_recipes = {}
    for path in sorted(HERE.iterdir()):
        if path.is_file() and path.suffix in ('.py', '.config', '.md'):
            name = 'inputs/admission-recipes/' + path.name
            copy(path, package / name)
            new_recipes[name] = digest(path)[1]
    tracked = ['tools/integration/linux9p/export.py', 'tools/integration/linux9p/source.json',
               'tools/integration/linux9p/patches/0004-hfs1-physical-views.patch',
               'tools/integration/lib9p/owned-inputs.json', 'tools/integration/lib9p/src/hfs9p.c',
               'tools/integration/lib9p/src/hfs9p_supervisor.c',
               'tools/integration/lib9p/tests/backend_test.c', 'tools/integration/lib9p/tests/terminal_test.c',
               'tools/integration/lib9p/verify.sh']
    diff = subprocess.check_output(['git', 'diff', '--binary', baseline, '--', *tracked], cwd=REPOSITORY)
    if diff:
        with (package / 'inputs/repository-changes.patch').open('xb') as output:
            output.write(diff)
    write_new(package / 'inputs/repository-source.json', {
        'repository_baseline': baseline, 'source_checkout': checkout,
        'meaning': 'baseline revision only; actual changed source and new recipes are separately pinned below',
        'maintained_changes_sha256': digest(package / 'inputs/repository-changes.patch')[1] if diff else None,
        'new_recipe_files': new_recipes, 'native_owned_seed': seed,
        'daemon_repository_baseline': daemon['repository']})


def write_records(package, evidence, records, bindings, physical, operator):
    native, daemon, linux = records['native'], records['daemon'], records['linux']
    write_new(package / 'inputs/toolchains.json', {'linux': linux['inputs']['toolchain'],
        'freebsd': native['toolchain'], 'go': daemon['toolchain'],
        'go_cgo': daemon['cgo_toolchain']})
    write_new(package / 'inputs/sysroot.json', {'native_consumed': native['sysroot_files'],
        'native_runtime_external': native['runtime_libraries'],
        'daemon_cgo_sysroot': daemon['sysroot'],
        'daemon_runtime_external': daemon['runtime_closure'],
        'provenance': 'measured binary inputs only; no inferred source ancestry or redistribution'})
    write_new(package / 'evidence/native-clean-repro.json',
              read_json(evidence / 'native-clean-repro-evidence.json')['repro'])
    write_new(package / 'evidence/daemon-clean-repro.json', read_json(evidence / 'daemon-clean-repro.json'))
    write_new(package / 'evidence/actual-builder-bindings.json', {
        'native': bindings['native'], 'kernel_recipe': linux['inputs']['recipe_sha256'],
        'daemon_recipe': bindings['daemon']['daemon.py'],
        'daemon_cgo_recipe': bindings['daemon']['daemon_cgo.py'],
        'native_bundle_recipe': bindings['native_bundle']})
    write_new(package / 'installation-proposal.json', pinned_proposal(package, physical, operator))


def inventory_kind(name):
    if name == 'inputs/toolchains.json': return 'toolchain'
    if name in ('inputs/sysroot.json', SYSROOT_CAPTURE): return 'sysroot'
    if name.startswith(('licenses/', 'inputs/licenses/')): return 'license'
    if name.endswith('.patch') or name.endswith('/series'): return 'patch'
    if name.startswith(('inputs/build-recipes/', 'inputs/admission-recipes/')): return 'recipe'
    if name == 'linux/config': return 'config'
    if name.startswith('linux/modules/') and name.endswith('.ko'): return 'module'
    if name in ('linux/vmlinux', 'linux/bzImage'): return 'kernel'
    if name == 'freebsd/hfs-controlled-runtime': return 'daemon'
    if name in ('freebsd/bhyve', 'freebsd/bhyve-slirp-helper') or name.startswith('freebsd/share/'): return 'exporter'
    if name.startswith('freebsd/') and name.endswith('.a'): return 'runtime-library'
    if name == 'installation-proposal.json': return 'proposal'
    if name.startswith('inputs/'): return 'source'
    return 'build-record'


def assemble(package, evidence, operator, physical):
    if (package / 'manifest.json').exists():
        raise ValueError('preserve sealed candidates; use a fresh package')
    checkout = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPOSITORY, text=True).strip()
    records = {'native': read_json(package / 'freebsd/build.json'),
               'daemon': read_json(package / 'freebsd/daemon-build.json'),
               'linux': read_json(package / 'linux/build.json')}
    baseline = records['daemon']['repository']
    native_bindings = read_json(evidence / 'native-final-source-and-first-seal.json')['recipe_sha256']
    seed, bundle_binding = copy_native_inputs(package, evidence, records['native'],
                                              records['linux'], native_bindings)
    daemon_bindings = copy_daemon_inputs(package, evidence, records['daemon'])
    write_repository_source(package, checkout, baseline, seed, records['daemon'])
    write_records(package, evidence, records, {'native': native_bindings, 'daemon': daemon_bindings,
                  'native_bundle': bundle_binding}, physical, operator)
    inventory = {path.relative_to(package).as_posix(): inventory_kind(path.relative_to(package).as_posix())
                 for path in sorted(package.rglob('*')) if path.is_file()}
    pin = seal(package, inventory, operator, baseline)
    result = admit(package, pin, operator, baseline)
    record = {'manifest_sha256': pin, 'repository_baseline': baseline, 'source_checkout': checkout,
              'operator_public': operator, 'result': result, 'files': len(inventory)}
    write_new(evidence / 'candidate-admission.json', record)
    print(json.dumps(record, sort_keys=True))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--evidence-root', type=Path, required=True)
    parser.add_argument('--operator-public', required=True)
    parser.add_argument('--physical-bytes', type=int, required=True)
    args = parser.parse_args()
    # Proofs and actual components must already exist. This never builds them.
    (args.package / 'evidence').mkdir(mode=0o700, exist_ok=False)
    assemble(args.package.resolve(strict=True), args.evidence_root.resolve(strict=True),
             args.operator_public, args.physical_bytes)


if __name__ == '__main__':
    main()
