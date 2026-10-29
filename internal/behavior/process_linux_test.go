package behavior

import (
	"reflect"
	"syscall"
	"testing"
)

func TestLinuxExecutableCredential(t *testing.T) {
	for _, noSetGroups := range []bool{false, true} {
		for _, groups := range [][]uint32{nil, {}, {20002, 20001, 20002}} {
			c := &syscall.Credential{Uid: 20001, Gid: 20001, Groups: groups, NoSetGroups: noSetGroups}
			got, err := executableCredential(c)
			if err != nil || !reflect.DeepEqual(got, c) || got == c {
				t.Fatalf("Linux exec credential = %+v, %v; want independent %+v", got, err, c)
			}
			if len(groups) > 0 {
				got.Groups[0]++
				if c.Groups[0] != 20002 {
					t.Fatal("Linux exec aliases caller groups")
				}
			}
		}
	}
	if got, err := executableCredential(nil); got != nil || err != nil {
		t.Fatal(got, err)
	}
}
