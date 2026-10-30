package behavior

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// Config names the actual profile. "filesystem" tries every portable case;
// "hfs-direct" covers append and "hfs-buffered" covers mmap. The latter two
// deliberately report their complementary case as skipped, requiring both
// reports for local P01–P04 coverage. Neither claims native/distributed acceptance.
type Config struct {
	Path, Profile, Identity string
	// Capabilities records declarations from an owning adapter. Standalone
	// targets omit this rather than inferring capabilities from a profile name.
	Capabilities map[string]bool
	// Clients contains three idle helpers spawned before mounting. Helpers 1
	// and 2 are intentionally terminated for process-exit/interruption checks.
	Clients []*Client
	// Users optionally contains fixed UID20001/group20002 and UID20003 helpers.
	Users    []*Client
	RootSkip string
	Timeout  time.Duration
	// Lifecycle callbacks are available only to an owning disposable fixture.
	Unmount                    func(context.Context) error
	LockBlocked, LockCancelled func(context.Context) error
}

type run struct {
	c         Config
	report    Report
	directory string
}

// Run exercises only a fresh run-owned directory, bounds every command, stops
// children before normal unmount and preserves data on any failed check.
func Run(ctx context.Context, c Config) Report {
	r := run{c: c}
	r.report = Report{Target: c.Path, Profile: c.Profile, Identity: c.Identity, Capabilities: c.Capabilities, Platform: runtime.GOOS + "/" + runtime.GOARCH, Started: time.Now().UTC(), Scope: "single-node local syscall semantics; no power-loss, replication, failover, arbitrary-client or native FreeBSD acceptance claim"}
	if r.c.Timeout <= 0 {
		r.c.Timeout = 5 * time.Second
	}
	if err := r.validate(); err != nil {
		r.add("setup", "infrastructure", err, 0)
		r.stop(ctx)
		return r.report
	}
	r.directory = fmt.Sprintf("hfs-behavior-%x", rand.Text()[:12])
	r.report.RunDirectory = filepath.Join(c.Path, r.directory)
	r.check(ctx, "bind", "infrastructure", func(ctx context.Context) error { return r.bind(ctx) })
	if !r.report.Failed() {
		r.cases(ctx)
	}
	if !r.report.Failed() && r.stopPeers(ctx) {
		r.check(ctx, "close-cleanup-lock", "infrastructure", func(ctx context.Context) error { return r.call(ctx, 0, "close-lock") })
		if !r.report.Failed() {
			r.check(ctx, "cleanup-data", "infrastructure", func(ctx context.Context) error { return r.call(ctx, 0, "cleanup") })
		}
	}
	stopped := r.stop(ctx)
	if stopped && r.c.Unmount != nil {
		unmounted := r.check(ctx, "normal-unmount", "lifecycle", r.c.Unmount)
		if unmounted && r.c.Identity != "" {
			r.check(ctx, "mount-loss-no-fallback", "lifecycle", r.mountLoss)
		}
	} else if r.c.Unmount == nil {
		r.skip("normal-unmount", "lifecycle", "standalone target: runner does not own its mount lifecycle")
		r.skip("mount-loss-no-fallback", "lifecycle", "requires an owned HomelabFS mount and exact pre-unmount identity")
	}
	return r.report
}

func (r *run) validate() error {
	if len(r.c.Clients) != 3 || (len(r.c.Users) != 0 && len(r.c.Users) != 2) {
		return fmt.Errorf("three helpers and zero or two fixed-credential users required")
	}
	switch r.c.Profile {
	case "filesystem":
		if r.c.Identity != "" {
			return fmt.Errorf("HomelabFS identity requires an explicit HomelabFS profile")
		}
	case "hfs-direct", "hfs-buffered":
		if r.c.Identity == "" {
			return fmt.Errorf("HomelabFS profile requires exact volume/generation/session identity")
		}
	default:
		return fmt.Errorf("unknown profile %q", r.c.Profile)
	}
	if !filepath.IsAbs(r.c.Path) {
		return fmt.Errorf("target must be absolute")
	}
	return nil
}

func (r *run) bind(ctx context.Context) error {
	for i, client := range append(append([]*Client{}, r.c.Clients...), r.c.Users...) {
		reply, err := client.Call(ctx, Command{Op: "bind", Path: r.c.Path, Run: r.directory, Identity: r.c.Identity, Create: i == 0})
		if err != nil {
			return err
		}
		if reply.Error != "" {
			return fmt.Errorf("bind helper %d: %s", i, reply.Error)
		}
	}
	return nil
}

func (r *run) cases(ctx context.Context) {
	for _, c := range []struct{ name, family, op string }{{"create-exclusive-rename-replace-hardlink-unlink-open-truncate", "P01", "namespace"}, {"umask-chmod-chown-setgid", "P02", "permissions"}, {"sync-open-fsync-fdatasync-directory-sync", "P04", "sync"}} {
		r.check(ctx, c.name, c.family, func(ctx context.Context) error { return r.call(ctx, 0, c.op) })
	}
	if r.c.Profile == "hfs-buffered" {
		r.skip("concurrent-append", "P01", "buffered profile rejects O_APPEND before effects; required append runs separately on hfs-direct")
	} else {
		r.check(ctx, "concurrent-append", "P01", r.append)
	}
	if r.c.Profile == "hfs-direct" {
		r.skip("mmap-msync-readback", "P04", "direct profile rejects mmap; required mmap runs separately on hfs-buffered")
	} else {
		r.check(ctx, "mmap-msync-readback", "P04", func(ctx context.Context) error { return r.call(ctx, 0, "mmap") })
	}
	if len(r.c.Users) == 0 {
		reason := r.c.RootSkip
		if reason == "" {
			reason = "requires root and two explicitly spawned fixed-credential users"
		}
		r.skip("multiuser-groups-sticky-permission-cache", "P02", reason)
	} else {
		r.check(ctx, "multiuser-groups-sticky-permission-cache", "P02", r.users)
	}
	r.check(ctx, "process-fcntl-flock-contention-exit", "P03", r.locks)
}

func (r *run) append(ctx context.Context) error {
	for i, marker := range []string{"A", "B"} {
		if err := r.c.Clients[i].Send(ctx, Command{Op: "append", Data: marker}); err != nil {
			return fmt.Errorf("append helper %d marker %s send: %w", i, marker, err)
		}
	}
	for i := range 2 {
		reply, err := r.c.Clients[i].Receive(ctx)
		if err != nil {
			return fmt.Errorf("append helper %d receive: %w", i, err)
		}
		if reply.Error != "" {
			return fmt.Errorf("append helper %d: %s", i, reply.Error)
		}
	}
	if err := r.call(ctx, 0, "verify-append"); err != nil {
		return fmt.Errorf("verify-append helper 0: %w", err)
	}
	return nil
}

func (r *run) locks(ctx context.Context) error {
	for i := range 3 {
		if err := r.lockStep(ctx, "open-descriptors", i, "open-lock"); err != nil {
			return err
		}
	}
	if err := r.lockStep(ctx, "initial-acquisition", 0, "flock-try"); err != nil {
		return err
	}
	if err := r.lockConflict(ctx, "initial-contention", 1, "flock-try"); err != nil {
		return err
	}
	if err := r.lockStep(ctx, "release-initial-flock", 0, "flock-unlock"); err != nil {
		return err
	}
	if err := r.lockStep(ctx, "initial-acquisition", 0, "fcntl-try"); err != nil {
		return err
	}
	if err := r.lockConflict(ctx, "initial-contention", 1, "fcntl-try"); err != nil {
		return err
	}
	r.check(ctx, "blocked-lock-process-interruption", "lifecycle", r.interrupt)
	if err := r.lockStep(ctx, "release-after-interruption", 0, "fcntl-unlock"); err != nil {
		return err
	}
	if err := r.lockStep(ctx, "acquire-after-unlock", 1, "fcntl-try"); err != nil {
		return err
	}
	if err := r.lockStep(ctx, "acquire-before-exit", 1, "flock-try"); err != nil {
		return err
	}
	if err := r.lockConflict(ctx, "contention-before-exit", 0, "fcntl-try"); err != nil {
		return err
	}
	if err := r.lockConflict(ctx, "contention-before-exit", 0, "flock-try"); err != nil {
		return err
	}
	if err := r.c.Clients[1].Stop(ctx); err != nil {
		return fmt.Errorf("exit-holder helper 1 stop: %w", err)
	}
	if err := r.lockStep(ctx, "acquire-after-exit", 0, "fcntl-try"); err != nil {
		return err
	}
	return r.lockStep(ctx, "acquire-after-exit", 0, "flock-try")
}

func (r *run) interrupt(ctx context.Context) error {
	if err := r.c.Clients[2].Send(ctx, Command{Op: "fcntl-wait"}); err != nil {
		return fmt.Errorf("interrupt-waiter helper 2 fcntl-wait send: %w", err)
	}
	if r.c.LockBlocked != nil {
		if err := r.c.LockBlocked(ctx); err != nil {
			return fmt.Errorf("interrupt-waiter helper 2 wait-admission: %w", err)
		}
	} else {
		// Native ordinary targets supply no daemon admission hook. A helper
		// blocked for this interval must not return while the conflicting lock
		// remains held. This is scheduling evidence, not a FUSE cancel oracle.
		short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		reply, err := r.c.Clients[2].Receive(short)
		if err == nil {
			return fmt.Errorf("interrupt-waiter helper 2 fcntl-wait returned under contention: %+v", reply)
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("interrupt-waiter helper 2 fcntl-wait receive: %w", err)
		}
	}
	if err := r.c.Clients[2].Stop(ctx); err != nil {
		return fmt.Errorf("interrupt-waiter helper 2 stop: %w", err)
	}
	if r.c.LockCancelled != nil {
		if err := r.c.LockCancelled(ctx); err != nil {
			return fmt.Errorf("interrupt-waiter helper 2 cancel-admission: %w", err)
		}
	}
	return r.lockConflict(ctx, "contention-after-interruption", 1, "fcntl-try")
}

func (r *run) conflict(ctx context.Context, i int, op string) error {
	reply, err := r.c.Clients[i].Call(ctx, Command{Op: op})
	if err != nil {
		return err
	}
	if reply.Errno != int(unix.EAGAIN) && reply.Errno != int(unix.EACCES) {
		return fmt.Errorf("%s conflict returned %+v", op, reply)
	}
	return nil
}

func (r *run) users(ctx context.Context) error {
	if err := r.call(ctx, 0, "prepare-users"); err != nil {
		return err
	}
	for _, c := range []struct {
		client *Client
		op     string
	}{{r.c.Users[0], "user-member"}, {r.c.Users[1], "user-outsider"}, {r.c.Clients[0], "publicize"}, {r.c.Users[1], "user-public"}} {
		reply, err := c.client.Call(ctx, Command{Op: c.op})
		if err != nil {
			return err
		}
		if reply.Error != "" {
			return fmt.Errorf("%s: %s", c.op, reply.Error)
		}
	}
	return nil
}

func (r *run) call(ctx context.Context, i int, op string) error {
	reply, err := r.c.Clients[i].Call(ctx, Command{Op: op})
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s: %s", op, reply.Error)
	}
	return nil
}

func (r *run) stop(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var errs []error
	for _, client := range append(append([]*Client{}, r.c.Clients...), r.c.Users...) {
		errs = append(errs, client.Stop(ctx))
	}
	err := errors.Join(errs...)
	r.add("stop-owned-helpers", "infrastructure", err, 0)
	return err == nil
}

func (r *run) mountLoss(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := openRoot(r.c.Path, r.c.Identity)
	if err == nil {
		return errors.Join(fmt.Errorf("unmounted fallback accepted"), root.Close())
	}
	entries, err := os.ReadDir(r.c.Path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("unmounted fallback has %d unexpected entries", len(entries))
	}
	return nil
}

func (r *run) stopPeers(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var errs []error
	for _, client := range append(append([]*Client{}, r.c.Clients[1:]...), r.c.Users...) {
		errs = append(errs, client.Stop(ctx))
	}
	err := errors.Join(errs...)
	r.add("stop-mutating-peers", "infrastructure", err, 0)
	return err == nil
}

func (r *run) check(ctx context.Context, name, family string, f func(context.Context) error) bool {
	bounded, cancel := context.WithTimeout(ctx, r.c.Timeout)
	defer cancel()
	start := time.Now()
	err := f(bounded)
	r.add(name, family, err, time.Since(start).Seconds())
	return err == nil
}

func (r *run) add(name, family string, err error, seconds float64) {
	c := Result{Name: name, Family: family, Status: "pass", Seconds: seconds}
	if err != nil {
		c.Status = "fail"
		c.Detail = err.Error()
	}
	r.report.Results = append(r.report.Results, c)
}

func (r *run) skip(name, family, reason string) {
	r.report.Results = append(r.report.Results, Result{Name: name, Family: family, Status: "skip", Detail: reason})
}
