// Package failpoint provides deterministic named fault injection for approved
// tests, simulators, and disposable lab processes. It does not implement any
// filesystem or replication protocol and does not authorize running tests.
//
// Data-path components receive a Registry and call Hit at an explicit boundary.
// New and the zero value are permanently disabled. Only NewForTesting returns
// the separate Control capability; a future test-only daemon entrypoint can
// retain it for an admin hook. No daemon/admin wiring is provided here.
//
// Each enabled name has an independent hit counter. A rule may fire on every
// hit or exactly once on its Nth hit. Concurrent hit order follows registry
// arrival; use barriers or blocking rules to reproduce a particular schedule.
// Enable replaces the rule and resets its count. Disable, DisableAll, and
// replacement release pending sleeps/blocks. All operations are concurrent-safe.
// Crash exits the complete process via os.Exit and must be exercised in a
// disposable subprocess. Sleep and Block honor caller cancellation.
package failpoint
