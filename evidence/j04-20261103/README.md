# j04 verification, 2026-11-03

Exact negotiated bridge 1.1 codec and dispatch foundation only. No durable view
controller, installation, mounts, VM operations or runtime serving acceptance.

Targeted Go tests, vet/errcheck, independent spec checker and the complete local
ASan/UBSan lib9p component suite passed. C checks ran on Linux; the FreeBSD
cross-build below covers Go, not a native bhyve build.

Fresh R1 found one blocking test gap: the new dispatch path was unreachable in
tests because all request providers refused. The test-only fix exercises real
Service.Execute with issued ROOT/CREATE capabilities, successful acquisition and
reservation, and independent authorization, attestation, capability, protected
response and target-source refusals. Fresh narrow R2 was clean. All 24 pre-fix
source hashes remained unchanged; only view_dispatch_test.go was added.

targeted/r1-fix contains the authoritative cache-free source receipt and checks.
Earlier targeted/source-sha256.json preserves historical preparation, including
non-source generated Python cache entries; those cache hashes are not source
binding. The actual source files were unchanged between those component checks.

root-full records all six final checks: build, vet, errcheck, lint, FreeBSD Go
cross-build and full non-race root suite. All exited zero; source-changes.json is
empty. Targeted race tests passed separately; no full-root race claim is made.
Temporary binaries and ledgers remain outside git in the preserved run root.
