#!/usr/bin/env bash
set -euo pipefail

# Use only on a disposable CI runner after authorization for this test batch.
# The fixture owns normal unmount/cleanup; this wrapper never removes resources.
if (( $# != 3 )); then
    echo "usage: $0 FIXTURE_BINARY NEW_REPORT_DIRECTORY runner|root" >&2
    exit 2
fi
binary=$(realpath -- "$1")
reports=$2
identity=$3
case "$identity" in
    runner|root) ;;
    *) echo "identity must be runner or root" >&2; exit 2 ;;
esac
if (( EUID == 0 )); then
    echo "invoke this wrapper as the runner user; it elevates only the root fixture" >&2
    exit 2
fi
test -x "$binary"
mkdir -- "$reports"
reports=$(realpath -- "$reports")
# Fixed synthetic users need to traverse the temporary ancestor to their helper.
# Backing roots created by the fixture remain private, with only the mount shared.
fixture_root=$(mktemp -d /tmp/hfs-linux-ci.XXXXXXXX)
chmod 0711 "$fixture_root"
mkdir -m 0711 "$fixture_root/fixtures"
owner="$(id -u):$(id -g)"

_collect() {
    local status=$?
    trap - EXIT
    # Root reports are mode 0600; make only these known plain files uploadable.
    if [[ "$identity" == root ]]; then
        local file
        for file in hfs-direct.json hfs-direct.xml hfs-buffered.json hfs-buffered.xml; do
            if [[ -f "$reports/$file" && ! -L "$reports/$file" ]]; then
                sudo chown -- "$owner" "$reports/$file" || status=1
            fi
        done
    fi
    # Read mount metadata only. Do not walk a potentially failed FUSE mount.
    cat /proc/self/mountinfo > "$reports/mountinfo-after.txt" || status=1
    if awk -v root="$fixture_root/" \
        'index($5, root) == 1 { found=1 } END { exit !found }' \
        "$reports/mountinfo-after.txt"; then
        echo "run-owned mount remains; preserved for inspection" >&2
        status=1
    fi
    printf 'exit_status=%s\nfixture_root=%s\n' "$status" "$fixture_root" \
        > "$reports/wrapper-result.txt" || status=1
    exit "$status"
}
trap _collect EXIT

{
    printf 'identity=%s\nfixture_root=%s\n' "$identity" "$fixture_root"
    uname -a
    go version
    sha256sum "$binary"
    dpkg-query -W fuse3
    fusermount3 --version
    if [[ "$identity" == root ]]; then
        sudo id
    else
        id
    fi
} > "$reports/environment.txt" 2>&1

command=(env GOMAXPROCS=2 HOMELABFS_FUSE_INTEGRATION=1
    "HOMELABFS_BEHAVIOR_REPORTS=$reports" "TMPDIR=$fixture_root/fixtures"
    "$binary" '-test.run=^TestLinux(PortableBehavior|BehaviorEINTRBoundary)$'
    -test.v=test2json -test.timeout=120s -test.count=1)
if [[ "$identity" == root ]]; then
    command=(sudo -- "${command[@]}")
fi
"${command[@]}" 2>&1 \
    | tee "$reports/test.log" \
    | go tool test2json -p github.com/snonux/homelabfs/internal/fuseadapter \
    | tee "$reports/test-events.jsonl"
