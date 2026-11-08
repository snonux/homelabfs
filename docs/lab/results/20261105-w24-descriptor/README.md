# W24 descriptor admission: preparation and unsupported-profile evidence

W24 remains **started**. This records reviewed code preparation, offline native
compilation and actual FreeBSD refusal on an unsupported host. Positive native
FD execution, SYSTEM setup, normal restoration and full runtime READY remain
unvalidated. The exact critical setup plan is not prepared or approved.

The CGO-enabled `TestDescriptorExecNativeUnsupported` passed on f3 through the
root-owned bounded native guard: no exporter child/pin, issuance mutation,
mount-device open or observer staging occurred. Its test stdout reports PASS
without a skip; the outer `checks/native-unsupported-v1` receipt has exit0,
no guard error and no source changes. The native child guard recorded
0.315 seconds and 93,323,264 bytes peak RSS. This negative test is distinct from
positive descriptor-execution qualification.

Fresh read-only facts after f3's independently observed reboot identify kernel
`15.1-RELEASE-p2`, source `aadd58dddcbc`, boot timeval
`1793876105.108150` (2026-11-05 10:55:05 UTC). `/dev/fd` remained devfs and the
inspector returned `supported:false`, `publication:null`; fdescfs was absent
from the module registry. Earlier pre-reboot facts are historical. These
observations do not authorize a module load or mount and are not future plan
admission. The actual module text is preserved in the
[parser fixture](../../../../tools/lab/testdata/fdescfs-kldstat-f3-20261105.txt).

All three offline FreeBSD builds passed with immutable source bindings: the C
namespace helper (`-std=c11 -Wall -Wextra -Werror -O1 -g`, link map), CGO1 Go
test binary and CGO1 runtime command (`trimpath`, `buildvcs=false`). Artifact
hashes and captured ELF loader/library closure are in [artifacts.json](artifacts.json).
Go/C source hashes still match the compile bindings after the Python-only
parser fix. Native binaries were not executed by the cross-build stages.

The ELF inventory child completed successfully. Its **guard did not pass**:
root appended native-coordinator inputs to `source-roots.json` during that
inventory stage. The drift is retained separately from successful ELF reads;
it is not reported as immutable-source verification.

R1 found that the controller rejected authentic `kldstat -v` pathname suffixes.
The fix preserves loaded-file path identity and validates the complete block
format. The final focused batch passed 14 tests, including the captured 24-file
inventory and 11 malformed-input subcases, with no guard error/source change.
R2 confirmed that finding resolved with no new blockers. Review clears the
preparation code, not a SYSTEM execution window.

Compact receipts, exact retained source locations and evidence boundaries are
in [summary.json](summary.json). Full failed/successful resources remain under
the recorded task root and `/var/tmp/hfs-w24-jlmt-r1`; no large logs are copied
here. No module load, mount, VM lifecycle or production change occurred in
these W24 checks.

## Continued 2026-11-08

Positive FD-exec qualification, negative controls before and after setup, and
normal restoration ran in disposable guest hfs-test0. The f3 host window was not
executed. See [20261108-native-fd-qualification](../20261108-native-fd-qualification/).
