#!/usr/bin/env bash
set -euo pipefail

# Exactly one compiler/test at a time. All outputs go to a new owned directory.
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
output=${1:?usage: verify.sh NEW_OUTPUT_DIRECTORY}
mkdir -m 700 -- "$output"
output=$(cd -- "$output" && pwd)
exec > >(tee "$output/verify.log") 2>&1
set -x
uname -a
"${CC:-cc}" --version
case $(uname -s) in
    FreeBSD) crypto=(-lmd) ;;
    Linux) crypto=(-lcrypto) ;;
    *) exit 1 ;;
esac
flags=(-std=c11
       -Wall -Wextra -Werror -O1 -g)
if [[ $(uname -s) == Linux ]]; then
    flags+=(-D_DEFAULT_SOURCE -D_POSIX_C_SOURCE=200809L)
fi
if [[ ${SANITIZE:-yes} == yes ]]; then
    flags+=("-fsanitize=address,undefined")
fi
if [[ -f "$source_dir/tests/fixtures.h" ]]; then
    cp -- "$source_dir/tests/fixtures.h" "$output/fixtures.h"
else
    python3 "$source_dir/tests/fixtures.py" "$output/fixtures.h"
fi
for name in hfs9p hfs9p_json hfs9p_ledger hfs9p_supervisor; do
    "${CC:-cc}" "${flags[@]}" -c "$source_dir/src/$name.c" \
        -o "$output/$name.o"
done
ar rcs "$output/libhfs9p.a" "$output/hfs9p.o" "$output/hfs9p_json.o" \
    "$output/hfs9p_ledger.o" "$output/hfs9p_supervisor.o"
"${CC:-cc}" "${flags[@]}" "$source_dir/tests/production_api_test.c" \
    "$output/libhfs9p.a" -pthread "${crypto[@]}" -o "$output/production_api_test"
nm "$output/production_api_test" | rg ' T hfs9p_controller_claim$'
"$output/production_api_test"
"${CC:-cc}" "${flags[@]}" -I"$output" \
    "$source_dir/tests/backend_test.c" "$source_dir/src/hfs9p_json.c" \
    "$source_dir/src/hfs9p_ledger.c" -pthread "${crypto[@]}" \
    -o "$output/backend_test"
"$output/backend_test" "$output/backend-ledger"
"${CC:-cc}" "${flags[@]}" "$source_dir/tests/ledger_test.c" \
    "$source_dir/src/hfs9p_ledger.c" -Wl,--wrap=fsync "${crypto[@]}" \
    -o "$output/ledger_test"
"$output/ledger_test" "$output/ledger"
"${CC:-cc}" "${flags[@]}" "$source_dir/tests/supervisor_test.c" \
    "$output/hfs9p.o" "$output/hfs9p_json.o" "$output/hfs9p_ledger.o" \
    -pthread -Wl,--wrap=fsync "${crypto[@]}" -o "$output/supervisor_test"
"$output/supervisor_test"
set +x
printf '%s\n' 'PASS: owned component verification; no FUSE/mount/runtime proof'
