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
pinned keys through `paul@f3`. Every native phase and binary transfer reads the
exact artifact-pinned policy named by the first `prepared-lab-preflight-v1`
step. Its literal `host_address` and `host_keys` artifact supply the f3 hop;
its `guest_keys` artifact must match all three repository native guest pins.
The historical subplan name `native-host-keys` still refers to those guest
pins. Context files must have absolute nonsymlink paths containing only
letters, digits, `_`, `.`, `/`, `+` or `-`, and their bytes are checked again
before every command.

Both SSH hops and SCP use `-F /dev/null`, strict host checking, explicit known
hosts files, disabled global known hosts and key updates, and disabled
connection sharing. The f3 hop uses the reviewed `HostName` and
`HostKeyAlias=f3`; guest access uses an explicit pinned `ProxyCommand`.
The native driver keeps its existing connection, liveness and phase limits.
No package, system account, FUSE rule, VM disk,
autostart setting, production consumer or replication sink is changed.

N9 collection returned SSH exit 255 with `Connection timed out during banner
exchange`. That observation did not identify which network hop or server
condition caused the failure. The explicit transport removes a confirmed
dependency on ambient SSH configuration; it does not establish that banner
timeouts are resolved. SSH 255, command timeout and other uncertain outcomes
stop immediately without retries or further diagnostic SSH commands. Only a
successful or proven terminal workload outcome permits collection. A changed
driver or transport policy requires a newly pinned and approved plan; archived
approved artifacts remain the recovery path for older runs.

Setup refuses existing local evidence, remote `/var/tmp/hfs-run-ID` and child
datasets. It stages exact binaries under that fresh root and creates
`hfsdataN/{data,journal,control}/hfs-run-ID`, each with a 128 MiB quota and a local
plan-hash ownership property. Explicit mountpoints are compact `d`, `j`, `c`
children of the run root. Parent/child GUIDs, local ownership properties,
mountpoints, quotas, root ownership/modes and binary digests are checked before
workload calls. Existing parent datasets are never initialized or overlaid.

Guest permission guards read FreeBSD `stat -f %Op` and mask the file type to
retain all `07777` permission bits. `%Lp` omits sticky/setuid/setgid: it reports
`777` for a correct `1777` `/var/tmp`. The shared temporary parent still requires
exact mode `1777`, and private run directories/files reject extra special bits.
Local regressions in `tools/lab/test_native_guest_modes.py` execute the shipped
guards against temporary inode modes with FreeBSD stat semantics; they provide
no native runtime acceptance evidence.

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

Controller command failures include bounded argv context, the actual child
return code and separate stdout/stderr tails in the outer runner's driver log.
Each stream retains at most its last 4096 bytes, with replacement decoding for
invalid UTF-8 and an omitted-byte count; argv retains at most its first and last
2048 characters plus a truncation marker. Control characters are JSON-escaped.
Timeouts retain available partial streams and mark the return code unavailable;
spawn errors mark both streams unavailable. Successful command stderr is logged
with the same bounds, while successful stdout is returned byte-for-byte for
machine parsing or archive collection and is never included in this diagnostic.
These diagnostics do not record stdin, environment values or private-key
contents. They do not change deadlines, evidence caps, failure categories or
the requirement to reconcile uncertain outcomes before further actions.

Cleanup is an idempotent quiescence check: it proves the run is normally
unmounted and has no daemon/helper processes, then records remaining storage.
Successful and failed run roots/datasets remain for evidence and inspection.
An absent root is accepted only when its child datasets are absent too. Any
orphan or uncertain resource fails cleanup. Removal after the 168-hour
retention window is a separate approved operation; this driver never deletes
data, destroys datasets or makes a failed test pass by cleanup.
