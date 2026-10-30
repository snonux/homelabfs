//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"runtime"
	"slices"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestMountSecurityOptions(t *testing.T) {
	want := []string{"default_permissions", "nosuid"}
	if runtime.GOOS == "linux" {
		want = append(want, "nodev")
	}
	if got := mountOptions().Options; !slices.Equal(got, want) {
		t.Fatalf("mount security options %v, want %v", got, want)
	}
}

func TestMknodCannotCreateDeviceNodes(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	service := a.service
	calls := 0
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		calls++
		return service.Dispatch(ctx, r, c)
	})
	for _, tc := range []struct {
		name string
		mode uint32
	}{{"character", syscall.S_IFCHR}, {"block", syscall.S_IFBLK}} {
		t.Run(tc.name, func(t *testing.T) {
			var out fuse.EntryOut
			in := fuse.MknodIn{InHeader: testHeader(1, 100), Mode: tc.mode | 0o600, Rdev: 1}
			if got := a.Mknod(nil, &in, tc.name, &out); got != fuse.ENOSYS {
				t.Fatalf("Mknod %v, want ENOSYS", got)
			}
			if calls != 0 || out.NodeId != 0 || out.Mode != 0 || out.Rdev != 0 {
				t.Fatalf("Mknod dispatched %d commands or published an entry: %+v", calls, out)
			}
		})
	}
	a.service = service
	for _, name := range []string{"character", "block"} {
		h := testHeader(1, 100)
		if got := a.Lookup(nil, &h, name, &fuse.EntryOut{}); got != fuse.ENOENT {
			t.Fatalf("Mknod left %q in the namespace: %v", name, got)
		}
	}
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateCannotPublishDeviceType(t *testing.T) {
	for _, mode := range []uint32{syscall.S_IFCHR, syscall.S_IFBLK} {
		a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
		var out fuse.CreateOut
		in := fuse.CreateIn{InHeader: testHeader(1, 100), Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: mode | 0o600}
		if got := a.Create(nil, &in, "file", &out); got != fuse.OK {
			t.Fatal(got)
		}
		if out.Mode&syscall.S_IFMT != syscall.S_IFREG || out.Rdev != 0 {
			t.Fatalf("CREATE published a special device: %+v", out)
		}
		a.OnUnmount()
		if err := a.Err(); err != nil {
			t.Fatal(err)
		}
	}
}
