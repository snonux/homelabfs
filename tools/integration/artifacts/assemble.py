#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Seal actual built components and retained proofs. No install/run operations."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess

from admit import admit
from kernel import write_new
from native_source import OWNED
from package import digest, read_json, seal
from proposal import pinned_proposal

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[2]


def copy(source, destination):
    if source.is_symlink() or not source.is_file():
        raise ValueError('regular retained input required')
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if destination.exists():
        if digest(source) != digest(destination):
            raise ValueError('existing retained input differs: ' + str(destination))
        return
    shutil.copyfile(source, destination)


def assemble(package, evidence, operator, physical):
    if (package / 'manifest.json').exists():
        raise ValueError('preserve sealed candidates; use a fresh package')
    checkout = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPOSITORY, text=True).strip()
    native = read_json(package / 'freebsd/build.json')
    daemon = read_json(package / 'freebsd/daemon-build.json')
    baseline = daemon['repository']
    linux = read_json(package / 'linux/build.json')
    capture = read_json(evidence / 'native-final-source-and-first-seal.json')
    native_repro = read_json(evidence / 'native-clean-repro-evidence.json')['repro']
    daemon_repro = read_json(evidence / 'daemon-clean-repro.json')
    first_binding = read_json(evidence / 'checks/daemon-build-private-first/binding.json')
    second_binding = read_json(evidence / 'checks/daemon-build-private-clean-repro/binding.json')
    bindings = capture['recipe_sha256']
    actual = {'native_build_v3.py': 'native_build.py', 'native_source_v2.py': 'native_source.py',
              'native_guard_v3.py': 'native_guard.py', 'elf.py': 'elf.py'}
    for name, local in actual.items():
        if digest(HERE / local)[1] != bindings[name]:
            raise ValueError('actual native recipe differs from candidate recipe')
        copy(HERE / local, package / 'inputs/build-recipes' / local)
    if digest(HERE / 'kernel.py')[1] != linux['inputs']['recipe_sha256']:
        raise ValueError('actual kernel recipe differs')
    daemon_path = str(HERE / 'daemon.py')
    if first_binding[daemon_path] != digest(HERE / 'daemon.py')[1] or second_binding[daemon_path] != first_binding[daemon_path]:
        raise ValueError('actual daemon recipe differs')
    copy(HERE / 'daemon.py', package / 'inputs/build-recipes/daemon.py')
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
    write_new(package / 'inputs/toolchains.json', {'linux': linux['inputs']['toolchain'],
        'freebsd': native['toolchain'], 'go': daemon['toolchain']})
    write_new(package / 'inputs/sysroot.json', {'native_consumed': native['sysroot_files'],
        'native_runtime_external': native['runtime_libraries'],
        'provenance': 'measured binary inputs only; no inferred source ancestry or redistribution'})
    write_new(package / 'evidence/native-clean-repro.json', native_repro)
    write_new(package / 'evidence/daemon-clean-repro.json', daemon_repro)
    write_new(package / 'evidence/actual-builder-bindings.json', {
        'native': bindings, 'kernel_recipe': linux['inputs']['recipe_sha256'],
        'daemon_recipe': first_binding[daemon_path], 'native_bundle_recipe': bundle_binding})
    write_new(package / 'installation-proposal.json', pinned_proposal(package, physical, operator))
    inventory = {}
    for path in sorted(package.rglob('*')):
        if not path.is_file():
            continue
        name = path.relative_to(package).as_posix()
        if name == 'inputs/toolchains.json': kind = 'toolchain'
        elif name == 'inputs/sysroot.json': kind = 'sysroot'
        elif name.startswith(('licenses/', 'inputs/licenses/')): kind = 'license'
        elif name.endswith('.patch') or name.endswith('/series'): kind = 'patch'
        elif name.startswith(('inputs/build-recipes/', 'inputs/admission-recipes/')): kind = 'recipe'
        elif name == 'linux/config': kind = 'config'
        elif name.startswith('linux/modules/') and name.endswith('.ko'): kind = 'module'
        elif name in ('linux/vmlinux', 'linux/bzImage'): kind = 'kernel'
        elif name == 'freebsd/hfs-controlled-runtime': kind = 'daemon'
        elif name in ('freebsd/bhyve', 'freebsd/bhyve-slirp-helper') or name.startswith('freebsd/share/'): kind = 'exporter'
        elif name.startswith('freebsd/') and name.endswith('.a'): kind = 'runtime-library'
        elif name == 'installation-proposal.json': kind = 'proposal'
        elif name.startswith('inputs/'): kind = 'source'
        else: kind = 'build-record'
        inventory[name] = kind
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
