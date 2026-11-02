/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef HFS_POLICY_COMPAT_H
#define HFS_POLICY_COMPAT_H
#include "../codec/compat.h"
#include <sys/stat.h>
#define MAX_NON_LFS 2147483647UL
static inline void sort(void *base, size_t count, size_t size,
			int (*compare)(const void *, const void *),
			void (*swap)(void *, void *, int))
{
	(void)swap;
	qsort(base, count, size, compare);
}
#endif
