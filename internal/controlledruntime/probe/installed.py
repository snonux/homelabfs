# Read-only installed-byte/boot probe. Executed with python3 -I through the
# pinned SSH channel; no caller arguments, shell expansion or mutable output file.
import hashlib, json, os, pathlib, struct, sys, signal
signal.alarm(8)
phase_only = sys.argv[1:] == ["phase"]
if sys.argv[1:] not in ([], ["phase"]):
    raise ValueError("probe invocation")

def read(path, limit):
    with open(path, 'rb') as f:
        value = f.read(limit + 1)
    if len(value) > limit:
        raise ValueError('probe bound')
    return value

def build_id(path):
    raw = read(path, 65536)
    offset, found = 0, []
    while offset < len(raw):
        if len(raw) - offset < 12:
            raise ValueError('truncated ELF note')
        namesz, descsz, kind = struct.unpack_from('<III', raw, offset)
        offset += 12
        name = raw[offset:offset+namesz]
        offset += (namesz + 3) & ~3
        desc = raw[offset:offset+descsz]
        offset += (descsz + 3) & ~3
        if offset > len(raw):
            raise ValueError('invalid ELF note')
        if kind == 3 and name == b'GNU\0':
            found.append(desc.hex())
    if len(found) != 1 or not 16 <= len(found[0]) <= 128:
        raise ValueError('missing/ambiguous build id')
    return found[0]

def file_hash(path):
    p = pathlib.Path(path)
    if not p.is_absolute() or '..' in p.parts:
        raise ValueError('artifact path')
    stat = os.stat(path, follow_symlinks=False)
    if not __import__('stat').S_ISREG(stat.st_mode) or stat.st_uid != 0 or stat.st_mode & 0o022 or stat.st_size > 128 << 20:
        raise ValueError('artifact ownership/bound')
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(65536), b''):
            h.update(chunk)
    return h.hexdigest()

if os.geteuid() != 0:
    raise ValueError('trusted guest root required')
manifest_raw = read('/etc/homelabfs/installed-admission-v1.json', 65536)
manifest = json.loads(manifest_raw)
if set(manifest) != {'version', 'kernel', 'modules', 'abi'} or manifest['version'] != 'homelabfs-installed-artifacts/1':
    raise ValueError('manifest shape')
kernel = manifest['kernel']
if set(kernel) != {'path', 'sha256', 'build_id'} or (not phase_only and file_hash(kernel['path']) != kernel['sha256']) or build_id('/sys/kernel/notes') != kernel['build_id']:
    raise ValueError('running kernel mismatch')
names = sorted(line.split()[0] for line in read('/proc/modules', 65536).decode('ascii').splitlines())
if len(names) > 256 or set(names) != set(manifest['modules']):
    raise ValueError('incomplete loaded module inventory')
modules = {}
for name in names:
    if not name.replace('_', '').isalnum():
        raise ValueError('module name')
    module = manifest['modules'][name]
    if set(module) != {'path', 'sha256', 'build_id'} or (not phase_only and file_hash(module['path']) != module['sha256']) or build_id('/sys/module/' + name + '/notes/.note.gnu.build-id') != module['build_id']:
        raise ValueError('loaded module mismatch')
    modules[name] = module['build_id']
boot = read('/proc/sys/kernel/random/boot_id', 64).decode('ascii').strip()
admission_raw = read('/sys/kernel/homelabfs/admission_v1', 131072)
admission = json.loads(admission_raw)
# Repeat the complete observations to reject a concurrent boot/module/inventory
# transition; module/session lifetime guarantees are independently mandatory.
if boot != read('/proc/sys/kernel/random/boot_id', 64).decode('ascii').strip() or admission_raw != read('/sys/kernel/homelabfs/admission_v1', 131072) or names != sorted(line.split()[0] for line in read('/proc/modules', 65536).decode('ascii').splitlines()):
    raise ValueError('unstable observation')
__import__('sys').stdout.write(json.dumps({'version':'homelabfs-installed-observation/1', 'manifest_hash':hashlib.sha256(manifest_raw).hexdigest(), 'boot_id':boot, 'kernel_build_id':kernel['build_id'], 'modules':modules, 'abi':manifest['abi'], 'admission':admission}, separators=(',', ':'), sort_keys=True))
