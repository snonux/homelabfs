/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef HFS_CODEC_COMPAT_H
#define HFS_CODEC_COMPAT_H
#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <stdarg.h>
#include <errno.h>
#include <limits.h>
#include <openssl/sha.h>
typedef uint8_t u8;
typedef uint16_t u16;
typedef uint32_t u32;
typedef uint64_t u64;
typedef int64_t s64;
typedef struct { u8 b[16]; } uuid_t;
#define U32_MAX UINT32_MAX
#define U64_MAX UINT64_MAX
#define S64_MAX INT64_MAX
#define GFP_KERNEL 0
#define __printf(a,b) __attribute__((format(printf,a,b)))
#define min_t(type,a,b) ((type)(a) < (type)(b) ? (type)(a) : (type)(b))
extern unsigned int hfs_test_alloc_call;
extern unsigned int hfs_test_alloc_fail;
extern size_t hfs_test_last_allocation_size;
static inline bool hfs_test_allocation_fail(void)
{
	return ++hfs_test_alloc_call == hfs_test_alloc_fail;
}
static inline void *kzalloc(size_t n, int g)
{
	(void)g;
	hfs_test_last_allocation_size = n;
	return hfs_test_allocation_fail() ? NULL : calloc(1, n);
}
static inline void *kvmalloc(size_t n, int g)
{
	(void)g;
	hfs_test_last_allocation_size = n;
	return hfs_test_allocation_fail() ? NULL : malloc(n);
}
static inline void *kvcalloc(size_t n, size_t s, int g)
{
	(void)g;
	hfs_test_last_allocation_size = n * s;
	return hfs_test_allocation_fail() ? NULL : calloc(n, s);
}
#define kfree free
#define kvfree free
static inline void sha256(const void *p, u32 n, u8 *hash) { SHA256(p,n,hash); }
static inline u16 get_unaligned_le16(const void *ptr) { const u8 *p=ptr; return (u16)p[0] | (u16)p[1]<<8; }
static inline u32 get_unaligned_le32(const void *ptr) { const u8 *p=ptr; return (u32)p[0] | (u32)p[1]<<8 | (u32)p[2]<<16 | (u32)p[3]<<24; }
static inline u64 get_unaligned_le64(const void *ptr) { const u8 *p=ptr; return get_unaligned_le32(p) | (u64)get_unaligned_le32(p+4)<<32; }
static inline void put_unaligned_le16(u16 v, void *ptr) { u8 *p=ptr; p[0]=v; p[1]=v>>8; }
static inline void put_unaligned_le32(u32 v, void *ptr) { u8 *p=ptr; p[0]=v; p[1]=v>>8; p[2]=v>>16; p[3]=v>>24; }
static inline void put_unaligned_le64(u64 v, void *ptr) { u8 *p=ptr; put_unaligned_le32(v,p); put_unaligned_le32(v>>32,p+4); }
#endif
