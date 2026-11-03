/* SPDX-License-Identifier: BSD-2-Clause */
#ifndef HFS9P_JSON_H
#define HFS9P_JSON_H
#include <stddef.h>
#include <stdint.h>

#define HFS9P_TOKENS 32768u
/* Token offsets always refer to immutable input. End includes punctuation. */
typedef struct {
   uint32_t start;
   uint32_t end;
   uint32_t next;
   uint32_t count;
   uint8_t type;
} Hfs9pToken;

typedef struct {
   const uint8_t *bytes;
   size_t length;
   Hfs9pToken *tokens;
   uint32_t count;
} Hfs9pJson;

int hfs9p_json_parse(Hfs9pJson *);
int hfs9p_json_key(const Hfs9pJson *, uint32_t, const char *, uint32_t *);
int hfs9p_json_equal(const Hfs9pJson *, uint32_t, const char *);
int hfs9p_json_u64(const Hfs9pJson *, uint32_t, uint64_t *);
#endif
