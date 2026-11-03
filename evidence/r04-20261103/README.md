# r04 controlled live facts and receipts verification

Fresh full R1 found no blocking findings. The final nine local checks passed on the integrated source: build, vet, errcheck, lint, FreeBSD non-cgo cross-build, focused race tests, full local suite, and compile-only FreeBSD/amd64 cgo test binaries for controlledruntime and fuseadapter. Build settings confirm cgo enabled; binaries are retained outside Git and were not run.

The source binding covers 458 Go/module/embedded-probe inputs and remained unchanged. Full suite took 101.090 seconds. Focused race took 21.089 seconds. Native FUSE tests were disabled. Tests use real local service/provider/controller/journal/process/receipt behavior with explicitly substituted native mount/header, guest credential and SSH/sysfs boundaries. No SSH, mounts, VMs, installation or lab operations occurred.

This establishes controlled component behavior. Actual kernel observations remain s04, reopened-source reconstruction v04, multi-provider runtime composition w04 and installed/native acceptance xz3/zq3. There is no distributed or normal publication acceptance claim. Root resource samples are sparse observations, not a continuous peak-memory measurement.
