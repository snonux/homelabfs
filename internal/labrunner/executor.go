package labrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ProcessExecutor runs exact argv without shell evaluation. A driver must own
// and reap its children, enforce plan limits, and use the exit-code contract:
// 10 assertion, 20 infrastructure, 30 timeout, 40 cleanup, 50 restoration.
// Any other nonzero result is uncertain, requiring operator reconciliation.
type ProcessExecutor struct{}

type boundedLog struct {
	file      *os.File
	remaining int64
}

var _ Executor = ProcessExecutor{}

// Execute checks bytes immediately before invocation. VM transport failures
// are always uncertain because the remote action can outlive its SSH client.
func (ProcessExecutor) Execute(ctx context.Context, p Plan, s Step, logPath string) error {
	if err := p.VerifyArtifacts(); err != nil {
		return &Error{Category: "infrastructure", Err: err}
	}
	var artifact Artifact
	for _, a := range p.Artifacts {
		if a.ID == s.Artifact {
			artifact = a
		}
	}
	args := append([]string(nil), s.Args...)
	if s.Action != "driver" {
		args = []string{"--approved-action", s.Action}
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return &Error{Category: "infrastructure", Err: err}
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	limit := p.Budget.ArtifactBytes/int64(len(p.Steps)+2) - info.Size()
	if limit <= 0 {
		return &Error{Category: "infrastructure", Err: errors.New("step evidence byte limit exhausted")}
	}
	w := &boundedLog{file: f, remaining: limit}
	cmd := exec.CommandContext(ctx, artifact.Path, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Stdin = nil
	cmd.Dir = p.StateDir
	cmd.WaitDelay = 2 * time.Second
	// Credentials and ambient environment are not injected into the plan. HOME
	// permits the operator's normal pinned SSH configuration; PATH is fixed.
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/usr/local/sbin", "HOME=" + os.Getenv("HOME"), "HFS_LAB_PLAN_HASH=" + mustHash(p), "HFS_LAB_RUN_ID=" + p.RunID, "HFS_LAB_PLAN=" + filepath.Join(p.StateDir, "plan.json")}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		cmd.Env = append(cmd.Env, "SSH_AUTH_SOCK="+sock)
	}
	err = cmd.Run()
	syncErr := f.Sync()
	if err == nil {
		return syncErr
	}
	failure := &Error{Category: "infrastructure", Uncertain: true, Err: err}
	if cmd.Process == nil {
		failure.Uncertain = false
	}
	if ctx.Err() != nil {
		failure.Category = "timeout"
		failure.Err = fmt.Errorf("step deadline/cancellation: %w", ctx.Err())
		return failure
	}
	var exit *exec.ExitError
	if s.Action == "driver" && errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 10:
			failure.Category = "assertion"
			failure.Uncertain = false
		case 20:
			failure.Category = "infrastructure"
			failure.Uncertain = false
		case 30:
			failure.Category = "timeout"
			failure.Uncertain = false
		case 40:
			failure.Category = "cleanup"
			failure.Uncertain = false
		case 50:
			failure.Category = "restoration"
			failure.Uncertain = false
		}
	}
	return errors.Join(failure, syncErr)
}

func (w *boundedLog) Write(b []byte) (int, error) {
	n := len(b)
	overflow := int64(n) > w.remaining
	if overflow {
		n = int(w.remaining)
	}
	written, err := w.file.Write(b[:n])
	w.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	if overflow {
		return written, errors.New("approved evidence byte limit exceeded")
	}
	return written, nil
}

func mustHash(p Plan) string { h, _ := p.Hash(); return h }

var _ io.Writer = (*boundedLog)(nil)
