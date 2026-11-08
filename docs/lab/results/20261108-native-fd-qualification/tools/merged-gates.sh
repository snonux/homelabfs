#!/usr/bin/env bash
# Merged gate runner for s24/w24 resume on 2026-11-08: one step at a time,
# bounded Go parallelism, every step's exit/duration recorded in steps.tsv.
set -uo pipefail
R=$1; cd /home/paul/git/homelabfs
export GOFLAGS=-p=1 GOMAXPROCS=2 GOGC=50
CC=/home/paul/tmp/codex/hfs-r24-20261105-0h1d6esn/freebsd-cc
O=$R/out; : > $R/steps.tsv
step() { local name=$1; shift; local t0=$(date +%s.%N)
  ( "$@" ) >$O/$name.stdout 2>$O/$name.stderr; local rc=$?
  printf '%s\t%s\t%.1f\n' "$name" "$rc" "$(echo "$(date +%s.%N) - $t0" | bc)" >> $R/steps.tsv; }
git rev-parse HEAD > $R/head; git status --porcelain > $R/status
step gofmt   bash -c 'out=$(gofmt -l $(git ls-files "*.go")); echo "$out"; test -z "$out"'
step build   go build ./...
step vet     go vet ./...
step vet-freebsd-cgo0 env GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go vet ./...
step vet-freebsd-cgo1 env GOOS=freebsd GOARCH=amd64 CGO_ENABLED=1 CC=$CC go vet ./...
step lint    golangci-lint run ./...
step errcheck errcheck ./...
step mage-build mage build
step x-diag-child $CC -std=c11 -Wall -Wextra -Werror -O1 -g internal/controlledruntime/testdata/diagnostic_child.c -o $O/diagnostic-child
step x-cr-test env GOOS=freebsd GOARCH=amd64 CGO_ENABLED=1 CC=$CC go test -c -trimpath -buildvcs=false -o $O/controlledruntime-freebsd.test ./internal/controlledruntime
step x-cr-cmd env GOOS=freebsd GOARCH=amd64 CGO_ENABLED=1 CC=$CC go build -trimpath -buildvcs=false -o $O/hfs-controlled-runtime ./cmd/hfs-controlled-runtime
step x-fdesc-helper $CC -std=c11 -Wall -Wextra -Werror -O1 -g tools/lab/fdescfs-namespace.c -o $O/fdescfs-namespace
git status --porcelain > $R/status-after
sha256sum $O/diagnostic-child $O/controlledruntime-freebsd.test $O/hfs-controlled-runtime $O/fdescfs-namespace > $R/artifacts.sha256 2>/dev/null
