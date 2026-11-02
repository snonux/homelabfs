// SPDX-License-Identifier: GPL-2.0-only
/* Execute the same policy functions used by session capture and inode apply. */
#include "hfs_policy.h"
#include "policycases.h"

unsigned int hfs_test_alloc_call;
unsigned int hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;

#define CHECK(expr) do { if (!(expr)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #expr); exit(1); } } while (0)

static void test_groups(void)
{
	u32 input[HFS_GROUPS_MAX + 1], output[HFS_GROUPS_MAX];
	u32 count, i;

	for (i = 0; i < HFS_GROUPS_MAX; i++)
		input[i] = HFS_GROUPS_MAX - i - 1;
	CHECK(!hfs_groups_snapshot(input, HFS_GROUPS_MAX, output, &count));
	CHECK(count == HFS_GROUPS_MAX);
	for (i = 0; i < count; i++)
		CHECK(output[i] == i);
	/* In-place conversion is what the production capture path uses. */
	CHECK(!hfs_groups_snapshot(input, HFS_GROUPS_MAX, input, &count));
	CHECK(count == HFS_GROUPS_MAX);
	for (i = 0; i < count; i++)
		CHECK(input[i] == i);
	for (i = 0; i <= HFS_GROUPS_MAX; i++)
		input[i] = 71;
	CHECK(!hfs_groups_snapshot(input, HFS_GROUPS_MAX, output, &count));
	CHECK(count == 1 && output[0] == 71);
	CHECK(hfs_groups_snapshot(input, HFS_GROUPS_MAX + 1, output, &count) == -E2BIG);
	CHECK(count == 0);
	input[HFS_GROUPS_MAX - 1] = U32_MAX;
	CHECK(hfs_groups_snapshot(input, HFS_GROUPS_MAX, output, &count) == -EOVERFLOW);
	CHECK(count == 0);
	CHECK(!hfs_groups_snapshot(NULL, 0, NULL, &count) && !count);
	CHECK(hfs_groups_snapshot(NULL, 1, output, &count) == -EINVAL);
	input[0] = U32_MAX - 1;
	input[1] = 0;
	input[2] = 2000;
	CHECK(!hfs_groups_snapshot(input, 3, output, &count));
	CHECK(count == 3 && output[0] == 0 && output[1] == 2000 && output[2] == U32_MAX - 1);
	/* Different supplementary groups must remain different with identical UID. */
	input[0] = 2001;
	CHECK(!hfs_groups_snapshot(input, 1, output, &count));
	CHECK(count == 1 && output[0] == 2001);
	input[0] = 2002;
	CHECK(!hfs_groups_snapshot(input, 1, output, &count));
	CHECK(count == 1 && output[0] == 2002);
}

static void test_identity_and_uuid(void)
{
	uuid_t ns = {{0}}, id;
	struct hfs_json *j;
	const char *valid = "\"12345678-9abc-def0-1234-56789abcdef0\"";
	static const char * const invalid[] = {
		"\"00000000-0000-0000-0000-000000000000\"",
		"\"12345678-9ABC-def0-1234-56789abcdef0\"",
		"\"123456789abc-def0-1234-56789abcdef0\"",
		"\"12345678_9abc-def0-1234-56789abcdef0\"",
		"\"12345678-9abc-def0-1234-56789abcdefg\"",
		"\"12345678-9abc-def0-1234-56789abcdef00\"",
		"\"12345678-9abc-def0-1234-56789abcdef\"",
		"null", "42", "{}", "\"\"",
	};
	u32 i;

	CHECK(hfs_identity_validate(1000, 2000, &ns, 0) == -EACCES);
	ns.b[15] = 1;
	CHECK(!hfs_identity_validate(1000, 2000, &ns, 0));
	CHECK(!hfs_identity_validate(0, 0, &ns, 0));
	CHECK(hfs_identity_validate(U32_MAX, 2000, &ns, 0) == -EOVERFLOW);
	CHECK(hfs_identity_validate(1000, U32_MAX, &ns, 0) == -EOVERFLOW);
	CHECK(hfs_identity_validate(1000, 2000, &ns, -EREMOTEIO) == -EREMOTEIO);
	CHECK(hfs_identity_validate(1000, 2000, &ns, -EACCES) == -EACCES);
	CHECK(hfs_identity_validate(1000, 2000, NULL, 0) == -EACCES);
	CHECK(!hfs_json_parse(valid, strlen(valid), &j));
	CHECK(!hfs_policy_uuid(j, 0, &id));
	CHECK(id.b[0] == 0x12 && id.b[1] == 0x34 && id.b[15] == 0xf0);
	hfs_json_free(j);
	for (i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) {
		CHECK(!hfs_json_parse(invalid[i], strlen(invalid[i]), &j));
		CHECK(hfs_policy_uuid(j, 0, &id) == -EPROTO);
		hfs_json_free(j);
	}
}

static void test_attributes(void)
{
	struct hfs_json *j;
	struct hfs_attr attr, unchanged;
	uuid_t home = {{0}}, allocation = {{0}}, wrong = {{0}};
	u32 i;
	int error;

	home.b[15] = 1;
	allocation.b[15] = 2;
	wrong.b[15] = 3;
	memset(&unchanged, 0xa5, sizeof(unchanged));
	for (i = 0; i < POLICY_ATTR_COUNT; i++) {
		const struct attr_case *test = &attribute_cases[i];
		CHECK(!hfs_json_parse(test->body, strlen(test->body), &j));
		attr = unchanged;
		error = hfs_attr_decode(j, 0, &home, &allocation, 0, &attr);
		if (test->valid) {
			CHECK(!error);
			CHECK(attr.uid == 12001 && attr.gid == 53000);
			CHECK(attr.mode == test->mode && attr.type == test->type);
			CHECK(attr.size == test->size && attr.nlink == test->nlink && attr.revision == test->revision);
			CHECK(!memcmp(attr.home.b, home.b, 16) && !memcmp(attr.allocation.b, allocation.b, 16));
			CHECK(attr.atime.seconds == INT64_MIN && attr.atime.nanos == 999999999);
			CHECK(attr.mtime.seconds == 0 && attr.mtime.nanos == 0);
			CHECK(attr.ctime.seconds == INT64_MAX && attr.ctime.nanos == 1);
			CHECK(!hfs_attr_decode(j, 0, &home, &allocation, attr.type, &attr));
			CHECK(hfs_attr_decode(j, 0, &home, &allocation, attr.type == S_IFDIR ? S_IFREG : S_IFDIR, &attr) == -EPROTO);
			CHECK(hfs_attr_decode(j, 0, &wrong, &allocation, 0, &attr) == -EPROTO);
			CHECK(hfs_attr_decode(j, 0, &home, &wrong, 0, &attr) == -EPROTO);
		} else {
			if (error != -EPROTO) {
				fprintf(stderr, "unexpected accept/error for %s: %d\n", test->name, error);
				exit(1);
			}
			CHECK(!memcmp(&attr, &unchanged, sizeof(attr)));
		}
		hfs_json_free(j);
	}
}

static void test_write_max_end(void)
{
	CHECK(hfs_write_max_end(0, true, 9223372036854775807ULL) == 0);
	CHECK(hfs_write_max_end(7, true, 9223372036854775807ULL) == 7);
	CHECK(hfs_write_max_end(U64_MAX, true, 9223372036854775807ULL) == 9223372036854775807ULL);
	CHECK(hfs_write_max_end(U64_MAX, true, U64_MAX) == 9223372036854775807ULL);
	CHECK(hfs_write_max_end(U64_MAX, false, U64_MAX) == 2147483647ULL);
	CHECK(hfs_write_max_end(19, false, U64_MAX) == 19);
	CHECK(hfs_write_max_end(U64_MAX, false, 1234) == 1234);
	CHECK(hfs_write_max_end(99, true, 20) == 20);
	CHECK(hfs_write_max_end(99, false, 0) == 0);
}

static void test_request_admission(void)
{
	u32 i;

	CHECK(hfs_request_admit(true, false, 0, 0, 1) == -EHOSTDOWN);
	CHECK(!hfs_request_admit(true, true, 0, 0, 1));
	CHECK(hfs_request_admit(false, false, 4096, 0, 1) == -ENOSPC);
	CHECK(!hfs_request_admit(false, true, 4096, 0, 1));
	CHECK(!hfs_request_admit(true, true, 4096, 0, 1));
	CHECK(hfs_request_admit(false, true, 0, 13312, 1) == -ENOSPC);
	CHECK(!hfs_request_admit(false, false, 0, 13312, 1));
	CHECK(!hfs_request_admit(false, false, 4095, 13312, U64_MAX - 1));
	CHECK(!hfs_request_admit(false, true, 4096, 13311, U64_MAX - 1));
	CHECK(!hfs_request_admit(true, true, 4096, 13311, U64_MAX - 1));
	CHECK(hfs_request_admit(false, false, U32_MAX, 0, 1) == -ENOSPC);
	CHECK(hfs_request_admit(false, true, 0, U32_MAX, 1) == -ENOSPC);
	for (i = 0; i < 2; i++) {
		CHECK(hfs_request_admit(false, i, 0, 0, 0) == -ENOSPC);
		CHECK(hfs_request_admit(false, i, 0, 0, U64_MAX) == -ENOSPC);
	}
	CHECK(hfs_request_admit(true, true, 0, 13312, 1) == -ENOSPC);
	CHECK(hfs_request_admit(true, true, 0, 0, U64_MAX) == -ENOSPC);
}

int main(void)
{
	test_groups();
	test_identity_and_uuid();
	test_attributes();
	test_write_max_end();
	test_request_admission();
	puts("PASS: actual production group/identity/UUID/metadata helpers; full1024/1025, idmap failures, 07777, committed UID/GID, object/type/schema/range boundaries; write max_end; separate ordinary/control claim admission");
	return 0;
}
