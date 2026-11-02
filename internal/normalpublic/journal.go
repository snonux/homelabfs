package normalpublic

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type journal struct {
	file           *os.File
	bytes, maximum int64
	poison         error
}

func openJournal(ctx context.Context, path string, maximum int64) (*journal, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// O_NOFOLLOW and the single-link/private-file check prevent accidentally
	// sharing an existing journal through an alias. Parent is caller-owned.
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("open public journal: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	j := &journal{file: f, maximum: maximum}
	fail := func(err error) (*journal, []byte, error) { _ = j.close(); return nil, nil, err }
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("lock public journal: %w", err))
	}
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || info.Size() > maximum {
		return fail(fmt.Errorf("public journal ownership/type/size"))
	}
	// Persist initial file creation before any allocation can be acknowledged.
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fail(err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return fail(syncErr)
	}
	if closeErr != nil {
		return fail(closeErr)
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return fail(err)
	}
	if int64(len(data)) > maximum {
		return fail(fmt.Errorf("public journal size changed"))
	}
	j.bytes = int64(len(data))
	return j, data, nil
}

func (j *journal) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.file == nil {
		return fmt.Errorf("public journal closed")
	}
	if j.poison != nil {
		return fmt.Errorf("public journal requires recovery: %w", j.poison)
	}
	return nil
}

func (j *journal) append(ctx context.Context, data []byte) error {
	if err := j.check(ctx); err != nil {
		return err
	}
	if int64(len(data)) > j.maximum-j.bytes {
		return ErrBlocked
	}
	n, err := j.file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = j.file.Sync()
	}
	if err != nil {
		j.poison = err
		return fmt.Errorf("persist public transition: %w", err)
	}
	j.bytes += int64(len(data))
	return nil
}

func (j *journal) close() error {
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

func (l *Ledger) restore(ctx context.Context, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(data) < 16 {
			return fmt.Errorf("public journal torn header; preserved")
		}
		n := uint64(binary.BigEndian.Uint32(data[12:])) + 48
		if n > MaxFrame || n > uint64(len(data)) {
			return fmt.Errorf("public journal torn/bounded entry; preserved")
		}
		var e event
		if err := decodeEnvelope(data[:n], "HFSPLOG1", &e); err != nil {
			return err
		}
		if e.Ledger != l.config.LedgerID || e.Sequence != l.sequence+1 || e.Sequence == 0 || e.Previous != l.last {
			return fmt.Errorf("public journal sequence/hash chain")
		}
		_, commit, err := l.transition(ctx, e, false)
		if err != nil {
			return fmt.Errorf("public journal replay transition: %w", err)
		}
		if commit == nil {
			return fmt.Errorf("public journal redundant transition")
		}
		commit()
		l.sequence = e.Sequence
		l.last = sha256.Sum256(data[:n])
		data = data[n:]
	}
	return nil
}
