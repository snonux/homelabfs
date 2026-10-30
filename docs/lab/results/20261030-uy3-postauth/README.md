# Authenticated SSH liveness failure, 2026-10-30 (uy3)

**The 17:49 UTC failure occurred after authentication. Its cause and the remote
command's completion remain unknown at the point of failure.** This adds a
distinct failure stage to [the earlier investigation](../20261030-uy3-ssh/README.md);
it does not establish one cause for N14's connection timeout, N15's banner
timeout, and this authenticated-session liveness timeout. Task `uy3` remains
open. No controller fix or native acceptance is claimed.

## Exact failure and stage

The [original receipt](host-actual-receipt.json) records the reviewed read-only
host inventory at 17:49:51.134980–17:49:56.470986 UTC, exit 255, stdout zero
bytes. Its [47-byte stderr](host-actual.stderr) says:

```text
Timeout, server 192.168.1.133 not responding.
```

This is not the initial-connect or banner-exchange timeout wording. The actual
local client was verified as OpenSSH 10.2p1. Its
[ServerAlive check](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/clientloop.c#L470-L485)
emits that message from the authenticated client loop; the
[session call follows login](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/ssh.c#L1739-L1773).
The receipt binds `ConnectTimeout=5`, `ServerAliveInterval=2`, and
`ServerAliveCountMax=1`. None of these settings was changed.

The Python inventory prints its result at the end. Empty stdout therefore does
not show that its remote command was never dispatched or started. Independent
ROOT inspection at 17:52:35 established that the prior helper was absent before
the fresh inventory at 17:54:29. The fresh inventory did not replay an
outstanding uncertain action. Later successful observations do not resolve the
cause of the failed connection.

## Independent historical authentication evidence

ROOT executed the [reviewed one-shot proposal](uy3-postauth-auth-window-proposal.json)
and [read-only helper](uy3-postauth-auth-window.py) at
18:07:38.550326–18:07:38.796311 UTC. The
[receipt](uy3-postauth-auth-window-actual-receipt.json) records exit 0 in
0.246 seconds, 813 stdout bytes and no stderr. The
[sanitized output](uy3-postauth-auth-window-actual.stdout) reports host UTC+0200
and these records for source `192.168.1.246`, client port `53412`:

| Host local time | UTC | sshd-session PID | Event |
|---|---|---:|---|
| 19:49:52 | 17:49:52 | 94042 | Public key accepted |
| 19:49:57 | 17:49:57 | 94044 | Connection closed |

These records independently corroborate authentication during the failed
invocation. They do not establish command dispatch, completion, a server stall,
or Wi-Fi/path loss. The helper read only the current auth.log tail, bounded to
1 MiB (801,961 bytes returned to its parser), and selected a 30-second local
time window. It returned only sanitized event classifications, times, PIDs and
ports; it did not read rotated logs, process arguments, environment values or
packet payloads.

## Next evidence and unchanged boundaries

No further live handshake experiment is justified by these records alone.
Useful next evidence would be simultaneous endpoint metadata during the next
separately authorized operation, restricted to that operation's owned SSH
socket tuple. For this authenticated-session failure, identify the tuple from
the owned local SSH process's socket inode after authentication, then restrict
any passive metadata capture to that exact client/server address and port pair.
An unrelated control connection must be excluded; no user traffic, payload dump
or pcap should be collected. Capturing only after tuple identification misses
initial SYN/banner events and must not be presented as diagnosing those stages.

Correlate endpoint timestamps, TCP acknowledgements/retransmissions and bounded
Wi-Fi counter deltas over the same interval. Server response packets visible
on f3 but absent at the controller would locate loss on the intervening path;
they would not by themselves identify Wi-Fi as the failing component. Prompt
TCP acknowledgement but no application response would suggest a different
failure layer. Aggregate historical drop counters cannot make either finding.
Exact capture scope, permissions and bounds still need review before execution.

Preserve failed command bytes and uncertain completion; never replay an
uncertain remote command to obtain a pass. No VM, workload, mount, configuration,
network or power change, packet capture, new harness, or test was performed by
this diagnostic update. ROOT owns remote execution and subsequent review.
The copied evidence is byte-bound by [manifest.json](manifest.json).
