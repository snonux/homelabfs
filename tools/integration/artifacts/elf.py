# SPDX-License-Identifier: BSD-2-Clause
"""Read bounded ELF64 identities without running an artifact or its loader."""
import struct

MAX_ELF = 128 << 20


def identity(path):
    with path.open('rb') as stream:
        size = stream.seek(0, 2)
        if not 64 <= size <= MAX_ELF:
            raise ValueError('ELF file bound')

        def read(offset, length):
            if offset < 0 or length < 0 or offset + length > size:
                raise ValueError('truncated ELF')
            stream.seek(offset)
            raw = stream.read(length)
            if len(raw) != length:
                raise ValueError('truncated ELF')
            return raw

        header = read(0, 64)
        if header[:7] != b'\x7fELF\x02\x01\x01' or header[7] not in (0, 9):
            raise ValueError('ELF64 little-endian selected OS ABI required')
        fields = struct.unpack('<HHIQQQIHHHHHH', header[16:])
        kind, machine, version, _, phoff, shoff, _, ehsize, phsize, phnum, shsize, shnum, _ = fields
        if kind not in (1, 2, 3) or machine != 62 or version != 1 or ehsize != 64 or phnum > 1024 or shnum > 32768:
            raise ValueError('selected x86_64 ELF header required')
        ranges = set()
        if phnum:
            if phsize != 56:
                raise ValueError('ELF program table')
            for i in range(phnum):
                p = struct.unpack('<IIQQQQQQ', read(phoff + i * phsize, phsize))
                if p[0] == 4:
                    ranges.add((p[2], p[5]))
        if shnum:
            if shsize != 64:
                raise ValueError('ELF section table')
            for i in range(shnum):
                s = struct.unpack('<IIQQQQIIQQ', read(shoff + i * shsize, shsize))
                if s[1] == 7:
                    ranges.add((s[4], s[5]))
        notes, consumed = {}, 0
        for start, length in sorted(ranges):
            if length > 1 << 20:
                raise ValueError('ELF notes bound')
            offset = start
            end = start + length
            while offset < end:
                if end - offset < 12:
                    raise ValueError('truncated ELF note')
                namesz, descsz, note_type = struct.unpack('<III', read(offset, 12))
                next_offset = offset + 12 + ((namesz + 3) & ~3) + ((descsz + 3) & ~3)
                if next_offset > end:
                    raise ValueError('ELF note size')
                name = read(offset + 12, namesz)
                desc = read(offset + 12 + ((namesz + 3) & ~3), descsz)
                notes[offset] = (name, note_type, desc)
                offset = next_offset
                consumed += 1
                if consumed > 8192:
                    raise ValueError('ELF note count bound')
        gnu = [desc.hex() for name, typ, desc in notes.values()
               if name == b'GNU\0' and typ == 3]
        go = [desc.decode('ascii') for name, typ, desc in notes.values()
              if name == b'Go\0\0' and typ == 4]
        if len(gnu) > 1 or len(go) > 1:
            raise ValueError('ambiguous ELF build identity')
        return {'osabi': header[7], 'type': kind, 'machine': machine,
                'gnu_build_id': gnu[0] if gnu else None,
                'go_build_id': go[0] if go else None}


def strong_symbols(path, wanted):
    """Require defined global symbols in the retained unstripped full exporter."""
    with path.open('rb') as stream:
        size = stream.seek(0, 2)
        if not 64 <= size <= MAX_ELF:
            raise ValueError('ELF file bound')
        stream.seek(0)
        header = stream.read(64)
        if header[:7] != b'\x7fELF\x02\x01\x01':
            raise ValueError('ELF64 required')
        h = struct.unpack('<HHIQQQIHHHHHH', header[16:])
        offset, stride, count = h[5], h[10], h[11]
        if stride != 64 or not 1 <= count <= 32768 or offset + count * stride > size:
            raise ValueError('ELF section bound')
        stream.seek(offset)
        sections = [struct.unpack('<IIQQQQIIQQ', stream.read(stride))
                    for _ in range(count)]
        found = set()
        for section in sections:
            if section[1] != 2:
                continue
            start, length, linked, entry = section[4], section[5], section[6], section[9]
            if entry != 24 or length % entry or length > 32 << 20 or start + length > size or linked >= count:
                raise ValueError('ELF symbol table bound')
            strings = sections[linked]
            if strings[1] != 3 or strings[5] > 8 << 20 or strings[4] + strings[5] > size:
                raise ValueError('ELF symbol string bound')
            stream.seek(strings[4])
            names = stream.read(strings[5])
            stream.seek(start)
            for _ in range(length // entry):
                name, info, _, defined, _, _ = struct.unpack('<IBBHQQ', stream.read(entry))
                if name >= len(names):
                    raise ValueError('ELF symbol name')
                if info >> 4 == 1 and defined != 0:
                    end = names.find(b'\0', name)
                    if end < 0:
                        raise ValueError('ELF symbol name termination')
                    text = names[name:end].decode('utf-8', errors='strict')
                    if text in wanted:
                        found.add(text)
        if found != set(wanted):
            raise ValueError('missing strong production exporter symbols: ' + repr(set(wanted) - found))
