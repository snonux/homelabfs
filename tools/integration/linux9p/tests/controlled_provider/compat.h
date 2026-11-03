/* SPDX-License-Identifier: GPL-2.0-only */
/* Declared kernel/sysfs/map primitive boundary; production logic is unchanged. */
#ifndef HFS_CONTROLLED_COMPAT_H
#define HFS_CONTROLLED_COMPAT_H
#include "hfs_protocol.h"
#include "hfs_policy.h"
#include <pthread.h>
#include <assert.h>
#include <stdatomic.h>
#include <sys/types.h>
#define check_add_overflow(a,b,out) __builtin_add_overflow(a,b,out)
#define MAX_NON_LFS 0x7fffffff

#include "enrollment_struct.inc"
struct device;
struct super_block;
struct hfs_session;
static inline void sort(void *base,size_t count,size_t size,
   int (*compare)(const void *,const void *),void *swap) {
   (void)swap;qsort(base,count,size,compare);
}
struct mutex { pthread_mutex_t native; };
#define DEFINE_MUTEX(name) struct mutex name = { PTHREAD_MUTEX_INITIALIZER }
static inline void mutex_lock(struct mutex *m) { assert(!pthread_mutex_lock(&m->native)); }
static inline void mutex_unlock(struct mutex *m) { assert(!pthread_mutex_unlock(&m->native)); }
typedef atomic_int atomic_t;
#define ATOMIC_INIT(n) ATOMIC_VAR_INIT(n)
static inline int atomic_inc_return(atomic_t *n) { return atomic_fetch_add(n,1)+1; }
static inline void atomic_dec(atomic_t *n) { atomic_fetch_sub(n,1); }
static inline bool uuid_equal(const uuid_t *a,const uuid_t *b) { return !memcmp(a,b,16); }
static inline bool uuid_is_null(const uuid_t *a) { static const uuid_t z;return uuid_equal(a,&z); }
static inline ssize_t strscpy(char *out,const char *in,size_t size) {
   size_t n=strlen(in);if(n>=size)return -E2BIG;memcpy(out,in,n+1);return n;
}
static inline int hfs_uuid(const struct hfs_json *j,int token,uuid_t *id) { return hfs_policy_uuid(j,token,id); }
struct uid_gid_extent { u32 first,lower_first,count; };
struct uid_gid_map { u32 nr_extents;struct uid_gid_extent extent[5]; };
struct user_namespace { struct uid_gid_map uid_map,gid_map; };
extern struct user_namespace init_user_ns,foreign_user_ns;
extern bool primitive_admin,primitive_initial_ns;
extern uuid_t primitive_boot,primitive_nonce;
static inline struct user_namespace *current_user_ns(void) { return primitive_initial_ns?&init_user_ns:&foreign_user_ns; }
#define CAP_SYS_ADMIN 21
static inline int capable(int capability) { assert(capability==CAP_SYS_ADMIN);return primitive_admin; }
static inline void get_boot_id(u8 out[16]) { memcpy(out,primitive_boot.b,16); }
static inline void uuid_gen(uuid_t *out) { *out=primitive_nonce; }
struct kernfs_node;
struct kernfs_open_file { void *priv; };
struct kernfs_ops {
   bool prealloc;
   int (*open)(struct kernfs_open_file *);
   ssize_t (*read)(struct kernfs_open_file *,char *,size_t,loff_t);
   void (*release)(struct kernfs_open_file *);
   ssize_t (*write)(struct kernfs_open_file *,char *,size_t,loff_t);
   size_t atomic_write_len;
};
struct kernfs_node { const char *name;unsigned mode;const struct kernfs_ops *ops; };
struct kobject { struct kernfs_node *sd; };
extern struct kobject *kernel_kobj;
extern struct kernfs_node primitive_nodes[8];
extern unsigned primitive_node_count;
struct kobject *kobject_create_and_add(const char *,struct kobject *);
static inline void kobject_put(struct kobject *kobject) { (void)kobject; }
#define GLOBAL_ROOT_UID 0
#define GLOBAL_ROOT_GID 0
#define IS_ERR(p) ((uintptr_t)(p)>(uintptr_t)-4096)
#define PTR_ERR(p) ((int)(intptr_t)(p))
struct kernfs_node *__kernfs_create_file(struct kernfs_node *,const char *,unsigned,
   unsigned,unsigned,loff_t,const struct kernfs_ops *,void *,void *,void *);
static inline void kernfs_remove(struct kernfs_node *node) { node->mode=0; }
#endif
