//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

const guestArtifactLimit = 128 << 10

// GuestArtifactBudget specifies separate finite guest bookkeeping capacities.
type GuestArtifactBudget struct {
	OrdinaryBytes uint64 `json:"ordinary_bytes"`
	ControlBytes  uint64 `json:"control_bytes"`
	Reservations  uint64 `json:"reservations"`
	Cookies       uint64 `json:"cookies"`
	Received      uint64 `json:"received"`
}

// GuestArtifactMount is an explicit local mount identity, not an observation.
type GuestArtifactMount struct {
	DomainIndex      int    `json:"domain_index"`
	Session          string `json:"session"`
	DeviceTag        string `json:"device_tag"`
	DeviceName       string `json:"device_name"`
	UserNamespace    string `json:"user_namespace"`
	IDMapHash        string `json:"idmap_hash"`
	Realm            string `json:"realm"`
	Subject          string `json:"subject"`
	Anchor           string `json:"anchor"`
	Nonce            string `json:"nonce"`
	ViewControlNonce string `json:"view_control_nonce"`
}

// GuestArtifactSigners holds one original domain's ordered signing keys.
type GuestArtifactSigners struct{ Host, Guest ed25519.PrivateKey }

// GuestProvisionGroup retains the complete original authorization and domain order.
type GuestProvisionGroup struct {
	Authorization viewcontrol.Authorization
	LocalMounts   []GuestArtifactMount
	Signers       []GuestArtifactSigners
}

type guestArtifactSignatures struct {
	Host  string `json:"host"`
	Guest string `json:"guest"`
}
type guestArtifactGroup struct {
	EnrollmentBytes             string                    `json:"enrollment_bytes"`
	EnrollmentOperatorSignature string                    `json:"enrollment_operator_signature"`
	EnrollmentDomainSignatures  []guestArtifactSignatures `json:"enrollment_domain_signatures"`
	LocalMounts                 []GuestArtifactMount      `json:"local_mounts"`
}
type guestProvisionPayload struct {
	Version       string               `json:"version"`
	BootID        string               `json:"boot_id"`
	ProviderNonce string               `json:"provider_nonce"`
	Groups        []guestArtifactGroup `json:"groups"`
	Budget        GuestArtifactBudget  `json:"budget"`
}
type guestProvisionArtifact struct {
	Payload           guestProvisionPayload       `json:"payload"`
	OperatorSignature string                      `json:"operator_signature"`
	GroupSignatures   [][]guestArtifactSignatures `json:"group_signatures"`
}
type guestBindPayload struct {
	Version       string `json:"version"`
	BootID        string `json:"boot_id"`
	ProviderNonce string `json:"provider_nonce"`
	ProvisionHash string `json:"provision_hash"`
	GroupID       string `json:"group_id"`
	Controller    string `json:"controller"`
}
type guestBindArtifact struct {
	Payload           guestBindPayload          `json:"payload"`
	OperatorSignature string                    `json:"operator_signature"`
	DomainSignatures  []guestArtifactSignatures `json:"domain_signatures"`
}
type guestArtifactInventory struct {
	ids, sessions, tags, names map[string]bool
	vm                         string
	count                      int
}

// SignGuestProvision signs inert administrative bytes. It grants no live guest,
// source, received-response, mount or physical authority.
func SignGuestProvision(bootID, providerNonce string, groups []GuestProvisionGroup, budget GuestArtifactBudget, operator ed25519.PrivateKey) ([]byte, error) {
	pub, err := guestPrivateKey(operator, nil)
	if err != nil {
		return nil, err
	}
	if !validUUIDString(bootID) || !validUUIDString(providerNonce) || len(groups) < 1 || len(groups) > 64 || !guestBudgetValid(budget) {
		return nil, fsapi.Invalid
	}
	payload := guestProvisionPayload{"homelabfs-guest-provision/1", bootID, providerNonce, make([]guestArtifactGroup, len(groups)), budget}
	inventory := guestArtifactInventory{ids: map[string]bool{}, sessions: map[string]bool{}, tags: map[string]bool{}, names: map[string]bool{}}
	for i, g := range groups {
		raw, err := guestAuthorization(g.Authorization, pub)
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", i, err)
		}
		if err = inventory.check(g.Authorization.Enrollment, g.LocalMounts, bootID); err != nil {
			return nil, err
		}
		payload.Groups[i] = guestArtifactGroup{hex.EncodeToString(raw), hex.EncodeToString(g.Authorization.Operator), guestOriginalSignatures(g.Authorization.Domains), g.LocalMounts}
	}
	raw, err := canonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	result := guestProvisionArtifact{Payload: payload, OperatorSignature: hex.EncodeToString(ed25519.Sign(operator, raw)), GroupSignatures: make([][]guestArtifactSignatures, len(groups))}
	for i, g := range groups {
		result.GroupSignatures[i], err = guestSignDomains(raw, g.Authorization.Enrollment, g.Signers)
		if err != nil {
			return nil, err
		}
	}
	return guestArtifactBytes(result)
}

// SignGuestBind verifies an exact emitted provision and signs one group's inert
// controller binding. The hash always covers the complete provision payload.
func SignGuestBind(provision []byte, groupID, controller string, operator ed25519.PrivateKey, signers []GuestArtifactSigners) ([]byte, error) {
	pub, err := guestPrivateKey(operator, nil)
	if err != nil {
		return nil, err
	}
	if len(provision) > guestArtifactLimit || !validUUIDString(groupID) || !validUUIDString(controller) {
		return nil, fsapi.Invalid
	}
	var artifact guestProvisionArtifact
	if err = strictJSON(provision, &artifact); err != nil {
		return nil, err
	}
	exact, err := canonicalJSON(artifact)
	if err != nil || !bytes.Equal(exact, provision) {
		return nil, fsapi.Invalid
	}
	payload, err := canonicalJSON(artifact.Payload)
	if err != nil {
		return nil, err
	}
	if !guestVerify(pub, payload, artifact.OperatorSignature) || len(artifact.GroupSignatures) != len(artifact.Payload.Groups) {
		return nil, fsapi.Permission
	}
	groups := make([]GuestProvisionGroup, len(artifact.Payload.Groups))
	selected := -1
	for i, g := range artifact.Payload.Groups {
		a, err := guestDecodeAuthorization(g)
		if err != nil {
			return nil, err
		}
		if _, err = guestAuthorization(a, pub); err != nil {
			return nil, err
		}
		if !guestVerifyDomains(payload, a.Enrollment, artifact.GroupSignatures[i]) {
			return nil, fsapi.Permission
		}
		groups[i] = GuestProvisionGroup{Authorization: a, LocalMounts: g.LocalMounts}
		if a.Enrollment.ID.String() == groupID {
			selected = i
		}
	}
	p := artifact.Payload
	if p.Version != "homelabfs-guest-provision/1" || !validUUIDString(p.BootID) || !validUUIDString(p.ProviderNonce) || len(groups) < 1 || len(groups) > 64 || !guestBudgetValid(p.Budget) || selected < 0 {
		return nil, fsapi.Invalid
	}
	inventory := guestArtifactInventory{ids: map[string]bool{}, sessions: map[string]bool{}, tags: map[string]bool{}, names: map[string]bool{}}
	for _, g := range groups {
		if err = inventory.check(g.Authorization.Enrollment, g.LocalMounts, p.BootID); err != nil {
			return nil, err
		}
	}
	binding := guestBindPayload{"homelabfs-guest-bind/1", p.BootID, p.ProviderNonce, hashBytes(payload), groupID, controller}
	raw, err := canonicalJSON(binding)
	if err != nil {
		return nil, err
	}
	signatures, err := guestSignDomains(raw, groups[selected].Authorization.Enrollment, signers)
	if err != nil {
		return nil, err
	}
	return guestArtifactBytes(guestBindArtifact{binding, hex.EncodeToString(ed25519.Sign(operator, raw)), signatures})
}

func guestPrivateKey(key ed25519.PrivateKey, expected []byte) (ed25519.PublicKey, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fsapi.Invalid
	}
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	if !bytes.Equal(key, derived) || (expected != nil && !bytes.Equal(derived[32:], expected)) {
		return nil, fsapi.Permission
	}
	return ed25519.PublicKey(derived[32:]), nil
}
func guestBudgetValid(b GuestArtifactBudget) bool {
	return b.OrdinaryBytes > 0 && b.OrdinaryBytes <= 1<<30 && b.ControlBytes > 0 && b.ControlBytes <= 1<<30 && b.OrdinaryBytes+b.ControlBytes <= 1<<30 && b.Reservations > 0 && b.Reservations <= 8192 && b.Cookies > 0 && b.Cookies <= 4096 && b.Received > 0 && b.Received <= 4096
}
func guestArtifactBytes(v any) ([]byte, error) {
	raw, err := canonicalJSON(v)
	if err == nil && len(raw) > guestArtifactLimit {
		return nil, fsapi.NoSpace
	}
	return raw, err
}
func guestOriginalSignatures(signatures []viewcontrol.Signatures) []guestArtifactSignatures {
	result := make([]guestArtifactSignatures, len(signatures))
	for i, s := range signatures {
		result[i] = guestArtifactSignatures{hex.EncodeToString(s.Host), hex.EncodeToString(s.Guest)}
	}
	return result
}
func guestSignDomains(raw []byte, e viewcontrol.Enrollment, keys []GuestArtifactSigners) ([]guestArtifactSignatures, error) {
	if len(keys) != len(e.Domains) {
		return nil, fsapi.Invalid
	}
	result := make([]guestArtifactSignatures, len(keys))
	for i, k := range keys {
		if _, err := guestPrivateKey(k.Host, e.Domains[i].HostKey); err != nil {
			return nil, err
		}
		if _, err := guestPrivateKey(k.Guest, e.Domains[i].GuestKey); err != nil {
			return nil, err
		}
		result[i] = guestArtifactSignatures{hex.EncodeToString(ed25519.Sign(k.Host, raw)), hex.EncodeToString(ed25519.Sign(k.Guest, raw))}
	}
	return result, nil
}
func guestVerify(key []byte, raw []byte, signature string) bool {
	sig, err := hex.DecodeString(signature)
	return err == nil && len(sig) == ed25519.SignatureSize && hex.EncodeToString(sig) == signature && len(key) == ed25519.PublicKeySize && ed25519.Verify(key, raw, sig)
}
func guestVerifyDomains(raw []byte, e viewcontrol.Enrollment, signatures []guestArtifactSignatures) bool {
	if len(signatures) != len(e.Domains) {
		return false
	}
	for i, s := range signatures {
		if !guestVerify(e.Domains[i].HostKey, raw, s.Host) || !guestVerify(e.Domains[i].GuestKey, raw, s.Guest) {
			return false
		}
	}
	return true
}
func guestDecodeAuthorization(g guestArtifactGroup) (viewcontrol.Authorization, error) {
	var a viewcontrol.Authorization
	raw, err := hex.DecodeString(g.EnrollmentBytes)
	if err != nil || hex.EncodeToString(raw) != g.EnrollmentBytes {
		return a, fsapi.Invalid
	}
	if err = json.Unmarshal(raw, &a.Enrollment); err != nil {
		return a, err
	}
	exact, err := json.Marshal(a.Enrollment)
	if err != nil || !bytes.Equal(exact, raw) {
		return a, fsapi.Invalid
	}
	a.Operator, err = hex.DecodeString(g.EnrollmentOperatorSignature)
	if err != nil || hex.EncodeToString(a.Operator) != g.EnrollmentOperatorSignature {
		return a, fsapi.Invalid
	}
	a.Domains = make([]viewcontrol.Signatures, len(g.EnrollmentDomainSignatures))
	for i, s := range g.EnrollmentDomainSignatures {
		a.Domains[i].Host, err = hex.DecodeString(s.Host)
		if err != nil || hex.EncodeToString(a.Domains[i].Host) != s.Host {
			return a, fsapi.Invalid
		}
		a.Domains[i].Guest, err = hex.DecodeString(s.Guest)
		if err != nil || hex.EncodeToString(a.Domains[i].Guest) != s.Guest {
			return a, fsapi.Invalid
		}
	}
	return a, nil
}
func guestAuthorization(a viewcontrol.Authorization, operator ed25519.PublicKey) ([]byte, error) {
	e := a.Enrollment
	if e.Version != viewcontrol.EnrollmentVersion || !e.ID.Valid() || e.PageSize != 4096 || !guestBaselineValid(e) || !guestViewBounds(e.Bounds) || len(e.Domains) < 1 || len(e.Domains) > 64 || len(e.Domains) != len(a.Domains) || len(e.TicketFamilies) != len(e.Domains) {
		return nil, fsapi.Invalid
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	if len(raw) > guestArtifactLimit || !ed25519.Verify(operator, raw, a.Operator) {
		return nil, fsapi.Permission
	}
	seen := map[string]bool{}
	for i, d := range e.Domains {
		if err = guestDomainValid(d); err != nil {
			return nil, err
		}
		f := e.TicketFamilies[i]
		for _, id := range []string{f.Acquisition.Anchor, f.Acquisition.Nonce, f.Control.Anchor, f.Control.Nonce} {
			if !validUUIDString(id) {
				return nil, fsapi.Invalid
			}
		}
		if f.Acquisition == f.Control || seen[d.Scope.Session] || !ed25519.Verify(d.HostKey, raw, a.Domains[i].Host) || !ed25519.Verify(d.GuestKey, raw, a.Domains[i].Guest) {
			return nil, fsapi.Permission
		}
		seen[d.Scope.Session] = true
	}
	return raw, nil
}
func guestDomainValid(d controlledvalidation.Enrollment) error {
	if !d.Profile.Disposable || !guestProfileName(d.Profile.Name) || !d.Ledger.Valid() || !d.Journal.Owner.Valid() || !d.Journal.Incarnation.Valid() || !d.Journal.Stream.Volume.Valid() || !d.Journal.Stream.Generation.Valid() || !d.Journal.Stream.Configuration.Valid() || d.Journal.Stream.Epoch == 0 || d.Journal.Stream.Kind < 1 || d.Journal.Stream.Kind > 2 || d.Protocol == nil || *d.Protocol != guestbridge.MappedProtocol() || len(d.HostKey) != 32 || len(d.GuestKey) != 32 || bytes.Equal(d.HostKey, d.GuestKey) {
		return fsapi.Invalid
	}
	raw, err := json.Marshal(d.Scope)
	if err != nil {
		return err
	}
	var fields map[string]string
	if err = json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for k, v := range fields {
		if k == "authority_hash" || k == "policy_hash" || k == "registry_hash" {
			if !guestHash(v) {
				return fsapi.Invalid
			}
		} else if !validUUIDString(v) {
			return fsapi.Invalid
		}
	}
	ep := d.Endpoint
	if !d.ServingGeneration.Valid() || ep.FuseSession != d.Scope.FuseSession || ep.CommandNode <= 1 || ep.ResultNode <= 1 || ep.RecallNode <= 1 || ep.CommandNode == ep.ResultNode || ep.CommandNode == ep.RecallNode || ep.ResultNode == ep.RecallNode {
		return fsapi.Invalid
	}
	return nil
}
func guestHash(s string) bool { return len(s) == 64 && isHex(s) }
func guestDevice(s string) bool {
	if len(s) < 1 || len(s) > 255 {
		return false
	}
	for _, b := range []byte(s) {
		if b < 0x20 || b > 0x7e || b == '/' {
			return false
		}
	}
	return true
}
func (inventory *guestArtifactInventory) check(e viewcontrol.Enrollment, mounts []GuestArtifactMount, boot string) error {
	if inventory.ids[e.ID.String()] || len(mounts) < 1 {
		return fsapi.Invalid
	}
	inventory.ids[e.ID.String()] = true
	local := 0
	for i, d := range e.Domains {
		if d.Scope.GuestBoot != boot {
			continue
		}
		if local >= len(mounts) {
			return fsapi.Invalid
		}
		m := mounts[local]
		local++
		if m.DomainIndex != i || m.Session != d.Scope.Session || !guestDevice(m.DeviceTag) || !guestDevice(m.DeviceName) || !guestHash(m.IDMapHash) || len(m.Subject) < 2 || len(m.Subject) > 2048 || !isHex(m.Subject) {
			return fsapi.Invalid
		}
		for _, id := range []string{m.UserNamespace, m.Realm, m.Anchor, m.Nonce, m.ViewControlNonce} {
			if !validUUIDString(id) {
				return fsapi.Invalid
			}
		}
		families := e.TicketFamilies[i]
		ordinary := viewcontrol.TicketFamily{Anchor: m.Anchor, Nonce: m.Nonce}
		if m.ViewControlNonce != families.Control.Nonce || ordinary == families.Acquisition || ordinary == families.Control {
			return fsapi.Invalid
		}
		if inventory.vm != "" && inventory.vm != d.Scope.VM {
			return fsapi.Invalid
		}
		inventory.vm = d.Scope.VM
		if inventory.sessions[m.Session] || inventory.tags[m.DeviceTag] || inventory.names[m.DeviceName] {
			return fsapi.Invalid
		}
		inventory.sessions[m.Session] = true
		inventory.tags[m.DeviceTag] = true
		inventory.names[m.DeviceName] = true
		inventory.count++
	}
	if local != len(mounts) || inventory.count > 64 {
		return fsapi.Invalid
	}
	return nil
}

func guestViewBounds(b viewcontrol.Bounds) bool {
	if b.Tickets < 1 || b.Tickets > 65536 || b.ControlTickets < 4 || b.ControlTickets > 16384 || b.Reservations < 1 || b.Reservations > 64 || b.Identities < b.Tickets+b.ControlTickets || b.Identities > 65536 || b.Sources < 2 || b.Sources > 128 || b.SourceBytes < 4096 || b.SourceBytes > 16<<20 || b.LedgerBytes < 0 || b.LedgerBytes > 512<<20 || b.WorkBytes < 1 || b.WorkBytes > 2<<30 {
		return false
	}
	needed := int64(b.Reservations)*(128<<10) + b.SourceBytes*int64(b.Sources)*3 + int64(b.Tickets+b.ControlTickets)*(128<<10) + int64(b.Identities)*(32<<10)
	return int64(b.LedgerBytes) >= needed
}

// guestProfileName mirrors the controlled guest's bounded private enrollment
// decoder. This local restriction does not change any network parser.
func guestProfileName(name string) bool {
	if len(name) < 1 || len(name) > 255 {
		return false
	}
	for _, b := range []byte(name) {
		if b < 0x20 || b > 0x7e || b == '<' || b == '>' || b == '&' {
			return false
		}
	}
	return true
}

func guestBaselineValid(e viewcontrol.Enrollment) bool {
	b := e.Baseline
	p := b.Position
	return b.StreamKind >= 1 && b.StreamKind <= 2 && (b.Reporter == nil || b.Reporter.Valid()) && (b.ReporterIncarnation == nil || b.ReporterIncarnation.Valid()) && p.Volume.Valid() && p.Generation.Valid() && p.Configuration.Valid() && p.Epoch > 0 && guestHash(e.BaselineStateHash)
}
