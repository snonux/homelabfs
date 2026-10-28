package fsapi

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Reply is the closed set of fsapi-owned completion values. Its private methods
// ensure every supported result validates and snapshots its mutable buffers.
type Reply[T any] interface {
	validate() error
	clone() T
}

// Completion is a single-assignment asynchronous reply. It must not be copied.
// Construct it with NewCompletion; no goroutine or callback is started here.
// Successful Complete publishes one owned result to every current/future waiter.
// The service must Complete every accepted request, including abandoned waits.
type Completion[T Reply[T]] struct {
	mu       sync.Mutex
	done     chan struct{}
	finished bool
	result   T
}

// Result is one ordinary filesystem reply. Fields have operation-specific
// meanings; Data may be a short read, Count a short write, Attr fresh metadata,
// and Handle a service-owned open reference. Directory/namespace methods can
// define their own typed result alongside this contract as they are implemented.
// A non-OK Code carries no success payload and never proves mutation rollback.
type Result struct {
	Code   Errno
	Data   []byte
	Count  int64
	Attr   *Attr
	Handle OpenHandle
}

// Request owns caller credentials and completion state for one adapter request.
// Operation methods accept their own typed arguments; they must snapshot mutable
// arguments before returning from dispatch. Request never borrows kernel memory.
// An adapter request identity is not a durable transaction idempotency identity.
type Request struct {
	credentials Cred
	completion  *Completion[Result]
}

// NewCompletion constructs an unfinished completion.
func NewCompletion[T Reply[T]]() *Completion[T] {
	return &Completion[T]{done: make(chan struct{})}
}

// NewRequest captures caller credentials for asynchronous service dispatch.
func NewRequest(credentials Cred) *Request {
	return &Request{credentials: credentials.Clone(), completion: NewCompletion[Result]()}
}

// Complete validates and snapshots result, then publishes it exactly once.
// Invalid results and subsequent completions return an error without changing
// the accepted result. The caller must not mutate result concurrently with this
// call; it may reuse all its buffers after Complete returns.
func (c *Completion[T]) Complete(result T) error {
	if err := result.validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done == nil {
		return fmt.Errorf("%w: use NewCompletion", Invalid)
	}
	if c.finished {
		return fmt.Errorf("%w: request already completed", Exists)
	}
	c.result = result.clone()
	c.finished = true
	close(c.done)
	return nil
}

// Wait waits only for this reply. Context cancellation never completes the
// request and cannot roll back admitted work. If completion is already visible,
// it wins over cancellation. Each successful wait receives an independent copy.
func (c *Completion[T]) Wait(ctx context.Context) (T, error) {
	var zero T
	select {
	case <-c.done:
		return c.snapshot(), nil
	default:
	}
	select {
	case <-c.done:
		return c.snapshot(), nil
	case <-ctx.Done():
		select {
		case <-c.done:
			return c.snapshot(), nil
		default:
			return zero, ctx.Err()
		}
	}
}

// Done closes when the reply has been published. Cancellation does not close it.
func (c *Completion[T]) Done() <-chan struct{} { return c.done }

// Credentials returns an independent snapshot of the caller credentials.
func (r *Request) Credentials() Cred { return r.credentials.Clone() }

// Completion returns this request's single reply slot.
func (r *Request) Completion() *Completion[Result] { return r.completion }

func (c *Completion[T]) snapshot() T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result.clone()
}

func (r Result) validate() error {
	if !r.Code.Valid() || r.Count < 0 {
		return fmt.Errorf("%w: filesystem result", Invalid)
	}
	if r.Code != OK && (len(r.Data) != 0 || r.Count != 0 || r.Attr != nil || r.Handle != 0) {
		return fmt.Errorf("%w: error with success payload", Invalid)
	}
	if r.Attr != nil {
		return r.Attr.Validate()
	}
	return nil
}

func (r Result) clone() Result {
	r.Data = slices.Clone(r.Data)
	if r.Attr != nil {
		a := *r.Attr
		r.Attr = &a
	}
	return r
}
