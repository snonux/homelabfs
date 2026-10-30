# Native Stage 1 attempt: 20261030-n3

Paul instructed continuation of the presented corrected plan and authorized
powering f3 back on if needed. Canonical plan:
`344023e57345562905ba8aef837f89763afe9ffabbd8297b24119191b49573b3`.

f3 was unreachable with rack fans off and AC on. The [action record](wake-actions.json)
documents enabling cooling and sending one f3-only Wake-on-LAN packet. A later
remote wake request was declined because f3 was already up. Pinned SSH then
showed a new host boot, Rocky PID 6440 running, all four FreeBSD VMs stopped and
zero used swap. No AC power cycle or forced VM action was performed.

The approved runner failed read-only admission: the old stopped-image snapshot
no longer matched the backing dataset's zero-written requirement after the host
restart. No runner lifecycle, native setup or workload step ran. Collection and
inapplicable teardown were skipped; the run closed `complete=true`,
`success=false`, exit 1.

- [Admission log](prepared-preflight.log), [report](report.json) and
  [JUnit report](report.xml) retain the actual failure.
- [Post-boot inventory](post-wake-state.json) projects the subsequent captured
  host/source inventory used for follow-up `dx3`.

Task `dx3` refreshes the four image attestations from an existing post-boot
snapshot. The zero-written check was not waived, and no native acceptance
result is claimed.
