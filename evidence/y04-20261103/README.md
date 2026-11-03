Task y04: actual carrier and inert guest attestation verification

Eleven distinct final root checks passed: two sanitizer C consumer builds and all nine Go checks, with both actual C2 and mapped templates temporarily materialized, then byte-verified and removed. Full local Go suite passed195.903s with -p1/GOMEMLIMIT256MiB/GOGC50; focused race40.283s passed earlier. Build/vet/errcheck/lint, FreeBSD build and two actual CGO-enabled FreeBSD test-binary compilations passed. Source hashes remained unchanged in the final validation window.

Initial staticcheck requested an equivalent test-only tagged switch; root made that change and reran all Go checks. Two full-suite attempts were stopped solely by swapout guards, first1254184->1254188, then1254188->1254220; partial results are preserved and are not full passes. No memory.high/max limit or OOM event was reported by the observed cgroups. Cause of swapout is unproven. The serialized lower-heap run kept1254295 stable throughout; disk guards remained below8GiB. No OS tuning or user process was changed.

Fresh independent R1 found no blockers; no R2. Nonnull original Ack across WAIT continuations is code-inspected, while null Ack multipoll and separate donor/release are directly covered. C nested SOURCE policy is narrower than Go; C6 must complete acquire/renew and FROZEN schema/request checks. These limits are documented and annotated on s04. Near-bound carrier probes call actual _native_body directly rather than dispatching the whole multichunk command.

Worker evidence includes strict backend ASan/UBSan, actual two-session C carrier/real protected host/OpenSSL integration under race with signed multi-leg EVENT and SOURCE/FLOOR,60 actual guest-builder comparisons, streaming-hash allocation bounds, three actual Linux kernel translation units and changed FreeBSD C source compilation. These are component and compile-only results. Earlier fixture/launcher failures remain in the preserved scratch root; the successful physical errcheck materialization has an exact canonical-template hash.

No production kernel Ed25519 adapter, private received-response provider, source-byte/physical validation, C6 readiness, restoration or deployment is claimed. The pure result is an inert host attestation. No SSH, mounts, VM, installation, lab, production/PV/NFS or received-sink operation occurred. Absolute scratch paths in the runners identify preserved evidence, not installed artifacts.

The failed errcheck CLI-help log expands tabs for the whitespace gate; curated-log.json records both hashes and its preserved original path.
