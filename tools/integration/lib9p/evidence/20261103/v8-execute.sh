set -eu
umask 077
cd /var/tmp/hfs-uz3-native-20261103.6LXlcvQC
test "$(sha256 -q input.tar)" = 9af3d2ad7a027830397fd349ca1738782a7bb8319677844f077ad51fc90ba05b
tar -xf input.tar
cd native-bundle-v8
while read -r expected_file_hash owned_path; do test "$(sha256 -q "$owned_path")" = "$expected_file_hash"; done < SHA256SUMS
/usr/bin/time -l /usr/bin/timeout -k 5 300 /usr/bin/limits -B -v 512m -m 512m -f 128m -c 0 /bin/sh ./native-verify.sh ../output
