//go:build linux

package controlledruntime

import (
	"context"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
)

func mountNative(context.Context, string, *fuseadapter.Shell) (*nativeMount, error) {
	return nil, fsapi.Unsupported
}
func openNativeTuple(context.Context, *nativeMount) error { return fsapi.Unsupported }
