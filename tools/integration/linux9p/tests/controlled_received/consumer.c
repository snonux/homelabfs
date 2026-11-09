// SPDX-License-Identifier: GPL-2.0-only
/* Actual provider/session/received component. OS primitives are declared here. */
#include <sys/stat.h>
#include <sys/wait.h>
int primitive_fstat(int,struct stat *);
ssize_t primitive_pread(int,void *,size_t,off_t);
ssize_t primitive_pwrite(int,const void *,size_t,off_t);
#define fstat primitive_fstat
#define pread primitive_pread
#define pwrite primitive_pwrite
#include "tools/integration/lib9p/src/hfs9p.c"
#undef fstat
#undef pread
#undef pwrite
#include "tools/integration/lib9p/src/hfs9p_ledger.h"
#include "hfs.h"
#include "controlled_compat.h"
#include "hfs_controlled.h"
#include "hfs_received.h"
#include "hfs_crypto.h"
#include "hfs_controlled_artifacts.c"
#include "hfs_admission.c"
#define p9_proto_hfs14 4
#include "hfs_controlled.c"
static DEFINE_MUTEX(hfs_provider_lock);
static const struct hfs_provider *hfs_provider;
static atomic_t hfs_active_sessions = ATOMIC_INIT(0);
static unsigned module_put_count,client_destroy_count;
void primitive_module_put(void *owner) { (void)owner;module_put_count++; }
static uuid_t primitive_process_uuid;
static bool primitive_process_uuid_pending;
void primitive_uuid_gen(uuid_t *out) {
 static unsigned calls;
 if(primitive_process_uuid_pending){*out=primitive_process_uuid;primitive_process_uuid_pending=false;}
 else if(!calls++)*out=primitive_nonce;
 else assert(RAND_bytes(out->b,16)==1);
}
struct task_struct primitive_current_task;
const struct cred *primitive_current_cred;
const struct cred *primitive_real_cred;
const struct cred *primitive_get_current_cred(void) {
 struct cred *copy=malloc(sizeof(*copy));struct group_info *groups=malloc(sizeof(*groups));
 assert(copy&&groups&&primitive_current_cred);*copy=*primitive_current_cred;*groups=*copy->group_info;
 copy->group_info=groups;copy->refs=1;copy->heap_owned=true;return copy;
}
const struct cred *primitive_get_cred(const struct cred *input) {
 struct cred *cred=(struct cred *)input;assert(cred&&cred->refs);cred->refs++;return cred;
}
void primitive_put_cred(const struct cred *input) {
 if(!input)return;struct cred *cred=(struct cred *)input;assert(cred->refs);
 if(!--cred->refs){assert(cred->heap_owned);free(cred->group_info);free(cred);}
}
#include "session.inc"
#include "lease.inc"
#include "release_prepare.inc"
#include "hfs_view.c"
#include "control_struct.inc"
#include "physical.inc"
#include "control.inc"

struct workqueue_struct *alloc_workqueue(const char *name,unsigned flags,unsigned max) {
 (void)name;assert(flags==(WQ_UNBOUND|WQ_MEM_RECLAIM)&&max==1);return calloc(1,sizeof(struct workqueue_struct));
}
bool queue_work(struct workqueue_struct *queue,struct work_struct *work) {
 if(work->queued)return false;assert(queue&&queue->count<32);
 work->queued=true;work->queue=queue;queue->pending[queue->count++]=work;return true;
}
bool queue_delayed_work(struct workqueue_struct *queue,struct delayed_work *work,unsigned long delay) {
 /* The test advances one explicitly selected timer/queue boundary at a time. */
 (void)delay;return queue_work(queue,&work->work);
}
bool cancel_delayed_work_sync(struct delayed_work *delayed) {
 struct work_struct *work=&delayed->work;assert(!work->running);
 if(!work->queued)return false;
 struct workqueue_struct *queue=work->queue;unsigned i;
 for(i=0;i<queue->count;i++)if(queue->pending[i]==work)break;
 assert(i<queue->count);memmove(queue->pending+i,queue->pending+i+1,(queue->count-i-1)*sizeof(queue->pending[0]));
 queue->count--;work->queued=false;work->queue=NULL;return true;
}
static bool primitive_step_queue(struct workqueue_struct *queue) {
 if(!queue||!queue->count)return false;
 struct work_struct *work=queue->pending[0];assert(work->queued&&work->queue==queue&&!work->running);
 memmove(queue->pending,queue->pending+1,(--queue->count)*sizeof(queue->pending[0]));
 work->queued=false;work->queue=NULL;work->running=true;work->function(work);work->running=false;return true;
}
void flush_workqueue(struct workqueue_struct *queue) {
 unsigned steps=0;while(primitive_step_queue(queue))assert(++steps<=128);
}
void destroy_workqueue(struct workqueue_struct *queue) { if(queue){flush_workqueue(queue);assert(!queue->count);free(queue);} }

unsigned hfs_test_alloc_call,hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
struct user_namespace init_user_ns={.uid_map={1,{{0,0,UINT32_MAX}}},.gid_map={1,{{0,0,UINT32_MAX}}}};
struct user_namespace foreign_user_ns;
bool primitive_admin=true,primitive_initial_ns=true;
uuid_t primitive_boot,primitive_nonce;
struct kernfs_node primitive_nodes[8];
unsigned primitive_node_count;
static struct kobject primitive_root;
struct kobject *kernel_kobj=&primitive_root;
struct kobject *kobject_create_and_add(const char *name,struct kobject *parent) {
 assert(!strcmp(name,"homelabfs")&&parent==kernel_kobj);return &primitive_root;
}
struct kernfs_node *__kernfs_create_file(struct kernfs_node *parent,const char *name,
 unsigned mode,unsigned uid,unsigned gid,loff_t size,const struct kernfs_ops *ops,
 void *priv,void *ns,void *key) {
 (void)parent;(void)priv;(void)ns;(void)key;
 assert(!uid&&!gid&&size==HFS_ADMISSION_MAX&&primitive_node_count<8);
 struct kernfs_node *node=&primitive_nodes[primitive_node_count++];
 node->name=name;node->mode=mode;node->ops=ops;return node;
}
struct virtio_device { struct device dev;void *priv; };
struct virtio_chan { bool inuse;struct p9_client *client;struct virtio_device *vdev;char *tag;struct list_head chan_list; };
static LIST_HEAD(virtio_chan_list);
static DEFINE_MUTEX(virtio_9p_lock);
static int p9_virtio_trans;
#define Connected 1
#define p9_proto_hfs14 4
#define device_is_registered(d) ((d)->registered)
#define dev_name(d) ((d)->name)
#include "virtio_identity.inc"

struct fixture_session {
 struct hfs_session *session;
 bool destroyed;
 struct p9_client *client;
 struct pid pid;
 struct group_info groups;
 struct virtio_chan channel;
 struct virtio_device device;
 struct super_block super;
 struct hfs_cred credential;
 struct hfs_cred captures[2];struct hfs_buf capture_bytes[2];unsigned nr_captures;
 struct hfs_cap *adopted_caps[32];unsigned nr_adopted_caps;
 struct cred native_cred;
 struct hfs_buf credentials;
 struct hfs_json *latest,*ordinary_result,*domain_alias;
 struct hfs_cap *root,*description,*created_path;
 struct hfs_description *cleanup_description;struct hfs_forget *cleanup_forget;
 struct hfs_lease *lease;
 struct hfs_cache_page *dirty_page;
 struct hfs_dirty *dirty;
 struct folio folio;
 bool actual_result;
 struct hfs_json *held_results[32];unsigned nr_held_results;
 struct hfs_json *result_pressure[2048], *query_pressure;unsigned nr_result_pressure;
 struct hfs_json *event_pressure[HFS_MMAP_EVENTS_MAX+1];unsigned nr_event_pressure;
 void *parked_carrier,*parked_received,*floor_pressure[2],*global_control_pressure;
 void *terminal_external_credit;
 void *pressure[4096];unsigned nr_pressure;
 struct hfs_mmap_control physical;
};
static void *hello_parse, *hello_data, *hello_tokens, *hello_candidate;
static unsigned hello_freed;
static void *retry_snapshot_watch;static unsigned retry_snapshot_frees;
void primitive_free(void *pointer) {
 if(pointer&&pointer==retry_snapshot_watch)retry_snapshot_frees++;
 if(pointer&&hello_candidate){
  if(pointer==hello_tokens)hello_freed|=1;
  if(pointer==hello_data)hello_freed|=2;
  if(pointer==hello_parse)hello_freed|=4;
  if(pointer==hello_candidate){assert(hello_freed==7);hello_freed|=8;}
 }
 free(pointer);
}
static struct fixture_session fixtures[64];
void p9_client_destroy(struct p9_client *client) {
 client_destroy_count++;
 for(unsigned i=0;i<64;i++)if(fixtures[i].client==client){fixtures[i].channel.inuse=false;fixtures[i].channel.client=NULL;fixtures[i].client=NULL;free(client);return;}
 abort();
}
static struct fixture_session *late_borrow;
static u64 late_borrow_floor;
static struct fixture_session *post_handoff_fault;
static struct hfs_controlled_mount *post_handoff_saved;
static unsigned post_fault_puts,post_fault_clients;
void primitive_after_mutex_unlock(struct mutex *lock) {
 if(lock==&hfs_controlled_lock&&post_handoff_fault&&post_handoff_fault->session->terminal_verified){
  post_handoff_saved=post_handoff_fault->session->terminal_mount;assert(post_handoff_saved);
  post_fault_puts=module_put_count;post_fault_clients=client_destroy_count;
  post_handoff_fault->session->terminal_mount=NULL;post_handoff_fault=NULL;
 }
 if(lock==&hfs_controlled_lock&&late_borrow&&hfs_controlled_mount(&(*late_borrow->session))->ordinary_floor==late_borrow_floor){
  struct fixture_session *f=late_borrow;late_borrow=NULL;
  struct hfs_request *request=container_of((*f->session).requests.next,struct hfs_request,list);
  assert(!request->users&&request->result&&request->terminal&&request->adopted&&!f->ordinary_result);
  assert(!hfs_request_result_borrow(request,&f->ordinary_result));assert(request->users==1);
 }
}
static Hfs9p *carrier;
static uuid_t primitive_supervised_controller;
static u64 primitive_native_nodes[3];
static unsigned current_command;
static char fault_operation[32];
static unsigned fault_mode;
static bool fault_reply;
static size_t fault_scratch, pending_scratch;
bool primitive_allocation_size_failure(size_t length) {
 if(fault_scratch&&length==fault_scratch){fault_scratch=0;return true;}return false;
}
static char *read_line(void) { char *line=NULL;size_t cap=0;ssize_t n=getline(&line,&cap,stdin);assert(n>0);if(line[n-1]=='\n')line[n-1]=0;return line; }
static void emit_hex(const void *bytes,size_t length) { const u8 *p=bytes;for(size_t i=0;i<length;i++)printf("%02x",p[i]); }
static size_t decode_hex(const char *s,void *out,size_t capacity) { size_t n=strlen(s)/2;assert(strlen(s)%2==0&&n<=capacity);u8 *p=out;for(size_t i=0;i<n;i++){int a=_hex_digit(s[i*2]),b=_hex_digit(s[i*2+1]);assert(a>=0&&b>=0);p[i]=a*16+b;}return n; }
static void emit_snapshot(void) { if(getenv("HFS_C6_BACKEND_ONLY"))return; struct hfs_buf snapshot={0};assert(!hfs_buf_init(&snapshot,HFS_ADMISSION_MAX));assert(!hfs_controlled_snapshot(&snapshot));printf("S ");emit_hex(snapshot.data,snapshot.len);puts("");fflush(stdout);hfs_buf_free(&snapshot); }
int primitive_fstat(int fd,struct stat *st) { assert(fd>=10&&fd<=12);memset(st,0,sizeof(*st));st->st_mode=S_IFREG|0600;st->st_ino=primitive_native_nodes[fd-10];st->st_size=HFS9P_VSIZE;return 0; }
ssize_t primitive_pwrite(int fd,const void *bytes,size_t length,off_t offset) {
 assert(fd==10);emit_snapshot();printf("W %llu ",(unsigned long long)offset);emit_hex(bytes,length);puts("");fflush(stdout);
 char *line=read_line();ssize_t result=-1;if(!strncmp(line,"OK ",3))result=strtoll(line+3,NULL,10);else errno=EIO;free(line);return result;
}
ssize_t primitive_pread(int fd,void *bytes,size_t length,off_t offset) {
 assert(fd==11||fd==12);emit_snapshot();printf("R %d %llu %zu\n",fd,(unsigned long long)offset,length);fflush(stdout);
 char *line=read_line();ssize_t result=-1;if(!strncmp(line,"DATA ",5))result=decode_hex(line+5,bytes,length);else errno=EAGAIN;free(line);return result;
}
static int native_identity(void *opaque,const Hfs9pEndpoint *endpoint,const int fd[3]) {
 (void)opaque;struct stat st;for(unsigned i=0;i<3;i++){if(fd[i]!=(int)(10+i)||primitive_fstat(fd[i],&st)||!S_ISREG(st.st_mode)||st.st_size!=HFS9P_VSIZE||st.st_ino!=(ino_t)endpoint->node[i])return EACCES;}return 0;
}
/* Native supervisor identity is a declared OS primitive in this harness.
 * Its bytes come from the running real Go supervisor and must also match the
 * independently verified signed guest provision in guest mode. Exporter-only
 * mode explicitly supplies this privileged OS identity, without kernel proof. */
static int native_controller(void *opaque,const Hfs9pEndpoint *endpoint,const int fd[3],u8 out[16]) {
 int err=native_identity(opaque,endpoint,fd);if(err)return err;
 if(!getenv("HFS_C6_BACKEND_ONLY"))for(u32 i=0;i<hfs_controlled_registry->nr_groups;i++)
  if(!hfs_controlled_registry->groups[i].bound||!uuid_equal(&primitive_supervised_controller,&hfs_controlled_registry->groups[i].controller))return EACCES;
 memcpy(out,primitive_supervised_controller.b,16);return 0;
}
static int claim_cb(void *arg,const Hfs9pEndpoint *e,const u8 hash[32]) { return hfs9p_ledger_claim(arg,e,hash); }
static int protect_cb(void *arg,const u8 *p,size_t n) { return hfs9p_ledger_protect(arg,p,n); }
static int primitive_rpc_exchange(struct p9_client *client,u16 tag,u8 op,u8 channel,u64 cell,u32 chunk,const void *request,u32 request_len,void *reply,u32 *reply_len) {
 unsigned index;for(index=0;index<64;index++)if(client==&(*fixtures[index].client))break;assert(index<carrier->session_count);
 int err;if(op==1){
  if(fault_mode){
   const void *body=NULL;u32 total=0,body_len=0;u8 hash[32];struct hfs_json *original=NULL;
   assert(!hfs_frame_decode_version(4,request,request_len,1,&(*fixtures[index].session).enrollment.session,cell,chunk,&total,hash,&body,&body_len));
   assert(!chunk&&total==body_len&&!hfs_json_parse(body,body_len,&original));
   char operation[32];assert(hfs_json_string(original,hfs_json_field(original,0,"op"),operation,sizeof(operation))>=0);hfs_json_free(original);
   if(!strcmp(operation,fault_operation)){unsigned mode=fault_mode;fault_mode=0;if(mode==1)return -EIO;if(mode==2)fault_reply=true;else pending_scratch=!strcmp(operation,"RETIRE")?HFS_BODY_MAX:HFS_VIEW_BODY_MAX;}
  }
  err=_send(carrier,index,tag,request,request_len);if(err){fprintf(stderr,"actual native SEND error %d tag%u cell%llu\n",err,tag,cell);return -err;}assert(*reply_len>=1);*(u8 *)reply=1;*reply_len=1;return 0;}
 assert(op==2&&*reply_len>1);size_t length=0;err=_fetch(carrier,index,cell,chunk,channel,(u8 *)reply+1,*reply_len-1,&length);
 if(err==EAGAIN){*(u8 *)reply=2;*reply_len=1;return 0;}if(err){fprintf(stderr,"actual native FETCH error %d cell%llu channel%u revoked%d pending%d\n",err,cell,channel,carrier->revoked,carrier->pending);return -err;}if(fault_reply){fault_reply=false;return -EIO;}if(pending_scratch){fault_scratch=pending_scratch;pending_scratch=0;}*(u8 *)reply=3;*reply_len=length+1;return 0;
}
/* Actual net9p exchange body, with the native RPC/tag allocator substituted.
 * P9_NOTAG is a request for an allocated ordinary tag, never a wire tag.
 */
#define P9_TBRIDGE 240
#define P9_RBRIDGE 241
#define p9_proto_hfs1 0
#define p9_proto_hfs11 1
#define p9_proto_hfs12 2
#define p9_proto_hfs13 3
struct p9_fcall { u32 size,offset;u8 id;u16 tag;u8 *sdata; };
struct p9_req_t { struct p9_fcall tc,rc; };
static struct p9_req_t *p9_client_rpc_tag(struct p9_client *client,s8 type,int wanted,const char *format,...) {
 assert((u8)type==P9_TBRIDGE&&!strcmp(format,"bbqdB"));
 va_list args;va_start(args,format);int method=va_arg(args,int),channel=va_arg(args,int);u64 cell=va_arg(args,u64);u32 chunk=va_arg(args,u32),length=va_arg(args,u32);const void *frame=va_arg(args,const void *);va_end(args);
 u16 tag=wanted<0?42:wanted;assert(tag<65535);

 struct p9_req_t *request=calloc(1,sizeof(*request));assert(request);request->rc.sdata=malloc(HFS_HEADER_SIZE+HFS_CHUNK_MAX+1);assert(request->rc.sdata);
 u32 count=HFS_HEADER_SIZE+HFS_CHUNK_MAX+1;int err=primitive_rpc_exchange(client,tag,method,channel,cell,chunk,frame,length,request->rc.sdata,&count);
 if(err){printf("L rpc returned error%d method%d cell%llu\n",err,method,cell);fflush(stdout);free(request->rc.sdata);free(request);return (void *)(intptr_t)err;}
 request->rc.size=count;request->rc.id=P9_RBRIDGE;request->rc.tag=request->tc.tag=tag;return request;
}
static void p9_req_put(struct p9_client *client,struct p9_req_t *request) { (void)client;free(request->rc.sdata);free(request); }
/* The substituted RPC owns no idr slot, so a fixed tag is always idle here;
 * tools/integration/linux9p/tests/tag runs the actual idle wait (964).
 */
static int p9_client_hfs_tag_idle(struct p9_client *client,u16 tag) { (void)client;(void)tag;return 0; }
#include "client_exchange.inc"
static void command(char *line);
void primitive_sleep(unsigned long lower,unsigned long upper) {
 (void)upper;printf("Y %u\n",current_command);fflush(stdout);
 for(;;){char *line=read_line();if(!strcmp(line,"CONTINUE")){free(line);break;}unsigned saved=current_command;command(line);current_command=saved;free(line);}
 usleep(lower);
}
/* The portable fixture has no background controller. Actual helper progress is
 * driven through PHASE; pressure never invents an ACK or proof. */
void hfs_mmap_progress(struct hfs_session *session) { (void)session; }
static void copy_json(const struct hfs_json *json,int token,struct hfs_json **out) { assert(token>=0);assert(!hfs_json_parse(json->data+json->tokens[token].start,json->tokens[token].end-json->tokens[token].start,out)); }
static int number(const struct hfs_json *j,int object,const char *name,u64 *out) { return hfs_json_u64(j,hfs_json_field(j,object,name),out); }
static void credential_init(struct fixture_session *f,const struct hfs_json *j,int token) {
 struct hfs_cred *c=&f->credential;memset(c,0,sizeof(*c));memset(&f->credentials,0,sizeof(f->credentials));u64 n;u32 length=32;
 assert(!number(j,token,"credential_revision",&c->revision));
 assert(!number(j,token,"fsuid",&n)&&n<=UINT32_MAX);c->uid=n;
 assert(!number(j,token,"fsgid",&n)&&n<=UINT32_MAX);c->gid=n;
 assert(!hfs_uuid(j,hfs_json_field(j,token,"process"),&c->process));
 assert(!hfs_uuid(j,hfs_json_field(j,token,"user_namespace"),&c->user_namespace));
 assert(!hfs_json_hex(j,hfs_json_field(j,token,"idmap_hash"),c->idmap_hash,&length)&&length==32);
 int groups=hfs_json_field(j,token,"groups");assert(groups>=0);
 for(u32 i=groups+1;i<j->count;i++)if(j->tokens[i].parent==(u32)groups){assert(c->ngroups<HFS_GROUPS_MAX&&!hfs_json_u64(j,i,&n)&&n<=UINT32_MAX);c->groups[c->ngroups++]=n;}
 f->groups.ngroups=c->ngroups;memcpy(f->groups.gid,c->groups,c->ngroups*sizeof(c->groups[0]));
 f->native_cred=(struct cred){.user_ns=&init_user_ns,.fsuid=c->uid,.fsgid=c->gid,.group_info=&f->groups,.refs=1};c->native=&f->native_cred;
 assert(!hfs_buf_init(&f->credentials,16384));hfs_cred_write(&f->credentials,c);assert(!f->credentials.error);
 assert(f->credentials.len==j->tokens[token].end-j->tokens[token].start&&!memcmp(f->credentials.data,j->data+j->tokens[token].start,f->credentials.len));
}
#include "bootstrap_test.inc"
static void fixture_set_current(struct fixture_session *f) {
 f->groups.ngroups=f->credential.ngroups;
 memcpy(f->groups.gid,f->credential.groups,f->credential.ngroups*sizeof(f->groups.gid[0]));
 f->native_cred.fsuid=f->credential.uid;f->native_cred.fsgid=f->credential.gid;
 f->native_cred.group_info=&f->groups;f->native_cred.user_ns=&init_user_ns;
 if(!f->native_cred.refs)f->native_cred.refs=1;
 if(!f->pid.refs)f->pid=(struct pid){.number=100+(int)(f-fixtures),.refs=1};
 primitive_current_task=(struct task_struct){.pid=&f->pid};
 primitive_current_cred=primitive_real_cred=&f->native_cred;
}
static void session_init(unsigned i) {
 struct fixture_session *f=&fixtures[i];
 f->session=calloc(1,sizeof(*f->session));f->client=calloc(1,sizeof(*f->client));assert(f->session&&f->client);
 atomic_inc_return(&hfs_active_sessions);
 struct hfs_session *s=f->session;
 struct hfs_controlled_mount *mount=hfs_controlled_registry->mounts[i];
 s->provider=hfs_provider;s->bridge_minor=4;s->client=&(*f->client);s->next_cell=1;s->next_claim=1;s->next_close=s->next_release=1;
 mutex_init(&s->lock);mutex_init(&s->writer);mutex_init(&s->retirement);spin_lock_init(&s->processes_lock);
 INIT_LIST_HEAD(&s->global);INIT_LIST_HEAD(&s->descriptions);INIT_LIST_HEAD(&s->processes);INIT_LIST_HEAD(&s->forgets);INIT_LIST_HEAD(&s->object_numbers);
 INIT_LIST_HEAD(&s->uncertain);INIT_LIST_HEAD(&s->requests);INIT_LIST_HEAD(&s->view_tickets);INIT_LIST_HEAD(&s->view_families);
 assert(!s->provider->enroll(mount->device_tag,&s->enrollment));
 assert(!hfs_enrollment_validate(&s->enrollment));
 assert(!hfs_enrollment_lanes(&s->enrollment,s->bridge_minor));
 mount_lane_regression(&s->enrollment);
 f->device.dev=(struct device){.references=1,.registered=true,.name=mount->device_name};
 f->device.priv=&f->channel;f->channel=(struct virtio_chan){.inuse=true,.client=&(*f->client),.vdev=&f->device,.tag=mount->device_tag};
 list_add_tail(&f->channel.chan_list,&virtio_chan_list);
 (*f->client)=(struct p9_client){.trans=&f->channel,.trans_mod=&p9_virtio_trans,.status=Connected,.proto_version=p9_proto_hfs14,.msize=65536};
 mutex_init(&(*f->client).hfs_control_tag);mutex_init(&(*f->client).hfs_wait_tag);
 assert(!hfs_controlled_bind_transport(s));
 bootstrap_failure_regression(f);
 const char *selected=hfs_protocol_selected_text(4,s->enrollment.protocol_features);
 struct hfs_buf hello={.data=(char *)selected,.len=strlen(selected)};
 struct hfs_json *response=NULL;
 /* Direct pre-super operations must refuse before touching native transport. */
 assert(hfs_control(s,"ALLOCATE",&hello,false,&response)<0&&!response);
 assert(hfs_control(s,"HELLO",&hello,true,&response)<0&&!response);
 struct hfs_buf malformed={.data="{}",.len=2};assert(hfs_control(s,"HELLO",&malformed,false,&response)<0&&!response);
 u64 hello_before=hfs_controlled_control_used;u32 hello_credits=hfs_controlled_nr_control_credits;
 assert(!hfs_control(s,"HELLO",&hello,false,&response));assert(response->owner&&response->owner_put==hfs_candidate_parse_put);
 assert(hfs_controlled_control_used>hello_before&&hfs_controlled_nr_control_credits==hello_credits+1&&hfs_received_require(s,response,0)<0);
 hello_parse=response;hello_tokens=response->tokens;hello_data=response->data;hello_candidate=response->owner;hello_freed=0;
 hfs_json_free(response);assert(hello_freed==15&&hfs_controlled_control_used==hello_before&&hfs_controlled_nr_control_credits==hello_credits);
 hello_parse=hello_data=hello_tokens=hello_candidate=NULL;
 w54_regressions(f,!i);
 f->super=(struct super_block){.s_fs_info=s,.s_dev=1000+i,.s_user_ns=&init_user_ns};
 /* Floor unit: bind_super charges exactly its 2|3 progress lane packages. */
 bool terminal=s->enrollment.protocol_features==HFS_TERMINAL_FEATURES;
 struct floor_mark lanes=floor_mark(s);
 assert(!hfs_controlled_bind_super(s,&f->super));
 {
  struct floor_mark bound=floor_mark(s);
  assert(bound.control-lanes.control==(terminal?3U:2U)*(1U+HFS_COMPLETION_SLOTS)&&bound.ordinary==lanes.ordinary);
  assert(bound.control_received-lanes.control_received==HFS_MMAP_EVENTS_MAX+3U+2U+terminal&&bound.ordinary_received==lanes.ordinary_received);
 }
 {
  struct hfs_session profile={0};const char *ops[]={"DESCRIPTION_CLOSE","DESCRIPTION_RELEASE","FORGET","PROCESS_EXIT"};
  const u64 features[]={HFS_MAPPED_FEATURES,HFS_TERMINAL_FEATURES,HFS_CONTROLLED_FEATURES,HFS_CLEANUP_FEATURES,0,32766};
  for(unsigned minor=0;minor<=5;minor++)for(unsigned n=0;n<ARRAY_SIZE(features);n++){
   profile.bridge_minor=minor;profile.enrollment.protocol_features=features[n];bool ordinary=minor==4&&(features[n]==HFS_MAPPED_FEATURES||features[n]==HFS_TERMINAL_FEATURES);
   for(unsigned j=0;j<ARRAY_SIZE(ops);j++){assert(hfs_call_control_bytes(&profile,ops[j],true)==!ordinary);assert(!hfs_call_control_bytes(&profile,ops[j],false));}
   assert(hfs_call_control_bytes(&profile,"DRAIN_WRITE",true));assert(hfs_call_control_bytes(&profile,"ROOT",true));
  }
 }
 accounting_regression(s);
 uuid_t user_namespace;u8 map[32];assert(!s->provider->map(s,&f->native_cred,&init_user_ns,&user_namespace,map));
 assert(uuid_equal(&user_namespace,&f->credential.user_namespace)&&!memcmp(map,f->credential.idmap_hash,32));
 if(s->enrollment.protocol_features==HFS_TERMINAL_FEATURES){
  struct hfs_cred *captured=NULL;struct hfs_buf bytes={0};fixture_set_current(f);
  primitive_process_uuid=f->credential.process;primitive_process_uuid_pending=true;
  u32 identities=hfs_controlled_mount(s)->control_credits; /* floor unit: one per new process */
  assert(!hfs_capture(s,&init_user_ns,&captured)&&!primitive_process_uuid_pending);
  assert(hfs_controlled_mount(s)->control_credits==identities+1);
  assert(!hfs_buf_init(&bytes,HFS_TERMINAL_OBJECT_MAX));hfs_cred_write(&bytes,captured);
  assert(!bytes.error&&bytes.len==f->credentials.len&&!memcmp(bytes.data,f->credentials.data,bytes.len));
  hfs_buf_free(&bytes);hfs_cred_free(captured);
 }
}
static void done(unsigned id,int error,const void *body,size_t length) { printf("D %u %d ",id,error);if(body&&length)emit_hex(body,length);else printf("-");puts("");fflush(stdout); }
static int fixture_result(struct fixture_session *f,const char *opcode) {
 if(f->actual_result)return hfs_result_validate(&(*f->session),f->ordinary_result,opcode,&f->credentials);
 struct hfs_json *reply=f->latest,*result=NULL;if(!reply)return -EINVAL;
 int payload=hfs_json_field(reply,0,"payload");
 if(!hfs_json_is(reply,hfs_json_field(reply,payload,"status"),"\"TERMINAL\""))return -EPROTO;
 int token=hfs_json_field(reply,payload,"result");int err=hfs_received_copy(reply,token,&result);
 if(!err)err=hfs_result_validate(&(*f->session),result,opcode,&f->credentials);
 if(err){hfs_json_free(result);return err;}
 hfs_json_free(f->ordinary_result);f->ordinary_result=result;return 0;
}
static int fixture_check(struct fixture_session *f,const char *check) {
 struct hfs_session *s=&(*f->session);struct hfs_json *json=NULL,*forged=NULL;
 int token=-1,err=-EINVAL;void *cookie=NULL;
 if(!strcmp(check,"cookie_balance"))return hfs_controlled_nr_cookies?-EUCLEAN:0;
 if(!strcmp(check,"cap")){struct hfs_cap *cap=f->description?f->description:f->root;if(!cap)return -EINVAL;u32 before=hfs_controlled_nr_cookies;err=s->provider->enter(s,cap,false,&cookie);if(!err){if(!cookie||hfs_controlled_nr_cookies!=before+1)return -EUCLEAN;s->provider->leave(s,cookie);if(hfs_controlled_nr_cookies!=before)return -EUCLEAN;}return err;}
 if(!strcmp(check,"retirement_refuses")){u8 zero[32]={0};return s->provider->retirement(s,1,zero,f->latest)<0&&s->provider->view_retirement(s,&f->credentials,1,zero,f->latest)<0&&s->provider->retire_session(s)<0?0:-EUCLEAN;}
 if(!strcmp(check,"bad_device")){
  f->device.dev.registered=false;err=hfs_controlled_admissible(s);f->device.dev.registered=true;if(err>=0)return -EUCLEAN;
  struct virtio_device impostor={.dev={.references=1,.registered=true,.name=f->device.dev.name},.priv=&f->channel};
  f->channel.vdev=&impostor;err=hfs_controlled_admissible(s);f->channel.vdev=&f->device;
  return err<0&&impostor.dev.references==1?0:-EUCLEAN;
 }
 if(!strcmp(check,"bad_map")){init_user_ns.uid_map.extent[0].count--;err=hfs_controlled_admissible(s);init_user_ns.uid_map.extent[0].count++;return err<0?0:-EUCLEAN;}
 if(!strcmp(check,"nonnull_publication")){
  struct hfs_json *result=f->ordinary_result;if(!result)return -EINVAL;int publication=hfs_json_field(result,0,"publication");
  if(publication<0||!hfs_json_is(result,publication,"null"))return -EINVAL;
  char *value=result->data+result->tokens[publication].start;memcpy(value,"true",4);err=s->provider->publication(s,result,publication);memcpy(value,"null",4);
  return err<0?0:-EUCLEAN;
 }
 if(!strcmp(check,"wrong_channel")){
  /* Actual private ordinary RESULT cannot become SOURCE/EVENT, nor can an
   * actual private recall EVENT be reinterpreted as a result ACK. */
  if(!f->ordinary_result)return -EINVAL;
  if(s->provider->target(s,f->ordinary_result,0)>=0||s->provider->view_event(s,f->ordinary_result,0)>=0)return -EUCLEAN;
  if(f->latest){struct hfs_buf payload={.data="{}",.len=2};if(hfs_received_acknowledgement(s,f->latest,&payload)>=0)return -EUCLEAN;}
  return 0;
 }
 if(!strcmp(check,"stale_target")){if(!f->lease)return -EINVAL;return s->provider->target(s,f->lease->json,hfs_json_field(f->lease->json,0,"target"))<0?0:-EUCLEAN;}
 bool detached=!strncmp(check,"forged_",7);const char *kind=detached?check+7:check;
 if(!strcmp(kind,"target")){if(!f->lease)return -EINVAL;json=f->lease->json;token=hfs_json_field(json,0,"target");}
 else if(!strcmp(kind,"publication")){json=f->ordinary_result;if(json)token=hfs_json_field(json,0,"publication");}
 else if(!strcmp(kind,"event")){json=f->latest;if(json)token=hfs_json_field(json,hfs_json_field(json,0,"payload"),"event");}
 else return -EINVAL;
 if(!json||token<0)return detached?0:-EINVAL;
 if(detached){if(hfs_json_parse(json->data,json->len,&forged))return -EINVAL;forged->transported=true;json=forged;}
 if(!strcmp(kind,"target"))err=s->provider->target(s,json,token);
 else if(!strcmp(kind,"publication"))err=s->provider->publication(s,json,token);
 else err=s->provider->view_event(s,json,token);
 hfs_json_free(forged);return detached?(err<0?0:-EUCLEAN):err;
}
static void command(char *line) {
 char operation[32];unsigned id,index;int consumed=0;
 assert(sscanf(line,"%31s %u %u %n",operation,&id,&index,&consumed)>=3&&index<(getenv("HFS_C6_BACKEND_ONLY")?carrier->session_count:hfs_controlled_registry->nr_mounts));
 current_command=id;struct fixture_session *f=&fixtures[index];fixture_commands++;
 /* Backend-only component mode deliberately has no Linux session/Received
  * authority. It uses the same real native carrier and protected exporter. */
 if(!strcmp(operation,"BACKEND_ADD")){
  assert(getenv("HFS_C6_BACKEND_ONLY"));
  u8 context[4096];size_t n=decode_hex(line+consumed,context,sizeof(context));
  int err=hfs9p_add_session_tuple(carrier,context,n,4,32767);
  done(id,-err,NULL,0);return;
 }
 if(!strcmp(operation,"BACKEND_STATS")){
  unsigned rows=0,old=0;bool removed=false;for(unsigned i=0;i<HFS9P_ENTRIES;i++){
   Hfs9pEntry *e=&carrier->entries[i];if(!e->guest||e->session!=index)continue;
   rows++;if(e->terminal_state){old=e->terminal_state->count;removed=e->terminal_state->removed;}
  }
  char state[256];int n=snprintf(state,sizeof(state),"{\"rows\":%u,\"ordinary\":%u,\"control\":%u,\"high\":%llu,\"old\":%u,\"removed\":%s,\"phase\":%u,\"package_bytes\":%zu}",rows,carrier->ordinary,carrier->control,(unsigned long long)carrier->high,old,removed?"true":"false",(unsigned)carrier->sessions[index].terminal_phase,carrier->sessions[index].terminal_package?sizeof(Hfs9pTerminalPackage):0);
  done(id,0,state,n);return;
 }
 if(!strcmp(operation,"BACKEND_EXCHANGE")){
  assert(getenv("HFS_C6_BACKEND_ONLY"));
  struct hfs_buf body={0};assert(!hfs_buf_init(&body,HFS_BODY_MAX));body.len=decode_hex(line+consumed,body.data,body.cap);
  struct hfs_json *json=NULL;assert(!hfs_json_parse(body.data,body.len,&json));u64 cell;uuid_t session;
  assert(!number(json,0,"cell",&cell)&&!hfs_uuid(json,hfs_json_field(json,hfs_json_field(json,0,"context"),"session"),&session));
  char opcode[32];assert(hfs_json_string(json,hfs_json_field(json,0,"op"),opcode,sizeof(opcode))>=0);unsigned tag=!strcmp(opcode,"HELLO")?42:65533;hfs_json_free(json);
  u8 *frame=malloc(HFS_HEADER_SIZE+HFS_CHUNK_MAX),*assembled=malloc(HFS_BODY_MAX);assert(frame&&assembled);
  int err=0;u64 deadline=jiffies+30*HZ;
  for(u32 chunk=0;chunk*HFS_CHUNK_MAX<body.len&&!err;chunk++){
   u32 n=HFS_HEADER_SIZE+HFS_CHUNK_MAX;assert(!hfs_frame_encode_version(4,1,&session,cell,body.data,body.len,chunk,frame,&n));
   do{err=_send(carrier,index,tag,frame,n);if(err==EAGAIN)primitive_sleep(1000,2000);}while(err==EAGAIN&&!time_after_eq(jiffies,deadline));
  }
  u32 total=0;u8 expected[32]={0};
  for(u32 chunk=0;!err;chunk++){
   size_t n=0;do{err=_fetch(carrier,index,cell,chunk,HFS_CHANNEL_RESULT,frame,HFS_HEADER_SIZE+HFS_CHUNK_MAX,&n);if(err==EAGAIN)primitive_sleep(1000,2000);}while(err==EAGAIN&&!time_after_eq(jiffies,deadline));
   if(err)break;const void *bytes;u32 length,amount;u8 hash[32];assert(!hfs_frame_decode_version(4,frame,n,HFS_FRAME_RESULT,&session,cell,chunk,&length,hash,&bytes,&amount));
   if(!chunk){total=length;memcpy(expected,hash,32);}else assert(total==length&&!memcmp(expected,hash,32));
   memcpy(assembled+chunk*HFS_CHUNK_MAX,bytes,amount);if((chunk+1)*HFS_CHUNK_MAX>=total)break;
  }
  if(!err){u8 hash[32];hfs_hash(assembled,total,hash);assert(!memcmp(hash,expected,32));}
  done(id,-err,err?NULL:assembled,err?0:total);free(assembled);free(frame);hfs_buf_free(&body);return;
 }
 if(!strcmp(operation,"TOMBSTONE")){
  struct hfs_controlled_mount *mount=hfs_controlled_registry->mounts[index];
  assert(f->pid.refs==1&&f->native_cred.refs==1);
  assert(f->destroyed&&!f->session&&!f->client&&!mount->session&&mount->claimed&&mount->terminal_retired&&mount->terminal_length);
  assert(!mount->super&&!mount->super_device);
  assert(!mount->cookies&&!mount->ordinary_credits&&!mount->control_credits&&!mount->ordinary_received&&!mount->control_received);
  unsigned native_rows=0;
  for(unsigned k=0;k<HFS9P_ENTRIES;k++){
   Hfs9pEntry *entry=&carrier->entries[k];if(!entry->guest||entry->session!=index)continue;
   native_rows++;assert(entry->terminal&&entry->issued&&entry->responded&&entry->terminal_state&&entry->terminal_state->removed);
   assert(entry->guest_cell==mount->terminal_through+1&&entry->native_cell==carrier->sessions[index].terminal_native&&entry->native_cell<=carrier->high);
  }
  assert(native_rows==1&&carrier->sessions[index].terminal_phase==3);
  struct hfs_enrollment enrollment={0};assert(hfs_controlled_provider.enroll(mount->device_tag,&enrollment)<0);
  struct hfs_buf observer={0};assert(!hfs_buf_init(&observer,HFS_ADMISSION_MAX));assert(!hfs_controlled_snapshot(&observer));
  done(id,0,observer.data,observer.len);hfs_buf_free(&observer);return;
 }
 assert(f->session&&!f->destroyed);fixture_set_current(f);
 if(!strcmp(operation,"DESTROY")||!strcmp(operation,"HANDOFF")){
  struct hfs_session *session=f->session;
  if(!session->closing){int err=hfs_controlled_begin_close(session);if(err){done(id,err,NULL,0);return;}
   session->closing=true;session->retained=true;(void)session->provider->stop(session);session->provider->retain(session);}
  unsigned puts=module_put_count,clients=client_destroy_count,active=atomic_read(&hfs_active_sessions);bool already_verified=session->terminal_verified;
  int err=!strcmp(operation,"HANDOFF")?session->provider->retire_session(session):hfs_session_destroy(session);
  if(err){struct hfs_candidate *candidate=list_empty(&session->uncertain)?NULL:container_of(session->uncertain.next,struct hfs_candidate,list);printf("L destroy error%d pending%d candidatebytes%u clientmsize%u proto%d\n",err,session->terminal_pending,candidate?candidate->command.len:0,session->client->msize,session->client->proto_version);fflush(stdout);}
  if(err)fprintf(stderr,"actual destroy error %d pending%d cell%llu through%llu payload%.*s\n",err,session->terminal_pending,session->terminal_cell,session->next_cell-1,session->terminal_payload.len,session->terminal_payload.data?session->terminal_payload.data:"");
  if(err){struct hfs_view *view=session->view;struct hfs_mmap_control *ctl=view?view->control:NULL;
   fprintf(stderr,"destroy inventory requests%u viewtickets%u ticketsbusy%d view%p cached%u dirty%u packages%u active%u grants%u viewrefs%u control%p controlrefs%u count%u delivered%llu completed%llu recall%d drained%d\n",session->nr_requests,session->nr_view_tickets,hfs_view_tickets_busy(session),view,view?view->nr_cached:0,view?view->nr_dirty:0,view?view->nr_dirty_packages:0,view?view->active:0,view?view->nr_grants:0,view?refcount_read(&view->refs.refcount):0,ctl,ctl?refcount_read(&ctl->refs.refcount):0,ctl?ctl->count:0,ctl?ctl->delivered:0,ctl?ctl->completed:0,ctl?ctl->have_recall:0,ctl?ctl->drained_acked:0);
   struct hfs_controlled_mount *m=hfs_controlled_mount(session);
   fprintf(stderr,"mount ordinary%u control%u received%u/%u progressidle%d/%d used%llu/%llu\n",m->ordinary_credits,m->control_credits,m->ordinary_received,m->control_received,hfs_completion_idle(session->control_completion),hfs_completion_idle(session->retirement_completion),session->control_completion?session->control_completion->used:0,session->retirement_completion?session->retirement_completion->used:0);
   struct hfs_received_link *link;list_for_each_entry(link,&hfs_received_links,list)if(link->owner->session==session){struct hfs_kernel_received *owner=link->owner;int op=hfs_json_field(owner->original,0,"op");fprintf(stderr,"retained Received cell%llu kind%d refs%u span%u/%u op%.*s\n",owner->cell,owner->kind,refcount_read(&owner->refs.refcount),link->start,link->length,owner->original->tokens[op].end-owner->original->tokens[op].start,owner->original->data+owner->original->tokens[op].start);}

  }

  if(!err&&strcmp(operation,"HANDOFF")){assert((already_verified?module_put_count==puts+2:module_put_count>=puts+2)&&client_destroy_count==clients+1&&atomic_read(&hfs_active_sessions)==active-1);f->session=NULL;f->destroyed=true;f->super.s_fs_info=NULL;assert(!f->client);}
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"ARM_POST_HANDOFF_FAULT")){
  assert(!post_handoff_fault&&!post_handoff_saved&&!f->session->terminal_verified);post_handoff_fault=f;done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"RESTORE_POST_HANDOFF")){
  assert(post_handoff_saved&&!f->session->terminal_mount&&f->session->terminal_verified&&post_handoff_saved->session==f->session);
  f->session->terminal_mount=post_handoff_saved;post_handoff_saved=NULL;done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"KERNEL_STATS")){
  struct hfs_session *s=f->session;unsigned ordinary=0,control=0,reserved=0,unknown=0,links=0,old_ordinary=0,old_control=0;
  for(unsigned i=0;i<ARRAY_SIZE(s->cells);i++){if(s->cells[i].active){assert(s->cells[i].ack_ready);if(i<64)ordinary++;else control++;}reserved+=s->cells[i].reserved;}
  struct hfs_candidate *candidate;list_for_each_entry(candidate,&s->uncertain,list)unknown++;
  struct hfs_received_link *link;list_for_each_entry(link,&hfs_received_links,list)if(link->owner->session==s)links++;
  for(unsigned i=0;i<s->nr_terminal_cells;i++)if(s->terminal_cells[i].control)old_control++;else old_ordinary++;
  struct hfs_controlled_mount *m=hfs_controlled_mount(s);char state[768];
  int n=snprintf(state,sizeof(state),"{\"ordinary\":%u,\"control\":%u,\"reserved\":%u,\"unknown\":%u,\"links\":%u,\"ordinary_credits\":%u,\"control_credits\":%u,\"ordinary_received\":%u,\"control_received\":%u,\"captured_ordinary\":%u,\"captured_control\":%u,\"verified\":%s,\"pending\":%s,\"module_puts\":%u,\"client_destroys\":%u,\"fault_module_puts\":%u,\"fault_clients\":%u,\"active\":%u,\"progress_parents\":%s}",ordinary,control,reserved,unknown,links,m->ordinary_credits,m->control_credits,m->ordinary_received,m->control_received,old_ordinary,old_control,s->terminal_verified?"true":"false",s->terminal_pending?"true":"false",module_put_count,client_destroy_count,post_fault_puts,post_fault_clients,atomic_read(&hfs_active_sessions),s->control_completion||s->retirement_completion||s->terminal_completion?"true":"false");
  done(id,0,state,n);return;
 }
 if(!strcmp(operation,"REPLAY_CONTROL")){
  unsigned reserved;char op[32];int used=0;assert(sscanf(line+consumed,"%u %31s %n",&reserved,op,&used)==2&&reserved<=1);
  struct hfs_buf payload={0};struct hfs_json *reply=NULL;assert(!hfs_buf_init(&payload,HFS_BODY_MAX));payload.len=decode_hex(line+consumed+used,payload.data,payload.cap);
  int err=hfs_control(f->session,op,&payload,reserved,&reply);
  if(!err){int body=hfs_json_field(reply,0,"payload");assert(hfs_json_is(reply,hfs_json_field(reply,body,"status"),"\"RETIRED_ID\""));assert(!hfs_received_require(f->session,reply,0));assert(hfs_received_source(f->session,reply,0)<0);}
  done(id,err,NULL,0);hfs_json_free(reply);hfs_buf_free(&payload);return;
 }
 if(!strcmp(operation,"POST_HANDOFF_FAILURE")){
  struct hfs_session *session=f->session;struct hfs_controlled_mount *mount=session->terminal_mount;
  assert(session->terminal_verified&&mount&&mount->terminal_retired&&mount->session==session);
  unsigned puts=module_put_count,clients=client_destroy_count,active=atomic_read(&hfs_active_sessions);
  u64 next=session->next_cell;struct p9_client *client=session->client;
  /* Negative continuity fault only: preserve the actual charged tombstone.
   * Restoring its original owner pointer creates no new receipt/authority. */
  session->terminal_mount=NULL;
  assert(hfs_session_destroy(session)==-ESTALE);
  assert(module_put_count==puts&&client_destroy_count==clients&&atomic_read(&hfs_active_sessions)==active);
  assert(session->terminal_verified&&session->retired&&session->closing&&session->retained&&session->client==client&&session->next_cell==next&&mount->session==session&&mount->terminal_retired);
  session->terminal_mount=mount;done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"TERMINAL_CANDIDATE_NEGATIVES")){
  struct hfs_session *session=f->session;struct hfs_candidate *candidate=hfs_terminal_candidate(session,&session->terminal_payload);assert(candidate);
  struct hfs_response_profile profile=candidate->response;candidate->response.channel=HFS_CHANNEL_RECALL;
  assert(!hfs_terminal_candidate(session,&session->terminal_payload));candidate->response=profile;
  u64 original=candidate->cell;candidate->cell++;assert(!hfs_terminal_candidate(session,&session->terminal_payload));candidate->cell=original;
  original=candidate->donor;candidate->donor++;assert(!hfs_terminal_candidate(session,&session->terminal_payload));candidate->donor=original;
  u32 slot=session->terminal_slot;session->terminal_slot=ARRAY_SIZE(session->cells);assert(!hfs_terminal_candidate(session,&session->terminal_payload));session->terminal_slot=slot;
  struct hfs_json *command=NULL;assert(!hfs_json_parse(candidate->command.data,candidate->command.len,&command));
  int protocol=hfs_json_field(command,0,"protocol"),feature=hfs_json_field(command,protocol,"features");assert(feature>=0);
  u32 offset=command->tokens[feature].end-1;char byte=candidate->command.data[offset];candidate->command.data[offset]='6';
  assert(!hfs_terminal_candidate(session,&session->terminal_payload));candidate->command.data[offset]=byte;
  hfs_json_free(command);assert(hfs_terminal_candidate(session,&session->terminal_payload)==candidate);
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"RESERVATION_HOLD")){
  assert(!f->terminal_external_credit);done(id,f->session->provider->reserve(f->session,1,false,&f->terminal_external_credit),NULL,0);return;
 }
 if(!strcmp(operation,"RESERVATION_DROP")){
  assert(f->terminal_external_credit);f->session->provider->unreserve(f->session,f->terminal_external_credit);f->terminal_external_credit=NULL;done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"DOMAIN_HOLD")){
  assert(f->session->domain&&!f->domain_alias);
  int err=hfs_received_copy(f->session->domain->json,0,&f->domain_alias);done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"DOMAIN_DROP")){assert(f->domain_alias);hfs_json_free(f->domain_alias);f->domain_alias=NULL;done(id,0,NULL,0);return;}
 if(!strcmp(operation,"PATH_FORGET")){
  assert(f->created_path);struct hfs_buf args={0};struct hfs_json *result=NULL;
  assert(!hfs_args_init(&args));hfs_buf_literal(&args,"{\"count\":1,\"object\":");hfs_cap_write(&args,f->created_path);hfs_buf_literal(&args,"}");
  int err=hfs_call(f->session,"FORGET",&args,&f->credential,true,&result);
  if(!err&&f->session->enrollment.protocol_features==HFS_TERMINAL_FEATURES){
   struct hfs_request *request=result->owner;assert(request&&request->control&&request->completion);
   struct hfs_controlled_credit *credit=request->reservation;
   assert(credit&&!credit->control&&!hfs_completion_control(request->completion));
  }
  hfs_json_free(result);hfs_buf_free(&args);if(!err){hfs_cap_free(f->created_path);f->created_path=NULL;}
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"DESCRIPTION_CLOSE")){
  assert(f->description);struct hfs_buf args={0};struct hfs_json *result=NULL;
  assert(!hfs_args_init(&args));hfs_buf_literal(&args,"{\"description\":");hfs_cap_write(&args,f->description);
  hfs_buf_printf(&args,",\"release_sequence\":%llu}",f->session->next_release++);
  int err=hfs_call(f->session,"DESCRIPTION_RELEASE",&args,&f->credential,true,&result);
  if(!err&&f->session->enrollment.protocol_features==HFS_TERMINAL_FEATURES){
   struct hfs_request *request=result->owner;assert(request&&request->control&&request->completion);
   struct hfs_controlled_credit *credit=request->reservation;
   assert(credit&&!credit->control&&!hfs_completion_control(request->completion));
  }
  hfs_json_free(result);hfs_buf_free(&args);
  if(!err){hfs_cap_free(f->description);f->description=NULL;}
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"ROOT_DROP")){assert(f->root);hfs_cap_free(f->root);f->root=NULL;done(id,0,NULL,0);return;}
 if(!strcmp(operation,"REPLIES_DROP")){
  hfs_json_free(f->latest);f->latest=NULL;hfs_json_free(f->ordinary_result);f->ordinary_result=NULL;
  while(f->nr_held_results)hfs_json_free(f->held_results[--f->nr_held_results]);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"RELEASE_NEGATIVES")){
  struct hfs_controlled_mount *mount=hfs_controlled_mount(&(*f->session));u32 ordinary=mount->ordinary_credits,control=mount->control_credits;u64 next=(*f->session).next_cell;
  struct hfs_buf ack={0};struct hfs_json *out=NULL;assert(!hfs_buf_init(&ack,256));
  hfs_buf_printf(&ack,"{\"cell\":%llu,\"response_hash\":",(*f->session).cells[0].number);hfs_buf_hex(&ack,(*f->session).cells[0].hash,32);hfs_buf_literal(&ack,"}");assert(ack.len<=128);
  assert(!fault_scratch);fault_scratch=sizeof(struct hfs_json);
  assert(hfs_control(&(*f->session),"RELEASE_CELL",&ack,true,&out)==-ENOMEM&&!out&&!fault_scratch);
  assert(next==(*f->session).next_cell&&ordinary==mount->ordinary_credits&&control==mount->control_credits);
  memset(ack.data,' ',129);ack.len=129;assert(hfs_control(&(*f->session),"RELEASE_CELL",&ack,true,&out)==-EINVAL&&!out);
  assert(next==(*f->session).next_cell&&ordinary==mount->ordinary_credits&&control==mount->control_credits);
  hfs_buf_free(&ack);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"READDIR_CLEAN")){
  struct hfs_session *session=&(*f->session);assert(f->ordinary_result&&f->ordinary_result->owner);
  assert(!hfs_controlled_begin_close(session));session->closing=true;assert(!session->provider->stop(session));session->retained=true;session->provider->retain(session);
  struct hfs_buf fake={.data="{}",.len=2};struct hfs_json *out=NULL;u64 next=session->next_cell;
  assert(hfs_control(session,"READDIR_CLEANUP",&fake,true,&out)==-EACCES&&!out&&next==session->next_cell);
  done(id,hfs_readdir_forget(session,f->ordinary_result,&f->credential),NULL,0);return;
 }
 if(!strcmp(operation,"CLOSE_PENDING")){
  struct hfs_session *session=&(*f->session);struct hfs_controlled_mount *mount=hfs_controlled_mount(session);assert(f->dirty&&f->dirty->adopted&&f->dirty->request->held);
  u32 ordinary=mount->ordinary_credits,control=mount->control_credits,cookies=mount->cookies,requests=session->nr_requests;
  assert(hfs_retire(session)==-EAGAIN&&hfs_view_retire(session)==-EAGAIN);
  assert(!hfs_controlled_begin_close(session));session->closing=true;assert(session->provider->stop(session)==-EBUSY);session->retained=true;session->provider->retain(session);
  assert(mount->session==session&&mount->retained&&mount->stopping&&f->dirty->request->held&&f->dirty->credit&&f->dirty->request->reservation);
  assert(ordinary==mount->ordinary_credits&&control==mount->control_credits&&cookies==mount->cookies&&requests==session->nr_requests);
  assert(session->provider->retire_session(session)==-EBUSY);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"LATE_BORROW")){assert(!late_borrow);late_borrow=f;late_borrow_floor=strtoull(line+consumed,NULL,10);assert(late_borrow_floor>hfs_controlled_mount(&(*f->session))->ordinary_floor);done(id,0,NULL,0);return;}
 if(!strcmp(operation,"DROP_RESULTS")){hfs_json_free(f->ordinary_result);f->ordinary_result=NULL;while(f->nr_held_results)hfs_json_free(f->held_results[--f->nr_held_results]);done(id,0,NULL,0);return;}
 if(!strcmp(operation,"FLOOR_NEGATIVES")){
  struct hfs_json *response=f->latest;int payload=hfs_json_field(response,0,"payload");u64 floor;u8 proof[32],wrong[32];u32 length=32;
  assert(!number(response,payload,"floor",&floor)&&!hfs_json_hex(response,hfs_json_field(response,payload,"proof_hash"),proof,&length)&&length==32);
  u64 previous=hfs_controlled_mount(&(*f->session))->ordinary_floor;u32 count=(*f->session).nr_requests;
  memcpy(wrong,proof,32);wrong[0]^=1;assert((*f->session).provider->retirement(&(*f->session),floor,wrong,response)<0);
  assert((*f->session).provider->retirement(&(*f->session),floor+1,proof,response)<0);
  struct hfs_json *detached=NULL;assert(!hfs_json_parse(response->data,response->len,&detached));detached->transported=true;
  assert((*f->session).provider->retirement(&(*f->session),floor,proof,detached)<0);hfs_json_free(detached);
  struct hfs_request *request=container_of((*f->session).requests.next,struct hfs_request,list);
  struct hfs_json *credentials=NULL;assert(!hfs_json_parse(request->credentials.data,request->credentials.len,&credentials));
  const char *fields[]={"groups","process","credential_revision","user_namespace","idmap_hash"};
  for(unsigned i=0;i<ARRAY_SIZE(fields);i++){
   int token=hfs_json_field(credentials,0,fields[i]);assert(token>=0);u32 offset=credentials->tokens[token].start;
   if(credentials->tokens[token].kind==HFS_STRING||credentials->tokens[token].kind==HFS_ARRAY)offset++;
   char old=request->credentials.data[offset];request->credentials.data[offset]=old=='1'?'2':'1';
   assert((*f->session).provider->retirement(&(*f->session),floor,proof,response)<0);request->credentials.data[offset]=old;
  }
  hfs_json_free(credentials);assert(previous==hfs_controlled_mount(&(*f->session))->ordinary_floor&&count==(*f->session).nr_requests);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"GAP_NEGATIVES")){
  struct hfs_request *request=container_of((*f->session).requests.prev,struct hfs_request,list);assert(request->gap&&request->terminal&&request->adopted&&!request->result&&request->gap_response&&request->response);
  struct hfs_buf expected={0};assert(!hfs_buf_init(&expected,HFS_BODY_MAX));hfs_request_payload(request,&expected);
  assert(!hfs_received_original(&(*f->session),request->gap_response,"GAP_RESOLVE",&expected));
  assert(hfs_received_original(&(*f->session),request->response,"GAP_RESOLVE",&expected)<0);
  struct hfs_json *credential_original=NULL;assert(!hfs_json_parse(expected.data,expected.len,&credential_original));int cred=hfs_json_field(credential_original,0,"credentials");
  const char *credential_fields[]={"groups","process","credential_revision","user_namespace","idmap_hash"};
  for(unsigned i=0;i<ARRAY_SIZE(credential_fields);i++){
   int token=hfs_json_field(credential_original,cred,credential_fields[i]);assert(token>=0);u32 offset=credential_original->tokens[token].start;
   if(credential_original->tokens[token].kind==HFS_STRING||credential_original->tokens[token].kind==HFS_ARRAY)offset++;
   char old=expected.data[offset];expected.data[offset]=old=='1'?'2':'1';assert(hfs_received_original(&(*f->session),request->gap_response,"GAP_RESOLVE",&expected)<0);expected.data[offset]=old;
  }
  hfs_json_free(credential_original);
  struct hfs_json *original=NULL;assert(!hfs_json_parse(expected.data,expected.len,&original));int claim=hfs_json_field(original,0,"claim"),number_token=hfs_json_field(original,claim,"number");assert(number_token>=0);expected.data[original->tokens[number_token].start]='3';assert(hfs_received_original(&(*f->session),request->gap_response,"GAP_RESOLVE",&expected)<0);hfs_json_free(original);hfs_buf_free(&expected);
  struct hfs_json *detached=NULL;assert(!hfs_json_parse(request->gap_response->data,request->gap_response->len,&detached));detached->transported=true;
  struct hfs_buf query={0};assert(!hfs_buf_init(&query,HFS_BODY_MAX));hfs_request_payload(request,&query);assert(hfs_received_original(&(*f->session),detached,"GAP_RESOLVE",&query)<0);
  hfs_json_free(detached);query.len=0;query.error=0;
  hfs_buf_literal(&query,"{\"claim\":");hfs_buf_add(&query,request->claim.data,request->claim.len);hfs_buf_literal(&query,"}");
  assert(!hfs_received_original(&(*f->session),request->response,"QUERY",&query));assert(hfs_received_original(&(*f->session),request->gap_response,"QUERY",&query)<0);
  struct hfs_json *query_original=NULL;assert(!hfs_json_parse(query.data,query.len,&query_original));int query_claim=hfs_json_field(query_original,0,"claim"),query_number=hfs_json_field(query_original,query_claim,"number");assert(query_number>=0);
  query.data[query_original->tokens[query_number].start]='3';assert(hfs_received_original(&(*f->session),request->response,"QUERY",&query)<0);hfs_json_free(query_original);hfs_buf_free(&query);
  int body=hfs_json_field(request->gap_response,0,"payload"),proof=hfs_json_field(request->gap_response,body,"proof_hash");assert(proof>=0);
  u32 offset=request->gap_response->tokens[proof].start+1;char old=request->gap_response->data[offset];request->gap_response->data[offset]=old=='1'?'2':'1';
  assert(hfs_controlled_request_hash(request,(u8[32]){0})==0); /* Inert hash decoding alone grants no floor. */
  assert(hfs_received_require(&(*f->session),request->gap_response,0)<0);request->gap_response->data[offset]=old;
  assert((*f->session).provider->resolve_gap(&(*f->session),request)==-EBUSY);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"ISSUER")){unsigned capture=(unsigned)strtoul(line+consumed,NULL,10);assert(capture<f->nr_captures);f->credential=f->captures[capture];f->credentials=f->capture_bytes[capture];fixture_set_current(f);done(id,0,NULL,0);return;}
 if(!strcmp(operation,"FAULT")){assert(!fault_mode&&!fault_reply&&!fault_scratch&&!pending_scratch);assert(sscanf(line+consumed,"%31s %u",fault_operation,&fault_mode)==2&&fault_mode>=1&&fault_mode<=3);if(fault_mode==3)assert(!strcmp(fault_operation,"RETIRE")||!strcmp(fault_operation,"VIEW_RETIRE"));done(id,0,NULL,0);return;}
 if(!strcmp(operation,"STATE")){
  unsigned cuts=0;struct hfs_view_family *family;list_for_each_entry(family,&(*f->session).view_families,list)if(family->retirement_cut)cuts++;
  unsigned unknown=0;struct hfs_candidate *candidate;list_for_each_entry(candidate,&(*f->session).uncertain,list){assert(candidate->reservation);unknown++;}
  struct hfs_controlled_mount *mount=hfs_controlled_mount(&(*f->session));struct hfs_buf state={0};assert(!hfs_buf_init(&state,1024));
  hfs_buf_printf(&state,"{\"cut\":%llu,\"cut_reply\":%u,\"view_cuts\":%u,\"floor\":%llu,\"requests\":%u,\"unknown\":%u,\"ordinary_credits\":%u,\"control_credits\":%u}",(*f->session).retirement_cut?(*f->session).retirement_cut->through:0,(*f->session).retirement_cut&&(*f->session).retirement_cut->response?1:0,cuts,mount->ordinary_floor,(*f->session).nr_requests,unknown,mount->ordinary_credits,mount->control_credits);
  done(id,0,state.data,state.len);hfs_buf_free(&state);return;
 }
 if(!strcmp(operation,"ADOPT_RESULT")){
  assert(f->ordinary_result&&f->nr_adopted_caps+2<=ARRAY_SIZE(f->adopted_caps));int err=hfs_result_validate(&(*f->session),f->ordinary_result,"ROOT",&f->credentials);
  if(!err)err=hfs_reply_cap(&(*f->session),f->ordinary_result,"ROOT",&f->adopted_caps[f->nr_adopted_caps++]);
  if(!err)err=hfs_reply_cap(&(*f->session),f->ordinary_result,"DOMAIN",&f->adopted_caps[f->nr_adopted_caps++]);
  if(!err)hfs_result_adopt(&(*f->session),f->ordinary_result);done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"ALLOCATE_RETRY")){
  struct hfs_session *session=&(*f->session);struct hfs_request *request=session->allocation_pending;assert(request&&!request->allocated&&!request->allocating&&session->allocation_uncertain&&!session->blocked);
  u64 cell=session->next_cell,claim=session->next_claim;u32 count=session->nr_requests;
  struct hfs_buf payload={0};assert(!hfs_buf_init(&payload,HFS_BODY_MAX)&&!hfs_request_payload(request,&payload));
  struct hfs_candidate *candidate=hfs_allocation_candidate(session,&payload);assert(candidate&&candidate->completion==request->completion&&candidate->reservation);
  struct hfs_response_profile profile=candidate->response;assert(profile.channel==HFS_CHANNEL_RESULT&&profile.kind==HFS_FRAME_RESULT&&profile.max_body==HFS_CONTEXT_MAX+4096+1024);
  int err=hfs_request_allocate(session,request,&payload);
  if(!err){assert(request->allocated&&request->allocation_receipt&&!session->allocation_pending&&!session->allocation_uncertain&&!session->blocked&&list_empty(&session->uncertain));assert(!hfs_received_original(session,request->allocation_receipt,"ALLOCATE",&payload));assert(request->allocation_receipt->channel==profile.channel&&request->allocation_receipt->frame_kind==profile.kind);}
  assert(cell==session->next_cell&&claim==session->next_claim&&count==session->nr_requests);hfs_buf_free(&payload);done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"REQUEST_CONTINUE")){
  struct hfs_request *request=container_of((*f->session).requests.prev,struct hfs_request,list);assert(request->allocated&&!request->terminal&&!f->ordinary_result);
  struct hfs_buf payload={0};assert(!hfs_buf_init(&payload,HFS_BODY_MAX));int err=hfs_request_run(&(*f->session),request,"ROOT",&payload,&f->ordinary_result);hfs_buf_free(&payload);
  if(!err){hfs_json_free(f->latest);f->latest=NULL;err=hfs_received_copy(request->response,0,&f->latest);}done(id,err,f->latest?f->latest->data:NULL,f->latest?f->latest->len:0);return;
 }
 if(!strcmp(operation,"RESOLVE_GAP")){
  struct hfs_request *request,*last=NULL;list_for_each_entry(request,&(*f->session).requests,list)last=request;assert(last);
  done(id,(*f->session).provider->resolve_gap(&(*f->session),last),NULL,0);return;
 }
 if(!strcmp(operation,"ACTOR_START")){
  assert(f->session->view&&f->session->domain);int err=hfs_mmap_start(f->session->view);
  if(err)fprintf(stderr,"actor reserve error%d control_used%llu base%llu limit%llu ordinary%llu\n",err,hfs_controlled_control_used,hfs_controlled_registry->control_base_bytes,hfs_controlled_registry->budget.control_bytes,hfs_controlled_ordinary_used);
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"RELEASE_READY")){
  assert(f->lease);struct hfs_view_ticket *ticket=f->lease->release_ticket;
  bool ready=ticket->admitted&&ticket->initial_response&&!ticket->family->initial;
  done(id,0,ready?"true":"false",ready?4:5);return;
 }
 if(!strcmp(operation,"ACTOR_STATE")){
  struct hfs_view *view=f->session->view;struct hfs_mmap_control *control=view->control;assert(control);
  if(!view->nr_dirty)f->dirty=NULL;
  char state[256];int length=snprintf(state,sizeof(state),"{\"completed\":%llu,\"count\":%u,\"delivered\":%llu,\"error\":%d,\"phase\":\"%s\"}",control->completed,control->count,control->delivered,control->error,hfs_controlled_mount(f->session)->phase);
  done(id,control->error,state,length);return;
 }
 if(!strcmp(operation,"ACTOR_STEP")){
  struct hfs_mmap_control *control=f->session->view->control;assert(control);
  struct workqueue_struct *queue=!strcmp(line+consumed,"poll")?control->poll_queue:
   !strcmp(line+consumed,"event")?control->event_queue:!strcmp(line+consumed,"drain")?control->drain_queue:NULL;
  assert(queue);bool ran=primitive_step_queue(queue);emit_snapshot();
  char state[256];int length=snprintf(state,sizeof(state),"{\"completed\":%llu,\"count\":%u,\"delivered\":%llu,\"error\":%d,\"ran\":%s}",control->completed,control->count,control->delivered,control->error,ran?"true":"false");
  done(id,control->error,state,length);return;
 }
 if(!strcmp(operation,"VIEW_NEW")){
  int err=(*f->session).view?-EEXIST:hfs_view_new_unbound(&(*f->session));
  if(!err){f->physical.view=(*f->session).view;assert(!hfs_buf_init(&f->physical.ack,HFS_VIEW_BODY_MAX));assert(!hfs_buf_init(&f->physical.last_ack,HFS_VIEW_BODY_MAX));}
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"EVENT_HOLD")){assert(f->latest&&f->nr_event_pressure<ARRAY_SIZE(f->event_pressure));assert(!hfs_received_event(&(*f->session),f->latest,hfs_json_field(f->latest,hfs_json_field(f->latest,0,"payload"),"event")));f->event_pressure[f->nr_event_pressure++]=f->latest;f->latest=NULL;done(id,0,NULL,0);return;}
 if(!strcmp(operation,"EVENT_RESTORE")){assert(!f->latest&&f->nr_event_pressure==ARRAY_SIZE(f->event_pressure));f->latest=f->event_pressure[--f->nr_event_pressure];done(id,0,NULL,0);return;}
 if(!strcmp(operation,"ACTOR_HOLD")){
  struct hfs_session *session=&(*f->session);assert(f->latest&&f->nr_event_pressure==HFS_MMAP_EVENTS_MAX&&!f->parked_carrier&&!f->parked_received);
  struct hfs_response_profile event=hfs_response_profile("VIEW_WAIT"),floor=hfs_response_profile("RETIRE");event.max_body=HFS_EVENT_BODY_MAX;floor.max_body=HFS_FLOOR_BODY_MAX;
  assert(!hfs_completion_reserve(session->control_completion,hfs_completion_carrier(HFS_BODY_MAX,HFS_BODY_MAX),false,&f->parked_carrier));
  assert(!hfs_received_prepare(session,HFS_CONTEXT_MAX+HFS_CAP_MAX+1024,event,true,session->control_completion,&f->parked_received));
  for(unsigned i=0;i<2;i++)assert(!hfs_received_prepare(session,HFS_BODY_MAX,floor,true,session->retirement_completion,&f->floor_pressure[i]));
  assert(session->control_completion->nr_received==HFS_MMAP_EVENTS_MAX+2&&session->retirement_completion->nr_received==2);
  if(!index){u64 remaining=hfs_controlled_registry->budget.control_bytes-hfs_controlled_registry->control_base_bytes-hfs_controlled_control_used;assert(remaining>sizeof(struct hfs_controlled_credit));assert(!hfs_controlled_reserve(session,remaining-sizeof(struct hfs_controlled_credit),true,&f->global_control_pressure));}
  void *extra=NULL;assert(hfs_controlled_reserve(session,1,true,&extra)==-ENOSPC&&!extra);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"FLOOR_UNHOLD")){for(unsigned i=0;i<2;i++){assert(f->floor_pressure[i]);hfs_controlled_received_release(&(*f->session),f->floor_pressure[i]);f->floor_pressure[i]=NULL;}done(id,0,NULL,0);return;}
 if(!strcmp(operation,"ACTOR_DROP")){
  while(f->nr_event_pressure)hfs_json_free(f->event_pressure[--f->nr_event_pressure]);
  hfs_controlled_received_release(&(*f->session),f->parked_received);f->parked_received=NULL;
  hfs_controlled_unreserve(&(*f->session),f->parked_carrier);f->parked_carrier=NULL;
  if(f->global_control_pressure){hfs_controlled_unreserve(&(*f->session),f->global_control_pressure);f->global_control_pressure=NULL;}
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"VIEW_QUERY_HOLD")){
  assert(f->lease&&!f->query_pressure&&!f->lease->ticket->control);u64 control=hfs_controlled_control_used;u32 slots=hfs_controlled_nr_control_credits,received=hfs_controlled_nr_control_received;
  int err=hfs_view_query(&(*f->session),f->lease->ticket,false,&f->query_pressure);
  if(!err){u32 offset,length;struct hfs_kernel_received *owner=hfs_received_get(f->query_pressure,&offset,&length);assert(owner&&!owner->control&&owner->completion==f->lease->ticket->completion);hfs_received_put(owner);}
  assert(control==hfs_controlled_control_used&&slots==hfs_controlled_nr_control_credits&&received==hfs_controlled_nr_control_received);done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"BORROW_PRESSURE")){
  unsigned count=(unsigned)strtoul(line+consumed,NULL,10);int err=0;u64 control=hfs_controlled_control_used;u32 slots=hfs_controlled_nr_control_credits,received=hfs_controlled_nr_control_received;
  struct hfs_request *request=container_of((*f->session).requests.next,struct hfs_request,list);assert(request->result&&request->terminal&&!request->control);
  for(unsigned i=0;i<count&&!err;i++){
   assert(f->nr_result_pressure<ARRAY_SIZE(f->result_pressure));struct hfs_json *copy=NULL;err=hfs_request_result_borrow(request,&copy);
   if(!err){u32 offset,length;struct hfs_kernel_received *owner=hfs_received_get(copy,&offset,&length);assert(owner&&!owner->control&&copy->owner==request);hfs_received_put(owner);f->result_pressure[f->nr_result_pressure++]=copy;}
  }
  assert(!err||err==-ENOSPC);assert(control==hfs_controlled_control_used&&slots==hfs_controlled_nr_control_credits&&received==hfs_controlled_nr_control_received);
  char result[128];int length=snprintf(result,sizeof(result),"{\"held\":%u,\"exhausted\":%s}",f->nr_result_pressure,err?"true":"false");done(id,0,result,length);return;
 }
 if(!strcmp(operation,"DROP_PRESSURE")){while(f->nr_result_pressure)hfs_json_free(f->result_pressure[--f->nr_result_pressure]);hfs_json_free(f->query_pressure);f->query_pressure=NULL;done(id,0,NULL,0);return;}
 if(!strcmp(operation,"OWNER_NEGATIVES")){
  assert(f->lease&&f->lease->ticket->bound);struct hfs_view_ticket *ticket=f->lease->ticket;
  struct hfs_completion *owner=hfs_completion_find(&(*f->session),"VIEW_QUERY",&ticket->query);assert(owner==ticket->completion&&!hfs_completion_control(owner));hfs_completion_put(owner);
  struct hfs_buf wrong={0};assert(!hfs_buf_init(&wrong,ticket->query.len+1));hfs_buf_add(&wrong,ticket->query.data,ticket->query.len);wrong.data[wrong.len-2]^=1;
  assert(!hfs_completion_find(&(*f->session),"VIEW_QUERY",&wrong));hfs_buf_free(&wrong);
  struct hfs_buf bogus={.data="{\"claim\":{}}",.len=12};assert(!hfs_completion_find(&(*f->session),"QUERY",&bogus));
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"ORDINARY_REFUSES")){
  struct hfs_json *result=NULL;struct hfs_buf args={.data="{}",.len=2};u32 count=(*f->session).nr_requests;u64 cell=(*f->session).next_cell;
  int err=hfs_call(&(*f->session),"ROOT",&args,&f->credential,false,&result);
  assert(err==-ENOSPC&&!result&&count==(*f->session).nr_requests&&cell==(*f->session).next_cell&&!(*f->session).blocked);
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"PIN")){f->folio.page.pinned=atoi(line+consumed)!=0;done(id,0,NULL,0);return;}
 if(!strcmp(operation,"DRAIN")){assert(f->dirty);
  if((*f->session).closing){
   struct hfs_buf *payload=&f->dirty->request->drain_payload;struct hfs_json *out=NULL;assert(!hfs_request_payload(f->dirty->request,payload));
   assert(hfs_controlled_before_send(&(*f->session),"CALL",payload,false)<0);
   struct hfs_buf fake={.data="{}",.len=2};u64 next=(*f->session).next_cell;
   assert(hfs_control(&(*f->session),"CALL",&fake,true,&out)==-EACCES&&!out&&next==(*f->session).next_cell);
   assert(!hfs_controlled_before_send(&(*f->session),"CALL",payload,true));
   payload->data[payload->len-2]^=1;assert(hfs_controlled_before_send(&(*f->session),"CALL",payload,true)==-EACCES);payload->data[payload->len-2]^=1;
  }
  int err=hfs_dirty_drain((*f->session).view,f->dirty);done(id,err,NULL,0);return;}
 if(!strcmp(operation,"RELEASE_DONE")){
  assert(f->lease);int err=hfs_lease_release(&(*f->session),f->lease);
  if(!err){if(f->dirty_page){assert(!f->dirty_page->page&&!f->dirty_page->dirty);hfs_view_page_discard(f->dirty_page);f->dirty_page=NULL;}hfs_lease_free(f->lease);f->lease=NULL;}
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"RETAIN_VERIFY")){
  struct hfs_session *session=&(*f->session);struct hfs_controlled_mount *mount=hfs_controlled_mount(session);
  u32 ordinary=mount->ordinary_credits,control=mount->control_credits,requests=session->nr_requests;u64 next=session->next_cell;struct device *device=mount->device;
  session->closing=true;int stopped=session->provider->stop(session);assert(stopped==0||stopped==-EBUSY);
  session->retained=true;session->provider->retain(session);assert(mount->retained&&mount->stopping&&session->blocked);
  assert(session->provider->retire_session(session)==-EBUSY&&mount->session==session&&mount->device==device);
  assert(ordinary==mount->ordinary_credits&&control==mount->control_credits&&requests==session->nr_requests&&next==session->next_cell);
  if(session->view){void *cookie=NULL;assert(hfs_view_enter(session->view,NULL,false,&cookie)<0&&!cookie);}
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"CLEANUP_OWNERS")){
  assert(f->ordinary_result&&!f->cleanup_description&&!f->cleanup_forget);
  if(list_empty(&f->session->processes)){primitive_process_uuid=f->credential.process;primitive_process_uuid_pending=true;}
  assert(!hfs_description_prepare(f->session,false,&f->cleanup_description));
  assert(!hfs_reply_cap(f->session,f->ordinary_result,"DESCRIPTION",&f->cleanup_description->cap));
  assert(!hfs_capture(f->session,&init_user_ns,&f->cleanup_description->issuer));
  f->cleanup_forget=kzalloc(sizeof(*f->cleanup_forget),GFP_KERNEL);assert(f->cleanup_forget);
  f->cleanup_forget->session=f->session;f->cleanup_forget->count=1;
  assert(!hfs_reply_cap(f->session,f->ordinary_result,"PATH",&f->cleanup_forget->cap));
  assert(!hfs_capture(f->session,&init_user_ns,&f->cleanup_forget->issuer));
  list_add_tail(&f->cleanup_forget->list,&f->session->forgets);
  hfs_result_adopt(f->session,f->ordinary_result);done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"CALLERS_UNFUNDED")){
  fflush(NULL);
  for(unsigned op=0;op<4;op++){
   pid_t child=fork();assert(child>=0);
   if(!child){
    struct hfs_session *s=f->session;u64 claim=s->next_claim,cell=s->next_cell,ordinary=hfs_controlled_ordinary_used,control=hfs_controlled_control_used;u32 requests=s->nr_requests;
    struct inode inode={.i_sb=&f->super};struct file file={.inode=&inode,.private_data=f->cleanup_description};
    if(op==0)assert(hfs_flush(&file,NULL)==-ENOSPC);
    else if(op==1){assert(hfs_release(&inode,&file)==-ENOSPC&&s->blocked&&file.private_data==f->cleanup_description);}
    else if(op==2){hfs_forget_work(&f->cleanup_forget->work);assert(s->blocked&&f->cleanup_forget->cap&&f->cleanup_forget->issuer&&!list_empty(&s->forgets));}
    else {struct hfs_process *process=hfs_process_find(s,&f->pid);assert(process&&process->last);struct hfs_cred *issuer=process->last;hfs_process_exit_work(&process->exit_work);assert(s->blocked&&process->last==issuer&&uuid_equal(&process->id,&f->credential.process));}
    assert(s->next_claim==claim&&s->next_cell==cell&&s->nr_requests==requests&&ordinary==hfs_controlled_ordinary_used&&control==hfs_controlled_control_used);
    _exit(0);
   }
   int status;assert(waitpid(child,&status,0)==child&&WIFEXITED(status)&&WEXITSTATUS(status)==0);
  }
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"CLEANUP_CALLER")){
  struct hfs_session *s=f->session;int err=0;u64 control=hfs_controlled_control_used;
  if(!strcmp(line+consumed,"PROCESS_EXIT")){
   struct hfs_process *process=hfs_process_find(s,&f->pid);assert(process&&process->last&&uuid_equal(&process->id,&f->credential.process));
   hfs_process_exit_work(&process->exit_work);err=s->blocked?-EHOSTDOWN:0;
  }else if(!strcmp(line+consumed,"FORGET")){
   assert(f->cleanup_forget);hfs_forget_work(&f->cleanup_forget->work);if(!s->blocked)f->cleanup_forget=NULL;err=s->blocked?-EHOSTDOWN:0;
  }else {
   struct inode inode={.i_sb=&f->super};struct file file={.inode=&inode,.private_data=f->cleanup_description};
   assert(f->cleanup_description);
   if(!strcmp(line+consumed,"DESCRIPTION_CLOSE"))err=hfs_flush(&file,NULL);
   else {assert(!strcmp(line+consumed,"DESCRIPTION_RELEASE"));err=hfs_release(&inode,&file);if(!err){assert(!file.private_data);f->cleanup_description=NULL;}}
  }
  assert(control==hfs_controlled_control_used);done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"BYTE_PRESSURE")||!strcmp(operation,"BYTE_UNPRESSURE")){
  bool control=!strcmp(line+consumed,"control");void **credit=&f->terminal_external_credit;
  if(!strcmp(operation,"BYTE_UNPRESSURE")){assert(*credit);hfs_controlled_unreserve(f->session,*credit);*credit=NULL;}
  else {assert(!*credit);u64 remaining=control?hfs_controlled_registry->budget.control_bytes-hfs_controlled_registry->control_base_bytes-hfs_controlled_control_used:hfs_controlled_registry->budget.ordinary_bytes-hfs_controlled_ordinary_used;assert(remaining>sizeof(struct hfs_controlled_credit));assert(!hfs_controlled_reserve(f->session,remaining-sizeof(struct hfs_controlled_credit),control,credit));}
  done(id,0,NULL,0);return;
 }
 if(!strcmp(operation,"CLEANUP_REQUEST")){
  char op[32];int used=0;assert(sscanf(line+consumed,"%31s %n",op,&used)==1);
  assert(!strcmp(op,"DESCRIPTION_CLOSE")||!strcmp(op,"DESCRIPTION_RELEASE")||!strcmp(op,"FORGET")||!strcmp(op,"PROCESS_EXIT"));
  struct hfs_session *session=f->session;u64 next=session->next_claim,cell=session->next_cell,control=hfs_controlled_control_used,ordinary=hfs_controlled_ordinary_used;u32 requests=session->nr_requests,control_requests=session->nr_control_requests;
  struct hfs_buf args={0};struct hfs_json *result=NULL;assert(!hfs_args_init(&args));args.len=decode_hex(line+consumed+used,args.data,args.cap);
  fflush(NULL);pid_t closed=fork();assert(closed>=0);
  if(!closed){
   session->closing=true;struct hfs_json *late=NULL;
   assert(hfs_call(session,op,&args,&f->credential,true,&late)==-ESTALE&&!late);
   assert(session->next_claim==next&&session->next_cell==cell&&session->nr_requests==requests&&session->nr_control_requests==control_requests&&control==hfs_controlled_control_used&&ordinary==hfs_controlled_ordinary_used);
   _exit(0);
  }
  int closed_status;assert(waitpid(closed,&closed_status,0)==closed&&WIFEXITED(closed_status)&&WEXITSTATUS(closed_status)==0);
  int err=hfs_call(session,op,&args,&f->credential,true,&result);
  if(!err){
   struct hfs_request *request=result->owner;struct hfs_controlled_credit *credit=request->reservation;
   assert(session->nr_control_requests==control_requests+1);
   assert(request->control&&!request->earmarked&&!request->view_stamped&&request->users&&credit&&!credit->control);
   assert(!hfs_completion_control(request->completion)&&hfs_completion_body(request->completion)==HFS_BODY_MAX);
   u32 offset,length;struct hfs_kernel_received *owner=hfs_received_get(result,&offset,&length);assert(owner&&!owner->control&&owner->completion==request->completion);hfs_received_put(owner);
   struct hfs_json *again=NULL;assert(!hfs_request_result_borrow(request,&again));assert(again->len==result->len&&!memcmp(again->data,result->data,result->len));
   u64 retry_cell=session->next_cell,retry_claim=session->next_claim;
   struct hfs_buf retry={0};struct hfs_json *replayed=NULL;assert(!hfs_buf_init(&retry,HFS_BODY_MAX));
   u32 retry_slots=request->completion->nr_slots,retry_received=request->completion->nr_received;u64 retry_bytes=request->completion->used;
   retry_snapshot_watch=request->result;retry_snapshot_frees=0;
   assert(!hfs_request_run(session,request,op,&retry,&replayed));
   assert(request->result==retry_snapshot_watch?!retry_snapshot_frees:retry_snapshot_frees==1);retry_snapshot_watch=NULL;
   assert(replayed->len==result->len&&!memcmp(replayed->data,result->data,result->len));
   assert(session->next_cell==retry_cell&&session->next_claim==retry_claim&&request->users>=3);hfs_json_free(replayed);hfs_buf_free(&retry);
   assert(request->completion->nr_slots==retry_slots&&request->completion->nr_received==retry_received&&request->completion->used==retry_bytes);
   assert(hfs_retire(session)==-EAGAIN&&request->users>=2&&request->control);hfs_json_free(again);
   assert(f->nr_held_results<ARRAY_SIZE(f->held_results));f->held_results[f->nr_held_results++]=result;result=NULL;
  }else if(err==-ENOSPC){assert(session->next_claim==next&&session->next_cell==cell&&session->nr_requests==requests&&session->nr_control_requests==control_requests&&ordinary==hfs_controlled_ordinary_used);}
  assert(control==hfs_controlled_control_used);hfs_json_free(result);hfs_buf_free(&args);done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"REQUEST")){
  char op[32];int used=0;assert(sscanf(line+consumed,"%31s %n",op,&used)==1);
  struct hfs_buf args={0};assert(!hfs_args_init(&args));args.len=decode_hex(line+consumed+used,args.data,args.cap);
  if(f->ordinary_result){assert(f->nr_held_results<ARRAY_SIZE(f->held_results));f->held_results[f->nr_held_results++]=f->ordinary_result;}f->ordinary_result=NULL;f->actual_result=true;
  struct floor_mark before=floor_mark(f->session);
  int err=hfs_call(&(*f->session),op,&args,&f->credential,false,&f->ordinary_result);hfs_buf_free(&args);
  if(!err)floor_owner_check(f->session,&before,1,0);
  if(!err){struct hfs_request *request=f->ordinary_result->owner;assert(request&&request->response);hfs_json_free(f->latest);f->latest=NULL;err=hfs_received_copy(request->response,0,&f->latest);}
  done(id,err,f->latest?f->latest->data:NULL,f->latest?f->latest->len:0);return;
 }
 if(!strcmp(operation,"DIRTY")){
  assert(!f->dirty&&!f->dirty_page&&f->description);
  struct hfs_lease *lease=NULL;struct hfs_view *view=(*f->session).view;struct hfs_view_stamp stamp;
  int err=hfs_lease_acquire(&(*f->session),&f->credential,NULL,f->description,0,true,NULL,&lease);
  if(!err)err=hfs_view_bind_initial(view,lease);
  if(!err)err=hfs_view_snapshot(view,&stamp);
  if(!err)err=hfs_view_page_prepare(view,&stamp,&f->dirty_page);
  if(!err)err=hfs_view_page_lease(f->dirty_page,lease);
  if(!err){lock_page(&f->folio.page);err=hfs_view_page_attach(f->dirty_page,&f->folio.page);unlock_page(&f->folio.page);}
  if(!err)err=hfs_dirty_reserve(&(*f->session),f->dirty_page,f->description,0,4096,&f->dirty);
  if(!err){lock_page(&f->folio.page);err=hfs_view_adopt_dirty(view,f->dirty_page,f->dirty);unlock_page(&f->folio.page);}
  if(!err){if(!f->lease)f->lease=lease;for(unsigned n=0;n<4096;n++)f->folio.page.data[n]=(u8)(n*17+index);f->folio.page.dirty=true;}
  done(id,err,err?NULL:f->dirty->reservation->data,err?0:f->dirty->reservation->len);return;
 }
 if(!strcmp(operation,"PRESSURE")){
  unsigned requested=(unsigned)strtoul(line+consumed,NULL,10),count=0;int err=0;
  struct hfs_cap *cap=f->description?f->description:f->root;assert(cap);
  while(!err&&f->nr_pressure<ARRAY_SIZE(f->pressure)&&(!requested||count<requested)){
   void *cookie=NULL;err=(*f->session).provider->enter(&(*f->session),cap,false,&cookie);
   if(!err){assert(cookie);f->pressure[f->nr_pressure++]=cookie;count++;}
  }
  if(!requested&&err==-ENOSPC)err=0;
  done(id,err,NULL,0);return;
 }
 if(!strcmp(operation,"UNPRESSURE")){while(f->nr_pressure)(*f->session).provider->leave(&(*f->session),f->pressure[--f->nr_pressure]);done(id,0,NULL,0);return;}
 if(!strcmp(operation,"RETIRE")){hfs_json_free(f->ordinary_result);f->ordinary_result=NULL;while(f->nr_held_results)hfs_json_free(f->held_results[--f->nr_held_results]);int err=hfs_retire(&(*f->session));if(err==-EBUSY&&(*f->session).retirement_cut)assert(!late_borrow&&f->ordinary_result&&f->ordinary_result->owner);done(id,err,NULL,0);return;}
 if(!strcmp(operation,"VIEW_RETIRE")){done(id,hfs_view_retire(&(*f->session)),NULL,0);return;}
 if(!strcmp(operation,"RUN")) {
  unsigned reserved;char op[32];int used=0;assert(sscanf(line+consumed,"%u %31s %n",&reserved,op,&used)==2&&reserved<=1);
  struct hfs_buf payload={0};assert(!hfs_buf_init(&payload,HFS_BODY_MAX));payload.len=decode_hex(line+consumed+used,payload.data,HFS_BODY_MAX);
  hfs_json_free(f->latest);f->latest=NULL;int err=hfs_control(&(*f->session),op,&payload,reserved,&f->latest);hfs_buf_free(&payload);
  done(id,err,f->latest?f->latest->data:NULL,f->latest?f->latest->len:0);return;
 }
 if(!strcmp(operation,"CHECK")){done(id,fixture_check(f,line+consumed),NULL,0);return;}
 if(!strcmp(operation,"ADOPT_ROOT")||!strcmp(operation,"ADOPT_ROOT_PREPARE")){
  if(f->root||(*f->session).domain||(*f->session).view){done(id,-EEXIST,NULL,0);return;}
  int err=fixture_result(f,"ROOT");if(!err)err=hfs_reply_cap(&(*f->session),f->ordinary_result,"ROOT",&f->root);
  if(!err)err=hfs_reply_cap(&(*f->session),f->ordinary_result,"DOMAIN",&(*f->session).domain);
  if(!err&&!strcmp(operation,"ADOPT_ROOT"))err=hfs_view_new_unbound(&(*f->session));
  if(!err&&!strcmp(operation,"ADOPT_ROOT")){f->physical.view=(*f->session).view;assert(!hfs_buf_init(&f->physical.ack,HFS_VIEW_BODY_MAX));assert(!hfs_buf_init(&f->physical.last_ack,HFS_VIEW_BODY_MAX));}
  if(!err)hfs_result_adopt(&(*f->session),f->ordinary_result);
  done(id,err,err?NULL:(*f->session).domain->json->data,err?0:(*f->session).domain->json->len);return;
 }
 if(!strcmp(operation,"ADOPT_DESCRIPTION")){
  int err=fixture_result(f,line+consumed);if(!err)err=hfs_reply_cap(&(*f->session),f->ordinary_result,"DESCRIPTION",&f->description);
  if(!err&&f->session->enrollment.protocol_features==HFS_TERMINAL_FEATURES&&!strcmp(line+consumed,"CREATE"))err=hfs_reply_cap(f->session,f->ordinary_result,"PATH",&f->created_path);
  if(!err)hfs_result_adopt(&(*f->session),f->ordinary_result);
  done(id,err,err?NULL:f->description->json->data,err?0:f->description->json->len);return;
 }
 if(!strcmp(operation,"ACQUIRE")||!strcmp(operation,"ACQUIRE_PAGE")){
  struct floor_mark before=floor_mark(f->session);
  struct hfs_lease *lease=NULL;int err=hfs_lease_acquire(&(*f->session),&f->credential,NULL,!strcmp(operation,"ACQUIRE_PAGE")?f->description:NULL,0,false,NULL,&lease);
  if(!err)floor_owner_check(f->session,&before,0,1);
  if(!err){
   /* Consumption is checked BEFORE initial physical binding; no phase
    * exception is invented to keep an old grant fresh afterwards. */
   assert(!hfs_received_source(&(*f->session),lease->json,0));
   assert(hfs_received_source(NULL,lease->json,0)<0);
   struct hfs_session outsider={.bridge_minor=4};
   assert(hfs_received_source(&outsider,lease->json,0)<0);
   outsider.provider=(*f->session).provider;
   assert(hfs_received_source(&outsider,lease->json,0)<0);
   if(hfs_controlled_registry->nr_mounts>1){unsigned other=f==&fixtures[0]?1:0;assert(hfs_received_source(&(*fixtures[other].session),lease->json,0)<0);}
   struct hfs_json *detached=NULL;assert(!hfs_json_parse(lease->json->data,lease->json->len,&detached));detached->transported=true;
   assert(hfs_received_source(&(*f->session),detached,0)<0);hfs_json_free(detached);
   assert(hfs_received_source(&(*f->session),lease->json,hfs_json_field(lease->json,0,"target"))<0);
   bool initial=(*f->session).view->phase==HFS_VIEW_UNBOUND;
   err=hfs_view_bind_initial((*f->session).view,lease);
   if(!err&&initial)assert(hfs_received_source(&(*f->session),lease->json,0)<0);
  }
  if(!err){assert(!f->lease);f->lease=lease;}
  done(id,err,err?NULL:lease->json->data,err?0:lease->json->len);return;
 }
 if(!strcmp(operation,"RELEASE_START")){
  struct hfs_lease *lease=f->lease;assert(lease);struct hfs_view_ticket *ticket=lease->release_ticket;struct hfs_buf *payload=&lease->release_payload;
  int err=hfs_view_ticket_activate(ticket);payload->len=0;payload->error=0;
  hfs_buf_literal(payload,"{\"lease\":");hfs_buf_add(payload,lease->json->data,lease->json->len);hfs_buf_literal(payload,",\"request\":");hfs_buf_add(payload,ticket->request.data,ticket->request.len);hfs_buf_literal(payload,"}");
  if(!err){hfs_json_free(f->latest);f->latest=NULL;err=fixture_prepare_exchange(&(*f->session),ticket,"VIEW_RELEASE",payload,&f->latest);}
  if(!err){hfs_json_free(f->latest);f->latest=NULL;err=hfs_control(&(*f->session),"VIEW_RELEASE",payload,true,&f->latest);}
  done(id,err,f->latest?f->latest->data:NULL,f->latest?f->latest->len:0);return;
 }
 if(!strcmp(operation,"PHASE")){
  struct hfs_json *event=f->latest;struct hfs_view *view=(*f->session).view;const char *phase=line+consumed;
  int body=event?hfs_json_field(event,hfs_json_field(event,0,"payload"),"event"):-1;
  int err=body<0?-EINVAL:(*f->session).provider->view_event(&(*f->session),event,body);
  uuid_t recall;u64 sequence;if(!err&&(hfs_uuid(event,hfs_json_field(event,body,"recall"),&recall)||number(event,body,"sequence",&sequence)))err=-EPROTO;
  bool manifest=!strcmp(phase,"STOPPED")||!strcmp(phase,"DRAINED"),target=strcmp(phase,"STOPPED")!=0;
  if(!err){if(!strcmp(phase,"STOPPED")){err=hfs_view_stop(view,&view->generation,&recall,sequence);if(!err&&f->dirty)err=hfs_dirty_freeze(view,f->dirty);if(!err&&f->dirty){assert(f->dirty->frozen&&f->dirty->frozen_entry&&f->dirty->request->held);assert(!hfs_received_source(&(*f->session),f->dirty->frozen_entry,0));}
    if(!err)err=hfs_view_seal(view);}
   else if(!strcmp(phase,"INSTALLED_DRAIN")||!strcmp(phase,"INSTALLED_FINAL"))err=hfs_mmap_install(view,event,hfs_json_field(event,body,"target"),&recall,sequence,!strcmp(phase,"INSTALLED_FINAL"));
   else if(!strcmp(phase,"DRAINED")){if(f->dirty)err=hfs_dirty_drain(view,f->dirty);if(!err){err=hfs_mmap_finish_drained(view);if(!err)f->dirty=NULL;}}
   else if(!strcmp(phase,"REOPENED"))err=hfs_view_reopen(view,&recall,sequence);else err=-EINVAL;}
  if(!err)err=hfs_mmap_ack(&f->physical,event,body,phase,manifest,target);
  if(err&&f->dirty){assert(f->dirty->adopted&&f->dirty->request->held&&f->dirty->credit&&f->dirty->request->reservation&&view->nr_dirty==1);}
  emit_snapshot();done(id,err,NULL,0);return;
 }
 done(id,-EOPNOTSUPP,NULL,0);
}
int main(void) {
 char *line=read_line();struct hfs_json *initial=NULL;assert(!hfs_json_parse(line,strlen(line),&initial));free(line);
 assert(!hfs_json_exact(initial,0,"base,binds,contexts,credentials,os,protocol_features,provision"));
 int os=hfs_json_field(initial,0,"os");int native_endpoint=hfs_json_field(initial,os,"native_endpoint");assert(!number(initial,native_endpoint,"command_node",&primitive_native_nodes[0]));assert(!number(initial,native_endpoint,"result_node",&primitive_native_nodes[1]));assert(!number(initial,native_endpoint,"recall_node",&primitive_native_nodes[2]));assert(!hfs_uuid(initial,hfs_json_field(initial,os,"controller"),&primitive_supervised_controller));assert(!hfs_uuid(initial,hfs_json_field(initial,os,"boot_id"),&primitive_boot));assert(!hfs_uuid(initial,hfs_json_field(initial,os,"provider_nonce"),&primitive_nonce));
 /* Explicit exporter-only OS/controller boundary: no Linux registry,
  * observer or private Received authority is constructed in this mode. */
 if(getenv("HFS_C6_BACKEND_ONLY")){
  Hfs9pEndpoint endpoint={{0},{0}};memcpy(endpoint.node,primitive_native_nodes,sizeof(endpoint.node));
  uuid_t fuse;assert(!hfs_uuid(initial,hfs_json_field(initial,native_endpoint,"fuse_session"),&fuse));memcpy(endpoint.session,fuse.b,16);
  int directory=open(getenv("HFS_C6_LEDGER"),O_RDONLY|O_DIRECTORY);assert(directory>=0);
  Hfs9pLedger *ledger=NULL;assert(!hfs9p_ledger_new(&ledger,directory,"carrier"));
  int fds[3]={10,11,12};Hfs9pVerifier verifier={ledger,claim_cb,protect_cb,native_identity,NULL,NULL,native_controller};assert(!_construct(&carrier,fds,&endpoint,verifier));
  pthread_mutex_lock(&carrier->mutex);carrier->stopping=true;pthread_mutex_unlock(&carrier->mutex);assert(!pthread_join(carrier->worker,NULL));
  u64 base,features;assert(!number(initial,0,"base",&base)&&!number(initial,0,"protocol_features",&features));carrier->high=base;
  int contexts=hfs_json_field(initial,0,"contexts");assert(contexts>=0);
  for(u32 i=contexts+1;i<initial->count;i++)if(initial->tokens[i].parent==(u32)contexts)assert(!hfs9p_add_session_tuple(carrier,(u8 *)initial->data+initial->tokens[i].start,initial->tokens[i].end-initial->tokens[i].start,4,features));
  hfs_json_free(initial);puts("READY");fflush(stdout);
  for(;;){line=read_line();if(!strcmp(line,"END")){free(line);break;}command(line);free(line);}
  return 0;
 }
 assert(!hfs_controlled_init()&&hfs_provider==&hfs_controlled_provider);
 int provision=hfs_json_field(initial,0,"provision");assert(provision>=0);
 assert(!hfs_install_provision(initial->data+initial->tokens[provision].start,initial->tokens[provision].end-initial->tokens[provision].start));
 int binds=hfs_json_field(initial,0,"binds");assert(binds>=0);
 for(u32 i=binds+1;i<initial->count;i++)if(initial->tokens[i].parent==(u32)binds)assert(!hfs_install_bind(initial->data+initial->tokens[i].start,initial->tokens[i].end-initial->tokens[i].start));
 Hfs9pEndpoint endpoint={{0},{0}};memcpy(endpoint.node,primitive_native_nodes,sizeof(endpoint.node));struct hfs_json *context=NULL;
 struct hfs_controlled_mount *first=hfs_controlled_registry->mounts[0];assert(!hfs_json_parse(first->enrollment.context,first->enrollment.context_len,&context));uuid_t fuse;assert(!hfs_uuid(context,hfs_json_field(context,0,"fuse_session"),&fuse));uuid_t observed_fuse;assert(!hfs_uuid(initial,hfs_json_field(initial,native_endpoint,"fuse_session"),&observed_fuse)&&uuid_equal(&fuse,&observed_fuse));memcpy(endpoint.session,fuse.b,16);hfs_json_free(context);
 int directory=open(getenv("HFS_C6_LEDGER"),O_RDONLY|O_DIRECTORY);assert(directory>=0);Hfs9pLedger *ledger=NULL;assert(!hfs9p_ledger_new(&ledger,directory,"carrier"));int fds[3]={10,11,12};Hfs9pVerifier verifier={ledger,claim_cb,protect_cb,native_identity,NULL,NULL,native_controller};assert(!_construct(&carrier,fds,&endpoint,verifier));
 pthread_mutex_lock(&carrier->mutex);carrier->stopping=true;pthread_mutex_unlock(&carrier->mutex);assert(!pthread_join(carrier->worker,NULL));
 u64 base;assert(!number(initial,0,"base",&base));carrier->high=base;
 int credentials=hfs_json_field(initial,0,"credentials");assert(credentials>=0);unsigned index=0;
 for(u32 i=credentials+1;i<initial->count;i++)if(initial->tokens[i].parent==(u32)credentials){assert(index<hfs_controlled_registry->nr_mounts);credential_init(&fixtures[index++],initial,i);}
 assert(index==hfs_controlled_registry->nr_mounts);
 for(unsigned i=0;i<index;i++){struct hfs_enrollment *e=&hfs_controlled_registry->mounts[i]->enrollment;assert(!hfs9p_add_session_tuple(carrier,(u8 *)e->context,e->context_len,4,e->protocol_features));}
 int captures=hfs_json_field(initial,os,"capture_tuples");assert(captures>=0);
 for(unsigned i=0;i<index;i++){
  struct fixture_session *f=&fixtures[i];f->captures[0]=f->credential;f->capture_bytes[0]=f->credentials;f->nr_captures=1;
  unsigned count=0;
  for(u32 token=captures+1;token<initial->count;token++)if(initial->tokens[token].parent==(u32)captures){
   assert(count<2);
   if(!count){assert(f->credentials.len==initial->tokens[token].end-initial->tokens[token].start&&!memcmp(f->credentials.data,initial->data+initial->tokens[token].start,f->credentials.len));}
   else {credential_init(f,initial,token);f->captures[1]=f->credential;f->capture_bytes[1]=f->credentials;f->nr_captures=2;}
   count++;
  }
  assert(count==f->nr_captures);f->credential=f->captures[0];f->credentials=f->capture_bytes[0];if(!getenv("HFS_C6_BACKEND_ONLY"))session_init(i);
 }
 hfs_json_free(initial);puts("READY");fflush(stdout);
 for(;;){line=read_line();if(!strcmp(line,"END")){free(line);break;}command(line);free(line);}
 return 0;
}
