//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	exporterLogBytes       = 256 << 10
	exporterLogStatusBytes = 4096
	exporterLogBufferBytes = 32 << 10
	// One MiB of the existing 16 MiB exporter overhead is reserved for two
	// drain workers, buffers, bounded status encoding and descriptor owners.
	exporterDiagnosticMemoryBytes       = 1 << 20
	exporterDiagnosticDiskBytes         = 2*exporterLogBytes + exporterLogStatusBytes
	exporterDiagnosticWorkers           = 2
	exporterDiagnosticPipes             = 2
	exporterDiagnosticSocketBytes       = 4096
	exporterDiagnosticSocketWasteFactor = 8
)

// DiagnosticStream describes captured bytes, not filesystem acceptance. Overflow
// or an I/O error makes evidence incomplete even while its pipe keeps draining.
type DiagnosticStream struct {
	Observed   uint64
	Stored     uint64
	Overflow   bool
	EOF        bool
	ReadError  bool
	WriteError bool
}

// ExporterDiagnostics is an inert snapshot of the two independent log drains.
// Complete requires process exit, both EOFs, and no lost diagnostic evidence.
// WaitError reports the child outcome independently of capture completeness.
type ExporterDiagnostics struct {
	Started       bool
	ProcessExited bool
	WaitError     bool
	ExitCode      int
	Stdout        DiagnosticStream
	Stderr        DiagnosticStream
	StatusError   bool
	Complete      bool
}

type diagnosticBudget struct {
	MemoryBytes, FileBytes, Workers, Pipes, SetupFDs, RetainedFDs, SocketBufferBytes, SocketWasteFactorMax uint64
}

// These private OS boundaries retain real file ownership in production. Tests
// can inject one write/sync result while still using the actual owned files.
type diagnosticSink interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type diagnosticStatus interface {
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Close() error
}

type exporterLogs struct {
	// A live drain retains the original funding even after Supervisor.Close.
	funding   *runtimeFunding
	mu        sync.Mutex
	state     ExporterDiagnostics
	streams   [2]*exporterLog
	status    diagnosticStatus
	statusErr error // first error remains observable after descriptor close; bounded
}

type exporterLog struct {
	owner          *exporterLogs
	index          int
	file           diagnosticSink
	reader, writer *os.File
}

var _ io.Writer = (*exporterLog)(nil)

func newExporterLogs(directory *os.File) (*exporterLogs, error) {
	l := &exporterLogs{state: ExporterDiagnostics{ExitCode: -1}}
	var err error
	l.status, err = ownedFile(directory, "exporter-diagnostics.json", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	for i, name := range []string{"exporter.stdout", "exporter.stderr"} {
		stream := &exporterLog{owner: l, index: i}
		l.streams[i] = stream
		var file *os.File
		file, err = ownedFile(directory, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0600)
		if err == nil {
			stream.file = file
			stream.reader, stream.writer, err = diagnosticOutputPair()
		}
		if err != nil {
			l.abort()
			return nil, err
		}
	}
	if err = l.checkpoint(); err == nil {
		err = directory.Sync()
	}
	if err != nil {
		l.abort()
		return nil, err
	}
	return l, nil
}

// Diagnostics reports only this actually launched exporter's retained capture.
// A live process necessarily has incomplete final evidence.
func (s *Supervisor) Diagnostics() (ExporterDiagnostics, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child == nil || s.child.logs == nil {
		return ExporterDiagnostics{}, false
	}
	l := s.child.logs
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshot(), true
}

// Write always consumes the complete incoming slice, including after a storage
// refusal. This prevents log overflow from breaking a VM's stdout/stderr pipe.
func (s *exporterLog) Write(p []byte) (int, error) {
	l := s.owner
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.streamState(s.index)
	n := uint64(len(p))
	if n > math.MaxUint64-state.Observed {
		state.Observed = math.MaxUint64
	} else {
		state.Observed += n
	}
	remaining := uint64(exporterLogBytes) - state.Stored
	if state.Observed > exporterLogBytes {
		state.Overflow = true
	}
	if !state.WriteError && remaining > 0 {
		stored, err := s.file.Write(p[:min(n, remaining)])
		state.Stored += uint64(stored)
		if err != nil || stored != int(min(n, remaining)) {
			state.WriteError = true
		}
	}
	return len(p), nil
}

func fixedDiagnosticBudget() diagnosticBudget {
	return diagnosticBudget{exporterDiagnosticMemoryBytes, exporterDiagnosticDiskBytes, exporterDiagnosticWorkers, exporterDiagnosticPipes, 7, 5, exporterDiagnosticSocketBytes, exporterDiagnosticSocketWasteFactor}
}

func (l *exporterLogs) attach(command *exec.Cmd) {
	command.Stdout = l.streams[0].writer
	command.Stderr = l.streams[1].writer
}

func (l *exporterLogs) start() {
	l.mu.Lock()
	l.state.Started = true
	_ = l.checkpointLocked()
	l.mu.Unlock()
	for _, stream := range l.streams {
		// Only the child owns the write ends after successful exec. The two
		// drain workers and fixed buffers remain owned until their actual EOF.
		if err := stream.writer.Close(); err != nil {
			l.mu.Lock()
			l.rememberStatusError(err)
			_ = l.checkpointLocked()
			l.mu.Unlock()
		}
		go stream.drain()
	}
}

func (s *exporterLog) drain() {
	buffer := make([]byte, exporterLogBufferBytes)
	_, err := io.CopyBuffer(s, struct{ io.Reader }{s.reader}, buffer)
	l := s.owner
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.streamState(s.index)
	state.EOF = err == nil
	state.ReadError = err != nil
	if err = errors.Join(s.reader.Close(), s.file.Sync(), s.file.Close()); err != nil {
		state.WriteError = true
	}
	_ = l.checkpointLocked()
	l.finishLocked()
}

func (l *exporterLogs) exited(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state.ProcessExited = true
	l.state.WaitError = err != nil
	if err == nil {
		l.state.ExitCode = 0
	} else {
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			l.state.ExitCode = failure.ExitCode()
		}
	}
	_ = l.checkpointLocked()
	l.finishLocked()
}

// checkpoint never joins the child or closes its drains. Supervisor.Close can
// preserve incomplete evidence while the actual VM continues to run.
func (l *exporterLogs) checkpoint() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.checkpointLocked()
}

func (l *exporterLogs) checkpointLocked() error {
	if l.status == nil {
		return l.statusErr
	}
	frame, err := diagnosticStatusFrame(l.snapshot())
	if err == nil {
		err = writeDiagnosticStatus(l.status, frame)
	}
	if err != nil {
		l.rememberStatusError(err)
		// A successful WriteAt followed by failed Sync can leave readable
		// Complete=true bytes. Invalidate once, best effort, without clearing
		// the original error. Failed storage cannot promise durable repair.
		invalid, invalidErr := diagnosticStatusFrame(l.snapshot())
		if invalidErr == nil {
			invalidErr = writeDiagnosticStatus(l.status, invalid)
		}
		l.rememberStatusError(invalidErr)
	}
	return l.statusErr
}

func diagnosticStatusFrame(state ExporterDiagnostics) ([]byte, error) {
	raw, err := json.Marshal(state)
	if err != nil || len(raw)+1 > exporterLogStatusBytes {
		return nil, errors.Join(err, io.ErrShortBuffer)
	}
	buffer := make([]byte, exporterLogStatusBytes)
	for i := range buffer {
		buffer[i] = ' '
	}
	copy(buffer, raw)
	buffer[len(raw)] = '\n'
	return buffer, nil
}

func writeDiagnosticStatus(status diagnosticStatus, frame []byte) error {
	n, err := status.WriteAt(frame, 0)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = status.Sync()
	}
	return err
}

func (l *exporterLogs) rememberStatusError(err error) {
	if err != nil {
		l.state.StatusError = true
		if l.statusErr == nil {
			l.statusErr = err
		}
	}
}

func (l *exporterLogs) snapshot() ExporterDiagnostics {
	state := l.state
	state.Complete = state.Started && state.ProcessExited && !state.StatusError && diagnosticComplete(state.Stdout) && diagnosticComplete(state.Stderr)
	return state
}

func diagnosticComplete(s DiagnosticStream) bool {
	return s.EOF && !s.Overflow && !s.ReadError && !s.WriteError
}

func (l *exporterLogs) streamState(i int) *DiagnosticStream {
	if i == 0 {
		return &l.state.Stdout
	}
	return &l.state.Stderr
}

func (l *exporterLogs) finishLocked() {
	if !l.state.ProcessExited || (!l.state.Stdout.EOF && !l.state.Stdout.ReadError) || (!l.state.Stderr.EOF && !l.state.Stderr.ReadError) {
		return
	}
	if l.status != nil {
		l.rememberStatusError(l.status.Close())
		l.status = nil
	}
}

// abort is used only before a child successfully starts. Failed files survive.
func (l *exporterLogs) abort() {
	for _, stream := range l.streams {
		if stream == nil {
			continue
		}
		for _, f := range []*os.File{stream.reader, stream.writer} {
			if f != nil {
				_ = f.Close()
			}
		}
		if stream.file != nil {
			_ = stream.file.Close()
		}
	}
	_ = l.checkpoint()
	if l.status != nil {
		l.rememberStatusError(l.status.Close())
		l.status = nil
	}
}
