//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestDescriptorExecReport(t *testing.T) {
	var output bytes.Buffer
	err := reportDescriptorExec(&output)
	var report descriptorExecReport
	if decodeErr := json.Unmarshal(output.Bytes(), &report); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if report.Supported != (err == nil) || report.Version != "homelabfs-descriptor-exec/1" || report.Publication != nil {
		t.Fatalf("inconsistent prerequisite report: %s: %v", output.Bytes(), err)
	}
}

func TestDescriptorExecFlagsExclusive(t *testing.T) {
	for _, extra := range []string{"--config=/absent", "--disposable-validation", "--check-process-observer", "argument"} {
		var output bytes.Buffer
		if err := Run(context.Background(), []string{"--check-descriptor-exec", extra}, &output, &output); err == nil || output.Len() != 0 {
			t.Fatalf("mixed prerequisite flags accepted: %s: %v", extra, err)
		}
	}
}
