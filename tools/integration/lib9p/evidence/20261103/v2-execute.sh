set -eu
umask 077
cd /var/tmp/hfs-uz3-native-20261103.DWn5BV8E
test "$(sha256 -q input.tar)" = b1c3100217f3881b658c6ee934a219594f5a745e1600e105c4f0fddc2b588368
tar -xf input.tar
cd native-bundle-v2
while read -r expected_file_hash owned_path; do test "$(sha256 -q "$owned_path")" = "$expected_file_hash"; done < SHA256SUMS
/usr/bin/time -l /usr/bin/timeout -k 5 300 /usr/bin/limits -B -v 512m -m 512m -f 128m -c 0 /bin/sh ./native-verify.sh ../output
