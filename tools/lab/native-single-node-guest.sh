#!/bin/sh
# Fixed FreeBSD operations, called only through the approved outer lab runner.
set -eu
PATH=/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/bin:/usr/local/sbin
LC_ALL=C
export PATH LC_ALL
ulimit -f 2048
action=$1 run=$2 index=$3 plan=$4 daemon_hash=$5 helper_hash=$6
case "$run" in ''|*[!a-zA-Z0-9_-]*) exit 2 ;; esac
case "$index" in 0|1|2) ;; *) exit 2 ;; esac
test "${#plan}" -eq 64
case "$plan$daemon_hash$helper_hash" in *[!0-9a-f]*) exit 2 ;; esac
test "$(id -u)" = 0
test "$(hostname -s)" = "hfs-test$index"
work="/var/tmp/hfs-run-$run"
pool="hfsdata$index"
for directory in / /var /var/tmp; do
    test -d "$directory" && test ! -L "$directory"
    test "$(stat -f '%u' "$directory")" = 0
done
test "$(stat -f '%Lp' /var/tmp)" = 1777
for directory in / /var; do
    permissions=$(stat -f '%Lp' "$directory")
    test "$((0$permissions & 0022))" -eq 0
done

_unmounted() {
    mounts=$(mount -p) || return 1
    printf '%s\n' "$mounts" | awk -v p="$work/m" '$2==p {found=1} END {exit found+0}'
}
_idle() {
    _unmounted
    processes=0
    pgrep -f "$work/bin/(homelabfs|hfs-behavior)" >/dev/null || processes=$?
    test "$processes" -eq 1
}
_parents() {
    test "$(zpool status -x "$pool")" = "pool '$pool' is healthy"
    for kind in data journal control; do
        parent="$pool/$kind"
        test "$(zfs get -H -o value type "$parent")" = filesystem
        test "$(zfs get -H -o value mounted "$parent")" = yes
        test "$(zfs get -H -o value readonly "$parent")" = off
        test "$(zfs get -H -o value mountpoint "$parent")" = "/var/db/homelabfs/$kind"
        test "$(zfs get -Hp -o value available "$parent")" -ge 1073741824
    done
}
_owned() {
    test -d "$work" && test ! -L "$work"
    test "$(stat -f '%u:%Lp' "$work")" = 0:711
    test -f "$work/plan" && test ! -L "$work/plan"
    test "$(stat -f '%u:%Lp:%l' "$work/plan")" = 0:600:1
    test "$(cat "$work/plan")" = "$plan"
}
_datasets() {
    _owned
    test -f "$work/datasets" && test ! -L "$work/datasets"
    test "$(stat -f '%u:%Lp:%l' "$work/datasets")" = 0:600:1
    test "$(wc -l < "$work/datasets" | tr -d ' ')" = 3
    while read -r kind guid parent_guid; do
        case "$kind" in data) leaf=d ;; journal) leaf=j ;; control) leaf=c ;; *) exit 2 ;; esac
        parent="$pool/$kind"
        dataset="$parent/hfs-run-$run"
        test "$(zfs get -H -o value guid "$parent")" = "$parent_guid"
        test "$(zfs get -H -o value guid "$dataset")" = "$guid"
        test "$(zfs get -H -o value org.homelabfs:plan "$dataset")" = "$plan"
        test "$(zfs get -H -o source org.homelabfs:plan "$dataset")" = local
        test "$(zfs get -H -o value mountpoint "$dataset")" = "$work/$leaf"
        test "$(zfs get -H -o value mounted "$dataset")" = yes
        test "$(zfs get -H -o value readonly "$dataset")" = off
        test "$(zfs get -Hp -o value quota "$dataset")" = 134217728
        test "$(stat -f '%u:%Lp' "$work/$leaf")" = 0:700
        test ! -L "$work/$leaf"
        actual=$(df -T "$work/$leaf" | awk 'NR==2 {print $1, $2}')
        test "$actual" = "$dataset zfs"
    done < "$work/datasets"
    test "$(sha256 -q "$work/bin/homelabfs")" = "$daemon_hash"
    test "$(sha256 -q "$work/bin/hfs-behavior")" = "$helper_hash"
}

case "$action" in
    preflight)
        test ! -e "$work" && test ! -L "$work"
        case "$(freebsd-version -k)" in 15.1-RELEASE*) ;; *) exit 2 ;; esac
        case "$(freebsd-version -u)" in 15.1-RELEASE*) ;; *) exit 2 ;; esac
        test -x /usr/local/bin/python3.12
        kldstat -q -m fusefs
        _parents
        for kind in data journal control; do
            if zfs list -H "$pool/$kind/hfs-run-$run" >/dev/null 2>&1; then
                echo 'Existing child dataset; fresh run ID required.' >&2
                exit 1
            fi
        done
        uname -a
        zpool status "$pool"
        ;;
    reserve)
        test ! -e "$work" && test ! -L "$work"
        umask 077
        mkdir -m 711 "$work"
        (set -C; printf '%s\n' "$plan" > "$work/plan")
        mkdir -m 711 "$work/bin"
        mkdir -m 700 "$work/evidence" "$work/m"
        ;;
    setup)
        _owned
        _parents
        _idle
        test "$(sha256 -q "$work/bin/homelabfs")" = "$daemon_hash"
        test "$(sha256 -q "$work/bin/hfs-behavior")" = "$helper_hash"
        chmod 555 "$work/bin/homelabfs" "$work/bin/hfs-behavior"
        # Never destroy or overlay anything, including on failure.
        umask 077
        (set -C; : > "$work/datasets")
        for kind in data journal control; do
            case "$kind" in data) leaf=d ;; journal) leaf=j ;; control) leaf=c ;; esac
            parent="$pool/$kind"
            dataset="$parent/hfs-run-$run"
            test ! -e "$work/$leaf" && test ! -L "$work/$leaf"
            if zfs list -H "$dataset" >/dev/null 2>&1; then exit 1; fi
            parent_guid=$(zfs get -H -o value guid "$parent")
            zfs create -o "mountpoint=$work/$leaf" -o quota=128M \
                -o compression=off -o devices=off -o exec=off -o setuid=off \
                -o "org.homelabfs:plan=$plan" "$dataset"
            guid=$(zfs get -H -o value guid "$dataset")
            printf '%s %s %s\n' "$kind" "$guid" "$parent_guid" >> "$work/datasets"
            chmod 700 "$work/$leaf"
        done
        _datasets
        cp "$work/datasets" "$work/evidence/datasets.txt"
        {
            uname -a
            freebsd-version -ku
            zpool status "$pool"
            zfs list -p -r "$pool"
            kldstat
            sysctl vfs.zfs.arc.max
            mount -p
            "$work/bin/homelabfs" -version
            "$work/bin/hfs-behavior" -version
        } > "$work/evidence/environment.txt"
        ;;
    run)
        role=$7 profile=$8 initialize=$9
        case "$role" in root|unprivileged) ;; *) exit 2 ;; esac
        case "$profile" in hfs-direct|hfs-buffered) ;; *) exit 2 ;; esac
        case "$initialize" in yes|no) ;; *) exit 2 ;; esac
        _datasets
        _idle
        status=0
        # Exact owned child; TERM once on deadline, then preserve a live child.
        # No KILL, force/lazy unmount, FUSE abort or backing deletion.
        /usr/local/bin/python3.12 - "$work" "$role" "$profile" \
            "$initialize" "$helper_hash" <<'SUPERVISOR' || status=$?
import json
from pathlib import Path
import resource
import signal
import subprocess
import sys
import time

work, role, profile, initialize, digest = sys.argv[1:]
prefix = Path(work) / "evidence" / (role + "-" + profile)
args = [work + "/bin/homelabfs", "behavior", "--disposable-validation",
        "--data-root", work + "/d", "--journal-root", work + "/j",
        "--control-root", work + "/c", "--mountpoint", work + "/m",
        "--behavior-binary", work + "/bin/hfs-behavior",
        "--behavior-sha256", digest, "--role", role, "--profile", profile,
        "--json", str(prefix) + ".json", "--junit", str(prefix) + ".xml"]
if initialize == "yes":
    args.append("--initialize")

def limits():
    resource.setrlimit(resource.RLIMIT_FSIZE, (262144, 262144))
    resource.setrlimit(resource.RLIMIT_NOFILE, (512, 512))

started = time.time()
with open(str(prefix) + ".stdout", "xb") as stdout, \
        open(str(prefix) + ".stderr", "xb") as stderr:
    child = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=stdout,
                             stderr=stderr, start_new_session=True,
                             preexec_fn=limits,
                             env={"PATH": "/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/bin", "GOMAXPROCS": "2"})
    with open(str(prefix) + ".process.json", "x") as record:
        json.dump({"pid": child.pid, "started": started, "args": args}, record)
    timed_out = False
    try:
        code = child.wait(timeout=95)
    except subprocess.TimeoutExpired:
        timed_out = True
        child.send_signal(signal.SIGTERM)
        try:
            code = child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            code = None
    with open(str(prefix) + ".exit.json", "x") as record:
        json.dump({"pid": child.pid, "exit_code": code, "timed_out": timed_out,
                   "seconds": time.time() - started,
                   "preserved_live_process": code is None}, record)
    if timed_out or code != 0:
        print("Failed fixture; preserve process/mount/datasets", file=sys.stderr)
        sys.exit(1 if code is None else (30 if timed_out else 10))
SUPERVISOR
        _idle
        _datasets
        for pair in data:d journal:j control:c; do
            kind=${pair%:*} leaf=${pair#*:}
            marker="$work/$leaf/fixture.json"
            test -f "$marker" && test ! -L "$marker"
            test "$(stat -f '%u:%Lp:%l' "$marker")" = 0:600:1
            cp "$marker" "$work/evidence/$role-$profile.marker-$kind.json"
        done
        mount -p > "$work/evidence/$role-$profile.mounts.txt"
        exit "$status"
        ;;
    collect)
        _owned
        # Evidence only; never traverse FUSE or a backing tree.
        tar -cf - -C "$work" evidence
        ;;
    cleanup)
        if test ! -e "$work" && test ! -L "$work"; then
            for kind in data journal control; do
                if zfs list -H "$pool/$kind/hfs-run-$run" >/dev/null 2>&1; then
                    echo 'Orphan dataset without run enrollment; inspect.' >&2
                    exit 1
                fi
            done
            exit 0
        fi
        _owned
        _idle
        _datasets
        # Even successful datasets remain for evidence/replay inspection. Cleanup
        # proves quiescence; removal requires a separate approved retention action.
        mount -p
        zfs list -p -r "$pool"
        ;;
    *) exit 2 ;;
esac
