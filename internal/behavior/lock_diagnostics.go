package behavior

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
)

// lockStep preserves a failed acquisition and asks the same process for one
// read-only holder snapshot only after unexpected fcntl contention. The snapshot
// is later than the failure: an unlocked result does not disprove a prior lock
// conflict or distinguish that conflict from adapter admission exhaustion.
func (r *run) lockStep(ctx context.Context, stage string, i int, op string) error {
	reply, err := r.c.Clients[i].Call(ctx, Command{Op: op})
	if err != nil {
		return fmt.Errorf("%s helper %d %s: %w", stage, i, op, err)
	}
	if reply.Error == "" {
		return nil
	}
	err = fmt.Errorf("%s helper %d %s: %s (errno %d)", stage, i, op, reply.Error, reply.Errno)
	if op != "fcntl-try" || (reply.Errno != int(unix.EAGAIN) && reply.Errno != int(unix.EACCES)) {
		return err
	}
	query, queryErr := r.c.Clients[i].Call(ctx, Command{Op: "fcntl-query"})
	if queryErr != nil {
		return fmt.Errorf("%w; F_GETLK diagnostic unavailable: %v", err, queryErr)
	}
	if query.Error != "" {
		return fmt.Errorf("%w; F_GETLK diagnostic failed: %s (errno %d)", err, query.Error, query.Errno)
	}
	return fmt.Errorf("%w; later F_GETLK snapshot: %s", err, query.Data)
}

func (r *run) lockConflict(ctx context.Context, stage string, i int, op string) error {
	if err := r.conflict(ctx, i, op); err != nil {
		return fmt.Errorf("%s helper %d %s: %w", stage, i, op, err)
	}
	return nil
}
