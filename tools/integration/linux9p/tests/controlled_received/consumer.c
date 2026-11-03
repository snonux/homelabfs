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
#include "session.inc"
#include "lease.inc"
#include "release_prepare.inc"
#include "hfs_view.c"
struct hfs_mmap_control { struct hfs_view *view;struct hfs_buf ack,last_ack; };
#include "physical.inc"

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
 struct hfs_session session;
 struct p9_client client;
 struct virtio_chan channel;
 struct virtio_device device;
 struct super_block super;
 struct hfs_cred credential;
 struct cred native_cred;
 struct hfs_buf credentials;
 struct hfs_json *latest,*ordinary_result;
 struct hfs_cap *root,*description;
 struct hfs_lease *lease;
 struct hfs_mmap_control physical;
};
static struct fixture_session fixtures[64];
static Hfs9p *carrier;
static unsigned current_command;
static char *read_line(void) { char *line=NULL;size_t cap=0;ssize_t n=getline(&line,&cap,stdin);assert(n>0);if(line[n-1]=='\n')line[n-1]=0;return line; }
static void emit_hex(const void *bytes,size_t length) { const u8 *p=bytes;for(size_t i=0;i<length;i++)printf("%02x",p[i]); }
static size_t decode_hex(const char *s,void *out,size_t capacity) { size_t n=strlen(s)/2;assert(strlen(s)%2==0&&n<=capacity);u8 *p=out;for(size_t i=0;i<n;i++){int a=_hex_digit(s[i*2]),b=_hex_digit(s[i*2+1]);assert(a>=0&&b>=0);p[i]=a*16+b;}return n; }
static void emit_snapshot(void) { struct hfs_buf snapshot={0};assert(!hfs_buf_init(&snapshot,HFS_ADMISSION_MAX));assert(!hfs_controlled_snapshot(&snapshot));printf("S ");emit_hex(snapshot.data,snapshot.len);puts("");fflush(stdout);hfs_buf_free(&snapshot); }
int primitive_fstat(int fd,struct stat *st) { assert(fd>=10&&fd<=12);memset(st,0,sizeof(*st));st->st_mode=S_IFREG|0600;st->st_ino=fd+100;st->st_size=HFS9P_VSIZE;return 0; }
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
static int claim_cb(void *arg,const Hfs9pEndpoint *e,const u8 hash[32]) { return hfs9p_ledger_claim(arg,e,hash); }
static int protect_cb(void *arg,const u8 *p,size_t n) { return hfs9p_ledger_protect(arg,p,n); }
static int primitive_rpc_exchange(struct p9_client *client,u16 tag,u8 op,u8 channel,u64 cell,u32 chunk,const void *request,u32 request_len,void *reply,u32 *reply_len) {
 unsigned index;for(index=0;index<64;index++)if(client==&fixtures[index].client)break;assert(index<carrier->session_count);
 int err;if(op==1){err=_send(carrier,index,tag,request,request_len);if(err)return -err;assert(*reply_len>=1);*(u8 *)reply=1;*reply_len=1;return 0;}
 assert(op==2&&*reply_len>1);size_t length=0;err=_fetch(carrier,index,cell,chunk,channel,(u8 *)reply+1,*reply_len-1,&length);
 if(err==EAGAIN){*(u8 *)reply=2;*reply_len=1;return 0;}if(err)return -err;*(u8 *)reply=3;*reply_len=length+1;return 0;
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
 if(err){free(request->rc.sdata);free(request);return (void *)(intptr_t)err;}
 request->rc.size=count;request->rc.id=P9_RBRIDGE;request->rc.tag=request->tc.tag=tag;return request;
}
static void p9_req_put(struct p9_client *client,struct p9_req_t *request) { (void)client;free(request->rc.sdata);free(request); }
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
 struct hfs_cred *c=&f->credential;u64 n;u32 length=32;
 assert(!number(j,token,"credential_revision",&c->revision));
 assert(!number(j,token,"fsuid",&n)&&n<=UINT32_MAX);c->uid=n;
 assert(!number(j,token,"fsgid",&n)&&n<=UINT32_MAX);c->gid=n;
 assert(!hfs_uuid(j,hfs_json_field(j,token,"process"),&c->process));
 assert(!hfs_uuid(j,hfs_json_field(j,token,"user_namespace"),&c->user_namespace));
 assert(!hfs_json_hex(j,hfs_json_field(j,token,"idmap_hash"),c->idmap_hash,&length)&&length==32);
 int groups=hfs_json_field(j,token,"groups");assert(groups>=0);
 for(u32 i=groups+1;i<j->count;i++)if(j->tokens[i].parent==(u32)groups){assert(c->ngroups<HFS_GROUPS_MAX&&!hfs_json_u64(j,i,&n)&&n<=UINT32_MAX);c->groups[c->ngroups++]=n;}
 f->native_cred.user_ns=&init_user_ns;c->native=&f->native_cred;
 assert(!hfs_buf_init(&f->credentials,16384));hfs_cred_write(&f->credentials,c);assert(!f->credentials.error);
 assert(f->credentials.len==j->tokens[token].end-j->tokens[token].start&&!memcmp(f->credentials.data,j->data+j->tokens[token].start,f->credentials.len));
}
#include "bootstrap_test.inc"
static void session_init(unsigned i) {
 struct fixture_session *f=&fixtures[i];struct hfs_session *s=&f->session;
 struct hfs_controlled_mount *mount=hfs_controlled_registry->mounts[i];
 s->provider=hfs_provider;s->bridge_minor=4;s->client=&f->client;s->next_cell=1;
 mutex_init(&s->lock);mutex_init(&s->writer);mutex_init(&s->retirement);
 INIT_LIST_HEAD(&s->global);
 INIT_LIST_HEAD(&s->uncertain);INIT_LIST_HEAD(&s->requests);INIT_LIST_HEAD(&s->view_tickets);INIT_LIST_HEAD(&s->view_families);
 assert(!s->provider->enroll(mount->device_tag,&s->enrollment));
 assert(!hfs_enrollment_validate(&s->enrollment));
 assert(!hfs_enrollment_lanes(&s->enrollment,s->bridge_minor));
 mount_lane_regression(&s->enrollment);
 f->device.dev=(struct device){.references=1,.registered=true,.name=mount->device_name};
 f->device.priv=&f->channel;f->channel=(struct virtio_chan){.inuse=true,.client=&f->client,.vdev=&f->device,.tag=mount->device_tag};
 list_add_tail(&f->channel.chan_list,&virtio_chan_list);
 f->client=(struct p9_client){.trans=&f->channel,.trans_mod=&p9_virtio_trans,.status=Connected,.proto_version=p9_proto_hfs14,.msize=65536};
 mutex_init(&f->client.hfs_control_tag);mutex_init(&f->client.hfs_wait_tag);
 assert(!hfs_controlled_bind_transport(s));
 bootstrap_failure_regression(f);
 struct hfs_buf hello={.data=(char *)hfs_protocol_text(4),.len=strlen(hfs_protocol_text(4))};
 struct hfs_json *response=NULL;
 /* Direct pre-super operations must refuse before touching native transport. */
 assert(hfs_control(s,"ALLOCATE",&hello,false,&response)<0&&!response);
 assert(hfs_control(s,"HELLO",&hello,true,&response)<0&&!response);
 struct hfs_buf malformed={.data="{}",.len=2};assert(hfs_control(s,"HELLO",&malformed,false,&response)<0&&!response);
 assert(!hfs_control(s,"HELLO",&hello,false,&response));hfs_json_free(response);
 f->super=(struct super_block){.s_fs_info=s,.s_dev=1000+i,.s_user_ns=&init_user_ns};
 assert(!hfs_controlled_bind_super(s,&f->super));
 accounting_regression(s);
 uuid_t user_namespace;u8 map[32];assert(!s->provider->map(s,&f->native_cred,&init_user_ns,&user_namespace,map));
 assert(uuid_equal(&user_namespace,&f->credential.user_namespace)&&!memcmp(map,f->credential.idmap_hash,32));
}
static void done(unsigned id,int error,const void *body,size_t length) { printf("D %u %d ",id,error);if(body&&length)emit_hex(body,length);else printf("-");puts("");fflush(stdout); }
static int fixture_result(struct fixture_session *f,const char *opcode) {
 struct hfs_json *reply=f->latest,*result=NULL;if(!reply)return -EINVAL;
 int payload=hfs_json_field(reply,0,"payload");
 if(!hfs_json_is(reply,hfs_json_field(reply,payload,"status"),"\"TERMINAL\""))return -EPROTO;
 int token=hfs_json_field(reply,payload,"result");int err=hfs_received_copy(reply,token,&result);
 if(!err)err=hfs_result_validate(&f->session,result,opcode,&f->credentials);
 if(err){hfs_json_free(result);return err;}
 hfs_json_free(f->ordinary_result);f->ordinary_result=result;return 0;
}
static int fixture_check(struct fixture_session *f,const char *check) {
 struct hfs_session *s=&f->session;struct hfs_json *json=NULL,*forged=NULL;
 int token=-1,err=-EINVAL;void *cookie=NULL;
 if(!strcmp(check,"cookie_balance"))return hfs_controlled_nr_cookies?-EUCLEAN:0;
 if(!strcmp(check,"cap")){struct hfs_cap *cap=f->description?f->description:f->root;if(!cap)return -EINVAL;u32 before=hfs_controlled_nr_cookies;err=s->provider->enter(s,cap,false,&cookie);if(!err){if(!cookie||hfs_controlled_nr_cookies!=before+1)return -EUCLEAN;s->provider->leave(s,cookie);if(hfs_controlled_nr_cookies!=before)return -EUCLEAN;}return err;}
 if(!strcmp(check,"retirement_refuses")){u8 zero[32]={0};return s->provider->retirement(s,1,zero,f->latest)<0&&s->provider->view_retirement(s,&f->credentials,1,zero)<0&&s->provider->retire_session(s)<0?0:-EUCLEAN;}
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
 assert(sscanf(line,"%31s %u %u %n",operation,&id,&index,&consumed)>=3&&index<hfs_controlled_registry->nr_mounts);
 current_command=id;struct fixture_session *f=&fixtures[index];
 if(!strcmp(operation,"RUN")) {
  unsigned reserved;char op[32];int used=0;assert(sscanf(line+consumed,"%u %31s %n",&reserved,op,&used)==2&&reserved<=1);
  struct hfs_buf payload={0};assert(!hfs_buf_init(&payload,HFS_BODY_MAX));payload.len=decode_hex(line+consumed+used,payload.data,HFS_BODY_MAX);
  hfs_json_free(f->latest);f->latest=NULL;int err=hfs_control(&f->session,op,&payload,reserved,&f->latest);hfs_buf_free(&payload);
  done(id,err,f->latest?f->latest->data:NULL,f->latest?f->latest->len:0);return;
 }
 if(!strcmp(operation,"CHECK")){done(id,fixture_check(f,line+consumed),NULL,0);return;}
 if(!strcmp(operation,"ADOPT_ROOT")){
  int err=fixture_result(f,"ROOT");if(!err)err=hfs_reply_cap(&f->session,f->ordinary_result,"ROOT",&f->root);
  if(!err)err=hfs_reply_cap(&f->session,f->ordinary_result,"DOMAIN",&f->session.domain);
  if(!err)err=hfs_view_new_unbound(&f->session);
  if(!err){f->physical.view=f->session.view;assert(!hfs_buf_init(&f->physical.ack,HFS_VIEW_BODY_MAX));assert(!hfs_buf_init(&f->physical.last_ack,HFS_VIEW_BODY_MAX));}
  done(id,err,err?NULL:f->session.domain->json->data,err?0:f->session.domain->json->len);return;
 }
 if(!strcmp(operation,"ADOPT_DESCRIPTION")){
  int err=fixture_result(f,line+consumed);if(!err)err=hfs_reply_cap(&f->session,f->ordinary_result,"DESCRIPTION",&f->description);
  done(id,err,err?NULL:f->description->json->data,err?0:f->description->json->len);return;
 }
 if(!strcmp(operation,"ACQUIRE")){
  struct hfs_lease *lease=NULL;int err=hfs_lease_acquire(&f->session,&f->credential,NULL,NULL,0,false,NULL,&lease);
  if(!err){
   /* Consumption is checked BEFORE initial physical binding; no phase
    * exception is invented to keep an old grant fresh afterwards. */
   assert(!hfs_received_source(&f->session,lease->json,0));
   assert(hfs_received_source(NULL,lease->json,0)<0);
   struct hfs_session outsider={.bridge_minor=4};
   assert(hfs_received_source(&outsider,lease->json,0)<0);
   outsider.provider=f->session.provider;
   assert(hfs_received_source(&outsider,lease->json,0)<0);
   if(hfs_controlled_registry->nr_mounts>1){unsigned other=f==&fixtures[0]?1:0;assert(hfs_received_source(&fixtures[other].session,lease->json,0)<0);}
   struct hfs_json *detached=NULL;assert(!hfs_json_parse(lease->json->data,lease->json->len,&detached));detached->transported=true;
   assert(hfs_received_source(&f->session,detached,0)<0);hfs_json_free(detached);
   assert(hfs_received_source(&f->session,lease->json,hfs_json_field(lease->json,0,"target"))<0);
   bool initial=f->session.view->phase==HFS_VIEW_UNBOUND;
   err=hfs_view_bind_initial(f->session.view,lease);
   if(!err&&initial)assert(hfs_received_source(&f->session,lease->json,0)<0);
  }
  if(!err){assert(!f->lease);f->lease=lease;}
  done(id,err,err?NULL:lease->json->data,err?0:lease->json->len);return;
 }
 if(!strcmp(operation,"RELEASE_START")){
  struct hfs_lease *lease=f->lease;assert(lease);struct hfs_view_ticket *ticket=lease->release_ticket;struct hfs_buf *payload=&lease->release_payload;
  int err=hfs_view_ticket_activate(ticket);payload->len=0;payload->error=0;
  hfs_buf_literal(payload,"{\"lease\":");hfs_buf_add(payload,lease->json->data,lease->json->len);hfs_buf_literal(payload,",\"request\":");hfs_buf_add(payload,ticket->request.data,ticket->request.len);hfs_buf_literal(payload,"}");
  if(!err){hfs_json_free(f->latest);f->latest=NULL;err=fixture_prepare_exchange(&f->session,ticket,"VIEW_RELEASE",payload,&f->latest);}
  if(!err){hfs_json_free(f->latest);f->latest=NULL;err=hfs_control(&f->session,"VIEW_RELEASE",payload,true,&f->latest);}
  done(id,err,f->latest?f->latest->data:NULL,f->latest?f->latest->len:0);return;
 }
 if(!strcmp(operation,"PHASE")){
  struct hfs_json *event=f->latest;struct hfs_view *view=f->session.view;const char *phase=line+consumed;
  int body=event?hfs_json_field(event,hfs_json_field(event,0,"payload"),"event"):-1;
  int err=body<0?-EINVAL:f->session.provider->view_event(&f->session,event,body);
  uuid_t recall;u64 sequence;if(!err&&(hfs_uuid(event,hfs_json_field(event,body,"recall"),&recall)||number(event,body,"sequence",&sequence)))err=-EPROTO;
  bool manifest=!strcmp(phase,"STOPPED")||!strcmp(phase,"DRAINED"),target=strcmp(phase,"STOPPED")!=0;
  if(!err){if(!strcmp(phase,"STOPPED")){err=hfs_view_stop(view,&view->generation,&recall,sequence);if(!err)err=hfs_view_seal(view);}
   else if(!strcmp(phase,"INSTALLED_DRAIN")||!strcmp(phase,"INSTALLED_FINAL"))err=hfs_mmap_install(view,event,hfs_json_field(event,body,"target"),&recall,sequence,!strcmp(phase,"INSTALLED_FINAL"));
   else if(!strcmp(phase,"DRAINED"))err=hfs_mmap_finish_drained(view);
   else if(!strcmp(phase,"REOPENED"))err=hfs_view_reopen(view,&recall,sequence);else err=-EINVAL;}
  if(!err)err=hfs_mmap_ack(&f->physical,event,body,phase,manifest,target);
  emit_snapshot();done(id,err,NULL,0);return;
 }
 done(id,-EOPNOTSUPP,NULL,0);
}
int main(void) {
 char *line=read_line();struct hfs_json *initial=NULL;assert(!hfs_json_parse(line,strlen(line),&initial));free(line);
 assert(!hfs_json_exact(initial,0,"base,binds,credentials,os,provision"));
 int os=hfs_json_field(initial,0,"os");assert(!hfs_uuid(initial,hfs_json_field(initial,os,"boot_id"),&primitive_boot));assert(!hfs_uuid(initial,hfs_json_field(initial,os,"provider_nonce"),&primitive_nonce));
 assert(!hfs_controlled_init()&&hfs_provider==&hfs_controlled_provider);
 int provision=hfs_json_field(initial,0,"provision");assert(provision>=0);
 assert(!hfs_install_provision(initial->data+initial->tokens[provision].start,initial->tokens[provision].end-initial->tokens[provision].start));
 int binds=hfs_json_field(initial,0,"binds");assert(binds>=0);
 for(u32 i=binds+1;i<initial->count;i++)if(initial->tokens[i].parent==(u32)binds)assert(!hfs_install_bind(initial->data+initial->tokens[i].start,initial->tokens[i].end-initial->tokens[i].start));
 Hfs9pEndpoint endpoint={{0},{110,111,112}};struct hfs_json *context=NULL;
 struct hfs_controlled_mount *first=hfs_controlled_registry->mounts[0];assert(!hfs_json_parse(first->enrollment.context,first->enrollment.context_len,&context));uuid_t fuse;assert(!hfs_uuid(context,hfs_json_field(context,0,"fuse_session"),&fuse));memcpy(endpoint.session,fuse.b,16);hfs_json_free(context);
 int directory=open(getenv("HFS_C6_LEDGER"),O_RDONLY|O_DIRECTORY);assert(directory>=0);Hfs9pLedger *ledger=NULL;assert(!hfs9p_ledger_new(&ledger,directory,"carrier"));int fds[3]={10,11,12};Hfs9pVerifier verifier={ledger,claim_cb,protect_cb,native_identity,NULL,NULL};assert(!_construct(&carrier,fds,&endpoint,verifier));
 pthread_mutex_lock(&carrier->mutex);carrier->stopping=true;pthread_mutex_unlock(&carrier->mutex);assert(!pthread_join(carrier->worker,NULL));
 u64 base;assert(!number(initial,0,"base",&base));carrier->high=base;
 int credentials=hfs_json_field(initial,0,"credentials");assert(credentials>=0);unsigned index=0;
 for(u32 i=credentials+1;i<initial->count;i++)if(initial->tokens[i].parent==(u32)credentials){assert(index<hfs_controlled_registry->nr_mounts);credential_init(&fixtures[index++],initial,i);}
 assert(index==hfs_controlled_registry->nr_mounts);
 for(unsigned i=0;i<index;i++){struct hfs_enrollment *e=&hfs_controlled_registry->mounts[i]->enrollment;assert(!hfs9p_add_session_protocol(carrier,(u8 *)e->context,e->context_len,4));}
 for(unsigned i=0;i<index;i++)session_init(i);
 hfs_json_free(initial);puts("READY");fflush(stdout);
 for(;;){line=read_line();if(!strcmp(line,"END")){free(line);break;}command(line);free(line);}
 return 0;
}
