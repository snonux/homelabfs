# Host SSH investigation, 2026-10-30 (uy3)

**The repeated connection/banner failures remain unexplained. No controller
fix, host configuration change, or native acceptance result is claimed.**
The read-only diagnostics below did not reproduce the failure. Task `uy3`
remains open; successful observations do not resolve the earlier failed runs.

## Failure and scope

[N15's original readiness log](../20261030-n15/native-readiness.log) records
SSH exit 255 and a banner-exchange timeout on its first host `vm list`, after
normal lab startup. The runner preserved uncertainty and required observation
before cleanup. Normal lab shutdown and Rocky restoration subsequently passed;
no native fixture/setup ran. N11 and N14 also had initial connection timeouts.
These distinct failure stages need not share one cause.

ROOT performed read-only host inspections and no-command SSH observations.
No VM power operation, workload, production change, SSH/PF/network configuration
change, or retry of an uncertain remote command was part of this investigation.
The existing command timeouts, attempt limits, pinning and uncertainty behavior
remain unchanged.

## Observations

The [effective host fields](uy3-host-ssh-diagnostics-r2.stdout), confirmed for
user `paul` and source `192.168.1.246` in the [source-specific inspection](uy3-host-ssh-diagnostics-r3.stdout),
show OpenSSH 10.0p2, `UseDNS yes`, `MaxStartups 10:30:100`, no per-source startup
limit, and default penalties including `noauth:1` and `min:15`. The sampled
host load was 0.34, swap use was zero, and SSH listener queues were empty.
The filtered recent auth evidence showed no startup/penalty rejection; this
is bounded historical evidence, not proof that throttling never occurred.
A single sparse bare TCP probe does not establish the accumulated penalty
needed for enforcement. ROOT's filtered PF inspection found no SSH/rate rule
match; it did not constitute a complete firewall/path audit.

FreeBSD has relevant code absent from upstream portable OpenSSH: with
`HAVE_LOGIN_CAP`, its session process calls `auth_get_canonical_hostname`
with `options.use_dns` before exchanging SSH identification. See
[FreeBSD 15.1 session source, lines 1161–1209](https://github.com/freebsd/freebsd-src/blob/releng/15.1/crypto/openssh/sshd-session.c#L1161-L1209).
Therefore using a literal destination IP on the controller does not avoid
server-side reverse DNS. This establishes a possible delay mechanism, not the
cause of the earlier timeouts. Upstream's
[startup/penalty rejection path](https://github.com/openssh/openssh-portable/blob/V_10_0_P2/sshd.c#L559-L605)
attempts a rejection message and closes the accepted socket, rather than
intentionally waiting silently.

At 16:04:40 UTC, three [system reverse lookups](uy3-host-resolver-timings.stdout)
for the controller returned unknown-host in 6.131, 3.633 and 3.930 ms. The
configured resolver was `192.168.1.1`. These later fast failures do not establish
DNS behavior during N15. ROOT also observed that the controller route uses
Wi-Fi `wlp0s20f3`; cumulative RX/TX drops of 4005/18 had no interval baseline
or correlation with the failures, so they do not prove path loss.

## Corrected bounded packet observation

The [R3 receipt](uy3-host-packet-diagnostic-r3-receipt.json) records a 35-second,
400-packet-limited metadata capture, excluding its own SSH connection. Its
filter admitted only controller SSH and controller PTR query/response traffic
to/from the resolver. It retained no payload dump or pcap. Capture ended at its
expected deadline (exit 124), with 131 packets captured and zero kernel drops;
that count does not establish absence of network loss.

Each of three separate pinned `ssh -v -N` observations had exactly one
`ConnectTimeout=3`. No remote command or forwarding was requested. ROOT held
each connection for five seconds, then terminated and reaped its local SSH
process; all exited 0. [Authentication excerpts](authentication-excerpts.json)
show public-key authentication and exit status 0 for each, with hashes binding
the excerpts to the original verbose logs retained outside the repository.
These are successful transport observations, not native test passes.

The [raw packet summaries](uy3-host-packet-diagnostic-r3.stdout) use host local
time UTC+02:00. Values below are calculated from timestamps on f3:

| Sample | Client port | SYN (local time) | Pre-banner PTR round trip | SYN to server banner | DNS response to server banner |
|---|---:|---|---:|---:|---:|
| 0 | 54428 | 18:09:48.694239 | 4.392 ms | 21.433 ms | 0.192 ms |
| 1 | 60028 | 18:09:55.696855 | 3.109 ms | 17.257 ms | 0.155 ms |
| 2 | 38784 | 18:10:02.697956 | 2.438 ms | 13.990 ms | 0.187 ms |

All three PTR replies were NXDOMAIN. The server sent its identification just
after each reply, corroborating the FreeBSD ordering. None of these samples
contained the earlier failure, so neither DNS nor Wi-Fi is established as its
cause. No deterministic controller defect warranting a rewrite was confirmed.

## Limitations and next evidence

Earlier diagnostic attempts remain preserved in the original evidence directory
listed in [the manifest](manifest.json). The initial host query failed before
inspection because privilege escalation cleared `SSH_CONNECTION`. The first
packet attempt failed shell quoting before sampling. R2 did capture two fast
handshakes, but observed its own SSH output until reaching its packet cap and
specified `ConnectTimeout=5` before `ConnectTimeout=3`; first-value semantics
mean it was not a three-second reproduction. R3 corrected both limitations.

The next useful evidence is a bounded passive capture during a separately
approved run, with the same narrow traffic filter and exclusion of the capture
connection. This report does not authorize that run or alter its approved
operations. A failure capture should correlate SYN/ACK, server identification,
DNS request/reply timing, and controller/server timestamps. DNS delay followed
by immediate identification would support DNS causation; prompt identification
with missing acknowledgements would instead support path loss. Preserve the
original failed logs and terminal/uncertain classifications regardless of the
subsequent result. Do not raise timeouts or retry uncertain commands to obtain
a pass.

The copied evidence bytes are listed and hashed in [manifest.json](manifest.json).
