# n4 restoration diagnosis (fx3)

This is a local, read-only analysis of recorded evidence, not a test run or an
operator reconciliation. Root owns remote observations and reconciliation under
the original approval. No SSH, VM operation, workload, helper edit, test, journal
edit, staging, commit, or task-state change was performed by this worker.

## Finding and safe disposition

The combined evidence supports the **failed** reconciliation of
`rocky-restoration-readiness` with `no_outstanding_action: true`. It does not
support marking the original step passed. The pinned helper deliberately treats
unproven SSH completion as uncertain, exits 1, and stops; that conservative
refusal is consistent with the documented driver contract.

There is a separate controller limitation: the current runner cannot mark this
run `complete=true` after a failed restoration reconciliation. Its `completed()`
requires every cleanup/restore result to be passed or skipped. Teardown considers
the failed restoration step again, but its only approved attempt is already
consumed. `step()` rejects it before calling the executor. The actual cleanup
receipt confirms a durable failed reconciliation, no ambiguous action, no new
execution attempt, `success=false`, `complete=false`, and an exhausted-attempt
error. This diagnoses and reconciles the uncertainty; it does **not** complete
or close the run under the runner’s completion rules. Do not change the journal, change
the approved plan, or call the later observation an original step pass to obtain
`complete=true`.

## Original execution evidence

Run `20261030-n4` used plan hash
`b4503fabe5cb6670b0537fec2c33e4f9be7bc449a74d65dcab6f9b67b23b48f1`.
`reviewed-plan-20261030-n4-r6.json`, its controller snapshot/journal, the initial
JSON/XML reports and execution log agree on the following UTC sequence:

- Prepared admission passed at 08:25:51.327251.
- Rocky stop passed at 08:26:01.130035; lab start passed at 08:26:02.381366.
- Native readiness failed at 08:26:02.801282 with known infrastructure exit 20.
  Its log names `hfs-test0 ... Bootloader (14489)` as an unaccepted transitional
  inventory row. This is a boot-readiness handling defect owned separately by
  ex3, not a native filesystem result.
- Native setup/test were never invoked; collection and native resource cleanup
  were skipped through their setup/test applicability gates. The twelve listed
  resources are the conservative plan inventory, not evidence of their creation.
- Lab stop passed at 08:26:04.286991; Rocky start passed at 08:26:05.382527.
  Starting a VM is asynchronous and does not itself prove guest SSH readiness.
- Restoration readiness started at 08:26:05.386704 and became ambiguous at
  08:26:08.469781, with driver exit 1. Its entire recorded log is the JSON error
  `SSH transport lost; remote completion unproven`. The initial report records
  `complete=false`, `success=false`. Root reports actual execute terminal
  `16a02f`, exit 1 (execution session 58355).

The pinned readiness helper and its private archived copy both had SHA-256
`0497ffb4d33d1ca1db06eb9f0c237a6ae9a1051e59c57dc6eb56095952a34068` when reviewed.
All 30 original current artifact paths and all 30 private archive copies matched
their plan SHA-256 pins during this review. Preserve those bytes through recovery.

## Which SSH operation is implicated

In the pinned helper, the first SSH command is the host inspection
`doas -n vm list`. `check_inventory()` prints and flushes an exact-completed-
inventory marker before any TCP or guest identity check. The restoration log
has no such marker and is far below its log cap. On the recorded helper's normal
control flow, this locates uncertainty at that **initial host inventory SSH
command**, before Rocky TCP/identity probing. This is a code-and-log inference;
the helper did not include the failing argv in its error. Do not describe this
record as proof of a failed Rocky guest SSH connection.

`command()` drains separate stdout/stderr pipes, then waits for the SSH child.
The exact logged message is raised for either return code 255 **or a negative
return code (signal)**. It sets `completed=True` before that check and reaps the
local child in `finally`. Thus the message alone does not distinguish those
return codes, prove that the remote command was dispatched, or establish remote
completion. An initial handoff described the event as SSH 255; that inference is corrected
here. The durable helper log does not record the numeric SSH return code.

The captured SSH stdout/stderr are not emitted when this branch raises, and
callers discard stderr for known completed commands too. Therefore the original
diagnostics cannot be recovered from these step logs. The three-second duration
does not identify DNS, authentication, handshake, network, server, or other
cause. Later working SSH and its successful-session warnings do not establish
the earlier cause. Retaining bounded argv/return-code/diagnostic context would
improve future diagnosis; the loss is a concrete evidence limitation, not proof
that the refusal itself was wrong. No retry-to-success change is justified by
the actual evidence.

## Fresh observations and what they establish

`n4-restoration-observation.json` records root's successful pinned read-only
commands, with return codes and full streams:

- At 08:27:22 UTC, f3 listed Rocky Running (17368), all three native guests and
  unrelated `freebsd` Stopped, zero swap use, no md devices and no run-owned
  temporary mounts in its mount inventory. Its process inspection found only
  the current observation shell matching the original inventory/nc/identity
  patterns.
- At 08:27:24 UTC, pinned root SSH via pinned f3 returned the approved Rocky
  kernel `5.14.0-687.61.1.el9_8.x86_64` and UID 0. Guest process inspection
  found only its current observation shell matching uname/id patterns.
- `n4-local-process-observation.json`, observed at 08:28:21.405951 UTC, records
  no matching original readiness CLI, n4 controller, or exact SSH identity/nc
  children. Root reports observation terminals `94e216` and `1bf127`, exit 0.

Local process absence alone is insufficient. In combination with the actual
terminal execution, specific host/guest process inspection, the helper's owned
and reaped local subprocess semantics, and its bounded read-only remote commands
(`vm list`, `exec nc -z -w 1`, `uname`/`id`, with no backgrounding or persistent
mutation), the observations establish that no original readiness action remains
outstanding. They also establish the desired VM/kernel/UID state **at their
observation times**. They do not prove uninterrupted restoration, original
readiness success, absence of every possible unrelated process, or a native
filesystem test pass. Stopped native guests were not reopened to inspect disks;
no need to boot them follows from this evidence review.

## Reconciliation and reporting criteria

Root recorded `reconcile-20261030-n4-restoration-failed.json` for the exact plan
and step with outcome `failed`, operator `Paul via Codex observing approved run`,
observed_at `2026-10-30T08:29:12Z`, concrete references to both observation JSON
files and actual execution, and `no_outstanding_action: true`. Original reports
are preserved as `report-before-reconciliation.json` and `.xml`, with the
original execution log as `execute-before-reconciliation.log`. No separate
pre-reconciliation journal copy was archived; the original reports preserve the
original transition history, and the current journal retains that history.
Root supplied the reconciliation only to `cleanup` with the original unchanged
plan, approval and artifacts. The current report preserves the original started
and ambiguous history alongside the appended failed reconciliation. The later
successful SSH observation remains a later observation.

Actual cleanup terminal `dd6ff0` exited 1; root's report verification terminal
`16050b` confirmed no ambiguous result, restoration readiness failed/reconciled,
and `complete=false`, `success=false`. Local review of
`cleanup-after-reconciliation.log`, the current JSON report and journal confirms
`approved attempt limit exhausted for rocky-restoration-readiness` and no new
started transition. The source puts that attempt-limit check before the
executor, so this cleanup refusal did not dispatch another readiness or lab
action. The existing passed lab-stop/Rocky-start steps remained passed.

Report separately: physical VM restoration observed, original readiness
uncertain then reconciled failed, no native setup/workload executed, no
outstanding original readiness action, and controller completion flag still
false. There is no claim of run closure/completion or native acceptance. Any
future workload run or changed readiness artifact needs a fresh exact-plan
approval.

## Self-review and limits

Reviewed AGENTS.md, Lab-operations, Test-automation, Lab-runner recovery contract,
task fx3 and its annotations, original plan/step schedule, journal, JSON/XML
reports, execution/step logs, pinned helper, observation records, artifact pins,
and the runner/reconciliation source. The agent-task-management skill was used
only for `ask info fx3`; IQ navigation supplied the f3/Rocky infrastructure
context. No tests were run. No original stderr or SSH return-code receipt exists
in the reviewed evidence. No claim of original restoration readiness success or
guessed transport cause is made. Actual reconciliation and cleanup receipts were
incorporated; runner completion remains false. Task fx3 can be concluded only
with the narrowed diagnosis-and-reconciliation scope, not a claim that the
failed run was closed.
