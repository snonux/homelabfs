package viewcontrol

import (
	"bytes"
	"context"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Restart carries original construction policy and the exact complete protected
// controller cut. Its use still requires real Facts.CheckRecovery authority.
type Restart struct {
	mu     sync.Mutex
	config Config
	digest string
	file   os.FileInfo
	used   bool
}

// SuspendRestart takes the full authoritative cut after all issuers are joined.
func (c *Controller) SuspendRestart(ctx context.Context) (*Restart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() || c.poisoned.Load() {
		return nil, fsapi.Unavailable
	}
	raw, err := c.rawState()
	if err != nil {
		return nil, err
	}
	info := c.file
	if info == nil {
		return nil, fsapi.Unavailable
	}
	if err = c.db.View(func(tx *bolt.Tx) error {
		if !bytes.Equal(raw, tx.Bucket([]byte(version)).Get([]byte("state"))) {
			return fsapi.Stale
		}
		return nil
	}); err != nil {
		return nil, err
	}
	r := &Restart{config: c.config, digest: hash(raw), file: info}
	r.config.Initialize = false
	r.config.Failpoint = nil
	r.config.RestoreCurrentSources = true
	r.config.Service = nil
	r.config.Providers = nil
	r.config.Facts = nil
	c.closed.Store(true)
	if err = c.db.Close(); err != nil {
		return nil, err
	}
	c.state = ledger{}
	return r, nil
}

// CheckCut compares the full original protected controller state before reopen
// clears learned acknowledgements or remembers a reconstructed current source.
func (r *Restart) CheckCut(raw []byte) error {
	if r == nil || hash(raw) != r.digest {
		return fsapi.Stale
	}
	return nil
}

// Reopen retains the exact original private inode and signed policy. Current
// source reconstruction is byte-exact; historical cuts stay unsupported.
func (r *Restart) Reopen(ctx context.Context, service *fsservice.Service, providers map[string]*controlledvalidation.Provider, facts Facts) (*Controller, error) {
	if r == nil {
		return nil, fsapi.Permission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used {
		return nil, fsapi.Permission
	}
	r.used = true
	// The actual filename is retained from the live DB, not supplied by a caller.
	info, err := os.Stat(filepath.Join(r.config.Directory, "viewcontrol-v1.db"))
	if err != nil || !os.SameFile(r.file, info) {
		return nil, fsapi.Stale
	}
	c := r.config
	c.restartFile = r.file
	c.Service = service
	c.Providers = providers
	c.Facts = facts
	next, err := New(ctx, c)
	if err != nil {
		return nil, err
	}
	current, err := os.Stat(next.db.Path())
	if err != nil || !os.SameFile(r.file, current) || !os.SameFile(r.file, next.file) {
		_ = next.Close()
		return nil, fsapi.Stale
	}
	return next, nil
}

// CheckRestoredTerminal reconciles a C8 terminal owner with the real controller
// ledger and exact scope snapshot before the owner is suspended or restored.
func (c *Controller) CheckRestoredTerminal(ctx context.Context, scope guestbridge.Context, seals []guestbridge.TerminalSeal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	saved, ok := c.state.Terminal[scope.Session]
	if !ok || c.closed.Load() || c.poisoned.Load() {
		return fsapi.Stale
	}
	var supplied *guestbridge.TerminalSeal
	for i := range seals {
		if seals[i].Owner == "CONTROLLER" {
			if supplied != nil {
				return fsapi.Stale
			}
			supplied = &seals[i]
		}
	}
	if supplied == nil || !equal(saved.Seal, *supplied) {
		return fsapi.Stale
	}
	snapshot, err := c.terminalSnapshot(scope)
	if err != nil {
		return err
	}
	if string(snapshot) != string(saved.Snapshot) || hash(snapshot) != supplied.StateHash {
		return fsapi.Stale
	}
	return nil
}

// Pin the actual descriptor bbolt will own, before it can map or mutate it.
// The private expected inode is selected only by the old live owner's token.
func restartLedgerOpener(expected os.FileInfo, captured *os.FileInfo) func(string, int, os.FileMode) (*os.File, error) {
	return func(name string, flag int, mode os.FileMode) (*os.File, error) {
		f, err := os.OpenFile(name, flag|syscall.O_NOFOLLOW, mode)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || expected != nil && !os.SameFile(expected, info) {
			_ = f.Close()
			return nil, fsapi.Stale
		}
		if captured != nil {
			*captured = info
		}
		return f, nil
	}
}
