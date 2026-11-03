/* SPDX-License-Identifier: BSD-2-Clause */
#ifndef HFS9P_SUPERVISOR_H
#define HFS9P_SUPERVISOR_H
#include "hfs9p.h"

/* Called once by the trusted pinned exporter before device construction.
 * parent is an already authorized private local directory. Exclusively creates
 * name beneath it and syncs both directories. Failed resources are retained.
 * scope is installed controller configuration, never bytes from a guest.
 * Uses inherited fd3 only; closes inherited executable fd4 before enrollment.
 * No restoration, fork, exec or descriptor delegation is supported. */
int hfs9p_supervisor_start(int parent, const char *name, const uint8_t *scope,
    size_t length, uint16_t minor);
#endif
