//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

const (
	// lostReplyOperationCall selects guestbridge.LostReplyOperation for any
	// guest CALL (the original v24 target).
	lostReplyOperationCall = 1
	// lostReplyOperationMkdir selects only a CALL whose guest intent opcode is
	// MKDIR (d44), so a guest mkdir(2), whose GETATTR/LOOKUP CALLs always come
	// first, loses the mutating reply itself. Every code outside
	// lostReplyIntents refuses; the 57-byte ARM frame is unchanged.
	lostReplyOperationMkdir = 2
	// lostReplyMemoryBytes funds the fixture: its fixed evidence record, the
	// copied claim, digests, the arm-time source baseline and their encodings.
	// The transient source capture itself is covered by the control lease.
	lostReplyMemoryBytes = 64 << 10
)

// lostReplyIntents maps each accepted HSC1 operation code to the guestbridge
// intent filter it arms ("" matches any CALL).
var lostReplyIntents = map[uint8]string{
	lostReplyOperationCall:  "",
	lostReplyOperationMkdir: guestbridge.LostReplyIntentMkdir,
}

// lifecycleLostReply is the single arming of the response-exposure fixture
// for this runtime. It keeps the exact transport it armed (later observations
// read the record from that owner even after a restart switched owners), the
// funding lease reserved before arming, and the service source identity seen
// at arming, so an external oracle can count the effects since then.
type lifecycleLostReply struct {
	id        fsapi.UUID
	cut       fsapi.UUID
	epoch     uint64
	session   string
	transport *guestbridge.Transport
	lease     *admission.Lease
	baseline  serviceEvidence
}

// lostReplyView is the inert ARM reply and observation section. Exposure is
// the actual transport record; it holds no acceptance value.
type lostReplyView struct {
	Fixture     string                        `json:"fixture"`
	Cut         string                        `json:"cut"`
	Epoch       uint64                        `json:"epoch"`
	Session     string                        `json:"session"`
	FundedBytes uint64                        `json:"funded_bytes"`
	Baseline    serviceEvidence               `json:"baseline"`
	Exposure    guestbridge.LostReplyEvidence `json:"exposure"`
	Error       string                        `json:"error,omitempty"`
}

// armLostReply arms the one-shot fixture on the actual current transport. The
// binding must name the exact current cut and epoch of a SERVING runtime, an
// enrolled session and an accepted operation code (any CALL, or only a MKDIR
// CALL). Funding is reserved and the source baseline captured before the
// transport is armed; any refusal releases the lease and leaves nothing armed,
// so a refused ARM consumes no one-shot.
func (l *runtimeLifecycle) armLostReply(ctx context.Context, r lifecycleRequest) (lostReplyView, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.checkArmLocked(r); err != nil {
		return lostReplyView{}, err
	}
	session := r.session.String()
	lease, err := l.supervisor.runtimeFunding.reserveLostReply()
	if err != nil {
		return lostReplyView{}, fmt.Errorf("%w: lost-reply fixture unfunded: %v", errLifecycleRefused, err)
	}
	armed := &lifecycleLostReply{cut: l.cut, epoch: l.epoch, session: session, transport: l.current.transport, lease: lease}
	armed.baseline = captureService(ctx, l.current.service, l.sourceBytes)
	if armed.baseline.Error == "" {
		_, err = rand.Read(armed.id[:])
	} else {
		err = fmt.Errorf("source baseline: %s", armed.baseline.Error)
	}
	if err == nil {
		err = armed.transport.ArmLostReply(session, guestbridge.LostReplyOperation, lostReplyIntents[r.operation])
	}
	if err != nil {
		if releaseErr := lease.Release(); releaseErr != nil {
			err = fmt.Errorf("%v; lease release: %v", err, releaseErr)
		}
		return lostReplyView{}, fmt.Errorf("%w: %v", errLifecycleRefused, err)
	}
	l.lostReply = armed
	return *l.lostReplyViewLocked(), nil
}

// checkArmLocked applies every binding check before any resource is touched.
func (l *runtimeLifecycle) checkArmLocked(r lifecycleRequest) error {
	switch {
	case l.stage != lifecycleServing:
		return fmt.Errorf("%w: lost-reply arming after restart attempt (%s)", errLifecycleRefused, l.stage)
	case r.cut != l.cut || r.epoch != l.epoch:
		return fmt.Errorf("%w: stale cut/epoch binding", errLifecycleRefused)
	case l.lostReply != nil:
		return fmt.Errorf("%w: lost-reply fixture already armed once", errLifecycleRefused)
	case !hasLostReplyIntent(r.operation):
		return fmt.Errorf("%w: unsupported lost-reply operation %d", errLifecycleRefused, r.operation)
	case !slices.Contains(l.sessions, r.session.String()):
		return fmt.Errorf("%w: session not enrolled in this runtime", errLifecycleRefused)
	}
	return nil
}

// hasLostReplyIntent reports whether an ARM operation code is in the closed
// lostReplyIntents table; anything else refuses before funding is reserved.
func hasLostReplyIntent(operation uint8) bool {
	_, ok := lostReplyIntents[operation]
	return ok
}

// lostReplyViewLocked reports the armed fixture from the transport it armed,
// or nil if no ARM ever succeeded.
func (l *runtimeLifecycle) lostReplyViewLocked() *lostReplyView {
	a := l.lostReply
	if a == nil {
		return nil
	}
	v := &lostReplyView{Fixture: a.id.String(), Cut: a.cut.String(), Epoch: a.epoch, Session: a.session, FundedBytes: lostReplyMemoryBytes, Baseline: a.baseline}
	exposure, ok := a.transport.LostReply()
	if !ok {
		v.Error = "armed transport holds no fixture record"
	}
	v.Exposure = exposure
	return v
}

// expireLostReplyLocked disarms an unfired fixture before a C9 suspension, so
// it never outlives the exact cut it was armed for.
func (l *runtimeLifecycle) expireLostReplyLocked() {
	if l.lostReply != nil {
		l.lostReply.transport.ExpireLostReply()
	}
}

// releaseLostReplyLocked returns the fixture funding at runtime close. The record
// itself stays with the (closed) transport and is never reused.
func (l *runtimeLifecycle) releaseLostReplyLocked() error {
	if l.lostReply == nil || l.lostReply.lease == nil {
		return nil
	}
	err := l.lostReply.lease.Release()
	l.lostReply.lease = nil
	return err
}
