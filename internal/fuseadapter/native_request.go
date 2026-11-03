//go:build linux || freebsd

package fuseadapter

import "context"

type nativeRequestKey struct{}
type nativeRequestObservation struct {
	caller    Caller
	node      uint64
	operation string
}

// ObservedNativeRequest checks the private raw-callback context. A caller-created
// value or context cannot supply this observation. It is scoped to metadata
// provisioning authentication only, never transport command/read/write context.
func ObservedNativeRequest(ctx context.Context, c Caller, node uint64, operation string) bool {
	observation, ok := ctx.Value(nativeRequestKey{}).(nativeRequestObservation)
	return ok && observation.caller == c && observation.node == node && observation.operation == operation
}

func observeNativeRequest(ctx context.Context, c Caller, node uint64, operation string) context.Context {
	return context.WithValue(ctx, nativeRequestKey{}, nativeRequestObservation{caller: c, node: node, operation: operation})
}

// ObservedNativeCaller checks that credentials were requested by a raw callback,
// rather than directly from caller-created header values.
func ObservedNativeCaller(ctx context.Context, c Caller) bool {
	observation, ok := ctx.Value(nativeRequestKey{}).(nativeRequestObservation)
	return ok && observation.caller == c
}
