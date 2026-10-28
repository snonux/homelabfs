package fsservice

import (
	"context"
	"math"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type heldLock struct {
	object fsapi.ObjectID
	handle fsapi.OpenHandle
	lock   fsapi.Lock
}

func (s *Service) lockCommand(c fsapi.Command) (fsapi.Result, error) {
	result := fsapi.Result{Position: s.selection.Through.Position}
	o := c.Lock.Owner
	if !o.Valid() || o.Generation != s.config.Workspace.ID {
		return result, fsapi.Stale
	}
	switch c.Operation {
	case fsapi.CancelLock:
		return result, s.cancelLock(c.Lock)
	case fsapi.ProcessExit, fsapi.SessionExit:
		return result, s.exitLocks(c)
	case fsapi.DescriptorClose:
		if c.Handle == 0 {
			return result, fsapi.Stale
		}
		return result, s.closeProcessLocks(o, s.handles[c.Handle].object)
	}
	if s.owners[o] || s.sessions[o.Session] {
		return result, fsapi.Stale
	}
	if _, ok := s.owners[o]; !ok {
		if len(s.owners) >= s.config.Limits.Locks {
			return result, fsapi.NoSpace
		}
		s.owners[o] = false
	}
	h, ok := s.handles[c.Handle]
	if !ok {
		return result, fsapi.Stale
	}
	l := c.Lock
	if c.Operation == fsapi.Unlock && l.Kind == 0 {
		l.Kind = fsapi.SharedLock
	}
	if err := l.Validate(); err != nil {
		return result, err
	}
	if c.Operation != fsapi.Unlock && c.Operation != fsapi.GetLock && l.Domain == fsapi.POSIXLock && (l.Kind == fsapi.SharedLock && h.flags.Access == fsapi.WriteOnly || l.Kind == fsapi.ExclusiveLock && h.flags.Access == fsapi.ReadOnly) {
		return result, fsapi.Permission
	}
	request := heldLock{object: h.object, handle: c.Handle, lock: l}
	if l.Domain == fsapi.POSIXLock {
		request.handle = 0
	}
	if c.Operation != fsapi.Unlock {
		for _, held := range s.locks {
			if conflicts(request, held) {
				if c.Operation == fsapi.GetLock {
					result.Conflict = &fsapi.LockConflict{Lock: held.lock, Handle: held.handle}
					return result, nil
				}
				// flock conversion relinquishes the old lock before waiting, as BSD
				// flock does. POSIX range replacement is atomic on conflict.
				if l.Domain == fsapi.FlockLock {
					s.removeLocks(request)
				}
				return result, fsapi.WouldBlock
			}
		}
		if c.Operation == fsapi.GetLock {
			return result, nil
		}
	}
	previous := slices.Clone(s.locks)
	s.removeLocks(request)
	if c.Operation != fsapi.Unlock {
		request.lock.WaitID = fsapi.UUID{}
		s.addLock(request)
	}
	if len(s.locks) > s.config.Limits.Locks {
		s.locks = previous
		return result, fsapi.NoSpace
	}
	return result, nil
}
func (s *Service) cancelLock(l fsapi.Lock) error {
	if !l.WaitID.Valid() || s.waits[l.WaitID] != l.Owner {
		return fsapi.Stale
	}
	if cmd, ok := s.pending[l.WaitID]; ok {
		delete(s.pending, l.WaitID)
		s.finish(cmd, fsapi.Result{Code: fsapi.Interrupted})
	}
	return nil
}
func (s *Service) exitLocks(c fsapi.Command) error {
	o := c.Lock.Owner
	if c.Operation == fsapi.SessionExit {
		if _, ok := s.sessions[o.Session]; !ok && len(s.sessions) >= s.config.Limits.Locks {
			return fsapi.NoSpace
		}
		s.sessions[o.Session] = true
	} else {
		if _, ok := s.owners[o]; !ok && len(s.owners) >= s.config.Limits.Locks {
			return fsapi.NoSpace
		}
		s.owners[o] = true
	}
	matches := func(owner fsapi.LockOwner) bool {
		return owner == o || c.Operation == fsapi.SessionExit && owner.Session == o.Session
	}
	s.locks = slices.DeleteFunc(s.locks, func(l heldLock) bool { return l.lock.Domain == fsapi.POSIXLock && matches(l.lock.Owner) })
	for id, cmd := range s.pending {
		if matches(cmd.value.Lock.Owner) {
			delete(s.pending, id)
			s.finish(cmd, fsapi.Result{Code: fsapi.Interrupted})
		}
	}
	return nil
}
func (s *Service) retryLocks(ctx context.Context) {
	// Every retry rechecks generation, descriptor access, permission and authority.
	// No FIFO or deadlock-detection guarantee is made for blocking lock requests.
	for {
		completed := false
		for id, cmd := range s.pending {
			result := s.evaluate(ctx, cmd)
			if result.Code != fsapi.WouldBlock {
				delete(s.pending, id)
				s.finish(cmd, result)
				completed = true
			}
		}
		if !completed {
			return
		}
	}
}
func (s *Service) closeProcessLocks(owner fsapi.LockOwner, object fsapi.ObjectID) error {
	s.locks = slices.DeleteFunc(s.locks, func(l heldLock) bool {
		return l.object == object && l.lock.Domain == fsapi.POSIXLock && l.lock.Owner == owner
	})
	for id, cmd := range s.pending {
		if cmd.value.Lock.Domain == fsapi.POSIXLock && cmd.value.Lock.Owner == owner && s.handles[cmd.value.Handle].object == object {
			delete(s.pending, id)
			s.finish(cmd, fsapi.Result{Code: fsapi.Interrupted})
		}
	}
	return nil
}
func (s *Service) closeLocks(c fsapi.Command, object fsapi.ObjectID) error {
	if c.DescriptionOnly {
		s.locks = slices.DeleteFunc(s.locks, func(l heldLock) bool { return l.handle == c.Handle && l.lock.Domain == fsapi.FlockLock })
		for id, cmd := range s.pending {
			if cmd.value.Handle == c.Handle {
				delete(s.pending, id)
				s.finish(cmd, fsapi.Result{Code: fsapi.Interrupted})
			}
		}
		return nil
	}
	o := c.Lock.Owner
	if o.Valid() && o.Generation != s.config.Workspace.ID {
		return fsapi.Stale
	}
	for _, l := range s.locks {
		if l.object == object && l.lock.Domain == fsapi.POSIXLock && !o.Valid() {
			return fsapi.Invalid
		}
	}
	for _, cmd := range s.pending {
		if cmd.value.Lock.Domain == fsapi.POSIXLock && s.handles[cmd.value.Handle].object == object && !o.Valid() {
			return fsapi.Invalid
		}
	}
	s.locks = slices.DeleteFunc(s.locks, func(l heldLock) bool {
		return l.handle == c.Handle && l.lock.Domain == fsapi.FlockLock || l.object == object && l.lock.Domain == fsapi.POSIXLock && l.lock.Owner == o
	})
	for id, cmd := range s.pending {
		if cmd.value.Handle == c.Handle {
			delete(s.pending, id)
			s.finish(cmd, fsapi.Result{Code: fsapi.Interrupted})
		}
	}
	return s.closeProcessLocks(o, object)
}
func (s *Service) removeLocks(request heldLock) {
	var next []heldLock
	for _, l := range s.locks {
		if !sameHolder(request, l) || !overlap(request.lock, l.lock) {
			next = append(next, l)
			continue
		}
		if l.lock.Start < request.lock.Start {
			left := l
			left.lock.Length = request.lock.Start - l.lock.Start
			next = append(next, left)
		}
		if end(request.lock) < end(l.lock) {
			right := l
			right.lock.Start = int64(end(request.lock))
			if l.lock.Length != 0 {
				right.lock.Length = int64(end(l.lock)) - right.lock.Start
			}
			next = append(next, right)
		}
	}
	s.locks = next
}
func (s *Service) addLock(request heldLock) {
	// Coalesce touching ranges of the same owner/type before enforcing capacity.
	// The existing ranges are disjoint for each process, so a single pass suffices.
	next := s.locks[:0]
	for _, l := range s.locks {
		if sameHolder(l, request) && l.lock.Kind == request.lock.Kind && uint64(l.lock.Start) <= end(request.lock) && uint64(request.lock.Start) <= end(l.lock) {
			last := max(end(l.lock), end(request.lock))
			request.lock.Start = min(l.lock.Start, request.lock.Start)
			request.lock.Length = 0
			if last != math.MaxUint64 {
				request.lock.Length = int64(last) - request.lock.Start
			}
		} else {
			next = append(next, l)
		}
	}
	s.locks = append(next, request)
}
func sameHolder(a, b heldLock) bool {
	return a.object == b.object && a.lock.Domain == b.lock.Domain && (a.lock.Domain == fsapi.POSIXLock && a.lock.Owner == b.lock.Owner || a.lock.Domain == fsapi.FlockLock && a.handle == b.handle)
}
func conflicts(a, b heldLock) bool {
	return a.object == b.object && a.lock.Domain == b.lock.Domain && !sameHolder(a, b) && overlap(a.lock, b.lock) && (a.lock.Kind == fsapi.ExclusiveLock || b.lock.Kind == fsapi.ExclusiveLock)
}
func overlap(a, b fsapi.Lock) bool { return uint64(a.Start) < end(b) && uint64(b.Start) < end(a) }
func end(l fsapi.Lock) uint64 {
	if l.Length == 0 {
		return math.MaxUint64
	}
	return uint64(l.Start + l.Length)
}
