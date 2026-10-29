//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/behavior"
	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestPortableBehaviorHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "behavior-helper" {
		t.Skip("private subprocess entry point")
	}
	if err := behavior.Helper(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

// TestLinuxPortableBehavior runs two explicitly distinct controlled profiles.
// Their union covers append and mmap without pretending one mount admits both.
func TestLinuxPortableBehavior(t *testing.T) {
	requireLocalFuse(t)
	reports := os.Getenv("HOMELABFS_BEHAVIOR_REPORTS")
	if reports == "" {
		var err error
		reports, err = os.MkdirTemp("", "hfs-behavior-reports-")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("JSON/JUnit evidence: %s", reports)
	for _, profile := range []string{"hfs-direct", "hfs-buffered"} {
		t.Run(profile, func(t *testing.T) { portableProfile(t, profile, reports) })
	}
}

func portableProfile(t *testing.T, profile, reports string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	base, err := os.MkdirTemp("", "hfs-behavior-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("run-owned fixture: %s", base)
	clean := false
	t.Cleanup(func() {
		if clean && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved failed fixture: %s", base)
		}
	})
	mountpoint := filepath.Join(base, "mount")
	if err := os.Mkdir(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	service := serviceWithIO(t, base, nativeRequestBytes)
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	buffered := profile == "hfs-buffered"
	multiuser := buffered && os.Geteuid() == 0
	a, err := New(Config{Service: service, Validation: service.FrontendInfo().Validation, Credentials: p, BufferedValidation: buffered, MultiuserValidation: multiuser})
	if err != nil {
		t.Fatal(err)
	}
	if !buffered {
		if err := p.BindMount(a.Identity()); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if multiuser {
		if err := os.Chmod(base, 0711); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(mountpoint, 0755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		executable = filepath.Join(base, "helper")
		if err := os.WriteFile(executable, data, 0555); err != nil {
			t.Fatal(err)
		}
	}
	spawn := func(ctx context.Context, in io.Reader, out io.Writer, credential *syscall.Credential) (behavior.Process, error) {
		return p.Start(ctx, ParticipantSpec{Executable: executable, Args: []string{"-test.run=^TestPortableBehaviorHelper$", "behavior-helper"}, Env: os.Environ(), Credentials: credential, Stdin: in, Stdout: out, Stderr: os.Stderr})
	}
	c := behavior.Config{Path: mountpoint, Profile: profile, Timeout: 8 * time.Second, RootSkip: "root-only synthetic multiuser fixture skipped under current UID; direct profile uses buffered companion for multiuser coverage"}
	caps := a.Capabilities()
	c.Capabilities = map[string]bool{"direct_io": caps.DirectIO, "mmap": caps.Mmap, "default_permissions": caps.DefaultPermissions, "local_locks": caps.LocalLocks, "file_sync": caps.FileSync, "directory_sync": caps.DirectorySync, "normal_publication": caps.NormalPublication, "arbitrary_credentials": caps.ArbitraryCredentials, "writeback": caps.Writeback}
	var mount *Mount
	// One cleanup owner: children stop before unmount, even after setup failure.
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		var stopErr error
		for _, child := range append(c.Clients, c.Users...) {
			stopErr = errors.Join(stopErr, child.Stop(stopCtx))
		}
		if stopErr != nil {
			t.Error(stopErr)
			return
		}
		if mount != nil {
			if err := mount.Unmount(); err != nil {
				t.Error(err)
			}
		}
	})
	for range 3 {
		child, err := behavior.Start(ctx, spawn, nil)
		if err != nil {
			t.Fatal(err)
		}
		c.Clients = append(c.Clients, child)
	}
	if multiuser {
		for _, cred := range []*syscall.Credential{{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}, {Uid: 20003, Gid: 20003, Groups: []uint32{}}} {
			child, err := behavior.Start(ctx, spawn, cred)
			if err != nil {
				t.Fatal(err)
			}
			c.Users = append(c.Users, child)
		}
	}
	waiting, cancelled := make(chan struct{}, 4), make(chan struct{}, 4)
	actual := a.service
	a.service = dispatchFunc(func(ctx context.Context, request *fsapi.Request, command fsapi.Command) error {
		err := actual.Dispatch(ctx, request, command)
		if err == nil {
			switch command.Operation {
			case fsapi.WaitLock:
				waiting <- struct{}{}
			case fsapi.CancelLock:
				cancelled <- struct{}{}
			}
		}
		return err
	})
	wait := func(ch <-chan struct{}) func(context.Context) error {
		return func(ctx context.Context) error {
			select {
			case <-ch:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	c.LockBlocked, c.LockCancelled = wait(waiting), wait(cancelled)
	mount, err = a.Mount(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	id := a.Identity()
	c.Identity = "homelabfs-validation-v1\n" + id.Volume.String() + "\n" + id.Generation.String() + "\n" + id.Session.String() + "\n"
	c.Unmount = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := mount.Unmount(); err != nil {
			return err
		}
		mount = nil
		return nil
	}
	report := behavior.Run(ctx, c)
	for _, result := range report.Results {
		t.Logf("%s %s: %s", result.Name, result.Status, result.Detail)
	}
	for suffix, write := range map[string]func(io.Writer) error{"json": report.JSON, "xml": report.JUnit} {
		f, err := os.OpenFile(filepath.Join(reports, profile+"."+suffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(write(f), f.Close()); err != nil {
			t.Fatal(err)
		}
	}
	if report.Failed() {
		t.Fatal("portable behavior failed; evidence and resources preserved")
	}
	clean = true
}
