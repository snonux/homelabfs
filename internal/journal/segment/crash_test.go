package segment

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestCrashAtEveryWriterFailpoint(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		ctx := context.Background()
		s := testStream(kind)
		baseline := simulator(t, vfs.Config{}, s)
		options := testOptions(s)
		var schedule []string
		options.Failpoint = func(point string) error { schedule = append(schedule, point); return nil }
		if _, err := writerSchedule(ctx, baseline, s, options); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
			for _, namespace := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
				for stop, stage := range schedule {
					t.Run(fmt.Sprintf("kind%d/data%d/name%d/%03d-%s", kind, mode, namespace, stop, stage), func(t *testing.T) {
						sim := simulator(t, vfs.Config{Seed: 17, DataLoss: mode, NamespaceLoss: namespace}, s)
						o := testOptions(s)
						reached := 0
						fault := errors.New("simulated process stop")
						o.Failpoint = func(point string) error {
							i := reached
							reached++
							if i == stop {
								if point != stage {
									t.Fatalf("schedule changed: %s != %s", point, stage)
								}
								return fault
							}
							return nil
						}
						acknowledged, err := writerSchedule(ctx, sim, s, o)
						if !errors.Is(err, fault) {
							t.Fatalf("did not reach failpoint: %v", err)
						}
						sim.Crash()
						// Gating on an interrupted OPEN is allowed; every previously
						// acknowledged exact frame must still exist in retained bytes.
						entries, err := discover(ctx, sim, s.Directory(), Limits{})
						if err != nil {
							t.Fatal(err)
						}
						var raw []byte
						for _, n := range entries {
							raw = append(raw, fileBytes(t, sim, s.Directory()+"/"+n.base)...)
						}
						for _, frame := range acknowledged {
							if !bytes.Contains(raw, frame) {
								t.Fatalf("acknowledged frame lost at %s", stage)
							}
						}
						_, recoveryErr := Recover(ctx, sim, s, RecoveryOptions{})
						if recoveryErr == nil {
							_ = readAll(t, sim, s)
						} // A corrupt/torn suffix must gate; never skip it.
					})
				}
			}
		}
	}
}

func writerSchedule(ctx context.Context, b Backend, s Stream, options Options) ([][]byte, error) {
	w, err := NewWriter(ctx, b, s, options)
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Close() }()
	var acknowledged [][]byte
	for range 8 {
		r := testRecord(s)
		receipt, err := w.Append(ctx, r)
		if err != nil {
			return acknowledged, err
		}
		if err := receipt.Wait(ctx); err != nil {
			return acknowledged, err
		}
		r.Header.Sequence = receipt.Reference.Position.Sequence
		frame, err := record.Encode(r)
		if err != nil {
			return acknowledged, err
		}
		acknowledged = append(acknowledged, frame)
	}
	return acknowledged, w.Seal(ctx)
}

func TestRepairAndPublicationCrashBoundaries(t *testing.T) {
	for _, stage := range []string{"before-truncate", "after-truncate", "before-sync", "after-sync"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			s := testStream(1)
			sim := simulator(t, vfs.Config{}, s)
			w := mustWriter(t, sim, s, testOptions(s))
			mustAppend(t, w, s)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := Recover(ctx, sim, s, RecoveryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			name := r.Segments[0].Name
			original := fileBytes(t, sim, name)
			boundary := int64(binary.BigEndian.Uint32(original[20:]))
			torn := original[:len(original)-1]
			replaceFile(t, sim, name, torn)
			preserved := bytes.Clone(torn)
			fault := errors.New("repair crash")
			_, err = Recover(ctx, sim, s, RecoveryOptions{Repair: func(context.Context, Tail) error { return nil }, Failpoint: func(point string) error {
				if point == stage {
					return fault
				}
				return nil
			}})
			if !errors.Is(err, fault) {
				t.Fatal(err)
			}
			sim.Crash()
			recovered := fileBytes(t, sim, name)
			if stage == "after-sync" {
				if !bytes.Equal(recovered, original[:boundary]) {
					t.Fatal("synced repair lost")
				}
			} else if !bytes.Equal(recovered, preserved) {
				t.Fatal("unsynced repair unexpectedly persisted")
			}
		})
	}
	for _, stage := range []string{"before-sync", "after-sync", "before-rename", "after-rename", "before-directory-sync", "after-directory-sync"} {
		t.Run("sealed-open/"+stage, func(t *testing.T) {
			ctx := context.Background()
			s := testStream(2)
			sim := simulator(t, vfs.Config{}, s)
			options := testOptions(s)
			w := mustWriter(t, sim, s, options)
			mustAppend(t, w, s)
			// Leave a fully durable SEAL under the original open name.
			w.opts.Failpoint = func(point string) error {
				if point == "before-rename" {
					return errors.New("stop publication")
				}
				return nil
			}
			if err := w.Seal(ctx); err == nil {
				t.Fatal("missing stop")
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			sim.Crash()
			fault := errors.New("recovery stop")
			_, err := Recover(ctx, sim, s, RecoveryOptions{Failpoint: func(point string) error {
				if point == stage {
					return fault
				}
				return nil
			}})
			if !errors.Is(err, fault) {
				t.Fatal(err)
			}
			sim.Crash()
			r, err := Recover(ctx, sim, s, RecoveryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Segments) != 1 || r.Segments[0].Seal == nil {
				t.Fatal("sealed prefix lost")
			}
			_ = readAll(t, sim, s)
		})
	}
}

func TestCompleteCorruptEmbeddedEvidenceNeverRepairs(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	o := testOptions(s)
	o.MaximumBytes = 1 << 20
	w := mustWriter(t, sim, s, o)
	inner := testRecord(s)
	inner.Header.Sequence = 2
	innerBytes, err := record.Encode(inner)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := record.ReferenceTo(inner, innerBytes)
	if err != nil {
		t.Fatal(err)
	}
	outer := testRecord(s)
	outer.Payload = record.OrdinaryResult{Mutation: ref, Commit: &ref, Outcome: 1, Evidence: record.Evidence{Frames: [][]byte{innerBytes}}}
	if _, err := w.Append(ctx, outer); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	name := r.Segments[0].Name
	data := fileBytes(t, sim, name)
	boundary := int(binary.BigEndian.Uint32(data[20:]))
	frame := data[boundary:]
	offset := bytes.Index(frame, innerBytes)
	if offset < 0 {
		t.Fatal("missing evidence frame")
	}
	embedded := frame[offset : offset+len(innerBytes)]
	binary.BigEndian.PutUint32(embedded[20:], uint32(len(embedded)+1))
	binary.BigEndian.PutUint32(embedded[24:], uint32(len(embedded)-148+1))
	binary.BigEndian.PutUint32(frame[len(frame)-4:], crc32.Checksum(frame[:len(frame)-4], crc32.MakeTable(crc32.Castagnoli)))
	replaceFile(t, sim, name, data)
	called := false
	_, err = Recover(ctx, sim, s, RecoveryOptions{Repair: func(context.Context, Tail) error { called = true; return nil }})
	if err == nil || called {
		t.Fatalf("complete outer frame treated as repairable: %v", err)
	}
	if !bytes.Equal(data, fileBytes(t, sim, name)) {
		t.Fatal("corrupt complete frame changed")
	}
}

func TestDiscoveryRejectsExtrasGapsRolesAndBudgets(t *testing.T) {
	for _, variant := range []string{"extra", "gap", "overlap", "stream", "segments-limit", "bytes-limit", "duplicate-id"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			s := testStream(1)
			sim := simulator(t, vfs.Config{}, s)
			o := testOptions(s)
			w := mustWriter(t, sim, s, o)
			for range 8 {
				mustAppend(t, w, s)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := Recover(ctx, sim, s, RecoveryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			opts := RecoveryOptions{}
			switch variant {
			case "extra":
				f, err := sim.Open(ctx, s.Directory()+"/unexpected", os.O_CREATE|os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
			case "gap":
				if err := sim.Unlink(ctx, r.Segments[0].Name); err != nil {
					t.Fatal(err)
				}
			case "overlap":
				f, err := sim.Open(ctx, s.Directory()+"/00000000000000000001-open.hfj", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
			case "stream":
				data := fileBytes(t, sim, r.Segments[0].Name)
				total := int(binary.BigEndian.Uint32(data[20:]))
				wrong := id(99)
				copy(data[32:48], wrong[:])
				binary.BigEndian.PutUint32(data[total-4:total], crc32.Checksum(data[:total-4], crc32.MakeTable(crc32.Castagnoli)))
				replaceFile(t, sim, r.Segments[0].Name, data)
			case "segments-limit":
				opts.Limits.Segments = 1
			case "bytes-limit":
				opts.Limits.Bytes = 1
			case "duplicate-id":
				data := fileBytes(t, sim, r.Segments[len(r.Segments)-1].Name)
				total := int(binary.BigEndian.Uint32(data[20:]))
				segmentID := r.Segments[0].Open.Segment
				copy(data[144:160], segmentID[:])
				binary.BigEndian.PutUint32(data[total-4:total], crc32.Checksum(data[:total-4], crc32.MakeTable(crc32.Castagnoli)))
				replaceFile(t, sim, r.Segments[len(r.Segments)-1].Name, data)
			}
			if _, err := Recover(ctx, sim, s, opts); err == nil {
				t.Fatal("invalid stream admitted")
			}
		})
	}
}
