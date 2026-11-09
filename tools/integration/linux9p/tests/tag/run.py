#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Run the actual net/9p fixed-tag exchange with concurrent owners (task 964).

zq3-r5 (R5-1) stopped after INSTALLED_DRAIN: the release loop's VIEW_QUERY and
the event worker's VIEW_ACK share fixed tag 65533. The receive callback drops
its request reference only after waking the waiter, so the next owner of the
per-tag mutex could find the tag still allocated and fail with -ENOMEM.

The tag allocator, request lifetime, receive callback, RPC wait and the hfs
exchange are copied verbatim from the owned net/9p/client.c. Only the idr,
slab, wait queue, locks, PDU marshalling and the virtio transport are
substituted (test.c); the substituted transport delays its final reference
drop after the wake-up, as a preempted vCPU does. No kernel or VM is used.
"""
import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import sys

# Definition order matters: each function only uses earlier ones (or the
# forward declaration of p9_client_rpc in test.c).
FUNCTIONS = ('p9_client_is_hfs', 'safe_errno', 'p9_fcall_init', 'p9_fcall_fini',
             'p9_tag_alloc', 'p9_tag_remove', 'p9_req_put', 'p9_client_cb',
             'p9_parse_header', 'p9_check_errors', 'p9_client_flush',
             'p9_client_prepare_req', 'p9_client_rpc_v', 'p9_client_rpc_tag',
             'p9_client_rpc', 'p9_client_hfs_tag_idle', 'p9_client_hfs_exchange')
# Introduced by task 964; absent from older sources, which this test must
# still compile so that it can reproduce the original failure.
OPTIONAL = {'p9_client_hfs_tag_idle'}


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def definition(source, name):
    """Copy a complete definition, including a return type on its own line.

    net/9p/client.c writes several signatures as "static struct p9_req_t *"
    followed by the name on the next line, which the shared helper in
    controlled_received/build.py does not match.
    """
    match = re.search(r'^[^\n;{}]*\b' + re.escape(name) + r'\([^;{}]*\)\n\{', source, re.M)
    if not match:
        return None
    start = match.start()
    previous = source.rfind('\n', 0, start - 1) + 1
    if re.fullmatch(r'(static\s+)?[\w \t*]+', source[previous:start - 1]):
        start = previous
    depth = 0
    token = re.compile(r'/\*.*?\*/|//[^\n]*|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|[{}]', re.S)
    for item in token.finditer(source, source.index('{', match.start())):
        depth += 1 if item.group() == '{' else -1 if item.group() == '}' else 0
        if item.group() == '}' and not depth:
            return source[start:item.end()] + '\n'
    raise ValueError('unbalanced definition: ' + name)


def extract(source, output):
    client = (source / 'net/9p/client.c').read_text()
    parts = []
    for name in FUNCTIONS:
        text = definition(client, name)
        if text is None and name not in OPTIONAL:
            raise ValueError('production function absent: ' + name)
        parts.append(text or f'/* {name}: absent from this source */\n')
    (output / 'client.inc').write_text('\n'.join(parts))
    return hashlib.sha256(client.encode()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    args = parser.parse_args()
    sys.dont_write_bytecode = True
    here = Path(__file__).resolve().parent
    source, output = args.source.resolve(strict=True), args.output.absolute()
    output.mkdir(parents=True, exist_ok=False)
    runner = load('codec_runner', here.parent / 'codec/run.py')
    log = output / 'tag.log'
    log.write_text('net9p_client_sha256=' + extract(source, output) + '\n')
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-Wno-sign-compare', '-Wno-unused-parameter', '-Wno-unused-function',
               '-pthread', '-I', str(output), str(here / 'test.c'),
               '-o', str(output / 'tag-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    runner.run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1'
    environment['UBSAN_OPTIONS'] = 'halt_on_error=1:print_stacktrace=1'
    runner.run([str(output / 'tag-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
