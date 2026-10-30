# Native Stage 1 admission attempt 20261030-n14

Canonical plan: `da066e0fc5b4cb946aa9eba09ca01fd53436c82b055c71a32e2695217b279c1c`.
The standing-authorized invocation (`72065`, terminal `8a3311`) ran
15:55:29.376–15:55:36.649 UTC and exited 1 after 7.273 seconds.
The initial host SSH connection timed out before any VM lifecycle, native
setup or workload step. No native behavior call ran; this does not verify
the process-exit fix or unprivileged append performance.

The original admission outcome was uncertain and blocked dependent actions.
Fresh pinned host inventory/process inspection (`304633`, exit 0 at 15:56:28 UTC)
confirmed Rocky still running as PID 37004, all four FreeBSD VMs stopped and
no outstanding prior SSH/preflight/native command. Scoped local process inspection
also found no outstanding action. Explicit reconciliation records admission as
failed, preserving the original ambiguous reports and timeout log.
Cleanup (`a084d8`) closed `complete=true`, `success=false`: inapplicable cleanup,
shutdown and restoration actions were skipped; other setup/test/collection steps
were never attempted. The overall exit remains 1. All thirty original artifact
bytes are retained privately with mode 0600 before preparation of another plan.

The connection-timeout cause remains unknown. No host power, configuration,
package, production or replication change occurred. R15 image metadata and
c946ac5 binaries remain eligible for honest reuse because no image or source
code change occurred; a new run still needs a fresh admission survey and plan.
