# HomelabFS security and encryption

[Architecture overview](Architecture.md)

Recommended f3s baseline: reuse the existing direct WireGuard mesh for host-to-host replication, keep persistent application connections over it, and enforce explicit HomelabFS membership and per-volume authorization. This is a proposed security design; network rules, credentials, and encryption settings have not been deployed or audited live.

## Trust boundaries

Trust the admitted FreeBSD storage hosts and their administrators. HomelabFS is not a Byzantine fault-tolerant system: a compromised authorized replica can read data entrusted to it and may falsify protocol state. Encryption does not make a malicious storage host safe. Isolate ordinary workloads from storage journals, backing paths, credentials, and administrative interfaces.

Distinguish four identities: a storage node, a daemon/service credential where used, a Kubernetes volume-management caller, and a Unix file-access user. A valid network identity does not grant ownership of a volume, and file UID/GID ownership does not grant cluster administration.

## Replication over WireGuard

The f3s reference describes direct mesh links between f0/f1/f2/f3, as well as other infrastructure peers. Reuse direct storage-host links. WireGuard provides packet encryption and peer-key authentication; its AllowedIPs configuration constrains the inner source addresses a peer can send. See [WireGuard cryptokey routing](https://www.wireguard.com/#cryptokey-routing).

Recommended HomelabFS replication listener behavior:

- Bind only the designated WireGuard address, not wildcard/LAN/public addresses. Enforce firewall ingress and egress rules as well: binding an address alone does not establish which interface carried a packet.
- Allow only explicitly admitted storage-host addresses on the replication port, for both enabled IP families. Do not allow the entire mesh subnet. VMs, gateways, laptops, and phones are not replication members merely because they are WireGuard peers.
- Map each admitted node UUID to approved WireGuard address(es) and the externally managed peer key. Verify exact host address assignments and routing/AllowedIPs; do not let forwarded/NATed traffic impersonate a storage peer. A broad routed prefix is not an individual node identity.
- Validate a connection's observed peer address against that mapping and require its claimed node UUID and cluster UUID to match. A supplied node name or DNS lookup is not authentication. Restrict replies and outgoing connections to approved peers.
- If WireGuard or the approved route is unavailable, retry or fail safely. Never fall back to cleartext LAN addresses. Keep plaintext application traffic confined to the trusted endpoints and encrypted tunnel.

Under this trusted-host model, WireGuard authenticates the host; any process permitted to originate traffic as that host may share that identity. A normal TCP socket does not expose a WireGuard public key directly. The address-to-key association depends on correct host routing/firewall configuration, which must be validated at deployment. If this assumption is unsuitable, use standard mutual TLS with per-node service certificates over the same WireGuard network. Do not invent a custom challenge-response or share one cluster-wide secret.

Persistent connections are useful with either transport. Use bounded, framed streams with deadlines, reconnect/backoff, protocol negotiation, and resume from verified journal checkpoints. Reauthenticate each new session and reject stale node incarnations. Isolate latency-sensitive replication/control from large snapshot transfers using bounded scheduling or separate connections; do not let bulk catch-up starve synchronous ACKs. WireGuard keepalives maintain NAT reachability when needed; application heartbeats measure service progress, and neither replaces fencing. [WireGuard keepalive behavior](https://www.wireguard.com/quickstart/).

TLS would not require a handshake for every write either: persistent TLS sessions amortize connection setup. Reusing WireGuard is recommended for operational simplicity in f3s, not because TLS inherently requires per-write handshakes or because a performance advantage has been measured. No stunnel service is needed for the proposed replication path.

## Membership and volume authorization

An explicit admitted-node list is an agreed requirement. Default to denying nodes absent from that list; network reachability and possession of a WireGuard key do not enroll a node.

Maintain an explicit admitted-node registry with node UUID, cluster UUID, transport identity, admission state, and recovery generation. Initial bootstrap uses a reviewed member list. Adding a WireGuard peer alone does not enroll it into HomelabFS; adding a HomelabFS member does not grant access to every volume.

For every operation, check the locally installed valid authority: cluster/recovery generation, replica-group configuration, sender role, ownership epoch, node incarnation, and expected sequence. Only the authorized primary may originate mutations for that group. Only assigned replicas or explicitly admitted recovery receivers may obtain its journal/snapshot. Never accept a larger claimed epoch as self-proving authority. These checks use validated cached configuration and the established fencing protocol, not an etcd query per write.

Reject foreign volumes, unassigned requests, stale generations, duplicate identities, malformed frames, unreasonable lengths, and unauthorized checkpoint or snapshot requests. Bound allocation before decoding payloads. Log security events without logging file contents or credentials.

In emergency mode, valid WireGuard keys still do not permit rejoining. The persistent emergency admission policy overrides normal membership. A manually approved peer receives recovery-only access to specified volumes; it cannot serve or become primary before explicit transition. External fencing remains necessary for excluded nodes that cannot see the new policy.

Removal revokes application admission, closes existing sessions, and triggers safe group reconfiguration. Remove or rotate transport credentials as appropriate too; changing a registry without closing established sessions is insufficient. WireGuard key rotation remains owned by existing FreeBSD/network tooling, not by a second key manager in HomelabFS.

## Administration and etcd

Prefer a local Unix-domain administrative socket with restricted ownership/mode and OS peer-credential checks. Remote operators use authenticated SSH to invoke that local interface. Separate status access from destructive actions such as emergency activation, forced recovery, membership changes, and snapshot rollback. Audit the operator and selected recovery generation.

If CSI or another remote control service needs an API, use authenticated service credentials with narrowly scoped volume-publication permissions, preferably mutual TLS. Replication membership must not confer arbitrary administrative privileges. Kubernetes service accounts and RBAC control the Kubernetes side; the host API must also authorize the caller and requested worker/volume association. Do not put host administrative credentials in workload pods.

For the dedicated etcd service, use its existing client/peer TLS authentication and RBAC rather than creating a custom authentication layer. Restrict listeners to the intended network and separate credentials for controllers, daemons, and operators. Grant only the necessary HomelabFS key ranges/actions; keep the ability to change admission or emergency policy out of routine replica credentials. Key-range RBAC cannot validate the semantic correctness of an ownership value, so the trusted coordinator and storage protocol must still enforce transitions. Mutual TLS over WireGuard is acceptable here for service identity even when replication itself uses WireGuard only. [etcd TLS authentication](https://etcd.io/docs/v3.6/op-guide/security/), [etcd RBAC](https://etcd.io/docs/v3.6/op-guide/authentication/rbac/).

Plan certificate/key rotation and expiry monitoring before deployment. Expired credentials or revoked nodes must fail closed and recover after authorized renewal without permitting stale ownership. Never disable verification to recover connectivity.

## Filesystem and guest isolation

Enforce Unix permissions using workload credentials through the full 9p/FUSE path, with consistent numeric IDs. Scope each publication to its assigned volume and prevent path traversal, symlink races, or handle reuse from escaping the volume root. Restrict backing datasets, journals, and control files to the storage service. The 9p share must not expose those private paths alongside published data.

The virtio-9p connection is a local VM/host boundary, not a network identity protocol. Explicitly validate credential mapping, root/privileged guest behavior, and the containment provided by bhyve and the share root. A guest administrator is not an untrusted tenant isolated merely by Unix UID ownership. Guest or Kubernetes compromise must not grant access to replication or administrative listeners through an overly broad WireGuard rule.

## Encryption at rest and recovery keys

Use native ZFS encryption for the backing data, journals, and checkpoints where encrypted storage is required, managed by existing FreeBSD tooling. Network encryption and ZFS encryption solve different problems. Ensure snapshots and exported backups retain the intended encryption; a decrypted export needs its own protection. [OpenZFS encryption properties](https://openzfs.github.io/openzfs-docs/man/master/7/zfsprops.7.html).

ZFS keys must be available independently of HomelabFS and its workload volumes so recovery has no circular dependency. Missing keys leave the replica unavailable with a clear diagnostic. Keep recoverable offline key material and test restore using it. Logs, crash dumps, swap, and etcd backups can contain sensitive material and require appropriate access/protection. Encryption at rest does not protect plaintext on a running, unlocked, compromised host.

## Security integration tests

| ID | Scenario | Expected result |
|---|---|---|
| SEC01 | Connect from LAN/public address, VM, gateway, or roaming mesh peer | Replication denied; only approved storage peers can reach and establish sessions |
| SEC02 | Authorized peer claims another UUID, cluster, volume role, epoch, or generation | Mismatch rejected; authenticated transport never overrides protocol authority |
| SEC03 | WireGuard disappears, routes change, or forwarded source impersonates a storage node | No cleartext fallback or identity bypass; writes follow ordinary quorum/authority rules |
| SEC04 | Revoke peer or rotate credentials with existing connections and in-flight work | Old sessions lose access; new authorized sessions resume safely with deduplication |
| SEC05 | Emergency-barred node has valid WireGuard identity; admitted peer is recovery-only | Automatic join/serving denied; only explicitly authorized catch-up allowed |
| SEC06 | Workload or low-privilege control client requests admin action, foreign volume, journal, or traversal | Access denied at the appropriate layer without leaking data |
| SEC07 | Oversized/malformed frames, connection floods, and stalled bulk transfers | Bounded resource use; unrelated groups and control traffic remain serviceable within resource limits |
| SEC08 | etcd certificate expires or role lacks privilege | Access fails closed; recovery after authorized repair never bypasses fencing |
| SEC09 | ZFS key absent; restore encrypted snapshot/backup using offline keys | No fallback plaintext volume; restore succeeds only with correct keys and recovery procedure |
| SEC10 | Alternate IPv6 path, duplicate UUID, credential replay after snapshot restore | Same admission/generation rules enforced; no alternate-path authorization bypass |

These complement the [recovery integration tests](Integration-tests.md). Validate security assumptions on the actual f3s topology before treating WireGuard source addresses as trusted node identities.
