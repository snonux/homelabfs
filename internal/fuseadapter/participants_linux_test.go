//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestControlledParticipantsIdentityAndRevocation(t *testing.T) {
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	id := MountIdentity{Volume: testID(1), Generation: testID(2), Session: testID(3)}
	if err := p.BindMount(id); err != nil {
		t.Fatal(err)
	}
	if err := p.BindMount(id); !errors.Is(err, fsapi.Stale) {
		t.Fatal("provider reused", err)
	}
	own := Caller{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()), Thread: uint32(os.Getpid())}
	snapshot, err := p.Snapshot(context.Background(), own)
	if err != nil || snapshot.Process != 1 {
		t.Fatal(snapshot, err)
	}
	p.processes[1].groups = append(p.processes[1].groups, 999999)
	if _, err := p.Snapshot(context.Background(), own); !errors.Is(err, fsapi.Permission) {
		t.Fatal("mutated groups accepted", err)
	}
	p.processes[1].groups = p.processes[1].groups[:len(p.processes[1].groups)-1]
	wrong := own
	wrong.Thread = 1
	if _, err := p.Snapshot(context.Background(), wrong); !errors.Is(err, fsapi.Permission) {
		t.Fatal("unknown PID accepted", err)
	}
	child := startFixtureChild(t, p, nil)
	child.call(t, fixtureCommand{Op: "ready"})
	caller := own
	caller.Thread = uint32(child.process.PID())
	issued, err := p.Snapshot(context.Background(), caller)
	if err != nil || issued.Process == snapshot.Process || issued.ProcessID != caller.Thread {
		t.Fatal(issued, err)
	}
	wrong = caller
	wrong.UID++
	if _, err := p.Snapshot(context.Background(), wrong); !errors.Is(err, fsapi.Permission) {
		t.Fatal("mutated header accepted", err)
	}
	child.call(t, fixtureCommand{Op: "exit"})
	if err := child.process.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Snapshot(context.Background(), caller); !errors.Is(err, fsapi.Permission) {
		t.Fatal("reaped incarnation admitted", err)
	}
	next := startFixtureChild(t, p, nil)
	next.call(t, fixtureCommand{Op: "ready"})
	caller.Thread = uint32(next.process.PID())
	fresh, err := p.Snapshot(context.Background(), caller)
	if err != nil || fresh.Process == issued.Process {
		t.Fatal("process token reused", fresh, err)
	}
	p.seal()
	if _, err := p.Start(t.Context(), ParticipantSpec{Executable: "/bin/true"}); !errors.Is(err, fsapi.Stale) {
		t.Fatal("spawn after publication admitted", err)
	}
	next.call(t, fixtureCommand{Op: "exit"})
	if err := next.process.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestControlledParticipantsRejectFixedCredentialMutation(t *testing.T) {
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := p.BindMount(MountIdentity{Volume: testID(1), Generation: testID(2), Session: testID(3)}); err != nil {
		t.Fatal(err)
	}
	// NoSetGroups would inherit whatever groups happen to exist, violating the
	// exact credential contract even when the requested UID/GID look plausible.
	if _, err := p.Start(t.Context(), ParticipantSpec{Executable: "/bin/true", Credentials: &syscall.Credential{NoSetGroups: true}}); !errors.Is(err, fsapi.Unsupported) {
		t.Fatal(err)
	}
	p.processes[1].uid++
	if _, err := p.Snapshot(context.Background(), Caller{UID: p.processes[1].uid, GID: p.processes[1].gid, Thread: uint32(os.Getpid())}); !errors.Is(err, fsapi.Permission) {
		t.Fatal("changed process IDs accepted", err)
	}
}

func TestLinuxParticipantCredentialGroupsUnchanged(t *testing.T) {
	c := syscall.Credential{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}
	native := participantCredential(c)
	if !slices.Equal(native.Groups, []uint32{20002}) {
		t.Fatal("Linux gained compatibility prefix", native)
	}
	native.Groups[0] = 999
	if !slices.Equal(c.Groups, []uint32{20002}) {
		t.Fatal("Linux constructor aliases input")
	}
	got, err := participantGroups([]int{20002}, 20001)
	if err != nil || !slices.Equal(got, []uint32{20002}) {
		t.Fatal("Linux group normalization changed", got, err)
	}
}
