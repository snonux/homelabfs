package main

import (
	"math"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestLockRanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end uint64
		length     int64
		status     fuse.Status
	}{
		{"whole_file", 0, math.MaxInt64, 0, fuse.OK},
		{"tail_to_eof", 4096, math.MaxInt64, 0, fuse.OK},
		{"one_byte", 4096, 4096, 1, fuse.OK},
		{"range", 4096, 8191, 4096, fuse.OK},
		{"largest_finite", 0, math.MaxInt64 - 1, math.MaxInt64, fuse.OK},
		{"inverted", 2, 1, 0, fuse.EINVAL},
		{"unsigned_end", 0, math.MaxUint64, 0, fuse.EINVAL},
		{"unsigned_start", math.MaxUint64, math.MaxUint64, 0, fuse.EINVAL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := fuse.FileLock{Start: tc.start, End: tc.end, Typ: syscall.F_WRLCK}
			output, status := lockToFlock(input)
			if status != tc.status {
				t.Fatalf("status %v, want %v", status, tc.status)
			}
			if status != fuse.OK {
				return
			}
			if output.Start != int64(tc.start) || output.Len != tc.length || output.Whence != 0 {
				t.Fatalf("unexpected fcntl range: %+v", output)
			}
			var roundTrip fuse.FileLock
			roundTrip.FromFlockT(&output)
			if roundTrip.Start != input.Start || roundTrip.End != input.End || roundTrip.Typ != input.Typ {
				t.Fatalf("round trip %+v, want %+v", roundTrip, input)
			}
		})
	}
}
