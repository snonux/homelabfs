#!/usr/bin/env bash
set -euo pipefail

# Workstation-only offline helper. Native FreeBSD helpers are in hfs-lab itself
# and need no Bash installation. This wrapper never runs native/SSH operations.
if (( $# < 3 )); then
  printf '%s\n' 'usage: private-etcd-plan.sh BINARY SHA256 COMMAND [FLAGS]' >&2
  exit 2
fi

binary=$1
digest=$2
command=$3
shift 3

if [[ $binary != /* || ! -f $binary || -L $binary || ! -x $binary ||
      ! $digest =~ ^[0-9a-f]{64}$ ]]; then
  printf '%s\n' 'require an absolute regular executable and exact SHA256' >&2
  exit 2
fi

case $command in
  private-etcd-plan | private-etcd-pki | private-etcd-request) ;;
  *)
    printf '%s\n' 'only offline review, test PKI and request preparation allowed' >&2
    exit 2
    ;;
esac

actual=$(sha256sum -- "$binary")
actual=${actual%% *}
if [[ $actual != "$digest" ]]; then
  printf '%s\n' 'planning binary checksum mismatch' >&2
  exit 2
fi

exec "$binary" "$command" "$@"
