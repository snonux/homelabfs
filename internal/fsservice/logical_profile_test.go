package fsservice

import (
	"bytes"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/logicalview"
)

func TestLogicalFourMiBProfileRAMPlanningAndRecoveryBeforeBacking(t *testing.T) {
	f := newLogicalFixture(t)
	config := f.config
	// This explicit disposable profile admits the existing 4 MiB logical workload.
	// Generic constructor defaults are deliberately smaller and prove no canary
	// calibration, platform cache behavior or runtime profile admission.
	config.Limits = logicalview.Limits{MaxSnapshotBytes: 6 << 20, MaxClaimBytes: 8 << 20, Versions: 8, Claims: 6, Handles: 16, Readers: 1, Dependencies: 16, Identities: 64}
	budget, err := admission.New(fourMiBLogicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	config.Budget = budget
	view, err := logicalview.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	f.config = config
	f.view = view
	policy, err := NewUnixPermissions(uid(419))
	if err != nil {
		t.Fatal(err)
	}
	plannerConfig := LogicalConfig{View: view, Permissions: policy, Clock: fixedClock{}, Limits: Limits{IOBytes: 1 << 20, LogicalBytes: 8 << 20}}
	f.planner, err = NewLogicalPlanner(plannerConfig)
	if err != nil {
		t.Fatal(err)
	}
	cred := logicalCred()
	root := f.local.c.Storage.Root
	created := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "four-mib", Mode: 0o640, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	f.commit(created, uid(11))
	id := created.Operation.Object
	h := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}}).Handle
	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i*31 + 7)
	}
	// Preserve record.Write's existing 1 MiB byte-range bound. The total file
	// remains 4 MiB and successive grants alternate independent origins.
	for chunk := 0; chunk < 4; chunk++ {
		offset := chunk * (1 << 20)
		node := uid(11)
		if chunk%2 == 1 {
			node = uid(9)
		}
		f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: h, Offset: int64(offset), Data: payload[offset : offset+1<<20]}), node)
	}
	patch := []byte("cross-boundary")
	offset := int64(1<<20) - 3
	planned := f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: h, Offset: offset, Data: patch})
	if planned.Operation.Offset != offset || planned.Result.Attr == nil || planned.Result.Attr.Size != 4<<20 {
		t.Fatal("planning used a stale size/cut", planned.Result)
	}
	f.commit(planned, uid(11))
	copy(payload[offset:], patch)
	assertData := func() {
		t.Helper()
		for _, off := range []int64{0, 1 << 20, 2 << 20, 3 << 20} {
			got := f.observe(cred, fsapi.Command{Operation: fsapi.Read, Handle: h, Offset: off, Size: 1 << 20})
			if !bytes.Equal(got.Data, payload[off:off+1<<20]) {
				t.Fatalf("4 MiB RAM profile data mismatch at %d", off)
			}
		}
	}
	assertData()
	if _, err = f.local.service.applied.Store.Stat(t.Context(), id); !errors.Is(err, fsapi.NotFound) {
		t.Fatal("owner backing advanced during RAM profile", err)
	}
	appendHandle := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Append: true}}).Handle
	appendPlan := f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: appendHandle, Data: []byte("!")})
	if appendPlan.Operation.Offset != 4<<20 {
		t.Fatal("dependent append did not use complete 4 MiB RAM view", appendPlan.Operation.Offset)
	}
	var encoded bytes.Buffer
	if err = f.view.WriteRecovery(t.Context(), &encoded); err != nil {
		t.Fatal(err)
	}
	restoredConfig := config
	restoredBudget, err := admission.New(fourMiBLogicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	restoredConfig.Budget = restoredBudget
	restored, err := logicalview.Restore(t.Context(), restoredConfig, bytes.NewReader(encoded.Bytes()), []byte("verified full source recovery"))
	if err != nil {
		t.Fatal(err)
	}
	f.view = restored
	plannerConfig.View = restored
	f.planner, err = NewLogicalPlanner(plannerConfig)
	if err != nil {
		t.Fatal(err)
	}
	assertData()
	if frontiers := restored.Frontiers(); frontiers.Visible == nil || frontiers.Visible.Slot.Serial != 6 || frontiers.Installed != nil || frontiers.Durable != nil {
		t.Fatal("restored projection invented backing coverage", frontiers)
	}
	if _, err = f.local.service.applied.Store.Stat(t.Context(), id); !errors.Is(err, fsapi.NotFound) {
		t.Fatal("serialized restore touched original owner backing", err)
	}
}

func fourMiBLogicalBudget() admission.Config {
	config := admission.DefaultConfig()
	memory := &config.Pools[admission.Memory]
	memory.Global.Bytes = 1 << 30
	memory.Global.OwnerRAM = 1 << 30
	memory.PerPeer.Bytes = 1 << 30
	memory.PerPeer.OwnerRAM = 1 << 30
	memory.PerVolume.Bytes = 512 << 20
	memory.PerVolume.OwnerRAM = 512 << 20
	recovery := &config.Pools[admission.Recovery]
	recovery.Global.Bytes = 1536 << 20
	recovery.Global.OwnerRAM = 1536 << 20
	recovery.PerPeer.Bytes = 1536 << 20
	recovery.PerPeer.OwnerRAM = 1536 << 20
	recovery.PerVolume.Bytes = 768 << 20
	recovery.PerVolume.OwnerRAM = 768 << 20
	return config
}
