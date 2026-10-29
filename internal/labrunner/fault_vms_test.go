package labrunner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMFaultWrapperRejectsUnsafeTargetsWithoutSSH(t *testing.T) {
	for _, args := range [][]string{{"--approved-fault", "run1", "rocky", "abrupt"}, {"--approved-fault", "run1", "f3", "abrupt"}, {"--approved-fault", "../escape", "hfs-test0", "clean"}, {"--approved-fault", "run1", "hfs-test0", "reset"}} {
		cmd := exec.Command("bash", append([]string{"../../tools/lab/vms.sh"}, args...)...)
		cmd.Env = []string{"PATH=/nonexistent", "HFS_LAB_RUN_ID=run1", "HFS_LAB_PLAN_HASH=" + strings.Repeat("a", 64)}
		if output, err := cmd.CombinedOutput(); err == nil || strings.Contains(string(output), "ssh") {
			t.Fatalf("unsafe args reached transport: %v %s %v", args, output, err)
		}
	}
}
func TestVMFaultWrapperPreparedScriptUsesOnlyEnrolledNativeGuest(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "prepared")
	// The stub captures the remote script. It never evaluates it or opens SSH.
	stub := "#!/bin/sh\n/usr/bin/cat > \"$CAPTURE\"\n"
	if err := os.WriteFile(filepath.Join(directory, "ssh"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../tools/lab/vms.sh", "--approved-fault", "run1", "hfs-test0", "abrupt")
	cmd.Env = []string{"PATH=" + directory + ":/usr/bin:/bin", "CAPTURE=" + capture, "HFS_LAB_RUN_ID=run1", "HFS_LAB_PLAN_HASH=" + strings.Repeat("a", 64)}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, output)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{"vm-enrollment", "enrolled_birth", "enrolled_uuid", "enrolled_config", "vm-fault-started", "timeout 10 vm stop", "timeout 10 vm poweroff -f", "fence_proof"} {
		if !strings.Contains(script, required) {
			t.Fatalf("missing %s", required)
		}
	}
	for _, forbidden := range []string{"kill -9", "vm poweroff rocky", "zfs destroy", "zdata/sink", "--force-unmount"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("unsafe prepared script %s", forbidden)
		}
	}
}

func TestVMFaultHostAdmissionPrecedesLockAndRefusesUnsafeResources(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "script")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE.args\"\n/usr/bin/cat > \"$CAPTURE\"\n"
	if err := os.WriteFile(filepath.Join(directory, "ssh"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../tools/lab/vms.sh", "--approved-fault", "run1", "hfs-test0", "clean")
	cmd.Env = []string{"PATH=" + directory + ":/usr/bin:/bin", "CAPTURE=" + capture, "HFS_LAB_RUN_ID=run1", "HFS_LAB_PLAN_HASH=" + strings.Repeat("a", 64)}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, output)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := os.ReadFile(capture + ".args")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(arguments), "lockf") {
		t.Fatal("transport acquired lock before host admission")
	}
	script := string(raw)
	index := strings.Index(script, "lockf -kn -t 0")
	if index < 0 {
		t.Fatal("missing noncreating retained lock")
	}
	prefix := script[:index]
	for _, guard := range []string{`test -d "$directory"`, `test ! -L "$directory"`, `test -f "$lock"`, `test ! -L "$lock"`, `test "$(stat -f '%u:%Lp:%l' "$lock")" = 0:600:1`, `test "$(stat -f '%u:%Lp' "$root")" = 0:700`} {
		if !strings.Contains(prefix, guard) {
			t.Fatalf("guard does not precede lock: %s", guard)
		}
	}
	if strings.Index(script, `lease="$root/$target.vm-enrollment"`) < index {
		t.Fatal("enrollment was not checked while holding lock")
	}
	// Execute only the read-only admission prefix with fake test/stat functions.
	// The captured locked script, VM commands and actual filesystem paths are
	// never evaluated. Every unsafe case must exit before the lock marker.
	mocks := `
 test() {
  case "$1" in
   -d) case "$SCENARIO:$2" in root-missing:/var/tmp/hfs-run-run1) return 1;; esac; return 0;;
   -f) case "$SCENARIO" in lock-missing) return 1;; esac; return 0;;
   '!') case "$SCENARIO:$3" in lock-symlink:/var/tmp/hfs-run-run1/vm-launch.lock|ancestor-symlink:/var) return 1;; esac; return 0;;
   *) command test "$@";;
  esac
 }
 stat() {
  case "$2" in
   '%u') case "$SCENARIO:$3" in root-unowned:/var/tmp/hfs-run-run1) printf '1';; *) printf '0';; esac;;
   '%Lp') case "$SCENARIO" in ancestor-writable) printf '777';; *) printf '755';; esac;;
   '%u:%Lp') case "$3" in /var/tmp) printf '0:1777';; *) printf '0:700';; esac;;
   '%u:%Lp:%l') case "$SCENARIO" in lock-shared) printf '0:600:2';; lock-open) printf '0:644:1';; *) printf '0:600:1';; esac;;
   '%d:%i') printf '1:42';;
  esac
 }
 `
	for _, scenario := range []string{"valid", "root-missing", "root-unowned", "ancestor-writable", "ancestor-symlink", "lock-missing", "lock-symlink", "lock-shared", "lock-open"} {
		t.Run(scenario, func(t *testing.T) {
			check := exec.Command("/bin/sh", "-s", "--", "run1", "hfs-test0", "clean", strings.Repeat("a", 64))
			check.Env = []string{"SCENARIO=" + scenario, "PATH=/nonexistent"}
			check.Stdin = strings.NewReader(mocks + "\n" + prefix + "\nprintf 'lock-reached'\n")
			output, err := check.CombinedOutput()
			if scenario == "valid" {
				if err != nil || string(output) != "lock-reached" {
					t.Fatalf("valid admission failed: %v %s", err, output)
				}
			} else if err == nil || strings.Contains(string(output), "lock-reached") {
				t.Fatalf("unsafe admission reached lock: %v %s", err, output)
			}
		})
	}
}
