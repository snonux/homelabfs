package behavior

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process owns a helper's lifetime. Wait must be the sole reaper.
type Process interface {
	Signal(os.Signal) error
	Wait() error
}

// Spawn starts an idle, trusted fixed-credential helper before any mount.
// Implementations must not exec or mutate credentials after helper admission.
type Spawn func(context.Context, io.Reader, io.Writer, *syscall.Credential) (Process, error)

// Command is the private bounded helper protocol, not a daemon control API.
type Command struct {
	Op                  string
	Path, Run, Identity string
	Name, Data          string
	Create              bool
}

// Reply records both a diagnostic and the native errno when available.
type Reply struct {
	Error string
	Errno int
	Data  string
}

type commandProcess struct{ command *exec.Cmd }

// Client owns one pipe-connected helper. Calls must be sequential per client.
type Client struct {
	process  Process
	input    *os.File
	output   *os.File
	decoder  *json.Decoder
	once     sync.Once
	closeErr error
	broken   error
}

// Start creates OS pipes (no exec copier goroutines) and starts an idle helper.
func Start(ctx context.Context, spawn Spawn, credential *syscall.Credential) (*Client, error) {
	in, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	read, out, err := os.Pipe()
	if err != nil {
		_ = in.Close()
		_ = write.Close()
		return nil, err
	}
	p, err := spawn(ctx, in, out, credential)
	_ = in.Close()
	_ = out.Close()
	if err != nil {
		_ = write.Close()
		_ = read.Close()
		return nil, err
	}
	return &Client{process: p, input: write, output: read, decoder: json.NewDecoder(read)}, nil
}

// ExecutableSpawner runs the given executable with its trusted helper arguments.
func ExecutableSpawner(executable string, args ...string) Spawn {
	return func(ctx context.Context, in io.Reader, out io.Writer, credential *syscall.Credential) (Process, error) {
		c := exec.CommandContext(ctx, executable, args...)
		c.Stdin, c.Stdout, c.Stderr = in, out, os.Stderr
		if credential != nil {
			c.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
		}
		if err := c.Start(); err != nil {
			return nil, err
		}
		return &commandProcess{c}, nil
	}
}

func (p *commandProcess) Signal(s os.Signal) error { return p.command.Process.Signal(s) }
func (p *commandProcess) Wait() error              { return p.command.Wait() }

// Call sends a command and waits only until its bounded context expires.
func (c *Client) Call(ctx context.Context, command Command) (Reply, error) {
	if err := c.Send(ctx, command); err != nil {
		return Reply{}, err
	}
	return c.Receive(ctx)
}

// Send starts an operation; Receive obtains its completion separately.
func (c *Client) Send(ctx context.Context, command Command) error {
	if c.broken != nil {
		return c.broken
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.input.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(c.input).Encode(command); err != nil {
		c.broken = err
		return err
	}
	return nil
}

// Receive waits for the next helper reply with a pipe read deadline.
func (c *Client) Receive(ctx context.Context) (Reply, error) {
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.output.SetReadDeadline(deadline); err != nil {
			return Reply{}, err
		}
	}
	var reply Reply
	if err := c.decoder.Decode(&reply); err != nil {
		c.broken = fmt.Errorf("helper response: %w", err)
		return reply, c.broken
	}
	return reply, nil
}

// Stop terminates a helper, closes its pipes and reaps exactly once. Expected
// signal exit is accepted; helpers are always stopped before normal unmount.
func (c *Client) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c.once.Do(func() {
		_ = c.input.Close()
		signalErr := c.process.Signal(syscall.SIGTERM)
		if errors.Is(signalErr, os.ErrProcessDone) {
			signalErr = nil
		}
		waited := make(chan error, 1)
		go func() { waited <- c.process.Wait() }()
		var waitErr error
		select {
		case waitErr = <-waited:
		case <-ctx.Done():
			waitErr = fmt.Errorf("helper stop: %w", ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() && status.Signal() == syscall.SIGTERM {
				waitErr = nil
			}
		}
		c.closeErr = errors.Join(signalErr, waitErr, c.output.Close())
	})
	return c.closeErr
}
