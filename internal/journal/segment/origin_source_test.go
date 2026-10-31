package segment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func originFixture() record.NormalRecord {
	s := record.OriginStream{Cluster: id(1), Volume: id(2), Generation: id(3), Configuration: id(4), Epoch: 1, Origin: id(5), Journal: id(6)}
	return record.NormalRecord{Stream: s, Sequence: 1, Operation: id(7), Creator: record.Launch{Node: s.Origin, Incarnation: id(8)}, Payload: record.OriginOpen{Segment: id(9), MaximumBytes: 1024}}
}

func mkdirOrigin(t *testing.T, b Backend, s record.OriginStream) {
	t.Helper()
	dir := "."
	for _, part := range strings.Split(OriginDirectory(s), "/") {
		next := path.Join(dir, part)
		if err := b.Mkdir(context.Background(), next, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		if err := b.SyncDir(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
}

func TestOriginSourceDurabilityRestartAndIdentity(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
		t.Run(string(rune('0'+mode)), func(t *testing.T) {
			sim, err := vfs.NewSimulator(vfs.Config{DataLoss: mode})
			if err != nil {
				t.Fatal(err)
			}
			r := originFixture()
			mkdirOrigin(t, sim, r.Stream)
			s, err := OpenOriginSource(ctx, sim, r.Stream, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			b, err := record.EncodeNormal(r)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := s.Append(ctx, b)
			if err != nil {
				t.Fatal(err)
			}
			sim.Crash()
			s, err = OpenOriginSource(ctx, sim, r.Stream, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Read(ctx, ref)
			if err != nil || !bytes.Equal(got, b) {
				t.Fatalf("durable creator bytes changed: %v", err)
			}
			if _, err := s.Append(ctx, b); err != nil {
				t.Fatal("identical replay failed", err)
			}
			bad := ref
			bad.SHA256[0] ^= 1
			if _, err := s.Read(ctx, bad); err == nil {
				t.Fatal("wrong hash accepted")
			}
			r.Creator.Incarnation = id(10)
			changed, err := record.EncodeNormal(r)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Append(ctx, changed); err == nil {
				t.Fatal("new daemon overwrote historical creator")
			}
			r.Sequence = 2
			r.Payload = record.NormalFrontier{Stream: r.Stream, Received: 1, Durable: 1}
			changed, err = record.EncodeNormal(r)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Append(ctx, changed); err != nil {
				t.Fatal("reconciled lifetime could not append new source position", err)
			}
		})
	}
}

func TestOriginSourcePreservesUncertainShortWrite(t *testing.T) {
	ctx := context.Background()
	sim, err := vfs.NewSimulator(vfs.Config{DataLoss: vfs.Keep, NamespaceLoss: vfs.Keep})
	if err != nil {
		t.Fatal(err)
	}
	r := originFixture()
	mkdirOrigin(t, sim, r.Stream)
	b := &shortBackend{Backend: sim}
	s, err := OpenOriginSource(ctx, b, r.Stream, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := record.EncodeNormal(r)
	if err != nil {
		t.Fatal(err)
	}
	b.armed = true
	if _, err := s.Append(ctx, data); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, data); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("uncertain append retried", err)
	}
	sim.Crash()
	if _, err := OpenOriginSource(ctx, sim, r.Stream, Limits{}); err == nil {
		t.Fatal("partial frame erased or admitted")
	}
	st, err := sim.Stat(ctx, OriginDirectory(r.Stream)+"/"+originName(1))
	if err != nil || st.Size != int64(len(data)/2) {
		t.Fatalf("failed evidence changed: %+v %v", st, err)
	}
}

func TestOriginSourceActualFilesystemAndBounds(t *testing.T) {
	ctx := context.Background()
	b, err := vfs.NewOS(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}()
	r := originFixture()
	mkdirOrigin(t, b, r.Stream)
	s, err := OpenOriginSource(ctx, b, r.Stream, Limits{Segments: 1, Bytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	data, err := record.EncodeNormal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, data); err != nil {
		t.Fatal(err)
	}
	r.Sequence = 2
	r.Payload = record.NormalFrontier{Stream: r.Stream, Received: 1, Durable: 1}
	data, err = record.EncodeNormal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, data); !errors.Is(err, ErrLimit) {
		t.Fatalf("frame count unbounded: %v", err)
	}
	if _, err := OpenOriginSource(ctx, b, r.Stream, Limits{Segments: 4097}); !errors.Is(err, ErrLimit) {
		t.Fatal("hard scan bound ignored", err)
	}
}
