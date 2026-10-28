package fsservice

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// New reconstructs the explicitly selected initialized volume before opening
// its serving gate. Errors preserve every resource. It never initializes a
// source, repairs a torn tail, imports production data or accepts CURRENT.
func New(ctx context.Context, c Config) (*Service, error) {
	if err := configure(&c); err != nil {
		return nil, err
	}
	s := &Service{config: c, accepted: make(map[*fsapi.Request]bool), queue: make(chan command, c.Limits.Queue), done: make(chan struct{}), ids: make(map[fsapi.UUID]bool), allocations: make(map[fsapi.ObjectID]bool), lifetimes: make(map[fsapi.ObjectID]*lifetime), handles: make(map[fsapi.OpenHandle]handle), pending: make(map[fsapi.UUID]command), waits: make(map[fsapi.UUID]fsapi.LockOwner), owners: make(map[fsapi.LockOwner]bool), sessions: make(map[fsapi.UUID]bool)}
	s.setState(Recovering, "replaying-local-history")
	if err := s.checkRoots(ctx); err != nil {
		return nil, err
	}
	if err := s.scanReservations(ctx); err != nil {
		return nil, err
	}
	if err := s.discover(ctx); err != nil {
		return nil, err
	}
	c = s.config
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	reader, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	ac := c.Apply
	ac.Storage = c.Storage
	ac.Workspace = c.Workspace.ID
	ac.Authorizer = s.authorizer()
	s.applied, err = apply.Replay(ctx, c.Workspace.Data, c.Workspace.Journal, c.Workspace.Progress, reader, s.selection, ac)
	closeErr := reader.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	image, err := s.applied.Store.Export(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.admitPlan(newPlan(image, c.Clock.Now())); err != nil {
		return nil, err
	}
	options := c.Segment
	options.GroupCommit = true
	options.Structural = func(uint64) record.Header {
		id, e := s.newUUID()
		if e != nil {
			return record.Header{}
		}
		return s.header(id)
	}
	options.NewSegment = func() fsapi.UUID { id, _ := s.newUUID(); return id }
	s.writer, err = segment.NewWriter(ctx, c.Source, c.Stream, options)
	if err != nil {
		return nil, err
	}
	if err = s.check(ctx); err != nil {
		_ = s.writer.Close()
		return nil, err
	}
	s.setState(Serving, "")
	go s.run()
	return s, nil
}

// Dispatch snapshots arguments before bounded admission. Cancellation affects
// admission/waiting only; an accepted command runs using service-owned context.
func (s *Service) Dispatch(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
	if c.DescriptionOnly && c.Operation != fsapi.Release {
		return fsapi.Invalid
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(c.Name) > 255 || len(c.NewName) > 255 || len(c.After) > 255 {
		return fsapi.NameTooLong
	}
	if len(c.Data) > s.config.Limits.IOBytes || c.Size < 0 || c.Operation < fsapi.Lookup || c.Operation > fsapi.Reap {
		return fsapi.Invalid
	}
	c = c.Clone()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.State() != Serving {
		return fsapi.Unavailable
	}
	select {
	case <-r.Completion().Done():
		return fsapi.Stale
	default:
	}
	if s.accepted[r] {
		return fsapi.Busy
	}
	select {
	case s.queue <- command{r, c}:
		s.accepted[r] = true
		return nil
	default:
		return fsapi.NoSpace
	}
}

// State returns the current serving gate, including uncertain failures.
func (s *Service) State() State {
	if s.stopReason.Load() != nil {
		return Blocked
	}
	return State(s.state.Load())
}

// Close closes admission and drains every accepted command. It adds no client
// durability promise and retains conservative lookup/open pins across restart.
func (s *Service) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	<-s.done
	return s.writer.Close()
}

func (s *Service) run() {
	defer close(s.done)
	ctx := context.Background()
	for cmd := range s.queue {
		if s.State() != Serving {
			s.finish(cmd, fsapi.Result{Code: fsapi.Unavailable})
			s.retryLocks(ctx)
			continue
		}
		if cmd.value.Operation == fsapi.WaitLock {
			id := cmd.value.Lock.WaitID
			_, used := s.waits[id]
			if !id.Valid() || used {
				s.finish(cmd, fsapi.Result{Code: fsapi.Invalid})
				continue
			}
			if len(s.waits) >= s.config.Limits.Locks {
				s.finish(cmd, fsapi.Result{Code: fsapi.NoSpace})
				continue
			}
			s.waits[id] = cmd.value.Lock.Owner
		}
		result := s.evaluate(ctx, cmd)
		if cmd.value.Operation == fsapi.WaitLock && result.Code == fsapi.WouldBlock {
			s.pending[cmd.value.Lock.WaitID] = cmd
		} else {
			s.finish(cmd, result)
		}
		s.retryLocks(ctx)
	}
	for id, cmd := range s.pending {
		s.finish(cmd, fsapi.Result{Code: fsapi.Unavailable})
		delete(s.pending, id)
	}
	s.locks = nil
	s.setState(Blocked, "service-closed")
}
func (s *Service) evaluate(ctx context.Context, cmd command) fsapi.Result {
	if s.State() != Serving {
		return fsapi.Result{Code: fsapi.Unavailable}
	}
	result, err := s.execute(ctx, cmd.request.Credentials(), cmd.value)
	if err != nil {
		return fsapi.Result{Code: errno(err)}
	}
	result.Generation = s.config.Workspace.ID
	if err = s.check(ctx); err != nil {
		s.setState(Blocked, "authority-check-failed")
		return fsapi.Result{Code: errno(err)}
	}
	return result
}
func (s *Service) finish(cmd command, result fsapi.Result) {
	if err := cmd.request.Completion().Complete(result); err != nil {
		s.setState(Blocked, "completion-failed")
		_ = cmd.request.Completion().Complete(fsapi.Result{Code: fsapi.IO})
	}
	s.mu.Lock()
	delete(s.accepted, cmd.request)
	s.mu.Unlock()
}
func (s *Service) check(ctx context.Context) error {
	if s.stopReason.Load() != nil {
		return fsapi.Unavailable
	}
	if s.config.Validation != nil {
		return nil
	}
	s.publishStatus("authority-check")
	err := s.config.Protocol.Check(ctx, s.selection.Through.Position)
	if err == nil {
		s.publishStatus("")
	}
	return err
}
func (s *Service) reader(ctx context.Context) (*segment.Reader, error) {
	p := fsapi.Position{Volume: s.config.Stream.Volume, Generation: s.config.Stream.Generation, Configuration: s.config.Stream.Configuration, Epoch: s.config.Stream.Epoch, Sequence: 1}
	return segment.OpenReader(ctx, s.config.Source, s.config.Stream, s.config.Segment.Recovery, p)
}
func (s *Service) discover(ctx context.Context) error {
	r, err := s.reader(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	selected := apply.Selection{Proof: []byte("disposable-local-validation-v1")}
	frames := 0
	var total int64
	for {
		e, err := r.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		frames++
		total += int64(len(e.Bytes))
		if frames > s.config.Apply.MaxFrames || total > s.config.Apply.MaxBytes {
			return fsapi.NoSpace
		}
		s.ids[e.Record.Header.Operation] = true
		selected.Through = e.Reference
		switch p := e.Record.Payload.(type) {
		case record.VolumeInit:
			s.allocations[p.Root] = true
			selected.Effects = append(selected.Effects, e.Reference)
		case record.Mutation:
			for _, o := range p.Redo.Objects {
				if o.ExpectedRevision == 0 {
					s.allocations[o.Object] = true
				}
			}
		case record.Prepared:
			for _, o := range p.Operation.Redo.Objects {
				if o.ExpectedRevision == 0 {
					s.allocations[o.Object] = true
				}
			}
		case record.Commit, record.HandlePin, record.HandleRelease, record.Reclaim:
			selected.Effects = append(selected.Effects, e.Reference)
		}
	}
	if len(selected.Effects) == 0 {
		return fsapi.Invalid
	}
	s.observedSource = selected.Through.Position
	if s.config.Validation != nil {
		s.selection = selected
	} else {
		s.selection = cloneSelection(s.config.Selection)
	}
	return nil
}
func (s *Service) protect(ctx context.Context, id fsapi.UUID, payloads ...any) error {
	proposal := Proposal{Previous: s.selection.Through, Selection: cloneSelection(s.selection)}
	for _, p := range payloads {
		if m, ok := p.(record.Mutation); ok {
			e, err := s.append(ctx, id, m)
			if err != nil {
				return s.block(err)
			}
			proposal.Frames = append(proposal.Frames, e)
			p = record.Commit{Mutation: e.Reference, Authority: s.config.Authority, Dependencies: []fsapi.Position{s.selection.Through.Position}}
		}
		e, err := s.append(ctx, id, p)
		if err != nil {
			return s.block(err)
		}
		proposal.Frames = append(proposal.Frames, e)
		proposal.Through = e.Reference
		proposal.Selection.Through = e.Reference
		proposal.Selection.Effects = append(proposal.Selection.Effects, e.Reference)
	}
	type answer struct {
		proof []byte
		err   error
	}
	ch := make(chan answer, 1)
	if s.config.Validation == nil {
		go func() { p, e := s.config.Protocol.Protect(ctx, cloneProposal(proposal)); ch <- answer{p, e} }()
	} else {
		ch <- answer{proof: []byte("disposable-local-validation-v1")}
	}
	s.publishStatus("local-journal-flush")
	localErr := s.writer.Flush(ctx)
	if s.config.Validation == nil {
		s.publishStatus("partner-protection")
	}
	remote := <-ch
	if localErr != nil {
		return s.block(localErr)
	}
	if remote.err != nil {
		return s.block(remote.err)
	}
	if len(remote.proof) == 0 || len(remote.proof) > 1<<20 {
		return s.block(fsapi.Invalid)
	}
	proposal.Selection.Proof = slices.Clone(remote.proof)
	if err := s.check(ctx); err != nil {
		return s.block(err)
	}
	reader, err := s.reader(ctx)
	if err != nil {
		return s.block(err)
	}
	s.publishStatus("local-apply")
	err = s.applied.Advance(ctx, reader, proposal.Selection)
	closeErr := reader.Close()
	if err != nil {
		return s.block(err)
	}
	if closeErr != nil {
		return s.block(closeErr)
	}
	s.selection = cloneSelection(proposal.Selection)
	s.publishStatus("")
	return nil
}
func (s *Service) append(ctx context.Context, id fsapi.UUID, p any) (segment.Entry, error) {
	r := record.Record{Header: s.header(id), Payload: p}
	receipt, err := s.writer.Append(ctx, r)
	if err != nil {
		return segment.Entry{}, err
	}
	s.observedSource = receipt.Reference.Position
	r.Header.Sequence = receipt.Reference.Position.Sequence
	raw, err := record.Encode(r)
	if err != nil {
		return segment.Entry{}, err
	}
	// Decode snapshots every pointer/slice from planning before handing bytes to
	// an external protocol implementation.
	owned, err := record.Decode(raw)
	if err != nil {
		return segment.Entry{}, err
	}
	return segment.Entry{Bytes: raw, Record: owned, Reference: receipt.Reference}, nil
}
func (s *Service) header(id fsapi.UUID) record.Header {
	return record.Header{Volume: s.config.Stream.Volume, Generation: s.config.Stream.Generation, Configuration: s.config.Stream.Configuration, Epoch: s.config.Stream.Epoch, Operation: id, Incarnation: s.config.Authority.OwnerIncarnation, Observed: s.config.Clock.Now()}
}
func (s *Service) block(err error) error {
	s.setState(Blocked, "operation-failed-preserve-state")
	return err
}
func cloneSelection(v apply.Selection) apply.Selection {
	v.Effects = slices.Clone(v.Effects)
	v.Proof = slices.Clone(v.Proof)
	return v
}
func errno(err error) fsapi.Errno {
	var code fsapi.Errno
	if errors.As(err, &code) && code != fsapi.OK {
		return code
	}
	if errors.Is(err, fs.ErrNotExist) {
		return fsapi.NotFound
	}
	if errors.Is(err, segment.ErrLimit) {
		return fsapi.NoSpace
	}
	return fsapi.IO
}
func configure(c *Config) error {
	if c.Validation != nil {
		if !c.Validation.Disposable || c.Validation.Name == "" || c.Protocol != nil {
			return fsapi.Invalid
		}
		v := *c.Validation
		c.Validation = &v
	} else if c.Protocol == nil || c.Permissions == nil {
		return fmt.Errorf("%w: explicit authority/commit and permission policies required", fsapi.Unavailable)
	}
	c.Authority.Members = slices.Clone(c.Authority.Members)
	a := c.Authority
	if !a.Owner.Valid() || !a.Partner.Valid() || a.Owner == a.Partner || !a.OwnerIncarnation.Valid() || !a.PartnerIncarnation.Valid() || a.CoordinatorTerm == 0 || len(a.Members) < 3 || len(a.Members) > 4096 || !slices.Contains(a.Members, a.Owner) || !slices.Contains(a.Members, a.Partner) {
		return fsapi.Invalid
	}
	for i, m := range a.Members {
		if !m.Valid() || i > 0 && bytes.Compare(a.Members[i-1][:], m[:]) >= 0 {
			return fsapi.Invalid
		}
	}

	if c.Stream.Kind != 1 || c.Stream.Volume != c.Storage.Volume || c.Stream.Generation != c.Authority.Generation || c.Stream.Configuration != c.Authority.Configuration || c.Stream.Epoch != c.Authority.Epoch || c.Authority.Volume != c.Storage.Volume || c.Storage.Root.Home != c.Storage.Volume || !c.Workspace.ID.Valid() || c.Storage.Closure != nil {
		return fsapi.Invalid
	}
	if c.Clock == nil {
		c.Clock = fsapi.WallClock{}
	}
	if c.Random == nil {
		c.Random = rand.Reader
	}
	l := &c.Limits
	if l.Queue == 0 {
		l.Queue = 64
	}
	if l.IOBytes == 0 {
		l.IOBytes = 1 << 20
	}
	if l.Handles == 0 {
		l.Handles = 1024
	}
	if l.Locks == 0 {
		l.Locks = 4096
	}
	if l.Locks < 1 || l.Locks > 1<<20 {
		return fsapi.Invalid
	}
	if l.Lookups == 0 {
		l.Lookups = 4096
	}
	if l.Reservations == 0 {
		l.Reservations = 4096
	}
	if l.LogicalBytes == 0 {
		l.LogicalBytes = 64 << 20
	}
	if l.Queue < 1 || l.Queue > 4096 || l.IOBytes < 1 || l.IOBytes > 1<<20 || l.Handles < 1 || l.Lookups < 1 || l.Reservations < 1 || l.Reservations > 1<<20 || l.Handles > 1<<20 || l.Lookups > 1<<20 || l.LogicalBytes < 1 {
		return fsapi.Invalid
	}
	if c.Apply.MaxFrames == 0 {
		c.Apply.MaxFrames = 4096
	}
	if c.Apply.MaxBytes == 0 {
		c.Apply.MaxBytes = 64 << 20
	}
	if c.Apply.MaxFrames < 1 || c.Apply.MaxFrames > 1<<20 || c.Apply.MaxBytes < 1 || c.Apply.MaxBytes > 1<<30 || c.Segment.Recovery.PreviousSeal != nil || c.Segment.Recovery.Repair != nil {
		return fsapi.Invalid
	}
	if c.Segment.MaximumBytes == 0 {
		c.Segment.MaximumBytes = 16 << 20
	}
	c.Selection = cloneSelection(c.Selection)
	return nil
}

type validationAuthorizer struct{}

func (validationAuthorizer) Authorize(_ context.Context, a apply.Authorization) error {
	if string(a.Selection.Proof) != "disposable-local-validation-v1" {
		return fsapi.Permission
	}
	return nil
}
func (s *Service) authorizer() apply.Authorizer {
	if s.config.Validation != nil {
		return validationAuthorizer{}
	}
	return s.config.Protocol
}

func cloneProposal(p Proposal) Proposal {
	p.Selection = cloneSelection(p.Selection)
	p.Frames = slices.Clone(p.Frames)
	for i := range p.Frames {
		p.Frames[i].Bytes = slices.Clone(p.Frames[i].Bytes)
		r, err := record.Decode(p.Frames[i].Bytes)
		if err == nil {
			p.Frames[i].Record = r
		}
	}
	return p
}
