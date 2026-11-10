# Private controlled validation provider

This package implements the local provider needed before the retained-9p lab
base gate. It executes the actual `fsservice` Unix planner, local journal,
application, read and lifetime paths. It does not implement a distributed
filesystem, normal publication, kernel cache barriers, installation or a mount.
It has no dependency on replication or public transaction integration.

Activation requires all of the following component inputs:

* A running, explicitly named disposable `fsservice.Service`, configured with
  the real Unix permission policy and without reclamation authority.
* An out-of-band pinned operator Ed25519 key, with operator, host and guest
  signatures over the exact enrollment. Enrollment binds the profile, complete
  bridge context, endpoint node identities, serving generation, nonreused private
  ledger UUID, actual owner stream and admitted source filesystem identity.
* A live `Facts` implementation that independently authenticates installed
  component enrollment and guest provenance. Signatures do not prove kernel or
  installation state. There is no permissive implementation or default.
* The bridge's separate actual native verifier, credential attestor and view
  provider. This package supplies Claims, Execution, private result finalization
  and restoration; it supplies neither a native process verifier nor cache hooks.

Both Claims and Execution verify the bridge's exact enrolled profile and endpoint
at construction. The guest must independently enforce its signed profile. Every
successful controlled result has `publication:null`. Normal v1 and normal3
schemas, grants and verification remain unchanged. This does not open a CLI,
mount, normal commit path, guest fallback or base-gate acceptance.

The caller provides a private, owner-controlled absolute directory. Creating its
ledger requires `Initialize:true`, an empty directory and an unused enrolled
ledger UUID. Ordinary reopening refuses a missing ledger. Existing files must be
regular, single-link, owner-only files. bbolt provides a process-exclusive OS lock
and synchronous transactions; directory creation is synchronized before success.
Administrative exclusion of source and directory ancestors remains mandatory.

The trusted local `Allocate` entry point protects each next number before it can
be sent. Remote CALL is not an allocator. `Reserve` atomically protects original
context, credential and intent bytes, operation UUID, creation allocation, and
bounded retained-result/source accounting credits. Defaults retain at most 4096
claims and reserve 4 MiB of retained state plus 2 MiB of source accounting per
admitted claim, within 256/128 MiB budgets. Actual encoded plan, exact wire result,
capability snapshot and planned outcome must fit those credits before effects.
These are logical accounting budgets; storage I/O errors remain uncertainty and
never become proof of absence. Every CALL, reads included, holds one claim and
both credits until it is retired. A successful retirement recycles every number
at or below the new floor (task o64, `recycle.go`): its entry and earmark are
deleted and their credits return, so `MaxClaims` and the two credit budgets
bound the unretired window, not the session's lifetime. Before o64 they were
lifetime budgets: the zq3 spec's 16 MiB / 1 MiB source credit refused the 17th
CALL of a domain. The session's protected bridge snapshot owner is kept until a
later claim owns the snapshot. A recycled number still answers `RETIRED_ID`
(Resolve, Cancel), its exact retirement receipt (Retire, and
`VerifyRetirementReceipt` against that receipt) and its READDIR cleanup receipt
(kept in `readdir-receipts`); `ResolveGap` refuses it as Stale. The same
retirement also prunes the bridge snapshot's grant tombstones that retired
claims released (`guestbridge.PruneRetiredGrants`). The `processes` bucket of
lock-owner tokens stays a lifetime count bounded by `MaxClaims`.

The sealed service worker serializes capture and append with every other local
origin. WRITE resolves the actual append offset before selecting its `max_end`
prefix. Before filesystem effects, a separate versioned private ledger protects
the complete typed redo, original semantic outcome, exact randomly issued wire
capabilities and controller/process/description snapshot. After journal protection
and application, a transaction marks those same wire bytes terminal before reply.
The private ledger does not add record kinds to either closed journal format.

Recovery uses the original operation UUID and compares its complete typed
mutation/COMMIT evidence. A missing original mutation keeps the claim uncertain;
it does not cause a new lookup or replanning. Creation/open pins are preplanned
and recovered under their original identities. Within one serving generation,
lifetime recovery additionally requires the service's exact operation-completion
evidence; an unexecuted OPEN/LOOKUP or release/forget/close/exit remains uncertain.
Recovery never fills a predicted handle slot or overwrites an intervening native
OPEN. After a service restart, unproven nonmutating lifetime effects stay uncertain.
Proven effects finish with their original protected result and capability bytes.
Prediction uses a private session clone; credits and the plan are protected before
filesystem/lifetime effects, and grants/process state install after protected
completion. Raw ancestor names retain their original bytes in private snapshots.
Old descriptions are excluded when the serving generation changes. Snapshot
versions prevent older recovery from rolling back newer grant/lifetime state.
A durable unresolved staged PENDING operation blocks new Reserve in that session;
new allocated numbers stay unadmitted and retained for authenticated no-effect
resolution. Original queries and recovery remain available. The bridge serializes
private reserve/planning with recovery and reloads the latest protected terminal
snapshot before preparing another, including after a failed live installation.
Pre-plan RESERVED entries without effects remain eligible for no-effect resolution.

Unsent and rejected allocations remain retained, unresolved allocations until
authenticated protected no-effect resolution. `ResolveGap` can protect no-effect
only before any plan/effect was admitted;
CANCEL cannot turn pending work into absence. Retirement verifies exact complete
result/gap coverage for every number and atomically protects its permanent floor
and an idempotent retirement receipt. No timeout, TTL, missing map entry or
counter decrement retires anything. All retained identities are bounded.

`RESUME` remains BLOCKED: this package does not reconstruct native cell mappings,
response snapshots, kernel domains or complete endpoint controller state. A new
Go object cannot prove a new endpoint lifetime. Actual supervised process,
guest-kernel, cache and installation facts are still required from the respective
platform components before any real guest/kernel acceptance.

Tests use real private OS roots, bbolt files and local journal segments, including
filesystem-effect-before-terminal failpoints, full service/provider restart,
original wire capability recovery, duplicate races, append limits, namespace and
metadata operations, descriptions, gaps, retirement, ledger exclusion and signed
enrollment negatives. No mount, VM, SSH or production data is involved.
