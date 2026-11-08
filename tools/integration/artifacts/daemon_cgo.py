# SPDX-License-Identifier: BSD-2-Clause
"""Offline evidence for the dynamic CGO FreeBSD controlled runtime.

The FreeBSD process observer exists only in `process_freebsd.go`
(`//go:build freebsd && cgo`). Its counterpart `process_freebsd_nocgo.go`
(`freebsd && !cgo`) returns fsapi.Unsupported for every observeProcess, and
the runtime launches bhyve before observing it, so a CGO_ENABLED=0 daemon can
never reach native READY. This module therefore proves, without executing the
binary or its loader, that the daemon was built with cgo and links only the
recorded FreeBSD base runtime closure from a hashed sysroot capture:

* Go's own embedded build information says CGO_ENABLED=1 for freebsd/amd64;
* the cgo observer trampolines generated for `C.hfs_process` and the
  runtime/cgo entry `x_cgo_init` are defined symbols. The build constraints
  are mutually exclusive, so the observer trampoline cannot coexist with the
  nocgo stub;
* PT_INTERP is FreeBSD's /libexec/ld-elf.so.1 and DT_NEEDED equals the
  recorded closure, whose libraries match the sysroot capture hashes.

It also records the host side of the cross compile, which lies outside the
FreeBSD sysroot: clang's resource directory (searched before the sysroot, so
e.g. stddef.h comes from there), the host shared-library closure of clang,
ld.lld and the wrapper's interpreter, and the effective header search list,
which must stay inside the resource directory and the sysroot.

These parsers stay separate from elf.py and native_build.py so that the
already pinned native recipe bytes do not change.
"""
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import struct
import subprocess

from native_build import dynamic_needed
from package import HASH, digest, read_json

MAX_ELF = 128 << 20
INTERPRETER = '/libexec/ld-elf.so.1'
CAPTURE_VERSION = 'homelabfs-freebsd-cgo-sysroot/1'
# FreeBSD rtld's standard search path is /lib:/usr/lib.
LIBRARY_DIRECTORIES = ('lib', 'usr/lib')
CONTROLLED_RUNTIME = 'github.com/snonux/homelabfs/internal/controlledruntime'
CGO_SOURCE = 'process_freebsd.go'
CGO_HEADER = 'process_freebsd.h'
NOCGO_SOURCE = 'process_freebsd_nocgo.go'
GO_OBSERVER = re.compile(re.escape(CONTROLLED_RUNTIME) + r'\._Cfunc_hfs_process(?:\.abi0)?\Z')
C_OBSERVER = re.compile(r'_cgo_[0-9a-f]{12}_Cfunc_hfs_process\Z')
BUILDINFO_MAGIC = b'\xff Go buildinf:'


class _Reader:
    """Bounded random access to one ELF64 little-endian x86_64 file."""

    def __init__(self, stream):
        self.stream = stream
        self.size = stream.seek(0, 2)
        if not 64 <= self.size <= MAX_ELF:
            raise ValueError('ELF file bound')
        header = self.read(0, 64)
        if header[:7] != b'\x7fELF\x02\x01\x01':
            raise ValueError('ELF64 little-endian required')
        fields = struct.unpack('<HHIQQQIHHHHHH', header[16:])
        self.shoff, self.shentsize, self.shnum, self.shstrndx = fields[5], fields[10], fields[11], fields[12]
        if fields[1] != 62 or self.shentsize != 64 or not 1 <= self.shnum <= 32768 or self.shstrndx >= self.shnum:
            raise ValueError('ELF section table bound')

    def read(self, offset, length):
        if offset < 0 or length < 0 or offset + length > self.size:
            raise ValueError('truncated ELF')
        self.stream.seek(offset)
        raw = self.stream.read(length)
        if len(raw) != length:
            raise ValueError('truncated ELF')
        return raw

    def sections(self):
        raw = self.read(self.shoff, self.shnum * self.shentsize)
        return [struct.unpack_from('<IIQQQQIIQQ', raw, index * 64) for index in range(self.shnum)]

    def section_bytes(self, section, limit):
        if section[1] == 8:  # SHT_NOBITS occupies no file bytes
            return b''
        if section[5] > limit:
            raise ValueError('ELF section size bound')
        return self.read(section[4], section[5])

    def named_sections(self):
        sections = self.sections()
        names = self.section_bytes(sections[self.shstrndx], 1 << 20)
        result = []
        for section in sections:
            end = names.find(b'\0', section[0])
            if section[0] >= len(names) or end < 0:
                raise ValueError('ELF section name bound')
            result.append((names[section[0]:end].decode('ascii', errors='strict'), section))
        return sections, result


def defined_symbols(path):
    """Return every defined .symtab symbol name, of any binding.

    cgo's Go-side trampolines are local symbols, so elf.strong_symbols (global
    only) is insufficient here. A stripped binary has no .symtab and refuses.
    """
    with path.open('rb') as stream:
        reader = _Reader(stream)
        sections = reader.sections()
        found, tables = set(), 0
        for section in sections:
            if section[1] != 2:
                continue
            tables += 1
            if section[9] != 24 or section[5] % 24 or section[6] >= len(sections):
                raise ValueError('ELF symbol table bound')
            strings = sections[section[6]]
            if strings[1] != 3:
                raise ValueError('ELF symbol string table required')
            names = reader.section_bytes(strings, 16 << 20)
            symbols = reader.section_bytes(section, 32 << 20)
            for offset in range(0, len(symbols), 24):
                name, _, _, defined, _, _ = struct.unpack_from('<IBBHQQ', symbols, offset)
                if defined == 0 or name == 0:
                    continue
                end = names.find(b'\0', name)
                if name >= len(names) or end < 0:
                    raise ValueError('ELF symbol name bound')
                found.add(names[name:end].decode('utf-8', errors='strict'))
        if tables != 1:
            raise ValueError('exactly one unstripped ELF symbol table required')
        return found


def _uvarint(data, offset):
    value, shift = 0, 0
    while True:
        if offset >= len(data) or shift > 63:
            raise ValueError('Go build information varint')
        byte = data[offset]
        offset += 1
        value |= (byte & 0x7f) << shift
        if byte < 0x80:
            return value, offset
        shift += 7


def _string(data, offset):
    length, offset = _uvarint(data, offset)
    if offset + length > len(data):
        raise ValueError('Go build information string bound')
    return data[offset:offset + length], offset + length


def parse_modinfo(version, modinfo):
    """Parse runtime/debug.BuildInfo text: path/mod/dep lines and build settings."""
    # Go wraps modinfo in two binary 16-byte sentinels; the text ends with a
    # newline just before the trailing sentinel.
    if len(modinfo) < 33 or modinfo[-17:-16] != b'\n':
        raise ValueError('Go module information sentinel')
    result = {'go_version': version.decode('utf-8', errors='strict'), 'path': None,
              'main': None, 'deps': [], 'settings': {}}
    for line in modinfo[16:-16].decode('utf-8', errors='strict').splitlines():
        kind, separator, value = line.partition('\t')
        if not separator:
            raise ValueError('Go build information line')
        if kind == 'path':
            result['path'] = value
        elif kind == 'mod':
            result['main'] = value
        elif kind == 'dep':
            result['deps'].append(value)
        elif kind == 'build':
            key, equal, setting = value.partition('=')
            if not equal or key in result['settings']:
                raise ValueError('ambiguous Go build setting')
            result['settings'][key] = setting
    return result


def go_buildinfo(path):
    """Read Go's embedded .go.buildinfo (debug/buildinfo format, inline strings)."""
    with path.open('rb') as stream:
        reader = _Reader(stream)
        _, named = reader.named_sections()
        selected = [section for name, section in named if name == '.go.buildinfo']
        if len(selected) != 1:
            raise ValueError('exactly one Go build information section required')
        data = reader.section_bytes(selected[0], 1 << 20)
    # 32-byte header: magic, pointer size, flags. 0x2 means inline strings;
    # 0x1 would be big-endian, which is not the selected amd64 target.
    if len(data) < 32 or data[:14] != BUILDINFO_MAGIC or data[15] & 0x3 != 0x2:
        raise ValueError('unsupported Go build information header')
    version, offset = _string(data, 32)
    modinfo, _ = _string(data, offset)
    return parse_modinfo(version, modinfo)


def observe(path):
    """Collect the Go build settings and symbols used by check_cgo.

    Kept separate from observe_linkage so a static CGO0 binary is refused for
    its missing cgo build, not only for its missing dynamic section.
    """
    return {'buildinfo': go_buildinfo(path), 'symbols': defined_symbols(path)}


def observe_linkage(path):
    """Collect PT_INTERP and DT_NEEDED for check_closure."""
    needed, interpreters = dynamic_needed(path)
    return {'needed': needed, 'interpreters': interpreters}


def check_cgo(observed, go_version):
    """Refuse a CGO0 build, a stripped observer or a foreign target/toolchain."""
    settings = observed['buildinfo']['settings']
    if settings.get('CGO_ENABLED') != '1':
        raise ValueError('CGO-enabled FreeBSD daemon required; CGO0 cannot observe native processes')
    if (settings.get('GOOS'), settings.get('GOARCH'), settings.get('-trimpath'), settings.get('-buildmode')) != ('freebsd', 'amd64', 'true', 'exe'):
        raise ValueError('daemon Go build settings mismatch')
    if observed['buildinfo']['go_version'] != go_version or observed['buildinfo']['path'] != 'github.com/snonux/homelabfs/cmd/hfs-controlled-runtime':
        raise ValueError('daemon Go build information mismatch')
    symbols = observed['symbols']
    if 'x_cgo_init' not in symbols or not any(GO_OBSERVER.match(name) for name in symbols) or not any(C_OBSERVER.match(name) for name in symbols):
        raise ValueError('cgo process observer symbols missing')


def check_source_closure(dependencies):
    """The recorded Go closure must contain the cgo observer, never the stub."""
    files = (dependencies.get(CONTROLLED_RUNTIME) or {}).get('files') or {}
    if CGO_SOURCE not in files or CGO_HEADER not in files or NOCGO_SOURCE in files:
        raise ValueError('daemon source closure lacks the cgo process observer')


def _reachable(needed, libraries):
    pending, seen = list(needed), set()
    while pending:
        name = pending.pop()
        if name in seen:
            continue
        if name not in libraries:
            raise ValueError('daemon runtime closure missing library: ' + name)
        seen.add(name)
        pending += libraries[name]['needed']
    return seen


def check_closure(observed, closure, capture):
    """Exact interpreter/DT_NEEDED and transitive library hashes vs capture."""
    files = capture['files']
    interpreter = closure.get('interpreter') or {}
    if observed['interpreters'] != [INTERPRETER] or interpreter.get('path') != INTERPRETER:
        raise ValueError('foreign dynamic loader; FreeBSD ' + INTERPRETER + ' required')
    if (files.get(interpreter.get('sysroot_path')) or {}).get('sha256') != interpreter.get('sha256') or interpreter.get('sysroot_path') != INTERPRETER.lstrip('/'):
        raise ValueError('daemon interpreter sysroot hash mismatch')
    libraries = closure.get('libraries')
    if observed['needed'] != closure.get('needed') or not observed['needed'] or not isinstance(libraries, dict):
        raise ValueError('daemon DT_NEEDED does not match recorded closure')
    if _reachable(closure['needed'], libraries) != set(libraries):
        raise ValueError('daemon runtime closure contains unreachable library')
    for name, item in libraries.items():
        row = files.get(item.get('sysroot_path'))
        # Only rtld's standard search directories can satisfy DT_NEEDED.
        location = PurePosixPath(str(item.get('sysroot_path')))
        if row is None or location.name != name or str(location.parent) not in LIBRARY_DIRECTORIES or row.get('sha256') != item.get('sha256') or item.get('redistributed') is not False:
            raise ValueError('daemon runtime library sysroot hash mismatch: ' + name)


def verify_sysroot(root):
    """Verify a closed sysroot capture: exactly the listed files and hashes."""
    capture_path = root / 'capture.json'
    capture = read_json(capture_path)
    if capture.get('version') != CAPTURE_VERSION or not isinstance(capture.get('files'), dict) or not capture['files']:
        raise ValueError('unsupported sysroot capture')
    found = set()
    for path in root.rglob('*'):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError('sysroot capture contains link or special file')
        if path.is_file():
            found.add(path.relative_to(root).as_posix())
    if found != set(capture['files']) | {'capture.json'}:
        raise ValueError('sysroot capture file set differs from its inventory')
    for name, row in capture['files'].items():
        if digest(root / name) != (row.get('bytes'), row.get('sha256')):
            raise ValueError('sysroot capture bytes mismatch: ' + name)
    return capture, digest(capture_path)[1]


def resolve_closure(binary, root, capture):
    """Record the produced binary's interpreter and transitive base libraries."""
    needed, interpreters = dynamic_needed(binary)
    if interpreters != [INTERPRETER]:
        raise ValueError('foreign dynamic loader; FreeBSD ' + INTERPRETER + ' required')
    libraries, pending = {}, list(needed)
    while pending:
        name = pending.pop()
        if name in libraries:
            continue
        candidates = [directory + '/' + name for directory in LIBRARY_DIRECTORIES
                      if directory + '/' + name in capture['files']]
        if not candidates:
            raise ValueError('daemon library absent from sysroot capture: ' + name)
        location = candidates[0]
        dependencies, _ = dynamic_needed(root / location)
        libraries[name] = {'sysroot_path': location, 'sha256': capture['files'][location]['sha256'],
                           'needed': dependencies, 'redistributed': False}
        pending += dependencies
    location = INTERPRETER.lstrip('/')
    if location not in capture['files']:
        raise ValueError('FreeBSD dynamic loader absent from sysroot capture')
    return {'needed': needed, 'libraries': libraries,
            'interpreter': {'path': INTERPRETER, 'sysroot_path': location,
                            'sha256': capture['files'][location]['sha256']},
            'provenance': 'measured FreeBSD base binary inputs; external, not redistributed'}


# Host toolchain inputs outside the sysroot -----------------------------------

HOST_ENVIRONMENT = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}
SEARCH_START = '#include <...> search starts here:'
SEARCH_END = 'End of search list.'
MAX_TREE_FILES = 20000


def tree_hashes(directory):
    """Hash every regular file under one tool directory, recording links.

    Symlinked files are hashed by content and their link text is kept, so a
    retargeted alias with identical bytes still changes the record.
    """
    files, links, count = {}, {}, 0
    for current, dirs, names in os.walk(directory, followlinks=False):
        dirs.sort()
        for name in sorted(dirs + names):
            path = Path(current) / name
            relative = path.relative_to(directory).as_posix()
            if path.is_symlink():
                links[relative] = os.readlink(path)
            if name in names:
                if not path.is_file():
                    raise ValueError('special file in compiler resource directory: ' + relative)
                count += 1
                if count > MAX_TREE_FILES:
                    raise ValueError('compiler resource directory bound')
                files[relative] = digest(path)[1]
    return {'files': files, 'symlinks': links}


def parse_ldd(text):
    """Map each host dependency (including the ELF interpreter) to its path."""
    if 'statically linked' in text:
        return {}
    rows = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith('linux-vdso.so'):
            continue
        if 'not found' in line:
            raise ValueError('unresolved host toolchain library: ' + line)
        match = re.fullmatch(r'(\S+) => (/\S+) \(0x[0-9a-f]+\)', line) or re.fullmatch(r'((/\S+)) \(0x[0-9a-f]+\)', line)
        if not match:
            raise ValueError('unrecognised host loader line: ' + line)
        name, path = match.groups()
        if rows.setdefault(name, path) != path:
            raise ValueError('ambiguous host library alias: ' + name)
    return rows


def shared_libraries(program):
    """Host shared-library closure of an existing trusted tool, with hashes."""
    result = subprocess.run(['/usr/bin/ldd', str(program)], env=HOST_ENVIRONMENT, text=True,
                            capture_output=True, timeout=30)
    if result.returncode and 'statically linked' not in result.stdout + result.stderr:
        raise ValueError('host toolchain loader observation failed: ' + str(program))
    return {name: {'path': path, 'resolved': str(Path(path).resolve(strict=True)),
                   'sha256': digest(Path(path))[1]}
            for name, path in parse_ldd(result.stdout).items()}


def tool_binding(path):
    resolved = Path(path).resolve(strict=True)
    return {'path': str(path), 'resolved': str(resolved), 'sha256': digest(resolved)[1],
            'libraries': shared_libraries(resolved)}


def interpreter_bindings(wrapper_text):
    """The wrapper's `#!` interpreter (and env's target) as hashed host tools."""
    first = wrapper_text.splitlines()[0] if wrapper_text else ''
    words = first[2:].split() if first.startswith('#!') else []
    if not words or not words[0].startswith('/'):
        raise ValueError('cc wrapper needs an absolute #! interpreter')
    programs = [words[0]]
    if PurePosixPath(words[0]).name == 'env':
        if len(words) != 2:
            raise ValueError('cc wrapper env interpreter must name one program')
        found = shutil.which(words[1], path=HOST_ENVIRONMENT['PATH'])
        if not found:
            raise ValueError('cc wrapper interpreter not found: ' + words[1])
        programs.append(found)
    return {program: tool_binding(program) for program in programs}


def parse_search_dirs(stderr):
    lines = stderr.splitlines()
    if lines.count(SEARCH_START) != 1 or lines.count(SEARCH_END) != 1:
        raise ValueError('cc wrapper header search list not reported')
    start, end = lines.index(SEARCH_START), lines.index(SEARCH_END)
    directories = [line.strip() for line in lines[start + 1:end]] if start < end else []
    if not directories:
        raise ValueError('cc wrapper header search list empty')
    return directories


def check_search_dirs(directories, resource, sysroot):
    """Every angle-bracket include directory must be hashed resource or sysroot."""
    allowed = [Path(resource).resolve(strict=True), Path(sysroot).resolve(strict=True)]
    for directory in directories:
        actual = Path(directory).resolve(strict=True)
        if not any(actual == root or actual.is_relative_to(root) for root in allowed):
            raise ValueError('cc wrapper header search escapes resource dir and sysroot: ' + directory)


def compiler_environment(wrapper, sysroot):
    """Record clang's resource directory and the effective header search."""
    resource = subprocess.check_output([str(wrapper), '-print-resource-dir'], env=HOST_ENVIRONMENT,
                                       text=True, timeout=30).strip()
    if not resource.startswith('/'):
        raise ValueError('cc wrapper resource directory must be absolute')
    report = subprocess.run([str(wrapper), '-E', '-v', '-x', 'c', '/dev/null', '-o', '/dev/null'],
                            env=HOST_ENVIRONMENT, text=True, capture_output=True, timeout=30, check=True)
    directories = parse_search_dirs(report.stderr)
    check_search_dirs(directories, resource, sysroot)
    resolved = Path(resource).resolve(strict=True)
    return {'resource_dir': {'path': resource, 'resolved': str(resolved), **tree_hashes(resolved)},
            'include_search': directories}


def _hashed_libraries(value):
    return isinstance(value, dict) and value and all(
        isinstance(item, dict) and HASH.fullmatch(str(item.get('sha256'))) for item in value.values())


def check_host_toolchain(toolchain):
    """Admission shape check: host compiler inputs were recorded, not skipped.

    The host bytes are not packaged, so admission can only require that the
    builder recorded a complete, hashed resource tree and library closures.
    """
    resource = toolchain.get('resource_dir') or {}
    files = resource.get('files')
    if not isinstance(files, dict) or 'include/stddef.h' not in files or not all(HASH.fullmatch(str(v)) for v in files.values()):
        raise ValueError('daemon cgo compiler resource directory not recorded')
    if not isinstance(toolchain.get('include_search'), list) or not toolchain['include_search']:
        raise ValueError('daemon cgo header search list not recorded')
    interpreters = toolchain.get('interpreter') or {}
    tools = [toolchain.get('clang') or {}, toolchain.get('lld') or {}] + list(interpreters.values())
    if not interpreters or any(not _hashed_libraries(tool.get('libraries')) for tool in tools):
        raise ValueError('daemon cgo host toolchain library closure not recorded')
