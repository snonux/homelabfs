#!/usr/bin/env bash
# Build the source-matching zq3 G3 initialisation helper: the actual
# cmd/homelabfs (node-init, volume-init, inspect) from the xz3 candidate
# source commit, for FreeBSD/amd64, twice with independent build caches, and
# require byte-identical results. Workstation only: no SSH, host, VM or
# install. Module downloads are disabled (GOPROXY=off); a missing module fails.
#
# The helper is CGO-disabled on purpose: node-init/volume-init/inspect are pure
# Go file operations. It is never used as a daemon (mount/serve) and is not the
# quarantined CGO-disabled hfs-controlled-runtime.
set -euo pipefail

readonly SOURCE_COMMIT=e0cca0c874f0027f050974ab5a54095d4af1c416

usage() {
    echo "usage: $0 /absolute/new/output-directory" >&2
    exit 2
}

# build SOURCE OUTPUT CACHE: one isolated, reproducible FreeBSD build.
build() {
    local source=$1 output=$2 cache=$3
    (cd -- "$source" && env GOENV=off GOTOOLCHAIN=local GOPROXY=off GOFLAGS='-p=1 -mod=readonly' \
        GOMAXPROCS=2 GOGC=50 GOCACHE="$cache" GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 \
        go build -a -trimpath -buildvcs=false -o "$output" ./cmd/homelabfs)
}

sha() {
    sha256sum -- "$1" | cut -d' ' -f1
}

main() {
    [[ $# -eq 1 && $1 == /* ]] || usage
    local output=$1 repo
    repo=$(git -C "$(dirname -- "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)
    mkdir -m 0700 -- "$output"
    mkdir -m 0700 -- "$output/src"
    git -C "$repo" archive --format=tar "$SOURCE_COMMIT" | tar -x -C "$output/src"
    build "$output/src" "$output/homelabfs" "$output/cache-a"
    build "$output/src" "$output/homelabfs.repro" "$output/cache-b"
    cmp -- "$output/homelabfs" "$output/homelabfs.repro"
    chmod 0500 -- "$output/homelabfs"
    printf '{"version":"homelabfs-init-helper-build/1","source":"%s","go":"%s","goos":"freebsd","goarch":"amd64","cgo_enabled":"0","go_mod_sha256":"%s","go_sum_sha256":"%s","helper_sha256":"%s","reproduced":true}\n' \
        "$SOURCE_COMMIT" "$(go version)" "$(sha "$output/src/go.mod")" "$(sha "$output/src/go.sum")" \
        "$(sha "$output/homelabfs")" >"$output/build.json"
    cat -- "$output/build.json"
}

main "$@"
