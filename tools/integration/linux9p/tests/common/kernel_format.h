/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef HFS_TEST_KERNEL_FORMAT_H
#define HFS_TEST_KERNEL_FORMAT_H
#include <stdarg.h>
#include <stddef.h>
int hfs_test_vsnprintf(char *out, size_t capacity, const char *format, va_list args);
#define vsnprintf hfs_test_vsnprintf
#endif
