/* SPDX-License-Identifier: GPL-2.0-only */
#define LIST_HEAD(name) struct list_head name={&(name),&(name)}
#define list_for_each_entry_safe(p,n,h,m) for(p=container_of((h)->next,__typeof__(*p),m),n=container_of(p->m.next,__typeof__(*p),m);&p->m!=(h);p=n,n=container_of(n->m.next,__typeof__(*n),m))
struct hfs_json;
typedef unsigned int vm_fault_t;
#define VM_FAULT_OOM 0x1U
#define VM_FAULT_SIGBUS 0x2U
#define VM_FAULT_NOPAGE 0x100U
#define VM_FAULT_LOCKED 0x200U
struct vm_fault { struct vm_area_struct *vma; struct page *page; };
#define PageDirty(p) ((p)->dirty)
#define usleep_range(low,high) ((void)(high),usleep(low))
#define ERESTARTSYS 512
static inline void list_del(struct list_head *p) { p->prev->next=p->next;p->next->prev=p->prev; }
static inline void list_move_tail(struct list_head *p,struct list_head *h) { list_del_init(p);list_add_tail(p,h); }
struct hfs_lease;
struct hfs_request;
static int invalidate_inode_pages2(struct address_space *);
static int hfs_lease_release(struct hfs_session *,struct hfs_lease *);
static void hfs_lease_get(struct hfs_lease *);
static void hfs_lease_free(struct hfs_lease *);
static void hfs_request_unhold(struct hfs_session *,struct hfs_request *);
static int hfs_request_drain(struct hfs_session *,struct hfs_request *,struct hfs_json **);
struct hfs_dirty;
static void hfs_dirty_mapping_error(struct hfs_view *,struct hfs_dirty *,int);
