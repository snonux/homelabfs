package guestbridge

import (
	"context"
	"encoding/json"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"os"
	"path/filepath"
	"testing"
)

type windowRestoreNative struct {
	localNative
	hook func() error
}

func (n *windowRestoreNative) RestoreEndpoint(context.Context, ProtectedTransportState, Config) error {
	return n.hook()
}

type windowRestoreClaims struct{ *localClaims }

func (windowRestoreClaims) CheckRestoredBridge(context.Context, Context, fsapi.UUID, json.RawMessage) error {
	return nil
}

func TestRestoreFinalWriterRejectsCallbackWindowReplacement(t *testing.T) {
	for _, fault := range []string{"database", "directory", "copied-directory", "same-inode-growth"} {
		t.Run(fault, func(t *testing.T) {
			// Trusted native/provider primitives isolate the real C8 owner handoff seam.
			// Full cross-ledger/native authority is tested by controlledruntime.
			f := newBridge(t)
			f.hello(t)
			n := &windowRestoreNative{localNative: localNative{expected: f.native}}
			f.s.config.Native = n
			f.s.config.Claims = windowRestoreClaims{f.claims}
			f.s.config.Validation = &fsservice.ValidationProfile{Disposable: true, Name: "final writer descriptor primitive"}
			c := f.s.config
			root := filepath.Join(t.TempDir(), "transport")
			old, err := NewDurableTransport(f.s, durableBudget(t), root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(old.Close)
			funding, err := old.Suspend()
			if err != nil {
				t.Fatal(err)
			}
			n.hook = func() error {
				if fault == "same-inode-growth" {
					return os.Truncate(filepath.Join(root, "transport.db"), transportFileCeiling+1)
				}
				if fault == "database" {
					name := filepath.Join(root, "transport.db")
					raw, e := os.ReadFile(name)
					if e != nil {
						return e
					}
					if e = os.Rename(name, filepath.Join(filepath.Dir(root), "original-transport.db")); e != nil {
						return e
					}
					return os.WriteFile(name, raw, 0600)
				}
				if e := os.Rename(root, root+".original"); e != nil {
					return e
				}
				if e := os.Mkdir(root, 0700); e != nil {
					return e
				}
				for _, name := range []string{"owner.lock", "transport.db"} {
					if fault == "directory" {
						// Preserve both actual file inodes while replacing only the
						// enclosing directory, proving its descriptor check matters.
						if e := os.Rename(filepath.Join(root+".original", name), filepath.Join(root, name)); e != nil {
							return e
						}
						continue
					}
					raw, e := os.ReadFile(filepath.Join(root+".original", name))
					if e != nil {
						return e
					}
					if e = os.WriteFile(filepath.Join(root, name), raw, 0600); e != nil {
						return e
					}
				}
				return nil
			}
			_, next, err := RestoreDurableEndpoint(t.Context(), c, root, funding)
			if next != nil {
				next.Close()
			}
			if err == nil {
				t.Fatal("callback-window replacement/growth installed")
			}
			if fault == "same-inode-growth" && err != fsapi.NoSpace {
				t.Fatalf("actual final capacity refusal=%v", err)
			}
		})
	}
}
