package daemoncli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

// testFaults intentionally implements one bounded failure, never process/host
// lifecycle, forced ownership, history loss, restoration, or fencing authority.
type testFaults struct{ journalBeforeSync atomic.Bool }

type adminService struct {
	server *admin.Server
	audit  *admin.AuditLog
}

func (f *testFaults) toggle(ctx context.Context, fault string, enabled bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fault != "journal-before-sync" {
		return fmt.Errorf("unknown test fault")
	}
	f.journalBeforeSync.Store(enabled)
	return nil
}
func (f *testFaults) journal(point string) error {
	if point == "before-sync" && f.journalBeforeSync.Load() {
		return fmt.Errorf("explicit test fault: journal-before-sync")
	}
	return nil
}
func (f *testFaults) enabled() []string {
	if f.journalBeforeSync.Load() {
		return []string{"journal-before-sync"}
	}
	return []string{}
}
func socketPath(c Config) string { return filepath.Join(c.ControlDir, "admin-"+c.VolumeID+".sock") }
func startAdmin(ctx context.Context, c Config, l *localService) (_ *adminService, retErr error) {
	a, err := admin.OpenAudit(ctx, filepath.Join(c.ControlDir, "admin-"+c.VolumeID+".audit.jsonl"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, a.Close())
		}
	}()
	generation := l.service.FrontendInfo().Generation.String()
	s, err := admin.Listen(ctx, admin.Config{SocketPath: socketPath(c), NodeID: c.NodeID, VolumeID: c.VolumeID, Generation: generation, EnableTestHooks: c.EnableTestHooks, ToggleFault: l.faults.toggle, Audit: a.Record, Status: func(ctx context.Context) (admin.Status, error) {
		if err := ctx.Err(); err != nil {
			return admin.Status{}, err
		}
		observation := l.service.Status()
		states := map[fsservice.State]string{fsservice.Recovering: "RECOVERING", fsservice.Serving: "SERVING", fsservice.Blocked: "BLOCKED"}
		return admin.Status{NodeID: c.NodeID, TestHooksEnabled: c.EnableTestHooks, EnabledFaults: l.faults.enabled(), Volumes: []admin.VolumeStatus{{VolumeID: c.VolumeID, Generation: generation, State: states[observation.State], WaitReason: observation.WaitReason, Profile: validationName, NormalPublication: false, SourcePosition: adminPosition(observation.SourcePosition), AppliedPosition: adminPosition(observation.AppliedPosition), BackingSlots: observation.BackingSlots}}}, nil
	}})
	if err != nil {
		return nil, err
	}
	return &adminService{server: s, audit: a}, nil
}
func (a *adminService) close() error { return errors.Join(a.server.Close(), a.audit.Close()) }
func adminPosition(p fsapi.Position) *admin.Position {
	if !p.Valid() {
		return nil
	}
	return &admin.Position{Volume: p.Volume.String(), Generation: p.Generation.String(), Configuration: p.Configuration.String(), Epoch: p.Epoch, Sequence: p.Sequence}
}
