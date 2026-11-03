// SPDX-License-Identifier: GPL-2.0-only
/* Kernel printf primitive boundary, including Linux's big-endian %pUb UUID.
 * Unsupported formats fail loudly; this is not protocol/authority logic.
 */
#include <stdarg.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>

static void append(char *out, size_t capacity, size_t *length, const char *text)
{
   while (*text) {
      if (*length + 1 < capacity) out[*length] = *text;
      (*length)++;
      text++;
   }
}

int hfs_test_vsnprintf(char *out, size_t capacity, const char *format, va_list args)
{
   size_t length = 0;
   char value[80], byte[2] = {0};
   const char *text;
   const unsigned char *uuid;
   while (*format) {
      if (*format != '%') {
         byte[0] = *format++;
         append(out, capacity, &length, byte);
         continue;
      }
      format++;
      if (!strncmp(format, "pUb", 3)) {
         uuid = va_arg(args, const void *);
         snprintf(value, sizeof(value),
            "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
            uuid[0], uuid[1], uuid[2], uuid[3], uuid[4], uuid[5], uuid[6], uuid[7],
            uuid[8], uuid[9], uuid[10], uuid[11], uuid[12], uuid[13], uuid[14], uuid[15]);
         format += 3;
      } else if (!strncmp(format, "llu", 3)) {
         snprintf(value, sizeof(value), "%llu", va_arg(args, unsigned long long));
         format += 3;
      } else if (!strncmp(format, "lld", 3)) {
         snprintf(value, sizeof(value), "%lld", va_arg(args, long long));
         format += 3;
      } else if (!strncmp(format, "zu", 2)) {
         snprintf(value, sizeof(value), "%zu", va_arg(args, size_t));
         format += 2;
      } else if (*format == 'u') {
         snprintf(value, sizeof(value), "%u", va_arg(args, unsigned int));
         format++;
      } else if (*format == 'd') {
         snprintf(value, sizeof(value), "%d", va_arg(args, int));
         format++;
      } else if (*format == 's') {
         text = va_arg(args, const char *);
         append(out, capacity, &length, text ? text : "(null)");
         format++;
         continue;
      } else if (*format == '%') {
         strcpy(value, "%");
         format++;
      } else {
         return -1;
      }
      append(out, capacity, &length, value);
   }
   if (capacity) out[length < capacity ? length : capacity - 1] = 0;
   return (int)length;
}
