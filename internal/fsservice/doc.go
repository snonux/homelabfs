// Package fsservice implements a bounded single-volume filesystem service over
// journal records, verified apply and stable-ID storage. Normal serving requires
// externally supplied authority and commit/recovery proof, plus an explicit
// permission policy; NewUnixPermissions supplies traditional Unix enforcement.
// An explicit disposable validation profile exercises local semantics only.
// Replication, foreign-home transactions, application locks,
// daemon/FUSE publication and checkpoint acceptance remain separate components.
package fsservice
