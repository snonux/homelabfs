# Static cluster membership and node lifetimes

Task `0r3` implements `internal/cluster`: an immutable, default-deny registry,
a separate static JSON configuration loader, and a durable exclusive local node
lifetime API. This is configuration and identity admission, not replication,
serving authority, a fence, readiness, emergency admission or a normal daemon.
The existing [local CLI configuration](Configuration.md) remains a disposable
validation profile with labelled fixture peers. It does not load this schema or
start a normal cluster node. Its immutable source creator incarnation remains
fixture history; it is not a transport lifetime.

## Configuration version 1

`cluster.Load(ctx, cluster.JSONFile{Path: name})` loads one complete snapshot
through the small `Loader` interface. Future coordination loading can implement
that interface. The registry makes no etcd calls, has no mutation/hot-reload API,
and copies all input/output slices. Concurrent policy checks use the same
installed snapshot. Replacing a configuration file does not revoke established
sessions or safely reconfigure a group; those transitions remain external work.

The separate UTF-8 JSON file is at most 1 MiB, regular, singly linked, owned by
the effective daemon UID and exactly mode 0600. Its path is clean and absolute,
with no symlink ancestors or final symlink. Administrative ownership/exclusivity
of the path and ancestors is required; these checks do not defend against a
hostile administrator replacing the tree. Every field below is required, names
are exact and case-sensitive at every nesting level, and duplicate/unknown
fields, nulls, trailing values and unsupported versions are errors.

```json
{
  "format_version": 1,
  "cluster_id": "11111111-1111-4111-8111-111111111111",
  "registry_revision": 1,
  "nodes": [
    {
      "node_id": "22222222-2222-4222-8222-222222222222",
      "admission": "admitted",
      "replication_addresses": ["192.0.2.1"]
    },
    {
      "node_id": "33333333-3333-4333-8333-333333333333",
      "admission": "admitted",
      "replication_addresses": ["192.0.2.2"]
    },
    {
      "node_id": "44444444-4444-4444-8444-444444444444",
      "admission": "admitted",
      "replication_addresses": ["2001:db8::3"]
    }
  ],
  "volumes": [
    {
      "volume_id": "55555555-5555-4555-8555-555555555555",
      "generation": "66666666-6666-4666-8666-666666666666",
      "configuration": "77777777-7777-4777-8777-777777777777",
      "epoch": 1,
      "coordinator_term": 1,
      "members": [
        {"node_id": "22222222-2222-4222-8222-222222222222", "role": "primary"},
        {"node_id": "33333333-3333-4333-8333-333333333333", "role": "sync"},
        {"node_id": "44444444-4444-4444-8444-444444444444", "role": "async"}
      ]
    }
  ]
}
```

These UUIDs and documentation addresses are examples, not a deployment. IDs must
be nonzero canonical lowercase UUIDs. Revision, epoch and coordinator term are
positive unsigned integers. A snapshot has 1–4096 nodes and 0–4096 volumes; an
empty volume list permits bootstrap and grants no volume placement.

Each node has 1–16 unique canonical numeric host addresses. Addresses are globally
unique across all records, including pending/removed nodes; neither DNS, prefixes,
ports, zones, mapped IPv4-in-IPv6 spelling, unspecified nor multicast addresses
are accepted. Admission is exactly `admitted`, `pending` or `removed`. Only the
first state passes admission. Duplicate node/volume IDs and reuse of cluster,
node or volume identities across these identity classes are rejected.

Each volume has exactly three distinct admitted members, with one each of
`primary`, `sync`, and `async`. No unassigned node acquires a role by joining the
cluster. The full typed `History` includes generation, configuration, epoch and
coordinator term. `CheckAssignment` requires exact history and role equality;
a larger supplied epoch/term never installs authority or silently substitutes a
partner. Initial fixed placement does not implement live group reconfiguration,
emergency recovery-only roles, automatic promotion or historical-history grants.

`CheckPeer` compares the claimed cluster/node with its approved observed host
address on each new connection. Deployment must independently authenticate the
WireGuard host/address/key mapping, AllowedIPs, firewall and route assumptions
from [Security](../Security.md). The registry cannot discover the WireGuard key
from a TCP socket. `CheckAssignment` supplies a second, per-volume placement
check. Neither function validates current incarnations, sessions, promises,
sequence, retention or readiness. The zero-value registry denies all peers.

## Exclusive durable local lifetime

After validating the registry and local node admission, future normal-node
startup must call `cluster.Start(ctx, controlDir, clusterID, nodeID)` **before
opening any replication or administrative socket**. `Start` requires the private
mode-0700 control root and the existing mode-0600 `node.json` produced by
`node-init`; it never creates or replaces that node identity. Paths have the
same clean absolute/no-symlink/administrative-exclusivity restrictions above.

`Start` validates the exact node manifest and holds a nonblocking exclusive
`flock` on that manifest's open descriptor. Different volumes do not permit two
normal daemon lifetimes for the same control directory. The lock is local to
that directory/inode on the host; it cannot fence a remote host, a cloned control
directory or an old owner. Administrative replacement/copy/rollback of identity
state requires its own recovery/fencing policy.

```text
control/node.json
control/cluster.json
control/incarnations/<fresh-incarnation-uuid>.json
```

The immutable version-1 `cluster.json` records `format_version`, `cluster_id`
and `node_id`, binding this control directory to its cluster. An existing corrupt,
foreign or incorrectly private binding is refused and preserved. An interrupted
but complete matching binding is resynced before use. Each version-1 incarnation
file adds `incarnation_id` to those fields. UUIDs come from the OS cryptographic
random source, files are created with exclusive creation, contents and modified
parent directories are synced, and no identity is returned before all required
barriers succeed. A collision fails rather than resuming or overwriting an old
attempt. Write/sync/close/cancellation failures preserve attempted files; retry
allocates a new UUID. No incarnation history cleanup or implicit repair exists.

The returned typed `Identity` names cluster, node and fresh incarnation. The
caller must keep `Lifetime` open until all services and sockets using that
identity have stopped and drained, then call `Close`. Close is idempotent and
releases the local lock without deleting history. Abrupt process loss releases
the OS lock; the next start persists a fresh incarnation. Inspection and volume
initialization must not call `Start`. This API is implemented but has no normal
daemon startup consumer yet; transport/startup integration belongs to `1r3` and
normal protocol/service integration to `4r3`.

Every new lifetime begins RECOVERING in the eventual protocol. A fresh UUID is
not evidence that an older owner is fenced or that accepted volatile history
was absent. The registry contains no authoritative current remote incarnation;
the [replication handshake](Replication-protocol.md#session-establishment-and-reconnect)
must authenticate and bind both endpoints, sessions and current incarnations,
reconcile history, and establish readiness before fresh acceptance/serving.

Historical journal creator incarnations remain unchanged in `record.Header`
and `record.Authority`. A current authenticated forwarding node's `Identity`
is separate from those immutable bytes. Catch-up must validate original history
and creator authority as well as the current forwarder's permitted transfer;
it must not compare them for unconditional equality, rewrite an old header or
turn a historical ACCEPT into fresh current-incarnation retention evidence.

## Local verification and remaining gates

Unit tests cover strict nested schema refusals, identities/addresses, default
denial, exact histories/roles, immutable concurrent reads and unassigned admitted
nodes. Real local process tests verify exclusive lock refusal in a second
process and fresh persisted identity after abrupt owned-process termination.
Fault tests inject uncertain incarnation writes, file sync, file close and parent
directory sync, verify preserved bytes, reject UUID reuse and allow a fresh
attempt. These are local syscall/process-failure checks, not a power-loss,
replication, fencing, mount, guest, lab or deployment result. Normal serving
and all disruptive/infrastructure validation remain separately gated.
