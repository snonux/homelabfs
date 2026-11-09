#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Actual signed installer/snapshot primitives and kernel TU preflight only."""
import argparse
import base64
import copy
import ctypes
import ctypes.util
import hashlib
import json
from pathlib import Path
import shlex
import subprocess

def encoded(value, *, canonical=False):
    """Original Go struct order or recursively sorted local canonical bytes."""
    return json.dumps(value, ensure_ascii=False, sort_keys=canonical,
                      separators=(',', ':')).encode()


class FixtureSigner:
    """Disposable deterministic214 seeds; real OpenSSL Ed25519, no key files."""

    def __init__(self):
        self.crypto = ctypes.CDLL(ctypes.util.find_library('crypto'))
        signatures = {
            'EVP_PKEY_new_raw_private_key': ([ctypes.c_int, ctypes.c_void_p,
                                             ctypes.c_void_p, ctypes.c_size_t],
                                            ctypes.c_void_p),
            'EVP_MD_CTX_new': ([], ctypes.c_void_p),
            'EVP_DigestSignInit': ([ctypes.c_void_p] * 5, ctypes.c_int),
            'EVP_DigestSign': ([ctypes.c_void_p, ctypes.c_void_p,
                               ctypes.POINTER(ctypes.c_size_t), ctypes.c_void_p,
                               ctypes.c_size_t], ctypes.c_int),
            'EVP_MD_CTX_free': ([ctypes.c_void_p], None),
            'EVP_PKEY_free': ([ctypes.c_void_p], None),
        }
        for name, (arguments, result) in signatures.items():
            function = getattr(self.crypto, name)
            function.argtypes, function.restype = arguments, result
        self.cache = {}

    def sign(self, seed, message):
        if (seed, message) in self.cache:
            return self.cache[seed, message]
        # OpenSSL NID_ED25519, the same EVP primitive used by the C driver.
        key = self.crypto.EVP_PKEY_new_raw_private_key(
            1087, None, bytes([seed]) * 32, 32)
        context = self.crypto.EVP_MD_CTX_new()
        signature = ctypes.create_string_buffer(64)
        length = ctypes.c_size_t(64)
        try:
            assert key and context
            assert self.crypto.EVP_DigestSignInit(
                context, None, None, None, key) == 1
            assert self.crypto.EVP_DigestSign(
                context, signature, ctypes.byref(length), message, len(message)) == 1
            assert length.value == 64
        finally:
            self.crypto.EVP_MD_CTX_free(context)
            self.crypto.EVP_PKEY_free(key)
        result = signature.raw.hex()
        self.cache[seed, message] = result
        return result

    def domains(self, group, message):
        return [{'guest': self.sign(11 + 2 * (group * 3 + domain), message),
                 'host': self.sign(10 + 2 * (group * 3 + domain), message)}
                for domain in range(3)]

    def outer(self, payload):
        raw = encoded(payload, canonical=True)
        return {'group_signatures': [self.domains(i, raw) for i in range(2)],
                'operator_signature': self.sign(1, raw), 'payload': payload}

    def fixture(self, original, enrollments, raw_overrides=None):
        result = copy.deepcopy(original)
        payload = result['provision']['payload']
        result['enrollment_hashes'], result['profile_hashes'] = [], []
        for i, enrollment in enumerate(enrollments):
            raw = (raw_overrides or {}).get(i, encoded(enrollment))
            group = payload['groups'][i]
            group['enrollment_bytes'] = raw.hex()
            group['enrollment_operator_signature'] = self.sign(1, raw)
            group['enrollment_domain_signatures'] = self.domains(i, raw)
            result['enrollment_hashes'].append(hashlib.sha256(raw).hexdigest())
            profile = {name: enrollment[name] for name in ['Domains', 'Bounds', 'PageSize']
                       if name in enrollment}
            profile['Families'] = enrollment['TicketFamilies']
            result['profile_hashes'].append(
                hashlib.sha256(encoded(profile, canonical=True)).hexdigest())
        result['provision'] = self.outer(payload)
        provision_hash = hashlib.sha256(encoded(payload, canonical=True)).hexdigest()
        for i, bind in enumerate(result['binds']):
            bind['payload']['provision_hash'] = provision_hash
            raw = encoded(bind['payload'], canonical=True)
            bind['operator_signature'] = self.sign(1, raw)
            bind['domain_signatures'] = self.domains(i, raw)
        return result


def expected_oracle(fixture):
    """Unsigned test expectations, derived only from signed fixture inputs.

    This sidecar is neither provisioning input nor runtime observation evidence.
    It does not consume installed C registry or production serializer output.
    """
    payload = fixture['provision']['payload']
    rows, protocols, scopes = [], {}, []
    for group_index, group in enumerate(payload['groups']):
        enrollment = json.loads(bytes.fromhex(group['enrollment_bytes']))
        for local in group['local_mounts']:
            domain_index = local['domain_index']
            domain = enrollment['Domains'][domain_index]
            family = enrollment['TicketFamilies'][domain_index]
            expected = copy.deepcopy(local)
            expected.update(group_index=group_index, context=domain['Scope'],
                            acquisition_anchor=family['Acquisition']['Anchor'],
                            acquisition_nonce=family['Acquisition']['Nonce'],
                            control_anchor=family['Control']['Anchor'])
            assert local['session'] == domain['Scope']['session']
            assert local['view_control_nonce'] == family['Control']['Nonce']
            rows.append(expected)
            scopes.append(copy.deepcopy(domain['Scope']))
            protocols[domain['Scope']['session']] = copy.deepcopy(domain['Protocol'])
    initial = {'boot_id': payload['boot_id'], 'provider_nonce': payload['provider_nonce'],
               'physical_contract': 'homelabfs-stop-adoption/1',
               'version': 'homelabfs-kernel-admission/1',
               'domains': scopes, 'domain_protocols': protocols,
               'observations': [{'scope': copy.deepcopy(scope), 'revision': 1,
                                 'fence': 1, 'phase': 'LIVE', 'event_hash': '',
                                 'acknowledgement_hash': ''} for scope in scopes]}
    revised = copy.deepcopy(initial)
    revised['observations'][0]['revision'] = 9
    return {'scope': 'unsigned fixture-derived test oracle; no runtime authority',
            'mounts': rows, 'initial': initial, 'revised': revised}


def fixture_cases(original):
    """Re-sign every affected layer; preserve malformed original bytes verbatim."""
    signer = FixtureSigner()
    enrollments = [json.loads(bytes.fromhex(g['enrollment_bytes']))
                   for g in original['provision']['payload']['groups']]
    # Reconstructing the unchanged producer fixture must be byte-equivalent at
    # every signed layer, including the independent host profile hash recipe.
    assert signer.fixture(original, enrollments) == original
    cases = []

    def original_case(name, change):
        modified = copy.deepcopy(enrollments)
        change(modified)
        cases.append((name, signer.fixture(original, modified), True))

    def payload_case(name, change):
        result = signer.fixture(original, enrollments)
        change(result['provision']['payload'])
        result['provision'] = signer.outer(result['provision']['payload'])
        cases.append((name, result, True))

    # Complete original field order is intentional. Only group1 identities change.
    distinct = copy.deepcopy(enrollments)
    group = distinct[1]
    def uuid(number):
        return f'{number:08x}-0000-0000-0000-{number:012x}'
    def go_id(number):
        return list(bytes.fromhex(uuid(number).replace('-', '')))
    group['Baseline']['Position']['Volume'] = go_id(1010)
    for domain in group['Domains']:
        domain['Scope']['volume'] = uuid(1010)
        domain['Scope']['origin_journal'] = uuid(1007)
        domain['Scope']['origin_incarnation'] = uuid(1006)
        domain['Journal']['Stream']['Volume'] = go_id(1010)
        domain['Journal']['Incarnation'] = go_id(1007)
    cases.append(('distinct-volume-journal', signer.fixture(original, distinct), False))

    def profile_name(items, value):
        for domain in items[0]['Domains']:
            domain['Profile']['Name'] = value
    for name, value in [('empty', ''), ('long', 'x' * 256),
                        ('control', 'bad\tname'), ('unicode', 'café'),
                        ('html-left', '<bad'), ('html-right', 'bad>'), ('html-and', 'bad&')]:
        original_case('profile-' + name, lambda e, v=value: profile_name(e, v))
    for name, value in [('one', 'x'), ('max', 'x' * 255),
                        ('escaped', '"\\ printable ~')]:
        modified = copy.deepcopy(enrollments)
        profile_name(modified, value)
        cases.append(('profile-' + name, signer.fixture(original, modified), False))

    original_case('original-extra-field', lambda e: e[0].update(Unknown=1))
    original_case('original-missing-field', lambda e: e[0].pop('PageSize'))
    original_case('original-field-order', lambda e: e[0].update(
        TicketFamilies=e[0].pop('TicketFamilies')))
    original_case('original-duplicate-session', lambda e: e[0]['Domains'][2]['Scope'].update(
        session=e[0]['Domains'][0]['Scope']['session']))
    original_case('original-base64-hex', lambda e: e[0]['Domains'][0].update(
        HostKey=base64.b64decode(e[0]['Domains'][0]['HostKey']).hex()))
    original_case('original-base64-padding', lambda e: e[0]['Domains'][0].update(
        HostKey=e[0]['Domains'][0]['HostKey'].rstrip('=')))
    original_case('original-zero-id', lambda e: e[0].update(ID=[0]*16))
    original_case('duplicate-group-id', lambda e: e[1].update(ID=e[0]['ID']))
    # Match changed original session in its local row to reach global uniqueness.
    modified = copy.deepcopy(enrollments)
    modified[1]['Domains'][0]['Scope']['session'] = modified[0]['Domains'][0]['Scope']['session']
    result = signer.fixture(original, modified)
    result['provision']['payload']['groups'][1]['local_mounts'][0]['session'] = modified[0]['Domains'][0]['Scope']['session']
    result['provision'] = signer.outer(result['provision']['payload'])
    cases.append(('duplicate-global-session', result, True))

    for field in ['device_tag', 'device_name']:
        payload_case('duplicate-' + field, lambda p, f=field:
                     p['groups'][1]['local_mounts'][0].update(
                         {f: p['groups'][0]['local_mounts'][0][f]}))
    payload_case('missing-local-row', lambda p: p['groups'][0]['local_mounts'].pop())
    payload_case('reordered-local-rows', lambda p: p['groups'][0]['local_mounts'].reverse())
    payload_case('extra-local-row', lambda p: p['groups'][0]['local_mounts'].append(
        copy.deepcopy(p['groups'][0]['local_mounts'][0])))
    for field in ['boot_id', 'provider_nonce']:
        payload_case('stale-' + field, lambda p, f=field: p.update({f: uuid(9999)}))
    # 32 is below the w54 mount slot floor for this fixture's four mounts.
    for field, value in [('reservations', 0), ('reservations', 32), ('reservations', 8193),
                         ('cookies', 4097), ('received', 0), ('received', 32),
                         ('ordinary_bytes', 1 << 30)]:
        payload_case('budget-' + field + '-' + str(value),
                     lambda p, f=field, v=value: p['budget'].update({f: v}))
    payload_case('ordinary-acquisition-lane', lambda p:
                 p['groups'][0]['local_mounts'][0].update(
                     anchor=enrollments[0]['TicketFamilies'][0]['Acquisition']['Anchor'],
                     nonce=enrollments[0]['TicketFamilies'][0]['Acquisition']['Nonce']))

    for layer in ['enrollment_domain_signatures', 'group_signatures']:
        for mutation in ['missing', 'extra', 'reordered', 'remote-bad']:
            result = signer.fixture(original, enrollments)
            rows = (result['provision']['payload']['groups'][0][layer]
                    if layer.startswith('enrollment') else result['provision'][layer][0])
            if mutation == 'missing':
                rows.pop(1)  # The omitted signer belongs to the remote domain.
            elif mutation == 'extra':
                rows.append(copy.deepcopy(rows[1]))
            elif mutation == 'reordered':
                rows[0], rows[1] = rows[1], rows[0]
            else:
                rows[1]['guest'] = '00' * 64
            if layer.startswith('enrollment'):
                result['provision'] = signer.outer(result['provision']['payload'])
            cases.append((layer + '-' + mutation, result, True))
    for field in ['enrollment_operator_signature', 'enrollment_bytes']:
        for mutation in ['uppercase', 'odd', 'invalid']:
            result = signer.fixture(original, enrollments)
            row = result['provision']['payload']['groups'][0]
            value = row[field]
            row[field] = {'uppercase': value.upper(), 'odd': value[:-1],
                          'invalid': 'z' + value[1:]}[mutation]
            result['provision'] = signer.outer(result['provision']['payload'])
            cases.append((field + '-' + mutation, result, True))
    raw = encoded(enrollments[0])
    duplicate = raw.replace(b'"PageSize":4096', b'"PageSize":4096,"PageSize":4096')
    cases.append(('original-duplicate-field',
                  signer.fixture(original, enrollments, {0: duplicate}), True))
    original_case('original-profile-field-order', lambda e:
                  e[0]['Domains'][0]['Profile'].update(
                      Disposable=e[0]['Domains'][0]['Profile'].pop('Disposable')))
    original_case('original-endpoint-nodes', lambda e:
                  e[0]['Domains'][0]['Endpoint'].update(command_node=3))

    for _, result, refuses in cases:
        if refuses:
            continue
        result['rejected_binds'] = []
        for mutation in ['boot_id', 'provider_nonce', 'provision_hash', 'group_id',
                         'operator', 'missing', 'extra', 'reordered', 'remote-bad']:
            bind = copy.deepcopy(result['binds'][0])
            if mutation in bind['payload']:
                bind['payload'][mutation] = ('f' * 64 if mutation == 'provision_hash'
                                             else uuid(9999))
            raw = encoded(bind['payload'], canonical=True)
            bind['operator_signature'] = signer.sign(1, raw)
            bind['domain_signatures'] = signer.domains(0, raw)
            rows = bind['domain_signatures']
            if mutation == 'operator':
                bind['operator_signature'] = '00' * 64
            elif mutation == 'missing':
                rows.pop(1)
            elif mutation == 'extra':
                rows.append(copy.deepcopy(rows[1]))
            elif mutation == 'reordered':
                rows.reverse()
            elif mutation == 'remote-bad':
                rows[1]['host'] = '00' * 64
            result['rejected_binds'].append(bind)
    cases[0][1]['distinct_identities'] = True
    return cases


p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--headers', type=Path, required=True)
p.add_argument('--fixture', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
source = a.source.resolve(strict=True)
headers = a.headers.resolve(strict=True)
fixture = a.fixture.resolve(strict=True)
out = a.output.absolute()
out.mkdir(mode=0o700, exist_ok=False)
here = Path(__file__).resolve().parent
repo = here.parents[4]
fixture_bytes = fixture.read_bytes()
public = json.loads(fixture_bytes)['operator_public_key']
(out/'fixture.json').write_bytes(fixture_bytes)
compat = (here.parent/'codec/compat.h').read_text().replace(
    'typedef uint64_t u64;', 'typedef unsigned long long u64;')
compat += '\n#include <assert.h>\n#include <sys/stat.h>\n#include <openssl/evp.h>\n'
compat += '#define CONFIG_9P_FS_HOMELABFS_OPERATOR_KEY "'+public+'"\n'
(out/'base_compat.h').write_text(compat)
(out/'controlled_compat.h').write_bytes((here/'compat.h').read_bytes())
declarations = (source/'fs/9p/hfs.h').read_text()
parts = ['#define HFS_CONTEXT_MAX 4096U']
for declaration in ['enum hfs_view_scope {', 'struct hfs_enrollment {']:
    start = declarations.index(declaration)
    end = declarations.index('\n};', start)+3
    parts.append(declarations[start:end])
(out/'enrollment_struct.inc').write_text('\n'.join(parts)+'\n')
session_parts = ['typedef pthread_mutex_t spinlock_t;',
                 'struct list_head { struct list_head *next, *prev; };']
for declaration in ['struct hfs_cell {', 'struct hfs_session {']:
    start = declarations.index(declaration)
    end = declarations.index('\n};', start)+3
    session_parts.append(declarations[start:end])
(out/'session_struct.inc').write_text('\n'.join(session_parts)+'\n')
crypto = source/'fs/9p/hfs_crypto'
commands = []
for unit in ['hfs_crypto_core', 'hfs_crypto_ed25519']:
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-fwhole-program', '-fsanitize=address,undefined',
               '-fno-omit-frame-pointer', '-fno-pie',
               '-I'+str(source/'fs/9p'), '-I'+str(crypto/'vendor/src'),
               '-c', str(source/('fs/9p/'+unit+'.c')), '-o', str(out/(unit+'.o'))]
    commands.append(command)
    subprocess.run(command, check=True, timeout=120)
command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
           '-Wno-unused-parameter', '-Wno-sign-compare', '-Wno-misleading-indentation',
           '-DHFS_CODEC_USERSPACE', '-DHFS_PROVIDER_USERSPACE',
           '-fsanitize=address,undefined', '-fno-omit-frame-pointer',
           '-fno-pie', '-no-pie', '-I'+str(out), '-I'+str(source/'fs/9p'),
           '-I'+str(crypto/'vendor/src'), '-include', str(out/'base_compat.h'),
           '-include', str(out/'controlled_compat.h'),
           '-include', str(here.parent/'common/kernel_format.h'),
           str(here/'test.c'), str(source/'fs/9p/hfs_protocol.c'),
           str(source/'fs/9p/hfs_policy.c'), str(source/'fs/9p/hfs_enrollment.c'),
           str(out/'hfs_crypto_core.o'), str(out/'hfs_crypto_ed25519.o'),
           str(here.parent/'common/kernel_format.c'), '-pthread', '-lcrypto',
           '-o', str(out/'installer-test')]
# Whole-program elimination is required only for unchanged crypto wrapper TUs.
# Other production helpers are included/called through the actual test driver.
subprocess.run(command, check=True, timeout=120)
commands.append(command)
# Preserve the reviewed214 bytes/hash exactly. Its original1MiBcontrol pool
# cannot fund the explicit8MiBaccepted-operation staging reserve.
original_fixture = json.loads(fixture_bytes)
subprocess.run([str(out/'installer-test'), str(out/'fixture.json'), 'budget-refuse'],
               check=True, timeout=120)
funded_input = copy.deepcopy(original_fixture)
funded_input['provision']['payload']['budget']['control_bytes'] = 16 << 20
# Its 32 reservations/received are also below the w54 slot floor (four
# mounts need at most 760/120); fund those too.
funded_input['provision']['payload']['budget']['reservations'] = 1024
funded_input['provision']['payload']['budget']['received'] = 256
enrollments = [json.loads(bytes.fromhex(group['enrollment_bytes']))
               for group in original_fixture['provision']['payload']['groups']]
funded_fixture = FixtureSigner().fixture(funded_input, enrollments)
for original_group, funded_group in zip(original_fixture['provision']['payload']['groups'],
                                       funded_fixture['provision']['payload']['groups']):
    for field in ['enrollment_bytes', 'enrollment_operator_signature',
                  'enrollment_domain_signatures']:
        assert original_group[field] == funded_group[field]
funded_path = out/'funded-fixture.json'
funded_path.write_bytes(encoded(funded_fixture, canonical=True))
baseline_oracle = out/'fixture.oracle.json'
baseline_oracle.write_bytes(encoded(expected_oracle(funded_fixture), canonical=True))
subprocess.run([str(out/'installer-test'), str(funded_path), 'accept',
                str(baseline_oracle)], check=True, timeout=120)
# Fresh processes retain the real install-once contract; there is no registry reset.
case_summary = []
for name, case, refuses in fixture_cases(funded_fixture):
    path = out/(name+'.json')
    path.write_bytes(encoded(case, canonical=True))
    arguments = [str(out/'installer-test'), str(path),
                 'refuse' if refuses else 'accept']
    oracle_path = None
    if not refuses:
        oracle_path = out/(name+'.oracle.json')
        oracle_path.write_bytes(encoded(expected_oracle(case), canonical=True))
        arguments.append(str(oracle_path))
    subprocess.run(arguments, check=True, timeout=120)
    case_summary.append({'name': name, 'refuses': refuses,
                         'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                         'oracle_sha256': (hashlib.sha256(oracle_path.read_bytes()).hexdigest()
                                           if oracle_path else None)})
(out/'cases.json').write_text(json.dumps(case_summary, indent=2)+'\n')

raw = (headers/'fs/9p/.hfs_session.o.cmd').read_text().splitlines()[0]
original = shlex.split(raw.split(' := ', 1)[1].split(' ; ', 1)[0])
for name in ['fs/9p/hfs_protocol', 'fs/9p/hfs_enrollment',
             'fs/9p/hfs_admission', 'fs/9p/hfs_controlled_artifacts',
             'net/9p/trans_virtio']:
    command = []
    for word in original:
        if word == '-DMODULE':
            continue
        if word.startswith('-Wp,-MMD,'):
            word = '-Wp,-MMD,'+str(out/(Path(name).name+'.d'))
        elif word.startswith('-I./'):
            word = '-I'+str(headers/word[2:])
        elif word.startswith('./'):
            word = str(headers/word)
        elif word == original[-1]:
            word = str(source/(name+'.c'))
        elif word == 'fs/9p/hfs_session.o':
            word = str(out/(Path(name).name+'.o'))
        elif word.startswith('-DKBUILD_BASENAME='):
            word = '-DKBUILD_BASENAME="'+Path(name).name+'"'
        elif word.startswith('-DKBUILD_MODNAME='):
            word = '-DKBUILD_MODNAME="'+Path(name).name+'"'
        command.append(word)
    stock = Path(original[-1]).parents[2]
    command[1:1] = ['-I'+str(source/'include'), '-I'+str(stock/'net/9p'),
                    '-I'+str(source/'fs/9p'), '-fstack-usage',
                    '-DCONFIG_9P_FS_HOMELABFS_CONTROLLED=1',
                    '-DCONFIG_9P_FS_HOMELABFS_OPERATOR_KEY="'+public+'"',
                    '-DKBUILD_MODFILE="'+name+'"']
    commands.append(command)
    subprocess.run(command, cwd=headers, check=True, timeout=90)
(out/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
(out/'fixture-binding.json').write_text(json.dumps({
    'path': str(fixture), 'bytes': len(fixture_bytes),
    'sha256': hashlib.sha256(fixture_bytes).hexdigest(),
    'scope': __doc__,
}, indent=2)+'\n')
