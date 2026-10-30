# Native single-node behavior driver

Task `xq3` supplies `tools/lab/check-native-single-node.sh`, a workstation driver
for the existing [Stage 1 runner](Lab-runner.md). Writing and locally checking
this driver is not native runtime evidence. Task `yq3` remains the separately
approved FreeBSD gate. No lab contact or VM lifecycle operation was performed
while implementing the driver.

## Offline preparation

Build the exact repository commands on the workstation, with bounded build
parallelism, then inspect their embedded command paths/platforms and hashes:

```sh
GOMAXPROCS=2 GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 \
  go build -p 1 -o /tmp/hfs-native-daemon ./cmd/homelabfs
GOMAXPROCS=2 GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 \
  go build -p 1 -o /tmp/hfs-native-behavior ./cmd/hfs-behavior
tools/lab/check-native-single-node.sh --dry-run UNIQUE_RUN_ID \
  /tmp/hfs-native-daemon /tmp/hfs-native-behavior > native-spec.json
tools/lab/check-native-single-node.sh --outer-fragment native-spec.json \
  > native-fragment.json
```

Both commands are offline: no SSH, binary execution, dataset creation or run-ID
reservation. Use new output files and a fresh ID. Socket length is checked
before SSH: the exact `/var/tmp/hfs-run-ID/c/node/admin-UUID.sock` path plus NUL
must fit FreeBSD's 104-byte capacity. Shorten the ID if it does not fit.

The fragment is **not** a complete or approved plan. Merge its artifacts,
resources, selection and four driver steps into a complete existing `hfs-lab`
Stage 1 Plan. Supply current target/version evidence, distinct controller state,
creation/expiry, the reviewed preflight and all required VM lifecycle/restoration
steps. The first driver remains `prepared-lab-preflight-v1`; this native driver
does not implement that general initial-inventory contract or start/stop VMs.
Its setup/test calls occur only after the outer lifecycle has admitted and
started the three guests. It independently requires all three to be running,
all other VMs stopped, 2 GiB/two CPUs per guest, sufficient physical memory and
zero active swap. Inspect actual state rather than historical inventory.

Keep exact fragment argv, contracts, artifacts and resource caps. The driver
requires native collection to pair with exactly the native setup/test IDs via
`applies_after`. If admission fails before either was attempted, the runner
skips guest collection; it still reports the admission failure and skips
inapplicable cleanup/restoration. A known partial native setup or test failure
still collects diagnostics. Uncertain outcomes continue to block all actions
pending reconciliation. Existing approved plans remain eligible for cleanup.
The driver
requires setup/test/collect/cleanup deadlines of at least 900/2400/180/180
seconds; its own shorter internal limits remain fixed. Include their sums in
the outer phase budgets along with the other drivers/lifecycle steps. Allocate
at least 32 MiB of evidence budget, 168-hour retention and all twelve resource
caps (three 320 MiB run directories and nine 128 MiB datasets). The I/O budget
must cover at least twelve 4 MiB logical fixture caps. These are logical data
and finite syscall-case bounds, not a physical-device bandwidth quota. ZFS
quotas separately bound retained source history and workspaces.

Canonicalize the complete plan with `hfs-lab plan`. The operator reviews it and
records fresh explicit approval of that exact hash, then invokes `hfs-lab
execute --plan ... --approval ...` as described in [Test automation](Test-automation.md).
There is no separate approval command/token in this driver. `--approved-run`
is intentionally unsupported. Runner environment variables and command flags
are not consent; drivers execute only under the approved outer runner boundary.

## Native operations and evidence

The driver pins its own Bash/Python/guest scripts, both FreeBSD binaries, the
subplan and `docs/lab/f3-known_hosts` in the outer plan. Guest SSH uses strict
pinned keys through `paul@f3`. No package, system account, FUSE rule, VM disk,
autostart setting, production consumer or replication sink is changed.

Setup refuses existing local evidence, remote `/var/tmp/hfs-run-ID` and child
datasets. It stages exact binaries under that fresh root and creates
`hfsdataN/{data,journal,control}/hfs-run-ID`, each with a 128 MiB quota and a local
plan-hash ownership property. Explicit mountpoints are compact `d`, `j`, `c`
children of the run root. Parent/child GUIDs, local ownership properties,
mountpoints, quotas, root ownership/modes and binary digests are checked before
workload calls. Existing parent datasets are never initialized or overlaid.

Each guest initializes exactly one node and volume through the real
[`homelabfs behavior`](spec/Disposable-behavior.md) command, then sequentially
reopens that same fixture for root/direct, root/buffered,
unprivileged/direct and unprivileged/buffered. Only the first call gets
`--initialize`. The daemon always runs as root; syscall helpers use the exact
fixed credentials specified by the command. No OS account creation is needed.
The native gate must still prove this full cross-role sequence at runtime.

Every command has a 95-second outer deadline. On expiry its exact owned child
receives TERM once and gets ten seconds to exit. If it remains, the supervisor
preserves it, its mount and datasets; it never sends KILL, aborts FUSE or force
unmounts. SSH has its own finite connection/liveness/command deadlines. An
uncertain transport, mount or live child returns an unknown nonzero status to
the outer runner, preventing automatic lifecycle restoration. A failed case
is terminal assertion exit 10 only after the guest proves the daemon/helpers
have exited and the mount is absent. A timed-out but reaped child returns known
terminal timeout exit 30 only after the same idle and dataset guards succeed.
An outstanding child or uncertain mount remains an unknown nonzero outcome.

Each command normally unmounts through its owning daemon. The driver verifies
no remaining mount/process before another invocation. It validates JSON and
JUnit independently, including all required case passes, only the explicit
profile/multiuser skips, matching roots/helper credentials/digest, actual storage
mapping, stable node/volume and fresh generation/session for every replay.

Versions, GUID enrollment, readiness, command arguments/PID/deadline/exit status,
stderr, mount inventories, three-root markers and JSON/JUnit reports are copied
to `docs/lab/results/ID/native-single-node/hfs-testN`. Collection reads only the
bounded evidence directory; it never traverses the FUSE mount or copies backing
payloads. Collection is attempted after a failed workload too. The outer runner
owns durable orchestration state, categories and final JSON/JUnit summary.

Cleanup is an idempotent quiescence check: it proves the run is normally
unmounted and has no daemon/helper processes, then records remaining storage.
Successful and failed run roots/datasets remain for evidence and inspection.
An absent root is accepted only when its child datasets are absent too. Any
orphan or uncertain resource fails cleanup. Removal after the 168-hour
retention window is a separate approved operation; this driver never deletes
data, destroys datasets or makes a failed test pass by cleanup.
