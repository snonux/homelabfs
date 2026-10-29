#!/usr/bin/env bash
# Workstation entrypoint. Planning/dry-run never contact the lab.
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
exec python3 "$repo/tools/lab/native-single-node.py" "$@"
