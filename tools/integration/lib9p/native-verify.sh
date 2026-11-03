#!/bin/sh
set -eu

# Native FreeBSD component check. No install/mount/device lifecycle actions.
# Caller sets process/disk/time limits. Inputs include frozen fixtures.h.
source_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
output=${1:?usage: native-verify.sh NEW_OUTPUT_DIRECTORY}
mkdir -m 700 -- "$output"
output=$(CDPATH='' cd -- "$output" && pwd)
exec > "$output/native.log" 2>&1
set -x
uname -a
cc --version
set -- -std=c11 -Wall -Wextra -Werror -O1 -g
for name in hfs9p hfs9p_json hfs9p_ledger; do
    cc "$@" -c "$source_dir/src/$name.c" -o "$output/$name.o"
done
cc "$@" "$source_dir/tests/production_api_test.c" "$output/hfs9p.o" \
    "$output/hfs9p_json.o" "$output/hfs9p_ledger.o" -pthread -lmd \
    -o "$output/production_api_test"
"$output/production_api_test"
cc "$@" -I"$source_dir/tests" "$source_dir/tests/backend_test.c" \
    "$source_dir/src/hfs9p_json.c" "$source_dir/src/hfs9p_ledger.c" \
    -pthread -lmd -o "$output/backend_test"
"$output/backend_test" "$output/backend-ledger"
cc "$@" "$source_dir/tests/ledger_test.c" \
    "$source_dir/src/hfs9p_ledger.c" -Wl,--wrap=fsync -lmd \
    -o "$output/ledger_test"
"$output/ledger_test" "$output/ledger"
printf '%s\n' 'PASS: native component objects/tests; no bhyve/FUSE runtime proof'
