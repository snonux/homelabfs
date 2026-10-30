# Multi-origin architecture and implementation review, 2026-10-30

Status: review and task reconciliation for `5z3`; future remediation remains
pending. Feature implementation and lab execution are frozen until `5z3` has
passed its fresh-context review. This report does not complete the implementation
project or the audit remediation cycle.

Baseline: `26a588f60da3a352f8204b913d4de576769117b0`.
Local audit start marker: `audit/2026-10-30`, not pushed. Do not move that marker
until the future remediation closure gate passes.

## Required behavior

Any admitted HomelabFS node must be able to accept normal-mode writes for any
volume regardless of preferred ownership. This includes independent FUSE daemons
on different hosts and origins outside the volume's current replica placement;
cluster admission does not bypass filesystem permissions, validated authority,
commit readiness or stale-client fencing. Do not replace this requirement with
all non-owner operations being committed only by the owner.

Each originating writer durably journals locally. The preferred owner retains
the complete ordered committed memory view and can serve it before its own disk
persistence or backing-file application. Owner-originated writes need a distinct
peer's retained memory; self-counting cannot meet the two-node normal boundary.
Receipt alone is not commitment. All origins must share coherent ordering,
conflict resolution, unknown-result handling and read visibility. The accepted
one-durable-copy plus peer-memory loss window remains explicit; there is no
silent local-only fallback. Emergency single-survivor mode remains separate.

Periodically reconsolidate preferred ownership toward the FreeBSD host receiving
the most original requests for the volume. Pod movement and non-owner access do
not depend on immediate ownership movement. Require bounded measurements,
hysteresis, minimum tenure/cooldown, fresh statistics and destination eligibility.
A cooperative handoff must transfer committed RAM state, dependencies and
in-flight/uncertain results safely. It is distinct from crash takeover, whole-VM
fencing and automatic replica-set redistribution. Preserve transaction/home
closure affinity. Numeric policy parameters remain to be specified in `kz3`.

Retain FreeBSD FUSE → bhyve/lib9p → Rocky kernel 9p. The user accepted advisory
locks within the same guest mount/kernel inode domain. Separate mounts/guests
have no shared application-lock guarantee. This limitation does not prohibit
multiple filesystem writers: HomelabFS must provide its own storage coordination
and cache coherence. The earlier blanket exclusive-client restriction was an
agent-imposed overreach and is superseded. Guest authorization, metadata, sync
and cache/fencing prerequisites remain open; no new trust boundary is approved.

## Coverage and review method

The baseline contains 1,959 tracked paths. The
[per-path coverage ledger](20261030-review-coverage.jsonl) records baseline SHA256,
lane, inspection method, disposition and evidence/reason. The
[coverage summary](20261030-review-coverage-summary.json) verifies an exact union
with no missing or duplicate paths. These hashes describe the baseline, not the
subsequent documentation edits in this review.

| Lane | Paths | Scope |
|---|---:|---|
| Authority/models | 214 | Coordination, identities, recovery, emergency, models and profiles |
| Durable storage | 105 | Journal bytes/segments, backing, redo, checkpoints, retention |
| Service/transactions | 86 | Namespace/data/permissions, homes, transactions, simulation/oracles |
| Frontends | 96 | FUSE, CLI/admin, retained9p, configuration and publication |
| Harness/operations | 91 | Native/Stage2 runners, plans, deployment, packaging, acceptance |
| Historical evidence | 1,353 | Authored reports plus archived outputs, manifests and source evidence |
| Root | 14 | Architecture, README, durability, module/license/version and diagrams/branding |

Authored source, tests, specifications and model profiles received content and
causal/contract review. Historical evidence used 75 authored full reads plus
1,278 raw artifact inspections by schema, hashes, provenance and claim scope;
this is not a claim that every raw output line received manual semantic review.
SVG XML and labels were inspected; branding PNG received metadata inspection,
not a distributed architecture validation. Dispositions are 293 retain, 149
revise, one supersede and 1,516 history. Retain means useful in its stated scope,
not proven correct for the new distributed target.

Historical integrity checks found 627/629 JSON files valid; the two empty JSON
captures belong to documented failed initial probes, not unexplained corruption.
All 55 XML parses and 134 referenced local artifact hashes passed. N18's dated
single-node native result remains 159 passes and 21 skips across 12 fixtures;
it is not evidence for multi-origin writes. Raw historical files are unchanged.

Review passes covered automated checks, reachable defects and design/conventions
with shared deduplication. Private lane reports, exact baseline inventories,
before/after task snapshots and verification receipts remain under
`/home/paul/tmp/codex/hfs-multiwriter-review-20261030`.

## Findings

Five confirmed current defects: three MEDIUM, two LOW. Seven HIGH design gaps
are grouped by underlying consequence below; representation and operational
schema compatibility are part of D1 rather than duplicate findings. Old
single-owner fixtures are scoped baselines, not newly alleged product bugs.

| ID | Category/severity | Evidence and consequence | Remedy tasks |
|---|---|---|---|
| B1 | Defect/MEDIUM | `internal/labrunner/fault_native.go:195–201` compares short targets against `os.Hostname`; prepared guests use `hfs-testN.test` (`prepare-freebsd-vms.sh:134–135`). Valid native faults safely refuse before effects. | `7z3`: exact shared trusted hostname mapping; valid/wrong/bare/foreign admission tests |
| B2 | Defect/MEDIUM | `tools/lab/smoke-probe.py:122–139`: raising SIGALRM between fork and PID registration loses child ownership; delivery between waitpid/reap and assignment can signal a released/reused PID. Existing method tests omit entry alarm. | `8z3`: short deferred-signal bookkeeping and actual boundary regressions using reviewed guest-lock child pattern |
| B3 | Defect/LOW | `internal/labrunner/plan.go:116–132`: LimitReader's synthetic EOF hides a padded tail beyond 4MiB, violating strict size/one-value contract. No typed semantic approval bypass demonstrated. | `9z3`: cap+1/real EOF, exact and over-limit public-reader cases |
| B4 | Defect/MEDIUM | `tools/poc/check-filesystem.py:120–132`, `check-io.py:71–73`: unconditional finally/rmtree removes failed DB/WAL/fixture evidence required by current runbook and AGENTS.md. | `az3`: preserve/report failed owned roots and original error; injected script failure and success-cleanup tests |
| B5 | Fixture defect/LOW | `internal/cluster/lifetime_test.go:92`: Mkdir0755 under umask077 actually creates0700; correct security acceptance falsely fails negative test. Focused 022/077 controls reproduce. | `bz3`: explicit checked chmod, both umasks and positive private control |
| D1 | Design/HIGH | `Replication-protocol.md` only allows A to originate/serve; journal roles, transport grants and operational schemas assume one writer. Relaxing a role check cannot establish common order or durable source coverage. | `cz3` normal contract; `dz3` bounded models; `ez3` versioned representation, followed by revised `4r3` |
| D2 | Design/HIGH | `internal/fsservice` reads applied backing and mutation protection waits for durable apply/export; there is no complete committed RAM logical projection or all-client cache contract. Owner RAM reads cannot be added as a bytes-only cache. | `fz3`; budget `6r3` precedes `4r3`; full platform acceptance downstream |
| D3 | Design/HIGH | Recovery/checkpoints/retention assume owner's durable prefix and fixed partner. Owner may now hold only RAM for another origin's committed writes; missing origin sources or dependency pins can lose recovery evidence. | `gz3`, revised `7r3`/`8r3`/takeover/snapshot/emergency tasks |
| D4 | Design/HIGH | Transaction engine/spec and `8w3` assume common-owner streams and owner-local execution. Multi-origin changes require preserved immutable anchors, closure visibility, homes and placement affinity. | `hz3` contract/model composition, revised `8w3` integration |
| D5 | Design/HIGH | Serial history oracle rejects overlapping per-volume calls; old simulator and rotating primaries across different volumes cannot prove concurrent same-volume behavior. Fake runner success is not real deployment evidence. | `iz3`, revised `hr3`/`br3`/`jr3`/`cw3` and isolated profiles |
| D6 | Design/HIGH | Retained9p guest group checks differ from host lib9p UID/account groups; host FUSE sees bhyve credentials. Source evidence also identifies setattr mode masking and create ownership/parent-group visibility issues. Retaining9p did not select the proposed trusted-export principal. | `jz3`: concrete 9p-compatible authorization/metadata/coherence resolution and explicit security decision where needed |
| D7 | Design/HIGH | No periodic demand policy or cooperative RAM/in-flight state handoff exists. Reusing crash takeover for preference changes can stop healthy guests; changing only owner UUID loses state or admits stale ordering. | `kz3` spec/model, `lz3` implementation and downstream acceptance |

D6 includes the nonmonotonic group-class example: host extra group membership may
select a denying group class instead of an allowing other class (mode0004), so
"host groups are a superset" is not a correctness argument. `access=client`
does not supply authentic dynamic supplemental groups; bypassing host checks
creates separate ownership/trust consequences. Setattr sticky/setID and
create/fchown/fchmod/parentGID intermediate observations need explicit handling.
These are requirements gaps in the selected path, not certified runtime outcomes.

## Reuse, revise and supersede

Retain rooted platform I/O, immutable frame identities/checksums/bounds, segment
writers, stable object IDs, local redo/checkpoint primitives, local namespace and
Unix permission logic, handle lifetimes, default-deny authenticated transport,
coordination CAS/lineage primitives, and bounded plan/evidence/restoration
controllers. Adapt their seams after choosing the new protocol; no blanket
rewrite is justified.

Revise normal authority, origin/order representation, logical visibility,
checkpoint/source coverage, transaction composition, status/frontiers and
publication/placement. Do not mandate per-origin log streams prematurely: a
canonical globally ordered stream with authorized origin evidence may reuse
existing formats, if proven. Preserve literal v1 and emergency goldens and
strict unsupported-version behavior. Early representation `ez3` precedes core
implementation; later `rr3` still owns rolling-upgrade integration.

Supersede exclusive-owner workload serving and blanket exclusive writable-client
policy. Existing diagrams and detailed owner-only protocol sections are legacy
baselines requiring revision by `cz3`; README/Architecture/ownership and dated
notices now state the new target. No raw archived result or completed local task
is rewritten to imply broader evidence. Correct dependency-notice scope during
packaging `tr3`: go-fuse is now used by the root adapter, not only the early PoC;
keep actual license texts intact and inventory current dependency pins.

## Automated verification and limits

Read-only static pass: all 321 Go files formatted, 18 Python files parsed,
nine shell scripts passed syntax checks, two SVG XML documents parsed.
Go source/mod/sum were unchanged from baseline full gates at
`f5d36fb1f1af8e7e2373afc4f64dc564107fd7e3` through audit baseline. Reused receipts
show build, vet, errcheck, lint, FreeBSD cross-build and Mage checks passed;
that full unit suite was non-race. The separate 19-test guest-lock Python batch
passed in 2.521s. Neither is distributed or live9p evidence.

A fresh `go test -race -p 1 -count=1 -timeout=90s ./...` audit attempt inherited
umask077, exposed B5, and was terminated at the outer 180-second deadline.
It is not a full-suite pass. Successful completed packages included apply,
checkpoint, filesystem service and FUSE adapter; later packages were incomplete.
Focused lifecycle test controls failed under077 and passed under022 as predicted.
A follow-up race batch for labrunner, normalnode, recovery, replication, sim,
storage, transactions, transport and vfs passed the completed packages except
`internal/sim`, which reached its 90-second package timeout. The captured active
stack was runnable seeded transaction decoding/proposal work, not evidence of
a deadlock. The documented transaction simulation race budget is 600 seconds;
the audit used a tighter profile. Full race coverage therefore remains incomplete
and must be reverified at remediation closure with appropriate bounded budgets.
Exact commands, timestamps, exit codes and stdout/stderr are in private receipts.

No tests were run by review workers. No SSH, mount, VM, installation or lab
operation was performed during this architecture review. No current infrastructure
state is inferred from historical status. No new safety model was checked and no
multi-origin runtime success is claimed.

## Priorities and task ordering

1. Finish review/task reconciliation `5z3`, then specify all-origin contract `cz3`
   and prove focused bounded ordinary behavior `dz3` before changing wire bytes.
2. Establish versioned origin/order representation `ez3`, bounded logical owner
   memory views `fz3` and early resource budgets `6r3` before core replication.
3. Specify complete recovery/retention `gz3` and transaction composition `hz3`;
   preserve acknowledged dependencies, stable retries and explicit loss handling.
4. Resolve retained9p authorization/coherence `jz3` and concurrent external oracle
   `iz3`. Keep pre-replication single-node gate `zq3` separate from later full
   multi-host deployment acceptance; never create a `4r3`↔`zq3` cycle.
5. Specify/model periodic preference handoff `kz3`, implement `lz3` after actual
   replication/recovery, then gather native and isolated guest acceptance.

The [all-36 task mapping](20261030-task-reconciliation.md) records each retained
scope, added prerequisites and the removed late-budget edge. Five bug tasks and
ten focused design/model/implementation tasks are new. All remain blocked by
`5z3` until review completion. Remediation closure `mz3` and the final marker `nz3` are
separate future tasks; closure checks must include every finding task, final
build/vet/race/format/linters and the actual acceptance appropriate to the changed
scope. No final audit tag or push is authorized by this report. The overall
assessment is useful reusable local foundations with substantial unimplemented
multi-origin protocol, logical-view and deployment work; implementation must
follow the corrected requirements rather than relaxing tests around the old model.
