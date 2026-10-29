// Package transactions implements a bounded persistent transaction protocol
// ledger. It never publishes a namespace, grants serving authority, or infers
// abort from a canceled context. Callers keep the entire dependency closure
// gated while applying or recovering its records.
package transactions
