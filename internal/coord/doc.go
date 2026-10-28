// Package coord isolates infrequent ownership allocation and admission policy
// storage from steady-state I/O. Its etcd and simulation backends share exact
// revision/CAS semantics. Allocations permit beginning takeover only; storage
// promises, cache fencing, history recovery and serving activation remain external.
package coord
