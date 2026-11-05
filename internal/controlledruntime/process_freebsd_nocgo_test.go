//go:build freebsd && !cgo

package controlledruntime

import (
	"bytes"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestUnsupportedObserverStartCreatesNoChild(t *testing.T) {
	s := ownerFixture(t)
	spec, marker := refusedObserverSpec(t)
	if err := s.Start(t.Context(), spec); !errors.Is(err, fsapi.Unsupported) {
		t.Fatalf("unsupported build start: %v", err)
	}
	assertObserverRefusalHasNoChild(t, s, nil, marker)
	if s.record.Stage != "start-failed" {
		t.Fatalf("failed ownership not consumed: %s", s.record.Stage)
	}
	var output bytes.Buffer
	if err := Run(t.Context(), []string{"--check-process-observer"}, &output, &output); !errors.Is(err, fsapi.Unsupported) || output.Len() != 0 {
		t.Fatalf("unsupported build qualification: %v %s", err, output.String())
	}
}
