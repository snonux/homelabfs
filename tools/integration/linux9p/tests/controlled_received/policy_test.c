// SPDX-License-Identifier: GPL-2.0-only
/* Inert source policy only: no received factory/provider/attestation authority. */
#include <assert.h>
#include "hfs_protocol.h"
unsigned hfs_test_alloc_call,hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
static void child(struct hfs_json *j,const char *name,struct hfs_json **out) {
 int token=hfs_json_field(j,0,name);assert(token>=0);
 assert(!hfs_json_parse(j->data+j->tokens[token].start,j->tokens[token].end-j->tokens[token].start,out));
}
int main(int argc,char **argv) {
 assert(argc>1);
 for(int index=1;index<argc;index++){
  FILE *file=fopen(argv[index],"rb");assert(file);assert(!fseek(file,0,SEEK_END));long length=ftell(file);assert(length>0&&length<HFS_BODY_MAX);rewind(file);
  char *bytes=malloc(length);assert(bytes&&fread(bytes,1,length,file)==(size_t)length);assert(!fclose(file));
  struct hfs_json *fixture=NULL,*scope=NULL,*original=NULL,*response=NULL;
  assert(!hfs_json_parse(bytes,length,&fixture));free(bytes);
  child(fixture,"scope",&scope);child(fixture,"original",&original);child(fixture,"response",&response);
  bool expected=hfs_json_is(fixture,hfs_json_field(fixture,0,"accept"),"true");
  int result=hfs_mapped_source_policy(scope,0,original,response,0);
  if((result==0)!=expected){fprintf(stderr,"policy %s returned%d expected%s\n",argv[index],result,expected?"accept":"refuse");return 1;}
  hfs_json_free(scope);hfs_json_free(original);hfs_json_free(response);hfs_json_free(fixture);
 }
 printf("inert source policy: %d cases passed; no authority minted\n",argc-1);return 0;
}
