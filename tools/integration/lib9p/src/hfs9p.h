/*:*
 * HomelabFS dedicated retained-9p backend. SPDX-License-Identifier: BSD-2-Clause
 * Copyright (c) 2026 HomelabFS contributors.
 *:*/
#ifndef HFS9P_H
#define HFS9P_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define HFS9P_MSIZE 65536u
#define HFS9P_BODY 1048576u
#define HFS9P_CHUNK 32768u
#define HFS9P_HEADER 88u
#define HFS9P_CELLS 80u
#define HFS9P_MAX_CELL 2147483647u
#define HFS9P_STRIDE UINT64_C(2097152)
#define HFS9P_VSIZE (UINT64_C(1) << 52)

typedef struct {
   uint8_t session[16];
   uint64_t node[3];
} Hfs9pEndpoint;

/* The controller owns the lifetime across bhyve/guest replacements. A claim
 * MUST verify installed/running bytes, supervised native identity, endpoint
 * continuity and complete ledgers before returning the sole writer instance.
 * No stock path or backend constructor can manufacture this authority. */
typedef struct Hfs9p_ Hfs9p;
typedef struct Hfs9pConnection_ Hfs9pConnection;

/* These callbacks are mandatory. They authenticate the actual supervised
 * exporter and real FUSE connection on EVERY operation, and authorize the
 * enrolled session scope before the first native write.
 * The backend validates canonical envelopes/routing fields; the host validates
 * the complete closed operation union before effects.
 * Neither is a semantic execution callback. Native I/O is always pread/pwrite.
 * A replacement controller must recover exact bytes/mappings, not return a new
 * empty Hfs9p. Reconciliation is not implemented by hfs9p_new. */
typedef struct {
   void *arg;
   int (*claim_cb)(void *, const Hfs9pEndpoint *, const uint8_t[32]);
   int (*protect_cb)(void *, const uint8_t *, size_t);
   int (*verify_cb)(void *, const Hfs9pEndpoint *, const int[3]);
} Hfs9pVerifier;

/* Takes ownership of a freshly enrolled fd tuple only on success. All fds must
 * be distinct real FreeBSD fusefs direct-I/O private synthetic inodes, size
 * 2^52, with the exact enrolled node IDs. No path is opened by this library.
 * endpoint lifetime MUST be claimed externally before calling this function.
 * The implementation refuses construction outside FreeBSD. */
int hfs9p_new(Hfs9p **, const int[3], const Hfs9pEndpoint *, Hfs9pVerifier);

/* Delete is permitted only after verified protected endpoint retirement. It is
 * not a disconnect/reset operation. Busy returns without destroying ledgers. */
int hfs9p_delete(Hfs9p *);

/* Exact bounded binary ledger; SHA-256 checksum is integrity, not authority.
 * claim_cb must authenticate protected recovery bytes and real endpoint/host
 * ledger continuity. A zero digest requests a genuinely fresh endpoint only.
 * protect_cb durably protects every complete candidate before native effects.
 * Snapshot memory belongs to the caller; free() after use. */
int hfs9p_snapshot(Hfs9p *, uint8_t **, size_t *);
int hfs9p_restore(Hfs9p **, const int[3], const Hfs9pEndpoint *,
    Hfs9pVerifier, const uint8_t *, size_t);

/* Privileged controller operation, never called from guest wire data. Context
 * is the complete canonical installed scope, copied into bounded session state.
 * At most 16 sessions. Existing session contexts cannot be overwritten; old
 * session adoption still requires separately verified host RESUME recovery. */
int hfs9p_add_session(Hfs9p *, const uint8_t *, size_t);

/* Explicit exact bridge minor (0, 1, 2 or 3), installed by the authorized controller.
 * Minor1 binds body schema2/features2047 for the whole session and ledger; no
 * downgrade or kernel PAGE_SIZE other than the 4096 profile is supported. */
int hfs9p_add_session_protocol(Hfs9p *, const uint8_t *, size_t, uint16_t);

/* Bounded nonblocking raw wire exchange, one 9p request/reply. A send receipt
 * never denotes filesystem success. Caller owns separate response storage.
 * Returns errno on malformed/authentication refusal, with *reply_length=0.
 * Allocator contention returns a wire AGAIN for FETCH or ERROR/CAPACITY for
 * SEND; consumers retry the identical packet at any chunk without advancing.
 * Exact Tversion selection is connection-local and does not take that lock. */
int hfs9p_exchange(Hfs9pConnection *, const uint8_t *, size_t, uint8_t *, size_t,
    size_t *);

/* Mandatory controller seam. The default implementation refuses ENOTSUP;
 * production supervision must replace it with actual verified enrollment and
 * recovered endpoint ownership. Merely accepting a config flag is forbidden. */
int hfs9p_controller_claim(const char *, Hfs9pConnection **);

/* Bind one virtio device to an already enrolled session. FETCH is implicitly
 * scoped by this connection, never by a guessed/reverse numeric lookup. */
int hfs9p_connection_new(Hfs9p *, const uint8_t[16], Hfs9pConnection **);
void hfs9p_connection_delete(Hfs9pConnection *);

#endif
