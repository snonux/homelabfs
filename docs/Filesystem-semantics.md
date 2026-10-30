# HomelabFS filesystem semantics and workloads

[Architecture overview](Architecture.md)

The required application-facing behavior and configured f3s workload baseline. Compatibility remains to be demonstrated through the [integration tests](Integration-tests.md).

## Application compatibility and passthrough

Target normal file access for every configured f3s application without application-specific storage APIs. Use underlying ZFS operations to implement local behavior where possible. Passing operations through does not automatically preserve their semantics across replicated copies, FUSE caches, and a VM access transport.

HomelabFS must coordinate all mutations, not only write calls: creation and exclusive creation, append, truncate, rename, unlink, mkdir/rmdir, links, ownership, permissions, and explicit timestamp changes. Define automatic timestamp behavior too. Preserve atomic namespace changes, open-file lifetime after unlink, inode identity for hard links, and ordering needed by application recovery. Cross-directory rename may require coordination over multiple objects.

Application-facing advisory locks (fcntl/flock) are distinct from HomelabFS's multi-origin ordering and preferred-owner role. Concurrent processes in the same supported lock domain must retain ordinary application locking behavior. Separate local ZFS replicas cannot by themselves enforce a shared lock domain. Specify lock handling across node changes, process exits, and failures before claiming compatibility.

Support and test fsync/fdatasync or their platform equivalents, synchronous I/O, directory persistence, and writable mmap/msync through the complete access path. Memory-mapped modifications must enter the same replication protocol. Keep database files, journals, WAL files, and namespace operations in a recovery-consistent order. Accepted loss of the last acknowledged writes under the chosen multi-failure scenario must not become arbitrary reordering or inconsistent recovery.

## f3s workload baseline

The local Argo CD manifests and their chart references were inspected on 2026-10-24. This is the configured portfolio, not a live cluster health or compatibility result. Files ending in `.yaml.disabled` are excluded.

Services: anki-sync-server, bgtutor, beets-art, filebrowser, forgejo, goprecords, immich, ipv6test, keybr, kobo-sync-server, miniflux, navidrome, opodsync, player, protonbridge, radicale, shuriken, syncthing, wallabag, webdav, xplayer, and ychat. Infrastructure and monitoring include registry, pkgrepo, cert-manager, traefik-config, prometheus, pushgateway, and argo-rollouts. Use isolated instances and synthetic fixtures representative of persistent-storage users and shared data consumers in that portfolio; not every listed component needs a replicated volume.

| Observed configuration | Compatibility focus |
|---|---|
| Forgejo and Keybr explicitly configured for SQLite | Locking, sync ordering, journal recovery, mmap where used, exclusive creation, and atomic rename |
| Immich and Miniflux PostgreSQL deployments | WAL/data ordering, durable sync boundaries, crash recovery, and single active database instance |
| Navidrome music and beets-art shared volumes | Readers observing another workload's changes, metadata freshness, rename, and permissions |
| Syncthing, WebDAV, filebrowser, registry, and other file consumers | File and directory operations, replacement, traversal, timestamps, and notification behavior where used |

SQLite WAL requires same-host shared-memory coordination; do not assume distributing its database files makes concurrent database processes on different hosts safe. Validate the actual journal mode and access transport for each SQLite workload. Application file notifications are an additional compatibility concern beyond POSIX and need testing if a workload relies on them.

Sources: local `~/git/conf/f3s/argocd-apps/README.md`, active application manifests, and referenced chart templates; f3s skill `references/k3s/install.md` and `references/storage/backups.md`. External semantics: [SQLite WAL documentation](https://www.sqlite.org/wal.html), [PostgreSQL reliability](https://www.postgresql.org/docs/current/wal-reliability.html).

The later [2026-10-28 deployment review](Deployment-readiness.md#inventory-before-volume-design) expands this baseline with Player/Syncthing sharing, Immich video aliases, Shuriken parent access and non-pod consumers. Use selected patterns for synthetic canary fixtures and volume design. This is not a production migration inventory; replacing remote access or changing production applications is outside this project.

## Unix ownership and permissions

HomelabFS must expose traditional Unix ownership and permissions through the mounted filesystem:

- Numeric owner UID and group GID for each file and directory.
- Read, write, and execute bits for owner, group, and others.
- Standard operations such as stat, chmod, chown, and chgrp.
- Creation permissions honoring the caller's umask, with FreeBSD-compatible group inheritance.
- Directory search and modification checks, supplementary group membership, sticky directories, and setuid/setgid semantics subject to mount policy.

Unix ownership identifies users and groups. It is separate from the preferred node ownership coordinated through etcd; ownership does not exclude admitted non-owner writers.

Store ownership and mode bits as replicated filesystem metadata. Permission and ownership changes use the same journal, ordering, fencing, and acknowledgement rules as data changes. Snapshot recovery must preserve them. Hard links, if supported, share the same inode metadata.

Require consistent numeric UID/GID assignments across nodes for shared users and groups; names are resolved locally and are not the replicated identity. Any future identity mapping must be explicit.

Enforce access using the requesting workload's credentials, not the daemon's credentials. Configure and verify the division of checks between FreeBSD FUSE and the daemon, including privileged ownership changes and special-bit handling. A privileged daemon's ability to access backing files must not grant that access to every caller.

Metadata caches and replica freshness must not allow new access checks against obsolete permissions after a committed permission change. Existing open descriptors should follow the chosen FreeBSD semantics; chmod is not a blanket revocation of already-open access.

The local service now implements version-one Unix checks and journaled metadata
replay; [the service contract](spec/Filesystem-service.md) defines exact rules and
explicit unsupported forms. [The platform boundary](Platform-interfaces.md#unix-permission-enforcement-boundary)
selects supplementary kernel `default_permissions` checks and requires complete
request-bound credentials. Native P02, replay across real replicas and guest
credential transport remain separate validation gates.

ACLs and extended permission models are outside the currently requested scope. Validate traditional permissions with multiple users and groups, denied operations, umask, directory traversal, sticky directories, ownership changes, replay, and failover.
