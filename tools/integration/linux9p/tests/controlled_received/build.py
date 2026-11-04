#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Build the actual C6 component consumer at declared OS boundaries."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess


def function(source, name):
    """Copy a complete production definition, without rewriting its body."""
    match = re.search(r'^([^\n;{}]+\b' + re.escape(name) + r'\([^;{}]*\)\n\{)',
                      source, re.M)
    if not match:
        raise ValueError('production function absent: ' + name)
    start = match.start()
    depth = 0
    # Production C has braces in strings/comments. Lex those before counting.
    token = re.compile(r'/\*.*?\*/|//[^\n]*|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|[{}]', re.S)
    for item in token.finditer(source, source.index('{', match.start())):
        if item.group() == '{':
            depth += 1
        elif item.group() == '}':
            depth -= 1
            if not depth:
                return source[start:item.end()] + '\n'
    raise ValueError('unbalanced definition: ' + name)


def portable(text):
    return '\n'.join(line for line in text.splitlines()
                     if not line.startswith(('#include <linux/', '#include <net/')))+'\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--operator-key', required=True)
    args = parser.parse_args()
    if not re.fullmatch('[0-9a-f]{64}', args.operator_key):
        parser.error('operator key must be32-byte lowercase hex')
    source = args.source.resolve(strict=True)
    out = args.output.absolute()
    out.mkdir(mode=0o700, exist_ok=False)
    here = Path(__file__).resolve().parent
    repo = here.parents[4]
    binding = {}
    def read(name):
        data = (source/name).read_bytes()
        binding[name] = hashlib.sha256(data).hexdigest()
        return data.decode()
    for path in sorted((source/'fs/9p').glob('hfs*')):
        if path.is_file() and path.suffix in ('.h', '.inc', '.c'):
            (out/path.name).write_text(portable(read(str(path.relative_to(source)))))
    codec = (here.parent/'codec/compat.h').read_text().replace(
        'typedef uint64_t u64;', 'typedef unsigned long long u64;')
    codec = codec.replace('static inline bool hfs_test_allocation_fail(void)', 'extern bool primitive_allocation_size_failure(size_t);\nstatic inline bool hfs_test_allocation_fail(void)').replace('return ++hfs_test_alloc_call == hfs_test_alloc_fail;', 'return ++hfs_test_alloc_call == hfs_test_alloc_fail || primitive_allocation_size_failure(hfs_test_last_allocation_size);')
    codec = codec.replace('#define kfree free', 'extern void primitive_free(void *);\n#define kfree primitive_free').replace('#define kvfree free', '#define kvfree primitive_free')
    (out/'hfs_codec_compat.h').write_text(codec)
    for name in ('kernel_format.c', 'kernel_format.h'):
        (out/name).write_bytes((here.parent/'common'/name).read_bytes())
    (out/'runtime_compat.h').write_bytes((here/'compat.h').read_bytes())
    # Reuse the independently reviewed sysfs/identity primitive declarations.
    admin = (here.parent/'controlled_provider/compat.h').read_text()
    admin = admin.replace('#include "enrollment_struct.inc"', '')
    admin = admin.replace('#include "session_struct.inc"', '')
    admin = admin.replace('struct mutex { pthread_mutex_t native; };', '')
    admin = re.sub(r'^static inline void sort\([^}]+}\n', '', admin, flags=re.S|re.M)
    admin = re.sub(r'^static inline int hfs_uuid.*\n', '', admin, flags=re.M)
    admin = admin.replace('typedef atomic_int atomic_t;', '')
    admin = admin.replace('static inline void uuid_gen(uuid_t *out) { *out=primitive_nonce; }',
                          'void primitive_uuid_gen(uuid_t *);\n#define uuid_gen primitive_uuid_gen')
    admin = re.sub(r'^#define DEFINE_MUTEX.*\n', '', admin, flags=re.M)
    admin = re.sub(r'^static inline void mutex_(lock|unlock).*\n', '', admin, flags=re.M)
    (out/'controlled_compat.h').write_text(admin)
    session = read('fs/9p/hfs_session.c')
    selected = ['hfs_field_u64', 'hfs_uuid', 'hfs_cred_write', 'hfs_cred_free',
                'hfs_process_exit_work', 'hfs_cred_equal', 'hfs_process_find',
                'hfs_capture_owned', 'hfs_capture', 'hfs_capture_terminal',
                'hfs_reply_scope', 'hfs_transport', 'hfs_result_needs_adoption',
                'hfs_operation', 'hfs_allocation_candidate', 'hfs_terminal_candidate', 'hfs_candidate_parse_put', 'hfs_control',
                'hfs_copy_token', 'hfs_result_code', 'hfs_result_validate',
                'hfs_args_init', 'hfs_cap_write', 'hfs_args_object',
                'hfs_cap_free', 'hfs_cap_parse', 'hfs_reply_cap',
                'hfs_request_unhold', 'hfs_request_result_put', 'hfs_request_payload',
                'hfs_request_allocate', 'hfs_request_resolve_gap', 'hfs_request_result_borrow', 'hfs_request_run',
                'hfs_request_earmark', 'hfs_request_cancel_earmark', 'hfs_request_earmark_admitted',
                'hfs_request_bind_drain', 'hfs_request_drain', 'hfs_call_control_bytes', 'hfs_call', 'hfs_retirement_cut_new', 'hfs_retirement_cut_free', 'hfs_retire', 'hfs_result_adopt',
                'hfs_readdir_cleanup', 'hfs_readdir_forget', 'hfs_session_free', 'hfs_session_destroy', 'hfs_enrollment_validate', 'hfs_enrollment_lanes', 'v9fs_hfs_register_provider']
    definitions = []
    for name in selected:
        definitions.append(function(session, name))
        if name == 'hfs_transport':
            definitions.append('#include "hfs_received.inc"\n')
    definitions.append(function(read('fs/9p/hfs_file.c'), 'hfs_description_discard'))
    (out/'session.inc').write_text('\n'.join(definitions))
    mount = function(session, 'v9fs_hfs_mount')
    fail = mount.index('\nfail:\n')
    (out/'mount_fail.inc').write_text('static void *fixture_actual_mount_fail(struct hfs_session *session,int err) {\nunsigned long irqflags;\n' + mount[fail+len('\nfail:\n'):])
    lease = read('fs/9p/hfs_lease.c')
    (out/'lease.inc').write_text(portable(lease))
    exchange = function(lease, 'hfs_view_exchange')
    bound = exchange.index('\n\t/* Initial admissions')
    (out/'release_prepare.inc').write_text(exchange[:bound].replace('int hfs_view_exchange(', 'static int fixture_prepare_exchange(') + '\n\t(void)reply;(void)sent;(void)op;(void)deadline;(void)control;(void)body;(void)status;return 0;\n}\n')
    mmap = read('fs/9p/hfs_mmap.c')
    first = mmap.index('static int hfs_mmap_invalidate_stopped(')
    last = mmap.index('/* Independent poll,', first)
    (out/'physical.inc').write_text(mmap[first:last])
    control_start = mmap.index('struct hfs_mmap_control {')
    control_end = mmap.index('\n};', control_start) + 4
    (out/'control_struct.inc').write_text(mmap[control_start:control_end])
    control_names = ['hfs_mmap_control_release', 'hfs_mmap_control_get',
                     'hfs_mmap_control_put', 'hfs_mmap_hash32', 'hfs_mmap_token',
                     'hfs_mmap_event_check', 'hfs_mmap_ack', 'hfs_mmap_fail',
                     'hfs_mmap_drains', 'hfs_mmap_process', 'hfs_mmap_drained',
                     'hfs_mmap_events', 'hfs_mmap_poll', 'hfs_mmap_pressure',
                     'hfs_mmap_pressure_work', 'hfs_mmap_start',
                     'hfs_mmap_quiesce_control', 'hfs_mmap_prepare_retire',
                     'hfs_mmap_terminal_prepare', 'hfs_mmap_stop']
    (out/'control.inc').write_text('static void hfs_mmap_drained(struct work_struct *);\n' +
                                 '\n'.join(function(mmap, name) for name in control_names))
    (out/'virtio_identity.inc').write_text(function(read('net/9p/trans_virtio.c'), 'p9_client_hfs_virtio_identity'))
    client = read('net/9p/client.c')
    (out/'client_exchange.inc').write_text(function(client, 'p9_client_is_hfs') + function(client, 'p9_client_hfs_exchange'))
    crypto = source/'fs/9p/hfs_crypto'
    common = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
              '-Wno-unused-parameter', '-Wno-sign-compare', '-Wno-unused-function',
              '-Wno-misleading-indentation', '-fsanitize=address,undefined',
              '-fno-omit-frame-pointer', '-fno-pie', '-ffunction-sections', '-fdata-sections']
    commands = []
    for unit in ('hfs_crypto_core', 'hfs_crypto_ed25519'):
        command = common + ['-fwhole-program', '-I'+str(source/'fs/9p'),
                            '-I'+str(crypto/'vendor/src'), '-c',
                            str(source/('fs/9p/'+unit+'.c')), '-o', str(out/(unit+'.o'))]
        commands.append(command)
        (out/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
        subprocess.run(command, check=True, timeout=90)
    command = common + ['-D_GNU_SOURCE', '-D_DEFAULT_SOURCE',
        '-DHFS_CODEC_USERSPACE', '-DHFS_PROVIDER_USERSPACE',
        '-DCONFIG_9P_FS_HOMELABFS=1', '-DCONFIG_9P_FS_HOMELABFS_CONTROLLED=1',
        '-DCONFIG_9P_FS_HOMELABFS_OPERATOR_KEY="'+args.operator_key+'"',
        '-include', str(out/'runtime_compat.h'), '-I'+str(out), '-I'+str(repo),
        str(here/'consumer.c'), str(out/'hfs_protocol.c'), str(out/'hfs_policy.c'),
        str(out/'hfs_enrollment.c'), str(out/'kernel_format.c'),
        str(out/'hfs_crypto_core.o'), str(out/'hfs_crypto_ed25519.o'),
        str(repo/'tools/integration/lib9p/src/hfs9p_json.c'),
        str(repo/'tools/integration/lib9p/src/hfs9p_ledger.c'),
        str(repo/'tools/integration/lib9p/src/hfs9p_supervisor.c'),
        '-pthread', '-lcrypto', '-Wl,--gc-sections', '-no-pie', '-o', str(out/'consumer')]
    commands.append(command)
    (out/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
    (out/'source-binding.json').write_text(json.dumps(binding, indent=2)+'\n')
    subprocess.run(command, check=True, timeout=120)


if __name__ == '__main__':
    main()
