# Retained-9p preparation verification, 2026-11-05

The external staged signer and bounded guest probe/controller helpers passed
focused Go build/tests/vet/errcheck/format checks and Python/shell checks.
The final Python suite has 16 tests, including actual pipe backpressure,
plan replacement, pinned input replacement, evidence durability failures and
OpenSSH path-expansion refusal. Tests use throwaway signing keys/local children.

[evidence.json](evidence.json) records source bindings, all retained check
results, both review rounds and the remaining acceptance gates. R1 found three
controller blockers; R2 confirmed those fixes and found an OpenSSH path expansion
issue. Root fixed that issue and verified the final 16 tests, without a third
review. The original failed fd-cleanup batch remains preserved and is not passed.

This is preparation only. No SSH, mount, VM lifecycle, installation or actual
operator-private-key use occurred in these checks. The old candidate remains
quarantined; a different digest is not eligibility. Host journal/source and
PTE/recall correlation, C9, pod compatibility, K04 and the original base gate
remain required. Publication is null and zq3 is incomplete.
