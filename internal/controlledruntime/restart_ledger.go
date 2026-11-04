//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type exporterReader struct {
	reader io.Reader
	err    error
}
type exporterSession struct {
	scope     guestbridge.Context
	high      uint64
	minor     uint64
	terminal  uint64
	finalCell uint64
}
type exporterOld struct {
	guest, native uint64
	control       bool
	hashes        [5]string
}

func (b *exporterReader) take(n int) []byte {
	if b.err != nil {
		return nil
	}
	if n < 0 || n > guestbridge.BodyBytes {
		b.err = fsapi.NoSpace
		return nil
	}
	p := make([]byte, n)
	_, b.err = io.ReadFull(b.reader, p)
	return p
}
func (b *exporterReader) number(n int) uint64 {
	p := b.take(n)
	var value uint64
	for i, c := range p {
		value |= uint64(c) << uint(8*i)
	}
	return value
}
func (b *exporterReader) blob() []byte {
	n := b.number(4)
	if n > guestbridge.BodyBytes {
		b.err = fsapi.NoSpace
		return nil
	}
	return b.take(int(n))
}
func (b *exporterReader) hash() string { return hex.EncodeToString(b.take(32)) }
func (b *exporterReader) uuid() string {
	var id fsapi.UUID
	copy(id[:], b.take(16))
	return id.String()
}
func decodeHash(value string) ([]byte, error) {
	p, err := hex.DecodeString(value)
	if err != nil || len(p) != 32 {
		return nil, fsapi.Stale
	}
	return p, nil
}

// reconcileExporter consumes the actual same-child, paused, protected-file-equal
// binary snapshot. The checksum authenticates no authority; source provenance is
// held by the private inherited recovery channel and surviving native witness.
func reconcileExporter(file *os.File, total int64, digest [32]byte, index guestbridge.ProtectedTransportState, cells map[uint64]guestbridge.ProtectedTransportCell, helloState map[string]bool) error {
	if total < 64 || total > exporterRestartLimit {
		return fsapi.Stale
	}
	prefix := io.NewSectionReader(file, 0, total-32)
	hash := sha256.New()
	b := &exporterReader{reader: io.TeeReader(prefix, hash)}
	format := string(b.take(8))
	terminal := format == "H9LEDG3\x00"
	if format != "H9LEDG2\x00" && !terminal {
		return fsapi.Unavailable
	}
	if b.uuid() != index.Endpoint.FuseSession {
		return fsapi.Stale
	}
	for _, node := range []uint64{index.Endpoint.CommandNode, index.Endpoint.ResultNode, index.Endpoint.RecallNode} {
		if b.number(8) != node {
			return fsapi.Stale
		}
	}
	if b.number(8) != index.High || b.number(4) != uint64(0xffffffff) {
		return fsapi.Unavailable
	}
	count := b.number(1)
	if count != uint64(len(index.Scopes)) || count > 16 {
		return fsapi.Stale
	}
	sessions := make([]exporterSession, 0, count)
	for i := uint64(0); i < count; i++ {
		id := b.uuid()
		raw := b.blob()
		var scope guestbridge.Context
		if json.Unmarshal(raw, &scope) != nil || scope.Session != id {
			return fsapi.Stale
		}
		found := -1
		for k, s := range index.Scopes {
			if s == scope {
				found = k
			}
		}
		if found < 0 {
			return fsapi.Stale
		}
		for _, old := range sessions {
			if old.scope.Session == id {
				return fsapi.Stale
			}
		}
		session := exporterSession{scope: scope, high: b.number(8)}
		if session.high != index.GuestHigh[id] {
			return fsapi.Stale
		}
		ranges := b.number(2)
		if ranges > 256 {
			return fsapi.Stale
		}
		var last uint64
		for k := uint64(0); k < ranges; k++ {
			first, end := b.number(8), b.number(8)
			if first == 0 || first > end || end > session.high || k > 0 && first <= last+1 {
				return fsapi.Stale
			}
			last = end
		}
		if last != session.high {
			return fsapi.Stale
		}
		hello := b.number(1)
		if hello > 1 || (hello == 1) != helloState[id] {
			return fsapi.Stale
		}
		session.minor = b.number(1)
		protocol := index.Protocols[found]
		if protocol == nil || session.minor != uint64(protocol.BridgeMinor) || !guestbridge.IsMappedProtocol(*protocol) {
			return fsapi.Unavailable
		}
		if terminal {
			features := b.number(4)
			controller := b.uuid()
			reserve, used, remaining := b.number(4), b.number(4), b.number(4)
			if features != uint64(protocol.Features) || features == 32767 && controller != index.Controller.String() || used > reserve || remaining != reserve-used {
				return fsapi.Stale
			}
			session.terminal = b.number(1)
			guest := b.number(8)
			session.finalCell = b.number(8)
			if session.terminal != 0 && session.terminal != 3 || (session.terminal == 3) != (index.Terminal[id] != 0) || session.finalCell != index.Terminal[id] || session.terminal == 3 && guest != session.high {
				return fsapi.Unavailable
			}
		} else if index.Terminal[id] != 0 {
			return fsapi.Stale
		}
		sessions = append(sessions, session)
	}
	seen := map[uint64]bool{}
	ordinary, control := 0, 0
	for slot := 0; slot < 97; slot++ {
		present := b.number(1)
		if present == 0 {
			continue
		}
		if present != 1 {
			return fsapi.Stale
		}
		guest, native := b.number(8), b.number(8)
		chunks, received, fetched := b.number(4), b.number(4), b.number(4)
		flags, sessionID, kind, bits := b.number(2), b.number(1), b.number(1), b.number(4)
		if sessionID >= uint64(len(sessions)) || bits&11 != 11 || chunks > 32 || flags > 3 {
			return fsapi.Unavailable
		}
		_ = received
		_ = fetched
		session := sessions[sessionID]
		host, ok := cells[native]
		if !ok || seen[native] || host.Command == nil || host.Command.Context != session.scope || host.Mapping == nil || host.Mapping.GuestCell != guest || host.Header.Flags != uint16(flags) || host.ResultKind != uint16(kind) || host.JobPhase != "responded" {
			return fsapi.Stale
		}
		seen[native] = true
		isControl := bits&32 != 0
		if isControl != host.Control || (flags&1 != 0) != isControl {
			return fsapi.Stale
		}
		if isControl {
			control++
		} else {
			ordinary++
		}
		var old []exporterOld
		var funding, oldOrdinary, oldControl uint64
		var removed bool
		var settled string
		if terminal {
			watermark := b.number(8)
			if watermark != host.GuestWatermark {
				return fsapi.Stale
			}
			manifest := b.number(1)
			if manifest > 1 {
				return fsapi.Stale
			}
			if manifest == 1 {
				n := b.number(1)
				funding = b.number(1)
				oldOrdinary = b.number(1)
				oldControl = b.number(1)
				removed = b.number(1) == 1
				settled = b.hash()
				if n > 80 {
					return fsapi.Stale
				}
				for k := uint64(0); k < n; k++ {
					entry := exporterOld{guest: b.number(8), native: b.number(8)}
					oldSlot := b.number(1)
					class := b.number(1)
					entry.control = class == 1
					if oldSlot >= 97 || class > 1 {
						return fsapi.Stale
					}
					for h := range entry.hashes {
						entry.hashes[h] = b.hash()
					}
					old = append(old, entry)
				}
			}
			if (manifest == 1) != (host.Terminal != nil) {
				return fsapi.Stale
			}
		}
		donor := b.number(4)
		b.number(8)
		b.number(8)
		pollCount := b.number(1)
		guestHash, nativeHash, responseGuestHash, responseNativeHash := b.hash(), b.hash(), b.hash(), b.hash()
		guestBody, nativeBody, guestResponse, nativeResponse := b.blob(), b.blob(), b.blob(), b.blob()
		polls := b.number(1)
		// Multi-leg asynchronous WAIT reconstruction remains explicitly unsupported.
		if donor != 0xffffffff || polls != 0 || pollCount > 1 || host.Mapping.PollIndex != 0 {
			return fsapi.Unavailable
		}
		if len(nativeBody) == 0 || len(guestBody) == 0 || len(nativeResponse) == 0 || len(guestResponse) == 0 || !bytes.Equal(nativeBody, host.Body) || !bytes.Equal(nativeResponse, host.Response) || guestHash != hashBytes(guestBody) || guestHash != host.Mapping.GuestCommandHash || nativeHash != hashBytes(nativeBody) || responseNativeHash != hashBytes(nativeResponse) || responseNativeHash != host.Mapping.NativeFullHash || responseGuestHash != hashBytes(guestResponse) || responseGuestHash != host.Mapping.GuestFullHash {
			return fsapi.Stale
		}
		original, err := guestbridge.DecodeCommandVersion(guestBody, uint16(session.minor))
		if err != nil || original.Cell != guest || original.Context != session.scope || original.Delegation != nil || original.Op != host.Mapping.GuestOp {
			return fsapi.Stale
		}
		if host.Terminal != nil {
			if bits&1024 == 0 || host.Terminal.Phase != "TERMINAL_RESPONDED" || host.Terminal.Ownership == nil || !removed || int(oldOrdinary) != host.Terminal.OldOrdinary || int(oldControl) != host.Terminal.OldControl {
				return fsapi.Stale
			}
			donors := append([]guestbridge.MappingDonor(nil), host.Terminal.Donors...)
			classes := append([]bool(nil), host.Terminal.Controls...)
			if host.Mapping.Donor != nil {
				donors = append(donors, *host.Mapping.Donor)
				classes = append(classes, host.Control)
			}
			if funding != 255 && (funding >= uint64(len(old)) || host.Mapping.Donor == nil || old[funding].native != host.Mapping.Donor.NativeCell) {
				return fsapi.Stale
			}
			if len(old) != len(donors) || (funding == 255) != (host.Mapping.Donor == nil) || settled != host.Terminal.Ownership.SettledHash {
				return fsapi.Stale
			}
			for _, record := range old {
				match := -1
				for k, d := range donors {
					if d.NativeCell == record.native {
						match = k
					}
				}
				if match < 0 {
					return fsapi.Stale
				}
				d := donors[match]
				if d.GuestCell != record.guest || d.GuestCommandHash != record.hashes[0] || d.MappingHash != record.hashes[2] || d.GuestFullHash != record.hashes[3] || d.NativeFullHash != record.hashes[4] || classes[match] != record.control {
					return fsapi.Stale
				}
				donors[match].NativeCell = 0
			}
		} else if bits&1024 != 0 {
			return fsapi.Stale
		}
	}
	if b.err != nil {
		return b.err
	}
	var extra [1]byte
	if n, err := prefix.Read(extra[:]); n != 0 || err != io.EOF {
		return fsapi.Stale
	}
	trailer := make([]byte, 32)
	if _, err := file.ReadAt(trailer, total-32); err != nil {
		return err
	}
	if !bytes.Equal(hash.Sum(nil), digest[:]) || !bytes.Equal(trailer, digest[:]) || len(seen) != len(cells) || ordinary != index.Ordinary || control != index.Control {
		return fsapi.Stale
	}
	return nil
}
