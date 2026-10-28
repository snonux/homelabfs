package failpoint_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/failpoint"
)

const waitLimit = 3 * time.Second

func TestDisabled(t *testing.T) {
	var zero failpoint.Registry
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range []*failpoint.Registry{&zero, failpoint.New()} {
		for _, name := range []failpoint.Name{failpoint.Apply, "unknown"} {
			if err := r.Hit(ctx, name); err != nil {
				t.Fatalf("disabled Hit: %v", err)
			}
		}
		if got := testing.AllocsPerRun(100, func() { _ = r.Hit(ctx, failpoint.Apply) }); got != 0 {
			t.Errorf("disabled Hit allocated %g times", got)
		}
	}
}

func TestStandardNames(t *testing.T) {
	want := []failpoint.Name{
		"operation.admission", "journal.append", "local.sync_completion", "partner.receipt",
		"partner.ack_transmit", "commit.visibility_decision", "apply", "client.reply",
		"epoch.persistence", "checkpoint.publication", "journal.trim",
	}
	names := failpoint.StandardNames()
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("standard names = %v, want %v", names, want)
	}
	r, c := failpoint.NewForTesting()
	t.Cleanup(c.DisableAll)
	injected := errors.New("injected")
	for _, name := range names {
		a := enable(t, c, name, failpoint.Rule{Action: failpoint.ReturnError, Err: injected})
		if err := r.Hit(t.Context(), name); !errors.Is(err, injected) {
			t.Errorf("%s: Hit = %v", name, err)
		}
		if a.Hits() != 1 {
			t.Errorf("%s: hits = %d", name, a.Hits())
		}
	}
	names[0] = "modified"
	if failpoint.StandardNames()[0] != failpoint.OperationAdmission {
		t.Fatal("StandardNames exposes mutable shared state")
	}
	if err := r.Hit(t.Context(), "unknown"); err != nil {
		t.Fatalf("unknown Hit: %v", err)
	}
}

func TestRuleValidation(t *testing.T) {
	injected := errors.New("existing rule")
	cases := []failpoint.Rule{
		{}, {Action: 255}, {Action: failpoint.ReturnError},
		{Action: failpoint.ReturnError, Err: injected, Duration: time.Second},
		{Action: failpoint.ReturnError, Err: injected, ExitCode: 1},
		{Action: failpoint.Crash}, {Action: failpoint.Crash, ExitCode: -1},
		{Action: failpoint.Crash, ExitCode: 256},
		{Action: failpoint.Crash, ExitCode: 1, Err: injected},
		{Action: failpoint.Crash, ExitCode: 1, Duration: time.Second},
		{Action: failpoint.Sleep}, {Action: failpoint.Sleep, Duration: -1},
		{Action: failpoint.Sleep, Duration: time.Second, Err: injected},
		{Action: failpoint.Sleep, Duration: time.Second, ExitCode: 1},
		{Action: failpoint.Block, Err: injected},
		{Action: failpoint.Block, ExitCode: 1},
		{Action: failpoint.Block, Duration: time.Second},
	}
	for i, rule := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r, c := failpoint.NewForTesting()
			a := enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.ReturnError, Err: injected})
			if _, err := c.Enable(failpoint.Apply, rule); !errors.Is(err, failpoint.ErrInvalidRule) {
				t.Fatalf("Enable = %v, want ErrInvalidRule", err)
			}
			if err := r.Hit(t.Context(), failpoint.Apply); !errors.Is(err, injected) || a.Hits() != 1 {
				t.Fatalf("invalid replacement changed existing rule: err=%v, hits=%d", err, a.Hits())
			}
		})
	}
	r, c := failpoint.NewForTesting()
	if _, err := c.Enable("typo", failpoint.Rule{Action: failpoint.Block}); !errors.Is(err, failpoint.ErrUnknownName) {
		t.Fatalf("Enable unknown = %v", err)
	}
	if err := c.Disable("typo"); !errors.Is(err, failpoint.ErrUnknownName) {
		t.Fatalf("Disable unknown = %v", err)
	}
	if err := r.Hit(t.Context(), failpoint.Apply); err != nil {
		t.Fatal(err)
	}
}

func TestNthHitAndReset(t *testing.T) {
	injected := errors.New("injected")
	for _, nth := range []uint64{0, 1, 3, 6} {
		t.Run(fmt.Sprint(nth), func(t *testing.T) {
			r, c := failpoint.NewForTesting()
			a := enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.ReturnError, Err: injected, Nth: nth})
			for hit := uint64(1); hit <= 5; hit++ {
				err := r.Hit(t.Context(), failpoint.Apply)
				wantError := nth == 0 || hit == nth
				if errors.Is(err, injected) != wantError {
					t.Fatalf("hit %d = %v, want injected error %t", hit, err, wantError)
				}
			}
			if a.Hits() != 5 {
				t.Fatalf("hits = %d", a.Hits())
			}
			fresh := enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.ReturnError, Err: injected, Nth: 1})
			if fresh.Hits() != 0 || a.Hits() != 5 {
				t.Fatal("replacement did not isolate counts")
			}
			if err := r.Hit(t.Context(), failpoint.Apply); !errors.Is(err, injected) {
				t.Fatalf("replacement first hit = %v", err)
			}
		})
	}
}

func TestSleepDuration(t *testing.T) {
	r, c := failpoint.NewForTesting()
	const duration = 10 * time.Millisecond
	enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.Sleep, Duration: duration})
	ctx, cancel := context.WithTimeout(t.Context(), waitLimit)
	defer cancel()
	start := time.Now()
	if err := r.Hit(ctx, failpoint.Apply); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < duration {
		t.Fatalf("sleep returned early: %s", elapsed)
	}
}

func TestWaitActions(t *testing.T) {
	for _, action := range []failpoint.Action{failpoint.Block, failpoint.Sleep} {
		for _, ending := range []string{"release", "cancel", "disable", "disable-all", "replace"} {
			t.Run(fmt.Sprintf("%d/%s", action, ending), func(t *testing.T) {
				testWaitAction(t, action, ending)
			})
		}
	}
}

func TestCanceledHit(t *testing.T) {
	for _, rule := range []failpoint.Rule{
		{Action: failpoint.ReturnError, Err: errors.New("must not return")},
		{Action: failpoint.Block}, {Action: failpoint.Sleep, Duration: time.Hour},
	} {
		t.Run(fmt.Sprint(rule.Action), func(t *testing.T) {
			r, c := failpoint.NewForTesting()
			a := enable(t, c, failpoint.Apply, rule)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := r.Hit(ctx, failpoint.Apply); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled hit = %v", err)
			}
			if a.Hits() != 1 {
				t.Fatalf("canceled hit count = %d", a.Hits())
			}
			select {
			case <-a.Triggered():
				t.Fatal("canceled hit triggered action")
			default:
			}
		})
	}
}

func TestContextDeadline(t *testing.T) {
	for _, rule := range []failpoint.Rule{{Action: failpoint.Block}, {Action: failpoint.Sleep, Duration: time.Hour}} {
		t.Run(fmt.Sprint(rule.Action), func(t *testing.T) {
			r, c := failpoint.NewForTesting()
			enable(t, c, failpoint.Apply, rule)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			if err := r.Hit(ctx, failpoint.Apply); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline hit = %v", err)
			}
		})
	}
}

func TestDisableIsolation(t *testing.T) {
	r, c := failpoint.NewForTesting()
	injected := errors.New("injected")
	for _, name := range []failpoint.Name{failpoint.Apply, failpoint.ClientReply} {
		enable(t, c, name, failpoint.Rule{Action: failpoint.ReturnError, Err: injected})
	}
	for range 2 {
		if err := c.Disable(failpoint.Apply); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Hit(t.Context(), failpoint.Apply); err != nil {
		t.Fatal(err)
	}
	if err := r.Hit(t.Context(), failpoint.ClientReply); !errors.Is(err, injected) {
		t.Fatalf("disabling another name changed client reply: %v", err)
	}
	c.DisableAll()
	c.DisableAll()
	if err := r.Hit(t.Context(), failpoint.ClientReply); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentNthHit(t *testing.T) {
	r, c := failpoint.NewForTesting()
	injected := errors.New("one selected arrival")
	a := enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.ReturnError, Err: injected, Nth: 57})
	var fired atomic.Uint64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		wg.Go(func() {
			<-start
			for range 32 {
				if err := r.Hit(t.Context(), failpoint.Apply); errors.Is(err, injected) {
					fired.Add(1)
				} else if err != nil {
					t.Errorf("Hit: %v", err)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	if fired.Load() != 1 || a.Hits() != 512 {
		t.Fatalf("fired=%d hits=%d, want 1 and 512", fired.Load(), a.Hits())
	}
}

func TestConcurrentReconfiguration(t *testing.T) {
	r, c := failpoint.NewForTesting()
	ctx, cancel := context.WithTimeout(t.Context(), waitLimit)
	defer cancel()
	injected := errors.New("injected")
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 200 {
				if err := r.Hit(ctx, failpoint.Apply); err != nil && !errors.Is(err, injected) {
					t.Errorf("Hit: %v", err)
				}
			}
		})
		wg.Go(func() {
			for range 100 {
				a, err := c.Enable(failpoint.Apply, failpoint.Rule{Action: failpoint.ReturnError, Err: injected, Nth: 2})
				if err != nil {
					t.Errorf("Enable: %v", err)
					return
				}
				_ = a.Hits()
				a.Release()
				if err := c.Disable(failpoint.Apply); err != nil {
					t.Errorf("Disable: %v", err)
				}
				c.DisableAll()
			}
		})
	}
	wg.Wait()
	c.DisableAll()
	if err := r.Hit(ctx, failpoint.Apply); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentBlockRelease(t *testing.T) {
	r, c := failpoint.NewForTesting()
	a := enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.Block})
	ctx, cancel := context.WithTimeout(t.Context(), waitLimit)
	defer cancel()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := r.Hit(ctx, failpoint.Apply); err != nil {
				t.Errorf("Hit: %v", err)
			}
		})
	}
	awaitTriggered(t, a)
	for range 8 {
		wg.Go(a.Release)
	}
	wg.Wait()
	if a.Hits() != 16 {
		t.Fatalf("hits=%d, want 16", a.Hits())
	}
}

func TestCrashProcess(t *testing.T) {
	if os.Getenv("HOMELABFS_FAILPOINT_CRASH_HELPER") == "1" {
		r, c := failpoint.NewForTesting()
		enable(t, c, failpoint.ClientReply, failpoint.Rule{Action: failpoint.Crash, ExitCode: 73})
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if err := r.Hit(canceled, failpoint.ClientReply); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled crash hit = %v", err)
		}
		enable(t, c, failpoint.ClientReply, failpoint.Rule{Action: failpoint.Crash, ExitCode: 73, Nth: 2})
		defer func() {
			if _, err := fmt.Fprintln(os.Stdout, "deferred cleanup ran"); err != nil {
				t.Error(err)
			}
		}()
		if err := r.Hit(t.Context(), failpoint.ClientReply); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(os.Stdout, "first hit returned"); err != nil {
			t.Fatal(err)
		}
		_ = r.Hit(t.Context(), failpoint.ClientReply)
		t.Fatal("crash action returned")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCrashProcess$")
	cmd.Env = append(os.Environ(), "HOMELABFS_FAILPOINT_CRASH_HELPER=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("crash subprocess exceeded deadline: %v", ctx.Err())
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("crash subprocess: err=%v, output=%q", err, output)
	}
	if strings.TrimSpace(string(output)) != "first hit returned" {
		t.Fatalf("unexpected crash output (cleanup must not run): %q", output)
	}
}

func enable(t *testing.T, c *failpoint.Control, name failpoint.Name, rule failpoint.Rule) *failpoint.Activation {
	t.Helper()
	a, err := c.Enable(name, rule)
	if err != nil {
		t.Fatalf("Enable %s: %v", name, err)
	}
	t.Cleanup(c.DisableAll)
	return a
}

func awaitTriggered(t *testing.T, a *failpoint.Activation) {
	t.Helper()
	select {
	case <-a.Triggered():
	case <-time.After(waitLimit):
		t.Fatal("action did not trigger before deadline")
	}
}

func testWaitAction(t *testing.T, action failpoint.Action, ending string) {
	t.Helper()
	r, c := failpoint.NewForTesting()
	rule := failpoint.Rule{Action: action}
	if action == failpoint.Sleep {
		rule.Duration = time.Hour
	}
	a := enable(t, c, failpoint.Apply, rule)
	ctx, cancel := context.WithTimeout(t.Context(), waitLimit)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- r.Hit(ctx, failpoint.Apply) }()
	awaitTriggered(t, a)
	select {
	case err := <-result:
		t.Fatalf("waiting action returned before release/cancel: %v", err)
	default:
	}
	var want error
	switch ending {
	case "release":
		a.Release()
		a.Release()
	case "cancel":
		cancel()
		want = context.Canceled
	case "disable":
		if err := c.Disable(failpoint.Apply); err != nil {
			t.Fatal(err)
		}
	case "disable-all":
		c.DisableAll()
	case "replace":
		fresh := enable(t, c, failpoint.Apply, failpoint.Rule{Action: failpoint.Block})
		a.Release() // An old handle must not release the fresh activation.
		select {
		case <-fresh.Triggered():
			t.Fatal("fresh activation unexpectedly triggered")
		default:
		}
		freshCtx, freshCancel := context.WithCancel(ctx)
		freshResult := make(chan error, 1)
		go func() { freshResult <- r.Hit(freshCtx, failpoint.Apply) }()
		awaitTriggered(t, fresh)
		select {
		case err := <-freshResult:
			t.Fatalf("old handle released fresh activation: %v", err)
		default:
		}
		freshCancel()
		if err := <-freshResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("fresh action cancellation: %v", err)
		}
	}
	select {
	case got := <-result:
		if !errors.Is(got, want) {
			t.Fatalf("wait result = %v, want %v", got, want)
		}
	case <-time.After(waitLimit):
		t.Fatal("waiting action did not return")
	}
}
