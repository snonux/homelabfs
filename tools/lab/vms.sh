#!/usr/bin/env bash
# Explicit lifecycle for the fixed f3 lab. Never force-kills guests.
set -euo pipefail
if [[ $# != 2 || $1 != --approved-action ]]; then
    echo "Usage: $0 --approved-action {start-lab|stop-lab|start-rocky|stop-rocky}" >&2
    exit 2
fi
case $2 in start-lab|stop-lab|start-rocky|stop-rocky) ;; *) exit 2 ;; esac
# shellcheck disable=SC2029 # action is checked against a fixed allowlist.
ssh -o BatchMode=yes f3 "doas -n /bin/sh -s -- '$2'" <<'HOST'
set -eu
action=$1
state() { vm list | awk -v n="$1" '$1==n {print $NF}'; }
stopped() { test "$(state "$1")" = Stopped; }
stop_vm() {
    stopped "$1" && return 0
    vm stop "$1"
    count=0
    until stopped "$1"; do
        count=$((count+1))
        test "$count" -le 60 || { echo "Timed out stopping $1" >&2; exit 1; }
        sleep 2
    done
}
case "$action" in
    stop-rocky) stop_vm rocky ;;
    stop-lab) for i in 0 1 2; do stop_vm "hfs-test$i"; done ;;
    start-rocky)
        for i in 0 1 2; do stopped "hfs-test$i"; done
        if stopped rocky; then vm start rocky; fi
        ;;
    start-lab)
        stopped rocky || { echo 'Stop Rocky in an approved window first.' >&2; exit 1; }
        stopped freebsd || { echo 'Other FreeBSD VM is running; reassess memory.' >&2; exit 1; }
        vm list | awk 'NR>1 && $1 !~ /^hfs-test[012]$/ && $NF!="Stopped" {exit 1}'
        # Fixed profile: 6 GiB guests plus at least 4 GiB host/overhead.
        test "$(sysctl -n hw.physmem)" -ge 10737418240
        test "$(swapinfo -k | awk 'NR>1 {used+=$3} END {print used+0}')" -eq 0
        for i in 0 1 2; do
            config="/zroot/bhyve/homelabfs-lab/hfs-test$i/hfs-test$i.conf"
            test "$(sysrc -f "$config" -n memory)" = 2G
        done
        for i in 0 1 2; do
            if stopped "hfs-test$i"; then vm start "hfs-test$i"; fi
        done
        ;;
esac
vm list
HOST
