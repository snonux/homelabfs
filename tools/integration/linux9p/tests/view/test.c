unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
/* SPDX-License-Identifier: GPL-2.0-only */
#include "production.c"

static struct page *dirty_pte;

static void unmap_mapping_range(struct address_space *mapping,
                               int start, int length, int cows)
{
   assert(mapping && !start && !length && !cows);
   assert(observed_view->phase == HFS_VIEW_RECALLING);
   assert(observed_view->active == 0);
   if (dirty_pte) {
      assert(!dirty_pte->dirty);
      assert(set_page_dirty(dirty_pte)); /* actual dirty-PTE removal leaf path */
      dirty_pte = NULL;
   }
   atomic_fetch_add(&unmaps, 1);
}

static int reserve(void *s, u64 bytes, bool control, void **out)
{
   assert(s == test_session && bytes > 0);
   assert(control || bytes >= PAGE_SIZE);
   *out = malloc(1);
   if (!*out) return -ENOMEM;
   atomic_fetch_add(&reservations, 1);
   return 0;
}

static void unreserve(void *s, void *reservation)
{
   assert(s == test_session && reservation);
   atomic_fetch_sub(&reservations, 1);
   free(reservation);
}

struct stop_args { struct hfs_view *view; uuid_t generation, recall; int result; };
static void *stop_thread(void *p)
{
   struct stop_args *a = p;
   a->result = hfs_view_stop(a->view, &a->generation, &a->recall, 1);
   return NULL;
}

int main(void)
{
   struct hfs_provider provider = {reserve, unreserve};
   struct hfs_session session = {.provider = &provider};
   struct hfs_view_stamp stamp, bad;
   uuid_t generation = {{1}}, recall = {{2}}, zero = {{0}};
   struct address_space mapping = {.id = 1, .a_ops = &hfs_cache_aops};
   struct hfs_inode object = {.inode = {&mapping, 1}};
   void *cookie;
   pthread_t thread;

   test_session = &session;
   mutex_init(&session.lock);
   INIT_LIST_HEAD(&object.view_list);
   assert(hfs_view_new(&session, &zero) == -EINVAL);
   assert(hfs_view_new(&session, &generation) == 0);
   observed_view = session.view;
   assert(hfs_view_seal(session.view) == -ESTALE);
   assert(hfs_view_new(&session, &generation) == -EINVAL);
   assert(hfs_view_register_inode(&object.inode) == 0);
   assert(hfs_view_snapshot(session.view, &stamp) == 0);
   struct hfs_cache_page *cached;
   struct folio folio = {.page = {.locked = true, .uptodate = true, .refs = 1}};
   struct hfs_cap description_cap = {.token = {1}};
   struct hfs_description description = {.cap = &description_cap};
   struct file file = {&object.inode, &description};
   struct vm_area_struct vma = {&file};
   struct hfs_lease lease = {.visibility = generation, .description = {1}};
   assert(hfs_view_page_prepare(session.view, &stamp, &cached) == 0);
   cached->lease = &lease; /* physical gate fixture; no source authority claim */
   assert(hfs_view_page_attach(cached, &folio.page) == 0);
   assert(folio.page.marker && folio.page.private == cached && folio.page.refs == 2);
   /* Keep creator ref to exercise an immutable delayed RPC after eviction. */
   assert(!hfs_cache_aops.release_folio(&folio, 0));
   assert(hfs_cache_aops.migrate_folio(&mapping, &folio, &folio, MIGRATE_ASYNC) == -EBUSY);
   assert(hfs_view_page_enter(&vma, &folio.page, true, &cookie) == -ESTALE);
   assert(hfs_view_page_enter(&vma, &folio.page, false, &cookie) == 0);
   hfs_view_leave(cookie);
   /* A legal inherited file retains this exact description. A different
    * description cannot borrow its cached page/PTE authorization.
    */
   struct file inherited = file;
   struct vm_area_struct child_vma = {&inherited};
   assert(hfs_view_page_enter(&child_vma, &folio.page, false, &cookie) == 0);
   hfs_view_leave(cookie);
   struct hfs_cap unrelated_cap = {.token = {3}};
   struct hfs_description unrelated = {.cap = &unrelated_cap};
   inherited.private_data = &unrelated;
   assert(hfs_view_page_enter(&child_vma, &folio.page, false, &cookie) == -ESTALE);
   struct hfs_view_ticket alternate_release = {.control = true};
   struct hfs_lease alternate_lease = {.description = {3}, .visibility = generation,
       .release_ticket = &alternate_release};
   struct hfs_cache_grant alternate_grant = {.lease = &alternate_lease};
   INIT_LIST_HEAD(&alternate_grant.list);
   assert(hfs_view_page_grant(cached, &alternate_grant, NULL) == 0);
   assert(session.view->nr_grants == 1);
   assert(hfs_view_page_enter(&child_vma, &folio.page, false, &cookie) == 0);
   hfs_view_leave(cookie);
   assert(alternate_lease.published);
   assert(hfs_view_page_enter(&child_vma, &folio.page, true, &cookie) == -ESTALE);

   bad = stamp; bad.revision++;
   assert(hfs_view_enter(session.view, &bad, false, &cookie) == -ESTALE);
   assert(cookie == NULL && module_refs == 1);
   assert(hfs_view_enter(session.view, &stamp, false, &cookie) == 0);
   assert(module_refs == 2);
   assert(hfs_view_free(&session) == -EBUSY);
   folio.page.pinned = true;
   struct stop_args args = {session.view, generation, recall, -1};
   assert(pthread_create(&thread, NULL, stop_thread, &args) == 0);
   while (READ_ONCE(session.view->phase) == HFS_VIEW_LIVE) usleep(100);
   assert(unmaps == 0 && session.view->phase == HFS_VIEW_RECALLING);
   void *second;
   assert(hfs_view_enter(session.view, NULL, false, &second) == -EAGAIN);
   assert(hfs_view_snapshot(session.view, &bad) == -EAGAIN);
   hfs_view_leave(cookie);
   assert(pthread_join(thread, NULL) == 0);
   assert(args.result == -EBUSY && session.view->phase == HFS_VIEW_RECALLING);
   assert(unmaps == 1 && module_refs == 1 && object.inode.refs == 1);
   /* Actual reservation adoption rejects a real late RPC result while STOP
    * owns the epoch. Resources are boundary fixtures, not host authority.
    */
   struct hfs_view_ticket reserve_ticket = {0}, freeze_ticket = {.control = true};
   struct hfs_request drain = {.reservation = &drain, .earmarked = true, .control = true};
   struct hfs_json reservation = {0};
   struct hfs_dirty delayed = {.reservation = &reservation,
       .reserve_ticket = &reserve_ticket, .freeze_ticket = &freeze_ticket,
       .request = &drain, .credit = &delayed, .length = 4096};
   lease.dirty = true;
   assert(hfs_view_adopt_dirty(session.view, cached, &delayed) == -ESTALE);
   assert(!delayed.adopted && !cached->dirty && !session.view->nr_dirty);
   assert(hfs_view_stop(session.view, &generation, &zero, 1) == -EINVAL);
   folio.page.pinned = false;
   assert(hfs_view_stop(session.view, &generation, &recall, 1) == 0);
   assert(session.view->phase == HFS_VIEW_QUIESCENT && unmaps == 2);
   assert(lease.revoked && alternate_lease.revoked);
   assert(hfs_cache_aops.release_folio(&folio, 0));
   assert(!folio.page.private && folio.page.marker && !folio.page.uptodate);
   assert(folio.page.refs == 1 && session.view->nr_cached == 1);
   hfs_cache_aops.free_folio(&folio);
   assert(!folio.page.marker && module_refs == 1);
   assert(hfs_view_stop(session.view, &generation, &recall, 1) == 0);
   assert(unmaps == 2); /* Exact already-quiescent replay does not zap again. */
   assert(hfs_view_seal(session.view) == 0);
   assert(session.view->phase == HFS_VIEW_STOPPED);
   const char empty_manifest[] =
       "{\"entries\":[],\"hash\":\"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945\"}";
   assert(session.view->manifest.len == sizeof(empty_manifest) - 1);
   assert(!memcmp(session.view->manifest.data, empty_manifest, sizeof(empty_manifest) - 1));
   assert(hfs_view_seal(session.view) == 0);
   assert(hfs_view_register_inode(&object.inode) == 0); /* idempotent existing registry */
   assert(session.view->nr_mappings == 1);
   struct hfs_inode another = {.inode = {&mapping, 1}};
   INIT_LIST_HEAD(&another.view_list);
   assert(hfs_view_register_inode(&another.inode) == -EAGAIN);
   assert(hfs_view_reopen(session.view, &recall, 1) == -ESTALE);
   /* Final target installation is a control-plane fixture in this primitive
    * test. The actual epoch transition/adoption/PTE gates are production C.
    */
   hfs_buf_literal(&session.view->target, "{}");
   session.view->phase = HFS_VIEW_FINAL;
   assert(hfs_view_reopen(session.view, &zero, 2) == -ESTALE);
   assert(hfs_view_reopen(session.view, &recall, 1) == -ESTALE);
   assert(hfs_view_reopen(session.view, &recall, 2) == 0);
   assert(hfs_view_reopen(session.view, &recall, 2) == 0);
   assert(session.view->revision == stamp.revision + 1);
   assert(hfs_view_adopt_dirty(session.view, cached, &delayed) == -ESTALE);
   assert(!delayed.adopted && !session.view->nr_dirty);
   assert(hfs_view_enter(session.view, &stamp, false, &cookie) == -ESTALE);
   assert(hfs_view_page_enter(&vma, &folio.page, true, &cookie) == -ESTALE);
   hfs_view_page_put(cached);
   assert(module_refs == 0);
   /* Cancellation and adoption share the ticket admission lock. A locally
    * closed never-adopted reservation cannot cross the final registry gate.
    */
   assert(hfs_view_snapshot(session.view, &stamp) == 0);
   assert(hfs_view_page_prepare(session.view, &stamp, &cached) == 0);
   struct hfs_lease fresh_lease = {.visibility = generation,
       .description = {1}, .dirty = true};
   cached->lease = &fresh_lease;
   spin_lock_init(&reserve_ticket.admission_lock);
   reserve_ticket.closed = true;
   assert(hfs_view_adopt_dirty(session.view, cached, &delayed) == -ECANCELED);
   assert(!delayed.adopted && !reserve_ticket.published && !session.view->nr_dirty);
   reserve_ticket.closed = false;
   assert(hfs_view_adopt_dirty(session.view, cached, &delayed) == -EINVAL);
   assert(!reserve_ticket.published); /* no page/slot, even with valid epoch */
   assert(hfs_view_dirty_slot(cached) == 0);
   struct folio dirty_folio = {.page = {.locked = true, .refs = 1}, .mapping = &mapping};
   assert(hfs_view_page_attach(cached, &dirty_folio.page) == 0);
   assert(hfs_view_adopt_dirty(session.view, cached, &delayed) == 0);
   assert(delayed.adopted && reserve_ticket.published && cached->dirty == &delayed);
   assert(session.view->nr_dirty == 1 && session.view->nr_dirty_packages == 1);
   assert(hfs_view_page_enter(&vma, &dirty_folio.page, true, &cookie) == 0);
   hfs_view_leave(cookie);
   assert(hfs_cache_aops.dirty_folio == noop_dirty_folio);
   assert(set_page_dirty(&dirty_folio.page));
   assert(!set_page_dirty(&dirty_folio.page));
   assert(hfs_cache_aops.launder_folio(&dirty_folio) == -EBUSY);
   assert(dirty_folio.page.dirty);
   /* Hardware PTE can redirty during zap after a bit-clear; the original
    * adopted reservation still owns every byte across RECALLING. */
   assert(clear_page_dirty_for_io(&dirty_folio.page));
   dirty_pte = &dirty_folio.page;
   assert(hfs_view_stop(session.view, &generation, &recall, 3) == 0);
   assert(hfs_cache_aops.launder_folio(&dirty_folio) == -EBUSY);
   /* The separate freeze/codec check exercises real DIRTY_FREEZE. Here its
    * successfully protected input is a primitive boundary fixture, so these
    * assertions establish only the actual stopped detachment/lifetime code.
    */
   delayed.captured = delayed.frozen = true;
   session.view->phase = HFS_VIEW_STOPPED;
   assert(hfs_cache_aops.launder_folio(&dirty_folio) == 0);
   assert(!dirty_folio.page.dirty && !delayed.resolved && cached->dirty == &delayed);
   assert(hfs_cache_aops.release_folio(&dirty_folio, 0));
   assert(!dirty_folio.page.private && dirty_folio.page.marker && !cached->page);
   assert(session.view->nr_dirty == 1 && delayed.adopted && !delayed.resolved);
   hfs_cache_aops.free_folio(&dirty_folio);
   assert(!dirty_folio.page.marker && dirty_folio.page.refs == 1);
   /* Fixture teardown is not a terminal drain or source-closure proof. */
   list_del_init(&delayed.list);
   session.view->nr_dirty--;
   cached->dirty = NULL;
   hfs_view_page_put(cached); /* fixture obligation reference */
   hfs_view_dirty_unslot(cached);
   hfs_view_page_put(cached);
   assert(module_refs == 0);
   hfs_view_fence(session.view);
   assert(hfs_view_enter(session.view, NULL, true, &cookie) == -ESTALE);
   hfs_view_unregister_inode(&object.inode);
   struct page private_cow = {.anon = true};
   assert(hfs_view_page_enter(&vma, &private_cow, true, &cookie) == 0);
   assert(session.view->active == 0 && module_refs == 1);
   assert(hfs_view_free(&session) == -EBUSY); /* actual lifetime-only cookie */
   hfs_view_leave(cookie);
   assert(module_refs == 0);
   assert(hfs_view_free(&session) == 0 && session.view == NULL);
   assert(reservations == 0);
   /* A newly enrolled empty domain polls before its first target:null
    * acquire. It exposes no target/observation/PTE until actual verified
    * lease/event input binds it (source verification is outside this shim).
    */
   assert(hfs_view_new_unbound(&session) == 0);
   observed_view = session.view;
   assert(session.view->phase == HFS_VIEW_UNBOUND);
   assert(uuid_is_null(&session.view->generation) && !session.view->target.len);
   assert(hfs_view_snapshot(session.view, &stamp) == -EAGAIN);
   assert(hfs_view_enter(session.view, NULL, false, &cookie) == -EAGAIN);
   assert(hfs_view_stop(session.view, &generation, &recall, 1) == 0);
   assert(session.view->phase == HFS_VIEW_QUIESCENT);
   assert(hfs_view_seal(session.view) == 0);
   hfs_view_fence(session.view);
   assert(hfs_view_free(&session) == 0);
   assert(reservations == 0 && module_refs == 0);
   puts("physical admission production-C checks passed; no VFS/PTE execution");
   return 0;
}
