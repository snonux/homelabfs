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
	for _, guard := range []string{`test -d "$directory"`, `test ! -L "$directory"`, `test -f "$lock"`, `test ! -L "$lock"`, `test "$(stat -f '%u:%Op:%l' "$lock")" = 0:100600:1`, `test "$(stat -f '%u:%Op' "$root")" = 0:40700`} {
		if !strings.Contains(prefix, guard) {
			t.Fatalf("guard does not precede lock: %s", guard)
		}
	}
	if strings.Index(script, `lease="$root/$target.vm-enrollment"`) < index {
		t.Fatal("enrollment was not checked while holding lock")
	}
	lockedStart := strings.Index(script, "<<'LOCKED_FAULT'\n")
	lockedEnd := strings.Index(script, "# Fixed seven-line data format:")
	if lockedStart < 0 || lockedEnd <= lockedStart {
		t.Fatal("missing locked read-only admission")
	}
	lockedPrefix := script[lockedStart+len("<<'LOCKED_FAULT'\n") : lockedEnd]
	// Execute only the read-only admissions before lock acquisition and inside
	// the lock. VM commands, enrollment reads and real paths are never evaluated.
	// FreeBSD %Op includes type/special bits; %Lp contains only the low 0777.
	mocks := `
 test() {
  case "$1" in
   -d) case "$SCENARIO:$2" in root-missing:/var/tmp/hfs-run-run1) return 1;; esac; return 0;;
   -f) case "$SCENARIO:$2" in lock-missing:*/vm-launch.lock|lease-missing:*/hfs-test0.vm-enrollment) return 1;; esac; return 0;;
   '!') case "$SCENARIO:$3" in lock-symlink:*/vm-launch.lock|lease-symlink:*/hfs-test0.vm-enrollment|ancestor-symlink:/var|tmp-symlink:/var/tmp) return 1;; esac; return 0;;
   *) command test "$@";;
  esac
 }
 stat() {
  owner=0 links=1 identity=1:42
  case "$3" in
   /var/tmp) mode=41777;;
   /var/tmp/hfs-run-run1) mode=40700;;
   */vm-launch.lock|*/hfs-test0.vm-enrollment) mode=100600;;
   *) mode=40755;;
  esac
  case "$SCENARIO:$3" in
   root-unowned:/var/tmp/hfs-run-run1|tmp-unowned:/var/tmp|lock-unowned:*/vm-launch.lock|lease-unowned:*/hfs-test0.vm-enrollment) owner=1;;
   ancestor-writable:/var) mode=40777;;
   tmp-no-sticky:/var/tmp) mode=40777;;
   tmp-setuid:/var/tmp) mode=45777;;
   tmp-setgid:/var/tmp) mode=43777;;
   root-open:/var/tmp/hfs-run-run1) mode=40755;;
   root-setuid:/var/tmp/hfs-run-run1) mode=44700;;
   root-setgid:/var/tmp/hfs-run-run1) mode=42700;;
   root-sticky:/var/tmp/hfs-run-run1) mode=41700;;
   lock-shared:*/vm-launch.lock|lease-shared:*/hfs-test0.vm-enrollment) links=2;;
   lock-open:*/vm-launch.lock|lease-open:*/hfs-test0.vm-enrollment) mode=100644;;
   lock-setuid:*/vm-launch.lock|lease-setuid:*/hfs-test0.vm-enrollment) mode=104600;;
   lock-setgid:*/vm-launch.lock|lease-setgid:*/hfs-test0.vm-enrollment) mode=102600;;
   lock-sticky:*/vm-launch.lock|lease-sticky:*/hfs-test0.vm-enrollment) mode=101600;;
   lock-replaced:*/vm-launch.lock) identity=1:43;;
  esac
  low=$(printf '%o' "$((0$mode & 0777))")
  case "$2" in
   '%u') printf '%s' "$owner";;
   '%Lp') printf '%s' "$low";;
   '%u:%Lp') printf '%s:%s' "$owner" "$low";;
   '%u:%Lp:%l') printf '%s:%s:%s' "$owner" "$low" "$links";;
   '%u:%Op') printf '%s:%s' "$owner" "$mode";;
   '%u:%Op:%l') printf '%s:%s:%s' "$owner" "$mode" "$links";;
   '%d:%i') printf '%s' "$identity";;
   *) return 1;;
  esac
 }
 `
	scenarios := []string{"valid", "root-missing", "root-unowned", "root-open", "root-setuid", "root-setgid", "root-sticky", "ancestor-writable", "ancestor-symlink", "tmp-no-sticky", "tmp-setuid", "tmp-setgid", "tmp-unowned", "tmp-symlink", "lock-missing", "lock-symlink", "lock-unowned", "lock-shared", "lock-open", "lock-setuid", "lock-setgid", "lock-sticky"}
	for _, phase := range []struct {
		name, script string
		locked       bool
	}{{"before-lock", prefix, false}, {"inside-lock", lockedPrefix, true}} {
		phaseScenarios := append([]string(nil), scenarios...)
		if phase.locked {
			phaseScenarios = append(phaseScenarios, "lock-replaced", "lease-missing", "lease-symlink", "lease-unowned", "lease-shared", "lease-open", "lease-setuid", "lease-setgid", "lease-sticky")
		}
		for _, scenario := range phaseScenarios {
			t.Run(phase.name+"/"+scenario, func(t *testing.T) {
				check := exec.Command("/bin/sh", "-s", "--", "run1", "hfs-test0", "clean", strings.Repeat("a", 64), "1:42")
				check.Env = []string{"SCENARIO=" + scenario, "PATH=/nonexistent"}
				check.Stdin = strings.NewReader(mocks + "\n" + phase.script + "\nprintf 'admitted'\n")
				output, err := check.CombinedOutput()
				if scenario == "valid" {
					if err != nil || string(output) != "admitted" {
						t.Fatalf("valid admission failed: %v %s", err, output)
					}
				} else if err == nil || strings.Contains(string(output), "admitted") {
					t.Fatalf("unsafe admission passed: %v %s", err, output)
				}
			})
		}
	}
}
