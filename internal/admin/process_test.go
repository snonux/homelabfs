package admin

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
)

func TestCallProcessRefusesSameUIDWrongProcessBeforeStatus(t *testing.T) {
	c := testConfig(t)
	var reads atomic.Int32
	c.Status = func(context.Context) (Status, error) { reads.Add(1); return Status{NodeID: c.NodeID}, nil }
	testServer(t, c)
	if _, err := CallProcess(t.Context(), c.SocketPath, Request{Version: 1, Operation: "status"}, os.Getpid()+1); err == nil {
		t.Fatal("same UID substituted for expected process identity")
	}
	if reads.Load() != 0 {
		t.Fatal("request was sent to the wrong process")
	}
	if _, err := CallProcess(t.Context(), c.SocketPath, Request{Version: 1, Operation: "status"}, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 {
		t.Fatal("expected process request did not complete")
	}
}
