//go:build !linux && !freebsd

package vfs

import (
	"io/fs"
	"os"
)

func physicalInfo(fs.FileInfo) (Info, error) { return Info{}, fs.ErrInvalid }

func statDirEntry(*os.File, string) (Info, error) { return Info{}, fs.ErrInvalid }
