#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Cross-check actual C freeze construction against maintained closed wire vectors."""
import argparse
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=True)


def self_id(value):
    value.pop('id',None)
    value['id']=hashlib.sha256(canonical(value).encode()).hexdigest()


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source',required=True,type=Path)
    p.add_argument('--output',required=True,type=Path)
    p.add_argument('--fixture',required=True,type=Path)
    args=p.parse_args();here=Path(__file__).resolve().parent
    args.output.mkdir(parents=False,exist_ok=False,mode=0o700)
    fixture=json.loads(args.fixture.read_text())
    command=copy.deepcopy(next(c['body'] for c in fixture['commands'] if c['name']=='freeze'))
    payload=command['payload']
    # Signed runtime enrollment may use one anchor and separate control nonce;
    # the closed structural vectors intentionally used arbitrary family tuples.
    payload['request']['anchor']=payload['claim']['anchor']
    frozen=copy.deepcopy(next(r['body'] for r in fixture['responses'] if r['command']=='freeze'))
    frozen['request']=payload['request']
    frozen['request_hash']=hashlib.sha256(canonical({'op':'DIRTY_FREEZE','payload':payload}).encode()).hexdigest()
    self_id(frozen)
    pending=copy.deepcopy(frozen);pending['status']='PENDING';pending['entry']=None;self_id(pending)
    corrupt=copy.deepcopy(frozen);corrupt['entry']['payload_hash']='00'*32
    entry=corrupt['entry'];entry['id']=hashlib.sha256(canonical({k:v for k,v in entry.items() if k not in ('id','claim')}).encode()).hexdigest()
    self_id(corrupt)
    drain=next(c['body']['payload']['intent']['args'] for c in fixture['commands'] if c['name']=='drain')
    values={'golden_payload':payload,'golden_query':{'request':payload['request']},
            'golden_reservation':payload['reservation'],'golden_credentials':payload['request']['credentials'],
            'golden_claim':payload['claim'],'golden_entry':frozen['entry'],'golden_args':drain,
            'golden_pending':{'payload':pending},'golden_frozen':{'payload':frozen},
            'corrupt_frozen':{'payload':corrupt}}
    fields=['static const char '+name+'[] = '+json.dumps(canonical(v))+';' for name,v in values.items()]
    for name,value in {'realm':payload['claim']['realm'],'anchor':payload['claim']['anchor'],
                       'claim_nonce':payload['claim']['nonce'],'control_nonce':payload['request']['nonce'],
                       'recall':payload['recall']}.items():
        fields.append('static const char '+name+'[] = '+json.dumps(value)+';')
    (args.output/'fixture.h').write_text('\n'.join(fields)+'\n')
    names=['fs/9p/hfs_lease.c','fs/9p/hfs_session.c','fs/9p/hfs.h','fs/9p/hfs_view.h','fs/9p/hfs_lease.h']
    inputs={name:(args.source/name).read_text() for name in names}
    body=['#include "hfs_protocol.h"']
    for name in names[3:]:
        body.append('\n'.join(x for x in inputs[name].splitlines() if not x.startswith('#include')))
    header=inputs[names[2]];start=header.index('struct hfs_request {')
    body.append(header[start:header.index('\n};',start)+3])
    source=inputs[names[0]]
    spans=[('int hfs_json_hash_fields(', '\nint hfs_view_target('),
           ('int hfs_view_ticket_activate(', '\nstatic int hfs_ticket_union('),
           ('static int hfs_ticket_union(', '\nint hfs_view_query('),
           ('int hfs_view_exchange(', '\nstatic int hfs_lease_decode('),
           ('int hfs_dirty_freeze(', '\nstatic void hfs_dirty_mapping_error(')]
    # First helper group excludes unrelated target parsing and its callbacks.
    start=source.index('int hfs_json_hash_fields(');end=source.index('\nstatic int hfs_normal_target(',start)
    body.append(source[start:end])
    for first,last in spans[1:]:
        start=source.index(first);body.append(source[start:source.index(last,start)])
    session=inputs[names[1]];start=session.index('int hfs_request_bind_drain(')
    bind=session[start:session.index('\nint hfs_request_drain(',start)]
    # Freeze calls the actual immutable Claim builder before its definition.
    body.insert(1,'int hfs_request_bind_drain(struct hfs_session *, struct hfs_request *, const struct hfs_buf *);')
    body.append(bind)
    (args.output/'production.c').write_text('\n'.join(body)+'\n')
    primitive=(here.parent/'codec/compat.h').read_text().replace('typedef uint64_t u64;','typedef unsigned long long u64;')
    (args.output/'hfs_codec_compat.h').write_text(primitive)
    compat=(here.parent/'requests/compat.h').read_text().replace('typedef int spinlock_t;','typedef pthread_mutex_t spinlock_t;')
    compat=compat.replace('struct list_head requests;','struct list_head requests, view_tickets;\n   bool blocked;')
    compat=compat.replace('#endif','''
#define wake_up_all(q) ((void)(q))
static inline long strscpy(char *d,const char *s,size_t n) { size_t len=strlen(s);if (!n) return -E2BIG;size_t copy=len<n ? len : n-1;memcpy(d,s,copy);d[copy]=0;return len<n ? (long)len : -E2BIG; }
#define U64_MAX UINT64_MAX
#define HZ 100UL
#define time_after_eq(a,b) ((long)((a)-(b))>=0)
#define spin_lock_init(p) assert(!pthread_mutex_init(p,NULL))
#define spin_lock_irqsave(p,f) ((void)(f),assert(!pthread_mutex_lock(p)))
#define spin_unlock_irqrestore(p,f) ((void)(f),assert(!pthread_mutex_unlock(p)))
#define READ_ONCE(p) __atomic_load_n(&(p),__ATOMIC_ACQUIRE)
#define WRITE_ONCE(p,v) __atomic_store_n(&(p),(v),__ATOMIC_RELEASE)
#define wait_event_killable(q,c) ({ (void)(q); while (!(c)) usleep(100); 0; })
static inline void list_move_tail(struct list_head *p, struct list_head *h) { list_del(p);list_add_tail(p,h); }
static inline void usleep_range(int low,int high) { assert(low==10000 && high==12000); }
struct page { unsigned char bytes[4096]; bool locked,mapped,writeback,pinned; };
#define PageWriteback(p) ((p)->writeback)
#define page_mapped(p) ((p)->mapped)
#define page_folio(p) (p)
#define folio_maybe_dma_pinned(p) ((p)->pinned)
#define lock_page(p) do { assert(!(p)->locked);(p)->locked=true; } while (0)
#define unlock_page(p) do { assert((p)->locked);(p)->locked=false; } while (0)
#define kmap_local_page(p) (assert((p)->locked),(p)->bytes)
#define kunmap_local(p) ((void)(p))
static inline bool uuid_equal(const uuid_t *a,const uuid_t *b) { return !memcmp(a,b,sizeof(*a)); }
#endif''')
    (args.output/'compat.h').write_text(compat)
    for file in ['kernel_format.h','kernel_format.c']:
        (args.output/file).write_bytes((here.parent/'common'/file).read_bytes())
    (args.output/'test.c').write_bytes((here/'test.c').read_bytes())
    binding={'source':{n:hashlib.sha256(v.encode()).hexdigest() for n,v in inputs.items()},
             'fixture_sha256':hashlib.sha256(args.fixture.read_bytes()).hexdigest(),
             'scope':'actual C freeze/claim/ticket code; page/transport/clock fixtures; closed codec interoperability, no VFS/runtime/source proof'}
    (args.output/'binding.json').write_text(json.dumps(binding,indent=2)+'\n')
    sys.dont_write_bytecode=True
    spec=importlib.util.spec_from_file_location('codec_runner',here.parent/'codec/run.py')
    runner=importlib.util.module_from_spec(spec);spec.loader.exec_module(runner)
    os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2])
    log=args.output/'dirty.log';log.write_text(json.dumps(binding)+'\n')
    runner.run(['cc','-std=gnu11','-O1','-g','-Wall','-Wextra','-Werror','-Wno-sign-compare','-Wno-unused-function',
                '-DHFS_CODEC_USERSPACE','-include',str(args.output/'hfs_codec_compat.h'),
                '-include',str(args.output/'kernel_format.h'),'-I',str(args.source/'fs/9p'),'-I',str(args.output),
                '-fsanitize=address,undefined','-fno-omit-frame-pointer','-fno-pie','-no-pie',
                str(args.source/'fs/9p/hfs_protocol.c'),str(args.output/'kernel_format.c'),str(args.output/'test.c'),
                '-pthread','-lcrypto','-o',str(args.output/'dirty-test')],here,log)
    runner.run([str(args.output/'dirty-test')],here,log)
    text=log.read_text();line=next(l for l in text.splitlines() if l.startswith('FREEZE_PAYLOAD '))
    command['payload']=json.loads(line.removeprefix('FREEZE_PAYLOAD '))
    (args.output/'c-command.json').write_text(canonical(command)+'\n')
    (args.output/'reply.json').write_text(canonical(frozen)+'\n')
    print(text)


if __name__=='__main__': main()
