from pathlib import Path
import subprocess,json,hashlib
r=Path(__file__).parent; root=Path('/home/paul/git/homelabfs'); rows=[]
items=['src/hfs9p.c', 'src/hfs9p_json.c', 'src/hfs9p_ledger.c', 'src/hfs9p_supervisor.c', 'tests/production_api_test.c', 'tests/backend_test.c', 'tests/supervisor_test.c']
for item in items:
 source=root/'tools/integration/lib9p'/item; target=r/'freebsd-c-objects'/(item.replace('/','-')+'.o'); argv=['zig','cc','-target','x86_64-freebsd','-std=c11','-Wall','-Wextra','-Werror','-O1','-UNDEBUG','-I/tmp/hfs-q04-headers','-I'+str(r/'c-suite'),'-c',str(source),'-o',str(target)]; subprocess.run(argv,check=True); rows.append({'source':str(source.relative_to(root)),'source_sha256':hashlib.sha256(source.read_bytes()).hexdigest(),'argv':argv,'object_sha256':hashlib.sha256(target.read_bytes()).hexdigest()})
(r/'freebsd-c-objects.json').write_text(json.dumps({'scope':'Actualfrozen7TUscompile-only withassertionsenabled; noFreeBSDnativelink/execution','header_sha256':hashlib.sha256(Path('/tmp/hfs-q04-headers/sha256.h').read_bytes()).hexdigest(),'rows':rows},indent=2)+'\n')
