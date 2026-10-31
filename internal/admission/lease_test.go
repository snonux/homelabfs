package admission

import (
	"errors"
	"testing"
)

func TestAcceptanceSurvivesCancellationAndTimeoutEvidence(t *testing.T) {
	cfg := testConfig()
	cfg.Policy = policyFunc(func(a Authorization) error {
		// A protocol authority may inspect accounting without deadlocking.
		if a.Source.Lease.budget.Usage().Accepted != 1 {
			t.Fatal("policy lost ownership")
		}
		if a.Evidence == "publication complete" && a.Action == FinishGrant {
			return nil
		}
		if a.Evidence == "verified durable replacement" && a.Action == ReleaseResources {
			return nil
		}
		return ErrAccepted
	})
	b := newBudget(t, cfg)
	l := enqueue(t, b, request("outside-placement-origin", "v"))
	next, ok := b.Next()
	if !ok || next != l {
		t.Fatal("not dispatched")
	}
	must(t, l.Accept("exact-grant-source-result"))
	before := b.Usage()
	if err := l.Release(); !errors.Is(err, ErrAccepted) {
		t.Fatal("cancellation released accepted operation", err)
	}
	if err := l.ReleaseAccepted("verified durable replacement"); !errors.Is(err, ErrState) {
		t.Fatal("release opened unfinished slot", err)
	}
	for _, evidence := range []string{"timeout", "fenced old epoch", "raw RAM ACK", "client reply"} {
		if err := l.Finish(evidence); !errors.Is(err, ErrAccepted) {
			t.Fatal(err)
		}
		if b.Usage() != before {
			t.Fatal("failed finish changed ownership")
		}
	}
	waiter := enqueue(t, b, request("second-origin", "v"))
	if _, ok := b.Next(); ok {
		t.Fatal("second mutating grant in closure")
	}
	must(t, l.Finish("publication complete"))
	if u := b.Usage(); u.Used.Bytes != 256 || u.Used.Workers != 0 || u.Retained != 1 {
		t.Fatal(u)
	}
	if next, ok := b.Next(); !ok || next != waiter {
		t.Fatal("next slot not opened")
	}
	must(t, waiter.Release())
	if err := l.ReleaseAccepted("raw RAM ACK"); !errors.Is(err, ErrAccepted) {
		t.Fatal("RAM receipt authorized GC", err)
	}
	must(t, l.ReleaseAccepted("verified durable replacement"))
	if b.Usage().Used != (Resources{}) {
		t.Fatal("credits leaked")
	}
	if err := l.Release(); !errors.Is(err, ErrState) {
		t.Fatal("double release", err)
	}
}

func TestMissingPolicyAndChangedObligationFailClosed(t *testing.T) {
	b := newBudget(t, testConfig())
	r := request("p", "v")
	r.Mutating = false
	r.Cost.Workers = 0
	l, err := b.Reserve(r)
	must(t, err)
	must(t, l.Accept("pin"))
	must(t, l.Accept("pin"))
	if err = l.Accept("different-pin"); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	for _, operation := range []func() error{l.Release, func() error { return l.Finish("") }, func() error { return l.ReleaseAccepted("ACK") }} {
		if err = operation(); !errors.Is(err, ErrAccepted) {
			t.Fatal(err)
		}
	}
	if b.Usage().Accepted != 1 || b.Usage().Used.Bytes != 128 {
		t.Fatal("missing policy dropped pin")
	}
}

func TestStalePolicyDecisionPreservesAcceptedBytes(t *testing.T) {
	cfg := testConfig()
	cfg.Policy = policyFunc(func(a Authorization) error {
		// The callback re-enters and changes this queued lease's generation.
		must(t, a.Source.Lease.SetReady(false))
		return nil
	})
	b := newBudget(t, cfg)
	r := request("p", "v")
	r.Mutating = false
	l := enqueue(t, b, r)
	must(t, l.Accept("retained"))
	before := b.Usage()
	if err := l.ReleaseAccepted("previous generation"); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if b.Usage() != before || len(b.leases) != 1 {
		t.Fatal("stale policy released ownership")
	}
	if _, ok := b.Next(); ok {
		t.Fatal("callback did not park queued work")
	}
}

func TestTransferOverlapsAndRequiresProtectedAuthority(t *testing.T) {
	cfg := testConfig()
	cfg.Policy = policyFunc(func(a Authorization) error {
		if a.Action != TransferResources {
			return nil
		}
		if a.Source.Lease.budget.Usage().Used.Bytes != 256 {
			t.Fatal("source freed before destination reserved")
		}
		if a.Target.Obligation != a.Source.Obligation || a.Target.Request.Peer != "new-holder" {
			t.Fatal("transfer changed obligation")
		}
		if a.Evidence != "protected transfer" {
			return ErrAccepted
		}
		return nil
	})
	b := newBudget(t, cfg)
	r := request("old-holder", "v")
	r.Mutating = false
	r.Cost.Workers = 0
	l, err := b.Reserve(r)
	must(t, err)
	must(t, l.Accept("origin-graph"))
	r.Peer = "new-holder"
	if _, err = l.Transfer(b, r, "receipt only"); !errors.Is(err, ErrAccepted) {
		t.Fatal(err)
	}
	if b.Usage().Used.Bytes != 128 || b.Usage().Accepted != 1 {
		t.Fatal("failed handoff lost or leaked bytes")
	}
	transferred, err := l.Transfer(b, r, "protected transfer")
	must(t, err)
	if b.Usage().Used.Bytes != 128 || b.Usage().Accepted != 1 {
		t.Fatal("handoff accounting")
	}
	if err = l.Release(); !errors.Is(err, ErrState) {
		t.Fatal("old lease survived handoff")
	}
	if err = transferred.Release(); !errors.Is(err, ErrAccepted) {
		t.Fatal("new holder lost protection")
	}
	must(t, transferred.ReleaseAccepted("verified equivalent"))
	if len(b.peers[Live])+len(b.volumes[Live]) != 0 {
		t.Fatal("handoff scope leak")
	}
	other := newBudget(t, testConfig())
	if _, err = transferred.Transfer(other, r, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("implicit cross-process handoff", err)
	}
}

func TestFailedTransferCapacityRetainsSource(t *testing.T) {
	cfg := testConfig()
	p := cfg.Pools[Live]
	p.Global.Bytes = 128
	p.Global.OwnerRAM = 128
	p.Global.Dependencies = 128
	p.PerPeer = p.Global
	p.PerVolume = p.Global
	cfg.Pools[Live] = p
	cfg.Policy = policyFunc(func(Authorization) error { t.Fatal("policy before capacity"); return nil })
	b := newBudget(t, cfg)
	r := request("p", "v")
	r.Mutating = false
	r.Cost.Workers = 0
	l, err := b.Reserve(r)
	must(t, err)
	must(t, l.Accept("accepted"))
	if _, err = l.Transfer(b, r, "transfer"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if b.Usage().Used.Bytes != 128 || b.Usage().Accepted != 1 {
		t.Fatal("lost source on full destination")
	}
}

func TestLocalArenaAndParkedLifecycle(t *testing.T) {
	b := newBudget(t, testConfig())
	r := request("p", "v")
	r.Mutating = false
	r.Class = Memory
	r.Cost.Workers = 0
	if _, err := b.Enqueue(r); !errors.Is(err, ErrInvalid) {
		t.Fatal("memory used as scheduler work")
	}
	l, err := b.Reserve(r)
	must(t, err)
	must(t, l.Finish(""))
	must(t, l.Release())
	if err = l.Accept("late"); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if err = l.SetReady(true); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	r = request("p", "v")
	r.Blocked = true
	l = enqueue(t, b, r)
	if _, ok := b.Next(); ok {
		t.Fatal("blocked final work took drain slot")
	}
	must(t, l.SetReady(true))
	next, ok := b.Next()
	if !ok || next != l {
		t.Fatal("not unparked")
	}
	if err = l.SetReady(false); !errors.Is(err, ErrState) {
		t.Fatal("running work parked")
	}
	must(t, l.Finish(""))
	must(t, l.Release())
}
