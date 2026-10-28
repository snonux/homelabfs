// Package apply rebuilds an externally selected complete committed owner history
// into a fresh local backing store, synchronizes effects and publishes separate
// local applied progress. It does not select commitment, authorize serving,
// publish distributed checkpoints or advance retention.
package apply
