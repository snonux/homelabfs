# Native fault primitives and evidence

Task `ar3` adds the Stage 1 fault library in `internal/labrunner`, the local
`hfs-lab fault-request` / `native-fault` boundaries, and a narrow `vms.sh` fault
stop. These are building blocks for reviewed scenario drivers. They do not
implement the native integration matrix, automatically install a remote driver,
enroll the prepared guests, or establish replication/failover correctness.
No lab run, VM stop, packet-filter change or ZFS fault was executed to validate
this implementation. Verification uses fake command executors, captured shell
recipes and owned synthetic local subprocesses only.

Every actual run and disruptive VM window still requires fresh explicit user
approval of its complete immutable plan. Neither a request JSON, an environment
value, a previously approved plan nor `--approved-fault` grants approval. Read
[lab operations](Lab-operations.md), [test automation](Test-automation.md), and
[replication ownership](Lab-replication.md) before preparing an actual run.
The f2 replication sink, production data/consumers, Rocky power loss and physical
host failures are outside these primitives.

## Plan and driver boundary

Optional `Plan.Faults` binds each fault ID, test step, paired cleanup step, exact
`hfs-test0`–`hfs-test2` target, node/volume/workspace generation, resource, private
admin socket, daemon log and action parameters. A fault test uses the exact
contract `native-fault-v1:FAULT_ID`; paired idempotent cleanup uses
`native-fault-cleanup-v1:FAULT_ID`. Both are pinned driver artifacts with no
custom argv. The mutation also requires `disruptions: ["fault:FAULT_ID", ...]`
and `preserve_failures:true`. Collection is a separate read-only collection
step. Stage 2 rejects this profile.

`PrepareFaultRequest` verifies the original approval/artifact bytes and produces
one exact command recipe, selected test seed, approved evidence cap and deadline.
`RunFaultStep` invokes a supplied `FaultExecutor` and validates its receipt.
Requests and receipts carry protocol `hfs-native-fault-v1`. They are bounded
JSON, including exact plan hash and target identities. Every missing/foreign,
nonterminal, oversized or late receipt remains uncertain; unsupported capability
is an unsuccessful outcome. A cleanup request can use the original approval
after plan expiry, and retains the original test seed.

`hfs-lab fault-request --plan FILE --approval FILE --step STEP --out NEW_FILE`
is controller-only preparation with no lab contact. `hfs-lab native-fault
--input REQUEST_FILE` runs the actual native backend **inside the exact enrolled
guest**, rechecks approval/plan/request binding and returns structured JSON.
The reviewed scenario driver owns pinned SSH transport through f3, transfer of
these bounded inputs, process lifecycle, receipt persistence and mapping known
terminal failures to the outer runner's exit-code contract. This package does
not treat a lost SSH connection or a generic CLI exit as terminal evidence.
It does not run an opaque command recipe supplied by an unvalidated caller.

## Runtime enrollment

The separately reviewed setup driver must prepare the private mode 0700 real
run root `/var/tmp/hfs-run-RUN_ID`, plus its explicitly planned child paths,
without following ancestor symlinks. It records an owned, mode 0600, singly
linked `fault-enrollment.json` containing `FaultEnrollment`: exact run/plan/guest/
node/volume/generation, live dataset GUIDs and network leases. A literal workspace
generation must match exactly. The current daemon generates a fresh unpredictable
workspace at startup, so its normal fault plan instead declares the explicit
`generation_binding:"runtime-readiness-v1"` policy and leaves `generation` empty.
This is a hash-bound authorization for the following readiness enrollment
protocol, never a default or an unknown-generation fallback:

1. Prepare and approve the complete plan before starting the lab. Bind the exact
   run/node/volume, private daemon socket, supervisor handle, executable digest,
   startup driver and run-owned resources; include the explicit runtime policy.
2. During that approved setup, launch the foreground child through
   `StartFaultSupervisor`. `SupervisorConfig.PlanHash` identifies that approved
   plan. The supervisor records PID/executable and fresh `Process.Birth`; its
   own admin generation is **that launch nonce**. Signal admission stays closed.
3. After the child publishes its private daemon status, call `BindWorkspace`.
   It requires the kernel peer PID of the actual connected status socket to equal
   the unreaped owned child PID before sending the request, then checks that
   child remains alive afterward. It verifies the exact configured
   node/volume/socket and records the actual
   independently generated daemon workspace. It writes the immutable private
   `supervisor-HANDLE.serving.json` binding and then opens signal admission.
4. The reviewed setup transport copies those exact bytes to the external
   controller as `state_dir/fault-binding-FAULT_ID.json`, and records the same
   workspace in the guest `FaultEnrollment`. The binding carries approved plan
   hash, target, full launch identity, socket, readiness status and observation
   time. No binding is accepted for another executable/launch/node/volume/run.
5. Only then prepare the fault request. The controller requires that protected
   binding; the native backend compares its exact bytes with the local immutable
   binding. Daemon controls use the learned **workspace generation** plus that child kernel PID. Supervisor
   signals use the separate **Birth nonce** as expected generation. Receipts
   retain both identities; neither is substituted for the other.

A same-UID parent or unrelated server reporting identical node/volume/workspace
cannot provide readiness for the owned child. Linux uses SO_PEERCRED. The native
FreeBSD amd64 adapter reads LOCAL_PEERCRED's actual cr_pid from the checked
88-byte xucred v0 ABI, with PID at byte 80, for supported 14.4/15.0/15.1 release
ranges. Returned size, version, group bounds and positive PID are mandatory;
unknown architectures/releases/layouts fail closed. The official
[14.4](https://raw.githubusercontent.com/freebsd/freebsd-src/releng/14.4/sys/sys/ucred.h),
[15.0](https://raw.githubusercontent.com/freebsd/freebsd-src/releng/15.0/sys/sys/ucred.h)
and [15.1](https://raw.githubusercontent.com/freebsd/freebsd-src/releng/15.1/sys/sys/ucred.h)
headers define this union. No launch nonce environment variable or implicit test
hook enablement is introduced. Native credential runtime validation remains a
separately approved gate; the ABI decoder has host-side fixture tests only.

A caller cannot omit the runtime policy, supply an arbitrary unknown generation,
or reopen supervisor admission after gate closure. A daemon restart requires a
new supervised launch and readiness binding; the immutable old binding survives
and cannot silently authorize its replacement. Actual transport/enrollment by
the reviewed setup driver remains outside these primitives.

The native backend serializes actions with a run-local lock. Before admitting a
side effect it syncs an `outstanding` step intent into the enrollment. A failed,
interrupted or ambiguous effect leaves this guard and all payloads/resources
intact; another action refuses it. Operator investigation must establish the
specific action is terminal before a separately reviewed reconciliation changes
the guard. Neither retry nor cleanup clears uncertain effects automatically.
There is no native reconciliation/repair CLI in this change.

Quota faults target an explicit **child** of a run dataset, never a pool,
shared dataset or run parent. The backend checks its live GUID and
`org.homelabfs:run` property, requires its original quota to equal the approved
resource cap, sets the bounded smaller quota and observes the result. The
workload must separately demonstrate ENOSPC. Cleanup restores the original cap
idempotently and never removes failed data. `gnop-error` is explicitly unsupported:
a provider below an existing pool cannot safely isolate one run child dataset.

## Owned daemon supervision

`StartFaultSupervisor` directly starts the reviewed foreground daemon executable,
checks its run-owned path/checksum, records a fresh birth nonce with its actual
PID and logical handle, and retains the child unreaped. Planned process inputs
have PID zero and an empty birth value; these are launch observations, not values
invented before approval. The private launch manifest binds the actual identity. Its Birth identifies
only the supervised process launch, never the daemon workspace. The daemon
must not daemonize or replace itself with a different executable.

Only SIGKILL, SIGSTOP and SIGCONT are admitted through the separate private
supervisor admin socket. Requests include a digest of the complete launch
identity and the exact expected node/volume/**launch Birth**. Daemon test-fault
controls separately bind the actual daemon workspace generation. The existing peer UID,
private socket and durable control audit checks apply. No `ps`-discovered PID,
`pkill`, detached process or check-then-kill shell sequence is admitted.
Linux waitid with WNOWAIT and FreeBSD kqueue NOTE_EXIT observe exit without
reaping. Signal admission and gate closure share a mutex; the admin endpoint is
drained after closing the gate and before Wait/reap. An exited zombie therefore
reserves the PID while signals can be admitted. Closed/exited targets refuse
new signals. The supervisor never kills/resumes a child as implicit cleanup;
its caller must perform the separately approved action before Close waits.
A pause fault's paired cleanup sends CONT to that same enrolled child.

The daemon failpoint primitive sends the real version-1 administrative
`journal-before-sync` toggle with exact expected target fields. Hooks remain
default-off in daemon configuration; the runner never enables them implicitly.
It records status before/after and preserves requested/applied/rejected/denied
admin audit evidence. Disabling the hook never claims to repair a blocked service.

## Guest network faults

Approved setup must already enable PF/dummynet, install an attached wildcard
anchor `hfs-run-RUN_ID/*`, and reserve each exact child anchor and numeric pipe.
The native backend never enables PF, changes root rules or loads kernel modules.
A network lease records root/anchor rule hashes, the exact peer, dedicated
replication port, direction, anchor and exclusive pipe ID. Ports below 1024 and
etcd's control ports are refused. Root rule changes, unattached anchors, duplicate
state labels and an unexpectedly occupied pipe all stop before mutation.

Recipes affect one guest-to-guest TCP replication link without assuming which
guest initiated the connection. For the selected `in` or `out` direction, two
rules match the exact peer IPs and dedicated test port: one uses source port N,
the other destination port N. Their distinct owned labels are
`hfs-RUN_ID-FAULT_ID-src` and `hfs-RUN_ID-FAULT_ID-dst`; both complete labels must
fit PF's 63-byte bound. Approved setup reserves that port exclusively for test
replication and preinstalls both labels before opening test connections.
They support bounded
loss, delay, one-way partition and reset-on-next-segment via `block return-rst`.
PF states bypass new filter rules, so only the two exclusive run rule labels'
states are invalidated after installing the fault. Each label must occur exactly
once in the run anchor and once across the live anchor tree before mutation.
No global flush or host/address-wide state kill is generated. Setup must ensure
all existing test replication states carry those owned labels; unrelated PF
states are never adopted or invalidated.
These forms use the native [PF/dummynet rule interface](https://man.freebsd.org/cgi/man.cgi?query=pf.conf)
and [label-scoped state invalidation](https://man.freebsd.org/cgi/man.cgi?query=pfctl&manpath=FreeBSD+15.1-RELEASE).

Cleanup restores both narrow pass rules, invalidates both owned labels and removes only its own
configured pipe. Root rules and SSH/control traffic remain outside all recipes.
Actual PF parser/kernel capability, packet direction, loss/delay and TCP reset
behavior still need an explicitly approved native validation run; compiled Go
and fake command tests are not that evidence.

## Whole-guest test faults

`vms.sh --approved-fault RUN_ID hfs-testN clean|abrupt` is invoked only by the
approved pinned host driver. It requires matching runner run/hash environment,
a real private root-owned run directory and validated ancestors, a setup-created
regular non-symlink root-owned mode 0600 single-link run launch lock, and a
seven-line owned
`hfs-testN.vm-enrollment`: run ID, target, plan hash, actual VM UUID, canonical
config SHA-256, launch PID and exact `ps -o lstart=` output. It verifies the live
VM/config/launch, 2 GiB profile and `/usr/sbin/bhyve` identity before stop.
Root/ancestor/lock admission precedes lock acquisition. `lockf -kn` refuses a
missing lock and retains it; the helper creates neither directories nor a lock.
After acquisition it rechecks the paths, exact lock inode and enrollment while
held. The approved window must exclude concurrent external lifecycle administrators;
the run lock is not a durable global launch inhibition mechanism.

A clean fault sends only bounded ACPI stop. The separately approved abrupt test
fault uses vm-bhyve poweroff against that enrolled native guest. Both retain a
synced intent file, cap management calls, wait at most 120 seconds for stopped
state plus absent VMM domain, and preserve failure. They cannot target Rocky,
FreeBSD development VMs or a physical host. They do not restart a guest or
manipulate disks. The original normal lifecycle actions retain their graceful
behavior.

**Neither stop is fencing proof.** The approved `native-vm-stop-v1` takeover
policy permits graceful stop only and additionally requires durable inhibition,
exact authority/launch bindings, broker attestation and verified coverage. Those
broker requirements remain external. Abrupt lab fault injection must never be
reused as an approved fencing mechanism.

## Evidence

Receipts include action/target/run/plan, node/volume/generation, exact supervised
process identity when applicable, seed, start/finish/deadline, bounded daemon
logs, raw before/after admin status (including epochs and complete source/applied/
sync/async positions), kernel/package/runner/artifact versions and conservative
remaining-resource inventory. Unavailable status and unsupported/unknown replica
progress are recorded explicitly, never synthesized as success or zero lag.
The outer runner retains JSON/JUnit classifications and transition history;
scenario drivers must persist receipts outside the disposable guests. Failed
payloads and uncertain effects survive for inspection.
