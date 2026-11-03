// SPDX-License-Identifier: GPL-2.0-only
#include <assert.h>
#include <stdbool.h>
#include <stdio.h>

struct mm_struct { unsigned long flags; };
struct vm_operations_struct { void *cache_begin; };
struct vm_area_struct {
   struct mm_struct *vm_mm;
   struct vm_operations_struct *vm_ops;
   unsigned long vm_end;
   void *vm_file;
   void *anon_vma;
   bool dax, executable, anonymous, temporary, suitable;
};
#define VM_NOHUGEPAGE 1UL
#define VM_HUGEPAGE 2UL
#define VM_NO_KHUGEPAGED 4UL
#define MMF_DISABLE_THP 0
#define TRANSPARENT_HUGEPAGE_NEVER_DAX 1
#define HPAGE_PMD_SIZE (2UL * 1024 * 1024)
static unsigned long transparent_hugepage_flags;
#define test_bit(n, p) ((*(p) & (1UL << (n))) != 0)
#define vma_is_dax(v) ((v)->dax)
#define transhuge_vma_suitable(v, a) ((void)(a), (v)->suitable)
#define shmem_file(f) ((void)(f), false)
#define shmem_huge_enabled(v, b) ((void)(v), (void)(b), false)
#define hugepage_flags_enabled() true
#define hugepage_flags_always() true
#define file_thp_enabled(v) ((v)->executable)
#define vma_is_anonymous(v) ((v)->anonymous)
#define vma_is_temporary_stack(v) ((v)->temporary)
#include "production.inc"

int main(void)
{
   struct mm_struct mm = {0};
   struct vm_operations_struct ops = {.cache_begin = &mm};
   struct vm_area_struct vma = {.vm_mm = &mm, .vm_ops = &ops,
       .vm_end = 4UL * HPAGE_PMD_SIZE, .suitable = true, .executable = true};
   for (int fault = 0; fault < 2; fault++)
      for (int sysfs = 0; sysfs < 2; sysfs++) {
         assert(!hugepage_vma_check(&vma, VM_NOHUGEPAGE, false, fault, sysfs));
         /* MADV_HUGEPAGE clears NOHUGEPAGE and sets HUGEPAGE; the observer
          * contract still refuses collapse and executable file THP.
          */
         assert(!hugepage_vma_check(&vma, VM_HUGEPAGE, false, fault, sysfs));
         assert(!hugepage_vma_check(&vma, 0, true, fault, sysfs));
      }
   vma.vm_ops = NULL;
   assert(hugepage_vma_check(&vma, VM_HUGEPAGE, false, false, false));
   vma.executable = false;
   vma.anonymous = true;
   vma.anon_vma = &mm;
   assert(hugepage_vma_check(&vma, VM_HUGEPAGE, false, true, true));
   puts("actual common THP refusal checks passed; no VFS/THP execution");
   return 0;
}
