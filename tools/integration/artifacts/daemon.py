#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Build the actual repository-native FreeBSD controlled runtime with cgo.

CGO is mandatory: the CGO_ENABLED=0 FreeBSD build compiles
process_freebsd_nocgo.go, whose observeProcess always returns
fsapi.Unsupported, so that daemon starts bhyve and then can never reach native
READY. The cgo build links process_freebsd.go/process_freebsd.h against the
FreeBSD libc ABI from a hashed sysroot capture through an explicit pinned
cross C compiler wrapper. Every compiler, wrapper, sysroot and runtime-closure
input is an explicit absolute argument and is recorded by hash. The produced
binary is parsed, never executed.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

import daemon_cgo
from elf import identity
from kernel import write_new
from package import digest

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[2]
VERSION = 'homelabfs-daemon-build/2'
TARGET = 'x86_64-unknown-freebsd15.1'
# Go's defaults for CGO_CFLAGS/CGO_LDFLAGS, stated explicitly so no ambient
# value can enter; the prefix map keeps sysroot paths out of C debug info.
CGO_FLAGS = '-O2 -g'
SAFE_PATH = re.compile(r'/[A-Za-z0-9_./+-]+\Z')


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
        packages[value['ImportPath']] = package_closure(value, goroot)
    if daemon_cgo.CONTROLLED_RUNTIME not in packages:
        raise ValueError('actual controlled runtime missing')
    # With CGO_ENABLED=1 the closure must select the cgo observer files.
    daemon_cgo.check_source_closure(packages)
    return packages


def package_closure(value, goroot):
    """Hash one package's exact source files plus its module licenses."""
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
    return {'directory': str(root), 'files': files, 'module': module,
            'licenses': licenses}


def compiler_binding(environment):
    version = subprocess.check_output(['/usr/bin/go', 'version'], env=environment, text=True).strip()
    if version.split()[2].partition('-X:')[0] != 'go1.26.10':
        raise ValueError('explicit selected Go1.26.10 toolchain required')
    actual = json.loads(subprocess.check_output(['/usr/bin/go', 'env', '-json',
        'GOROOT', 'GOTOOLDIR', 'GOVERSION', 'GOENV', 'GOWORK', 'CC',
        'CGO_ENABLED', 'CGO_CFLAGS', 'CGO_CPPFLAGS', 'CGO_LDFLAGS'], env=environment, text=True))
    if environment.get('GOENV') != 'off' or actual['GOENV'] not in ('', 'off') or actual['GOWORK'] != 'off':
        raise ValueError('ambient Go workspace/settings not disabled')
    if actual['CGO_ENABLED'] != '1' or actual['CC'] != environment['CC']:
        raise ValueError('selected cgo compiler not effective')
    directory = Path(actual['GOTOOLDIR']).resolve(strict=True)
    binaries = {'go': Path('/usr/bin/go').resolve(strict=True)}
    binaries.update({name: (directory / name).resolve(strict=True)
                     for name in ['compile', 'asm', 'link', 'cgo']})
    tools = {name: {'path': str(path), 'sha256': digest(path)[1]}
             for name, path in binaries.items()}
    licenses = {str(path): digest(path)[1]
                for pattern in ['LICENSE*', 'PATENTS*', 'NOTICE*']
                for path in Path(actual['GOROOT']).glob(pattern) if path.is_file()}
    return {'version': version, 'selected_environment': actual,
            'binaries': tools, 'licenses': licenses}


def explicit_path(value, kind):
    """Require an explicit absolute, shell/flag-safe, existing path."""
    if not SAFE_PATH.fullmatch(str(value)):
        raise ValueError('explicit absolute safe path required: ' + kind)
    return value.resolve(strict=True)


def tool_record(path, version_argument):
    resolved = path.resolve(strict=True)
    banner = subprocess.check_output([str(path), version_argument], text=True,
                                     timeout=30, env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'})
    return {'path': str(path), 'resolved': str(resolved),
            'sha256': digest(resolved)[1], 'version': banner.splitlines()[0]}


def cgo_toolchain(args):
    """Bind the wrapper bytes to the explicit clang, ld.lld and sysroot.

    The wrapper is a pinned build input, so its text must name exactly the
    tools and sysroot recorded here, disable ambient clang configuration and
    select the FreeBSD 15.1 target. Otherwise its recorded hash would not
    explain what actually compiled and linked the daemon.
    """
    wrapper = explicit_path(args.cc_wrapper, 'cc wrapper')
    text = wrapper.read_text()
    for required in ['exec ' + str(args.clang) + ' --no-default-config',
                     '--target=' + TARGET, '--sysroot=' + str(args.sysroot) + ' ',
                     '--ld-path=' + str(args.lld) + ')']:
        if required not in text:
            raise ValueError('cc wrapper does not bind selected input: ' + required)
    size, value = digest(wrapper)
    return {'wrapper': {'path': str(wrapper), 'bytes': size, 'sha256': value},
            'clang': tool_record(args.clang, '--version'),
            'lld': tool_record(args.lld, '--version'), 'target': TARGET}


def build_environment(args):
    environment = {key: os.environ[key] for key in
                   ['HOME', 'TMPDIR', 'GOTMPDIR', 'GOCACHE', 'GOMODCACHE',
                    'GOMAXPROCS', 'GOMEMLIMIT', 'GOGC'] if key in os.environ}
    environment.update(PATH='/usr/bin:/bin', LC_ALL='C', TZ='UTC',
                       GOOS='freebsd', GOARCH='amd64', CGO_ENABLED='1',
                       CC=str(args.cc_wrapper),
                       CGO_CFLAGS=CGO_FLAGS + ' -ffile-prefix-map=' + str(args.sysroot) + '/=/',
                       CGO_CPPFLAGS='', CGO_LDFLAGS=CGO_FLAGS,
                       GOFLAGS='-p=1 -mod=readonly', GOTOOLCHAIN='local', GOENV='off',
                       GOWORK='off', GOPROXY='off')
    if not environment.get('GOMODCACHE') or not Path(environment['GOMODCACHE']).is_absolute():
        raise ValueError('explicit private module-cache path required')
    return environment


def verify_modules(environment):
    result = subprocess.run(['/usr/bin/go', 'mod', 'verify'], cwd=REPOSITORY,
                            env=environment, check=True, text=True,
                            capture_output=True, timeout=180)
    return {'stdout': result.stdout, 'stderr': result.stderr}


def inputs_snapshot(args, environment):
    """Every input whose change during the build must refuse the result."""
    toolchain = compiler_binding(environment)
    capture, capture_sha = daemon_cgo.verify_sysroot(args.sysroot)
    return {'toolchain': toolchain, 'cgo_toolchain': cgo_toolchain(args),
            'sysroot': {'root': str(args.sysroot), 'capture_sha256': capture_sha,
                        'capture_version': capture['version'],
                        'platform': capture.get('platform'),
                        'files': len(capture['files'])},
            'modules': {name: digest(REPOSITORY / name)[1] for name in ['go.mod', 'go.sum']},
            'dependencies': dependencies(environment, toolchain['selected_environment']['GOROOT'])}, capture


def prepare(package, args, environment):
    environment['GOPROXY'] = 'https://proxy.golang.org'
    snapshot, _ = inputs_snapshot(args, environment)
    verified = verify_modules(environment)
    write_new(package / 'daemon-preparation.json', {**snapshot, 'module_verification': verified, 'compiled': False})
    print('selected daemon sources/dependencies prepared in explicit private cache; not compiled')


def copy_licenses(package, toolchain, inputs):
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
    return copied


def observe_output(target, snapshot, capture, sysroot):
    """Parse the produced daemon: identity, cgo evidence and runtime closure."""
    observed = identity(target)
    if observed['osabi'] != 9 or not observed['go_build_id']:
        raise ValueError('FreeBSD Go build identity required')
    facts = daemon_cgo.observe(target)
    daemon_cgo.check_cgo(facts, snapshot['toolchain']['version'].split()[2])
    closure = daemon_cgo.resolve_closure(target, sysroot, capture)
    daemon_cgo.check_closure(daemon_cgo.observe_linkage(target), closure, capture)
    return observed, facts, closure


def build(package, args, environment):
    directory = package / 'freebsd'
    target = directory / 'hfs-controlled-runtime'
    record = directory / 'daemon-build.json'
    source_revision = subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], cwd=REPOSITORY, text=True).strip()
    snapshot, capture = inputs_snapshot(args, environment)
    verified_before = verify_modules(environment)
    command = ['/usr/bin/go', 'build', '-a', '-trimpath', '-buildvcs=false', '-o',
               str(target), './cmd/hfs-controlled-runtime']
    subprocess.run(command, cwd=REPOSITORY, env=environment, check=True, timeout=1190)
    verified_after = verify_modules(environment)
    if inputs_snapshot(args, environment)[0] != snapshot:
        raise ValueError('Go/cgo source, module, compiler or sysroot closure changed during build')
    observed, facts, closure = observe_output(target, snapshot, capture, args.sysroot)
    toolchain = snapshot['toolchain']
    write_new(record, {'licenses': copy_licenses(package, toolchain, snapshot['dependencies']),
        'version': VERSION, 'repository': source_revision, 'goos': 'freebsd',
        'goarch': 'amd64', 'cgo_enabled': '1', 'go_version': toolchain['version'],
        'toolchain': toolchain, 'cgo_toolchain': snapshot['cgo_toolchain'],
        'sysroot': snapshot['sysroot'], 'runtime_closure': closure,
        'go_buildinfo': facts['buildinfo'],
        'module_verify_before': verified_before, 'module_verify_after': verified_after,
        'compiler_sha256': digest(Path('/usr/bin/go'))[1],
        'go_mod_sha256': snapshot['modules']['go.mod'],
        'go_sum_sha256': snapshot['modules']['go.sum'],
        'dependencies': snapshot['dependencies'], 'command': command,
        'environment': environment, 'identity': observed,
        'artifacts': {'freebsd/hfs-controlled-runtime': digest(target)[1]},
        'installed': False})
    print('actual dynamic CGO FreeBSD controlled runtime staged; never started')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--cc-wrapper', type=Path, required=True,
                        help='absolute pinned FreeBSD cross C compiler wrapper')
    parser.add_argument('--sysroot', type=Path, required=True,
                        help='absolute captured FreeBSD sysroot containing capture.json')
    parser.add_argument('--clang', type=Path, required=True, help='absolute clang named by the wrapper')
    parser.add_argument('--lld', type=Path, required=True, help='absolute ld.lld named by the wrapper')
    parser.add_argument('--prepare', action='store_true', help='download only selected target dependencies into explicit private GOMODCACHE')
    args = parser.parse_args()
    for name in ('cc_wrapper', 'sysroot', 'clang', 'lld'):
        explicit_path(getattr(args, name), name)
    package = args.package.resolve(strict=True)
    directory = package / 'freebsd'
    directory.mkdir(mode=0o700, exist_ok=True)
    if (directory / 'hfs-controlled-runtime').exists() or (directory / 'daemon-build.json').exists():
        raise ValueError('fresh daemon outputs required; preserve old resources')
    environment = build_environment(args)
    if args.prepare:
        prepare(package, args, environment)
    else:
        build(package, args, environment)


if __name__ == '__main__':
    main()
