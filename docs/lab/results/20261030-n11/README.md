# Native Stage 1 admission attempt 20261030-n11

Canonical plan: `1d1463266fa43da06062396d520caf841f6236f814592b5f5558968969b8ff22`.
The standing-authorized invocation ran 14:52:55.130–14:53:03.721 UTC and
exited 1 after 8.592 seconds. Its first host admission SSH connection timed out.
The driver correctly recorded unknown completion and blocked later actions.
No VM lifecycle, native setup, workload or guest collection was attempted.

Separate completed pinned inspections (`928fad`, `36d6dd`) found f3 reachable,
Rocky PID 3774 running, all four FreeBSD VMs stopped and no outstanding original
admission command/SSH session. A scoped local process check also found none.
The reason for the transient connection timeout remains unknown; no host power,
configuration or network change was made.

Explicit reconciliation marked admission failed, retaining the original
ambiguous reports and transport error. Cleanup (`ae38b1`, exit 1 because the
run failed) skipped inapplicable actions and closed `complete=true`,
`success=false`. The twelve possibly remaining resources are the conservative
declared inventory; native setup never created them. No native acceptance or
latency result was obtained. Existing R13 source attestations and clean binaries
may be reused only with unchanged hashes and fresh live admission checks; this
failed approval cannot authorize a changed plan by itself.
