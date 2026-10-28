// Package storage implements the private home-owned backing store over vfs.
// Logical metadata and names belong to its catalog, never to host stat or paths.
// The trusted apply layer supplies protected, contiguous committed operations;
// storage neither chooses commitment nor enforces workload permissions.
//
// Format is explicit. Open requires an exact clean Image and verifies payload
// hashes; an older catalog cannot bless payloads modified by a later history.
// Journal recovery, checkpoint acceptance, fencing and distributed publication
// remain responsibilities of apply/recovery. Local immutable artifact helpers
// supply barriers, not evidence of a completed distributed checkpoint.
package storage
