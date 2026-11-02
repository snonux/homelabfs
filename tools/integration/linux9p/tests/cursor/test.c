// SPDX-License-Identifier: GPL-2.0-only
/* Execute production directory-cookie storage; no invented readdir executor. */
#include "hfs_cursor.h"

unsigned int hfs_test_alloc_call;
unsigned int hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;

#define CHECK(expr) do { if (!(expr)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #expr); exit(1); } } while (0)

static void check_seek(struct hfs_dir_cursor *cursor, u64 position,
		       const void *expected, u32 expected_len, u64 expected_revision)
{
	u8 after[255];
	u32 len = sizeof(after);
	u64 revision = U64_MAX;

	CHECK(!hfs_cursor_seek(cursor, position, after, &len, &revision));
	CHECK(len == expected_len && revision == expected_revision);
	CHECK(!len || !memcmp(after, expected, len));
}

static void check_failed_seek(struct hfs_dir_cursor *cursor, u64 position,
			      u32 capacity, int expected_error)
{
	u8 after[255], unchanged[255];
	u64 revision = 0x12345678;
	u32 len = capacity;

	memset(after, 0xa5, sizeof(after));
	memcpy(unchanged, after, sizeof(after));
	CHECK(hfs_cursor_seek(cursor, position, after, &len, &revision) == expected_error);
	CHECK(len == capacity && revision == 0x12345678);
	CHECK(!memcmp(after, unchanged, sizeof(after)));
}

static void test_resume(struct hfs_dir_cursor *cursor)
{
	u64 a, b, page2, z, again, changed;
	u8 raw[] = {0x80, 0xff};

	hfs_cursor_init(cursor);
	CHECK(cursor->count == 0 && cursor->next == 3);
	CHECK(!hfs_cursor_record(cursor, "A", 1, 17, &a) && a == 3);
	CHECK(!hfs_cursor_record(cursor, "B", 1, 17, &b) && b == 4);
	CHECK(!hfs_cursor_record(cursor, "page2", 5, 17, &page2) && page2 == 5);
	CHECK(!hfs_cursor_record(cursor, "Z", 1, 17, &z) && z == 6);
	check_seek(cursor, a, "A", 1, 17);
	check_seek(cursor, b, "B", 1, 17);
	check_seek(cursor, page2, "page2", 5, 17);
	check_seek(cursor, z, "Z", 1, 17);
	check_seek(cursor, 0, NULL, 0, 0);
	check_seek(cursor, 1, NULL, 0, 0);
	check_seek(cursor, 2, NULL, 0, 0);
	CHECK(!hfs_cursor_record(cursor, "A", 1, 17, &again) && again == a);
	CHECK(cursor->count == 4 && cursor->next == 7);
	CHECK(!hfs_cursor_record(cursor, "A", 1, 18, &changed) && changed == 7);
	check_seek(cursor, a, "A", 1, 17);
	check_seek(cursor, changed, "A", 1, 18);
	CHECK(!hfs_cursor_record(cursor, raw, sizeof(raw), 18, &again));
	check_seek(cursor, again, raw, sizeof(raw), 18);
	check_failed_seek(cursor, 100, 255, -EINVAL);
	check_failed_seek(cursor, U64_MAX, 255, -EINVAL);
	check_failed_seek(cursor, page2, 4, -ENOSPC);
}

static void test_bounds(struct hfs_dir_cursor *cursor)
{
	struct hfs_dir_cursor *unchanged = malloc(sizeof(*unchanged));
	u64 position = 0xbad, next;
	u32 i;
	char name[256];
	static const struct {
		const char *name;
		u32 len;
	} invalid[] = {{"", 0}, {".", 1}, {"..", 2}, {"a/b", 3}, {"a\0b", 3}, {NULL, 1}};

	CHECK(unchanged);
	hfs_cursor_init(cursor);
	for (i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) {
		CHECK(hfs_cursor_record(cursor, invalid[i].name, invalid[i].len, 1, &position) == -EINVAL);
		CHECK(cursor->count == 0 && cursor->next == 3 && position == 0xbad);
	}
	memset(name, 'a', sizeof(name));
	CHECK(hfs_cursor_record(cursor, name, 256, 1, &position) == -EINVAL);
	CHECK(hfs_cursor_record(cursor, "a", 1, 0, &position) == -EINVAL);
	CHECK(!hfs_cursor_record(cursor, name, 255, U64_MAX, &position));
	check_seek(cursor, position, name, 255, U64_MAX);
	hfs_cursor_init(cursor);
	for (i = 0; i < HFS_DIR_COOKIES_MAX; i++) {
		int len = snprintf(name, sizeof(name), "name-%u", i);
		CHECK(!hfs_cursor_record(cursor, name, len, 12, &position));
		CHECK(position == i + 3);
	}
	CHECK(cursor->count == HFS_DIR_COOKIES_MAX);
	memcpy(unchanged, cursor, sizeof(*unchanged));
	position = 0xbad;
	CHECK(hfs_cursor_record(cursor, "overflow", 8, 12, &position) == -ENOSPC);
	CHECK(position == 0xbad && !memcmp(cursor, unchanged, sizeof(*cursor)));
	CHECK(!hfs_cursor_record(cursor, "name-0", 6, 12, &position) && position == 3);
	CHECK(!memcmp(cursor, unchanged, sizeof(*cursor)));
	for (i = 0; i < HFS_DIR_COOKIES_MAX; i++) {
		int len = snprintf(name, sizeof(name), "name-%u", i);
		check_seek(cursor, i + 3, name, len, 12);
	}
	check_failed_seek(cursor, cursor->next, 255, -EINVAL);
	hfs_cursor_init(cursor);
	cursor->next = S64_MAX;
	CHECK(!hfs_cursor_record(cursor, "final", 5, 21, &position) && position == S64_MAX);
	check_seek(cursor, position, "final", 5, 21);
	next = cursor->next;
	CHECK(next == (u64)S64_MAX + 1);
	position = 0xbad;
	CHECK(hfs_cursor_record(cursor, "past", 4, 21, &position) == -ENOSPC);
	CHECK(position == 0xbad && cursor->next == next && cursor->count == 1);
	CHECK(!hfs_cursor_record(cursor, "final", 5, 21, &position) && position == S64_MAX);
	free(unchanged);
}

int main(void)
{
	struct hfs_dir_cursor *cursor = calloc(1, sizeof(*cursor));

	CHECK(cursor);
	test_resume(cursor);
	test_bounds(cursor);
	CHECK(!hfs_test_alloc_call);
	free(cursor);
	puts("PASS: production cursor keeps old page/revision cookies, rewind/reuse, exact raw names, 4096 bound/no eviction, signed-offset boundary, unchanged failure outputs; zero helper allocations");
	return 0;
}
