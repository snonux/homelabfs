# l04 implementation handoff — source frozen

Worktree: `/home/paul/tmp/codex/hfs-l04-20261103-pe5jz8w8/worktree`
Branch: `codex/capability-reclaim-l04-20261103`, base `6f9b2b3`.
No git/task mutations or nested agents. Root owns fresh review, full suite, commit and task completion.
Exact 25-path freeze with hashes: `source-freeze.json`. No further worker edits planned.

## Behavior

- Live capability admission counts only unreleased authority. Every issuance row remains retained/nonreused under a distinct finite `MaxCapabilityIdentities` ceiling (default65536), including tombstones. ACK/claim retirement never deletes grant obligations.
- Each PATH token owns exactly one reference. FORGET/count1 verifies the exact scoped live grant, bypasses removed ancestor traversal for authority reduction, closes only that grant and its optional own lookup pin. READDIR PATH grants have no filesystem lookup pin.
- Typed optional `TransientForgetCaptured` + controlled `PlanTransientForget` preserve the exact Binding, persisted plan/result/snapshot and original recovery driver for grant-only closure. There is no public fsapi/native-command flag. Normal execution remains gated and refuses the unsupported seam.
- Protected private snapshots and exact local ledger scope carry capability-lifetime schema2; old unclassified snapshots/ledgers are refused. Exact scope byte comparison also prevents old readers opening new ledgers. No migration/deletion/reset authority is introduced.
- After process exit, only a still-live exact original grant plus the retained exact canonical original credentials/hash/revision permits FORGET/final DESCRIPTION_RELEASE cleanup. Ordinary requests, changed credentials, forged/unknown/released tokens remain rejected; exited markers persist and original terminal claims replay.
- Guest result adoption includes READDIR entry capabilities. After leaving physical observation, actual production consumer closes every transient entry using original issuer, including entries not emitted because the buffer/cursor fills. Failure blocks with original request evidence unadopted.
- Inode reuse keeps a distinct forget record for each issued path token instead of aggregating counts under an older token. Inode eviction queues each separately; uncertain worker results retain exact records. Existing description/view/dirty/open-unlinked/source lifetimes remain separately pinned and gated.

## Verification receipts

- `targeted-race.log`: affected guestbridge PASS3.468s, controlledvalidation PASS72.015s, fsservice PASS51.551s; viewcontrol initial failure was the existing 128-clean pressure test's3s per-response timeout. All failure logs preserved.
- `clean128-rerun.log`: exact initial test repeated3s timeout; `pressure-diagnostic1/` has approved diagnostic-only Go overlay, worker trace, goroutine stacks and preserved fixtures. Repeated WouldBlock retries occurred while the explicit primitive enrollment verifier serialized the acquisition/control lanes; no terminal refusal, poisoned controller or capability/snapshot-space refusal was observed.
- Scoped configurable response wait retains default3s elsewhere; the one slow128-pressure component test uses20s, with overall240s bound. Actual corrected exact race PASS116.319s, receipt `clean128-corrected-receipt.json`. A failed scripted setter attempt still used3s and is honestly preserved as `clean128-deadline-unset.log`; it was not corrected validation. No product deadline/native latency acceptance changed.
- `final-checks.json`: final focused race PASS (controlledvalidation1.839s/viewcontrol5.189s), affected4-package build/vet/errcheck/staticcheck/golangci allPASS. Focused cases include exact exited cleanup negatives/replay, pending transient plan/effect ledger close+reopen and exact result/tombstone, unclassified-ledger refusal, dirty release/process-exit + originalissuer cleanup after drain, actual framed READDIR/FORGET/duplicate/replay and separate lookup pin preservation.
- Actual Service+journal tests use live limit5 and12 sequential scan/forget/lookup/open/close/release cycles, simultaneously held-cap refusal, identity ceiling8 exhaustion despite releases, stale caps, invalid aggregate counts, original result replay. `unlinked-race.log` verifies forget after unlink preserves the separate open description until final release. Fixture claim/source credits are lowered only in the12-cycle test; production budgets/source ceilings and the4MiB canary remain unchanged.
- `reclaim-final1/`: ASan/UBSan actual production C decoder/transient FORGET consumer, adoption predicate, inode attach/eviction and forget worker consume two distinct actual Service/local-journal-issued same-object tokens, checking full matching cap JSON/count1/original issuer and failed transport/allocation retention/no adoption. Primitive inode/workqueue/transport fixtures are explicit.
- `requests-final1/`: ASan/UBSan production result-owner/request retirement, concurrent held result, dirty drain, wrong floor/gap/denied proof PASS. `root-final1/`: actual host ROOT decoder18 positive/negative cases PASS.
- Exported via owned exporter with `--task l04 --development`; pinned archive SHA verified. `prepare.py --verify` passed the exact28 maintained-file subset (not a full kernel source tree).
- `objects1/receipt.json`: allocated serial actual `-c` compile of hfs_session/hfs_inode/hfs_file PASS, source/owned headers + pinned recorded compiler flags + read-only generated/stock header fallback, exact compiler/command/dependency/object hashes. Peak sampled process-group RSS120/110/65MB. All28 maintained bytes and generated config/header/reference metadata unchanged before/after. No Kbuild, link, ABI or installed-kernel proof.
- `git diff --check` and Python AST parsing of exporter/new runners PASS. Full repository suite intentionally left to root once after source freeze.

## Resource/safety and limits

`footprint.json` includes taskroot31,223,808B + eight preserved diagnostic `/tmp/hvc-*` roots11,022,336B + eighteen earlier preserved failed controlled-test roots6,938,624B =49,184,768B aggregate allocated footprint. No old/historical/failed resources deleted or modified. Exact pinned previous reference trees remain untouched.

The attempted physical-fsync injection through a wrapper was correctly refused by ControlledJournalInfo's exact `*vfs.OS` gate. That experiment was removed; no gate weakening or physical disk-fsync acceptance is claimed. Failed C transport/flush-boundary and protected driver failpoint cases are component evidence only.

No SSH, VM lifecycle, mount, install, lab/prod/PV/NFS or received-sink action occurred. No actual FUSE endpoint, running guest/kernel/syscall, physical PTE/TLB/source closure, distributed filesystem or normal publication acceptance is claimed. Existing4MiB and any-node write/origin durability + owner RAM contracts remain unchanged.

## Self-review

Asked: did the changes make sense, and is there a better simpler way? The implementation retains one issuance ledger and computes live count instead of adding a recovery-sensitive counter; a separate identity ceiling keeps tombstones finite. The typed captured preparation seam avoids a permissive public/native bool. Each kernel token retains its own record, without widening generic upstream/MM paths. Narrow post-exit cleanup reduces authority and never revives ordinary access. Recovery tests were strengthened to close/reopen while pending and compare exact original proposed bytes; failed plans cannot publish a predicted released snapshot. Concrete issues from this review were addressed before handoff.
