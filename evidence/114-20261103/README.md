Task 114: authenticated host mapping foundation

Ten distinct merged checks passed: the unchanged sanitizer C consumer preflight, then all nine Go checks on the final source. Full local suite:111.392s; focused race:42.011s. Build/vet/errcheck/lint, FreeBSD build and both actual CGO-enabled FreeBSD test-binary compilations passed. Binaries were inspected, never executed on FreeBSD. The canonical existing C2 consumer test was temporarily materialized for all Go checks and hash-verified before removal. Final source hashes remained unchanged.

R1 found one attachment-failure arena leak; the narrow fix preserves the service/ledger claim while returning unopened reservations. R2 confirmed the real exact-budget reuse regression and found no new blocker. A nonblocking test comment was corrected to describe private-ingress refusal. Root then applied the equivalent De Morgan expression requested by staticcheck; all nine Go checks were rerun. The initial static failure and correction are preserved. No third formal review occurred.

Concrete runtime tests cover two scopes with real SOURCE/FLOOR receipts, single-leg WAIT-to-POLL EVENT, and nonempty parent-funded cleanup. Transport tests cover16 nonadjacent actual EMPTY legs, hashes/reclaim/closure, replay after actual donor deletion, partial/out-of-order requests, bounded marker saturation and protection failures. Signed multipoll terminal EVENT is compositional coverage, not one integrated fixture. Broad preexisting cleanup race was intentionally stopped at344.566s during known slow setup; its complete pass is not claimed.

Native/kernel/credential primitives remain explicit test boundaries. Pure V2 verification yields an inert host attestation. Actual C1.4 carrier/guest integration(Y04), kernel receipt/source authority(C6), complete restoration and deployment remain unimplemented. No SSH, mounts, VM, installation, lab or production operations occurred.

Recorded guards show each final stage stable at pswpout1229428, sampled process groups below2GiB and allocated owned disk below8GiB; background counters advanced between earlier batches. Evidence uses preserved absolute scratch paths. The worker coverage run preceded only the narrow constructor fixes and equivalent lint expression.

The raw R1 unified diff is retained byte-exact, including context prefixes before Go tabs. Only that raw patch is excluded from the whitespace gate; its hash is recorded in raw-patch.json.
