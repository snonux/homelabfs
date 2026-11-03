/*:*
 * Bounded canonical ASCII JSON parser. SPDX-License-Identifier: BSD-2-Clause
 * Copyright (c) 2026 HomelabFS contributors.
 *:*/
#include "hfs9p_json.h"

#include <errno.h>
#include <stdbool.h>
#include <string.h>

static int
_json_string(Hfs9pJson *j, size_t *pos, uint32_t *id) {
   uint8_t c;
   if (j->count == HFS9P_TOKENS || *pos >= j->length ||
       j->bytes[*pos] != '"')
      return (EINVAL);
   *id = j->count++;
   j->tokens[*id].start = (uint32_t)(*pos)++;
   j->tokens[*id].type = '"';
   while (*pos < j->length) {
      c = j->bytes[(*pos)++];
      if (c < 32 || c > 126)
         return (EINVAL);
      if (c == '"') {
         j->tokens[*id].end = (uint32_t)*pos;
         j->tokens[*id].next = j->count;
         return (0);
      }
      if (c == '\\') {
         if (*pos >= j->length ||
             (j->bytes[*pos] != '\\' && j->bytes[*pos] != '"'))
            return (EINVAL);
         (*pos)++;
      }
   }
   return (EINVAL);
}

/* Compare decoded keys: sorting the escaped spellings is not canonical JSON
 * key ordering (a quote sorts before a backslash). No temporary key allocation. */
static int
_json_key_order(const Hfs9pJson *j, uint32_t a, uint32_t b) {
   size_t x = j->tokens[a].start + 1, y = j->tokens[b].start + 1;
   size_t xe = j->tokens[a].end - 1, ye = j->tokens[b].end - 1;
   uint8_t xc, yc;
   while (x < xe && y < ye) {
      xc = j->bytes[x++];
      yc = j->bytes[y++];
      if (xc == '\\')
         xc = j->bytes[x++];
      if (yc == '\\')
         yc = j->bytes[y++];
      if (xc != yc)
         return (xc < yc ? -1 : 1);
   }
   return (x == xe ? (y == ye ? 0 : -1) : 1);
}

static int
_json_value(Hfs9pJson *j, size_t *pos, unsigned depth, uint32_t *id) {
   size_t start = *pos;
   uint32_t child, key = 0, previous = 0;
   uint64_t number = 0;
   bool negative;
   uint8_t type;
   if (depth > 24 || start >= j->length || j->count == HFS9P_TOKENS)
      return (EINVAL);
   type = j->bytes[start];
   if (type == '"')
      return (_json_string(j, pos, id));
   *id = j->count++;
   j->tokens[*id].start = (uint32_t)start;
   j->tokens[*id].type = type;
   j->tokens[*id].count = 0;
   if (type == '{' || type == '[') {
      (*pos)++;
      if (*pos < j->length &&
          j->bytes[*pos] == (type == '{' ? '}' : ']')) {
         (*pos)++;
      } else {
         for (;;) {
            if (type == '{') {
               if (_json_string(j, pos, &key) != 0 ||
                   (j->tokens[*id].count &&
                    _json_key_order(j, previous, key) >= 0) ||
                   *pos >= j->length || j->bytes[(*pos)++] != ':')
                  return (EINVAL);
               previous = key;
            }
            if (_json_value(j, pos, depth + 1, &child) != 0)
               return (EINVAL);
            j->tokens[*id].count++;
            if (*pos >= j->length)
               return (EINVAL);
            if (j->bytes[*pos] == (type == '{' ? '}' : ']')) {
               (*pos)++;
               break;
            }
            if (j->bytes[(*pos)++] != ',')
               return (EINVAL);
         }
      }
   } else if (type == 'n' || type == 't' || type == 'f') {
      const char *literal = type == 'n' ? "null" :
          (type == 't' ? "true" : "false");
      size_t n = strlen(literal);
      if (j->length - start < n || memcmp(j->bytes + start, literal, n))
         return (EINVAL);
      *pos += n;
   } else {
      negative = type == '-';
      if (negative)
         (*pos)++;
      if (*pos >= j->length || j->bytes[*pos] < '0' ||
          j->bytes[*pos] > '9' ||
          (negative && j->bytes[*pos] == '0'))
         return (EINVAL);
      do {
         unsigned digit = j->bytes[(*pos)++] - '0';
         if (number > (UINT64_MAX - digit) / 10)
            return (EINVAL);
         number = number * 10 + digit;
         if (digit == 0 && *pos == start + 1 &&
             *pos < j->length && j->bytes[*pos] >= '0' &&
             j->bytes[*pos] <= '9')
            return (EINVAL);
      } while (*pos < j->length && j->bytes[*pos] >= '0' &&
          j->bytes[*pos] <= '9');
      if (negative && number > (UINT64_C(1) << 63))
         return (EINVAL);
   }
   j->tokens[*id].end = (uint32_t)*pos;
   j->tokens[*id].next = j->count;
   return (0);
}

int
hfs9p_json_parse(Hfs9pJson *j) {
   size_t pos = 0;
   uint32_t root;
   j->count = 0;
   if (!j->bytes || !j->tokens || j->length == 0 ||
       j->length > 1048576 || _json_value(j, &pos, 0, &root) ||
       pos != j->length)
      return (EINVAL);
   return (0);
}

int
hfs9p_json_equal(const Hfs9pJson *j, uint32_t id, const char *value) {
   const Hfs9pToken *t = &j->tokens[id];
   size_t n = strlen(value);
   return (t->type == '"' && t->end - t->start == n + 2 &&
       memcmp(j->bytes + t->start + 1, value, n) == 0);
}

int
hfs9p_json_key(const Hfs9pJson *j, uint32_t id, const char *key,
    uint32_t *value) {
   uint32_t i = id + 1;
   if (j->tokens[id].type != '{')
      return (EINVAL);
   while (i < j->tokens[id].next) {
      if (hfs9p_json_equal(j, i, key)) {
         *value = i + 1;
         return (0);
      }
      i = j->tokens[i + 1].next;
   }
   return (EINVAL);
}

int
hfs9p_json_u64(const Hfs9pJson *j, uint32_t id, uint64_t *value) {
   const Hfs9pToken *t = &j->tokens[id];
   uint64_t n = 0;
   if (t->type < '0' || t->type > '9')
      return (EINVAL);
   for (uint32_t i = t->start; i < t->end; i++)
      n = n * 10 + j->bytes[i] - '0';
   *value = n;
   return (0);
}
