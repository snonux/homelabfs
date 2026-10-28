// Package fsapi defines the platform-independent filesystem request and result
// contract. Adapters translate native flags, credentials, errors and inode numbers
// at this boundary; none of those native layouts are replicated metadata.
//
// Completion ends one request wait exactly once. It does not implement admission,
// replication, durable deduplication or rollback. Services retain accepted work
// independently of a caller's wait context, and complete every accepted request
// even if its original waiter has gone away. A successful mutation reply requires
// the local-durable plus partner-retained-memory and visibility boundaries in
// docs/spec/Durability.md. Reads require fresh committed metadata and authority.
//
// Object identity survives rename, unlink and ownership changes. Local lookup and
// open references are separate from namespace links and durable recovery pins;
// dropping local references alone never authorizes object reclamation.
package fsapi
