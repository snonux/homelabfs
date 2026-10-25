#!/usr/bin/env bash
# Prepare stopped VMs only. This is not the integration-test runner.
set -euo pipefail

if [[ ${1:-} != --prepare-stopped || $# -ne 3 ]]; then
    echo "Usage: $0 --prepare-stopped IMAGE.raw.xz ADMIN_PUBLIC_KEY" >&2
    exit 2
fi
[[ $(id -u) == 0 && $(uname -s) == FreeBSD ]] || exit 2
[[ $(hostname -s) == f3 ]] || { echo 'This plan targets f3 only.' >&2; exit 2; }

readonly image=$2
readonly public_key=$3
readonly image_sha=a8e2c0f8331be615ef008c167069076833a417e968ff6b7d8e83b4ac762e9409
readonly dataset=zroot/bhyve/homelabfs-lab
readonly datastore=hfslab
readonly switch=hfslab
readonly lab=/zroot/bhyve/homelabfs-lab
readonly cache=/var/tmp/homelabfs-vm-prep
readonly prep_id=homelabfs-freebsd-15.1-preparation-v1

[[ $(sha256 -q "$image") == "$image_sha" ]] || {
    echo 'Image checksum mismatch.' >&2; exit 1;
}
ssh-keygen -lf "$public_key" >/dev/null
if zfs list -H "$dataset" >/dev/null 2>&1 || [[ -e $lab ]]; then
    echo 'Lab already exists; refusing to overwrite or resume implicitly.' >&2
    exit 1
fi
if vm datastore list | awk '{print $1}' | grep -qx "$datastore"; then
    echo 'Datastore name already registered.' >&2; exit 1
fi
if vm switch list | awk '{print $1}' | grep -qx "$switch"; then
    echo 'Switch name already registered.' >&2; exit 1
fi
for i in 0 1 2; do
    if vm list | awk '{print $1}' | grep -qx "hfs-test$i"; then
        echo 'VM name already registered.' >&2; exit 1
    fi
    if zpool list -H "hfsdata$i" >/dev/null 2>&1; then
        echo 'Data pool name already imported.' >&2; exit 1
    fi
    [[ ! -e /dev/gpt/hfs-data$i ]] || exit 1
done
[[ $(route -n get 172.31.253.1 | awk '/mask:/ {print $2}') == 0.0.0.0 ]] || {
    echo 'Chosen lab subnet already routed; revise the plan.' >&2; exit 1;
}
[[ $(sysctl -n net.inet.ip.forwarding) == 0 ]] || {
    echo 'Host forwarding is enabled; network isolation needs review.' >&2; exit 1;
}
[[ $(zfs get -Hp -o value available zroot/bhyve) -gt 128849018880 ]] || {
    echo 'Insufficient free space for the bounded lab allocation.' >&2; exit 1;
}

mkdir -p "$cache"
# Recreate the immutable public base from verified compressed bytes.
xz -dc "$image" | dd of="$cache/base.raw.new" bs=1m conv=sparse status=none
mv "$cache/base.raw.new" "$cache/base.raw"
original_autostart=$(sysrc -n vm_list)
readonly original_autostart
rocky_config_sha=$(sha256 -q /zroot/bhyve/rocky/rocky.conf)
readonly rocky_config_sha

zfs create -o quota=120G -o org.homelabfs:purpose="$prep_id" "$dataset"
chmod 700 "$lab"
vm datastore add "$datastore" "zfs:$dataset"
# No physical uplink or NAT. Do not use vm-bhyve's per-port "private" flag:
# the three guests must be able to communicate with one another.
vm switch create -a 172.31.253.1/24 "$switch"
printf '%s\n' "$prep_id" > "$lab/PREPARATION"
printf 'image_sha256=%s\nautostart_before=%s\n' \
    "$image_sha" "$original_autostart" > "$lab/preparation-state.txt"
printf 'vm\tip\tram\tcpu\tos_disk\tdata_disk\tpool\tstatus\n' > "$lab/manifest.tsv"

root_md=''
data_md=''
root_mount=''
active_pool=''
cleanup() {
    local status=$?
    trap - EXIT
    if [[ -n $active_pool ]]; then
        if ! zpool export "$active_pool"; then
            echo "Could not export $active_pool; preserving attached disks." >&2
            exit 1
        fi
    fi
    if [[ -n $root_mount ]]; then
        if ! umount "$root_mount"; then
            echo "Could not unmount $root_mount; preserving attached disks." >&2
            exit 1
        fi
    fi
    [[ -z $data_md ]] || mdconfig -d -u "$data_md" || status=1
    [[ -z $root_md ]] || mdconfig -d -u "$root_md" || status=1
    if (( status != 0 )); then
        echo 'Preparation incomplete; owned resources retained for inspection.' >&2
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

for i in 0 1 2; do
    name="hfs-test$i"
    ip="172.31.253.$((11+i))"
    pool="hfsdata$i"
    vm create -d "$datastore" -t default -s 20G -m 2G -c 2 "$name"
    dir="$lab/$name"
    zfs set quota=40G "${dataset}/${name}"
    sysrc -f "$dir/$name.conf" loader=bhyveload guest=freebsd \
        disk0_type=nvme disk1_type=nvme disk1_name=disk1.img \
        network0_switch="$switch" graphics=no
    # Fresh VM-owned disk: replacing the empty image created by vm create.
    cp "$cache/base.raw" "$dir/disk0.img"
    truncate -s 20G "$dir/disk0.img"
    root_md=$(mdconfig -a -t vnode -f "$dir/disk0.img")
    gpart recover "$root_md"
    gpart resize -i 4 "$root_md"
    gpart modify -i 4 -l "hfs-root$i" "$root_md"
    gpart modify -i 3 -l "hfs-swap$i" "$root_md"
    gpart modify -i 2 -l "hfs-efi$i" "$root_md"
    growfs -y "/dev/${root_md}p4"
    mkdir -p "$dir/offline-root" "$dir/offline-pool"
    mount "/dev/${root_md}p4" "$dir/offline-root"
    root_mount="$dir/offline-root"

    cat > "$root_mount/etc/fstab" <<EOF
/dev/gpt/hfs-root$i / ufs rw,noatime 1 1
/dev/gpt/hfs-efi$i /boot/efi msdosfs rw 2 2
# The image's swap partition is intentionally unused in the small test profile.
EOF
    cat > "$root_mount/etc/rc.conf" <<EOF
hostname="$name.test"
ifconfig_vtnet0="inet $ip/24"
sshd_enable="YES"
zfs_enable="YES"
hfs_lab_pool_enable="YES"
kld_list="fusefs"
sendmail_enable="NONE"
dumpdev="NO"
EOF
    cat >> "$root_mount/boot/loader.conf" <<'EOF'
console="comconsole"
comconsole_speed="115200"
vfs.zfs.arc.max="268435456"
EOF
    cat > "$root_mount/etc/hosts" <<'EOF'
127.0.0.1 localhost
::1 localhost
172.31.253.1 f3-lab
172.31.253.11 hfs-test0.test hfs-test0
172.31.253.12 hfs-test1.test hfs-test1
172.31.253.13 hfs-test2.test hfs-test2
EOF
    # No default route, DNS, LAN attachment, or production credentials.
    : > "$root_mount/etc/resolv.conf"
    install -d -m 700 "$root_mount/root/.ssh"
    install -m 600 "$public_key" "$root_mount/root/.ssh/authorized_keys"
    # Put policy first so earlier defaults cannot override it.
    cat > "$root_mount/etc/ssh/sshd_config.hfslab" <<'EOF'
PermitRootLogin prohibit-password
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
UseDNS no
EOF
    cat "$root_mount/etc/ssh/sshd_config" >> "$root_mount/etc/ssh/sshd_config.hfslab"
    mv "$root_mount/etc/ssh/sshd_config.hfslab" "$root_mount/etc/ssh/sshd_config"
    ssh-keygen -A -f "$root_mount"
    ssh-keygen -lf "$root_mount/etc/ssh/ssh_host_ed25519_key.pub" \
        >> "$lab/host-key-fingerprints.txt"
    awk -v names="$ip,$name,$name.test" '{print names, $1, $2}' \
        "$root_mount/etc/ssh/ssh_host_ed25519_key.pub" >> "$lab/known_hosts"
    uuidgen > "$root_mount/etc/hostid"

    truncate -s 16G "$dir/disk1.img"
    data_md=$(mdconfig -a -t vnode -f "$dir/disk1.img")
    gpart create -s gpt "$data_md"
    gpart add -a 1m -t freebsd-zfs -l "hfs-data$i" "$data_md"
    zpool create -o cachefile=none -O compression=lz4 -O atime=off \
        -O mountpoint=/var/db/homelabfs -R "$dir/offline-pool" \
        "$pool" "/dev/gpt/hfs-data$i"
    active_pool="$pool"
    zfs create "$pool/data"
    zfs create "$pool/journal"
    zfs create "$pool/control"
    zpool export "$pool"
    active_pool=''
    mdconfig -d -u "$data_md"
    data_md=''

    # Import an already formatted test pool; never create/force a pool at boot.
    cat > "$root_mount/etc/rc.d/hfs_lab_pool" <<EOF
#!/bin/sh
# PROVIDE: hfs_lab_pool
# REQUIRE: zfs
# BEFORE: LOGIN
. /etc/rc.subr
name=hfs_lab_pool
rcvar=hfs_lab_pool_enable
start_cmd=hfs_lab_pool_start
stop_cmd=":"
hfs_lab_pool_start() {
    if ! /sbin/zpool list -H "$pool" >/dev/null 2>&1; then
        /sbin/zpool import -d /dev/gpt "$pool" || return 1
    fi
    /sbin/zpool set cachefile=/boot/zfs/zpool.cache "$pool"
}
load_rc_config "\$name"
run_rc_command "\$1"
EOF
    chmod 755 "$root_mount/etc/rc.d/hfs_lab_pool"
    printf '%s\n' "$prep_id" > "$root_mount/etc/homelabfs-lab-preparation"
    sync
    umount "$root_mount"
    root_mount=''
    mdconfig -d -u "$root_md"
    root_md=''
    rmdir "$dir/offline-root" "$dir/offline-pool/var/db/homelabfs" \
        "$dir/offline-pool/var/db" "$dir/offline-pool/var" "$dir/offline-pool"
    chmod 600 "$dir/disk0.img" "$dir/disk1.img"
    printf '%s\t%s\t2GiB\t2\t20GiB\t16GiB\t%s\tprepared-never-booted\n' \
        "$name" "$ip" "$pool" >> "$lab/manifest.tsv"
    echo "Prepared $name; left stopped."
done

[[ $(sysrc -n vm_list) == "$original_autostart" ]]
[[ $(sha256 -q /zroot/bhyve/rocky/rocky.conf) == "$rocky_config_sha" ]]
printf 'complete=yes\n' >> "$lab/preparation-state.txt"
vm list
vm switch info "$switch"
echo "No VM was started. Manifest: $lab/manifest.tsv"
