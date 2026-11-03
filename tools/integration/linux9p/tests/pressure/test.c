// SPDX-License-Identifier: GPL-2.0-only
/* Actual pressure/mkwrite/stop/install C. PTE, VFS cache, source verification and
 * transport result primitives are fixtures, never running-kernel proof.
 */
unsigned int hfs_test_alloc_call,hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#include "production.c"
static struct folio folios[128];
static struct hfs_lease leases[128];
static struct hfs_view_ticket release_credits[128];
static uuid_t generation={{1}},recall={{2}};
static int releases,invalid_source,mapping_error;
static struct hfs_json *target_event;
static struct vm_area_struct *test_vma;
static int reserve(void *s,u64 bytes,bool control,void **out)
{
 (void)control;assert(s==test_session && bytes);*out=malloc(1);assert(*out);atomic_fetch_add(&reservations,1);return 0;
}
static void unreserve(void *s,void *credit)
{ assert(s==test_session && credit);free(credit);atomic_fetch_sub(&reservations,1); }
static void unmap_mapping_range(struct address_space *mapping,int start,int length,int cows)
{
 assert(mapping && !start && !length && !cows && observed_view->phase==HFS_VIEW_RECALLING && !observed_view->active);
 for(unsigned int i=0;i<ARRAY_SIZE(folios);i++)if(folios[i].mapping==mapping)folios[i].page.mapped=false;
 atomic_fetch_add(&unmaps,1);
}
static int invalidate_inode_pages2(struct address_space *mapping)
{
 for(unsigned int i=0;i<ARRAY_SIZE(folios);i++) {
  struct folio *f=&folios[i];if(f->mapping!=mapping || !f->page.private)continue;
  assert(!f->page.locked);lock_page(&f->page);
  if(f->page.dirty) {int err=hfs_cache_aops.launder_folio(f);if(err){unlock_page(&f->page);return err;}}
  hfs_cache_aops.invalidate_folio(f,0,PAGE_SIZE);f->mapping=NULL;
  hfs_cache_aops.free_folio(f);unlock_page(&f->page);
 }
 return 0;
}
int hfs_view_target(struct hfs_session *session,const struct hfs_json *event,int token,uuid_t *visibility,u8 hash[32])
{
 assert(session==test_session && event==target_event && token==hfs_json_field(event,0,"target"));
 if(invalid_source)return -EACCES;
 *visibility=generation;memset(hash,0,32);return 0;
}
static void hfs_lease_get(struct hfs_lease *lease) { kref_get(&lease->refs); }
static void hfs_lease_free(struct hfs_lease *lease) { assert(atomic_fetch_sub(&lease->refs.refcount,1)>1); }
static int hfs_lease_release(struct hfs_session *session,struct hfs_lease *lease)
{
 struct hfs_view *view=session->view;u64 seq=view->sequence+1;int target=hfs_json_field(target_event,0,"target"),err;
 assert(lease->published && !lease->dirty && lease->release_ticket && lease->release_ticket->control);
 assert(!view->active && !pthread_mutex_trylock(&view->transitions.native));pthread_mutex_unlock(&view->transitions.native);
 assert(!pthread_mutex_trylock(&view->lock));pthread_mutex_unlock(&view->lock);
 for(unsigned int i=0;i<ARRAY_SIZE(folios);i++)assert(!folios[i].page.locked);
 releases++;recall.b[0]++;
 err=hfs_view_stop(view,&generation,&recall,seq++);if(err)return err;
 err=hfs_view_seal(view);if(err)return err;
 err=hfs_mmap_install(view,target_event,target,&recall,seq++,false);if(err)return err;
 err=hfs_mmap_finish_drained(view);if(err)return err;
 err=hfs_mmap_install(view,target_event,target,&recall,seq++,true);if(err)return err;
 err=hfs_view_reopen(view,&recall,seq);if(err)return err;
 assert(lease->revoked);lease->released=true;return 0;
}
static void hfs_request_unhold(struct hfs_session *s,struct hfs_request *r) { (void)s;(void)r;abort(); }
void hfs_view_ticket_put(struct hfs_view_ticket *ticket) { (void)ticket;abort(); }
static int hfs_request_drain(struct hfs_session *s,struct hfs_request *r,struct hfs_json **out) { (void)s;(void)r;(void)out;abort(); }
static void hfs_dirty_mapping_error(struct hfs_view *v,struct hfs_dirty *d,int err) { (void)v;(void)d;mapping_error=err; }
static void clean_page(struct hfs_view *view,struct address_space *mapping,unsigned int index)
{
 struct hfs_view_stamp stamp;struct hfs_cache_page *entry;struct hfs_lease *lease=&leases[index];void *cookie;
 assert(!hfs_view_snapshot(view,&stamp));assert(!hfs_view_page_prepare(view,&stamp,&entry));
 memset(lease,0,sizeof(*lease));kref_init(&lease->refs);release_credits[index].control=true;
 lease->release_ticket=&release_credits[index];lease->description[0]=1;lease->visibility=generation;entry->lease=lease;
 folios[index]=(struct folio){.page={.locked=true,.uptodate=true,.refs=1},.mapping=mapping};
 assert(!hfs_view_page_attach(entry,&folios[index].page));
 assert(!hfs_view_page_enter(test_vma,&folios[index].page,false,&cookie));hfs_view_leave(cookie);
 folios[index].page.mapped=true;unlock_page(&folios[index].page);hfs_view_page_put(entry);
}
int main(void)
{
 struct hfs_provider provider={reserve,unreserve};struct hfs_session session={.provider=&provider};
 struct address_space mapping={.id=1,.a_ops=&hfs_cache_aops};struct hfs_inode inode={.inode={&mapping,1}};
 struct hfs_cap cap={.token={1}};struct hfs_description desc={.cap=&cap};struct file file={&inode.inode,&desc};struct vm_area_struct vma={&file};
 test_vma=&vma;test_session=&session;mutex_init(&session.lock);INIT_LIST_HEAD(&inode.view_list);
 assert(!hfs_view_new(&session,&generation));observed_view=session.view;assert(!hfs_view_register_inode(&inode.inode));
 assert(!hfs_json_parse("{\"target\":{}}",13,&target_event));
 for(unsigned int i=0;i<128;i++)clean_page(session.view,&mapping,i);
 assert(session.view->nr_cached==128);struct hfs_view_stamp old_stamp;struct hfs_cache_page *extra=NULL;
 assert(!hfs_view_snapshot(session.view,&old_stamp));assert(hfs_view_page_prepare(session.view,&old_stamp,&extra)==-ENOSPC && !extra);
 assert(hfs_mmap_fault_error(session.view,-ENOSPC)==VM_FAULT_NOPAGE);
 assert(releases==1 && session.view->phase==HFS_VIEW_LIVE && !session.view->nr_cached && session.view->revision>old_stamp.revision);
 assert(!hfs_view_snapshot(session.view,&old_stamp));assert(!hfs_view_page_prepare(session.view,&old_stamp,&extra));hfs_view_page_put(extra);
 // Read-first page_mkwrite uses the exact original page, unlocks before its
 // funded RELEASE, and retries only after actual stopped cache invalidation.
 clean_page(session.view,&mapping,0);struct vm_fault vmf={&vma,&folios[0].page};struct page *original=vmf.page;
 assert(hfs_mmap_mkwrite(&vmf)==VM_FAULT_NOPAGE && vmf.page==original);
 assert(releases==2 && session.view->phase==HFS_VIEW_LIVE && !session.view->nr_cached && !folios[0].page.private);
 // A source verifier refusal cannot manufacture DRAINED or reopen.
 clean_page(session.view,&mapping,0);invalid_source=1;
 assert(hfs_mmap_mkwrite(&vmf)==VM_FAULT_SIGBUS && session.view->phase==HFS_VIEW_STOPPED);
 assert(releases==3 && session.view->nr_cached==1);invalid_source=0;
 // Explicit fixture teardown of the denied stopped cache, not authority proof.
 assert(!invalidate_inode_pages2(&mapping));
 // Actual drain result validation and actual DRAINED registry gate: short and
 // error results retain the immutable obligation and can never close it.
 struct hfs_request request={.terminal=true};struct hfs_cache_page entry={0};
 struct hfs_dirty dirty={.adopted=true,.frozen=true,.request=&request,.entry=&entry,.length=4};
 INIT_LIST_HEAD(&dirty.list);list_add_tail(&dirty.list,&session.view->dirty);session.view->nr_dirty=1;
 session.view->phase=HFS_VIEW_INSTALLED_DRAIN;
 assert(!hfs_json_parse("{\"code\":\"OK\",\"count\":3}",strlen("{\"code\":\"OK\",\"count\":3}"),&request.result));
 assert(hfs_dirty_drain(session.view,&dirty)==-EIO && mapping_error==-EIO && !dirty.resolved);
 assert(hfs_mmap_finish_drained(session.view)==-EBUSY && session.view->phase==HFS_VIEW_INSTALLED_DRAIN);
 hfs_json_free(request.result);assert(!hfs_json_parse("{\"code\":\"IO\"}",13,&request.result));
 assert(hfs_dirty_drain(session.view,&dirty)==-EIO && !dirty.resolved);
 assert(hfs_mmap_finish_drained(session.view)==-EBUSY && session.view->phase!=HFS_VIEW_DRAINED);
 hfs_json_free(request.result);list_del_init(&dirty.list);session.view->nr_dirty=0;
 hfs_json_free(target_event);hfs_view_unregister_inode(&inode.inode);hfs_view_fence(session.view);assert(!hfs_view_free(&session));
 assert(!reservations && !module_refs);
 puts("PASS: actual128-clean pressure/129th admission,mkwrite release/stop/install/reopen,source denial,short/error neverDRAINED; primitive VFS/PTE/source boundaries only");return 0;
}
