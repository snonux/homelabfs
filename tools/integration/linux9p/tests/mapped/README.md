# Portable mapped-receipt integration

Run `run.py --source OWNED_SOURCE --output NEW_OUTPUT --budget-root OWNED_ROOT`.
The source must match the maintained `source.json`; prepare/export it through the
existing integration tools. Optional `--race` applies only to the focused Go
integration. Optional `--kernel-headers GENERATED_OBJECT_ROOT` compiles the three
changed owned kernel translation units against read-only historical headers;
it never modifies that root or builds/installs a kernel.

The runner owns and counts its short temporary root, requires stable swapout for
three seconds before each stage, and monitors the memory/swap floor,2GiB group
RSS,8GiB owned disk and20minute stage bounds. It retains outputs and failures.
A sanitizer consumer build and one same-package Go overlay test run sequentially.
The build retains the established kernel userspace shim's `u64` spelling and
`-Wno-sign-compare`; all other selected strict warnings remain errors.

The consumer uses production `_send`, `_fetch`, WAIT translation and the real
local lib9p ledger. It substitutes native fd observations and deterministic
scheduling only. Its extracted guest command builder is byte-compared with Go
canonical commands across all five minors, six operation classes and both Ack
forms. The actual guest protocol C primitive verifies real Producer signatures
using OpenSSL, with a per-verification allocation ceiling of16KiB. Whole-body
parsing is caller-owned and occurs before that measurement.

The Go overlay reuses the real `sourceRig` with two private providers, its
concrete router, protected Transport, controller, source journal and Producer.
The source rig's mount/INIT and guest credential/kernel observations remain
explicit portable primitives. No issuer or source/signing callback is replaced.
The test also signs deliberately malformed policy fixtures with the actual host
key to distinguish shape/policy rejection from signature rejection.

No test installs a provider, mints KernelReceived, independently verifies source
bytes, proves physical facts, restores a runtime or accesses infrastructure.
Missing kernel Ed25519/provider integration remains a C6 refusal boundary.
