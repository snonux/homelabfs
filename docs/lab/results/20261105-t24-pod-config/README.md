# Selected pod-kernel configuration, 2026-11-05

All 14 focused artifact tests passed, including mandatory feature refusals and
resolved-config duplicate/malformed controls. The actual owned configure recipe
also passed: all 29 required profile features resolved to `y`. UTS namespaces
and PID cgroup limits become enabled; IPC namespaces are now explicit.
Overlay, veth and bridge remain disabled for the documented VFS/private-loopback
namespace fixture; this does not claim those optional features work.

[evidence.json](evidence.json) records actual source, tools, configuration,
features and all check receipts. The first old-source attempt correctly refused
a stale patch identity before creating configure outputs. That failed source
was preserved. A fresh current tree from the already pinned archive passed its
78,739-file owned-source verification, then allnoconfig/olddefconfig and admission.

This proves configuration resolution only: no full kernel/module build, boot,
pod, installation, mount or VM operation occurred. The operator public value was
synthetic test input. The final artifact must use fresh build/config outputs with
the real selected operator public key; only the verified source tree is reusable.
Actual runtime/image pinning and all original namespace/pod/SELinux/9p acceptance
remain required in zq3. Publication stays null.

Fresh R1 found no blocking or nonblocking issue. The merged source `2d944b3`
passed all artifact and preparation Python tests, shell checks and helper
format checks; see [merged checks](merged-checks.json).
