set -eu
umask 077
cd /var/tmp/hfs-uz3-native-20261103.Hh2pjG8t
test "$(sha256 -q input.tar)" = 8c3778574692f10c16a0d18e426abf15aca3559e58b9d606ec8565c58c52e853
tar -xf input.tar
cd native-bundle-v4
while read -r expected_file_hash owned_path; do test "$(sha256 -q "$owned_path")" = "$expected_file_hash"; done < SHA256SUMS
/usr/bin/time -l /usr/bin/timeout -k 5 300 /usr/bin/limits -B -v 512m -m 512m -f 128m -c 0 /bin/sh ./native-verify.sh ../output
