/* SPDX-License-Identifier: BSD-2-Clause */
#ifndef HFS9P_LEDGER_H
#define HFS9P_LEDGER_H
#include "hfs9p.h"

typedef struct Hfs9pLedger_ Hfs9pLedger;

/* Owns a duplicate of an already authorized local state directory, never a
 * guest-selected path. The lock survives connection/session changes. All names
 * are one bounded component. Existing unfinished .next files are preserved. */
int hfs9p_ledger_new(Hfs9pLedger **, int, const char *);
void hfs9p_ledger_delete(Hfs9pLedger *);
int hfs9p_ledger_load(Hfs9pLedger *, uint8_t **, size_t *);
int hfs9p_ledger_claim(Hfs9pLedger *, const Hfs9pEndpoint *, const uint8_t[32]);
int hfs9p_ledger_protect(Hfs9pLedger *, const uint8_t *, size_t);
/* Full descriptor-backed equality without allocating a second ledger copy. */
int hfs9p_ledger_matches(Hfs9pLedger *, const uint8_t *, size_t);

/* This adapter supplies durable local transport bytes, not a real FUSE/native
 * enrollment verifier or normal public claim/source/domain protection. A
 * controller must combine claim() with actual host endpoint continuity facts.
 * Local disk loss/missing ledger after issue must never be treated as fresh. */
#endif
