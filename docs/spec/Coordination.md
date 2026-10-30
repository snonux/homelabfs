# Coordination allocation and admission storage

Task `cr3` implements `internal/coord`, an infrequent control-plane client with
an etcd v3 backend and an in-memory simulator. Neither backend is connected to
filesystem operations, normal-node startup or automatic takeover. Normal I/O
must use locally installed validated authority without an etcd query per operation.

An allocation authorizes **starting takeover only**. It provides no serving
lease, cache exclusion, durable storage promise, selected history, replica
readiness, acceptance permission, workload publication or VM action. Every gate
in [Takeover](Takeover.md) remains independently required. In particular, a larger
allocated epoch never proves that an old owner has stopped or that its acknowledged
history was recovered. There is no lease expiry/clock-based fencing shortcut.

## Principal capabilities and namespace

Each client binds one cluster UUID and one of three capabilities. Deployment must
provision separate certificate identities and matching etcd RBAC roles; selecting
a capability in Go does not grant server permissions. `Permissions` returns the
minimum ranges for external provisioning; the client cannot manage users or roles.

The root is `/homelabfs/v1/clusters/<canonical-cluster-uuid>/`:

| Suffix | Meaning | Daemon | Controller | Operator |
|---|---|---|---|---|
| `policy` | Complete versioned policy document | read/watch | read/watch | read/write/watch |
| `grants/<volume-uuid>` | Latest attempted normal ownership allocation | read | read/write | read |
| `configurations/<configuration-uuid>` | Immutable allocation/reservation history | none | read/write | none |

`policy` is an exact key, without a prefix wildcard. Directory ranges include the
trailing slash and use etcd's lexicographic prefix successor as the exclusive end;
foreign clusters and sibling names are outside the grants. The controller needs
read/write on reservation keys because its transaction compares absence before
writing. RBAC cannot prevent a trusted controller from bypassing this API or
semantically invalidating a grant; controller credentials must remain private.
There is no key-delete or history-cleanup API. Restoring old etcd snapshots,
namespace reuse and manual edits require the controlled recovery procedure from
Takeover; a fresh/reduced etcd revision is never authority over storage promises.

`NewEtcd` requires explicit HTTPS endpoints, explicit trust-root, client-certificate
and private-key files, a certificate CN for the etcd user, TLS 1.3 or later, and
positive dial/request timeouts. It uses standard certificate/server-name checks.
There is no plaintext, insecure-verification, password or system-root fallback.
Files/ancestors and credentials require administrative protection. Endpoints are
not automatically discovered. Deployment owns server/peer TLS, enabled etcd auth,
certificate expiry/rotation, network restrictions and independent bootstrapping.
The constructor creates the connection pool; a subsequent operation establishes
whether the endpoint and credentials are usable.

## Policy and manual emergency admission

`Policy` format 1 embeds a complete validated `cluster.Config`, reusing its
explicit admission/address/fixed-three-member placement rules. `PutPolicy` is an
operator-only compare-and-swap: revision zero creates only, later writes require
the exact previous etcd modification revision and a strictly increasing registry
revision. The cluster UUID must match the client's namespace. Values are bounded
to 1 MiB and use the package's canonical versioned JSON; malformed, unknown,
duplicate, aliased or noncanonical stored fields fail closed.

Normal policy contains no emergency permissions. Emergency policy requires a
nonzero emergency generation, an explicitly admitted serving-node identity and
an optional bounded list of manually installed recovery-only node/volume grants.
Unknown, removed, duplicate or unspecified recovery grants are denied. No node
can automatically join through WireGuard membership, a larger epoch or a return
from partition. `CheckRecovery` authorizes only the named recovery-transfer scope;
it never makes that peer a normal replica or primary. All normal allocation is
blocked while emergency policy is installed, including allocation to the named
serving node.

Storing a normal or emergency policy **does not activate that mode**, revoke
existing sessions, authorize data loss, prove exclusion of stale clients, restore
normal replication or satisfy the emergency entry/exit barriers. The caller must
perform those separate [Emergency](Emergency.md) and Takeover workflows before
installing any serving configuration. This package supplies no automatic rejoin,
mode transition engine or local policy hot-reload path.

## Allocation and uncertain outcomes

A controller supplies an admitted assigned candidate, fresh daemon incarnation,
volume UUID, exact policy modification revision and exact latest grant revision
(zero only for a volume without any allocation). The policy's configured history
is a floor; a previous allocation's epoch and coordinator term are also floors.
The successor increments both, retains the recovery generation, and obtains a
fresh random version-4 configuration UUID. A generated UUID matching any currently
installed volume configuration is rejected before the transaction. Counter exhaustion fails closed.
A change of recovery generation across existing allocation history is refused;
controlled new-generation recovery belongs to its separate protocol.

One etcd transaction compares the policy revision, per-volume grant revision and
configuration-reservation absence, then writes the new grant and immutable
reservation together. Competing requests using the same predecessor yield at
most one winner. Both keys record the exact cluster/volume/candidate/incarnation,
history and policy revision. Every successful allocation, including one whose
reply is lost, leaves its UUID reservation and full attempt history intact.
Policy changes cannot reset allocated counters. Reservations are never deleted.
The API is per volume: a future takeover controller must also serialize and
reconcile the entire dependent closure before activation.

An RPC failure or deadline may follow a committed transaction. Do not treat it
as proof of rejection or blindly increment/retry with an invented predecessor.
Read the grant using quorum-backed linearizable `GetGrant` and reconcile the
attempt. Reusing the old exact predecessor cannot create a second grant. There
is no automatic retry of failed transactions inside this package. An existing
allocation does not imply continuing authority during coordination failure.

## Reads, notifications and simulation

Reads use etcd's default linearizable range API; there is no serializable stale
read mode. `GetPolicy` returns both `Revision` (the exact key modification revision for CAS)
and `WatchRevision` (the linearizable response revision for watch resumption).
They differ when other coordination keys changed. `WatchPolicy` replays policy
changes strictly after **WatchRevision** and delivers complete validated
replacement candidates. This closes the snapshot/watch race. etcd watches are
notifications, not linearizable reads or proof of quorum/serving rights. The
watch uses require-leader and an independent linearizable policy health read at
each request-timeout interval. A failed probe cancels the library watch and emits
a terminal error even if the library is retrying connection establishment or
reconnection. With scheduler/consumer progress, detection takes one poll interval
plus at most one request deadline; it is not immediate or a clock-based serving
lease. Slow/abandoned consumers must cancel their streams.

Compaction, deletion, corrupt policy or a terminal connection failure ends the
stream with an error. Obtain a fresh linearizable snapshot and explicitly start
a new watch; no gap is silently skipped or policy installed automatically.
Consumers must cancel abandoned watches and independently close any serving
rights when their authority cannot be established. No watch opens such rights.

`Memory` uses the same client/validation/CAS code. Independent connections can be
partitioned; global quorum loss rejects reads and transactions before commit and
terminates existing watches. Recovery enables new requests, without restoring
old watches. `LoseNextCommitReply` instead commits the next successful transaction
and returns unavailable, modelling an unknown outcome. Explicit compaction tests
gap handling. The simulator retains histories until compacted; slow consumers
receive a terminal unavailable error instead of silently losing policy changes.
It does not model disk persistence, Raft elections or actual network timing.

## Validation scope and dependencies

Both backends run the same contract suite: policy creation/CAS, role denial,
monotonic epoch/term and unique configuration allocation, competing requests,
emergency restrictions, replay without a snapshot/watch gap, compaction, malformed
policy and cancellation. Simulation adds isolated partition, quorum-loss and
committed-lost-reply checks. Linux-only test code starts a disposable embedded
etcd with real TLS and enabled RBAC, exercises the contract, and directly checks
server-side write denial, cross-cluster/sibling denial, invalid trust and expired
client credentials. A bounded real-server-stop regression checks that watch
reconnection cannot hide unavailability indefinitely. Embedded etcd is a **test-only import**, not a production
server or an upstream FreeBSD support claim. Production `coord` builds separately
for Linux and FreeBSD. These are local tests, not native lab, VM, fence, canary or
deployment evidence.

The bounded `ox3` [coordination model](../../spec/tla/README.md#coordination-allocation-model)
checks allocation/policy CAS, immutable reservations, supersession, uncertain
outcomes, controller restart and the offline restore boundary. Its monotonicity
and uniqueness checks apply within an uninterrupted etcd namespace lineage;
a snapshot restore can roll back that coordination history. The modeled
operator stops controllers and abandons pending requests before restore. Durable
storage promises and existing cache rights survive independently, so a reused
or reduced etcd revision/epoch grants no authority over them. This is a bounded
contract model, not an implemented restoration or takeover workflow.

The module pins etcd client/API/server packages to **v3.6.16**. The module proxy
reports Go 1.26.0 compatibility, matching this project. The current patch was
verified against the [official 3.6 changelog](https://github.com/etcd-io/etcd/blob/main/CHANGELOG/CHANGELOG-3.6.md).
Design references are the [etcd API guarantees](https://etcd.io/docs/v3.6/learning/api_guarantees/),
[transactions and watches](https://etcd.io/docs/v3.6/learning/api/),
[TLS security](https://etcd.io/docs/v3.6/op-guide/security/) and
[RBAC documentation](https://etcd.io/docs/v3.6/op-guide/authentication/rbac/).
