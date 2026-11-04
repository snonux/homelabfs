/* SPDX-License-Identifier: BSD-2-Clause */
#ifndef HFS9P_SUPERVISOR_H
#define HFS9P_SUPERVISOR_H
#include "hfs9p.h"

/* Called once by the trusted pinned exporter startup worker after pending
 * device construction and complete operator enrollment.
 * parent is an already authorized private local directory. Exclusively creates
 * name beneath it and syncs both directories. Failed resources are retained.
 * scope is installed controller configuration, never bytes from a guest.
 * Uses inherited fd3 only; closes inherited executable fd4 before enrollment.
 * No restoration, fork, exec or descriptor delegation is supported. */
int hfs9p_supervisor_start(int parent, const char *name, const uint8_t *scope,
    size_t length, uint16_t minor);
/* Explicit exact controller-installed feature selection; old entry point keeps
 * the original tuple. Terminal selection additionally pins actual controller. */
int hfs9p_supervisor_start_tuple(int, const char *, const uint8_t *, size_t,
    uint16_t, uint32_t);

/* Read bounded operator-installed HSB1 from inherited fd6 and owned parent fd7.
 * Called only by the one owned asynchronous worker after device registration.
 * No virtqueue or VM boot thread waits here. */
int hfs9p_supervisor_bootstrap(void);
typedef struct Hfs9pPending_ Hfs9pPending;
/* Constructor registers dedicated pending ownership without blocking VM boot. */
int hfs9p_supervisor_prepare(const char *, Hfs9pPending **);
int hfs9p_pending_exchange(Hfs9pPending *, const uint8_t *, size_t,
    uint8_t *, size_t, size_t *);

#endif
