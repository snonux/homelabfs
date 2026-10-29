package transport

import (
	"context"
	"time"
)

// Timer is a stoppable deadline notification supplied by Clock.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Clock drives every transport timeout, including handshake contexts, socket
// deadlines, dispatch handoffs and reconnect backoff. Nil Config.Clock uses real
// time. Simulation implementations must also drive their Network deadlines.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}
type wallClock struct{}
type wallTimer struct{ *time.Timer }

func (wallClock) Now() time.Time                 { return time.Now() }
func (wallClock) NewTimer(d time.Duration) Timer { return wallTimer{time.NewTimer(d)} }
func (wallClock) WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
func (t wallTimer) C() <-chan time.Time { return t.Timer.C }
