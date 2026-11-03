set -eu
umask 077
cd /var/tmp/hfs-uz3-native-20261103.0wRtq2r6
test "$(sha256 -q input.tar)" = 49fa395e13d9231c580ad0bb97799224a0385ae4e75a6d851cc3060c41f25e98
tar -xf input.tar
cd native-bundle-v3
while read -r expected_file_hash owned_path; do test "$(sha256 -q "$owned_path")" = "$expected_file_hash"; done < SHA256SUMS
/usr/bin/time -l /usr/bin/timeout -k 5 300 /usr/bin/limits -B -v 512m -m 512m -f 128m -c 0 /bin/sh ./native-verify.sh ../output
