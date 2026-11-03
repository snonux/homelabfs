# u04 C8 handoff

Frozen source on root-provided base 3ecab6e. Root owns all Git/task mutations; none performed here. Four in-scope paths are listed and hashed in source-freeze.json. No unrelated edits.

Implemented explicit NewDurableTransport and actual synchronous private bbolt delta protection. Index stores exact controller/endpoint/scopes/selected protocol tuple/high/counters/live ID complement/workspace; cells own original accepted chunks and historical native observations, command/native/job phase, immutable response/wire/read coverage. Write protection precedes queue/ACK; executing protection precedes Execute; response/read/reclaim protection precedes exposure/release. Protection uncertainty fences all further traffic. Exclusive private root/DB ownership, fd-pinned nofollow owner/nlink/mode checks and bounded inert streaming inspector.

Exact remaining boundary: no restoration, requeue, reprepare, controller handoff or RESUME. Historical Native never constructs native observation authority. C9 must reconcile semantic effects and every other ledger before any restoration. Portable fixtures do not prove native physical behavior.

Bounds: <=80 live rows + one workspace, <=8MiB encoded cell and128KiB index; <=16MiB cell puts per transaction; bbolt AllocSize1MiB; actual-FD pretransaction768MiB operating ceiling, not a proven hard physical maximum. Inspect rejects oversized files before mapping. Durable constructor requires separate1GiB Memory ledger reservation in addition to unchanged512MiB ordinary+128MiB control+12MiB workspace accounting; unfunded construction refuses before root creation. Capacity is reserved, not eagerly allocated. Futurew04/xz3 must rebudget full unchanged4MiB canary/guest limits or refuse.

Deliberate behavior corrections: malformed replacement workspaces retain already acknowledged partial chunks instead of discarding them; only one existing workspace remains funded. A full command that encountered full dispatch queue may retry original bytes instead of receiving a false accepted duplicate without dispatch. No cells or IDs are recycled.

Self-review: asked whether all changes make sense and whether a simpler model exists. Single bbolt transaction/row delta and per-cell durable job owner avoid fullstate/history copies and separate recovery scheduling. Tightened exact command/wire/read/native/protocol validation, refused oversized DB before inspection mmap, removed borrowed frameData slice, and made private postcommitfailpoint errors consistently refuse Read/Write while retaining committed bytes. No known blocking defect after self-review.

Verification actually run: preflight go test -p1 ./internal/guestbridge -run ^$ PASS. Complete guestbridge package PASS0.612s before final narrow validation/comment/newCellData refinements. Final focused TestDurableTransport PASS; focused race PASS1.965s package time; go vet/staticcheck/errcheck package checks PASS; gofmt and git diff --check clean. Full repository suite intentionally left to root after merge. Tests use actual files and subprocess os.Exit without Close at chunk/workspace/response/read/reclaim/executing-job boundaries; negatives include commit uncertainty/read refusal, corruption/ownership/secondwriter, limits/unfundedbudget and response/job bindings. Large1MiB row repeatedly rewritten verifies page reuse rather than append history.

Checks.py logs and metrics.json retain exact final commands/results. Final sampled total descendant peak RSS157843456bytes, memory floor>=8GiB and25%, swap<=55%, no pswpout change1218271 throughout. GOMAXPROCS2/GOMEMLIMIT1GiB/go -p1, stages<=20min. Task root28MiB, no background test process remains. Heavy slot RELEASED. No SSH/VM/mount/install/lab/production/PV/NFS/sink action, no upstream edits or dependency changes, no nested agents.

## Root self-review namespace correction (before R1)

Root found initial-root namespace durability missing: syncing transport.db and
its new root directory did not sync the naming parent. Fixed by pinning the
parent directory fd, creating/opening the child with Mkdirat/Openat and retaining
the parent fd until after the initial typed index commit. The constructor calls
actual parent.Sync before workers or success; failure retains files and the
service attachment. No callback asserts successful namespace protection. Tests
rename/replace the parent path and verify the original descriptor stays pinned;
a closed actual parent fd makes finishNamespace fail with os.ErrClosed while
the committed original ledger remains inspectable and cannot be freshly reused.

Focused C8 tests, focused race, vet/staticcheck/errcheck PASS after this correction,
with no full-suite repeat. Current metrics.json/logs cover corrected frozen bytes;
prior logs/metrics/manifest are retained with pre-parentfix prefixes. Final sampled
peak149368832bytes, owned root28MiB and pswpout unchanged1218271. Source hashes
refreshed only after checks and second self-review. Heavy slot RELEASED; R1 unused.

## Root final-gate process-loss oracle correction (after clean R1)

Root's focused final race exposed a kernel lock-release handoff after an actual
child had exited73: bbolt's first database flock returned EWOULDBLOCK even though
the independent owner.lock had released. Existing bbolt Timeout1ms does no retry
with its50ms retry interval. Unchanged-product race repeat100 reproduced3failures
in23.445s. Actual fd probes on failed inode75263429/dev54 remained EWOULDBLOCK
at0us/1.13ms/2.24ms and succeeded3.36ms. No byte corruption was retried. Full
/proc/locks snapshots and probe timings remain in lock-repro.log;3failed roots
remain under failed-lock-roots (144KiB). The OS deferred file/mapping release
mechanism is an inference; actual transient lock contention/release was measured.

The approved correction changes only transport_ledger_test.go. After the exact
child exits73, the oracle waits at most1second for actual nonblocking ownerEX and
databaseSH flocks, retrying ONLY EWOULDBLOCK/EAGAIN. Other errors/persistent
locks fail and preserve the crash fixture. No elapsed time grants authority, no
Inspect/corruption error is retried, and product Inspect/Timeout stays one-shot
and unchanged. Actual held-owner/held-database negatives plus missing-file error
prove fail-closed behavior. Failed process fixtures now retain their MkdirTemp
root and log its path instead of automatic deletion; successful fixtures clean.

All focused tests/race/vet/staticcheck/errcheck PASS; repeated100 process-loss
race PASS28.813s after correction (600 actual crashed child stages), sampled
peak93073408B for repeat; all focused stages max159297536B. Logs/metrics retain
old/pre-lockfix results separately; final metrics.json plus repeat-metrics.json
cover updated test bytes. No ongoing swapout (still1218271), owned28MiB. The3
product/document hashes remain identical to clean R1 and only testhash changes.
Root owns final full gates/commit/done; no second formal review required after
clean R1. Heavy slot RELEASED, sources re-frozen after self-verification.
