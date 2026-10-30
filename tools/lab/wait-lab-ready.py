#!/usr/bin/env python3
"""Pinned read-only boot readiness; no retry after uncertain SSH completion."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import shlex
import signal
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
NAMES = {"hfs-test0", "hfs-test1", "hfs-test2", "rocky", "freebsd"}
CONTRACT = "lab-readiness-v1"
OUTPUT_LIMIT = 16 << 10
COMMAND_SECONDS = 8
TOTAL_SECONDS = 170


class Blocked(Exception):
    """Known terminal refusal with no outstanding remote action."""


class Uncertain(Exception):
    """Remote completion is unproven; local SSH reaping is not proof."""


def command(argv, deadline):
    end = min(deadline, time.monotonic() + COMMAND_SECONDS)
    if end <= time.monotonic():
        raise Blocked('readiness deadline before command dispatch')
    child = None
    completed = False
    output = {'stdout': bytearray(), 'stderr': bytearray()}
    # Own the child before pending handled signals can raise in the parent.
    # This CLI is single-threaded; restore the inherited mask before child exec
    # so SSH and its proxy retain ordinary signal delivery.
    spawn_mask = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGTERM})
    try:
        try:
            child = subprocess.Popen(argv, stdin=subprocess.DEVNULL,
                                     stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                     start_new_session=True,
                                     preexec_fn=lambda: signal.pthread_sigmask(signal.SIG_SETMASK, spawn_mask))
        except (OSError, subprocess.SubprocessError) as error:
            raise Uncertain('SSH dispatch failed: ' + str(error)) from error
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, spawn_mask)
        with selectors.DefaultSelector() as selector:
            selector.register(child.stdout, selectors.EVENT_READ, 'stdout')
            selector.register(child.stderr, selectors.EVENT_READ, 'stderr')
            while selector.get_map():
                left = end - time.monotonic()
                if left <= 0:
                    raise Uncertain('SSH timeout; remote completion unproven')
                for key, _ in selector.select(min(left, 0.2)):
                    block = os.read(key.fd, 4096)
                    if not block:
                        selector.unregister(key.fileobj)
                        continue
                    output[key.data].extend(block)
                    if sum(map(len, output.values())) > OUTPUT_LIMIT:
                        raise Uncertain('SSH output overflow; remote completion unproven')
        left = end - time.monotonic()
        if left <= 0:
            raise Uncertain('SSH completion timeout; remote completion unproven')
        try:
            code = child.wait(timeout=left)
        except subprocess.TimeoutExpired as error:
            raise Uncertain('SSH wait timeout; remote completion unproven') from error
        completed = True
        if code == 255 or code < 0:
            raise Uncertain('SSH transport lost; remote completion unproven')
        return code, bytes(output['stdout']), bytes(output['stderr'])
    except OSError as error:
        raise Uncertain('SSH local I/O failure; remote completion unproven') from error
    finally:
        # Defer another handled interruption until the owned local child is
        # reaped. Local cleanup is never evidence of remote completion.
        old_mask = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGTERM})
        try:
            if child is not None:
                if not completed:
                    try:
                        os.killpg(child.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                child.wait()
                child.stdout.close()
                child.stderr.close()
        except OSError as error:
            raise Uncertain('local SSH cleanup failed; remote completion unproven') from error
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, old_mask)


def require(condition, message):
    if not condition:
        raise Blocked(message)


def read_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate JSON key: ' + key)
            result[key] = value
        return result
    return json.loads(Path(path).read_text(), object_pairs_hook=unique)


def canonical_hash(plan):
    # Match the typed Go Plan snapshot, including its preserved field order.
    raw = json.dumps(plan, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
    for char, escaped in (('&', r'\u0026'), ('<', r'\u003c'), ('>', r'\u003e'),
                          ('\u2028', r'\u2028'), ('\u2029', r'\u2029')):
        raw = raw.replace(char, escaped)
    return hashlib.sha256(raw.encode()).hexdigest()


def artifact(plan, artifact_id):
    matches = [a for a in plan['artifacts'] if a['id'] == artifact_id]
    require(len(matches) == 1, 'exact pinned artifact required: ' + artifact_id)
    item = matches[0]
    path = Path(item['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink(),
            'regular absolute pinned artifact required: ' + artifact_id)
    require(hashlib.sha256(path.read_bytes()).hexdigest() == item['sha256'],
            'pinned artifact changed: ' + artifact_id)
    return path


def key_rows(path, address):
    return [row.split()[1:] for row in path.read_text().splitlines()
            if row.strip() and not row.lstrip().startswith('#')
            and address in row.split()[0].split(',')]


def load_context(mode, policy_id):
    plan = read_json(os.environ['HFS_LAB_PLAN'])
    require(canonical_hash(plan) == os.environ.get('HFS_LAB_PLAN_HASH'),
            'canonical runner Plan hash mismatch')
    require(plan['schema'] == 1 and plan['stage'] == 'stage1' and plan['prepared_guests'] is True
            and plan['run_id'] == os.environ.get('HFS_LAB_RUN_ID'), 'runner context mismatch')
    require(Path(os.environ['HFS_LAB_PLAN']) == Path(plan['state_dir']) / 'plan.json',
            'runner controller Plan snapshot required')
    first = plan['steps'][0]
    require(first['contract'] == 'prepared-lab-preflight-v1'
            and first['args'] == ['--policy-artifact', policy_id], 'pinned preflight policy required')
    args = ['--mode', mode, '--policy-artifact', policy_id]
    steps = [step for step in plan['steps'] if step['contract'] == CONTRACT + ':' + mode]
    require(len(steps) == 1, 'exact readiness step required')
    step = steps[0]
    require(step['action'] == 'driver' and step['effects'] == 'read-only'
            and step['phase'] == ('setup' if mode == 'native-boot' else 'restore')
            and step['args'] == args and step['timeout_seconds'] >= 180
            and step['max_attempts'] == 1, 'readiness contract/argv/deadline mismatch')
    require(artifact(plan, step['artifact']) == Path(__file__).resolve(), 'readiness executable pin mismatch')
    policy = read_json(artifact(plan, policy_id))
    ipaddress.ip_address(policy['host_address'])
    host_keys, guest_keys = (artifact(plan, policy[name]) for name in ('host_keys', 'guest_keys'))
    require(len(key_rows(host_keys, 'f3')) == 1 and len(key_rows(host_keys, 'f3')[0]) >= 2
            and key_rows(host_keys, 'f3')[0][0] == 'ssh-ed25519',
            'exact pinned f3 ed25519 key required')
    targets = {target['name']: target for target in plan['targets']}
    require(len(plan['targets']) == 5 and set(targets) == NAMES, 'exact five-target Plan inventory required')
    for name, target in targets.items():
        require(target['host'] == 'f3' and target['initial_state'] in ('Running', 'Stopped')
                and (name == 'rocky' or target['initial_state'] == 'Stopped'), 'target context mismatch')
        require(isinstance(target['kernel'], str) and target['kernel']
                and '\n' not in target['kernel'], 'inspected target kernel required')
        require(isinstance(target['address'], str) and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.-]*', target['address']),
                'safe Plan target address required')
    for index in range(3):
        address = '172.31.253.' + str(11 + index)
        require(targets['hfs-test' + str(index)]['address'] == address, 'exact native address required')
        expected = key_rows(HERE.parents[1] / 'docs/lab/f3-known_hosts', address)
        require(len(expected) == 1 and key_rows(guest_keys, address) == expected,
                'exact repository native SSH pin required: ' + address)
    if mode == 'rocky-restore':
        require(targets['rocky']['initial_state'] == 'Running', 'Rocky restoration requires initially running Rocky')
        rows = key_rows(guest_keys, targets['rocky']['address'])
        require(len(rows) == 1 and len(rows[0]) >= 2 and rows[0][0] == 'ssh-ed25519', 'exact pinned Rocky ed25519 key required')
    return targets, policy, host_keys, guest_keys


def check_inventory(stdout, mode, allow_booting=False):
    rows = stdout.decode().strip().splitlines()
    require(rows and rows[0].split() == ['NAME', 'DATASTORE', 'LOADER', 'CPU', 'MEMORY', 'VNC', 'AUTO', 'STATE'],
            'complete vm list header required')
    states = {}
    for row in rows[1:]:
        match = re.fullmatch(r'(\S+)\s+\S+\s+\S+\s+[0-9]+\s+\S+\s+\S+\s+'
                             r'(?:No|Yes(?:\s+\[[0-9]+\])?)\s+(Stopped|Running|Bootloader)'
                             r'(?:\s+\(([1-9][0-9]*)\))?', row)
        require(match is not None, 'unknown/malformed inventory row: ' + row)
        name, state, pid = match.groups()
        require(name not in states and ((state != 'Stopped') == (pid is not None)), 'ambiguous inventory: ' + name)
        states[name] = state
    require(set(states) == NAMES, 'exact completed five-VM inventory required')
    running = {'hfs-test0', 'hfs-test1', 'hfs-test2'} if mode == 'native-boot' else {'rocky'}
    # A completed start request is asynchronous. Only expected targets may be
    # stopped or in the known bootloader state while waiting; every other VM
    # must already be stopped, even during this boot window.
    require(all(states[name] == 'Stopped' for name in NAMES - running),
            'forbidden active VM in completed inventory for ' + mode)
    pending = sorted(name for name in running if states[name] != 'Running')
    require(allow_booting or not pending,
            'completed VM inventory does not establish ' + mode)
    if pending:
        print(mode + ': completed boot inventory not ready: ' + ','.join(pending), flush=True)
        return False
    print(mode + ': exact completed VM inventory verified', flush=True)
    return True


def execute(mode, policy_id):
    deadline = time.monotonic() + TOTAL_SECONDS
    targets, policy, host_keys, guest_keys = load_context(mode, policy_id)
    base = ['/usr/bin/ssh', '-F', '/dev/null', '-o', 'BatchMode=yes', '-o',
            'StrictHostKeyChecking=yes', '-o', 'GlobalKnownHostsFile=/dev/null',
            '-o', 'UpdateHostKeys=no', '-o', 'ControlMaster=no', '-o',
            'ControlPath=none', '-o', 'ConnectTimeout=3', '-o',
            'ServerAliveInterval=2', '-o', 'ServerAliveCountMax=1']
    host = base + ['-o', 'UserKnownHostsFile=' + str(host_keys), '-o',
                   'HostName=' + policy['host_address'], '-o', 'HostKeyAlias=f3']
    proxy = host + ['-W', '%h:%p', 'paul@f3']
    def inventory(allow_booting=False):
        code, stdout, _ = command(host + ['paul@f3', 'doas -n vm list'], deadline)
        require(code == 0, 'completed inventory command failed')
        return check_inventory(stdout, mode, allow_booting)
    ready = inventory(allow_booting=True)
    while not ready and time.monotonic() < deadline:
        time.sleep(min(2, max(0, deadline - time.monotonic())))
        if time.monotonic() < deadline:
            ready = inventory(allow_booting=True)
    require(ready, 'completed boot inventories exhausted deadline')
    pending = {name: target for name, target in targets.items()
               if (name.startswith('hfs-test') if mode == 'native-boot' else name == 'rocky')}
    while pending and time.monotonic() < deadline:
        for name, target in list(pending.items()):
            if time.monotonic() >= deadline:
                break
            # Only a positively completed nc exit 1 permits another boot probe.
            tcp = host + ['paul@f3', 'exec /usr/bin/nc -z -w 1 ' + target['address'] + ' 22']
            code, stdout, _ = command(tcp, deadline)
            if code == 1 and not stdout:
                continue
            require(code == 0 and not stdout, 'unexpected completed TCP probe result: ' + name)
            identity = base + ['-o', 'UserKnownHostsFile=' + str(guest_keys),
                               '-o', 'ProxyCommand=' + shlex.join(proxy),
                               'root@' + target['address'],
                               '/usr/bin/uname -r; /usr/bin/id -u']
            code, stdout, _ = command(identity, deadline)
            require(code == 0 and stdout.splitlines() == [target['kernel'].encode(), b'0'],
                    'completed guest kernel/UID verification failed: ' + name)
            print(name + ': pinned root SSH ready, kernel=' + target['kernel'], flush=True)
            del pending[name]
        if pending and time.monotonic() < deadline:
            time.sleep(min(2, max(0, deadline - time.monotonic())))
    require(not pending, 'completed not-ready probes exhausted deadline: ' + ','.join(sorted(pending)))
    inventory()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', required=True, choices=('native-boot', 'rocky-restore'))
    parser.add_argument('--policy-artifact', required=True)
    args = parser.parse_args()
    def interrupted(signum, _frame):
        raise Uncertain('interrupted; remote completion requires observation')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        execute(args.mode, args.policy_artifact)
        return 0
    except Uncertain as error:
        print(json.dumps({'status': 'uncertain', 'error': str(error),
                          'remote_completion': 'reconciliation required; never retry to success'}), file=sys.stderr)
        return 1
    except (Blocked, OSError, KeyError, ValueError, TypeError) as error:
        print(json.dumps({'status': 'blocked', 'error': str(error)}), file=sys.stderr)
        return 20


if __name__ == '__main__':
    sys.exit(main())
