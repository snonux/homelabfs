set -eu
umask 077
cd /var/tmp/hfs-uz3-native-20261103.FxalSo2E
test "$(sha256 -q input.tar)" = 4bbbc719a2996c9ab27df8807a413f39b5858b187b1206194b00229a8fcf7180
tar -xf input.tar
cd native-bundle-v7
while read -r expected_file_hash owned_path; do test "$(sha256 -q "$owned_path")" = "$expected_file_hash"; done < SHA256SUMS
/usr/bin/time -l /usr/bin/timeout -k 5 300 /usr/bin/limits -B -v 512m -m 512m -f 128m -c 0 /bin/sh ./native-verify.sh ../output
