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
#define READ_ONCE(x) (x)
#define smp_rmb() __atomic_thread_fence(__ATOMIC_ACQUIRE)
#define WRITE_ONCE(x, v) ((x) = (v))
struct hfs_session {
	u16 bridge_minor;
	struct { uuid_t session; } enrollment;
	int writer;
	void *client;
	/* oc4: the retry loops stop on blocked unless unmounting; a failed
	 * exchange records its raw ERROR reason in exporter_error.
	 */
	bool blocked;
	bool unmounting;
	bool closing;
	bool transport_failed;
	u16 exporter_error;
	/* 5f4: woken through hfs_session_stop_wake (a counting fixture). */
	int stop_wait;
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
/* 964: FETCHes that find the fixed tag still held; the actual exchange then
 * returns -EBUSY without sending, and hfs_transport must poll again.
 */
static u32 busy_fetches, busy_fetched;
static struct hfs_session *active;
/* oc4: block_at_yield blocks the session during that yield (another worker
 * failing); start_blocked/start_unmounting/start_closing preset the
 * flags and exempt is hfs_transport's exemption argument; a nonzero
 * fetch_reply_len makes the first non-AGAIN FETCH return fetch_reply (an
 * injected ERROR packet) instead of the frame.
 */
static u32 block_at_yield;
static bool start_blocked, start_unmounting, start_closing, exempt;
/* zq3-r13: preset transport_failed; the flag after the last exchange. */
static bool start_failed, last_transport_failed;
/* Review round 2: the exchange is an ALLOCATE (HFS_TRANSPORT_ALLOCATE). */
static bool allocate;
static u8 fetch_reply[4];
static u32 fetch_reply_len;
static u16 last_exporter_error;
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
	if (block_at_yield && yields == block_at_yield)
		active->blocked = true;
	jiffies += expire_sleep ? 30 * HZ : 1;
}

/* 5f4: the actual hfs_transport_failed wakes the view waits through
 * hfs_session_stop_wake (hfs_session.c); counted per exchange.
 */
static u32 stop_wakes;
static void hfs_session_stop_wake(struct hfs_session *session)
{
	CHECK(session == active);
	stop_wakes++;
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
	if (busy_fetched < busy_fetches) {
		busy_fetched++;
		return -EBUSY;
	}
	if (pending_fetches < pending_before_reply) {
		pending_fetches++;
		*(u8 *)packet = 2;
		*len = 1;
		return 0;
	}
	if (fetch_reply_len) {
		memcpy(packet, fetch_reply, fetch_reply_len);
		*len = fetch_reply_len;
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

/* One actual hfs_transport call whose SEND is refused at chunk (3: none).
 * Returns its error; *held is the writer state it reported.
 */
static int exchange(u32 chunk, bool *held)
{
	struct hfs_session session = { .writer = 1, .bridge_minor = transport_minor,
				       .blocked = start_blocked,
				       .unmounting = start_unmounting,
				       .closing = start_closing,
				       .transport_failed = start_failed };
	struct hfs_buf body = { .len = 2 * HFS_CHUNK_MAX + 17 };
	void *reply = NULL;
	u32 reply_len = 0, i;
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
	pending_fetches = progress_signals = busy_fetched = stop_wakes = 0;
	refused = false;
	/* Exercise wrap-safe kernel deadline comparisons as well. */
	jiffies = ULONG_MAX - 100;
	*held = true;
	error = hfs_transport(&session, &body, cell, tag,
		(struct hfs_response_profile){fetch_channel, fetch_kind, HFS_BODY_MAX},
		(exempt ? HFS_TRANSPORT_EXEMPT : 0) | (allocate ? HFS_TRANSPORT_ALLOCATE : 0),
		&reply, &reply_len, held);
	CHECK(*held == !!session.writer);
	CHECK(!error || (!reply && !reply_len));
	if (!error)
		CHECK(reply_len == sizeof(result) - 1 && !memcmp(reply, result, reply_len));
	last_exporter_error = session.exporter_error;
	last_transport_failed = session.transport_failed;
	/* 5f4: an exchange that set transport_failed woke the view waits. */
	CHECK(start_failed || session.transport_failed == (stop_wakes != 0));
	free(reply);
	free(body.data);
	return error;
}

static void run_case(u32 chunk, int expected)
{
	bool held;
	int error = exchange(chunk, &held);

	CHECK(error == expected);
	if (!error) {
		CHECK(!held && sends == 4 && retries == 1 &&
		      yields == 1 + pending_before_reply + busy_fetches);
		CHECK(fetches == 1 + pending_before_reply + busy_fetches && next_chunk == 3);
	} else {
		CHECK(held && !fetches && next_chunk == chunk);
		if (always_refuse)
			CHECK(sends == chunk + 30 * HZ && yields == 30 * HZ);
		else
			CHECK(sends == chunk + 1 && !retries);
	}
}

/* oc4: an injected ERROR reply to the FETCH of a VIEW_WAIT (tag 65534,
 * recall channel 3) or a VIEW_QUERY (reserved tag 65533, result channel 2)
 * fails at once with its reason's errno, records the raw reason, sends no
 * further FETCH and leaves the writer released.
 */
static void fetch_error_case(u16 selected_tag, u16 channel, const u8 *packet, u32 len,
			     int expected, u16 reason)
{
	bool held;
	int error;

	tag = selected_tag;
	fetch_channel = fetch_kind = channel;
	memcpy(fetch_reply, packet, len);
	fetch_reply_len = len;
	pending_before_reply = 2;
	error = exchange(3, &held);
	CHECK(error == expected && !held && last_exporter_error == reason);
	CHECK(sends == 3 && fetches == 3 && yields == 2 && next_chunk == 3);
	fetch_reply_len = 0;
	pending_before_reply = 0;
}

static void fetch_error_cases(void)
{
	static const struct {
		u8 packet[4];
		u32 len;
		int expected;
		u16 reason;
	} cases[] = {
		{ {4, HFS_ERROR_UNAVAILABLE, 0}, 3, -EHOSTDOWN, HFS_ERROR_UNAVAILABLE },
		{ {4, HFS_ERROR_RETIRED_CELL, 0}, 3, -ESTALE, HFS_ERROR_RETIRED_CELL },
		{ {4, HFS_ERROR_INVALID, 0}, 3, -EPROTO, HFS_ERROR_INVALID },
		{ {4, HFS_ERROR_SCOPE, 0}, 3, -EPROTO, HFS_ERROR_SCOPE },
		{ {4, HFS_ERROR_CAPACITY, 0}, 3, -EPROTO, HFS_ERROR_CAPACITY },
		/* High byte set: 0x0105 is not UNAVAILABLE. */
		{ {4, HFS_ERROR_UNAVAILABLE, 1}, 3, -EPROTO, 0x105 },
		/* Malformed ERROR replies record nothing and stay -EPROTO. */
		{ {4, HFS_ERROR_UNAVAILABLE}, 2, -EPROTO, 0 },
		{ {4, HFS_ERROR_UNAVAILABLE, 0, 0}, 4, -EPROTO, 0 },
		{ {1}, 1, -EPROTO, 0 },
		{ {3}, 1, -EPROTO, 0 },
	};
	u32 i;

	for (transport_minor = 0; transport_minor < 4; transport_minor++)
		for (i = 0; i < sizeof(cases) / sizeof(cases[0]); i++) {
			fetch_error_case(65534, 3, cases[i].packet, cases[i].len,
					 cases[i].expected, cases[i].reason);
			fetch_error_case(65533, 2, cases[i].packet, cases[i].len,
					 cases[i].expected, cases[i].reason);
		}
	tag = 65533;
}

/* oc4 (zq3-r12 R12-1): once the session is blocked, neither retry loop
 * sends again; the exchange fails -EHOSTDOWN long before the 30 s deadline,
 * also when it was blocked before the exchange started (review round 1).
 * Exempt traffic (retirement, release, cleanup and the reserved DRAIN_WRITE
 * and GAP_RESOLVE: hfs_after_block_operation)
 * and a session in teardown (unmounting, or closing after a failed mount)
 * keep retrying.
 */
static void blocked_send_cases(void)
{
	bool held;
	u32 chunk;

	transport_minor = 1;
	response[0] = 4; response[1] = HFS_ERROR_CAPACITY; response[2] = 0;
	response_len = 3;
	for (chunk = 0; chunk < 3; chunk++) {
		/* SEND CAPACITY retries stop at the block raised in yield 3. */
		always_refuse = true;
		block_at_yield = 3;
		CHECK(exchange(chunk, &held) == -EHOSTDOWN && held);
		CHECK(sends == chunk + 3 && retries == 2 && yields == 3 && !fetches);
		/* Teardown or exempt: the retries run to the original deadline. */
		start_unmounting = true;
		CHECK(exchange(chunk, &held) == -ETIMEDOUT && held);
		CHECK(sends == chunk + 30 * HZ && yields == 30 * HZ);
		start_unmounting = false;
		start_closing = true;
		CHECK(exchange(chunk, &held) == -ETIMEDOUT && sends == chunk + 30 * HZ);
		start_closing = false;
		exempt = true;
		CHECK(exchange(chunk, &held) == -ETIMEDOUT && sends == chunk + 30 * HZ);
		exempt = false;
		/* Already blocked at the start: one attempt, no retry. */
		block_at_yield = 0;
		start_blocked = true;
		CHECK(exchange(chunk, &held) == -EHOSTDOWN && held);
		CHECK(sends == chunk + 1 && yields == 1 && !fetches);
		start_blocked = always_refuse = false;
	}
}

/* FETCH AGAIN polling, for the VIEW_WAIT and the VIEW_QUERY tag and
 * channel alike.
 */
static void blocked_fetch_cases(void)
{
	bool held;

	pending_before_reply = 1000;
	block_at_yield = 4;
	tag = 65534; fetch_channel = fetch_kind = 3;
	CHECK(exchange(3, &held) == -EHOSTDOWN && !held && fetches == 4 && yields == 4);
	tag = 65533; fetch_channel = fetch_kind = 2;
	CHECK(exchange(3, &held) == -EHOSTDOWN && !held && fetches == 4 && yields == 4);
	/* Teardown or exempt: polling continues to the reply. */
	start_unmounting = true;
	CHECK(exchange(3, &held) == 0 && !held && fetches == 1001 && yields == 1000);
	start_unmounting = false;
	exempt = true;
	CHECK(exchange(3, &held) == 0 && !held && fetches == 1001 && yields == 1000);
	exempt = false;
	block_at_yield = 0;
	start_blocked = true;
	CHECK(exchange(3, &held) == -EHOSTDOWN && !held && fetches == 1 && yields == 1);
	/* Blocked with nothing to retry: the first attempt completes. */
	pending_before_reply = 0;
	CHECK(exchange(3, &held) == 0 && !held && sends == 3 && fetches == 1 && !yields);
	start_blocked = false;
}

/* zq3-r13: m0's umount hung > 270 s in hfs_control after the host was
 * stopped, one deadline per exempt teardown exchange. An exchange that hits
 * its deadline without an answer, or gets ERROR UNAVAILABLE, marks the
 * session transport_failed; later exchanges then stop at their first retry
 * even when exempt and unmounting.
 */
static void transport_failed_cases(void)
{
	static const u8 unavailable[] = {4, HFS_ERROR_UNAVAILABLE, 0};
	bool held;

	transport_minor = 1;
	tag = 65533; fetch_channel = fetch_kind = 2;
	response[0] = 4; response[1] = HFS_ERROR_CAPACITY; response[2] = 0;
	response_len = 3;
	always_refuse = true;
	start_unmounting = start_blocked = exempt = true;
	CHECK(exchange(1, &held) == -ETIMEDOUT && last_transport_failed);
	start_failed = true;
	CHECK(exchange(1, &held) == -EHOSTDOWN && held && sends == 2 && yields == 1);
	always_refuse = false;
	pending_before_reply = 1000;
	CHECK(exchange(3, &held) == -EHOSTDOWN && !held && fetches == 1 && yields == 1);
	pending_before_reply = 0;
	start_failed = start_unmounting = start_blocked = exempt = false;
	/* Retries that reach the deadline in hfs_transport_stop (above) set
	 * it: after host loss a pinned write answers AGAIN forever. Only a
	 * refusal whose own reply crosses the deadline (hfs_send_reply's
	 * expired CAPACITY) does not, and an ALLOCATE's deadline never does
	 * (review round 2: it only makes the allocation uncertain).
	 */
	always_refuse = true;
	expire_reply = true;
	CHECK(exchange(1, &held) == -ETIMEDOUT && !last_transport_failed);
	expire_reply = false;
	allocate = true;
	CHECK(exchange(1, &held) == -ETIMEDOUT && held && !last_transport_failed);
	CHECK(sends == 1 + 30 * HZ && yields == 30 * HZ);
	allocate = always_refuse = false;
	memcpy(fetch_reply, unavailable, sizeof(unavailable));
	fetch_reply_len = sizeof(unavailable);
	CHECK(exchange(3, &held) == -EHOSTDOWN && last_transport_failed);
	fetch_reply_len = 0;
	fetch_reply[1] = HFS_ERROR_INVALID;
	fetch_reply_len = sizeof(unavailable);
	CHECK(exchange(3, &held) == -EPROTO && !last_transport_failed);
	fetch_reply_len = 0;
	/* R13-1: no reply within the bounded 9p wait (net/9p -ETIMEDOUT). */
	exchange_error = -ETIMEDOUT;
	CHECK(exchange(1, &held) == -ETIMEDOUT && held && last_transport_failed);
	exchange_error = 0;
}

static void blocked_cases(void)
{
	blocked_send_cases();
	blocked_fetch_cases();
	transport_failed_cases();
	CHECK(!hfs_transport_halt(false, false, false) && !hfs_transport_halt(false, false, true));
	CHECK(hfs_transport_halt(false, true, false) == -EHOSTDOWN);
	CHECK(!hfs_transport_halt(false, true, true) && !hfs_transport_halt(true, true, false));
	CHECK(!hfs_transport_halt(true, true, true));
	CHECK(hfs_after_block_operation("VIEW_RELEASE") && hfs_after_block_operation("FORGET") &&
	      hfs_after_block_operation("SESSION_RETIRE") && hfs_after_block_operation("RETIRE"));
	CHECK(!hfs_after_block_operation("VIEW_QUERY") && !hfs_after_block_operation("VIEW_WAIT") &&
	      !hfs_after_block_operation("QUERY") && !hfs_after_block_operation("CALL") &&
	      !hfs_after_block_operation("DIRTY_FREEZE"));
	/* dh4: reserved drain and reconciliation work keeps running on a
	 * blocked session; ordinary CALL opcodes and ALLOCATE still halt.
	 */
	CHECK(hfs_after_block_operation("DRAIN_WRITE") &&
	      hfs_after_block_operation("GAP_RESOLVE"));
	CHECK(!hfs_after_block_operation("ROOT") && !hfs_after_block_operation("WRITE") &&
	      !hfs_after_block_operation("ALLOCATE"));
}

int main(void)
{
	static const u8 bad[][4] = {
		{2}, {3}, {4, 1, 0}, {4, 2, 0}, {4, 6, 0},
		{4, 3}, {4, 3}, {4, 3, 1}, {1, 0}, {4, 3, 0, 0}, {0}, {0}, {255},
		{4, 5}, {4, 5, 1},
	};
	static const u32 lengths[] = {1, 1, 3, 3, 3, 1, 2, 3, 2, 4, 0, 1, 1, 2, 3};
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
		/* 964: a busy fixed tag (nothing sent) at SEND retries the exact
		 * frame of this chunk and cell; at FETCH it polls again.
		 */
		exchange_error = -EBUSY;
		busy_fetches = 2;
		run_case(chunk, 0);
		busy_fetches = 0;
		exchange_error = 0;
		for (i = 0; i < sizeof(bad) / sizeof(bad[0]); i++) {
			memcpy(response, bad[i], sizeof(response));
			response_len = lengths[i];
			CHECK(hfs_send_reply(response, response_len, false) == -EPROTO);
			CHECK(hfs_send_reply(response, response_len, true) == -EPROTO);
			run_case(chunk, -EPROTO);
		}
		/* oc4: a SEND ERROR other than CAPACITY maps its reason
		 * (UNAVAILABLE -EHOSTDOWN, RETIRED_CELL -ESTALE) and records it.
		 */
		response[0] = 4; response[1] = HFS_ERROR_UNAVAILABLE; response[2] = 0;
		response_len = 3;
		CHECK(hfs_send_reply(response, 3, false) == -EHOSTDOWN);
		run_case(chunk, -EHOSTDOWN);
		CHECK(last_exporter_error == HFS_ERROR_UNAVAILABLE);
		response[1] = HFS_ERROR_RETIRED_CELL;
		CHECK(hfs_send_reply(response, 3, true) == -ESTALE);
		run_case(chunk, -ESTALE);
		CHECK(last_exporter_error == HFS_ERROR_RETIRED_CELL);
		response[1] = HFS_ERROR_CAPACITY;
		run_case(chunk, 0);
		CHECK(!last_exporter_error);
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
	fetch_error_cases();
	blocked_cases();
	puts("PASS: oc4 FETCH ERROR on VIEW_WAIT/VIEW_QUERY maps UNAVAILABLE -EHOSTDOWN, RETIRED_CELL -ESTALE, others -EPROTO and records the raw reason; SEND ERROR likewise; a blocked session stops SEND and FETCH retries (not exempt or teardown traffic, unless the host stopped answering: zq3-r13)");
	puts("PASS: actual hfs_transport + policy/codec; chunk0/1/last exact frame/session/cell/tag/body retry; no advance/rebind; yielding writer/control progress; fixed/wrapped deadline at reply/sleep/relock/repeated refusal; malformed status/code/length and I/O errors; busy fixed tag retried at SEND/FETCH (964); result2/recall3");
	return 0;
}
