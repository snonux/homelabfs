#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Build actual guest issuance/transport and owned backend for real-host IPC.

Kernel scheduling/credential/resource/native fd observations are primitives.
No provider, native enrollment, source verification or runtime is certified.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
p.add_argument('--sanitize', action='store_true')
a = p.parse_args()
os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
here = Path(__file__).resolve().parent
repo = here.parents[4]
src = a.source.resolve(strict=True)
out = a.output.absolute()
out.mkdir(mode=0o700, exist_ok=False)
header = (src/'fs/9p/hfs.h').read_text()
session = (src/'fs/9p/hfs_session.c').read_text()
def structure(name):
    start = header.index('struct '+name+' {')
    return header[start:header.index('\n};',start)+3]
parts = [structure(n) for n in ['hfs_enrollment','hfs_provider','hfs_cap','hfs_cred',
                               'hfs_cell','hfs_candidate','hfs_request','hfs_session']]

for filename, name in [('hfs_view.h', 'hfs_cache_page'), ('hfs_view.h', 'hfs_dirty'), ('hfs_lease.h', 'hfs_lease')]:
    raw=(src/'fs/9p'/filename).read_text()
    start=raw.index('struct '+name+' {')
    parts.append(raw[start:raw.index('\n};',start)+3])
(out/'types.inc').write_text('\n'.join(parts))
spans = [('static int hfs_field_u64(', '\nvoid hfs_cred_write('),
         ('void hfs_cred_write(', '\nvoid hfs_cred_free('),
         ('static int hfs_reply_scope(', '\nstatic int hfs_copy_token('),
         ('static int hfs_copy_token(', '\nstatic int hfs_result_validate('),
         ('static void hfs_request_result_put(', '\nint hfs_request_cancel_earmark('),
         ('int hfs_call(', '\nint hfs_args_init(')]
parts=[]
for first,last in spans:
    start=session.index(first)
    parts.append(session[start:session.index(last,start)])
(out/'production.inc').write_text('\n'.join(parts))
lease=(src/'fs/9p/hfs_lease.c').read_text()
start=session.index('void hfs_request_earmark_admitted(')
decoder=[session[start:session.index('\nvoid hfs_request_unhold(',start)]]
start=lease.index('int hfs_json_hash_fields(')
decoder.append(lease[start:lease.index('\nstatic int hfs_normal_target(',start)])
start=lease.index('\tbody = hfs_json_field(reply, 0, "reservation");',lease.index('int hfs_dirty_reserve('))
end=lease.index('\nint hfs_dirty_resume_reserve(',start)
decoder.append('static int production_dirty_decoder(struct hfs_session *session, struct hfs_dirty *dirty, struct hfs_cache_page *entry, struct hfs_cap *description, u64 offset, struct hfs_json *reply) {\n u8 expected[32], actual[32];uuid_t provenance;u64 number;int err=0,body;\n'+lease[start:end])
(out/'decoder.inc').write_text('\n'.join(decoder))
views=json.loads((repo/'docs/spec/retained9p-fixtures/views.json').read_text())
original=next(v['body'] for v in views['responses'] if v['command']=='reserve')
def canonical(v):return json.dumps(v,sort_keys=True,separators=(',',':'))
fixtures=['static const char dirty_original[] = '+json.dumps(canonical(original))+';']
changed=[]
for field in ['claim_number','credentials','scope','description','lease_id','id','provenance','offset']:
    value=copy.deepcopy(original)
    reservation=value['reservation']
    if field=='claim_number':reservation[field]['number']+=1
    elif field=='credentials':reservation[field]['groups'][0]+=1
    elif field=='scope':reservation[field]['guest_boot']='00000000-0000-0000-0000-000000000099'
    elif field=='description':reservation[field]['token']='f'*64
    elif field in ['lease_id','id']:reservation[field]='f'*64
    elif field=='provenance':reservation[field]='00000000-0000-0000-0000-000000000000'
    else:reservation[field]+=4096
    changed.append(canonical(value))
fixtures.append('static const char *dirty_bad[] = {'+','.join(json.dumps(v) for v in changed)+'};')
(out/'dirty-fixture.inc').write_text('\n'.join(fixtures)+'\n')

compat=(here.parent/'codec/compat.h').read_text().replace('typedef uint64_t u64;', 'typedef unsigned long long u64;')
compat += '\n#include <sys/stat.h>\n#define MAX_NON_LFS 2147483647UL\nstatic inline void sort(void *b,size_t n,size_t s,int (*cmp)(const void *,const void *),void (*swap)(void *,void *,int)) { (void)swap;qsort(b,n,s,cmp); }\n'
(out/'hfs_codec_compat.h').write_text(compat)
command=['cc','-std=gnu11','-O1','-g','-Wall','-Wextra','-Werror','-Wno-sign-compare',
         '-D_GNU_SOURCE','-D_DEFAULT_SOURCE','-D_POSIX_C_SOURCE=200809L','-DHFS_CODEC_USERSPACE',
         '-include',str(out/'hfs_codec_compat.h'),'-include',str(here.parent/'common/kernel_format.h'),'-I',str(src/'fs/9p'),'-I',str(out),
         '-I',str(here.parent/'common'),'-I',str(repo),str(here/'consumer.c'),
         str(src/'fs/9p/hfs_protocol.c'),str(src/'fs/9p/hfs_policy.c'),
         str(here.parent/'common/kernel_format.c'),
         str(repo/'tools/integration/lib9p/src/hfs9p_json.c'),str(repo/'tools/integration/lib9p/src/hfs9p_ledger.c'),str(repo/'tools/integration/lib9p/src/hfs9p_supervisor.c'),'-pthread','-lcrypto',
         '-o',str(out/'consumer')]
if a.sanitize:
    command[1:1]=['-fsanitize=address,undefined','-fno-omit-frame-pointer','-fno-pie','-no-pie']
with (out/'build.log').open('w') as log:
    subprocess.run(command,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=60)
(out/'bindings.json').write_text(json.dumps({'source':str(src), 'session_sha256':hashlib.sha256(session.encode()).hexdigest(),
 'header_sha256':hashlib.sha256(header.encode()).hexdigest(), 'lease_sha256':hashlib.sha256(lease.encode()).hexdigest(), 'argv':command,
 'scope':'production guest call/allocation/control/framing -> production owned lib9p -> real host IPC; native/kernel/provider observations remain primitives; no deployment'},indent=2)+'\n')
print(out/'consumer')
