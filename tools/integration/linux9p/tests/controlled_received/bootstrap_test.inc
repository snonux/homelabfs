/* SPDX-License-Identifier: GPL-2.0-only */
/* Actual mount failure decision after actual binding and pre-HELLO allocation
 * failure. The disposable child preserves that permanent owner without changing
 * the parent's separate, successful runtime fixture. No native bytes are sent.
 */
static spinlock_t hfs_sessions_lock=PTHREAD_MUTEX_INITIALIZER;
static unsigned primitive_retained_modules;
#define __module_get(owner) ((void)(owner),primitive_retained_modules++)
#define ERR_PTR(error) ((void *)(intptr_t)(error))
static int fixture_unexpected_free(struct hfs_session *session) {
 (void)session;fprintf(stderr,"early admitted session attempted free\n");abort();
}
/* An aborting cleanup oracle is not a successful teardown implementation. */
#define hfs_session_free fixture_unexpected_free
#include "mount_fail.inc"
#undef hfs_session_free
static void bootstrap_failure_regression(struct fixture_session *fixture) {
 fflush(NULL);
 pid_t child=fork();assert(child>=0);
 if(!child){
  struct hfs_session *session=fixture->session;
  struct hfs_controlled_mount *mount=hfs_controlled_mount(session);
  assert(mount&&mount->session==session&&mount->device==&fixture->device.dev);
  assert(session->next_cell==1&&!session->nr_requests&&!session->blocked&&!mount->super);
  unsigned allocation=hfs_test_alloc_call;
  hfs_test_alloc_fail=allocation+1;
  const char *selected=hfs_protocol_selected_text(4,session->enrollment.protocol_features);
  struct hfs_buf hello={.data=(char *)selected,.len=strlen(selected)};
  struct hfs_json *response=NULL;
  int error=hfs_control(session,"HELLO",&hello,false,&response);
  assert(error==-ENOMEM&&!response&&hfs_test_alloc_call==allocation+1);
  assert(session->next_cell==1&&!session->nr_requests&&!session->blocked);
  assert(PTR_ERR(fixture_actual_mount_fail(session,error))==-ENOMEM);
  assert(session->retained&&session->closing&&session->blocked&&primitive_retained_modules==1);
  assert(mount->retained&&mount->session==session&&mount->device==&fixture->device.dev&&session->client==fixture->client);
  assert(hfs_controlled_admissible(session)<0&&session->provider->retire_session(session)<0);
  struct hfs_enrollment replacement;assert(session->provider->enroll(mount->device_tag,&replacement)<0);
  _exit(0);
 }
 int status;assert(waitpid(child,&status,0)==child&&WIFEXITED(status)&&WEXITSTATUS(status)==0);
}

/* The same actual mount predicate is exercised, without installing these
 * synthetic lane tuples or creating any received authority from them.
 */
static void mount_lane_regression(const struct hfs_enrollment *original) {
 struct hfs_enrollment e=*original;
 e.anchor=(uuid_t){{1}};e.acquisition_anchor=(uuid_t){{2}};e.control_anchor=(uuid_t){{3}};
 e.acquisition_nonce=e.view_control_nonce=e.nonce;
 assert(!hfs_enrollment_lanes(&e,4));
 for(unsigned minor=1;minor<4;minor++)assert(hfs_enrollment_lanes(&e,minor)<0);
 assert(!hfs_enrollment_lanes(&e,0));
 struct hfs_enrollment bad=e;bad.acquisition_anchor=e.anchor;assert(hfs_enrollment_lanes(&bad,4)<0);
 bad=e;bad.control_anchor=e.anchor;assert(hfs_enrollment_lanes(&bad,4)<0);
 bad=e;bad.control_anchor=e.acquisition_anchor;assert(hfs_enrollment_lanes(&bad,4)<0);
 for(unsigned field=0;field<6;field++){
  bad=e;uuid_t *fields[]={&bad.anchor,&bad.nonce,&bad.acquisition_anchor,&bad.acquisition_nonce,&bad.control_anchor,&bad.view_control_nonce};
  memset(fields[field],0,sizeof(uuid_t));assert(hfs_enrollment_lanes(&bad,4)<0);
 }
 e.view_control_nonce=(uuid_t){{4}};
 memset(&e.acquisition_anchor,0,sizeof(uuid_t));memset(&e.acquisition_nonce,0,sizeof(uuid_t));memset(&e.control_anchor,0,sizeof(uuid_t));
 for(unsigned minor=0;minor<4;minor++)assert(!hfs_enrollment_lanes(&e,minor));
 e.view_scope=HFS_SCOPE_NORMAL_PUBLIC;assert(!hfs_enrollment_lanes(&e,1));
 assert(hfs_enrollment_lanes(&e,2)<0&&hfs_enrollment_lanes(&e,3)<0&&hfs_enrollment_lanes(&e,4)<0);
}

/* Exact finite control accounting, not a claim that bytes are physically
 * preallocated or that C7 funded-control progress has been implemented.
 */
static void accounting_regression(struct hfs_session *session) {
 struct hfs_controlled_registry *registry=hfs_controlled_registry;
 u64 base=registry->control_base_bytes,before=hfs_controlled_control_used;
 assert(base>HFS_CONTROLLED_STAGING_BYTES+HFS_ADMISSION_OPEN_MAX*HFS_ADMISSION_MAX);
 assert(base+before<registry->budget.control_bytes);
 u64 remaining=registry->budget.control_bytes-base-before;
 assert(remaining>sizeof(struct hfs_controlled_credit));
 void *credit=NULL;
 assert(hfs_controlled_reserve(session,remaining-sizeof(struct hfs_controlled_credit)+1,true,&credit)==-ENOSPC&&!credit);
 assert(!hfs_controlled_reserve(session,remaining-sizeof(struct hfs_controlled_credit),true,&credit));
 assert(base+hfs_controlled_control_used==registry->budget.control_bytes);
 void *extra=NULL;assert(hfs_controlled_reserve(session,1,true,&extra)==-ENOSPC&&!extra);
 hfs_controlled_unreserve(session,credit);assert(hfs_controlled_control_used==before);
 remaining=registry->budget.ordinary_bytes-hfs_controlled_ordinary_used;
 assert(remaining>sizeof(struct hfs_controlled_credit));
 assert(hfs_controlled_reserve(session,remaining-sizeof(struct hfs_controlled_credit)+1,false,&credit)==-ENOSPC&&!credit);
 assert(hfs_controlled_control_used==before&&registry->control_base_bytes==base);
}

/* w54 (zq3-r3 R3-1). The lab provisioned reservations 32, so bind_super's
 * first progress lane refused ENOSPC, and mount_nodev's kill_sb then made
 * begin_close dereference the absent terminal lane. The actual hfs_fill_super
 * and v9fs_hfs_kill_super (super.inc) run here; only VFS primitives they never
 * reach after a failed bind abort. Each case runs in a disposable child after
 * the actual accepted HELLO, so the parent fixture is unchanged.
 */
struct super_operations { int unused; };
const struct super_operations hfs_super_ops;
#define V9FS_MAGIC 0x01021997
#define MAX_LFS_FILESIZE 0x7fffffffffffffffLL
#define PAGE_SHIFT 12
static unsigned primitive_anon_kills;
static void kill_anon_super(struct super_block *sb) { (void)sb;primitive_anon_kills++; }
static void tracepoint_synchronize_unregister(void) {}
static struct dentry *d_make_root(struct inode *root) { (void)root;abort(); }
struct inode *hfs_inode_get(struct super_block *sb,struct hfs_json *result,const char *kind,const struct hfs_cred *issuer) {
 (void)sb;(void)result;(void)kind;(void)issuer;abort();
}
int hfs_mmap_bootstrap(struct hfs_session *session,const struct hfs_cred *cred) { (void)session;(void)cred;abort(); }
#include "super.inc"

/* Runs one case in a child; reports instead of aborting so every case of a
 * pre-w54 source shows its own failure. */
static bool regression_child(const char *name,void (*body)(struct fixture_session *),struct fixture_session *fixture) {
 fflush(NULL);
 pid_t child=fork();assert(child>=0);
 if(!child){body(fixture);fflush(NULL);_exit(0);}
 int status;assert(waitpid(child,&status,0)==child);
 bool passed=WIFEXITED(status)&&!WEXITSTATUS(status);
 if(!passed)fprintf(stderr,"w54 regression %s FAILED (status 0x%x)\n",name,status);
 return passed;
}

static void failed_bind_kill_sb(struct fixture_session *fixture) {
 struct hfs_session *session=fixture->session;
 struct hfs_controlled_mount *mount=hfs_controlled_mount(session);
 bool terminal=session->enrollment.protocol_features==HFS_TERMINAL_FEATURES;
 assert(mount&&!mount->super&&session->next_cell>1);
 hfs_controlled_registry->budget.reservations=32; /* zq3-spec.json before w54 */
 u32 credits=hfs_controlled_nr_credits,control=hfs_controlled_nr_control_credits,received=hfs_controlled_nr_received;
 u64 used=hfs_controlled_control_used;
 struct super_block sb={.s_dev=2000,.s_user_ns=&init_user_ns};
 assert(hfs_fill_super(&sb,session,0)==-ENOSPC);
 assert(!session->control_completion&&!session->retirement_completion&&!session->terminal_completion);
 /* Every lane credit returned; the consumed identity stays claimed. */
 assert(hfs_controlled_nr_credits==credits&&hfs_controlled_nr_control_credits==control);
 assert(hfs_controlled_nr_received==received&&hfs_controlled_control_used==used);
 assert(!mount->super&&mount->claimed&&mount->session==session);
 unsigned kills=primitive_anon_kills,modules=primitive_retained_modules;
 v9fs_hfs_kill_super(&sb); /* oopsed in zq3-r3 */
 assert(primitive_anon_kills==kills+1&&primitive_retained_modules==modules+1&&!sb.s_fs_info);
 assert(session->closing&&session->blocked&&session->retained&&mount->retained&&mount->stopping);
 assert(hfs_controlled_begin_close(session)==(terminal?-ESTALE:0));
 void *credit=&credit;assert(hfs_completion_reserve(NULL,1,false,&credit)==-ESTALE&&!credit);
 assert(hfs_controlled_admissible(session)<0&&session->provider->retire_session(session)<0);
 struct hfs_enrollment replacement;assert(session->provider->enroll(mount->device_tag,&replacement)<0);
}

/* Independent oracle of the documented mount floor: per mount 95 (32767) or
 * 78 (16383) control and 75 ordinary credits; 15 or 14 control and 12
 * ordinary Received slots. Provisioning must refuse anything below twice the
 * larger partition and accept exactly that. */
static void provision_slot_floor(struct fixture_session *fixture) {
 struct hfs_controlled_registry *registry=hfs_controlled_registry;
 u32 control=0,ordinary=0,control_received=0,ordinary_received=0;
 (void)fixture;
 for(u32 i=0;i<registry->nr_mounts;i++){
  bool terminal=registry->mounts[i]->enrollment.protocol_features==HFS_TERMINAL_FEATURES;
  control+=terminal?95:78;control_received+=terminal?15:14;ordinary+=75;ordinary_received+=12;
 }
 u32 reservations=2*(control>ordinary?control:ordinary);
 u32 received=2*(control_received>ordinary_received?control_received:ordinary_received);
 registry->budget.received=received;
 registry->budget.reservations=32;assert(hfs_controlled_base_charge(registry)==-ENOSPC);
 registry->budget.reservations=reservations-1;assert(hfs_controlled_base_charge(registry)==-ENOSPC);
 registry->budget.reservations=reservations;assert(!hfs_controlled_base_charge(registry));
 registry->budget.received=received-1;assert(hfs_controlled_base_charge(registry)==-ENOSPC);
}

static void w54_regressions(struct fixture_session *fixture,bool first) {
 bool passed=regression_child("failed-bind-kill-sb",failed_bind_kill_sb,fixture);
 if(first)passed&=regression_child("provision-slot-floor",provision_slot_floor,fixture);
 assert(passed);
}

/* The floor's units checked against the actual accounting of one mount:
 * credits/Received charged to it, request/ticket counts and families. */
struct floor_mark { u32 ordinary,control,ordinary_received,control_received,requests,tickets,families[2];unsigned commands; };
static unsigned fixture_commands;
static struct floor_mark floor_mark(struct hfs_session *session) {
 struct hfs_controlled_mount *mount=hfs_controlled_mount(session);struct hfs_view_family *family;
 struct floor_mark mark={mount->ordinary_credits,mount->control_credits,mount->ordinary_received,
  mount->control_received,session->nr_requests,session->nr_view_tickets,{0,0},fixture_commands};
 mutex_lock(&session->lock);
 list_for_each_entry(family,&session->view_families,list)mark.families[family->control]++;
 mutex_unlock(&session->lock);
 return mark;
}
/* One owner (reservation + package) per new request, and per lease one
 * ordinary acquire plus one control release ticket owner, one credit per new
 * family and nothing else. A nested command or a pressure retirement changes
 * the counts; the check is then skipped rather than guessed. */
static void floor_owner_check(struct hfs_session *session,const struct floor_mark *before,u32 requests,u32 leases) {
 struct floor_mark after=floor_mark(session);
 if(after.commands!=before->commands||after.requests!=before->requests+requests||after.tickets!=before->tickets+2*leases)return;
 u32 owner=1U+1U+HFS_COMPLETION_SLOTS;
 assert(after.ordinary-before->ordinary==(requests+leases)*owner+after.families[0]-before->families[0]);
 assert(after.control-before->control==leases*owner+after.families[1]-before->families[1]);
 assert(after.ordinary_received-before->ordinary_received==(requests+leases)*HFS_COMPLETION_RECEIVED);
 assert(after.control_received-before->control_received==leases*HFS_COMPLETION_RECEIVED);
}
