// SPDX-License-Identifier: GPL-2.0-only
/* Real transport loop; only kernel scheduling and p9 I/O are substituted. */
#include "hfs_policy.h"

unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#define CHECK(e) do { if (!(e)) { fprintf(stderr, "%d: %s\n", __LINE__, #e); exit(1); } } while (0)
#define HZ 100UL
#define msecs_to_jiffies(ms) (((ms) * HZ + 999) / 1000)
#define P9_NOTAG 65535
#define time_after_eq(a, b) ((long)((a) - (b)) >= 0)
#define kmalloc kvmalloc
struct hfs_session {
	u16 bridge_minor;
	struct { uuid_t session; } enrollment;
	int writer;
	void *client;
};
static unsigned long jiffies;
static u16 transport_minor;
static u32 refused_chunk, sends, fetches, yields, retries, next_chunk;
static u32 saved_len;
static u8 saved[HFS_HEADER_SIZE + HFS_CHUNK_MAX];
static u8 response[4];
static u32 response_len;
static int exchange_error;
static bool refused, expire_reply, expire_sleep, expire_lock, always_refuse;
static u16 fetch_channel, fetch_kind;
static const u64 cell = 0x12345678;
static u16 tag = 65533;
static u32 pending_before_reply, pending_fetches, progress_signals;
static struct hfs_session *active;
static const struct hfs_buf *command;
static const char result[] = "{\"reply\":true}";

static void mutex_unlock(int *lock)
{
	CHECK(*lock == 1);
	*lock = 0;
}

static void mutex_lock(int *lock)
{
	CHECK(*lock == 0);
	*lock = 1;
	if (expire_lock)
		jiffies += 30 * HZ;
}

static void usleep_range(unsigned int low, unsigned int high)
{
	CHECK(low == 10000 && high == 12000 && !active->writer);
	/* Models a reserved control exchange progressing while writer is yielded. */
	yields++;
	jiffies += expire_sleep ? 30 * HZ : 1;
}

static void hfs_mmap_progress(struct hfs_session *session)
{
	CHECK(session == active && !active->writer);
	progress_signals++; /* actual pressure executor is a separate component */
}

static int p9_client_hfs_exchange(void *client, u16 selected_tag, u8 method,
				u8 channel, u64 selected_cell, u32 chunk,
				const void *frame, u32 frame_len,
				void *packet, u32 *len)
{
	u32 total, part;
	u8 hash[32];
	const void *bytes;

	CHECK(client == active && selected_tag == tag && selected_cell == cell);
	if (method == 1) {
		CHECK(channel == 1 && active->writer && chunk == next_chunk);
		sends++;
		CHECK(!hfs_frame_decode_version(active->bridge_minor, frame, frame_len, 1,
			&active->enrollment.session, cell, chunk, &total, hash, &bytes, &part));
		CHECK(total == command->len);
		CHECK(!memcmp(bytes, command->data + chunk * HFS_CHUNK_MAX, part));
		if (refused) {
			CHECK(frame_len == saved_len && !memcmp(frame, saved, saved_len));
			retries++;
		}
		if (chunk == refused_chunk && (!refused || always_refuse)) {
			memcpy(saved, frame, frame_len);
			saved_len = frame_len;
			refused = true;
			if (expire_reply)
				jiffies += 30 * HZ;
			memcpy(packet, response, response_len);
			*len = response_len;
			return exchange_error;
		}
		refused = false;
		next_chunk++;
		*(u8 *)packet = 1;
		*len = 1;
		return 0;
	}
	CHECK(method == 2 && channel == fetch_channel && !active->writer);
	CHECK(next_chunk == 3 && !frame && !frame_len && chunk == 0);
	fetches++;
	if (pending_fetches < pending_before_reply) {
		pending_fetches++;
		*(u8 *)packet = 2;
		*len = 1;
		return 0;
	}
	*(u8 *)packet = 3;
	part = *len - 1;
	CHECK(!hfs_frame_encode_version(active->bridge_minor, fetch_kind, &active->enrollment.session, cell,
		result, sizeof(result) - 1, 0, (u8 *)packet + 1, &part));
	*len = part + 1;
	return 0;
}

#include "transport.inc"

static void run_case(u32 chunk, int expected)
{
	struct hfs_session session = { .writer = 1, .bridge_minor = transport_minor };
	struct hfs_buf body = { .len = 2 * HFS_CHUNK_MAX + 17 };
	void *reply = NULL;
	u32 reply_len = 0, i;
	bool held = true;
	int error;

	body.data = malloc(body.len);
	CHECK(body.data);
	for (i = 0; i < body.len; i++)
		body.data[i] = (char)(i * 17 + i / 251);
	session.enrollment.session.b[15] = 7;
	session.client = &session;
	active = &session;
	command = &body;
	refused_chunk = chunk;
	sends = fetches = yields = retries = next_chunk = saved_len = 0;
	pending_fetches = progress_signals = 0;
	refused = false;
	/* Exercise wrap-safe kernel deadline comparisons as well. */
	jiffies = ULONG_MAX - 100;
	error = hfs_transport(&session, &body, cell, tag,
		(struct hfs_response_profile){fetch_channel, fetch_kind, HFS_BODY_MAX},
		&reply, &reply_len, &held);
	CHECK(error == expected);
	CHECK(held == !!session.writer);
	if (!error) {
		CHECK(!held && sends == 4 && retries == 1 && yields == 1 + pending_before_reply);
		CHECK(fetches == 1 + pending_before_reply && next_chunk == 3);
		CHECK(reply_len == sizeof(result) - 1 && !memcmp(reply, result, reply_len));
	} else {
		CHECK(held && !reply && !reply_len && !fetches && next_chunk == chunk);
		if (always_refuse)
			CHECK(sends == chunk + 30 * HZ && yields == 30 * HZ);
		else
			CHECK(sends == chunk + 1 && !retries);
	}
	free(reply);
	free(body.data);
}

int main(void)
{
	static const u8 bad[][4] = {
		{2}, {3}, {4, 1, 0}, {4, 2, 0}, {4, 4, 0},
		{4, 3}, {4, 3}, {4, 3, 1}, {1, 0}, {4, 3, 0, 0}, {0}, {0}, {255},
	};
	static const u32 lengths[] = {1, 1, 3, 3, 3, 1, 2, 3, 2, 4, 0, 1, 1};
	u32 chunk, i;

	CHECK(hfs_send_reply(NULL, 0, false) == -EPROTO);
	for (transport_minor = 0; transport_minor < 4; transport_minor++)
	for (chunk = 0; chunk < 3; chunk++) {
		response[0] = 4; response[1] = 3; response[2] = 0;
		response_len = 3;
		fetch_channel = 2; fetch_kind = 2;
		run_case(chunk, 0);
		fetch_channel = 3; fetch_kind = 3;
		run_case(chunk, 0);
		expire_reply = true;
		run_case(chunk, -ETIMEDOUT);
		expire_reply = false;
		expire_sleep = true;
		run_case(chunk, -ETIMEDOUT);
		expire_sleep = false;
		expire_lock = true;
		run_case(chunk, -ETIMEDOUT);
		expire_lock = false;
		always_refuse = true;
		run_case(chunk, -ETIMEDOUT);
		always_refuse = false;
		exchange_error = -EIO;
		run_case(chunk, -EIO);
		exchange_error = 0;
		for (i = 0; i < sizeof(bad) / sizeof(bad[0]); i++) {
			memcpy(response, bad[i], sizeof(response));
			response_len = lengths[i];
			CHECK(hfs_send_reply(response, response_len, false) == -EPROTO);
			CHECK(hfs_send_reply(response, response_len, true) == -EPROTO);
			run_case(chunk, -EPROTO);
		}
	}
	response[0] = 1;
	CHECK(!hfs_send_reply(response, 1, true));
	response[0] = 4; response[1] = 3; response[2] = 0;
	CHECK(hfs_send_reply(response, 1, false) == -EPROTO);
	CHECK(hfs_send_reply(response, 2, false) == -EPROTO);
	/* Pending ordinary work pulses pressure outside writer ownership. A
	 * reserved VIEW_WAIT never drives cache eviction merely by long-polling.
	 */
	response_len = 3;
	pending_before_reply = 10;
	transport_minor = 1;
	tag = P9_NOTAG;
	fetch_channel = 2; fetch_kind = 2;
	run_case(1, 0);
	CHECK(progress_signals > 0);
	tag = 65534;
	fetch_channel = 3; fetch_kind = 3;
	run_case(0, 0);
	CHECK(progress_signals == 0);
	transport_minor = 0;
	tag = P9_NOTAG;
	fetch_channel = 2; fetch_kind = 2;
	run_case(2, 0);
	CHECK(progress_signals == 0);
	puts("PASS: actual hfs_transport + policy/codec; chunk0/1/last exact frame/session/cell/tag/body retry; no advance/rebind; yielding writer/control progress; fixed/wrapped deadline at reply/sleep/relock/repeated refusal; malformed status/code/length and I/O errors; result2/recall3");
	return 0;
}
