// SPDX-License-Identifier: GPL-2.0-only
/* Entire production result/capability decoder; only publication source is a shim. */
#include "hfs_policy.h"

unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#define HFS_CAP_MAX 8192U
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "%d: %s\n", __LINE__, #x); exit(1); } } while (0)
static inline bool uuid_equal(const uuid_t *a, const uuid_t *b)
{
	return !memcmp(a, b, sizeof(*a));
}
struct hfs_session;
struct hfs_provider {
	int (*publication)(struct hfs_session *, const struct hfs_json *, int);
};
struct hfs_session {
	struct { char context[4096]; u32 context_len; } enrollment;
	const struct hfs_provider *provider;
};
#include "result.inc"
#include "fixture.h"

static u32 publications;
static int publication(struct hfs_session *s, const struct hfs_json *j, int token)
{
	(void)s;
	CHECK(hfs_json_is(j, token, "null"));
	publications++;
	return 0;
}

int main(void)
{
	const struct hfs_provider provider = {publication};
	struct hfs_session session = {.provider = &provider};
	struct hfs_buf credentials = {.data = (char *)fixture_credentials,
		.len = sizeof(fixture_credentials) - 1};
	u32 i;

	memcpy(session.enrollment.context, fixture_context, sizeof(fixture_context) - 1);
	session.enrollment.context_len = sizeof(fixture_context) - 1;
	for (i = 0; i < sizeof(cases) / sizeof(cases[0]); i++) {
		struct hfs_json *result = NULL;
		int err;

		CHECK(!hfs_json_parse(cases[i].body, strlen(cases[i].body), &result));
		publications = 0;
		err = hfs_result_validate(&session, result, "ROOT", &credentials);
		if (!!err != !!cases[i].reject) {
			fprintf(stderr, "case %s returned %d\n", cases[i].name, err);
			return 1;
		}
		CHECK(publications == (cases[i].reject ? 0U : 1U));
		hfs_json_free(result);
	}
	puts("PASS: actual host literal ROOT+DOMAIN through production guest decoder; wrapper-rehashed identity/credential/rights/ancestor/order/scope negatives");
	return 0;
}
