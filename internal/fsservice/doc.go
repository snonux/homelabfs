// Package fsservice implements a bounded single-volume filesystem service over
// journal records, verified apply and stable-ID storage. Normal serving requires
// externally supplied authority, commit/recovery proof and permission policies.
// An explicit disposable validation profile exercises local semantics only.
// Replication, foreign-home transactions, Unix authorization, application locks,
// daemon/FUSE publication and checkpoint acceptance remain separate components.
package fsservice
