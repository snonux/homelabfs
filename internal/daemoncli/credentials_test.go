package daemoncli

import (
	"context"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fuseadapter"
)

func TestSelfCredentialsRejectForeignOrInconsistentCallers(t *testing.T) {
	c, err := newSelfCredentials()
	if err != nil {
		t.Fatal(err)
	}
	// Linux's process leader is also a live task in /proc/self/task. FreeBSD's
	// native FUSE headers directly contain this process PID.
	caller := fuseadapter.Caller{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()), Thread: uint32(os.Getpid())}
	id, err := c.Snapshot(t.Context(), caller)
	if err != nil || id.Process != 1 || id.ProcessID != uint32(os.Getpid()) {
		t.Fatal(id, err)
	}
	for _, change := range []func(*fuseadapter.Caller){
		func(c *fuseadapter.Caller) { c.UID++ },
		func(c *fuseadapter.Caller) { c.GID++ },
		func(c *fuseadapter.Caller) { c.Thread = 0 },
		func(c *fuseadapter.Caller) { c.Thread = 1 },
	} {
		bad := caller
		change(&bad)
		if _, err := c.Snapshot(t.Context(), bad); err == nil {
			t.Fatal("accepted foreign/changed caller", bad)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Snapshot(ctx, caller); err == nil {
		t.Fatal("accepted canceled credential capture")
	}
	// Returned credentials cannot change the provider's immutable group vector.
	if len(id.Credentials.Groups) > 0 {
		id.Credentials.Groups[0]++
		if _, err := c.Snapshot(t.Context(), caller); err != nil {
			t.Fatal("request aliased provider groups", err)
		}
	}
}
