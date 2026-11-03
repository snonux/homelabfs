/* Actual inode attach/eviction and forget worker; inode/workqueue primitives.
 */
#include "forget.inc"
struct inode {
   int i_lock, i_data;
   struct hfs_forget *forget;
};
#define HFS_I(inode) (inode)
static void hfs_view_unregister_inode(struct inode *inode) { (void)inode; }
static void truncate_inode_pages_final(int *mapping) { (void)mapping; }
static void clear_inode(struct inode *inode) { (void)inode; }
static void hfs_cred_free(struct hfs_cred *cred) { free(cred); }
static struct work_struct *queued[2];
static unsigned nr_queued;
static void queue_work(int control, struct work_struct *work) {
   (void)control;
   CHECK(nr_queued < 2);
   queued[nr_queued++] = work;
}
#include "inode.inc"
static void test_inode_tokens(struct hfs_session *session,
                              struct hfs_cred *cred) {
   struct inode inode = {0};
   const char *literals[2] = {fixture_result, fixture_second_result};
   struct hfs_forget *records[2];
   unsigned i;
   INIT_LIST_HEAD(&session->forgets);
   for (i = 0; i < 2; i++) {
      struct hfs_json *page = NULL;
      int entries;
      records[i] = calloc(1, sizeof(*records[i]));
      CHECK(records[i]);
      CHECK(!hfs_json_parse(literals[i], strlen(literals[i]), &page));
      entries = hfs_json_field(page, 0, "entries");
      CHECK(!hfs_cap_parse(session, page,
                           hfs_json_field(page, entries + 1, "capability"),
                           "PATH", &records[i]->cap));
      hfs_json_free(page);
      records[i]->issuer = malloc(sizeof(*cred));
      CHECK(records[i]->issuer);
      *records[i]->issuer = *cred;
      records[i]->count = 1;
      INIT_LIST_HEAD(&records[i]->list);
      hfs_inode_link_forget(&inode, records[i], session);
   }
   CHECK(inode.forget == records[1] && records[1]->next == records[0]);
   CHECK(memcmp(records[0]->cap->token, records[1]->cap->token, 32));
   expected_caps[0] = records[1]->cap;
   expected_caps[1] = records[0]->cap;
   inode_mode = true;
   calls = 0;
   hfs_evict_inode(&inode);
   CHECK(!inode.forget && nr_queued == 2 && !calls);
   // First exact token closes independently of its same-object sibling.
   transport_error = 0;
   expected_issuer = records[1]->issuer;
   queued[0]->run(queued[0]);
   CHECK(calls == 1 && session->forgets.next == &records[0]->list);
   // Failed flush/transport keeps the exact second token/issuer inventoried.
   transport_error = -EIO;
   expected_issuer = records[0]->issuer;
   queued[1]->run(queued[1]);
   CHECK(session->blocked && calls == 2);
   CHECK(session->forgets.next == &records[0]->list &&
         records[0]->cap == expected_caps[1]);
   // Fixture teardown only: production requires protected original-request or
   // whole-session recovery, never an invented successful retry/new claim.
   list_del(&records[0]->list);
   hfs_cap_free(records[0]->cap);
   hfs_cred_free(records[0]->issuer);
   free(records[0]);
   inode_mode = false;
}
