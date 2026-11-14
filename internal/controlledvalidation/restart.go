package controlledvalidation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	hashpkg "hash"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	bolt "go.etcd.io/bbolt"
)

// Restart retains only immutable construction policy and the complete original
// private ledger cut. It grants no native or physical restoration authority.
type Restart struct {
	mu     sync.Mutex
	config Config
	digest string
	file   os.FileInfo
	used   bool
}

// SuspendRestart snapshots all actual keys, values and bucket sequences before
// closing this sole writer. The runtime must have joined all issuing workers.
func (p *Provider) SuspendRestart(ctx context.Context) (*Restart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	if p.closed.Load() {
		return nil, fsapi.Unavailable
	}
	digest, err := providerDigest(p.db)
	if err != nil {
		return nil, err
	}
	info := p.file
	if info == nil {
		return nil, fsapi.Unavailable
	}
	c := p.config
	c.Initialize = false
	c.Admission = nil
	c.Failpoint = nil
	c.Service = nil
	c.Facts = nil
	r := &Restart{config: c, digest: digest, file: info}
	p.closed.Store(true)
	if err = p.db.Close(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reopen authenticates the unchanged actual file and full original ledger before
// normal provider construction can create any missing buckets.
func (r *Restart) Reopen(ctx context.Context, service *fsservice.Service, facts Facts) (*Provider, error) {
	if r == nil {
		return nil, fsapi.Permission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used {
		return nil, fsapi.Permission
	}
	r.used = true
	path := r.config.Directory + "/controlled-validation-v1.db"
	info, err := os.Stat(path)
	if err != nil || !os.SameFile(r.file, info) {
		return nil, fsapi.Stale
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 100 * time.Millisecond, OpenFile: restartLedgerOpener(r.file, nil)})
	if err != nil {
		return nil, err
	}
	digest, err := providerDigest(db)
	closeErr := db.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if digest != r.digest {
		return nil, fsapi.Stale
	}
	c := r.config
	c.restartFile = r.file
	c.Service = service
	c.Facts = facts
	p, err := New(ctx, c)
	if err != nil {
		return nil, err
	}
	final, err := providerDigest(p.db)
	current, statErr := os.Stat(p.db.Path())
	if err != nil || statErr != nil || final != r.digest || !os.SameFile(r.file, current) || !os.SameFile(r.file, p.file) {
		_ = p.Close()
		return nil, fsapi.Stale
	}
	return p, nil
}

func providerDigest(db *bolt.DB) (digest string, err error) {
	err = db.View(func(tx *bolt.Tx) error { digest, err = providerDigestTx(tx); return err })
	return digest, err
}

// providerDigestTx is providerDigest inside an existing read transaction, so
// LedgerEvidence can take the digest, size and claim summary from one
// snapshot.
func providerDigestTx(tx *bolt.Tx) (string, error) {
	h := sha256.New()
	err := tx.ForEach(func(k []byte, b *bolt.Bucket) error { hashPart(h, k); return hashBucket(h, b) })
	return hex.EncodeToString(h.Sum(nil)), err
}
func hashPart(h hashpkg.Hash, p []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(p)))
	_, _ = h.Write(n[:])
	_, _ = h.Write(p)
}
func hashBucket(h hashpkg.Hash, b *bolt.Bucket) error {
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], b.Sequence())
	_, _ = h.Write(seq[:])
	return b.ForEach(func(k, v []byte) error {
		hashPart(h, k)
		if v == nil {
			_, _ = h.Write([]byte{0})
			return hashBucket(h, b.Bucket(k))
		}
		_, _ = h.Write([]byte{1})
		hashPart(h, v)
		return nil
	})
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
