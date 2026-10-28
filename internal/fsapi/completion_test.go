package fsapi_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestCompletionConcurrentPublish(t *testing.T) {
	completion := fsapi.NewCompletion[fsapi.Result]()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const contenders = 32
	var winners atomic.Int32
	var wg sync.WaitGroup
	results := make(chan fsapi.Result, contenders)
	for i := range contenders {
		wg.Go(func() {
			err := completion.Complete(fsapi.Result{Count: int64(i + 1)})
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, fsapi.Exists) {
				t.Errorf("Complete: %v", err)
			}
		})
		wg.Go(func() {
			result, err := completion.Wait(ctx)
			if err != nil {
				t.Errorf("Wait: %v", err)
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	if winners.Load() != 1 {
		t.Fatalf("winning completions = %d", winners.Load())
	}
	var first int64
	for result := range results {
		if first == 0 {
			first = result.Count
		}
		if result.Count != first {
			t.Fatalf("waiters received different results: %d, %d", first, result.Count)
		}
	}
	select {
	case <-completion.Done():
	default:
		t.Fatal("completion was not published")
	}
}

func TestCanceledWaitDoesNotCancelAcceptedMutation(t *testing.T) {
	// Model an operation held behind a service barrier after admission. Canceling
	// the adapter wait must not stop its completion or permit a second result.
	request := fsapi.NewRequest(fsapi.Cred{UID: 1000})
	accepted := make(chan struct{})
	apply := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(accepted)
		<-apply
		finished <- request.Completion().Complete(fsapi.Result{Count: 4})
	}()
	<-accepted
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := request.Completion().Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait: %v", err)
	}
	select {
	case <-request.Completion().Done():
		t.Fatal("wait cancellation completed the operation")
	default:
	}
	close(apply)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	result, err := request.Completion().Wait(ctx)
	if err != nil || result.Count != 4 {
		t.Fatalf("accepted mutation result = %+v, %v", result, err)
	}
	if err := request.Completion().Complete(fsapi.Result{Code: fsapi.Interrupted}); !errors.Is(err, fsapi.Exists) {
		t.Fatalf("second completion: %v", err)
	}
}

func TestRequestAndResultOwnership(t *testing.T) {
	cred := fsapi.Cred{UID: 1000, Groups: []uint32{20, 30}}
	request := fsapi.NewRequest(cred)
	cred.Groups[0] = 99
	snapshot := request.Credentials()
	if snapshot.Groups[0] != 20 {
		t.Fatal("request borrowed supplementary groups")
	}
	snapshot.Groups[0] = 88
	if request.Credentials().Groups[0] != 20 {
		t.Fatal("credentials accessor leaked ownership")
	}
	attr := validAttr()
	result := fsapi.Result{Data: []byte("data"), Attr: &attr}
	if err := request.Completion().Complete(result); err != nil {
		t.Fatal(err)
	}
	result.Data[0] = 'X'
	result.Attr.UID = 99
	first, err := request.Completion().Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Data) != "data" || first.Attr.UID != 1000 {
		t.Fatalf("completion borrowed caller result: %+v", first)
	}
	first.Data[0] = 'Y'
	first.Attr.UID = 88
	second, err := request.Completion().Wait(t.Context())
	if err != nil || string(second.Data) != "data" || second.Attr.UID != 1000 {
		t.Fatalf("waiter mutated retained result: %+v, %v", second, err)
	}
}

func TestCompletionRejectsInvalidResultWithoutConsumingReply(t *testing.T) {
	for _, result := range []fsapi.Result{
		{Code: fsapi.Errno(255)},
		{Count: -1},
		{Code: fsapi.IO, Data: []byte("false success")},
		{Attr: &fsapi.Attr{}},
	} {
		completion := fsapi.NewCompletion[fsapi.Result]()
		if err := completion.Complete(result); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("invalid reply accepted: %+v: %v", result, err)
		}
		if err := completion.Complete(fsapi.Result{Code: fsapi.IO}); err != nil {
			t.Fatalf("invalid reply consumed slot: %v", err)
		}
	}
}
