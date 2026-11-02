# Reconstruct the R1 text for diff display only; verify every byte against the
# original freeze manifest before accepting a reconstructed baseline.
import pathlib,json,hashlib,subprocess,difflib,importlib.util
root=pathlib.Path.cwd();ev=root/'evidence/sz3-20261102';freeze=json.loads((ev/'freeze.json').read_text())['files'];old={p:pathlib.Path(p).read_text() for p in freeze}
p='internal/fsservice/logical.go';s=old[p];start=s.index('\treturn p.plan(',s.index('func (p *LogicalPlanner) Plan('));end=s.index('\tif err := p.boundCommand',start);s=s[:start]+s[end:];s=s.replace('\n\t"errors"','');start=s.index('// ErrDelegatedFileTooLarge');end=s.index('// NewLogicalPlanner',start);s=s[:start]+s[end:];start=s.index('\tif maxEnd != nil {');end=s.index('\tconfig := planningConfig',start);s=s[:start]+s[end:];s=s.replace('\n\t\tMaxEnd      *int64 `json:",omitempty"`','').replace('}{1, cred.Clone(), bound, maxEnd}','}{1, cred.Clone(), bound}');old[p]=s
p='internal/guestbridge/logical.go';s=old[p].replace('\n\t"encoding/json"','');start=s.index('\t\tvar plan fsservice.Planned');end=s.index('\n\t\tif err != nil {',s.index('\n\t\t} else {',start));s=s[:start]+'\t\tplan, err := c.planner.Plan(ctx, c.observation, cred, command, b.Operation, b.Allocation)'+s[end:];old[p]=s
p='internal/guestbridge/types.go';s=old[p];start=s.index('\t// DelegatedError');end=s.index('\n\tResult ',start);old[p]=s[:start]+s[end+1:]
p='internal/guestbridge/result.go';s=old[p].replace('\n\t"errors"','').replace('\n\t"github.com/snonux/homelabfs/internal/fsservice"','');start=s.index('\tresultCode :=');end=s.index('\tr := map',start);s=s[:start]+s[end:];old[p]=s.replace('"code": resultCode','"code": errnoNames()[out.Result.Code]').replace('if out.Result.Code == fsapi.OK && out.DelegatedError == nil {','if out.Result.Code == fsapi.OK {')
p='internal/guestbridge/schema.go';old[p]=old[p].replace('"WRITE": "description offset data max_end"','"WRITE": "description offset data"').replace('case "offset", "size", "max_end":','case "offset", "size":')
p='internal/guestbridge/response.go';old[p]=old[p].replace(', "FileTooLarge"','')
p='internal/guestbridge/bridge_test.go';s=old[p];s=s[:s.index('\nfunc TestWriteMaxEndWireAndRetry')];s=s.replace('\n\tif errors.Is(e, fsservice.ErrDelegatedFileTooLarge) {\n\t\treturn Outcome{DelegatedError: e, Publication: d.publication()}, nil\n\t}','').replace(', "max_end": int64(1<<63 - 1)','').replace('\n\t\t\t\tdelete(args, "max_end")','').replace('states map[string]ClaimState','states map[string]ClaimState');s=s.replace('\n\tnext   byte\n','\n\tnext byte\n\tgapProof bool\n');old[p]=s
p='internal/guestbridge/codec.go';s=old[p].replace('"encoding/json"','"encoding/json"\n "fmt"');s+='func invalid(format string, args ...any) error {\n return fmt.Errorf("%w: %s", fsapi.Invalid, fmt.Sprintf(format, args...))\n}\n';old[p]=s
p='internal/guestbridge/endpoint.go';s=old[p];i=s.index('// Endpoint returns');s=s[:i]+'''func equalAck(a, b *Ack) bool {
 if a == nil || b == nil {return a == nil && b == nil}
 return *a == *b
}

'''+s[i:];old[p]=s
p='tools/spec/check_retained9p.py';old[p]=old[p].replace('\n        if intent["opcode"] == "WRITE":\n            keys.add("max_end")\n            integer(args["max_end"], 0, 2**63 - 1)','').replace('            intent["args"].pop("max_end", None)\n','')
p='docs/spec/Retained-9p-protocol.md';s=old[p].replace('| WRITE | description:Cap, offset:size, data:bytes(65536), max_end:size |','| WRITE | description:Cap, offset:size, data:bytes(65536) |');start=s.index('WRITE captures `max_end`');end=s.index('`Times={',start);s=s[:start]+s[end:];old[p]=s.replace('bridge ID_CONFLICT, RETIRED_ID, PROTOCOL_ERROR and delegated FileTooLarge;\nnever native errno integers.','bridge ID_CONFLICT, RETIRED_ID, PROTOCOL_ERROR; never native errno integers.')
p='internal/guestbridge/README.md';s=old[p].replace('The schema checker updates the WRITE guest golden for captured max_end, preserves the other three guest golden frames','The schema checker preserves all four guest golden frames');old[p]=s[:s.index('\nWRITE uses the shared PlanDelegatedWrite')]
p='docs/spec/retained9p-fixtures/cases.json';f=json.loads(old[p]);import sys;sys.dont_write_bytecode=True;sp=importlib.util.spec_from_file_location('checker',root/'tools/spec/check_retained9p.py');m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
for g in f['golden']:
 b=g['body']
 if b['op']=='CALL' and b['payload']['intent']['opcode']=='WRITE':
  b['payload']['intent']['args'].pop('max_end',None);b['payload']['claim']['original_hash']=hashlib.sha256(m.canonical(b['payload']['intent'])).hexdigest();g['body_hash']=hashlib.sha256(m.canonical(b)).hexdigest()
  g['frame_hex']=[x.hex() for x in m.frames(b)]
old[p]=json.dumps(f,indent=2)+'\n'
valid={};bad=[];patch=[]
for p,s in old.items():
 if p.endswith('.go'):
  result=subprocess.run(['gofmt'],input=s,text=True,capture_output=True,check=True);s=result.stdout
 digest=hashlib.sha256(s.encode()).hexdigest()
 if digest!=freeze[p]:bad.append(p);continue
 valid[p]=digest
 if s!=pathlib.Path(p).read_text():patch.extend(difflib.unified_diff(s.splitlines(True),pathlib.Path(p).read_text().splitlines(True),fromfile='a/'+p,tofile='b/'+p))
p='internal/fsservice/delegated_write_test.go';patch.extend(difflib.unified_diff([],pathlib.Path(p).read_text().splitlines(True),fromfile='/dev/null',tofile='b/'+p))
(ev/'max-end-fix.patch').write_text(''.join(patch));(ev/'max-end-reconstruction.json').write_text(json.dumps({'verified':valid,'unreconstructed':bad},indent=2));print('unreconstructed',bad)
