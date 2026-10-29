package behavior

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestFreeBSDExecutableCredential(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credential  *syscall.Credential
		setgroups   uintptr
		want        []uint32
		unsupported bool
	}{
		{name: "member", credential: &syscall.Credential{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}, setgroups: 80, want: []uint32{20001, 20002}},
		{name: "root empty", credential: &syscall.Credential{Groups: []uint32{}}, setgroups: 80, want: []uint32{0}},
		{name: "nil supplementary", credential: &syscall.Credential{Uid: 20003, Gid: 20003}, setgroups: 80, want: []uint32{20003}},
		{name: "preserve primary and duplicates", credential: &syscall.Credential{Uid: 20001, Gid: 20001, Groups: []uint32{20002, 20001, 20002}}, setgroups: 80, want: []uint32{20001, 20002, 20001, 20002}},
		{name: "new ABI rejected", credential: &syscall.Credential{Groups: []uint32{20002}}, setgroups: 596, unsupported: true},
		{name: "unknown ABI rejected", credential: &syscall.Credential{}, setgroups: 0, unsupported: true},
		{name: "inherit groups", credential: &syscall.Credential{Uid: 20001, Gid: 20001, Groups: []uint32{20002}, NoSetGroups: true}, setgroups: 80, want: []uint32{20002}},
		{name: "inherit ignores unused ABI", credential: &syscall.Credential{Groups: []uint32{}, NoSetGroups: true}, setgroups: 596, want: []uint32{}},
		{name: "nil credential", setgroups: 80},
		{name: "nil ignores unused ABI", setgroups: 596},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := copyExecutableCredential(tc.credential)
			got, err := freeBSDExecutableCredential(tc.credential, tc.setgroups)
			if !reflect.DeepEqual(tc.credential, original) {
				t.Fatal("constructor mutated caller credential")
			}
			if tc.unsupported {
				if !errors.Is(err, syscall.ENOTSUP) || got != nil {
					t.Fatalf("unsupported ABI returned %v, %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.credential == nil {
				if got != nil {
					t.Fatal("nil credential changed to explicit credentials")
				}
				return
			}
			want := *original
			want.Groups = tc.want
			if !reflect.DeepEqual(*got, want) {
				t.Fatalf("native vector = %+v, want %+v", *got, want)
			}
			got.Uid++
			for i := range got.Groups {
				got.Groups[i]++
			}
			if !reflect.DeepEqual(tc.credential, original) {
				t.Fatal("native credential aliases caller storage")
			}
		})
	}
}

func TestCopyExecutableCredential(t *testing.T) {
	if copyExecutableCredential(nil) != nil {
		t.Fatal("nil credential changed to explicit credentials")
	}
	for _, groups := range [][]uint32{nil, {}, {20002, 20001, 20002}} {
		c := &syscall.Credential{Uid: 20001, Gid: 20001, Groups: groups, NoSetGroups: true}
		got := copyExecutableCredential(c)
		if !reflect.DeepEqual(got, c) || got == c {
			t.Fatalf("credential copy = %+v, want independent %+v", got, c)
		}
		if len(groups) > 0 {
			got.Groups[0]++
			if c.Groups[0] != 20002 {
				t.Fatal("copy aliases caller groups")
			}
			c.Groups[1]++
			if got.Groups[1] != 20001 {
				t.Fatal("caller aliases copied groups")
			}
		}
	}
}

func TestExecutableCredentialHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "behavior-credential-helper" {
		t.Skip("private subprocess entry point")
	}
	groups, err := os.Getgroups()
	if err != nil {
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		UID, GID int
		Groups   []int
	}{os.Geteuid(), os.Getegid(), groups}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestExecutableSpawnerOwnCredential(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(groups)
	for _, tc := range []struct {
		name       string
		credential *syscall.Credential
	}{
		{name: "nil inherits"},
		// An unused, deliberately different Groups vector detects accidental
		// setgroups calls. UID/GID remain the caller's own; no privilege needed.
		{name: "NoSetGroups inherits", credential: &syscall.Credential{Uid: uint32(os.Geteuid()), Gid: uint32(os.Getegid()), Groups: []uint32{^uint32(0)}, NoSetGroups: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var output bytes.Buffer
			original := copyExecutableCredential(tc.credential)
			process, err := ExecutableSpawner(executable, "-test.run=^TestExecutableCredentialHelper$", "behavior-credential-helper")(ctx, nil, &output, tc.credential)
			if err != nil {
				t.Fatal(err)
			}
			if err := process.Wait(); err != nil {
				t.Fatal(err)
			}
			var got struct {
				UID, GID int
				Groups   []int
			}
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			slices.Sort(got.Groups)
			if got.UID != os.Geteuid() || got.GID != os.Getegid() || !slices.Equal(got.Groups, groups) {
				t.Fatalf("child credential = %+v, want UID %d GID %d groups %v", got, os.Geteuid(), os.Getegid(), groups)
			}
			if !reflect.DeepEqual(tc.credential, original) {
				t.Fatal("spawner mutated caller credential")
			}
		})
	}
}
