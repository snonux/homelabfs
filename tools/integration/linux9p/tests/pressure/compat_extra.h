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
/* jiffies, time_before, msecs_to_jiffies and ERESTARTSYS (the settable clock
 * of the bounded pressure hold, p84) live in view/compat.h since 1a4, which
 * also needs them for hfs_view_enter's bound.
 */
/* hfs_mmap_fail and its control struct (1a4): workqueue items are opaque. */
struct workqueue_struct;
struct work_struct { int unused; };
struct delayed_work { int unused; };
#define test_and_set_bit(n,p) ((__atomic_fetch_or((p),1UL<<(n),__ATOMIC_SEQ_CST)>>(n))&1UL)
#define set_bit(n,p) ((void)__atomic_fetch_or((p),1UL<<(n),__ATOMIC_SEQ_CST))
/* Kernel log substitute: keeps the last line and counts lines. It formats
 * the kernel's %pUb (a uuid_t pointer, "(null)" for NULL) besides the %d and
 * %s that hfs_mmap_fail's line uses; anything else aborts.
 */
static char last_warning[512];
static unsigned int warnings;
static void pr_warn(const char *f,...)
{
 va_list args;size_t n=0;
 va_start(args,f);
 for(;*f;f++) {
  if(*f!='%'){assert(n+1<sizeof(last_warning));last_warning[n++]=*f;continue;}
  f++;
  if(*f=='d')n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%d",va_arg(args,int));
  else if(*f=='s')n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%s",va_arg(args,const char *));
  else if(f[0]=='p'&&f[1]=='U'&&f[2]=='b'){
   const uuid_t *u=va_arg(args,const uuid_t *);f+=2;
   if(!u)n+=snprintf(last_warning+n,sizeof(last_warning)-n,"(null)");
   else for(int i=0;i<16;i++)n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%s%02x",(i==4||i==6||i==8||i==10)?"-":"",u->b[i]);
  } else abort();
  assert(n<sizeof(last_warning));
 }
 va_end(args);last_warning[n]=0;warnings++;fputs(last_warning,stderr);
}
