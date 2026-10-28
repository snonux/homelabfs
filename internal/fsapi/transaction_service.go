package fsapi

import "fmt"

// SessionRequest captures an authenticated session and owned execution
// credentials. The adapter must bind Principal to authentication; user-supplied
// identity fields or numeric credentials alone never authorize an operation.
type SessionRequest struct {
	session     SessionID
	credentials Cred
}

// IdentityRequest addresses the original anchor without resolving any paths.
// GetStatus/Cancel must not execute an unknown identity.
type IdentityRequest struct {
	session SessionRequest
	number  uint64
}

// RetirementRequest advances an anchor's durable monotonic request floor.
// Through includes unused lower numbers too. Clients first persist terminal
// receipts; unresolved claims block advancement. Close also requests a permanent
// session tombstone after every allocated request has been resolved and retired.
type RetirementRequest struct {
	session SessionRequest
	through uint64
	close   bool
}

// SessionState is an authoritative registration/retirement outcome.
type SessionState uint8

const (
	// SessionReady reports a protected live session and its durable retirement floor.
	SessionReady SessionState = iota + 1
	// SessionClosed reports a permanent tombstone; registration cannot recreate it.
	SessionClosed
	// SessionPending reports registration/retirement whose protection is in progress.
	SessionPending
	// SessionIndeterminate reports unavailable authority or evidence.
	SessionIndeterminate
	// SessionRejected reports a proven unsuccessful registration/retirement request.
	SessionRejected
)

// SessionOutcome is the typed registration/retirement reply. RetiredThrough is
// the proven protected floor, not an echoed requested floor. No wall-clock expiry
// or owner change may erase this floor, tombstone or unretired outcome evidence.
type SessionOutcome struct {
	Session        SessionID
	State          SessionState
	RetiredThrough uint64
	Code           TransactionCode
	Uncertainty    Uncertainty
}

// TransactionService is the asynchronous application namespace API. Each method
// returns promptly with one non-nil completion, which the implementation must
// complete exactly once. It starts no work through this interface definition.
//
// Route every method by immutable SessionID.Anchor. The current authoritative
// anchor consults registration, tombstone, retirement floor and original claim
// BEFORE pathname resolution or coordinator selection. Protect exact original
// bytes plus digest, then immutable coordinator/participant binding. Authenticate
// GetStatus/Cancel to the originating principal or an authorized administrator.
// Missing history is uncertainty, never proof that a request may execute afresh.
//
// Wait cancellation abandons only a wait. Cancel requests abort before a commit
// proposal and returns the actual outcome if commitment won. It cannot reverse
// COMMIT. Ordinary cross-mount POSIX rename/link still reports CrossDevice.
// These methods do not imply guest/9p support or implement transport or storage.
type TransactionService interface {
	RegisterSession(SessionRequest) *Completion[SessionOutcome]
	Execute(ExecuteRequest) *Completion[TransactionOutcome]
	GetStatus(IdentityRequest) *Completion[TransactionOutcome]
	Cancel(IdentityRequest) *Completion[TransactionOutcome]
	RetireSession(RetirementRequest) *Completion[SessionOutcome]
}

// NewSessionRequest snapshots credentials bound to a complete anchored session.
func NewSessionRequest(session SessionID, credentials Cred) (SessionRequest, error) {
	if !session.Valid() {
		return SessionRequest{}, fmt.Errorf("%w: anchored session", Invalid)
	}
	return SessionRequest{session: session, credentials: credentials.Clone()}, nil
}

// NewIdentityRequest addresses one persisted nonzero request number.
func NewIdentityRequest(session SessionRequest, number uint64) (IdentityRequest, error) {
	if !session.session.Valid() || number == 0 {
		return IdentityRequest{}, fmt.Errorf("%w: anchored request identity", Invalid)
	}
	return IdentityRequest{session: session, number: number}, nil
}

// NewRetirementRequest captures an explicit prefix retirement/closure request.
// Through zero permits closing an empty session; the service checks obligations.
func NewRetirementRequest(session SessionRequest, through uint64, closeSession bool) (RetirementRequest, error) {
	if !session.session.Valid() || through == 0 && !closeSession {
		return RetirementRequest{}, fmt.Errorf("%w: session retirement", Invalid)
	}
	return RetirementRequest{session: session, through: through, close: closeSession}, nil
}

// Session returns the immutable authenticated anchor identity.
func (r SessionRequest) Session() SessionID { return r.session }

// Credentials returns a copy of captured execution credentials.
func (r SessionRequest) Credentials() Cred { return r.credentials.Clone() }

// ID returns the complete original anchored request identity.
func (r IdentityRequest) ID() TransactionID {
	return TransactionID{Session: r.session.session, Number: r.number}
}

// Credentials returns a copy of captured execution credentials.
func (r IdentityRequest) Credentials() Cred { return r.session.Credentials() }

// Session returns the immutable anchor whose floor/tombstone must be updated.
func (r RetirementRequest) Session() SessionID { return r.session.session }

// Credentials returns a copy of captured execution credentials.
func (r RetirementRequest) Credentials() Cred { return r.session.Credentials() }

// Through returns the requested inclusive retirement floor.
func (r RetirementRequest) Through() uint64 { return r.through }

// Close reports whether a permanent closed-session tombstone was requested.
func (r RetirementRequest) Close() bool { return r.close }

// Validate checks a registration/retirement result's portable structure.
func (o SessionOutcome) Validate() error { return o.validate() }

func (o SessionOutcome) validate() error {
	if !o.Session.Valid() || o.State < SessionReady || o.State > SessionRejected || o.Code > AbortRequested || o.Uncertainty > DecisionUnresolved {
		return fmt.Errorf("%w: session outcome", Invalid)
	}
	if (o.State == SessionRejected) != (o.Code != NoFailure) || (o.State == SessionIndeterminate) != (o.Uncertainty != NoUncertainty) {
		return fmt.Errorf("%w: incompatible session outcome", Invalid)
	}
	return nil
}

func (o SessionOutcome) clone() SessionOutcome { return o }
