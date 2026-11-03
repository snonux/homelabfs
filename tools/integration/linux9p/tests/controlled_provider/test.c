/* SPDX-License-Identifier: GPL-2.0-only */
/* Actual signed installer/snapshot code; explicit OS primitives, no runtime
 * provider/source/physical authority, mount, SSH or installed kernel proof.
 */
#include <assert.h>
#include <openssl/evp.h>
#include "controlled_compat.h"
#include "hfs_controlled_artifacts.c"
#include "hfs_admission.c"

unsigned int hfs_test_alloc_call,hfs_test_alloc_fail;
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
static char *read_file(const char *path,size_t *length) {
   FILE *f=fopen(path,"rb");assert(f);assert(!fseek(f,0,SEEK_END));
   long n=ftell(f);assert(n>0&&n<1024*1024);rewind(f);
   char *data=malloc(n+1);assert(data&&fread(data,1,n,f)==(size_t)n);
   assert(!fclose(f));data[n]=0;*length=n;return data;
}
static void sign_seed(u8 value,const void *bytes,u32 length,u8 signature[64]) {
   u8 seed[32];memset(seed,value,32);
   EVP_PKEY *key=EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519,NULL,seed,32);
   EVP_MD_CTX *context=EVP_MD_CTX_new();size_t n=64;
   assert(key&&context&&EVP_DigestSignInit(context,NULL,NULL,NULL,key)==1);
   assert(EVP_DigestSign(context,signature,&n,bytes,length)==1&&n==64);
   EVP_MD_CTX_free(context);EVP_PKEY_free(key);
}
/* Test-only deterministic214 keys. Actual OpenSSL signatures ensure policy
 * negatives pass cryptographic checks before the production installer refuses.
 */
static void resign(const struct hfs_buf *payload,struct hfs_buf *wrapper) {
   u8 signature[64];
   assert(!hfs_buf_init(wrapper,HFS_ADMISSION_MAX));
   hfs_buf_literal(wrapper,"{\"group_signatures\":[");
   for(unsigned group=0;group<2;group++) {
      if(group)hfs_buf_literal(wrapper,",");hfs_buf_literal(wrapper,"[");
      for(unsigned domain=0;domain<3;domain++) {
         if(domain)hfs_buf_literal(wrapper,",");
         hfs_buf_literal(wrapper,"{\"guest\":");
         sign_seed(11+2*(group*3+domain),payload->data,payload->len,signature);
         hfs_buf_hex(wrapper,signature,64);hfs_buf_literal(wrapper,",\"host\":");
         sign_seed(10+2*(group*3+domain),payload->data,payload->len,signature);
         hfs_buf_hex(wrapper,signature,64);hfs_buf_literal(wrapper,"}");
      }
      hfs_buf_literal(wrapper,"]");
   }
   hfs_buf_literal(wrapper,"],\"operator_signature\":");
   sign_seed(1,payload->data,payload->len,signature);hfs_buf_hex(wrapper,signature,64);
   hfs_buf_literal(wrapper,",\"payload\":");hfs_buf_add(wrapper,payload->data,payload->len);
   hfs_buf_literal(wrapper,"}");assert(!wrapper->error);
}
static void policy_negative(const struct hfs_json *fixture,int payload,int token,
   const char *replacement) {
   struct hfs_buf changed,wrapper;
   assert(token>=0);assert(!hfs_buf_init(&changed,HFS_ADMISSION_MAX));
   u32 start=fixture->tokens[payload].start,end=fixture->tokens[payload].end;
   hfs_buf_add(&changed,fixture->data+start,fixture->tokens[token].start-start);
   hfs_buf_literal(&changed,replacement);
   hfs_buf_add(&changed,fixture->data+fixture->tokens[token].end,end-fixture->tokens[token].end);
   assert(!changed.error);resign(&changed,&wrapper);
   assert(hfs_install_provision(wrapper.data,wrapper.len)<0);
   assert(!hfs_controlled_registry);
   hfs_buf_free(&changed);hfs_buf_free(&wrapper);
}
static void assert_hash(const struct hfs_json *fixture,int array,u32 index,const u8 hash[32]) {
   int token=array+1;u8 expected[32];u32 length=32;
   for(u32 i=0;i<index;i++)token=hfs_enrollment_next(fixture,token);
   assert(!hfs_json_hex(fixture,token,expected,&length)&&length==32);
   assert(!memcmp(expected,hash,32));
}
static bool oracle_uuid(const struct hfs_json *oracle,int row,
   const char *name,const uuid_t *actual) {
   uuid_t expected;
   return !hfs_uuid(oracle,hfs_json_field(oracle,row,name),&expected)&&
      uuid_equal(&expected,actual);
}
static bool oracle_hex(const struct hfs_json *oracle,int row,
   const char *name,const u8 *actual,u32 actual_length) {
   u8 expected[1024];u32 length=sizeof(expected);
   return !hfs_json_hex(oracle,hfs_json_field(oracle,row,name),expected,&length)&&
      length==actual_length&&!memcmp(expected,actual,length);
}
static bool oracle_text(const struct hfs_json *oracle,int row,
   const char *name,const char *actual) {
   char expected[256];
   return hfs_json_string(oracle,hfs_json_field(oracle,row,name),
      expected,sizeof(expected))>=0&&!strcmp(expected,actual);
}
static bool oracle_mounts(const struct hfs_json *oracle) {
   int rows=hfs_json_field(oracle,0,"mounts"),row=rows+1;
   u32 count;
   if(hfs_enrollment_array(oracle,rows,64,&count)||
      count!=hfs_controlled_registry->nr_mounts)return false;
   for(unsigned i=0;i<count;i++) {
      const struct hfs_controlled_mount *m=hfs_controlled_registry->mounts[i];
      const struct hfs_enrollment *e=&m->enrollment;
      u64 group,index;int context=hfs_json_field(oracle,row,"context");
      if(hfs_json_u64(oracle,hfs_json_field(oracle,row,"group_index"),&group)||
         hfs_json_u64(oracle,hfs_json_field(oracle,row,"domain_index"),&index)||
         group>=hfs_controlled_registry->nr_groups||context<0||
         e->view_scope!=HFS_SCOPE_CONTROLLED_LOCAL||
         m->group!=&hfs_controlled_registry->groups[group]||m->domain_index!=index||
         e->context_len!=oracle->tokens[context].end-oracle->tokens[context].start||
         memcmp(e->context,oracle->data+oracle->tokens[context].start,e->context_len)||
         !oracle_text(oracle,row,"device_tag",m->device_tag)||
         !oracle_text(oracle,row,"device_name",m->device_name)||
         !oracle_hex(oracle,row,"idmap_hash",m->idmap_hash,32)||
         !oracle_hex(oracle,row,"subject",e->subject,e->subject_len)||
         !oracle_uuid(oracle,row,"user_namespace",&m->user_namespace)||
         !oracle_uuid(oracle,row,"session",&e->session)||
         !oracle_uuid(oracle,row,"realm",&e->realm)||
         !oracle_uuid(oracle,row,"anchor",&e->anchor)||
         !oracle_uuid(oracle,row,"nonce",&e->nonce)||
         !oracle_uuid(oracle,row,"acquisition_anchor",&e->acquisition_anchor)||
         !oracle_uuid(oracle,row,"acquisition_nonce",&e->acquisition_nonce)||
         !oracle_uuid(oracle,row,"control_anchor",&e->control_anchor)||
         !oracle_uuid(oracle,row,"view_control_nonce",&e->view_control_nonce))
         return false;
      row=hfs_enrollment_next(oracle,row);
   }
   return true;
}
static bool oracle_snapshot(const struct hfs_json *oracle,const char *name,
   const struct hfs_buf *actual) {
   struct hfs_json *decoded=NULL;
   int expected=hfs_json_field(oracle,0,name);
   if(expected<0||actual->error||
      hfs_json_parse(actual->data,actual->len,&decoded))return false;
   bool equal=!hfs_json_exact(decoded,0,
      "boot_id,domain_protocols,domains,observations,physical_contract,provider_nonce,version")&&
      actual->len==oracle->tokens[expected].end-oracle->tokens[expected].start&&
      !memcmp(actual->data,oracle->data+oracle->tokens[expected].start,actual->len);
   hfs_json_free(decoded);return equal;
}
static void oracle_negative_controls(const struct hfs_json *oracle) {
   struct hfs_buf initial;assert(!hfs_buf_init(&initial,HFS_ADMISSION_MAX));
   assert(!hfs_controlled_snapshot(&initial));
   assert(oracle_snapshot(oracle,"initial",&initial));hfs_buf_free(&initial);
   struct hfs_controlled_mount *first=hfs_controlled_registry->mounts[0];
   u32 index=first->domain_index;
   first->domain_index=1;assert(!oracle_mounts(oracle));first->domain_index=index;
   first->enrollment.nonce.b[0]^=1;assert(!oracle_mounts(oracle));
   first->enrollment.nonce.b[0]^=1;assert(oracle_mounts(oracle));
   struct hfs_buf changed;assert(!hfs_buf_init(&changed,HFS_ADMISSION_MAX));
   /* Deliberate diagnostic state corruption only, no physical-state claim. */
   struct hfs_controlled_mount *second=hfs_controlled_registry->mounts[1];
   second->revision=2;assert(!hfs_controlled_snapshot(&changed));
   assert(!oracle_snapshot(oracle,"initial",&changed));second->revision=1;
   hfs_buf_free(&changed);
   assert(!hfs_buf_init(&changed,HFS_ADMISSION_MAX));
   hfs_controlled_registry->mounts[0]=second;
   hfs_controlled_registry->mounts[1]=first;
   assert(!hfs_controlled_snapshot(&changed));
   assert(!oracle_snapshot(oracle,"initial",&changed));
   hfs_controlled_registry->mounts[0]=first;
   hfs_controlled_registry->mounts[1]=second;hfs_buf_free(&changed);
   assert(!hfs_buf_init(&changed,HFS_ADMISSION_MAX));
   strcpy(second->phase,"STOPPED");second->event_hash[0]=1;
   assert(!hfs_controlled_snapshot(&changed));
   assert(!oracle_snapshot(oracle,"initial",&changed));
   strcpy(second->phase,"LIVE");second->event_hash[0]=0;
   hfs_buf_free(&changed);assert(oracle_mounts(oracle));
   puts("fixture oracle rejects wrong index/ordinary nonce/revision/scope order/phase+hash: PASS");
}
static void synthetic_observer_bound(void) {
   /* Synthetic serializer stress only: repeated actual original scopes are NOT
    * a signed/admitted inventory. No installed registry pointer is replaced.
    */
   struct hfs_controlled_registry *synthetic=calloc(1,sizeof(*synthetic));
   struct hfs_buf measured,bounded;assert(synthetic);
   synthetic->nr_mounts=64;
   for(unsigned i=0;i<64;i++)synthetic->mounts[i]=hfs_controlled_registry->mounts[i%4];
   assert(!hfs_buf_init(&measured,HFS_BODY_MAX));
   hfs_registry_snapshot(synthetic,&measured,true);assert(!measured.error);
   assert(!hfs_buf_init(&bounded,HFS_ADMISSION_MAX));
   hfs_registry_snapshot(synthetic,&bounded,true);
   assert(measured.len>HFS_ADMISSION_MAX&&bounded.error==-ENOSPC);
   printf("synthetic64 actual-scope worst-case observer: %u bytes, bounded refusal: PASS\n",measured.len);
   hfs_buf_free(&measured);hfs_buf_free(&bounded);free(synthetic);
}
static void snapshots(const struct hfs_json *oracle) {
   struct kernfs_open_file files[5]={0};
   struct hfs_buf old;assert(!hfs_buf_init(&old,HFS_ADMISSION_MAX));
   assert(!hfs_controlled_snapshot(&old)&&old.len>4096);
   assert(oracle_snapshot(oracle,"initial",&old));
   for(unsigned i=0;i<4;i++)assert(!hfs_admission_open(&files[i]));
   assert(hfs_admission_open(&files[4])==-ENOSPC&&!files[4].priv);
   hfs_controlled_registry->mounts[0]->revision=9; /* Snapshot primitive only. */
   char *received=malloc(old.len+1);assert(received);
   size_t offset=0;while(offset<old.len) {
      ssize_t n=hfs_admission_read(&files[0],received+offset,37,offset);
      assert(n>0);offset+=n;
   }
   assert(offset==old.len&&!memcmp(received,old.data,old.len));
   struct hfs_buf frozen={.data=received,.len=offset};
   assert(oracle_snapshot(oracle,"initial",&frozen));
   assert(!hfs_admission_read(&files[0],received,1,offset));
   assert(hfs_admission_read(&files[0],received,1,-1)==-EINVAL);
   char seek[23];
   assert(hfs_admission_read(&files[0],seek,sizeof(seek),101)==sizeof(seek));
   assert(!memcmp(seek,old.data+101,sizeof(seek)));
   assert(!hfs_admission_read(&files[0],seek,sizeof(seek),old.len+99));
   for(unsigned i=0;i<4;i++)hfs_admission_release(&files[i]);
   for(unsigned failure=1;failure<=2;failure++) {
      hfs_test_alloc_fail=hfs_test_alloc_call+failure;
      assert(hfs_admission_open(&files[0])==-ENOMEM&&!files[0].priv);
      hfs_test_alloc_fail=0;
      assert(atomic_load(&hfs_admission_opens)==0);
   }
   for(unsigned i=0;i<4;i++)assert(!hfs_admission_open(&files[i]));
   struct hfs_buf current;assert(!hfs_buf_init(&current,HFS_ADMISSION_MAX));
   assert(!hfs_controlled_snapshot(&current));
   assert(oracle_snapshot(oracle,"revised",&current));
   assert(current.len!=old.len||memcmp(current.data,old.data,old.len));
   assert(!hfs_admission_read(&files[0],received,0,0));
   assert(current.len<=old.len+1);
   ssize_t first=hfs_admission_read(&files[0],received,current.len,0);
   assert(first==(ssize_t)current.len&&!memcmp(received,current.data,current.len));
   frozen.len=first;assert(oracle_snapshot(oracle,"revised",&frozen));
   for(unsigned i=0;i<4;i++) {
      hfs_admission_release(&files[i]);hfs_admission_release(&files[i]);
   }
   assert(atomic_load(&hfs_admission_opens)==0);
   hfs_buf_free(&current);
   free(received);hfs_buf_free(&old);
}
int main(int argc,char **argv) {
   assert(argc==3||argc==4);size_t length;char *bytes=read_file(argv[1],&length);
   if(length&&bytes[length-1]=='\n')bytes[--length]=0;
   struct hfs_json *fixture=NULL;assert(!hfs_json_parse(bytes,length,&fixture));
   int provision=hfs_json_field(fixture,0,"provision");
   int payload=hfs_json_field(fixture,provision,"payload");
   /* Actual primitive boot/nonce remain independent of the candidate payload. */
   int first_bind=hfs_json_field(fixture,0,"binds")+1;
   int boot_payload=hfs_json_field(fixture,first_bind,"payload");
   assert(!hfs_uuid(fixture,hfs_json_field(fixture,boot_payload,"boot_id"),&primitive_boot));
   assert(!hfs_uuid(fixture,hfs_json_field(fixture,boot_payload,"provider_nonce"),&primitive_nonce));
   assert(!hfs_controlled_artifacts_init());
   assert(primitive_node_count==3&&primitive_nodes[0].mode==0400);
   assert(primitive_nodes[1].mode==0200&&primitive_nodes[2].mode==0200);
   assert(!strcmp(primitive_nodes[0].name,"admission_v1"));
   assert(!strcmp(primitive_nodes[1].name,"provision_v1"));
   assert(!strcmp(primitive_nodes[2].name,"bind_v1"));
   assert(primitive_nodes[1].ops->atomic_write_len==HFS_ADMISSION_MAX);
   assert(primitive_nodes[2].ops->atomic_write_len==HFS_ADMISSION_MAX);
   primitive_admin=false;
   assert(hfs_provision_write(NULL,"{}",2,0)==-EPERM);
   assert(hfs_bind_write(NULL,"{}",2,0)==-EPERM);primitive_admin=true;
   primitive_initial_ns=false;
   assert(hfs_provision_write(NULL,"{}",2,0)==-EPERM);
   assert(hfs_bind_write(NULL,"{}",2,0)==-EPERM);primitive_initial_ns=true;
   assert(hfs_provision_write(NULL,"{}",2,1)==-EINVAL);
   assert(hfs_install_provision("",0)<0&&hfs_install_provision("x",HFS_ADMISSION_MAX+1)<0);
   const char *raw=fixture->data+fixture->tokens[provision].start;
   u32 n=fixture->tokens[provision].end-fixture->tokens[provision].start;
   if(argc==3&&!strcmp(argv[2],"refuse")) {
      assert(hfs_install_provision(raw,n)<0);
      assert(!hfs_controlled_registry);
      printf("signed admission refusal: %s: PASS\n",argv[1]);
      hfs_json_free(fixture);free(bytes);return 0;
   }
   assert(argc==4&&!strcmp(argv[2],"accept"));
   size_t oracle_length;char *oracle_bytes=read_file(argv[3],&oracle_length);
   struct hfs_json *oracle=NULL;
   assert(!hfs_json_parse(oracle_bytes,oracle_length,&oracle));
   int groups=hfs_json_field(fixture,payload,"groups"),group=groups+1;
   int mounts=hfs_json_field(fixture,group,"local_mounts"),mount=mounts+1;
   int budget=hfs_json_field(fixture,payload,"budget");
   policy_negative(fixture,payload,hfs_json_field(fixture,mount,"domain_index"),"1");
   policy_negative(fixture,payload,hfs_json_field(fixture,mount,"view_control_nonce"),"\"ffffffff-ffff-4fff-bfff-ffffffffffff\"");
   policy_negative(fixture,payload,hfs_json_field(fixture,mount,"idmap_hash"),"\"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\"");
   policy_negative(fixture,payload,hfs_json_field(fixture,budget,"ordinary_bytes"),"0");
   policy_negative(fixture,payload,hfs_json_field(fixture,budget,"control_bytes"),"18446744073709551615");
   policy_negative(fixture,payload,hfs_json_field(fixture,mount,"device_name"),"\"bad/name\"");
   init_user_ns.uid_map.extent[0].count--;
   assert(hfs_install_provision(fixture->data+fixture->tokens[provision].start,
      fixture->tokens[provision].end-fixture->tokens[provision].start)<0);
   assert(!hfs_controlled_registry);init_user_ns.uid_map.extent[0].count++;
   /* Exhaust every allocation position before the first successful transaction.
    * The successful final attempt remains installed for original/hash/bind checks.
    */
   unsigned fault;
   for(fault=1;fault<256;fault++) {
      hfs_test_alloc_fail=hfs_test_alloc_call+fault;
      int result=hfs_install_provision(raw,n);
      hfs_test_alloc_fail=0;
      if(!result)break;
      assert(!hfs_controlled_registry);
   }
   assert(fault<256&&hfs_test_last_allocation_size==HFS_ADMISSION_MAX);
   /* The final allocation is the actual worst-case preflight buffer. Its
    * injected failure above must leave the registry unpublished as well.
    */
   printf("transaction allocation rollback positions: %u: PASS\n",fault-1);
   assert(hfs_controlled_registry->nr_groups==2&&hfs_controlled_registry->nr_mounts==4);
   assert(hfs_install_provision(raw,n)==-EALREADY);
   for(unsigned i=0;i<2;i++) {
      const struct hfs_signed_enrollment *e=hfs_controlled_registry->groups[i].enrollment;
      assert(e->count==3);
      int group_token=groups+1;
      if(i)group_token=hfs_enrollment_next(fixture,group_token);
      int encoded=hfs_json_field(fixture,group_token,"enrollment_bytes");
      u32 original_length=(fixture->tokens[encoded].end-fixture->tokens[encoded].start-2)/2;
      u8 *original=malloc(original_length);assert(original);
      assert(!hfs_json_hex(fixture,encoded,original,&original_length));
      assert(e->json->len==original_length);
      assert(!memcmp(e->json->data,original,original_length));free(original);
      for(unsigned d=0;d<3;d++) {
         assert(uuid_equal(&e->domains[d].boot,&primitive_boot)==(d!=1));
      }
      assert_hash(fixture,hfs_json_field(fixture,0,"enrollment_hashes"),i,e->enrollment_hash);
      assert_hash(fixture,hfs_json_field(fixture,0,"profile_hashes"),i,e->profile_hash);
   }
   if(hfs_json_field(fixture,0,"distinct_identities")>=0) {
      const char *names[]={"volume","origin_journal","origin_incarnation"};
      for(unsigned field=0;field<ARRAY_SIZE(names);field++) {
         uuid_t identities[2];
         for(unsigned i=0;i<2;i++) {
            const struct hfs_signed_enrollment *e=hfs_controlled_registry->groups[i].enrollment;
            assert(!hfs_policy_uuid(e->json,hfs_json_field(e->json,
               e->domains[0].scope,names[field]),&identities[i]));
         }
         assert(!uuid_equal(&identities[0],&identities[1]));
      }
   }
   assert(oracle_mounts(oracle));oracle_negative_controls(oracle);
   int rejected=hfs_json_field(fixture,0,"rejected_binds");
   if(rejected>=0) {
      u32 count;assert(!hfs_enrollment_array(fixture,rejected,32,&count));
      int candidate=rejected+1;
      for(unsigned i=0;i<count;i++) {
         assert(hfs_install_bind(fixture->data+fixture->tokens[candidate].start,
            fixture->tokens[candidate].end-fixture->tokens[candidate].start)<0);
         assert(!hfs_controlled_registry->groups[0].bound);
         assert(!hfs_controlled_registry->groups[1].bound);
         candidate=hfs_enrollment_next(fixture,candidate);
      }
      printf("signed controller binding refusals: %u: PASS\n",count);
   }
   int binds=hfs_json_field(fixture,0,"binds"),bind=binds+1;
   for(unsigned i=0;i<2;i++) {
      raw=fixture->data+fixture->tokens[bind].start;n=fixture->tokens[bind].end-fixture->tokens[bind].start;
      assert(!hfs_install_bind(raw,n));assert(hfs_install_bind(raw,n)==-EALREADY);
      uuid_t controller;
      assert(!hfs_uuid(fixture,hfs_json_field(fixture,
         hfs_json_field(fixture,bind,"payload"),"controller"),&controller));
      assert(hfs_controlled_registry->groups[i].bound);
      assert(uuid_equal(&hfs_controlled_registry->groups[i].controller,&controller));
      bind=hfs_enrollment_next(fixture,bind);
   }
   synthetic_observer_bound();snapshots(oracle);
   hfs_json_free(oracle);free(oracle_bytes);
   hfs_json_free(fixture);free(bytes);
   puts("real signed transactional group installer + immutable snapshot primitives: PASS");return 0;
}
