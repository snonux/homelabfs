set -eu
umask 077
cd /var/tmp/hfs-uz3-native-20261103.qSXYFku5
test "$(sha256 -q input.tar)" = 45bb90ef74a7187e4d840f3418c1e52637c89ee8c4e6f3c1b99f7ce2440bdba9
tar -xf input.tar
cd native-bundle-v6
while read -r expected_file_hash owned_path; do test "$(sha256 -q "$owned_path")" = "$expected_file_hash"; done < SHA256SUMS
/usr/bin/time -l /usr/bin/timeout -k 5 300 /usr/bin/limits -B -v 512m -m 512m -f 128m -c 0 /bin/sh ./native-verify.sh ../output
