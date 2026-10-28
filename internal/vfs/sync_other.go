//go:build !linux

package vfs

import "os"

// File.Sync is a conservative fdatasync fallback, including on FreeBSD.
func syncData(f *os.File) error { return f.Sync() }
