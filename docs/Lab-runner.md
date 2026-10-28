# Plan-bound lab runner

`cmd/hfs-lab` implements the external approval, execution, recovery and reporting
control plane described in [Test automation](Test-automation.md). It does not
implement the native test matrix, deployment, guest provisioning, cloning, or
the future scenario drivers. It never imports the throwaway PoC module.

The separate [Stage 2 live smoke profile](Live-smoke-runner.md) reuses this
control plane with dedicated volumes, a pinned adapter protocol and a bounded
Rocky probe. It rejects all Stage 1 VM lifecycle actions and requires its own
plan and approval. The prepared-guest details below describe Stage 1.

The first profile reuses the prepared three guests on f3. Its supported initial
state is all three lab guests and the unrelated `freebsd` VM stopped, with
`rocky` either running or stopped. A different initial state requires a new
supported lifecycle contract, not a guessed restoration sequence. Planning
does not change that state. Do not run the creation script or overwrite disks.

No executable plan is supplied by this change: the pinned preflight and
scenario drivers still need implementation and review. Naming a contract in
JSON does not implement it. The runner refuses missing or changed artifacts;
an invented preflight success is not valid evidence. A fake-executor unit test
pass is not a Stage 1 integration pass.

## Prepare and review a plan

Build with `go build -o bin/hfs-lab ./cmd/hfs-lab`. `hfs-lab -version` prints the
project version. `plan` reads a complete JSON input, validates it, verifies all
local artifact checksums, writes a canonical plan without overwriting a file,
and prints its SHA-256. It performs no lab contact or workload execution:

```sh
bin/hfs-lab plan --input proposed-plan.json --out reviewed-plan.json
```

Every field is hash-bound. JSON whitespace and key ordering do not affect the
hash. Unknown fields and trailing JSON values are rejected. There are no
post-approval defaults. Plan validity is at most 24 hours; execution and forward
resume must occur within that window. Use a unique run ID and new controller
state/evidence directories for every plan.

The full schema is defined by the exported types in
[`internal/labrunner/plan.go`](../internal/labrunner/plan.go). Required inputs:

| Field | Reviewed content |
|---|---|
| `schema`, `run_id`, `stage`, `profile` | Schema `1`, unique bounded ID, `stage1`, specific profile name |
| `created`, `expires` | Explicit RFC 3339 timestamps |
| `prepared_guests` | Must be `true`; provisioning is unsupported |
| `targets` | Exactly `hfs-test0`–`hfs-test2`, `rocky`, `freebsd` on `f3`; address, initial state, kernel and package-manifest evidence for each |
| `artifacts` | Unique ID, absolute local path, immutable version, lowercase SHA-256 for every driver, lifecycle helper, package and required input |
| `resources` | Unique run-owned directories or guest datasets, target, location and byte cap; reusable VMs belong in `targets` |
| `budget` | Guest memory, host headroom, CPUs, disk, I/O, evidence bytes, retention hours and separate execution/cleanup/restoration seconds |
| `disruptions` | Explicit allowed disruptions; `rocky-downtime` is mandatory when initially running Rocky is stopped |
| `tests` | Explicit selection identifiers and integer seeds |
| `state_dir`, `evidence_dir` | Distinct absolute paths on the external controller, outside the disposable cluster |
| `preserve_failures` | Driver cleanup policy for failed payloads within the approved size and retention caps |
| `steps` | Complete ordered setup, test, collection, cleanup and restoration schedule |

The prepared hardware profile requires 6 GiB total guest memory, six guest CPUs,
and at least 4 GiB host headroom. All budgets must be positive. Sum of resource
caps must fit the disk budget. Directory resources are confined to
`/var/tmp/hfs-run-RUN_ID[/...]`; dataset resources to
`hfsdataN/{data,journal,control}/hfs-run-RUN_ID[/...]` on `hfs-testN`.
No broad production path or received replication sink is accepted.

Each step specifies `id`, `phase`, `action`, `artifact`, exact `args`, reviewed
`contract`, `effects`, `timeout_seconds`, `max_attempts`, `applies_after`, and
`idempotent`. IDs are safe bounded identifiers. Deadlines are positive and at
most one day; attempt limits are between one and three. Sum of step deadlines
must fit each approved phase budget, including every permitted teardown attempt.
The runner never retries forward work
automatically. A recovery command may retry a known failed idempotent cleanup
step within its approved attempt limit, retaining every prior transition.

Every selected test must have at least one `phase: "test"`, `action: "driver"`
step whose `test` field names that selection. Other phases leave `test` empty.
Duplicate selections, unselected test references and lifecycle-only plans with
declared tests are rejected. The selected seed remains bound in the plan read
by that driver; declaring a test without executing its driver cannot pass.

`action` is either `driver`, or one of `stop-rocky`, `start-lab`, `stop-lab`,
`start-rocky`. VM actions execute the pinned `tools/lab/vms.sh` artifact with
exactly `--approved-action ACTION`; custom arguments are rejected. Lab startup
and shutdown must be paired. Rocky stop and restoration must exactly match its
reviewed initial state. A previously stopped Rocky remains stopped.

The first step must be a read-only `prepared-lab-preflight-v1` driver. Before
any mutation that driver must compare **live** inventory with the complete
approved initial state, reject other running VMs, inspect active users and
host memory pressure/swap/disk capacity, check guest allocations and approved
artifact/package/kernel inputs, and verify pinned SSH access using
`docs/lab/f3-known_hosts`. It must fail closed when any check is unavailable.
The lifecycle helper additionally rechecks its fixed memory admission rules.
Recorded historical inventory is not a substitute for this check.

## Record approval and execute

The operator reviews the canonical plan and every executable driver, then
records an approval file containing exactly:

```json
{
  "plan_hash": "THE_HASH_PRINTED_BY_PLAN",
  "operator": "OPERATOR_NAME",
  "approved_at": "EXPLICIT_CURRENT_RFC3339_TIMESTAMP",
  "statement": "I approve this exact plan"
}
```

The placeholders above are deliberately not executable approval. An operator
must enter the actual hash, identity and timestamp after explicit approval.
There is no `approve` command, generated token, prompt default, CI auto-approval,
or elapsed-time approval. Local files are a trusted operator boundary, not a
cryptographic identity service; protect them and the controller account.

Only after fresh approval for that exact run:

```sh
bin/hfs-lab execute --plan reviewed-plan.json --approval operator-approval.json
```

Changed targets, argv, artifacts, budgets, disruption scope, tests, seeds or any
other plan input require a new plan and approval. Artifact bytes are verified
before execution and again immediately before each command. Keep the approved
artifact files immutable and available until recovery completes.

## Driver boundary

Drivers are local executable artifacts invoked with the exact approved argv,
without shell evaluation or implicit command arguments. Their working directory
is `state_dir`. The runner supplies `HFS_LAB_PLAN` (the canonical controller
snapshot), `HFS_LAB_PLAN_HASH`, and `HFS_LAB_RUN_ID`. Drivers must read that plan
and verify their declared contract. The environment includes a fixed system
`PATH`, the operator's `HOME` for SSH configuration, and `SSH_AUTH_SOCK` when
present. Other ambient environment variables are not inherited. Private keys
are never copied into a guest or included in reports.

Driver effects are `read-only` or `run-owned`; VM actions use `lifecycle`.
Collection is read-only. Every mutating setup/test driver needs paired cleanup
via `applies_after`. Cleanup/restoration must be idempotent and refer only to
earlier forward step IDs. The driver contract must enforce approved resource
ownership, I/O/storage/retention bounds, pinned guest host keys, expected mounts,
normal unmounting, diagnostic collection and failure preservation. The runner
enforces process deadlines and per-step log caps; it cannot establish a remote
driver's I/O safety from an argv string. Review the pinned implementation.

A driver owns and reaps every subprocess and remote operation it starts. Exit
`0` means terminal success. These nonzero exits certify a **known terminal**
failure with no outstanding child/remote action: `10` assertion, `20`
infrastructure, `30` timeout, `40` cleanup, `50` restoration. Unknown exits,
signals, controller timeouts and VM-helper transport errors are uncertain.
Never return a known-terminal code while a remote action might still run.

The runner collects declared read-only evidence after a known assertion or
infrastructure failure, within the original runtime budget. It then runs
applicable cleanup, and restoration only after all applicable cleanup succeeds.
It never starts Rocky while lab shutdown remains failed or uncertain. A cleanup
failure remains separate from an assertion failure in JSON and JUnit.

## Status and recovery

```sh
bin/hfs-lab status --plan reviewed-plan.json
bin/hfs-lab cleanup --plan reviewed-plan.json --approval operator-approval.json
```

`status` reads controller state only. Cleanup requires the original approval
and exact unchanged plan/artifacts, but may occur after plan expiry to avoid
stranding a run. It never resumes workload execution. Reissuing `execute`
resumes completed known steps without replay, within plan validity/runtime;
completed unsuccessful runs still return nonzero.

`state_dir` holds an exclusive controller lock, `plan.json`, and an atomic,
file-and-directory-synced `journal.json` checkpoint. The checkpoint contains
current step states and full started/terminal transition history, outside the
failed guest cluster. Do not edit or delete it to bypass a failed run.

A step left `started` after controller failure, or marked `ambiguous`, blocks
all subsequent actions. Missing SSH or a missing process alone cannot establish
success. Inspect the specific command and its effects, establish that no action
is still outstanding, and record a separate reconciliation JSON with
`plan_hash`, `step_id`, `outcome` (`passed` or `failed`), `operator`, `observed_at`,
concrete `evidence`, and `no_outstanding_action: true`. Supply that file with
`--reconcile FILE` to `execute` or `cleanup`. Successful reconciliation permits
forward resume only while the original plan is fresh; failed reconciliation
permits cleanup and preserves the failed result. Additional unresolved steps
continue to block actions. This is an explicit operator observation, not an
automatic repair or approval for a changed plan.

The runner emits a human summary, JSON on standard output, and durable
`report.json` plus JUnit `report.xml` in `evidence_dir`. Reports include plan hash,
versions, seeds, timings, categorized step outcomes, transition history and a
conservative inventory of resources that may remain. Logs are capped per step
from the approved evidence budget. Drivers must separately cap other evidence
files. Targets are reusable and are never deleted by this runner. An incomplete
run, failed assertion, failed cleanup, or failed restoration returns nonzero;
recovered teardown does not erase a failed test or manufacture a suite pass.

Unit verification uses only fake executors and temporary controller files.
Live inventory checks, VM lifecycle, driver execution and tests still require
their own fresh explicit approval under [AGENTS.md](../AGENTS.md).
