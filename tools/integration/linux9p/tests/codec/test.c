// SPDX-License-Identifier: GPL-2.0-only
/* The implementation is separately compiled from the owned kernel source. */
#include "hfs_protocol.h"
#include "corpus.h"

unsigned int hfs_test_alloc_call;
unsigned int hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;

#define CHECK(expr) do { if (!(expr)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #expr); exit(1); } } while (0)

static void parse_case(const char *body, bool valid)
{
	struct hfs_json *j = NULL;
	int err = hfs_json_parse(body, strlen(body), &j);

	CHECK(valid ? !err && j : err < 0 && !j);
	hfs_json_free(j);
}

static void test_json(void)
{
	static const char * const invalid[] = {
		"", " ", "01", "-0", "-01", "1.0", "1e1", "+1", "NaN",
		"18446744073709551616", "-9223372036854775809", "[1,]",
		"{\"b\":1,\"a\":2}", "{\"a\":1,\"a\":2}", "\"\\n\"",
		"\"\\/\"", "\"\\u0061\"", "\"\177\"", "\"\200\"",
		"{\"a\":1}x", "[", "{", "\"x", "nullx", "truefalse",
		"{\"a\":1,}", "{\"a\" 1}", "[1 2]",
	};
	struct hfs_json *j;
	struct hfs_buf buf;
	u64 number;
	s64 signed_number;
	bool boolean;
	char text[8], hex[4];
	u32 len, i;
	int token;

	for (i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++)
		parse_case(invalid[i], false);
	parse_case("{\"a\":[{\"b\":false},null],\"c\":\"\\\"\\\\\"}", true);
	/* Use explicit strlen so edits cannot silently exercise a prefix. */
	{
		const char *body = "{\"a\":18446744073709551615,\"b\":-9223372036854775808,\"c\":true,\"d\":\"00ff\",\"e\":{\"x\":1}}";
		CHECK(!hfs_json_parse(body, strlen(body), &j));
	}
	CHECK(!hfs_json_exact(j, 0, "a,b,c,d,e"));
	CHECK(hfs_json_exact(j, 0, "a,b,c,d") < 0);
	CHECK(hfs_json_exact(j, 0, "a,b,c,d,e,f") < 0);
	CHECK(hfs_json_exact(j, 0, "a,b,c,d,e,") < 0);
	CHECK(hfs_json_field(j, 0, "x") == -ENOENT);
	token = hfs_json_field(j, 0, "a");
	CHECK(!hfs_json_u64(j, token, &number) && number == U64_MAX);
	CHECK(hfs_json_i64(j, token, &signed_number) == -ERANGE);
	token = hfs_json_field(j, 0, "b");
	CHECK(!hfs_json_i64(j, token, &signed_number) && signed_number == INT64_MIN);
	CHECK(hfs_json_u64(j, token, &number) < 0);
	CHECK(!hfs_json_bool(j, hfs_json_field(j, 0, "c"), &boolean) && boolean);
	token = hfs_json_field(j, 0, "d");
	CHECK(hfs_json_string(j, token, text, sizeof(text)) == 4 && !strcmp(text, "00ff"));
	CHECK(hfs_json_string(j, token, text, 4) == -ENOSPC);
	len = 1;
	CHECK(hfs_json_hex(j, token, hex, &len) == -ENOSPC);
	len = sizeof(hex);
	CHECK(!hfs_json_hex(j, token, hex, &len) && len == 2 && !hex[0] && (u8)hex[1] == 255);
	CHECK(!hfs_buf_init(&buf, 64));
	hfs_buf_literal(&buf, "{\"a\":");
	hfs_buf_token(&buf, j, token);
	hfs_buf_literal(&buf, ",\"b\":");
	hfs_buf_hex(&buf, hex, len);
	hfs_buf_literal(&buf, "}");
	CHECK(!buf.error && buf.len == 23 && !memcmp(buf.data, "{\"a\":\"00ff\",\"b\":\"00ff\"}", 23));
	hfs_buf_free(&buf);
	hfs_json_free(j);
	CHECK(!hfs_buf_init(&buf, 3));
	hfs_buf_literal(&buf, "abc");
	hfs_buf_literal(&buf, "d");
	CHECK(buf.error == -ENOSPC && buf.len == 3);
	hfs_buf_free(&buf);
	CHECK(!hfs_buf_init(&buf, 3));
	hfs_buf_printf(&buf, "%u", 123U);
	CHECK(buf.error == -ENOSPC);
	hfs_buf_free(&buf);
	for (i = 0; i < 3; i++) {
		const char *bad[] = {"\"FF\"", "\"a\"", "\"gg\""};
		CHECK(!hfs_json_parse(bad[i], strlen(bad[i]), &j));
		len = sizeof(hex);
		CHECK(hfs_json_hex(j, 0, hex, &len) < 0);
		hfs_json_free(j);
	}
}

static void test_bounds(void)
{
	struct hfs_buf b;
	struct hfs_json *j;
	char *body = malloc(HFS_BODY_MAX + 1);
	u32 i;

	CHECK(body);
	memset(body, '[', 65);
	memset(body + 65, ']', 65);
	CHECK(hfs_json_parse(body, 130, &j) < 0);
	memset(body, '[', 64);
	body[64] = '0';
	memset(body + 65, ']', 64);
	CHECK(!hfs_json_parse(body, 129, &j));
	hfs_json_free(j);
	body[0] = '"';
	memset(body + 1, 'a', HFS_BODY_MAX - 2);
	body[HFS_BODY_MAX - 1] = '"';
	CHECK(!hfs_json_parse(body, HFS_BODY_MAX, &j));
	CHECK(j->count == 1);
	CHECK(hfs_test_last_allocation_size == sizeof(*j->tokens));
	hfs_json_free(j);
	CHECK(hfs_json_parse(body, HFS_BODY_MAX + 1, &j) < 0);
	CHECK(!hfs_buf_init(&b, HFS_BODY_MAX));
	hfs_buf_literal(&b, "{\"groups\":[");
	for (i = 0; i < HFS_GROUPS_MAX; i++)
		hfs_buf_printf(&b, "%s%u", i ? "," : "", i);
	hfs_buf_literal(&b, "]}");
	CHECK(!b.error && !hfs_json_parse(b.data, b.len, &j));
	CHECK(j->count == HFS_GROUPS_MAX + 3);
	hfs_json_free(j);
	hfs_buf_free(&b);
	/* Dense untrusted tokens exhaust the fixed parser budget cleanly. */
	body[0] = '[';
	for (i = 1; i < 2 * 131072U; i += 2) {
		body[i] = '0';
		body[i + 1] = ',';
	}
	body[2 * 131072U] = ']';
	CHECK(hfs_json_parse(body, 2 * 131072U + 1, &j) == -E2BIG);
	free(body);
}

static void test_frames(void)
{
	uuid_t session = {{0}};
	u8 hash[32], computed[32];
	u8 *frame = malloc(HFS_HEADER_SIZE + HFS_CHUNK_MAX + 1);
	u8 *body = malloc(HFS_BODY_MAX);
	const void *chunk_body;
	u32 total, length, size, offset, i, j;

	CHECK(frame && body);
	session.b[15] = 12;
	memset(body, 'x', HFS_BODY_MAX);
	for (i = 0; i < 32; i++) {
		size = HFS_HEADER_SIZE + HFS_CHUNK_MAX;
		CHECK(!hfs_frame_encode(1, &session, HFS_CELL_MAX, body, HFS_BODY_MAX, i, frame, &size));
		CHECK(!hfs_frame_decode(frame, size, 1, &session, HFS_CELL_MAX, i, &total, hash, &chunk_body, &length));
		CHECK(total == HFS_BODY_MAX && length == HFS_CHUNK_MAX);
		CHECK(!memcmp(chunk_body, body + i * HFS_CHUNK_MAX, length));
		CHECK(hfs_frame_decode(frame, size - 1, 1, &session, HFS_CELL_MAX, i, &total, hash, &chunk_body, &length) < 0);
		CHECK(hfs_frame_decode(frame, size + 1, 1, &session, HFS_CELL_MAX, i, &total, hash, &chunk_body, &length) < 0);
	}
	for (i = 0; i < HFS_HEADER_SIZE; i++)
		CHECK(hfs_frame_decode(frame, i, 1, &session, HFS_CELL_MAX, 31, &total, hash, &chunk_body, &length) < 0);
	size = HFS_HEADER_SIZE + HFS_CHUNK_MAX;
	CHECK(!hfs_frame_encode(2, &session, 1, body, HFS_CHUNK_MAX + 1, 1, frame, &size));
	CHECK(size == HFS_HEADER_SIZE + 1);
	CHECK(!hfs_frame_decode(frame, size, 2, &session, 1, 1, &total, hash, &chunk_body, &length));
	CHECK(total == HFS_CHUNK_MAX + 1 && length == 1);
	CHECK(hfs_frame_decode(frame, size, 3, &session, 1, 1, &total, hash, &chunk_body, &length) < 0);
	CHECK(hfs_frame_decode(frame, size, 2, &session, 2, 1, &total, hash, &chunk_body, &length) < 0);
	CHECK(hfs_frame_decode(frame, size, 2, &session, 1, U32_MAX, &total, hash, &chunk_body, &length) < 0);
	size = HFS_HEADER_SIZE;
	CHECK(hfs_frame_encode(1, &session, 1, body, 1, 0, frame, &size) == -ENOSPC);
	CHECK(hfs_frame_encode(1, &session, 1, body, HFS_BODY_MAX + 1, 0, frame, &size) < 0);
	CHECK(hfs_frame_encode(1, &session, 1, body, HFS_BODY_MAX, 32, frame, &size) < 0);
	CHECK(hfs_frame_encode(1, &session, 0, body, 1, 0, frame, &size) < 0);
	CHECK(hfs_frame_encode(1, &session, HFS_CELL_MAX + 1ULL, body, 1, 0, frame, &size) < 0);
	CHECK(hfs_frame_encode(1, &session, 1, body, 0, 0, frame, &size) < 0);
	for (i = 0; i < CORPUS_GOLDEN_COUNT; i++) {
		struct hfs_json *parsed;
		const struct golden *g = &corpus_golden[i];
		memcpy(session.b, g->frames[0] + 32, 16);
		CHECK(!hfs_json_parse(g->body, g->body_len, &parsed));
		hfs_json_free(parsed);
		hfs_hash(g->body, g->body_len, computed);
		for (j = 0; j < g->nframes; j++) {
			size = HFS_HEADER_SIZE + HFS_CHUNK_MAX;
			CHECK(!hfs_frame_encode(1, &session, g->cell, g->body, g->body_len, j, frame, &size));
			CHECK(size == g->sizes[j] && !memcmp(frame, g->frames[j], size));
			CHECK(!hfs_frame_decode(frame, size, 1, &session, g->cell, j, &total, hash, &chunk_body, &length));
			CHECK(!memcmp(computed, hash, 32));
		}
	}
	for (i = 0; i < CORPUS_RAW_COUNT; i++) {
		struct hfs_json *parsed;
		CHECK(hfs_json_parse(corpus_raw[i].bytes, corpus_raw[i].len, &parsed) < 0);
	}
	for (i = 0; i < CORPUS_FRAME_COUNT; i++) {
		const struct golden *g = &corpus_golden[0];
		const struct bad_frame *bad = &corpus_frames[i];
		memcpy(session.b, g->frames[0] + 32, 16);
		size = g->sizes[0];
		memcpy(frame, g->frames[0], size);
		offset = bad->offset;
		frame[offset] = bad->byte;
		if (bad->hash_only) {
			CHECK(!hfs_frame_decode(frame, size, 1, &session, g->cell, 0, &total, hash, &chunk_body, &length));
			hfs_hash(chunk_body, length, computed);
			CHECK(memcmp(computed, hash, 32));
		} else {
			CHECK(hfs_frame_decode(frame, size, 1, &session, g->cell, 0, &total, hash, &chunk_body, &length) < 0);
		}
	}
	free(frame);
	free(body);
}

/* Exercise the exact selector consumed by hfs_transport, with literal wire
 * expectations independent of its enums. Ordinary control/release stay on 2.
 */
static void test_response_profiles(void)
{
	static const char * const operations[] = {
		"VIEW_WAIT", "HELLO", "CALL", "QUERY", "CANCEL", "RETIRE",
		"RELEASE_CELL", "RESUME", "VIEW_ACK",
	};
	const char body[] = "{\"cell\":17}";
	uuid_t session = {{0}}, other;
	u8 frame[HFS_HEADER_SIZE + sizeof(body)], hash[32], computed[32];
	const void *bytes;
	u32 i, size, total, length;

	session.b[15] = 12;
	other = session;
	other.b[15]++;
	for (i = 0; i < sizeof(operations) / sizeof(operations[0]); i++) {
		const struct hfs_response_profile response =
			hfs_response_profile(operations[i]);
		u16 expected = i ? 2 : 3;

		CHECK(response.channel == expected);
		CHECK(response.kind == expected);
		size = sizeof(frame);
		CHECK(!hfs_frame_encode(expected, &session, 17, body,
				       sizeof(body) - 1, 0, frame, &size));
		CHECK(!hfs_frame_decode(frame, size, response.kind, &session,
				       17, 0, &total, hash, &bytes, &length));
		CHECK(total == sizeof(body) - 1 && length == total);
		CHECK(!memcmp(bytes, body, length));
		hfs_hash(bytes, length, computed);
		CHECK(!memcmp(hash, computed, sizeof(hash)));
		CHECK(hfs_frame_decode(frame, size, response.kind, &other,
				      17, 0, &total, hash, &bytes, &length) < 0);
		CHECK(hfs_frame_decode(frame, size, response.kind, &session,
				      18, 0, &total, hash, &bytes, &length) < 0);
		/* Cross-channel frames must never satisfy this operation. */
		size = sizeof(frame);
		CHECK(!hfs_frame_encode(i ? 3 : 2, &session, 17, body,
				       sizeof(body) - 1, 0, frame, &size));
		CHECK(hfs_frame_decode(frame, size, response.kind, &session,
				      17, 0, &total, hash, &bytes, &length) < 0);
		/* Decode preserves the selected hash; transport verifies it. */
		size = sizeof(frame);
		CHECK(!hfs_frame_encode(expected, &session, 17, body,
				       sizeof(body) - 1, 0, frame, &size));
		frame[56] ^= 1;
		CHECK(!hfs_frame_decode(frame, size, response.kind, &session,
				       17, 0, &total, hash, &bytes, &length));
		hfs_hash(bytes, length, computed);
		CHECK(memcmp(hash, computed, sizeof(hash)));
	}
	puts("PASS: production response selector: VIEW_WAIT FETCH3/kind3; all ordinary/control/release FETCH2/kind2; cross-kind/session/cell/hash negatives");
}

static void test_cleanup_and_fuzz(void)
{
	struct hfs_json *j;
	struct hfs_buf b;
	char input[128];
	u32 i, k, size, random = 0x12345678;

	for (i = 1; i <= 4; i++) {
		hfs_test_alloc_call = 0;
		hfs_test_alloc_fail = i;
		CHECK(hfs_json_parse("{}", 2, &j) == -ENOMEM && !j);
	}
	hfs_test_alloc_call = 0;
	hfs_test_alloc_fail = 1;
	CHECK(hfs_buf_init(&b, 32) == -ENOMEM);
	hfs_buf_free(&b);
	hfs_test_alloc_fail = 0;
	/* Deterministic arbitrary ASCII and high-bit/truncated input. */
	for (i = 0; i < 10000; i++) {
		size = i % sizeof(input);
		for (k = 0; k < size; k++) {
			random ^= random << 13;
			random ^= random >> 17;
			random ^= random << 5;
			input[k] = random;
		}
		if (!hfs_json_parse(input, size, &j)) {
			CHECK(!hfs_buf_init(&b, sizeof(input)));
			hfs_buf_token(&b, j, 0);
			CHECK(!b.error && b.len == size && !memcmp(b.data, input, size));
			hfs_buf_free(&b);
			hfs_json_free(j);
		}
	}
}

int main(void)
{
	test_json();
	test_bounds();
	test_frames();
	test_response_profiles();
	test_cleanup_and_fuzz();
	puts("PASS: production codec canonical JSON, writer, 1024 groups, bounds, exact golden frames, malformed frames, 32-chunk 1MiB body");
	return 0;
}
