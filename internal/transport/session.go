package transport

import (
	"context"
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func (e *Endpoint) handshake(ctx context.Context, c *Conn, peer fsapi.UUID, class Class, dial bool) error {
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()
	var local, remote Frame
	var err error
	if dial {
		local, err = e.initial(c, peer, class)
		if err != nil {
			return err
		}
		if err = c.sendFrame(ctx, local); err != nil {
			return err
		}
		remote, err = c.readFrame(ctx, cfg.Limits.Message)
		if err != nil {
			return err
		}
	} else {
		remote, err = c.readFrame(ctx, cfg.Limits.Message)
		if err != nil {
			return err
		}
		local = Frame{Kind: Hello, Class: remote.Class, Connection: remote.Connection, Source: cfg.Identity.Node, SourceIncarnation: cfg.Identity.Incarnation, Destination: remote.Source, DestinationIncarnation: remote.SourceIncarnation, Correlation: remote.Correlation, Session: remote.Session, Body: encodeGreeting(greeting{cfg.Registry.Revision(), cfg.Limits, cfg.Identity.Cluster})}
	}
	if err = e.checkHello(c, local, remote); err != nil {
		return err
	}
	g, err := decodeGreeting(remote.Body)
	if err != nil {
		return err
	}
	if g.revision != cfg.Registry.Revision() || g.cluster != cfg.Identity.Cluster {
		return fmt.Errorf("registry revision mismatch")
	}
	negotiated := cfg.Limits.lower(g.limits)
	if !dial {
		if err = c.sendFrame(ctx, local); err != nil {
			return err
		}
	}
	var ok Frame
	if dial {
		ok, err = c.readFrame(ctx, cfg.Limits.Message)
		if err != nil {
			return err
		}
		if err = checkOK(local, remote, ok, negotiated); err != nil {
			return err
		}
	} else {
		sessionID := local.Session
		if local.Class == Control {
			sessionID, err = e.newUUID()
			if err != nil {
				return err
			}
		}
		ok = local
		ok.Kind = HelloOK
		ok.Session = sessionID
		ok.Body = encodeGreeting(greeting{cfg.Registry.Revision(), negotiated, cfg.Identity.Cluster})
		if err = c.sendFrame(ctx, ok); err != nil {
			return err
		}
	}
	confirm := ok
	confirm.Source, confirm.SourceIncarnation, confirm.Destination, confirm.DestinationIncarnation = local.Source, local.SourceIncarnation, remote.Source, remote.SourceIncarnation
	if dial {
		if err = c.sendFrame(ctx, confirm); err != nil {
			return err
		}
		final, err := c.readFrame(ctx, cfg.Limits.Message)
		if err != nil {
			return err
		}
		if !sameGreeting(final, ok) {
			return fmt.Errorf("session confirmation mismatch")
		}
	} else {
		ack, err := c.readFrame(ctx, cfg.Limits.Message)
		if err != nil {
			return err
		}
		expected := ok
		expected.Source, expected.SourceIncarnation, expected.Destination, expected.DestinationIncarnation = remote.Source, remote.SourceIncarnation, local.Source, local.SourceIncarnation
		if !sameGreeting(ack, expected) {
			return fmt.Errorf("session confirmation mismatch")
		}
	}
	if err = e.install(c, remote, ok.Session, negotiated); err != nil {
		return err
	}
	if !dial {
		if err = c.sendFrame(ctx, ok); err != nil {
			return err
		}
	}
	return nil
}
func (e *Endpoint) initial(c *Conn, peer fsapi.UUID, class Class) (Frame, error) {
	connection, err := e.newUUID()
	if err != nil {
		return Frame{}, err
	}
	correlation, err := e.newUUID()
	if err != nil {
		return Frame{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	id := e.cfg.Identity
	f := Frame{Kind: Hello, Class: class, Connection: connection, Source: id.Node, SourceIncarnation: id.Incarnation, Destination: peer, Correlation: correlation, Body: encodeGreeting(greeting{e.cfg.Registry.Revision(), e.cfg.Limits, e.cfg.Identity.Cluster})}
	if class != Control {
		s := e.sessions[peer]
		if s == nil {
			return Frame{}, fmt.Errorf("data connection requires a control session")
		}
		f.Session, f.DestinationIncarnation = s.id, s.peerInc
	}
	return f, nil
}
func (e *Endpoint) checkHello(c *Conn, local, remote Frame) error {
	if remote.Kind != Hello || remote.Class != local.Class || remote.Connection != local.Connection || remote.Correlation != local.Correlation || remote.Destination != local.Source || remote.Source != local.Destination || remote.Session != local.Session || remote.Binding.Valid() {
		return fmt.Errorf("handshake envelope mismatch")
	}
	if remote.DestinationIncarnation.Valid() && remote.DestinationIncarnation != local.SourceIncarnation {
		return fmt.Errorf("stale destination incarnation")
	}
	addr, err := observed(c.raw.RemoteAddr())
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.revoked[remote.Source] {
		return fmt.Errorf("revoked peer")
	}
	if err := e.cfg.Registry.CheckPeer(e.cfg.Identity.Cluster, remote.Source, addr.Addr()); err != nil {
		return err
	}
	// Claimed host identity is checked independently of the cluster greeting.
	if expected := e.incarnations[remote.Source]; expected.Valid() && expected != remote.SourceIncarnation {
		return fmt.Errorf("unexpected peer incarnation")
	}
	c.peer, c.peerInc, c.connection, c.class = remote.Source, remote.SourceIncarnation, remote.Connection, remote.Class
	if local.Class == Control {
		if local.Session.Valid() || e.sessions[remote.Source] != nil {
			return fmt.Errorf("duplicate control session")
		}
	} else {
		s := e.sessions[remote.Source]
		if s == nil || s.id != remote.Session || s.peerInc != remote.SourceIncarnation || remote.DestinationIncarnation != local.SourceIncarnation {
			return fmt.Errorf("unbound data session")
		}
	}
	return nil
}
func (e *Endpoint) install(c *Conn, remote Frame, id fsapi.UUID, limits Limits) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.revoked[remote.Source] || c.stopped() {
		return fmt.Errorf("session revoked during handshake")
	}
	if expected := e.incarnations[remote.Source]; expected.Valid() && expected != remote.SourceIncarnation {
		return fmt.Errorf("peer incarnation expectation changed")
	}
	e.incarnations[remote.Source] = remote.SourceIncarnation
	// A registry replacement closes every handshake socket before it can install.
	if c.class == Control {
		if e.sessions[c.peer] != nil {
			return fmt.Errorf("duplicate control session")
		}
		e.sessions[c.peer] = &session{id, c.peerInc, c}
	} else {
		s := e.sessions[c.peer]
		if s == nil || s.id != id || s.peerInc != c.peerInc {
			return fmt.Errorf("control session changed")
		}
	}
	c.session, c.limits = id, limits
	return nil
}
func checkOK(local, remote, ok Frame, limits Limits) error {
	expected := remote
	expected.Kind = HelloOK
	expected.Session = ok.Session
	expected.DestinationIncarnation = local.SourceIncarnation
	g, err := decodeGreeting(ok.Body)
	if err != nil {
		return err
	}
	rg, err := decodeGreeting(remote.Body)
	if err != nil {
		return err
	}
	if g.cluster != rg.cluster || g.revision != rg.revision || !ok.Session.Valid() || g.limits != limits || (local.Class != Control && ok.Session != local.Session) {
		return fmt.Errorf("session limits or identity mismatch")
	}
	expected.Body = encodeGreeting(greeting{g.revision, limits, g.cluster})
	if !sameGreeting(ok, expected) {
		return fmt.Errorf("HELLO_OK mismatch")
	}
	return nil
}
func sameGreeting(a, b Frame) bool {
	return a.Kind == b.Kind && a.Class == b.Class && a.Session == b.Session && a.Connection == b.Connection && a.Source == b.Source && a.SourceIncarnation == b.SourceIncarnation && a.Destination == b.Destination && a.DestinationIncarnation == b.DestinationIncarnation && a.Correlation == b.Correlation && a.Binding == b.Binding && string(a.Body) == string(b.Body)
}
