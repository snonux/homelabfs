#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Build the actual repository-native FreeBSD controlled runtime, without cgo."""
import argparse
import json
import os
import shutil
from pathlib import Path
import subprocess

from elf import identity
from kernel import write_new
from package import digest

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[2]


def dependencies(environment, goroot):
    raw = subprocess.check_output(['/usr/bin/go', 'list', '-deps', '-json',
                                   './cmd/hfs-controlled-runtime'],
                                  cwd=REPOSITORY, env=environment, text=True,
                                  timeout=180)
    decoder, offset, packages = json.JSONDecoder(), 0, {}
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        value, offset = decoder.raw_decode(raw, offset)
        if value.get('Error') or value.get('DepsErrors'):
            raise ValueError('incomplete Go source closure')
        root = Path(value['Dir'])
        names = sum((value.get(key, []) for key in
                     ['GoFiles', 'CgoFiles', 'CFiles', 'HFiles', 'SFiles',
                      'SysoFiles', 'EmbedFiles']), [])
        files = {name: digest(root / name)[1] for name in names}
        # Preserve available dependency licensing alongside the exact package
        # source paths. Module-level licenses are often above package dirs.
        module = value.get('Module') or {}
        module_root = Path(module['Dir']) if module.get('Dir') else Path(goroot) if value.get('Standard') else root
        licenses = {str(path): digest(path)[1]
                    for pattern in ['LICENSE*', 'LICENCE*', 'COPYING*', 'NOTICE*']
                    for path in module_root.glob(pattern) if path.is_file()}
        packages[value['ImportPath']] = {'directory': str(root), 'files': files,
                                       'module': module, 'licenses': licenses}
    if 'github.com/snonux/homelabfs/internal/controlledruntime' not in packages:
        raise ValueError('actual controlled runtime missing')
    return packages


def compiler_binding(environment):
    version = subprocess.check_output(['/usr/bin/go', 'version'], env=environment, text=True).strip()
    if version.split()[2].partition('-X:')[0] != 'go1.26.10':
        raise ValueError('explicit selected Go1.26.10 toolchain required')
    actual = json.loads(subprocess.check_output(['/usr/bin/go', 'env', '-json',
        'GOROOT', 'GOTOOLDIR', 'GOVERSION', 'GOENV', 'GOWORK'], env=environment, text=True))
    if environment.get('GOENV') != 'off' or actual['GOENV'] not in ('', 'off') or actual['GOWORK'] != 'off':
        raise ValueError('ambient Go workspace/settings not disabled')
    directory = Path(actual['GOTOOLDIR']).resolve(strict=True)
    binaries = {'go': Path('/usr/bin/go').resolve(strict=True)}
    binaries.update({name: (directory / name).resolve(strict=True)
                     for name in ['compile', 'asm', 'link']})
    tools = {name: {'path': str(path), 'sha256': digest(path)[1]}
             for name, path in binaries.items()}
    licenses = {str(path): digest(path)[1]
                for pattern in ['LICENSE*', 'PATENTS*', 'NOTICE*']
                for path in Path(actual['GOROOT']).glob(pattern) if path.is_file()}
    return {'version': version, 'selected_environment': actual,
            'binaries': tools, 'licenses': licenses}


def verify_modules(environment):
    result = subprocess.run(['/usr/bin/go', 'mod', 'verify'], cwd=REPOSITORY,
                            env=environment, check=True, text=True,
                            capture_output=True, timeout=180)
    return {'stdout': result.stdout, 'stderr': result.stderr}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--prepare', action='store_true', help='download only selected target dependencies into explicit private GOMODCACHE')
    args = parser.parse_args()
    package = args.package.resolve(strict=True)
    directory = package / 'freebsd'
    directory.mkdir(mode=0o700, exist_ok=True)
    target = directory / 'hfs-controlled-runtime'
    record = directory / 'daemon-build.json'
    if target.exists() or record.exists():
        raise ValueError('fresh daemon outputs required; preserve old resources')
    environment = {key: os.environ[key] for key in
                   ['HOME', 'TMPDIR', 'GOTMPDIR', 'GOCACHE', 'GOMODCACHE',
                    'GOMAXPROCS', 'GOMEMLIMIT', 'GOGC'] if key in os.environ}
    environment.update(PATH='/usr/bin:/bin', LC_ALL='C', TZ='UTC',
                       GOOS='freebsd', GOARCH='amd64', CGO_ENABLED='0',
                       GOFLAGS='-p=1 -mod=readonly', GOTOOLCHAIN='local', GOENV='off',
                       GOWORK='off', GOPROXY='off')
    if not environment.get('GOMODCACHE') or not Path(environment['GOMODCACHE']).is_absolute():
        raise ValueError('explicit private module-cache path required')
    if args.prepare:
        environment['GOPROXY'] = 'https://proxy.golang.org'
        toolchain = compiler_binding(environment)
        inputs = dependencies(environment, toolchain['selected_environment']['GOROOT'])
        verified = verify_modules(environment)
        write_new(package / 'daemon-preparation.json', {'dependencies': inputs, 'toolchain': toolchain, 'module_verification': verified, 'compiled': False})
        print('selected daemon sources/dependencies prepared in explicit private cache; not compiled')
        return
    source_revision = subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], cwd=REPOSITORY, text=True).strip()
    toolchain = compiler_binding(environment)
    module_files = {name: digest(REPOSITORY / name)[1] for name in ['go.mod', 'go.sum']}
    verified_before = verify_modules(environment)
    inputs = dependencies(environment, toolchain['selected_environment']['GOROOT'])
    command = ['/usr/bin/go', 'build', '-a', '-trimpath', '-buildvcs=false', '-o',
               str(target), './cmd/hfs-controlled-runtime']
    subprocess.run(command, cwd=REPOSITORY, env=environment, check=True, timeout=1190)
    verified_after = verify_modules(environment)
    if dependencies(environment, toolchain['selected_environment']['GOROOT']) != inputs or compiler_binding(environment) != toolchain or {name: digest(REPOSITORY / name)[1] for name in ['go.mod', 'go.sum']} != module_files:
        raise ValueError('Go source/module/compiler closure changed during build')
    observed = identity(target)
    if observed['osabi'] != 9 or not observed['go_build_id']:
        raise ValueError('FreeBSD Go build identity required')
    licenses = dict(toolchain['licenses'])
    for dependency in inputs.values():
        licenses.update(dependency['licenses'])
    copied = {}
    for name, expected in sorted(licenses.items()):
        path = Path(name)
        destination = package / 'licenses/go' / (expected + '-' + path.name)
        destination.parent.mkdir(parents=True, exist_ok=True)
        if not destination.exists():
            shutil.copyfile(path, destination)
        if digest(destination)[1] != expected:
            raise ValueError('Go license bytes changed')
        copied[str(destination.relative_to(package))] = expected
    write_new(record, {'licenses': copied, 'version': 'homelabfs-daemon-build/1',
        'repository': source_revision, 'goos': 'freebsd', 'goarch': 'amd64',
        'cgo_enabled': '0', 'go_version': toolchain['version'],
        'toolchain': toolchain, 'module_verify_before': verified_before,
        'module_verify_after': verified_after,
        'compiler_sha256': digest(Path('/usr/bin/go'))[1],
        'go_mod_sha256': digest(REPOSITORY / 'go.mod')[1],
        'go_sum_sha256': digest(REPOSITORY / 'go.sum')[1],
        'dependencies': inputs, 'command': command, 'environment': environment,
        'identity': observed,
        'artifacts': {'freebsd/hfs-controlled-runtime': digest(target)[1]},
        'installed': False})
    print('actual FreeBSD controlled runtime staged; never started')


if __name__ == '__main__':
    main()
