# Replication transport 1.0

Implemented by `internal/transport` for disposable synthetic testing. This is a
socket/session boundary for [Replication-protocol](Replication-protocol.md),
not an implementation of its stream state machine, acceptance ledger, continuity
proof, fencing, or workload publication. `internal/normalnode` supplies a separate
normal-node startup caller; the disposable local CLI remains a separate fixture.
No lab deployment, WireGuard policy validation, native compatibility gate or
normal distributed service readiness is claimed.

## Address identity and admission

`Network` supplies context-aware numeric-address `Listen` and `Dial` operations,
returning ordinary `net.Listener`/`net.Conn` sockets with exact numeric local and
remote addresses. A simulator can replace the network without replacing session
validation. `TCP` binds only the configured host IP, binds the outgoing source IP,
and dials only the selected approved numeric destination/port. DNS, wildcard,
IPv4-mapped IPv6, address zones, multicast, and unspecified addresses are rejected.
An ephemeral local port is supported for disposable tests. No route or alternate
interface fallback exists. Returned socket addresses are independently checked.

Each connection checks its observed host against the immutable static cluster
registry and its claimed node. Both endpoints explicitly compare the cluster
UUID and exact registry revision. The local identity must already be admitted at
the configured host address. WireGuard authenticates the host only under the
external routing, AllowedIPs and firewall assumptions in [Security](../Security.md).
TCP cannot inspect a WireGuard public key or prove the ingress/egress interface;
these assumptions still require deployment validation. No custom authentication
scheme is introduced.

The first successfully authenticated peer incarnation is pinned for this local
Endpoint lifetime. Disconnect and registry replacement retain that pin. A new
UUID cannot supersede a known peer lifetime merely by connecting. The future
recovery/coordinator caller must explicitly install an externally reconciled
`ExpectIncarnation` before changing a pin. That call closes all existing classes
for the peer, does not clear revocation, does not prove fencing or accepted-tail
continuity, and supplies no serving rights. A newly started Endpoint has no
knowledge of prior peer lifetimes and must remain RECOVERING until the protocol
layer reconciles them; this in-memory pin is not durable anti-rollback evidence.

## Exact envelope

All integers are unsigned big-endian. UUIDs are raw 16-byte nonzero semantic
identities. Protocol 1.0 has no optional features or flags. Unknown versions,
message/class tags, reserved bits, malformed identities and trailing bytes fail
closed. An envelope has the following fixed 144-byte header:

| Offset | Size | Field |
|---|---:|---|
| 0 | 4 | Total frame bytes, including length and header |
| 4 | 4 | ASCII `HFST` |
| 8 | 2 | Protocol major = 1 |
| 10 | 2 | Protocol minor = 0 |
| 12 | 2 | Message tag |
| 14 | 1 | Connection class |
| 15 | 1 | Reserved = 0 |
| 16 | 16 | Session UUID |
| 32 | 16 | Connection UUID |
| 48 | 16 | Source node UUID |
| 64 | 16 | Source daemon incarnation UUID |
| 80 | 16 | Destination node UUID |
| 96 | 16 | Destination daemon incarnation UUID |
| 112 | 16 | Correlation UUID |
| 128 | 16 | Stream binding UUID |
| 144 | remainder | Body bytes |

Tags 1–21 are respectively HELLO, HELLO_OK, OPEN_STREAM, RESUME, READY, DATA,
ACCEPT_REQUEST, ACCEPTED, RECEIPT, PROGRESS, NEED, FETCH, ARTIFACT_CHUNK, CREDIT,
WAIT, STATUS, STATUS_REPLY, ERROR, CLOSE, PING and PONG. Classes 1–3 are control,
live-data and bulk. DATA uses live or bulk, ARTIFACT_CHUNK uses bulk, ERROR/CLOSE/
PING/PONG may use any class, and other established messages use control.

Only HELLO may omit a session or destination incarnation; control HELLO starts
without a session. Every stream message (OPEN_STREAM through STATUS_REPLY)
requires a nonzero binding. Connection/correlation and both node/source
incarnation identities are always mandatory. Established dispatch compares all
session, connection, source/destination and incarnation identities and the class
against the authenticated connection. Correlation is never an operation ID.

The decoder reads exactly four length bytes before allocating. It rejects any
length below 144, above the configured limit or above the absolute 16 MiB cap.
`Decode` returns a view into its input; `ReadFrame` owns the allocated bytes.
`Send` takes an owned copy before queuing. Caller mutation during Send is invalid.
Transport does not reinterpret journal creator IDs or serialize journal records
a second time: a DATA body can retain the exact original journal bytes. Complete
journal/graph/stream validation belongs to the mandatory protocol Authorizer.

## Handshake and session ownership

HELLO and HELLO_OK have an exact 48-byte body:

| Body offset | Size | Field |
|---|---:|---|
| 0 | 2 | Journal major = 1 |
| 2 | 2 | Journal minor = 0 |
| 4 | 8 | Installed registry revision |
| 12 | 4 | Maximum complete message bytes |
| 16 | 4 | Maximum queued messages |
| 20 | 8 | Maximum queued bytes |
| 28 | 4 | Maximum queued operations |
| 32 | 16 | Cluster UUID |

Both endpoints send HELLO. The dialer supplies a fresh connection/correlation;
the acceptor's greeting echoes these and authenticates the reverse direction.
The acceptor selects the componentwise lower limits in HELLO_OK and allocates a
fresh session UUID for control. The dialer validates and confirms that exact
HELLO_OK with reversed endpoint identities. The acceptor installs the session
before echoing a final HELLO_OK; the dialer installs it after validating that
confirmation. The full handshake has one bounded deadline. No control session
is exposed before confirmation; duplicate control sessions are rejected.
Simultaneous control dials may both fail closed and require the caller to retry.

Additional live/bulk connections repeat the full handshake, naming the exact
control session and both live incarnations. Unbound or stale connections fail.
Closing/losing a control socket invalidates all sockets in its session. Losing a
data socket leaves control alive. A new control connection always creates a new
session. Reconnect retries only the same pinned destination, with bounded
exponential backoff, finite attempts and caller cancellation; it does not resume
stream readiness or infer retained bytes from a previous socket.

## Bounds, revocation and protocol integration

There is exactly one framing writer per connection, including during handshake.
Every write has a deadline and serializes the whole encoded frame. Queued plus
currently writing bytes/operations are reserved before encoding. Capacity errors
provide immediate backpressure. Send cancellation closes the socket because
partial delivery is uncertain. Each connection has one bounded reader and an
unbuffered delivery handoff, so the transport retains at most one incoming frame
outside the caller. Peers must exchange timely traffic/heartbeats: an idle read
expires at the configured timeout (maximum one minute). An undelivered frame
also has that bounded handoff timeout; a stalled application closes its socket
and control-owned session rather than indefinitely hiding EOF behind dispatch.
Socket loss during a blocked handoff is detected no later than that handoff
budget, subject to local scheduling. Neither PING nor a
successful socket write creates a receipt, lease or fencing evidence.

Maximum connections includes pending outgoing dials and all unauthenticated
accepted sockets. Pending dials have deadlines and are cancelled/drained on
shutdown. A queue permits at most 1024 messages/operations and 64 MiB; the product
of configured connections and queued bytes cannot exceed 128 MiB per Endpoint.
Incoming frame storage is separately bounded by connections × maximum message
bytes, also at most 128 MiB. Kernel socket buffers, caller-owned frames and future
protocol retention budgets are outside these transport-owned byte budgets.
Accepted-connection notification storage is bounded by the connection cap.

`Revoke` blocks new connections immediately, closes all active/pending identified
classes and prevents queued dispatch. Registry replacement requires a strictly
newer revision in the same cluster, validates local admission, cancels pending
dials, and closes every session/handshake before resetting explicit revocations.
Authorization is checked again after a protocol callback and before delivered
traffic is returned, so a callback cannot restore revoked admission. Already
transmitted bytes cannot be withdrawn: receivers must independently enforce
installed protocol authority and revocation when accepting or applying work.

`Authorizer.Check` is mandatory for every stream message in both directions.
Absent authorizers deny all stream traffic. The callback must be bounded,
nonblocking, concurrency safe and a validation check without side effects; it
may run more than once for incoming messages. It must validate the exact stream
binding, full generation/configuration/epoch/term, per-volume assignments,
installed authority/promises, readiness/continuity, graph dependencies and any
fresh receipt/acceptance restrictions. Transport authenticates the current
forwarder; that identity need not equal an immutable historical frame's creator.
The callback must distinguish historical forwarding from fresh proposals and
fresh reporter evidence. No default permissive protocol implementation exists.

## Normal-node lifetime

`normalnode.Start` loads the static registry and validates local admission, then
calls durable `cluster.Start` before constructing any Endpoint/listener/session.
A fresh node is RECOVERING; no workload service is created here. The held
Lifetime persists until transport workers and explicitly attached services have
drained. An attached closer failure preserves exclusive lifecycle admission,
including a process-lifetime reference preventing garbage collection from
releasing the lock. Such a node requires process exit/inspection; retrying Close
does not release an uncertain lifetime. Failed listener startup closes transport
before releasing local exclusivity. The local fixture CLI does not use this
normal-cluster startup path or fabricate normal membership/fencing evidence.

## Simulation dependencies

`Config.Clock` supplies every transport timer and bounded context, including
handshake/dial, socket read/write deadlines, dispatch handoff and reconnect
backoff. Nil retains real-time behavior. An injected Network must interpret
socket deadlines on the same time source. `Config.Random` supplies connection,
correlation and session UUID entropy; nil retains cryptographic randomness.
Fixture readers must be concurrency safe and deterministic under their documented
caller ordering. Neither dependency grants identity or protocol authority.
The explicitly controlled in-process fixture and its limitations are documented
in [Simulation](Simulation.md).
