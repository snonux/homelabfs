/* SPDX-License-Identifier: GPL-2.0-only */
/* Substituted virtio transport, owners and scenarios for the fixed-tag test. */

static struct p9_client client;
static pthread_mutex_t queue_lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t queue_cond = PTHREAD_COND_INITIALIZER;
static struct p9_req_t *queue[64];
static unsigned int queue_head, queue_count, in_flight[65536];
static unsigned long sent_requests, next_cell = 1;
static bool receiver_stopping;

/* Like a preempted vCPU between wake_up and the callback's p9_req_put, keep
 * the callback's reference until some owner probes a tag after this wake-up
 * (idr_alloc or idr_find), or 50 ms pass when no further exchange follows.
 */
static void wake_up(wait_queue_head_t *q)
{
	unsigned long probes = __atomic_load_n(&tag_probes, __ATOMIC_SEQ_CST);
	unsigned long deadline = now_ms() + 50;

	CHECK(!pthread_mutex_lock(&q->lock));
	CHECK(!pthread_cond_broadcast(&q->cond));
	CHECK(!pthread_mutex_unlock(&q->lock));
	while (__atomic_load_n(&tag_probes, __ATOMIC_SEQ_CST) == probes &&
	       !time_after_eq(now_ms(), deadline))
		usleep(100);
}

static int fake_request(struct p9_client *c, struct p9_req_t *req)
{
	WRITE_ONCE(req->status, REQ_STATUS_SENT);
	CHECK(!pthread_mutex_lock(&queue_lock));
	CHECK(queue_count < 64);
	/* A fixed tag may be in flight at most once (the wire tag identifies it). */
	if (req->tc.tag >= 65533)
		CHECK(++in_flight[req->tc.tag] == 1);
	queue[(queue_head + queue_count++) % 64] = req;
	CHECK(!pthread_cond_signal(&queue_cond));
	CHECK(!pthread_mutex_unlock(&queue_lock));
	__atomic_add_fetch(&sent_requests, 1, __ATOMIC_SEQ_CST);
	return 0;
}

static int fake_cancel(struct p9_client *c, struct p9_req_t *req) { return 0; }
static struct p9_trans_module fake_virtio = { false, fake_request, fake_cancel, fake_cancel };

/* Echo the cell so each owner proves it read its own reply. */
static void fake_reply(struct p9_req_t *req)
{
	u8 method = req->tc.sdata[7], ok = 1, result = 3, type = P9_RBRIDGE;
	u16 tag = req->tc.tag;
	u64 cell;

	CHECK(req->tc.size >= 7 + 2 + 8 + 4);
	memcpy(&cell, req->tc.sdata + 9, 8);
	spin_lock_irq(&client.lock);
	CHECK(client.reqs.slots[tag] == req);
	spin_unlock_irq(&client.lock);
	p9pdu_reset(&req->rc);
	CHECK(!pdu_put(&req->rc, "\0\0\0\0", 4) && !pdu_put(&req->rc, &type, 1) &&
	      !pdu_put(&req->rc, &tag, 2));
	if (method == 1)
		CHECK(!pdu_put(&req->rc, &ok, 1));
	else
		CHECK(!pdu_put(&req->rc, &result, 1) && !pdu_put(&req->rc, &cell, 8));
	memcpy(req->rc.sdata, &req->rc.size, 4);
}

static void *receive_thread(void *unused)
{
	struct p9_req_t *req;

	for (;;) {
		CHECK(!pthread_mutex_lock(&queue_lock));
		while (!queue_count && !receiver_stopping)
			CHECK(!pthread_cond_wait(&queue_cond, &queue_lock));
		if (!queue_count) {
			CHECK(!pthread_mutex_unlock(&queue_lock));
			return NULL;
		}
		req = queue[queue_head];
		queue_head = (queue_head + 1) % 64;
		queue_count--;
		if (req->tc.tag >= 65533)
			in_flight[req->tc.tag]--;
		CHECK(!pthread_mutex_unlock(&queue_lock));
		fake_reply(req);
		p9_client_cb(&client, req, REQ_STATUS_RCVD); /* actual callback */
	}
}

/* One hfs_transport round: SEND chunk 0, then FETCH its reply at once. */
static int round_trip(u16 tag)
{
	u64 cell = __atomic_fetch_add(&next_cell, 1, __ATOMIC_SEQ_CST), echoed;
	u8 frame[89] = { 0 }, reply[64];
	u32 len = sizeof(reply);
	int err;

	err = p9_client_hfs_exchange(&client, tag, 1, 1, cell, 0, frame, sizeof(frame),
				     reply, &len);
	if (err)
		return err;
	if (len != 1 || reply[0] != 1)
		return -EPROTO;
	len = sizeof(reply);
	err = p9_client_hfs_exchange(&client, tag, 2, 2, cell, 0, NULL, 0, reply, &len);
	if (err)
		return err;
	memcpy(&echoed, reply + 1, 8);
	return len == 9 && reply[0] == 3 && echoed == cell ? 0 : -EPROTO;
}

struct owner {
	const char *name;
	u16 tag;
	int rounds, pause_us;
	int failed_round, error;
};

/* Stops at the first error: in hfs_control any transport error is a fault
 * that retains the candidate and marks the session blocked.
 */
static void *owner_thread(void *argument)
{
	struct owner *owner = argument;
	int round;

	for (round = 0; round < owner->rounds; round++) {
		owner->error = round_trip(owner->tag);
		if (owner->error) {
			owner->failed_round = round;
			break;
		}
		if (owner->pause_us)
			usleep(owner->pause_us);
	}
	return NULL;
}

/* All requests are freed and every tag slot is empty once the receive
 * thread has dropped its last delayed reference.
 */
static bool quiescent(void)
{
	unsigned long deadline = now_ms() + 1000;
	bool empty;
	int tag;

	while (__atomic_load_n(&live_objects, __ATOMIC_SEQ_CST) &&
	       !time_after_eq(now_ms(), deadline))
		usleep(1000);
	spin_lock_irq(&client.lock);
	for (empty = true, tag = 0; tag < 65536; tag++)
		empty &= !client.reqs.slots[tag];
	spin_unlock_irq(&client.lock);
	return empty && !__atomic_load_n(&live_objects, __ATOMIC_SEQ_CST);
}

static int report(const char *scenario, struct owner *owners, int count)
{
	int i, failures = 0;

	for (i = 0; i < count; i++) {
		if (!owners[i].error)
			continue;
		failures++;
		printf("FAIL %s: %s round %d returned %d%s\n", scenario, owners[i].name,
		       owners[i].failed_round, owners[i].error,
		       owners[i].error == -ENOMEM ?
			       " (ENOMEM: fixed tag still held; hfs_control blocks the session)" : "");
	}
	if (!quiescent()) {
		failures++;
		printf("FAIL %s: requests or tag slots remain\n", scenario);
	}
	if (!failures)
		printf("PASS %s\n", scenario);
	return failures;
}

/* zq3-r5: the release loop polls VIEW_QUERY on tag 65533 while the event
 * worker sends STOPPED, INSTALLED_DRAIN and DRAINED on the same tag.
 */
static int two_owners(void)
{
	struct owner owners[] = {
		{ "VIEW_QUERY loop", 65533, 20, 1000, 0, 0 },
		{ "VIEW_ACK worker", 65533, 3, 2000, 0, 0 },
	};
	pthread_t threads[2];
	int i;

	for (i = 0; i < 2; i++)
		CHECK(!pthread_create(&threads[i], NULL, owner_thread, &owners[i]));
	for (i = 0; i < 2; i++)
		CHECK(!pthread_join(threads[i], NULL));
	return report("two-owners-tag65533", owners, 2);
}

/* The VIEW_WAIT poll worker reposts at once on tag 65534: the same race with a
 * single owner, between one exchange and the next.
 */
static int back_to_back(void)
{
	struct owner owner = { "VIEW_WAIT poll", 65534, 10, 0, 0, 0 };

	owner_thread(&owner);
	return report("back-to-back-tag65534", &owner, 1);
}

/* A reference that never drops must not hang the owner: the bounded wait
 * returns -EBUSY after about one second, and nothing is sent.
 */
static int stuck_tag(void)
{
	static struct p9_req_t stuck;
	unsigned long before = sent_requests, start = now_ms(), elapsed;
	u8 frame[89] = { 0 }, reply[64];
	u32 len = sizeof(reply);
	int err;

	spin_lock_irq(&client.lock);
	client.reqs.slots[65533] = &stuck;
	spin_unlock_irq(&client.lock);
	err = p9_client_hfs_exchange(&client, 65533, 1, 1, 1, 0, frame, sizeof(frame), reply, &len);
	elapsed = now_ms() - start;
	spin_lock_irq(&client.lock);
	client.reqs.slots[65533] = NULL;
	spin_unlock_irq(&client.lock);
	if (err == -EBUSY && elapsed >= 1000 && elapsed < 5000 && sent_requests == before) {
		printf("PASS stuck-tag-bounded-ebusy elapsed_ms=%lu\n", elapsed);
		return 0;
	}
	printf("FAIL stuck-tag-bounded-ebusy: returned %d after %lu ms, sent %lu\n", err,
	       elapsed, sent_requests - before);
	return 1;
}

static void receiver_stop(pthread_t receiver)
{
	CHECK(!pthread_mutex_lock(&queue_lock));
	receiver_stopping = true;
	CHECK(!pthread_cond_signal(&queue_cond));
	CHECK(!pthread_mutex_unlock(&queue_lock));
	CHECK(!pthread_join(receiver, NULL));
}

int main(void)
{
	pthread_t receiver;
	int failures = 0;

	CHECK(!pthread_mutex_init(&client.lock, NULL));
	CHECK(!pthread_mutex_init(&client.hfs_control_tag.native, NULL));
	CHECK(!pthread_mutex_init(&client.hfs_wait_tag.native, NULL));
	client.msize = 65536;
	client.proto_version = p9_proto_hfs14;
	client.trans_mod = &fake_virtio;
	client.status = Connected;
	CHECK(!pthread_create(&receiver, NULL, receive_thread, NULL));
	failures += two_owners();
	failures += back_to_back();
	failures += stuck_tag();
	receiver_stop(receiver);
	printf("fixed_tag cases=3 failures=%d requests=%lu\n", failures, sent_requests);
	return failures ? 1 : 0;
}
