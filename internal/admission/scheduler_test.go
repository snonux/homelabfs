package admission

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestVolumeAndOriginFairnessPastStalledClosure(t *testing.T) {
	b := newBudget(t, testConfig())
	stalled := enqueue(t, b, request("slow-origin", "stalled"))
	if next, ok := b.Next(); !ok || next != stalled {
		t.Fatal("missing slow grant")
	}
	must(t, stalled.Accept("unresolved-slot"))
	for range 4 {
		enqueue(t, b, request("slow-origin", "stalled"))
	}
	for range 8 {
		enqueue(t, b, request("hot-origin", "hot"))
	}
	for range 2 {
		enqueue(t, b, request("rare-origin", "hot"))
	}
	for range 3 {
		enqueue(t, b, request("outside-R", "independent"))
	}
	var volumes, hotPeers []string
	for range 6 {
		l, ok := b.Next()
		if !ok {
			t.Fatal("stalled volume blocked independent queue")
		}
		r := l.Request()
		volumes = append(volumes, r.Volume)
		if r.Volume == "hot" {
			hotPeers = append(hotPeers, r.Peer)
		}
		must(t, l.Finish(""))
		must(t, l.Release())
	}
	if fmt.Sprint(volumes) != "[hot independent hot independent hot independent]" {
		t.Fatal("volume starvation", volumes)
	}
	if fmt.Sprint(hotPeers) != "[hot-origin rare-origin hot-origin]" {
		t.Fatal("origin starvation", hotPeers)
	}
	if b.Usage().Accepted != 1 || b.active["stalled"] != stalled {
		t.Fatal("stalled grant was discarded")
	}
}

func TestReservedPoolsDispatchUnderSaturatedLiveTraffic(t *testing.T) {
	cfg := testConfig()
	for c := Class(0); c < ClassCount; c++ {
		p := cfg.Pools[c]
		p.Global.Workers = 1
		p.PerPeer.Workers = 1
		p.PerVolume.Workers = 1
		cfg.Pools[c] = p
	}
	b := newBudget(t, cfg)
	active := enqueue(t, b, request("slow", "blocked-live"))
	next, ok := b.Next()
	if !ok || next != active {
		t.Fatal("not active")
	}
	must(t, active.Accept("live-disk-stalled"))
	for i := range 63 {
		r := request(fmt.Sprint("peer", i), fmt.Sprint("vol", i))
		enqueue(t, b, r)
	}
	if _, err := b.Enqueue(request("new", "new")); !errors.Is(err, ErrCapacity) {
		t.Fatal("live overload not bounded", err)
	}
	for _, c := range []Class{CatchUp, Recovery, Control, Drain} {
		r := request("slow", "blocked-live")
		r.Class = c
		r.Closure = fmt.Sprint("reserved", c)
		enqueue(t, b, r)
	}
	seen := map[Class]bool{}
	for range 4 {
		l, ok := b.Next()
		if !ok {
			t.Fatal("reserved worker unavailable")
		}
		if l.Request().Class == Live || seen[l.Request().Class] {
			t.Fatal("reserved class starvation")
		}
		seen[l.Request().Class] = true
		// Retain each worker, proving later classes do not borrow it.
	}
	if len(seen) != 4 || b.Usage().Used.Workers != 5 {
		t.Fatal("reserve not isolated", b.Usage())
	}
	if _, ok := b.Next(); ok {
		t.Fatal("oversubscribed worker cap")
	}
}

func TestDefaultStalledVolumeCannotConsumeIndependentVolumeCapacity(t *testing.T) {
	for c := Class(0); c < ClassCount; c++ {
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			cfg := DefaultConfig()
			b := newBudget(t, cfg)
			limit := cfg.Pools[c].PerVolume
			cost := limit
			// OwnerRAM and Dependencies are contained, disjoint byte categories.
			cost.Dependencies = 0
			r := Request{Peer: "same-peer", Volume: "stalled-volume", Class: c, Cost: cost}
			stalled, err := b.Reserve(r)
			must(t, err)
			must(t, stalled.Accept("stalled-volume-owned"))
			if _, err = b.Reserve(r); !errors.Is(err, ErrCapacity) {
				t.Fatal("volume quota exceeded", err)
			}
			// This requests every remaining dimension, including worker/socket
			// headroom, on the same peer. The stalled volume keeps all its pins.
			r.Volume = "independent-volume"
			independent, err := b.Reserve(r)
			must(t, err)
			must(t, independent.Release())
			if c != Memory {
				r.Mutating = true
				r.Closure = r.Volume
				queued := enqueue(t, b, r)
				if next, ok := b.Next(); !ok || next != queued {
					t.Fatal("stalled volume consumed independent worker")
				}
				must(t, queued.Release())
			}
			if b.Usage().Used != cost || b.Usage().Accepted != 1 {
				t.Fatal("stalled volume ownership changed", b.Usage())
			}
		})
	}
}

func TestPreDirtyReservationAndDrainBeforeFinalGrant(t *testing.T) {
	cfg := testConfig()
	p := cfg.Pools[Drain]
	p.Global.Operations = 1
	p.PerPeer.Operations = 1
	p.PerVolume.Operations = 1
	cfg.Pools[Drain] = p
	cfg.Policy = policyFunc(func(a Authorization) error {
		if a.Evidence != "intermediate installed" {
			return ErrAccepted
		}
		return nil
	})
	b := newBudget(t, cfg)
	finalReq := request("reader", "v")
	finalReq.Blocked = true
	final := enqueue(t, b, finalReq)
	pageReq := request("dirty-domain", "v")
	pageReq.Class = Drain
	pageReq.Blocked = true
	page := enqueue(t, b, pageReq) // Reserve BEFORE writable permission and CPU stores.
	must(t, page.Accept("page-provenance-origin-result"))
	if _, err := b.Enqueue(pageReq); !errors.Is(err, ErrCapacity) {
		t.Fatal("second page gained writable permission without capacity", err)
	}
	if _, ok := b.Next(); ok {
		t.Fatal("frozen final waiter acquired drain resources")
	}
	// Physical quiescence is an external prerequisite. This component receives
	// the frozen package only after it, and never asserts STOPPED itself.
	must(t, page.SetReady(true))
	l, ok := b.Next()
	if !ok || l != page {
		t.Fatal("drain deadlocked behind conflicting final")
	}
	if err := page.Release(); !errors.Is(err, ErrAccepted) {
		t.Fatal("cancel lost dirty provenance")
	}
	must(t, page.Finish("intermediate installed"))
	if b.Usage().Used.Bytes != 256 || b.Usage().Used.Workers != 0 {
		t.Fatal("intermediate cut released package early")
	}
	if _, ok = b.Next(); ok {
		t.Fatal("final opened before positive drain completion")
	}
	must(t, page.ReleaseAccepted("intermediate installed"))
	must(t, final.SetReady(true))
	l, ok = b.Next()
	if !ok || l != final {
		t.Fatal("final not dispatched")
	}
	must(t, final.Release())
}

// TestOverloadRetainsOwnedPayloadsUntilDurableEquivalence exercises actual
// buffers, fair dispatch, cancellation, synced replacement files and rereads.
// It is a component simulator, not a normal-protocol acknowledgement oracle.
func TestOverloadRetainsOwnedPayloadsUntilDurableEquivalence(t *testing.T) {
	root := t.TempDir()
	payloads := map[*Lease][]byte{}
	paths := map[*Lease]string{}
	publication := map[*Lease]bool{}
	cfg := testConfig()
	cfg.Policy = policyFunc(func(a Authorization) error {
		if a.Action == FinishGrant && publication[a.Source.Lease] {
			return nil
		}
		if a.Action != ReleaseResources {
			return ErrAccepted
		}
		stored, err := os.ReadFile(paths[a.Source.Lease])
		if err != nil {
			return err
		}
		if sha256.Sum256(stored) != sha256.Sum256(payloads[a.Source.Lease]) {
			return ErrAccepted
		}
		return nil
	})
	b := newBudget(t, cfg)
	for i := range 64 {
		r := request(fmt.Sprint("origin", i%4), fmt.Sprint("volume", i%4))
		r.Cost.Bytes = 4096
		r.Cost.OwnerRAM = 4096
		r.Cost.Dependencies = 0
		r.Cost.Journal = 4096
		l := enqueue(t, b, r)
		// The payload allocation follows a successful reservation.
		payloads[l] = bytes.Repeat([]byte{byte(i + 1)}, 4096)
		must(t, l.Accept(fmt.Sprint("immutable-source-graph", i)))
	}
	full := b.Usage()
	for range 1000 {
		if _, err := b.Enqueue(request("outside-placement", "extra")); !errors.Is(err, ErrCapacity) {
			t.Fatal("unbounded overload", err)
		}
	}
	if b.Usage() != full {
		t.Fatal("rejected overload retained resources")
	}
	for l := range payloads {
		if err := l.Release(); !errors.Is(err, ErrAccepted) {
			t.Fatal("client cancellation lost accepted buffer", err)
		}
	}
	completed := 0
	for {
		l, ok := b.Next()
		if !ok {
			break
		}
		want := append([]byte(nil), payloads[l]...)
		publication[l] = true
		must(t, l.Finish("component publication"))
		if err := l.ReleaseAccepted("RAM ACK"); err == nil {
			t.Fatal("missing durable equivalent released buffer")
		}
		path := filepath.Join(root, fmt.Sprint("source-", completed))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		must(t, err)
		_, err = file.Write(want)
		must(t, err)
		must(t, file.Sync())
		must(t, file.Close())
		dir, err := os.Open(root)
		must(t, err)
		must(t, dir.Sync())
		must(t, dir.Close())
		paths[l] = path
		must(t, l.ReleaseAccepted("verified synced replacement"))
		clear(payloads[l])
		delete(payloads, l)
		stored, err := os.ReadFile(path)
		must(t, err)
		if !bytes.Equal(stored, want) {
			t.Fatal("accepted bytes lost after original buffer reuse")
		}
		completed++
	}
	if completed != 64 || len(payloads) != 0 || b.Usage().Used != (Resources{}) {
		t.Fatal("lost accepted operation or resource leak", completed, b.Usage())
	}
}

// BenchmarkSmallSynchronousWrite is non-gating component evidence: 4 KiB owned
// bytes, one real file Sync, one directory Sync and policy-checked release. It
// measures no network, normal replication, FUSE, 9p or distributed guarantee.
func BenchmarkSmallSynchronousWrite(b *testing.B) {
	root := b.TempDir()
	cfg := testConfig()
	cfg.Policy = policyFunc(func(Authorization) error { return nil })
	budget := newBudget(b, cfg)
	payload := bytes.Repeat([]byte("x"), 4096)
	file, err := os.OpenFile(filepath.Join(root, "journal"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	must(b, err)
	defer func() { must(b, file.Close()) }()
	dir, err := os.Open(root)
	must(b, err)
	defer func() { must(b, dir.Close()) }()
	b.SetBytes(4096)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r := request("origin", "volume")
		r.Cost.Bytes = 4096
		r.Cost.OwnerRAM = 4096
		r.Cost.Dependencies = 0
		r.Cost.Journal = 4096
		l := enqueue(b, budget, r)
		owned := append([]byte(nil), payload...)
		if next, ok := budget.Next(); !ok || next != l {
			b.Fatal("dispatch")
		}
		must(b, l.Accept("component-operation"))
		_, err = file.WriteAt(owned, 0)
		must(b, err)
		must(b, file.Sync())
		must(b, dir.Sync())
		must(b, l.Finish("component-only"))
		must(b, l.ReleaseAccepted("component-only"))
	}
}
