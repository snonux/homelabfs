#!/usr/bin/env python3
"""Independent literal 1.1 byte checks and bounded design histories, no engine."""
import copy
import hashlib
import json
from pathlib import Path
import struct
import uuid

from check_retained9p import canonical, credentials, decode_body, fields, require

PROTOCOL = dict(body_schema=2, bridge_major=1, bridge_minor=1, features=2047,
                msize=65536, normal_major=3, normal_minor=0)
HEADER = struct.Struct('<8sHHHHIIII16sQ32s')


def hashed(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def check_views():
    path = Path(__file__).resolve().parents[2] / 'docs/spec/retained9p-fixtures/views.json'
    corpus = json.loads(path.read_text())
    require(corpus['protocol'] == PROTOCOL, 'view tuple')
    commands = {}
    for case in corpus['commands']:
        body = case['body']
        fields(body, ['cell', 'context', 'op', 'payload', 'protocol', 'reclaim'])
        require(body['protocol'] == PROTOCOL, 'command tuple')
        raw = canonical(body)
        wire = bytes.fromhex(case['frame_hex'])
        h = HEADER.unpack(wire[:HEADER.size])
        require(h[:4] == (b'HFS9BR1\0', 1, 1, 1), 'view frame version')
        require(h[5:9] == (len(raw), 0, len(raw), 0), 'view frame lengths')
        require(h[9] == uuid.UUID(body['context']['session']).bytes and
                h[10] == body['cell'], 'view frame scope')
        require(h[11].hex() == case['sha256'] == hashlib.sha256(raw).hexdigest(),
                'literal digest')
        require(wire[HEADER.size:] == raw and decode_body(raw) == body,
                'literal canonical body')
        commands[case['name']] = body
    reserve = commands['reserve']['payload']
    frozen = commands['freeze']['payload']
    r = frozen['reservation']
    require(hashed({k: v for k, v in r.items() if k != 'id'}) == r['id'],
            'reservation digest')
    require('original_hash' not in r['claim_number'], 'unknown dirty hash')
    require(reserve['claim_number'] == r['claim_number'], 'earmarked number')
    require(frozen['request']['credentials'] == r['credentials'], 'dirty provenance')
    intent = commands['drain']['payload']['intent']
    require(hashed(intent) == frozen['claim']['original_hash'], 'frozen claim hash')
    require(commands['drain']['payload']['claim'] == frozen['claim'], 'drain claim')
    require(intent['args']['data'] == frozen['data'] and
            intent['args']['reservation'] == r['id'], 'exact frozen bytes')
    for response in corpus['responses']:
        body = response['body']
        if 'request' not in body:
            continue
        require(hashed({k: v for k, v in body.items() if k != 'id'}) == body['id'],
                'protected reply digest')
        credentials(body['request']['credentials'])
        if body['status'] in ('PENDING', 'INDETERMINATE', 'BLOCKED', 'RETIRED_ID'):
            require(all(body[k] is None for k in ('lease', 'reservation', 'entry')),
                    'pending authority')
        original = commands[response['command']]
        if original['op'] in ('VIEW_QUERY', 'VIEW_CANCEL'):
            original = commands['dirty_page']
        require(body['operation'] == original['op'] and
                body['request_hash'] == hashed(dict(op=original['op'],
                                                   payload=original['payload'])),
                'original admission lineage')
    check_ticket_history(commands, corpus['responses'])
    print(f"PASS: {len(commands)} literal bridge1.1 commands and "
          f"{len(corpus['responses'])} replies; frozen immutable claim; bounded ticket histories")
    print('Design/byte subset only: no durable controller, source proof, physical MM or runtime claim.')


def check_ticket_history(commands, responses):
    """Small explicit oracle for required future-controller transitions."""
    original = commands['dirty_page']
    ticket = original['payload']['request']
    principal = canonical(dict(context=original['context'],
                               credentials=ticket['credentials'],
                               anchor=ticket['anchor'], nonce=ticket['nonce']))
    binding = hashed(dict(op=original['op'], payload=original['payload']))
    admitted = next(r['body'] for r in responses
                    if r['command'] == 'dirty_page' and r['body']['status'] == 'PENDING')
    terminal = next(r['body'] for r in responses
                    if r['command'] == 'dirty_page' and r['body']['status'] == 'GRANTED')
    # Original retry stays immutable while QUERY may expose a protected terminal.
    require(admitted['id'] != terminal['id'] and
            admitted['request_hash'] == terminal['request_hash'] == binding,
            'pending versus terminal lineage')
    require(canonical(admitted) == canonical(copy.deepcopy(admitted)), 'retry bytes')
    changed = copy.deepcopy(original)
    changed['payload']['access']['offset'] += 4096
    require(hashed(dict(op=changed['op'], payload=changed['payload'])) != binding,
            'conflicting request must refuse')
    for field in ('process', 'credential_revision', 'groups', 'user_namespace'):
        other = copy.deepcopy(ticket['credentials'])
        if field == 'groups':
            other[field] = other[field] + [4294967295]
        elif field == 'credential_revision':
            other[field] += 1
        else:
            other[field] = str(uuid.UUID(int=777))
        key = canonical(dict(context=original['context'], credentials=other,
                             anchor=ticket['anchor'], nonce=ticket['nonce']))
        require(key != principal, 'cross-principal ticket lookup')
    # Allocated gaps are obligations; a pending/lost grant is never no-effect.
    allocated = {1: 'NO_EFFECT', 2: 'UNKNOWN', 3: 'GRANTED'}
    require(any(state not in ('NO_EFFECT', 'CLOSED') for state in allocated.values()),
            'retirement gap blocks')
    allocated[2] = 'NO_EFFECT'
    require(allocated[3] == 'GRANTED', 'reply loss retains lease')
    allocated[3] = 'CLOSED'  # only external physical release/fence AND source closure
    require(all(v in ('NO_EFFECT', 'CLOSED') for v in allocated.values()),
            'retirement complete coverage')
    # Fixed transport partition: ordinary acquisitions cannot spend control reserve.
    ordinary, control, session_ordinary, session_total = 64, 16, 12, 16
    require(ordinary > 0 and control >= 4 and session_total - session_ordinary >= 4,
            'independent stop/drain/release capacity')


if __name__ == '__main__':
    check_views()
