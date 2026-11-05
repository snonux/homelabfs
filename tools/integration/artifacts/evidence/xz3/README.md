# xz3: controlled validation build artifacts

**Not deployment eligible: xz3 reopened during zq3 preparation.** The selected
CGO-disabled FreeBSD daemon uses an unsupported process observer. Its launch
path starts the child before the observation fails, so this candidate cannot
reach native READY. Tasks r24/s24 and the reopened artifact task must supply
a qualified native observer, prelaunch refusal and corrected artifacts. No VM
was started and nothing was installed. The build and offline checks below
retain their narrower historical scope; they did not qualify this capability.

Source commit: `13fbad97dad85ce3145f22c92892aefa132ee1e9`.
Final candidate manifest: `7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b`.
Candidate: `/home/paul/tmp/codex/hfs-xz3-20261104-4sgkdoky/package-final`.

The complete custom Linux kernel, boot image and configured modules reproduce
byte-for-byte across two clean builds, with actual module CRC/vermagic checks.
The full native FreeBSD exporter/libraries reproduce all83 retained files;
the actual FreeBSD Go runtime also reproduces after forced rebuilds. Complete
selected native source and notices are included; installed binary sysroot
libraries remain external hashed inputs. The original FreeBSD archive capture
was explicitly TOFU, with its observed hash reviewed before compilation.

The final195-file candidate passes complete component admission and14 real-copy
negative controls, with original and restored copies passing afterward. R1 was
clean; no second round was needed. Merged build, vet, errcheck, lint, full Go
suite and FreeBSD cross-build passed. One cross-build attempt hit the disk guard;
only the dedicated rebuildable cache was reclaimed before the successful retry.
All failed resources and receipts remain. [Evidence](evidence.json) records exact
source, compiler, config, binary, proof and check hashes and operational limits.

This is build and offline verification evidence. No kernel was installed or
booted, no exporter/VM workload ran, and no PTE/TLB, native canary, replication,
failover or normal-publication acceptance is claimed. The installation proposal
is inert; actual installation and disruptive windows require a separate exact
reviewed approval. The4MiB canary and signed2GiB guest envelope remain; native
FreeBSD guest allocations are unchanged. C9 stays limited to the same surviving
owner/current cut; replacement FUSE/exporter and guest RESUME remain blocked.
The repository revision in the package is the actual Go builder's baseline;
maintained changes and current recipes have separate retained hashes.
