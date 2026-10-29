package failpoint

import (
	"context"
	"errors"
	"testing"
)

func TestBlockHoldRequiresLiveMatchingScope(t *testing.T) {
	registry, control := NewForTesting()
	activation, err := control.Enable(JournalAppend, Rule{Action: Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	control.Scope(7)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- registry.Hit(ctx, JournalAppend) }()
	select {
	case <-activation.Triggered():
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if _, err = activation.HoldBlock(8); err == nil {
		t.Fatal("unrelated scope acquired block")
	}
	hold, err := activation.HoldBlock(7)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = activation.HoldBlock(7); err == nil {
		t.Fatal("canceled/held waiter authorized another hold")
	}
	hold.Release()
	hold.Release()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = activation.HoldBlock(7); err == nil {
		t.Fatal("historical hit authorized hold")
	}
	activation.Release()
	if _, err = activation.HoldBlock(7); err == nil {
		t.Fatal("released activation authorized hold")
	}
}

func TestBlockHoldDefersReleaseWithoutChangingNormalReleaseResult(t *testing.T) {
	registry, control := NewForTesting()
	activation, err := control.Enable(JournalAppend, Rule{Action: Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	control.Scope(1)
	done := make(chan error, 1)
	go func() { done <- registry.Hit(t.Context(), JournalAppend) }()
	<-activation.Triggered()
	hold, err := activation.HoldBlock(1)
	if err != nil {
		t.Fatal(err)
	}
	activation.Release()
	select {
	case <-hold.Interrupted():
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	select {
	case e := <-done:
		t.Fatalf("release escaped held block: %v", e)
	default:
	}
	hold.Release()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
