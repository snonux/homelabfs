import json
from pathlib import Path
import subprocess

root = Path('/home/paul/tmp/codex/hfs-s24-20261105-64_wmjyv')
(root / 'checks' / 'bin').mkdir(exist_ok=True)
commands = [
    ['go', 'build', './internal/controlledruntime'],
    ['go', 'build', '-o', str(root / 'checks' / 'bin' / 'hfs-controlled-runtime'), './cmd/hfs-controlled-runtime'],
    ['go', 'vet', './internal/controlledruntime'],
    ['/home/paul/go/bin/errcheck', './internal/controlledruntime'],
]
for command in commands:
    print(json.dumps({'command': command}), flush=True)
    subprocess.run(command, check=True)
paths = [
    'internal/controlledruntime/process_logs.go',
    'internal/controlledruntime/process_logs_test.go',
    'internal/controlledruntime/process_logs_freebsd.go',
    'internal/controlledruntime/process_logs_linux.go',
    'internal/controlledruntime/process_logs_parent_test.go',
    'internal/controlledruntime/process.go',
    'internal/controlledruntime/state.go',
    'internal/controlledruntime/runtime_budget.go',
    'internal/controlledruntime/driver.go',
    'internal/controlledruntime/process_observer_test.go',
    'internal/controlledruntime/assembly_test.go',
]
command = ['gofmt', '-l', *paths]
print(json.dumps({'command': command}), flush=True)
result = subprocess.run(command, check=True, text=True, capture_output=True)
if result.stdout or result.stderr:
    raise SystemExit('gofmt diagnostic: ' + result.stdout + result.stderr)
print(json.dumps({'result': 'all static checks passed'}), flush=True)
