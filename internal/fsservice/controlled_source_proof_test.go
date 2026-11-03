package fsservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func TestControlledSourceProofRetainsActualHistoricalBytes(t *testing.T) {
	f := setup(t, "os")
	policy, err := NewUnixPermissions(uid(60))
	if err != nil {
		t.Fatal(err)
	}
	f.c.Permissions = policy
	f.start()
	var source ControlledViewSource
	if err := f.service.Controlled(t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
		var err error
		source, err = cut.ViewSource(ctx, 1<<20)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err != nil {
		t.Fatal(err)
	}
	// Advance the actual journal/catalog/payload state. Old proof remains tied to
	// its actual worker capture, not a same-tail-only checksum comparison.
	f.create(f.c.Storage.Root, "later")
	if err := f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err != nil {
		t.Fatal(err)
	}
	altered := source
	altered.State = bytes.Clone(source.State)
	altered.State[len(altered.State)-1] ^= 1
	if err := f.service.VerifyControlledSource(t.Context(), *f.c.Validation, altered); err == nil {
		t.Fatal("altered old bytes accepted after tail advance")
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var reopened ControlledViewSource
	if err = json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	if err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, reopened); err == nil {
		t.Fatal("caller/reopened JSON minted a live capture seal")
	}
	changed := source
	changed.Applied.Sequence++
	if err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, changed); err == nil {
		t.Fatal("changed applied position accepted")
	}
	f.service.sourceCut.Lock()
	err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source)
	f.service.sourceCut.Unlock()
	if !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("source proof waited under a writer cut: %v", err)
	}
	// Change actual original journal bytes, not a supplied digest. A valid live
	// capture seal cannot hide damaged original retained history.
	recovered, err := segment.Recover(t.Context(), f.c.Source, f.c.Stream, segment.RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := f.c.Source.Open(t.Context(), recovered.Segments[0].Name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	var byteAt [1]byte
	if _, err = file.ReadAt(t.Context(), byteAt[:], 32); err != nil {
		t.Fatal(err)
	}
	byteAt[0] ^= 1
	if _, err = file.WriteAt(t.Context(), byteAt[:], 32); err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err == nil {
		t.Fatal("damaged actual journal prefix accepted")
	}
}
