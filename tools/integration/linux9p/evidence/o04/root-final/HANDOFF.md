# o04 frozen implementation handoff

Base e0d329e, worktree /home/paul/tmp/codex/hfs-o04-20261103-oct9t3af/worktree.
Root owns ALL git/task mutations and fresh R1 (at most two rounds, second only blockers). No nested agents. No active process/heavy slot held. All changes belong to o04 owned linux9p/lib9p areas; initial worktree was clean, no unrelated changes touched.

Final maintained 28 Linux inputs: source-freeze2.json; reconstructed source/;
current source.json/export hashes and objects2/receipt.json match exactly. No
source edits after objects2 compilation. Historical final-source3/final-objects4
and x04 raw references remained immutable. No full Linux copy/Kbuild.

Production behavior:
- Explicit exact 9P2000.L.hfs1.2 and hfs1.3 version/frame/body negotiation, fixed tuples4095/8191, no legacy fallback/closed tuple expansion. Backend pins/restores0..3, exact op unions, ALLOCATE ordinary, GAP/CLEANUP reserved; full verification bytes retained.
- Guest retains complete original claim/hash/context/full creds before ordinary ALLOCATE; exact ALLOCATED permits CALL. Pending issuance gates ALL later ordinary/control claim numbers and typed earmarks. Actual uncertain allocation retains original command/cell/donor/slot; same-frame retry reconciles it. Concurrent allocation attempts serialize without holding session lock over RPC.
- Typed dirty earmarks own no-OriginalHash numbers, never ordinary Allocate; direct ordinary DRAIN_WRITE refuses before numbering. Typed pending clears only after actual lease decoder checks/copies protected exact reservation or exact existing CANCELLED-ticket closure. Preowned drain and unnumbered reserved controls remain available.
- Selected successor HELLO/CALL/QUERY lane selection matches actual host/backend classes. Legacy branches preserved; root recorded their preexisting lane mismatch separately as z04, not C2 regression.
- Full terminal QUERY/GAP envelopes retained in request. A refused result revalidates its immutable original envelope without another Allocate/CALL/QUERY or new effects. Existing native_cell field is translated guest cell, explicitly NOT actual native endpoint cell. RETIRE callback receives complete borrowed envelope; original full credentials and contiguous same-issuer prefix are sent.
- GAP ack-loss retry is exact original Call in a NEW reserved control cell. Old unknown candidate and blocked state stay owned for C9. No same-frame GAP or automatic resume claim.
- lib9p preserves signed native receipt bytes and native binding. y04 must provide separately negotiated authenticated native-to-guest mapping before missing C6 provider can verify source/event/floor authority. No re-sign/downgrade, new verified flag or permissive production provider.

Verification (all bounded local component checks, no infrastructure):
- combined8/receipt.json and host.log: strict C + ASan/UBSan actual production guest hfs_call/allocator/control/framing + actual lib9p + REAL backend ledger -> real host Transport/Service/provider/journal, no fixture host Allocate call. Actual protected allocation before CALL, full expected original, lost allocation ACK identical frame/cell/donor retry, no later numbering, typed pending interleave, real ROOT, explicit missing-C6 EOPNOTSUPP twice against same retained full-envelope pointer+SHA with unchanged effects/numbers, GAP ack loss/full-envelope retention.
- Same combined fixture executes exact production hfs_lease reservation decoder using owned literal controller GRANTED receipt: exact typed owner clear, wrong current owner preserved, 8 binding mutations and copy allocation failure retain ownership. These are literal/controller/physical primitives, NOT actual host DIRTY_RESERVE enrollment or source authority.
- objects2/receipt.json: eight actual -c TUs (hfs_protocol/session/lease/inode/file/mmap + net9p client/protocol), recorded flags and readonly pinned generated/stock headers; all source/reference hashes unchanged. No link/modpost/boot/ABI claim.
- lib3.json + lib3/verify.log: strict C/ASan/UBSan literal2/3 routing/mixed tuples, guest97->native2 receipt-cell preservation, native command-hash preservation for actual1.2 canonical vector, unchanged but unverified1.2 literal through1.3, snapshots, production absent-native/provider refusal, real fsync/rename/lock/failure ledger tests. PASS11.05s, sampled751MB.
- codec2.json: exact0..3 tuple/frame matrix and mixed0..4 negatives PASS; later protocol-header change was comment only.
- requests3.json: actual result ownership + retirement, full callback response carrier, held-owner/wrong-floor/denied/gap negatives PASS.
- transport2.json: actual loop retry/capacity/deadline/channel checks over minors0..3 PASS.
- git diff --check clean; final manifests verified against actual bytes. Scoped Go overlay only, no full suite (production Go unchanged).

Failed attempts/resources preserved, identified precisely in tools/integration/linux9p/evidence/o04/evidence.json. Failures were outdated codec expectation, primitive declarations/session/decoder/UUID printf shim, stale helper extraction boundaries and sampler cleanup race; no final failing check remains. combined5 logs component PASS but runner failed sampler race; do not count it as runner PASS. Later combined8 is complete PASS. Current aggregate allocated task+external failed resources58,167,296 bytes; short /tmp/hfo04-* roots empty. No OS tuning, user process kills, SSH/VM/mount/install/lab/prod/PV/NFS/received-sink actions.

Self-review before handoff: Did it make sense? Yes: reuse actual retained transport and native ledger rather than introducing another carrier/allocator or provider. Better approach considered: native receipt rewriting would invalidate signature/closed1.3; preserved native bytes + explicit y04/C6 refusal is the scoped solution. Fixed found issuance/lane/receipt lifetime/retry wedges before R1. No additional source-supported blocker within C2 remains. Root R1 should assess stated C2 AC only; native/provider/distributed deployment remains separate.
