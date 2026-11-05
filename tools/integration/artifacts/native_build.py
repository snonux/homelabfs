#!/usr/local/bin/python3.12
# SPDX-License-Identifier: BSD-2-Clause
"""Private native full exporter recipe; coordinator reviews source capture first."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess

from elf import identity, strong_symbols

PREFLIGHT = 'build.preflight-v3.json'
REVISION = 'aadd58dddcbc78f4d5594827b46b5633552b15ce'


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for data in iter(lambda: stream.read(1 << 20), b''):
            h.update(data)
    return h.hexdigest()


def write_new(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def source_state(root):
    record = json.loads((root / 'source.inventory.json').read_text())
    if record['revision'] != REVISION:
        raise ValueError('unselected native source')
    source = root / 'source'
    actual = {str(path.relative_to(source)): sha(path)
              for path in sorted(source.rglob('*')) if path.is_file()}
    if actual != record['files']:
        raise ValueError('owned native source changed')
    return record


def binding(path):
    path = Path(path)
    info = path.lstat()
    return {'resolved': str(path.resolve(strict=True)), 'sha256': sha(path),
            'bytes': path.stat().st_size, 'uid': info.st_uid,
            'mode': info.st_mode & 0o7777,
            'symlink': os.readlink(path) if path.is_symlink() else None}


def loader_catalog(program, env):
    result = subprocess.run(['/usr/bin/ldd', str(program)], env=env,
                            text=True, capture_output=True, timeout=30)
    if result.returncode and 'statically linked' not in result.stderr + result.stdout:
        raise ValueError('existing native tool loader observation failed: ' + str(program))
    rows = {}
    for line in result.stdout.splitlines():
        match = re.match(r'\s*(\S+) => (/\S+) \(.*\)', line)
        if match:
            name, path = match.groups()
            actual = binding(Path(path))
            if name in rows and rows[name] != {'path': path, 'binding': actual}:
                raise ValueError('ambiguous native loader alias')
            rows[name] = {'path': path, 'binding': actual}
        elif 'not found' in line:
            raise ValueError('unresolved existing native tool dependency')
    return rows


def headers(root, env):
    resource = Path(subprocess.check_output(['/usr/bin/cc', '-print-resource-dir'],
                                            env=env, text=True, timeout=10).strip())
    rows, links = {}, {}
    for top in [Path('/usr/include'), Path('/usr/local/include'), resource / 'include']:
        if not top.exists():
            continue
        for directory, dirs, files in os.walk(top, followlinks=False):
            for name in dirs:
                path = Path(directory) / name
                if path.is_symlink():
                    links[str(path)] = os.readlink(path)
            for name in files:
                path = Path(directory) / name
                if path.is_file():
                    rows[str(path)] = binding(path)
    return {'files': rows, 'directory_symlinks': links}


def verify_inputs(pre, root):
    env = environment(root)
    for name, expected in pre['makefiles'].items():
        if binding(Path(name)) != expected:
            raise ValueError('consumed makefile changed')
    for name, expected in pre['toolchain'].items():
        if binding(Path(name)) != expected:
            raise ValueError('native binary tool changed')
    for program, expected in pre['loader_dependencies'].items():
        if loader_catalog(Path(program), env) != expected:
            raise ValueError('native tool/runtime loader or alias changed')
    if headers(root, env) != pre['binary_headers']:
        raise ValueError('binary sysroot/header/alias snapshot changed')


def dynamic_needed(path):
    # Pure ELF parsing: never invoke the newly built exporter or its loader.
    with path.open('rb') as stream:
        size = stream.seek(0, 2)
        if not 64 <= size <= 128 << 20:
            raise ValueError('ELF runtime bound')
        def read(offset, count):
            if offset < 0 or count < 0 or offset + count > size:
                raise ValueError('truncated dynamic ELF')
            stream.seek(offset)
            return stream.read(count)
        raw = read(0, 64)
        if raw[:7] != b'\x7fELF\x02\x01\x01':
            raise ValueError('ELF64 runtime required')
        h = struct.unpack('<HHIQQQIHHHHHH', raw[16:])
        if h[8] != 56 or h[9] > 1024:
            raise ValueError('ELF program table bound')
        program = [struct.unpack('<IIQQQQQQ', read(h[4] + n * 56, 56)) for n in range(h[9])]
        loads = [p for p in program if p[0] == 1]
        dyn = [p for p in program if p[0] == 2]
        interpreter = [read(p[2], p[5]).rstrip(b'\0').decode('ascii') for p in program if p[0] == 3]
        if len(dyn) != 1 or len(interpreter) > 1:
            raise ValueError('dynamic native exporter expected')
        values = {}
        if dyn[0][5] > 1 << 20 or dyn[0][5] % 16:
            raise ValueError('dynamic table bound')
        for offset in range(dyn[0][2], dyn[0][2] + dyn[0][5], 16):
            key, value = struct.unpack('<qQ', read(offset, 16))
            if key == 0:
                break
            values.setdefault(key, []).append(value)
        if values.get(15) or values.get(29):
            raise ValueError('unreviewed runtime RPATH/RUNPATH')
        if len(values.get(5, [])) != 1 or len(values.get(10, [])) != 1:
            raise ValueError('dynamic string table required')
        address, length = values[5][0], values[10][0]
        mapped = [p[2] + address - p[3] for p in loads if p[3] <= address and address + length <= p[3] + p[5]]
        if len(mapped) != 1 or length > 1 << 20:
            raise ValueError('dynamic string mapping bound')
        strings = read(mapped[0], length)
        needed = []
        for index in values.get(1, []):
            end = strings.find(b'\0', index)
            if index >= len(strings) or end < index:
                raise ValueError('dynamic needed name bound')
            name = strings[index:end].decode('ascii')
            if not re.fullmatch(r'lib[A-Za-z0-9_+.-]+\.so(?:\.[A-Za-z0-9_.-]+)*', name):
                raise ValueError('unselected runtime dependency name')
            needed.append(name)
        return needed, interpreter


def runtime_closure(exporter, catalog, target):
    required, interpreters = dynamic_needed(exporter)
    if 'lib9p.so.1' in required or 'libvmmapi.so.7' in required:
        raise ValueError('stock shared9p/vmmapi fallback')
    pending, found = list(required), {}
    while pending:
        name = pending.pop()
        if name in ('lib9p.so.1', 'libvmmapi.so.7'):
            raise ValueError('transitive stock9p/vmmapi fallback')
        if name in found:
            continue
        if name not in catalog:
            raise ValueError('new runtime dependency absent from measured native base catalog: ' + name)
        item = catalog[name]
        path = Path(item['path'])
        if binding(path) != item['binding']:
            raise ValueError('runtime alias/bytes changed')
        found[name] = item
        dependencies, _ = dynamic_needed(path)
        pending += dependencies
    external = {name: {'source': item['path'], 'binding': item['binding'],
                       'redistributed': False,
                       'provenance': 'measured binary base input; source ancestry and license not inferred'}
                for name, item in found.items()}
    for name in interpreters:
        external[name] = {'source': name, 'binding': binding(Path(name)),
                          'redistributed': False,
                          'provenance': 'measured binary base interpreter input'}
    return external


def environment(root):
    env = {key: os.environ[key] for key in ('HOME', 'USER', 'LOGNAME', 'TMPDIR') if key in os.environ}
    source = root / 'source'
    env.update(PATH='/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/bin', LC_ALL='C', TZ='UTC',
        MAKEOBJDIRPREFIX=str(root / 'objects'), SOURCE_DATE_EPOCH='1793750400',
        ZERO_AR_DATE='1',
        CFLAGS=f'-O2 -pipe -g0 -MD -I{source}/lib/libvmmapi '
               f'-ffile-prefix-map={root}=/usr/src/homelabfs-native '
               f'-fdebug-prefix-map={root}=/usr/src/homelabfs-native '
               f'-fmacro-prefix-map={root}=/usr/src/homelabfs-native',
        LDFLAGS='-Wl,--build-id=sha1 -Wl,-t')
    return env


def make_args(root, directory, library=False):
    source = root / 'source'
    objtop = root / 'objects' / str(source).lstrip('/')
    argv = ['/usr/bin/make', '-j1', '-m', str(source / 'share/mk'), '-C', str(directory),
            '__MAKE_CONF=/dev/null', 'SRCCONF=/dev/null', 'SRCTOP=' + str(source),
            'OBJTOP=' + str(objtop), 'MK_META_MODE=no', 'MK_DEBUG_FILES=no',
            'MK_BHYVE_SNAPSHOT=no', 'CC=/usr/bin/cc', 'CXX=/usr/bin/c++',
            'AR=/usr/bin/ar', 'ARFLAGS=crD', '_DP_9p=sbuf md casper cap_pwd cap_grp']
    if library:
        argv.append('INTERNALLIB=yes')
    else:
        ninep = objtop / 'lib/lib9p/lib9p_pie.a'
        vmm = objtop / 'lib/libvmmapi/libvmmapi_pie.a'
        argv += ['LIB9P=' + str(ninep), 'DPADD_9p=' + str(ninep),
                 'LDADD_9p=' + str(ninep) + ' -lsbuf -lmd -lcasper -lcap_pwd -lcap_grp',
                 'LIBVMMAPI=' + str(vmm), 'DPADD_vmmapi=' + str(vmm),
                 'LDADD_vmmapi=' + str(vmm) + ' -lutil',
                 'DPADD_nv=/usr/lib/libnv.so',
                 'DPADD_uvmem=/usr/lib/libuvmem.so']
    return argv, objtop


def preflight(root):
    state = source_state(root)
    env = environment(root)
    rows, consumed_makefiles = {}, {}
    for relative in ['lib/lib9p', 'lib/libvmmapi', 'usr.sbin/bhyve']:
        directory = root / 'source' / relative
        # Precreate every selected recursive make directory so bmake can never
        # fall back to writing object/generated files in the immutable sources.
        for current in [directory] + [p for p in directory.rglob('*') if p.is_dir()]:
            (root / 'objects' / str(current).lstrip('/')).mkdir(mode=0o700, parents=True, exist_ok=True)
        command, objtop = make_args(root, directory, library=relative.startswith('lib/'))
        names = ['.OBJDIR', 'SRCTOP', 'OBJTOP', 'LIBADD', '_DP_9p',
                 'MK_PIE', 'CFLAGS', 'LDFLAGS', 'SRCS', '.MAKE.MAKEFILES',
                 'LDADD', 'DPADD', 'LDADD_9p', 'DPADD_9p', 'LDADD_vmmapi', 'DPADD_vmmapi']
        values = []
        for name in names:
            values.append(subprocess.check_output(command + ['-v', name], env=env,
                                                 text=True, timeout=30).strip())
        row = dict(zip(names, values))
        wanted = root / 'objects' / str(directory).lstrip('/')
        if row['.OBJDIR'] != str(wanted) or row['SRCTOP'] != str(root / 'source') or row['OBJTOP'] != str(objtop) or row['MK_PIE'] != 'yes':
            raise ValueError('native object/source/PIE binding mismatch: ' + relative)
        if relative == 'lib/lib9p' and set(row['LIBADD'].split()) != {'sbuf', 'md', 'casper', 'cap_pwd', 'cap_grp'}:
            raise ValueError('actual owned9p dependencies differ from reviewed recipe')
        if relative == 'usr.sbin/bhyve':
            ninep = str(objtop / 'lib/lib9p/lib9p_pie.a')
            vmm = str(objtop / 'lib/libvmmapi/libvmmapi_pie.a')
            for key in ['LDADD', 'DPADD']:
                words = row[key].split()
                if ninep not in words or vmm not in words or '-l9p' in words or '-lvmmapi' in words:
                    raise ValueError('expanded actual native linkage does not bind ownedPIEarchives')
        if relative == 'usr.sbin/bhyve':
            for name in row['DPADD'].split():
                if name not in (ninep, vmm):
                    path = Path(name)
                    if not path.is_file() or root in path.parents:
                        raise ValueError('unavailable external native dependency: ' + name)
        for name in row['.MAKE.MAKEFILES'].split():
            path = Path(name) if name.startswith('/') else directory / name
            consumed_makefiles[str(path)] = binding(path)
        rows[relative] = row
    tools = {}
    for name in ('cc', 'c++', 'make', 'ar', 'ld', 'nm', 'objcopy', 'readelf', 'patch'):
        path = Path('/usr/bin') / name
        tools[str(path)] = binding(path)
    loader = {name: loader_catalog(Path(name), env) for name in tools}
    loader['/usr/sbin/bhyve'] = loader_catalog(Path('/usr/sbin/bhyve'), env)
    binary_headers = headers(root, env)
    source_state(root)
    write_new(root / PREFLIGHT, {'version': 'homelabfs-native-build-preflight/1',
        'source_inventory_sha256': sha(root / 'source.inventory.json'),
        'source_archive_sha256': state['archive_sha256'], 'environment': env,
        'make': rows, 'makefiles': consumed_makefiles, 'toolchain': tools,
        'loader_dependencies': loader, 'binary_headers': binary_headers, 'compiler_version': subprocess.check_output(
            ['/usr/bin/cc', '--version'], text=True), 'compiled': False})
    print('actual native source/object/library/PIE/make preflight protected; not compiled')


def build(root, action):
    pre = json.loads((root / PREFLIGHT).read_text())
    if sha(root / 'source.inventory.json') != pre['source_inventory_sha256']:
        raise ValueError('changed source preflight')
    verify_inputs(pre, root)
    source_state(root)
    relative = {'library9p': 'lib/lib9p', 'libraryvmmapi': 'lib/libvmmapi',
                'bhyve': 'usr.sbin/bhyve'}[action]
    command, _ = make_args(root, root / 'source' / relative, library=action != 'bhyve')
    subprocess.run(command + ['all'], env=environment(root), check=True, timeout=1190)
    verify_inputs(pre, root)
    source_state(root)
    write_new(root / ('completed-' + action + '.json'), {
        'argv': command + ['all'], 'source_inventory_sha256': pre['source_inventory_sha256'],
        'compiler_version': pre['compiler_version'], 'installed': False, 'executed_exporter': False})


def collect(root):
    state = source_state(root)
    pre = json.loads((root / PREFLIGHT).read_text())
    verify_inputs(pre, root)
    for action in ['library9p', 'libraryvmmapi', 'bhyve']:
        done = json.loads((root / ('completed-' + action + '.json')).read_text())
        if done['source_inventory_sha256'] != pre['source_inventory_sha256']:
            raise ValueError('native stage source/preflight mismatch')
    source = root / 'source'
    objtop = root / 'objects' / str(source).lstrip('/')
    target = root / 'package/freebsd'
    target.mkdir(mode=0o700, parents=True)
    outputs = {'bhyve': objtop / 'usr.sbin/bhyve/bhyve',
        'bhyve-slirp-helper': objtop / 'usr.sbin/bhyve/slirp/bhyve-slirp-helper',
        'lib9p.a': objtop / 'lib/lib9p/lib9p.a',
        'lib9p_pie.a': objtop / 'lib/lib9p/lib9p_pie.a',
        'libvmmapi.a': objtop / 'lib/libvmmapi/libvmmapi.a',
        'libvmmapi_pie.a': objtop / 'lib/libvmmapi/libvmmapi_pie.a'}
    for name, path in outputs.items():
        if (target / name).exists():
            raise ValueError('fresh full native artifacts required')
        shutil.copyfile(path, target / name)
        if name in ('bhyve', 'bhyve-slirp-helper'):
            (target / name).chmod(path.stat().st_mode & 0o755)
    exporter = identity(target / 'bhyve')
    if exporter['osabi'] != 9 or not exporter['gnu_build_id']:
        raise ValueError('actual full FreeBSD exporter identity missing')
    strong_symbols(target / 'bhyve', {'hfs9p_supervisor_start_tuple',
        'hfs9p_supervisor_prepare', 'hfs9p_pending_exchange', 'hfs9p_controller_claim'})
    sysroot = {}
    # -MD includes actual system/compiler headers. No installed header is
    # assigned guessed source ancestry merely because its OS version matches.
    for path in objtop.rglob('*'):
        if path.is_file() and (path.name.endswith('.d') or path.name.startswith('.depend.')):
            raw = path.read_text(errors='strict').replace('\\\n', ' ')
            for name in re.findall(r'(?<![^\s])(/[^\s\\]+)', raw):
                candidate = Path(name)
                if candidate.is_file() and root not in candidate.parents:
                    sysroot[str(candidate)] = binding(candidate)
    for log in (root / 'checks').glob('*/stdout'):
        for line in log.read_text(errors='strict').splitlines():
            text = line.strip().split('(', 1)[0]
            if text.startswith('/') and '\t' not in text and ' ' not in text:
                path = Path(text)
                if path.is_file() and root not in path.parents:
                    sysroot[str(path)] = binding(path)
    if not sysroot:
        raise ValueError('actual consumed binary sysroot closure missing')
    runtime = runtime_closure(target / 'bhyve', pre['loader_dependencies']['/usr/sbin/bhyve'], target)
    for subdirectory in ['kbdlayout', 'gdb']:
        for base in [source / 'usr.sbin/bhyve' / subdirectory, objtop / 'usr.sbin/bhyve' / subdirectory]:
            for path in base.iterdir():
                if path.is_file() and path.suffix not in ['.o', '.d'] and not path.name.startswith(('.', 'Makefile')):
                    destination = target / 'share/bhyve' / subdirectory / path.name
                    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                    if not destination.exists():shutil.copyfile(path, destination)
    artifacts = {'freebsd/' + str(path.relative_to(target)): sha(path) for path in target.rglob('*') if path.is_file()}
    write_new(target / 'build.json', {'version': 'homelabfs-freebsd-build/1',
        'revision': REVISION, 'source_files': state['files'], 'source_capture': state,
        'sysroot_files': sysroot, 'toolchain': pre['toolchain'],
        'preflight': pre, 'artifacts': artifacts, 'exporter': exporter,
        'runtime_libraries': runtime,
        'installed': False, 'executed_exporter': False})
    print('full source-owned exporter/library/helper artifacts staged; never executed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['preflight', 'library9p', 'libraryvmmapi', 'bhyve', 'collect'])
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    if root.parent != Path('/var/tmp') or not root.name.startswith('hfs-xz3-'):
        raise ValueError('approved private native root required')
    if args.action == 'preflight':
        preflight(root)
    elif args.action == 'collect':
        collect(root)
    else:
        build(root, args.action)


if __name__ == '__main__':
    main()
