package segment

import (
	"context"
	"io"

	"github.com/snonux/homelabfs/internal/vfs"
)

const scanWindowBytes = 64 << 10

// scanWindow coalesces sequential frame reads within a fixed file snapshot.
// It owns only a bounded byte window, never decoded records or validation state.
type scanWindow struct {
	file   vfs.File
	buffer []byte
	size   int64
	start  int64
	valid  int
}

func newScanWindow(file vfs.File, size int64) *scanWindow {
	return &scanWindow{file: file, size: size, buffer: make([]byte, min(size, scanWindowBytes))}
}

func (w *scanWindow) ReadAt(ctx context.Context, data []byte, offset int64) (int, error) {
	read := 0
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return read, err
		}
		if offset < 0 || offset >= w.size {
			return read, io.EOF
		}
		if offset < w.start || offset >= w.start+int64(w.valid) {
			w.valid = 0
			chunk := w.buffer[:min(int64(len(w.buffer)), w.size-offset)]
			n, err := w.file.ReadAt(ctx, chunk, offset)
			// Never hide a read-ahead fault, even when its partial bytes would
			// satisfy the current frame. Do not cache failed or short reads.
			if err != nil {
				return read, err
			}
			if n != len(chunk) {
				return read, io.ErrUnexpectedEOF
			}
			w.start, w.valid = offset, n
		}
		n := copy(data, w.buffer[offset-w.start:int64(w.valid)])
		read += n
		offset += int64(n)
		data = data[n:]
	}
	return read, nil
}
