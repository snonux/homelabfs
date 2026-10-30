#!/usr/bin/env bash
# Explicit lifecycle and separately approved faults for the fixed f3 lab.
# Fault stops are never fencing proof and never target Rocky or the host.
set -euo pipefail
# This branch acknowledges approval already checked by the plan-bound driver.
# A flag or environment value does not authorize a future run.
if [[ ${1-} == --approved-fault ]]; then
    if [[ $# != 4 || ! $2 =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$ ||
          ! $3 =~ ^hfs-test[012]$ || ! $4 =~ ^(clean|abrupt)$ ||
          ${HFS_LAB_RUN_ID-} != "$2" ||
          ! ${HFS_LAB_PLAN_HASH-} =~ ^[0-9a-f]{64}$ ]]; then
        echo "Fault stop requires an approved run, exact target and mode." >&2
        exit 2
    fi
    # Values are restricted above. Host script takes a run launch lock before
    # checking the private enrollment; it cannot create its own authorization.
    # shellcheck disable=SC2029
    ssh -o BatchMode=yes -o StrictHostKeyChecking=yes \
        -o ConnectTimeout=10 -o ServerAliveInterval=5 \
        -o ServerAliveCountMax=2 f3 \
        "doas -n /bin/sh -s -- '$2' '$3' '$4' '$HFS_LAB_PLAN_HASH'" <<'FAULT_HOST'
set -eu
run=$1 target=$2 mode=$3 plan_hash=$4
root="/var/tmp/hfs-run-$run"
# Approved setup must create root-owned private, real ancestors and enroll the
# live launch. Never source this file, evaluate its contents, or create a lease.
for directory in / /var /var/tmp "$root"; do
    test -d "$directory"
    test ! -L "$directory"
    test "$(stat -f '%u' "$directory")" = 0
done
# /var/tmp is the root-owned system sticky temporary parent. The run root is
# private; only the approved exclusive administrator may alter it in this window.
# FreeBSD %Op preserves special mode bits; %Lp reports only the low permissions.
for directory in / /var; do
    permissions=$(stat -f '%Lp' "$directory")
    test "$((0$permissions & 0022))" -eq 0
done
test "$(stat -f '%u:%Op' /var/tmp)" = 0:41777
test "$(stat -f '%u:%Op' "$root")" = 0:40700
lock="$root/vm-launch.lock"
test -f "$lock"
test ! -L "$lock"
test "$(stat -f '%u:%Op:%l' "$lock")" = 0:100600:1
lock_identity=$(stat -f '%d:%i' "$lock")
# -n refuses a missing file; -k preserves this setup-owned lock after use.
# No run directory or lock is created by the fault path.
lockf -kn -t 0 "$lock" /bin/sh -s -- \
    "$run" "$target" "$mode" "$plan_hash" "$lock_identity" <<'LOCKED_FAULT'
set -eu
run=$1 target=$2 mode=$3 plan_hash=$4 lock_identity=$5
root="/var/tmp/hfs-run-$run"
lock="$root/vm-launch.lock"
# Recheck paths, the exact lock inode and all enrollment while the lock is held.
for directory in / /var /var/tmp "$root"; do
    test -d "$directory"
    test ! -L "$directory"
    test "$(stat -f '%u' "$directory")" = 0
done
for directory in / /var; do
    permissions=$(stat -f '%Lp' "$directory")
    test "$((0$permissions & 0022))" -eq 0
done
test "$(stat -f '%u:%Op' /var/tmp)" = 0:41777
test "$(stat -f '%u:%Op' "$root")" = 0:40700
test -f "$lock"
test ! -L "$lock"
test "$(stat -f '%u:%Op:%l' "$lock")" = 0:100600:1
test "$(stat -f '%d:%i' "$lock")" = "$lock_identity"
lease="$root/$target.vm-enrollment"
test -f "$lease"
test ! -L "$lease"
test "$(stat -f '%u:%Op:%l' "$lease")" = 0:100600:1
# Fixed seven-line data format: run, target, plan hash, UUID, config digest,
# launch PID, exact ps lstart. Additional/missing lines fail closed.
test "$(wc -l < "$lease" | tr -d ' ')" = 7
exec 3< "$lease"
IFS= read -r enrolled_run <&3
IFS= read -r enrolled_target <&3
IFS= read -r enrolled_plan <&3
IFS= read -r enrolled_uuid <&3
IFS= read -r enrolled_config <&3
IFS= read -r enrolled_pid <&3
IFS= read -r enrolled_birth <&3
exec 3<&-
test "$enrolled_run" = "$run"
test "$enrolled_target" = "$target"
test "$enrolled_plan" = "$plan_hash"
case "$enrolled_pid" in ''|*[!0-9]*) exit 2 ;; esac
test "$enrolled_pid" -gt 1
test -n "$enrolled_birth"
test -n "$enrolled_uuid"
config="/zroot/bhyve/homelabfs-lab/$target/$target.conf"
for directory in /zroot /zroot/bhyve /zroot/bhyve/homelabfs-lab \
    "/zroot/bhyve/homelabfs-lab/$target"; do
    test -d "$directory"
    test ! -L "$directory"
done
test -f "$config"
test ! -L "$config"
test "$(stat -f '%u' "$config")" = 0
test "$(sha256 -q "$config")" = "$enrolled_config"
test "$(sysrc -f "$config" -n uuid)" = "$enrolled_uuid"
test "$(sysrc -f "$config" -n memory)" = 2G
# The prepared profile excludes concurrent lifecycle administrators/autostarts.
# This run lock is not the takeover broker's durable launch inhibition guard.
current_pid=$(vm list | awk -v n="$target" '$1==n {gsub(/[()]/,"",$NF);print $NF}')
test "$current_pid" = "$enrolled_pid"
test "$(ps -p "$enrolled_pid" -o lstart=)" = "$enrolled_birth"
test "$(procstat -b "$enrolled_pid" | awk 'NR==2 {print $NF}')" = /usr/sbin/bhyve
# Journal intent before asking vm-bhyve. Ambiguous/failed actions retain it.
intent="$root/$target.vm-fault-started"
test ! -e "$intent"
(umask 077; set -C; printf '%s\n' "$plan_hash" "$mode" > "$intent")
sync
started=$(date +%s)
case "$mode" in
    clean) timeout 10 vm stop "$target" ;;
    # Abrupt synthetic power loss is distinct from approved graceful fencing.
    abrupt) timeout 10 vm poweroff -f "$target" ;;
esac
while :; do
    state=$(vm list | awk -v n="$target" '$1==n {print $NF}')
    if test "$state" = Stopped && test ! -e "/dev/vmm/$target"; then
        printf '{"run":"%s","target":"%s","mode":"%s","stopped":true,"fence_proof":false}\n' \
            "$run" "$target" "$mode"
        exit 0
    fi
    test "$(( $(date +%s) - started ))" -lt 120 || {
        echo 'Bounded guest stop failed; preserve intent and resources.' >&2
        exit 1
    }
    sleep 2
done
LOCKED_FAULT
FAULT_HOST
    exit $?
fi
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
        # Preflight may inspect Rocky while it still occupies RAM. Actual
        # guest-start admission must use available memory after its clean stop.
        memory_pagesize=$(sysctl -n hw.pagesize)
        memory_free=$(sysctl -n vm.stats.vm.v_free_count)
        memory_inactive=$(sysctl -n vm.stats.vm.v_inactive_count)
        for memory_value in "$memory_pagesize" "$memory_free" "$memory_inactive"; do
            case "$memory_value" in
                ''|*[!0-9]*) echo 'Unavailable current memory evidence.' >&2; exit 1 ;;
            esac
        done
        test "$memory_pagesize" -gt 0
        memory_available=$(( (memory_free + memory_inactive) * memory_pagesize ))
        test "$memory_available" -ge 10737418240 || {
            echo 'Insufficient current free+inactive memory for 6 GiB lab + 4 GiB host.' >&2
            exit 1
        }
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
