# Approval and script-driven test execution

[Native integration tests](Integration-tests.md) · [FreeBSD lab](FreeBSD-test-lab.md) · [Live smoke tests](Live-smoke-tests.md)

## Mandatory approval boundary

**Neither Stage 1 integration tests nor Stage 2 live smoke tests may start without Paul's explicit approval for that run.** Documentation changes, commits, package publication, deployment completion, CI success, and an earlier approved run do not authorize another test run. This applies to future automated executions; the completed PoCs remain historical evidence.

Each stage has its own approval. Finishing integration tests does not authorize deployment or live smoke tests automatically. Deployment itself follows the separately authorized rollout plan.

Read-only planning may inspect versions, available resources, and existing state. It must not provision or start test VMs, stop Rocky, change configuration, install packages, inject faults, create test volumes, or issue workload I/O. Those actions belong to approved execution.

Separately requested offline infrastructure preparation may create stopped VMs without authorizing a test run. The [f3 preparation](FreeBSD-lab-preparation.md) was explicitly requested and leaves the guests stopped. Its script does not authorize booting guests, stopping Rocky, or executing either test stage.

## Runner contract

Implement the lifecycle as version-controlled scripts with explicit inputs and machine-readable outputs. The intended workflow is:

```text
prepare plan → operator reviews and approves run → execute scripts → collect evidence → cleanup/restore → report
```

Entrypoints and their command syntax are still to be implemented. They must provide plan, execute-approved-run, status, and cleanup/recovery operations. The existing `tools/poc/` programs are individual experimental probes, not the future approved two-stage runner.

A plan records a run ID, stage, immutable artifact versions/checksums, targets, directories/volumes, resource budgets, allowed disruptions, tests/seeds, runtime and I/O limits, evidence destinations, cleanup actions, and state-restoration requirements. Execution must verify the approval matches the exact plan and reject stale or changed plans. Approval is recorded explicitly by the operator; no default-yes prompt, elapsed-time approval, automatic CI approval, or LLM-generated approval token.

Once approved, the scripts execute without routine LLM participation. Deterministic setup, assertions, retries, timeouts, cleanup, and restoration are implemented in code. The approved plan covers its expected fault injection and teardown; it should not demand a new prompt at every step. Any change to targets, disruption scope, artifact versions, or resource limits requires a new plan and approval.

## Failure behavior and reporting

- Use bounded retries and deadlines. An unexpected condition stops dependent work and invokes the already approved safe cleanup/restoration path; it does not ask an LLM to invent live repairs.
- Keep run state and artifacts outside the disposable cluster. Record started/completed steps durably so execution and cleanup can resume idempotently after interruption.
- Separate assertion failures, infrastructure/resource failures, test timeouts, cleanup failures, and restoration failures. Provide a nonzero exit status for an incomplete or unsuccessful run.
- Produce a human-readable summary plus JSON/JUnit-style machine-readable results, package/kernel versions, seeds, timings, relevant logs, and remaining-resource inventory. Preserve useful diagnostics even when tests fail.
- An LLM may help write the runner, review a plan, or analyze collected evidence afterward. It is not part of the test control loop and cannot approve tests on the user's behalf.

CI may build artifacts, lint the runner, and prepare test plans. Any job that runs either integration stage or live smoke stage requires a manual approval gate tied to the plan. There are no scheduled unattended runs of these suites without explicit per-run approval.
