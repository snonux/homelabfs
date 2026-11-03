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
  struct hfs_session *session=&fixture->session;
  struct hfs_controlled_mount *mount=hfs_controlled_mount(session);
  assert(mount&&mount->session==session&&mount->device==&fixture->device.dev);
  assert(session->next_cell==1&&!session->nr_requests&&!session->blocked&&!mount->super);
  unsigned allocation=hfs_test_alloc_call;
  hfs_test_alloc_fail=allocation+1;
  struct hfs_buf hello={.data=(char *)hfs_protocol_text(4),.len=strlen(hfs_protocol_text(4))};
  struct hfs_json *response=NULL;
  int error=hfs_control(session,"HELLO",&hello,false,&response);
  assert(error==-ENOMEM&&!response&&hfs_test_alloc_call==allocation+1);
  assert(session->next_cell==1&&!session->nr_requests&&!session->blocked);
  assert(PTR_ERR(fixture_actual_mount_fail(session,error))==-ENOMEM);
  assert(session->retained&&session->closing&&session->blocked&&primitive_retained_modules==1);
  assert(mount->retained&&mount->session==session&&mount->device==&fixture->device.dev&&session->client==&fixture->client);
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
