#!/usr/bin/env bash
set -euo pipefail

# Additive proof for the real command, only on an authorized disposable root CI
# matrix member. The test owns normal lifecycle; failed resources stay intact.
if (( $# != 1 )); then
    echo "usage: $0 EXISTING_ROOT_MATRIX_EVIDENCE_DIRECTORY" >&2
    exit 2
fi
if (( EUID == 0 )); then
    echo "invoke as the runner; only the explicit test is elevated" >&2
    exit 2
fi
reports=$(realpath -- "$1")
test -d "$reports"
test ! -e "$reports/cli-test-events.jsonl"
go_binary=$(command -v go)
module_cache=$(go env GOMODCACHE)
test -d "$module_cache"
fixture_root=$(mktemp -d /tmp/hfs-behavior-cli-ci.XXXXXXXX)
chmod 0711 "$fixture_root"
mkdir -m 0711 "$fixture_root/fixtures"

_collect() {
    local status=$?
    trap - EXIT
    # Inspect mount metadata only, never traverse a potentially failed mount.
    cat /proc/self/mountinfo > "$reports/cli-mountinfo-after.txt" || status=1
    if awk -v root="$fixture_root/" \
        'index($5, root) == 1 { found=1 } END { exit !found }' \
        "$reports/cli-mountinfo-after.txt"; then
        echo "command-owned mount remains; preserve for inspection" >&2
        status=1
    fi
    printf 'exit_status=%s\nfixture_root=%s\n' "$status" "$fixture_root" \
        > "$reports/cli-wrapper-result.txt" || status=1
    exit "$status"
}
trap _collect EXIT

{
    printf 'fixture_root=%s\nmodule_cache=%s\n' "$fixture_root" "$module_cache"
    "$go_binary" version
    sudo -n -- id
} > "$reports/cli-environment.txt" 2>&1

# PATH and the runner's module cache are explicit across sudo. Use a new,
# root-owned build cache and traversable run parent for numeric UID helpers.
# pipefail preserves any go/sudo/tee failure before validating exact test events.
sudo -n -- env "PATH=$PATH" "GOMODCACHE=$module_cache" \
    "GOCACHE=$fixture_root/go-cache" "TMPDIR=$fixture_root/fixtures" \
    GOMAXPROCS=2 CGO_ENABLED=0 HOMELABFS_BEHAVIOR_CLI_INTEGRATION=1 \
    "$go_binary" test -json ./internal/daemoncli \
    -run '^TestLinuxBehaviorCLIIntegration$' -v -count=1 -timeout=8m \
    | tee "$reports/cli-test-events.jsonl"
python3 tools/ci/validate-linux-behavior-cli.py \
    "$reports/cli-test-events.jsonl"
