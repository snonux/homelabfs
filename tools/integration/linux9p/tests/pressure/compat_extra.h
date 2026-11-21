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
/* A pending fatal signal for hfs_mmap_fault_final (1a4 review). */
static bool test_fatal_signal;
#define current NULL
#define fatal_signal_pending(t) ((void)(t),test_fatal_signal)
/* hfs_mmap_fail and its control struct (1a4): workqueue items are opaque. */
struct workqueue_struct;
struct work_struct { int unused; };
struct delayed_work { int unused; };
#define test_and_set_bit(n,p) ((__atomic_fetch_or((p),1UL<<(n),__ATOMIC_SEQ_CST)>>(n))&1UL)
#define set_bit(n,p) ((void)__atomic_fetch_or((p),1UL<<(n),__ATOMIC_SEQ_CST))
/* Kernel log substitute: keeps the last line and counts lines. It formats
 * the kernel's %pUb (a uuid_t pointer, "(null)" for NULL) besides the %d and
 * %s that hfs_mmap_fail's line uses, and %llu (2f4's fences); anything else
 * aborts.
 */
static char last_warning[512];
static unsigned int warnings;
/* 1h4: every line since the case cleared it, for lines before the last. */
static char warning_log[8192];
static void pr_warn(const char *f,...);
#define pr_warn_ratelimited pr_warn
#define pr_info_ratelimited pr_warn
/* ie4: hfs_mmap_begin's control reference and cleanup packets are fixtures
 * (test.c). Queueing a packet records it; the test then plays the cleanup
 * worker's release, so a packet queued twice or never is caught.
 */
struct hfs_mmap_control;
static struct hfs_mmap_control *hfs_mmap_control_get(struct hfs_view *view);
static void hfs_mmap_control_put(struct hfs_mmap_control *control);
static void hfs_mmap_cleanup(struct work_struct *work);
static bool test_queue_cleanup(struct delayed_work *work);
#define INIT_DELAYED_WORK(w,f) ((void)(w),(void)(f))
#define queue_delayed_work(q,w,d) ((void)(q),(void)(d),test_queue_cleanup(w))
/* re4: hfs_mmap_retire_work's container_of, the BLOCKED back-off's pause
 * (a test sleep only advances jiffies, one jiffy per ms, and is counted) and
 * hfs_view_retire, a host-model fixture in test.c.
 */
#define to_delayed_work(w) ((struct delayed_work *)(void *)(w))
#define min(a,b) ((a)<(b)?(a):(b))
#define jiffies_to_msecs(j) ((unsigned int)(j))
#define time_after_eq(a,b) ((long)((a)-(b))>=0)
static unsigned int test_sleeps;
static void (*test_pause_hook)(void); // e.g. the cleanup worker runs meanwhile
static inline long schedule_timeout_killable(long t)
{ test_sleeps++;jiffies+=t;if(test_pause_hook)test_pause_hook();return 0; }
/* ie4: hfs_inode_get_fail and hfs_evict_inode. iget_failed evicts the new
 * inode as the kernel's does (through iput); a queued FORGET is recorded by
 * test_queue_forget. The capability and credential frees are counted.
 */
#define I_NEW 8UL
struct hfs_cred;
static void hfs_cap_free(struct hfs_cap *cap);
static void hfs_cred_free(struct hfs_cred *cred);
static void hfs_evict_inode(struct inode *inode);
static void hfs_forget_work(struct work_struct *work);
static bool test_queue_forget(struct work_struct *work);
#define iget_failed(i) hfs_evict_inode(i)
#define INIT_WORK(w,f) ((void)(w),(void)(f))
#define queue_work(q,w) ((void)(q),test_queue_forget(w))
#define clear_inode(i) ((void)(i))
#define truncate_inode_pages_final(m) ((void)(m))
/* 2f4: the actual hfs_view_exchange (zq3-r17 R17-1). Its transport, the
 * ticket reply check, the hfs1.4 source receipt (patch 0005), the mount's
 * current_fence, the session block and the poll halt are fixtures in
 * test.c; HZ matches the one-jiffy-per-ms clock.
 */
#define HZ 1000UL
struct hfs_buf;
struct hfs_view_ticket;
int hfs_control_flagged(struct hfs_session *,const char *,const struct hfs_buf *,bool,bool,struct hfs_json **);
static int hfs_ticket_reply(struct hfs_view_ticket *,const struct hfs_json *,int,const char *);
bool hfs_after_block_operation(const char *);
u64 hfs_controlled_current_fence(struct hfs_session *);
int hfs_session_poll_halt(struct hfs_session *,bool);
void hfs_session_block(struct hfs_session *,const char *,const char *,int);
static int test_received_source_ex(struct hfs_session *,const struct hfs_json *,int,bool *);
static inline long strscpy(char *d,const char *s,size_t n)
{ size_t len=strlen(s);assert(n);size_t copy=len<n?len:n-1;memcpy(d,s,copy);d[copy]=0;return len<n?(long)len:-7; }
static inline const char *strnstr(const char *s,const char *find,size_t len)
{ return memmem(s,len,find,strlen(find)); }
static void pr_warn(const char *f,...)
{
 va_list args;size_t n=0;
 va_start(args,f);
 for(;*f;f++) {
  if(*f!='%'){assert(n+1<sizeof(last_warning));last_warning[n++]=*f;continue;}
  f++;
  if(*f=='d')n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%d",va_arg(args,int));
  else if(f[0]=='l'&&f[1]=='l'&&f[2]=='u'){f+=2;n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%llu",va_arg(args,unsigned long long));} // 2f4: fences
  else if(*f=='s')n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%s",va_arg(args,const char *));
  else if(f[0]=='p'&&f[1]=='U'&&f[2]=='b'){
   const uuid_t *u=va_arg(args,const uuid_t *);f+=2;
   if(!u)n+=snprintf(last_warning+n,sizeof(last_warning)-n,"(null)");
   else for(int i=0;i<16;i++)n+=snprintf(last_warning+n,sizeof(last_warning)-n,"%s%02x",(i==4||i==6||i==8||i==10)?"-":"",u->b[i]);
  } else abort();
  assert(n<sizeof(last_warning));
 }
 va_end(args);last_warning[n]=0;warnings++;fputs(last_warning,stderr);
 if(strlen(warning_log)+n<sizeof(warning_log))strcat(warning_log,last_warning);
}
