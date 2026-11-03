#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Construct every maximum-sized closed1.1 ticket union and count canonical bytes.

This is resource arithmetic, not an authority, VFS model, or runtime fit proof.
All variable strings in these unions are fixed UUID/hash/enums or hex bytes;
there is no unbounded escaped text. Full1024 distinct groups use10 digits each.
"""
import argparse
import hashlib
import json
from pathlib import Path

U64 = 2 ** 64 - 1
ID = 'ffffffff-ffff-ffff-ffff-ffffffffffff'
HASH = 'f' * 64
OFFSET = ((2 ** 63 - 1 - 4096) // 4096) * 4096


def canon(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()


def digest(value):
    return hashlib.sha256(canon(value)).hexdigest()


def hashed(value):
    return dict(value, id=digest(value))


def cases():
    context = {key: HASH if key.endswith('_hash') else ID for key in (
        'authority_hash', 'cluster', 'configuration', 'export', 'exporter_incarnation',
        'fuse_session', 'generation', 'guest_boot', 'guest_mount', 'origin_incarnation',
        'origin_journal', 'origin_node', 'policy_hash', 'registry_hash', 'session', 'vm', 'volume')}
    credentials = dict(credential_revision=U64, fsuid=2 ** 32 - 2, fsgid=2 ** 32 - 2,
                       groups=list(range(2 ** 32 - 1025, 2 ** 32 - 1)),
                       idmap_hash=HASH, process=ID, user_namespace=ID)
    cap = dict(ancestors_hash=HASH, credential_hash=digest(credentials), description=None,
               kind='DOMAIN', object_allocation=ID, object_home=ID, revision=U64,
               rights=0, scope=context, token=HASH)
    description = dict(cap, kind='DESCRIPTION', description=ID, rights=63)
    access = dict(kind='PAGE', description=description, offset=OFFSET, length=4096)
    cut = dict(context_hash=digest(context), enrollment_hash=HASH, journal_epoch=U64,
               journal_hash=HASH, journal_incarnation=ID, journal_position=U64,
               profile_hash=HASH, state_hash=HASH, visibility_generation=ID)
    local = dict(kind='CONTROLLED_LOCAL', cut=cut)
    ref = {key: ID for key in ('cluster', 'volume', 'generation', 'configuration',
                               'origin_node', 'journal_incarnation', 'creator_node', 'creator_incarnation')}
    ref.update(epoch=U64, sequence=U64, frame_hash=HASH, kind=13)
    normal = dict(kind='NORMAL_PUBLIC', publication=dict(authority_hash=HASH,
        sequencer_node=ID, sequencer_incarnation=ID, serial=U64, selection_hash=HASH,
        result_ref=ref, source_manifest_hash=HASH, visibility_generation=ID))
    target = max((local, normal), key=lambda value: len(canon(value)))
    lease = hashed(dict(scope=context, domain=cap, access=access,
                        credential_hash=digest(credentials), mode='EXCLUSIVE_DIRTY',
                        target=target, fence=U64, revision=U64))
    number = dict(realm=ID, subject='ff' * 1024, anchor=ID, nonce=ID, number=U64)
    reservation = hashed(dict(scope=context, lease_id=lease['id'], description=description,
        credentials=credentials, offset=OFFSET, length=4096, claim_number=number,
        provenance=ID, drain_credit=HASH))
    data = 'ff' * 4096
    entry = dict(description=description, offset=OFFSET, length=4096,
                 payload_hash=hashlib.sha256(bytes.fromhex(data)).hexdigest(), reservation=reservation['id'])
    entry['id'] = digest(entry)
    intent = dict(opcode='DRAIN_WRITE', args=dict(description=description, offset=OFFSET,
        data=data, recall=ID, dirty_entry=entry['id'], reservation=reservation['id']))
    claim = dict(number, original_hash=digest(intent))
    entry['claim'] = claim
    ticket = dict(anchor=ID, nonce=ID, number=U64, credentials=credentials)
    payloads = {
        'VIEW_ACQUIRE': dict(request=ticket, domain=cap, access=access, mode='EXCLUSIVE_DIRTY', target=target),
        'VIEW_RELEASE': dict(request=ticket, lease=lease),
        'VIEW_RENEW': dict(request=ticket, lease=lease),
        'VIEW_QUERY': dict(request=ticket), 'VIEW_CANCEL': dict(request=ticket),
        'DIRTY_RESERVE': dict(request=ticket, lease=lease, description=description,
                              offset=OFFSET, length=4096, claim_number=number),
        'DIRTY_FREEZE': dict(request=ticket, reservation=reservation, recall=ID, data=data, claim=claim),
        'VIEW_RETIRE': dict(anchor=ID, nonce=ID, credentials=credentials,
                            through=U64, result_hashes=[HASH] * 1024),
    }
    protocol = dict(body_schema=2, bridge_major=1, bridge_minor=1, features=2047,
                    msize=65536, normal_major=3, normal_minor=0)
    old_lease = dict(lease, revision=U64 - 1)
    del old_lease['id']
    payloads['VIEW_RENEW']['lease'] = hashed(old_lease)
    commands = {op: dict(cell=2147483647, context=context, op=op, payload=payload,
                        protocol=protocol, reclaim=dict(cell=2147483646, response_hash=HASH))
                for op, payload in payloads.items()}
    replies = {}
    for op in ('VIEW_ACQUIRE', 'VIEW_RENEW', 'DIRTY_RESERVE', 'DIRTY_FREEZE', 'VIEW_RELEASE'):
        body = dict(status='GRANTED', request=ticket, operation=op,
                    request_hash=digest(dict(op=op, payload=payloads[op])),
                    lease=None, reservation=None, entry=None)
        if op in ('VIEW_ACQUIRE', 'VIEW_RENEW'):
            body['lease'] = lease
        elif op == 'DIRTY_RESERVE':
            body['reservation'] = reservation
        elif op == 'DIRTY_FREEZE':
            body.update(status='FROZEN', entry=entry)
        else:
            body['status'] = 'RELEASED'
        replies[op] = dict(cell=2147483647, context=context, op=op, payload=hashed(body), protocol=protocol)
    return dict(context=context, credentials=credentials, domain=cap, description=description,
                ticket=ticket, lease=lease, reservation=reservation, entry=entry,
                normal_target=normal, local_target=local, commands=commands, replies=replies)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    values = cases()
    sizes = {name: len(canon(values[name])) for name in (
        'context', 'credentials', 'domain', 'description', 'ticket', 'lease',
        'reservation', 'entry', 'normal_target', 'local_target')}
    sizes['commands'] = {name: len(canon(value)) for name, value in values['commands'].items()}
    sizes['replies'] = {name: len(canon(value)) for name, value in values['replies'].items()}
    def tokens(value):
        if isinstance(value, dict):
            return 1 + sum(1 + tokens(v) for v in value.values())
        if isinstance(value, list):
            return 1 + sum(tokens(v) for v in value)
        return 1
    def depth(value):
        children = list(value.values()) if isinstance(value, dict) else value if isinstance(value, list) else []
        return 1 + max((depth(v) for v in children), default=0)
    all_bodies = list(values['commands'].values()) + list(values['replies'].values())
    sizes['maximum_tokens'] = max(tokens(v) for v in all_bodies)
    sizes['maximum_depth'] = max(depth(v) for v in all_bodies)
    assert max(sizes['commands'].values()) < 131072
    assert max(sizes['replies'].values()) < 131072
    args.output.mkdir(mode=0o700, exist_ok=False)
    (args.output / 'unions.json').write_bytes(canon(values) + b'\n')
    (args.output / 'sizes.json').write_text(json.dumps(sizes, indent=2) + '\n')
    print(json.dumps(sizes, indent=2))


if __name__ == '__main__':
    main()
