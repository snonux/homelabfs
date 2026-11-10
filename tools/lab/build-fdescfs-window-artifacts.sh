#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
#
# Offline, workstation-only rebuild of the f3 host fdescfs window artifacts
# (task v34, zq3 gap G12): the FreeBSD CGO controlledruntime test binary, the
# fdescfs-namespace helper and, as a toolchain cross-check, the candidate
# daemon hfs-controlled-runtime. No SSH, network, VM, mount or install.
#
# Usage: build-fdescfs-window-artifacts.sh OUT_DIR [COMMIT]
#
# OUT_DIR must not exist yet; its parent must. COMMIT defaults to the zq3 candidate source
# b8697101df601d7748bae85721e91c0e01aae474 (y64 rebuild; earlier 060dea0 for c64+d64,
# a20aca1 for z54, defd974 for f44 and e0cca0c). The source is exported with
# `git archive`, so no worktree state or VCS metadata reaches the build.
# Every artifact is built twice with separate fresh Go caches; the script
# fails unless both builds are byte-identical. The Go environment mirrors the
# candidate's daemon recipe (freebsd/daemon-build.json in the package): the
# pinned clang cross wrapper and captured f3 sysroot, GOPROXY=off,
# -mod=readonly, -trimpath, -buildvcs=false and bounded parallelism. The C
# helper uses the flags of the merged gate recipe (-std=c11 -Wall -Wextra
# -Werror -O1 -g); its DWARF records the source directory, so a byte-identical
# rebuild needs the same OUT_DIR/src path.
set -euo pipefail

readonly CANDIDATE=b8697101df601d7748bae85721e91c0e01aae474
readonly TOOLROOT=/home/paul/tmp/codex/hfs-r24-20261105-0h1d6esn
readonly CC_WRAPPER=$TOOLROOT/freebsd-cc
readonly SYSROOT=$TOOLROOT/sysroot-capture/root
readonly GO_BIN=/usr/bin/go
readonly MODCACHE=/home/paul/go/pkg/mod

usage() {
    echo "usage: $0 OUT_DIR [COMMIT]" >&2
    exit 2
}

# log_run records the exact argv (one shell-quoted line) before running it,
# so the committed record can cite the commands verbatim.
log_run() {
    printf '%q ' "$@" >>"$out/commands.log"
    printf '\n' >>"$out/commands.log"
    "$@"
}

export_source() {
    local repo
    repo=$(git -C "$(dirname -- "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)
    mkdir -m 0700 "$out/src"
    git -C "$repo" archive --format=tar "$commit" | tar -x -C "$out/src"
    git -C "$repo" rev-parse "$commit^{commit}" >"$out/source-commit"
}

# go_env prints the fixed environment for build number $1 as env(1) words.
go_env() {
    local cache=$out/cache-$1
    mkdir -m 0700 "$cache" "$cache/build" "$cache/tmp"
    printf '%s\n' HOME="$HOME" PATH=/usr/bin:/bin LC_ALL=C TZ=UTC \
        GOOS=freebsd GOARCH=amd64 CGO_ENABLED=1 CC="$CC_WRAPPER" \
        "CGO_CFLAGS=-O2 -g -ffile-prefix-map=$SYSROOT/=/" CGO_CPPFLAGS= \
        "CGO_LDFLAGS=-O2 -g" "GOFLAGS=-p=1 -mod=readonly" GOMAXPROCS=2 \
        GOGC=50 GOENV=off GOPROXY=off GOTOOLCHAIN=local GOWORK=off \
        GOMODCACHE="$MODCACHE" GOCACHE="$cache/build" \
        GOTMPDIR="$cache/tmp" TMPDIR="$cache/tmp"
}

build_once() {
    local n=$1 dest=$out/run-$1
    local -a environment
    mapfile -t environment < <(go_env "$n")
    mkdir -m 0700 "$dest"
    cd "$out/src"
    log_run env -i "${environment[@]}" "$GO_BIN" test -c -a -trimpath \
        -buildvcs=false -o "$dest/controlledruntime.test" \
        ./internal/controlledruntime
    log_run env -i "${environment[@]}" "$GO_BIN" build -a -trimpath \
        -buildvcs=false -o "$dest/hfs-controlled-runtime" \
        ./cmd/hfs-controlled-runtime
    log_run env -i PATH=/usr/bin:/bin LC_ALL=C TZ=UTC "$CC_WRAPPER" \
        -std=c11 -Wall -Wextra -Werror -O1 -g tools/lab/fdescfs-namespace.c \
        -o "$dest/fdescfs-namespace"
}

record_outputs() {
    local name
    (cd "$out" && sha256sum run-1/* run-2/*) >"$out/sha256.txt"
    for name in controlledruntime.test hfs-controlled-runtime fdescfs-namespace; do
        cmp "$out/run-1/$name" "$out/run-2/$name"
        llvm-readelf --file-header --program-headers --dynamic-table \
            "$out/run-1/$name" >"$out/readelf-$name.txt"
        stat -c '%s %n' "$out/run-1/$name" >>"$out/sizes.txt"
    done
    sha256sum "$CC_WRAPPER" "$GO_BIN" /usr/bin/clang /usr/bin/ld.lld \
        "$TOOLROOT/sysroot-capture/sysroot.tar.gz" >"$out/toolchain.sha256"
    "$GO_BIN" version >"$out/go-version"
}

main() {
    [[ $# -ge 1 && $# -le 2 ]] || usage
    out=$1
    commit=${2:-$CANDIDATE}
    [[ $out == /* && ! -e $out ]] || { echo "OUT_DIR must be absolute and new" >&2; exit 2; }
    mkdir -m 0700 "$out"  # the parent must already exist
    : >"$out/commands.log"
    export_source
    build_once 1
    build_once 2
    record_outputs
    cat "$out/sha256.txt"
}

main "$@"
