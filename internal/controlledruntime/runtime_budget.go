//go:build linux || freebsd

package controlledruntime

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// RuntimeBudget separates the host envelope from the unchanged guest ceiling.
// These reservations are admission accounting, never evidence of OS placement,
// available physical RAM, mounted storage capacity or native pressure acceptance.
type RuntimeBudget struct {
	HostBytes, ServiceBytes, ExporterBytes, GuestCopyBytes uint64
	GuestBytes, GuestBaseBytes                             uint64
	Guest                                                  GuestArtifactBudget
}

type runtimeFunding struct {
	transport *admission.Budget
	owner     *admission.Lease
	// control funds the private post-READY lifecycle endpoint: one worker, one
	// connection, its frames/evidence and transient C8/controller/source
	// inspection copies. Unlike owner it holds no native/guest obligation, so it
	// is released once that endpoint's single worker has actually joined.
	controlMu sync.Mutex
	control   *admission.Lease
}

func checkedBudgetSum(values ...uint64) (uint64, error) {
	var sum uint64
	for _, v := range values {
		if v > math.MaxUint64-sum {
			return 0, fsapi.Invalid
		}
		sum += v
	}
	return sum, nil
}

// fundRuntime reserves complete C8 mmap/copy capacity independently of its
// 652 MiB arenas, then all semantic/source/controller/exporter/guest-copy owners
// and the separate private lifecycle control endpoint.
func fundRuntime(b RuntimeBudget, p ProviderLimits, views viewcontrol.Bounds, domains int) (*runtimeFunding, error) {
	if !validProviderLimits(p) || domains < 1 || domains > 16 || views.WorkBytes <= 0 || views.SourceBytes <= 0 || views.LedgerBytes <= 0 || views.Sources < 1 || b.ServiceBytes < 64<<20 || b.ExporterBytes < ((64+64+130+15)<<20)+exporterDiagnosticMemoryBytes || b.GuestCopyBytes < 16<<20 || b.GuestBaseBytes < 256<<20 || b.GuestBytes == 0 || b.GuestBytes > 2<<30 || !guestBudgetValid(b.Guest) {
		return nil, fmt.Errorf("explicit runtime owner budgets: %w", fsapi.Invalid)
	}
	guest, err := checkedBudgetSum(b.GuestBaseBytes, b.Guest.OrdinaryBytes, b.Guest.ControlBytes, b.Guest.Reservations*131072, b.Guest.Cookies*4096, b.Guest.Received*4096)
	if err != nil || guest > b.GuestBytes {
		return nil, fmt.Errorf("guest budget exceeds unchanged 2 GiB placement: %w", fsapi.NoSpace)
	}
	extra, err := runtimeOwnerBytes(b, p, views, domains)
	if err != nil {
		return nil, err
	}
	control, err := lifecycleMemoryBytes(views)
	if err != nil {
		return nil, err
	}
	total, err := checkedBudgetSum(extra, control, 652<<20, guestbridge.TransportLedgerMemoryBytes)
	if err != nil || total > b.HostBytes {
		return nil, fmt.Errorf("host budget omits a runtime owner: %w", fsapi.NoSpace)
	}
	return reserveRuntimeFunding(b.HostBytes, extra, control)
}

// runtimeOwnerBytes sums the retained semantic/exporter/guest-copy owners.
func runtimeOwnerBytes(b RuntimeBudget, p ProviderLimits, views viewcontrol.Bounds, domains int) (uint64, error) {
	// Four provider ledger/decoded/serializer copies plus permanent identity
	// metadata are separate from the C8 and view-controller reservations.
	perProvider, err := checkedBudgetSum(p.RetainedBytes, p.SourceBytes, uint64(p.Claims)*4096, 16<<20)
	if err != nil || perProvider > math.MaxUint64/4 {
		return 0, fsapi.Invalid
	}
	perProvider *= 4
	if perProvider > math.MaxUint64/uint64(domains) {
		return 0, fsapi.Invalid
	}
	// View WorkBytes already owns three source copies and ledger/parser peaks.
	return checkedBudgetSum(b.ServiceBytes, b.ExporterBytes, b.GuestCopyBytes, uint64(views.WorkBytes), perProvider*uint64(domains))
}

// reserveRuntimeFunding creates the finite pools and reserves the retained owner
// lease plus the separate lifecycle-control lease. The Memory pool admits
// exactly those two leases and the transport's workspace and ledger leases.
func reserveRuntimeFunding(host, owner, control uint64) (*runtimeFunding, error) {
	var config admission.Config
	for class, resources := range map[admission.Class]admission.Resources{
		admission.Live:    {Bytes: 512 << 20, Operations: 64, Workers: 2},
		admission.Control: {Bytes: 128 << 20, Operations: 16, Workers: 2},
		admission.Memory:  {Bytes: host - (640 << 20), Operations: 3 + 1, Workers: 1 + exporterDiagnosticWorkers + lifecycleWorkers, Connections: exporterDiagnosticPipes + lifecycleConnections},
	} {
		config.Pools[class] = admission.Limits{Global: resources, PerPeer: resources, PerVolume: resources}
	}
	budget, err := admission.New(config)
	if err != nil {
		return nil, err
	}
	lease, err := budget.Reserve(admission.Request{Class: admission.Memory, Peer: "controlled-runtime", Volume: "controlled-runtime", Cost: admission.Resources{Bytes: owner, Operations: 1, Workers: exporterDiagnosticWorkers, Connections: exporterDiagnosticPipes}})
	if err != nil {
		return nil, err
	}
	controlLease, err := budget.Reserve(admission.Request{Class: admission.Memory, Peer: "controlled-runtime-control", Volume: "controlled-runtime", Cost: admission.Resources{Bytes: control, Operations: 1, Workers: lifecycleWorkers, Connections: lifecycleConnections}})
	if err != nil {
		return nil, errors.Join(err, lease.Release())
	}
	return &runtimeFunding{transport: budget, owner: lease, control: controlLease}, nil
}

// releaseControl returns only the lifecycle endpoint's own funding. It is
// idempotent and must run after that endpoint's worker joined (or never ran).
func (f *runtimeFunding) releaseControl() error {
	if f == nil {
		return nil
	}
	f.controlMu.Lock()
	defer f.controlMu.Unlock()
	if f.control == nil {
		return nil
	}
	err := f.control.Release()
	f.control = nil
	return err
}
