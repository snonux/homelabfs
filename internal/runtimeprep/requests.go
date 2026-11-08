//go:build linux || freebsd

package runtimeprep

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/retained9plab"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

var (
	// The guest provider reports dev_name(&vdev->dev) of the 9p virtio device.
	guestDeviceName = regexp.MustCompile(`^virtio(0|[1-9][0-9]{0,3})$`)
	// Same rule as the runtime's guest observer SSH destination check.
	observerHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)
)

// ObserverInput is the closed, hex-encoded form of the runtime's pinned
// guest observer (GuestObserver): host-side SSH executable, known-hosts file,
// run-only observer identity, guest address and installed-manifest hash.
type ObserverInput struct {
	Version                                     string
	SSH, KnownHosts, Identity, Host             string
	SSHSHA256, KnownHostsSHA256, ManifestSHA256 string
}

// EnrollmentInputs are the actual values that exist only after the runtime
// emitted AWAITING_OPERATOR_ENROLLMENT and the booted guest was measured.
type EnrollmentInputs struct {
	ProposalPath          string
	Proposal              viewcontrol.Enrollment
	BootID, ProviderNonce string
	DeviceNames           []string
	Observer              ObserverInput
}

// AwaitingGuestBind is the runtime's inert AWAITING_GUEST_BIND stdout record.
type AwaitingGuestBind struct {
	Stage         string          `json:"stage"`
	Controller    string          `json:"controller"`
	ProvisionHash string          `json:"provision_hash"`
	Publication   json.RawMessage `json:"publication"`
}

// FillEnrollment binds the template to the actual proposal and measurements.
// It refuses a proposal whose sessions, keys or volume differ from this
// generation, so a stale or foreign proposal cannot be signed by mistake.
func FillEnrollment(t RequestTemplate, in EnrollmentInputs) (retained9plab.EnrollmentRequest, error) {
	var r retained9plab.EnrollmentRequest
	if err := validateTemplate(t); err != nil {
		return r, err
	}
	if !cleanAbsolute(in.ProposalPath) || !validUUID(in.BootID) || !validUUID(in.ProviderNonce) {
		return r, fmt.Errorf("enrollment requires an absolute proposal path and actual boot/provider UUIDs")
	}
	if err := checkDeviceNames(in.DeviceNames); err != nil {
		return r, err
	}
	observer, err := observerFrom(in.Observer)
	if err != nil {
		return r, err
	}
	if err := matchProposal(t, in.Proposal, in.BootID); err != nil {
		return r, err
	}
	r = retained9plab.EnrollmentRequest{Proposal: in.ProposalPath, OperatorKey: t.OperatorKey,
		BootID: in.BootID, ProviderNonce: in.ProviderNonce,
		Keys: append([]retained9plab.DomainKeys(nil), t.Keys...), Budget: t.Budget, Observer: observer}
	for i, mount := range t.Mounts {
		mount.DeviceName = in.DeviceNames[i]
		mount.ViewControlNonce = in.Proposal.TicketFamilies[i].Control.Nonce
		r.Mounts = append(r.Mounts, mount)
	}
	return r, nil
}

// FillBind binds the template to the exact emitted provision and the actual
// controller announced at AWAITING_GUEST_BIND, whose provision hash must cover
// that same provision payload.
func FillBind(t RequestTemplate, provisionPath string, provision, awaiting []byte) (retained9plab.BindRequest, error) {
	var r retained9plab.BindRequest
	if err := validateTemplate(t); err != nil {
		return r, err
	}
	var record AwaitingGuestBind
	if err := decodeStrict(awaiting, &record); err != nil {
		return r, fmt.Errorf("AWAITING_GUEST_BIND record: %w", err)
	}
	if record.Stage != "AWAITING_GUEST_BIND" || string(record.Publication) != "null" || !validUUID(record.Controller) {
		return r, fmt.Errorf("record is not an inert AWAITING_GUEST_BIND with a controller UUID")
	}
	hash, enrollment, err := inspectProvision(provision)
	if err != nil {
		return r, err
	}
	if hash != record.ProvisionHash {
		return r, fmt.Errorf("provision payload hash %s differs from the runtime's %s", hash, record.ProvisionHash)
	}
	if err := matchDomains(t, enrollment); err != nil {
		return r, err
	}
	if !cleanAbsolute(provisionPath) {
		return r, fmt.Errorf("require the absolute provision path the signer will read")
	}
	return retained9plab.BindRequest{Provision: provisionPath, OperatorKey: t.OperatorKey,
		GroupID: enrollment.ID.String(), Controller: record.Controller,
		Keys: append([]retained9plab.DomainKeys(nil), t.Keys...)}, nil
}

// validateTemplate re-checks a template read back from disk.
func validateTemplate(t RequestTemplate) error {
	if t.Version != TemplateVersion || !runIDPattern.MatchString(t.RunID) || !validUUID(t.Volume) || !cleanAbsolute(t.OperatorKey) {
		return fmt.Errorf("request template identity invalid")
	}
	if len(t.Keys) != sessionCount || len(t.Mounts) != sessionCount ||
		len(t.HostPublicKeys) != sessionCount || len(t.GuestPublicKeys) != sessionCount {
		return fmt.Errorf("request template must describe exactly %d domains", sessionCount)
	}
	for i, k := range t.Keys {
		m := t.Mounts[i]
		if !validUUID(k.Session) || !cleanAbsolute(k.Host) || !cleanAbsolute(k.Guest) ||
			m.DomainIndex != i || m.Session != k.Session || m.DeviceName != "" || m.ViewControlNonce != "" {
			return fmt.Errorf("request template domain %d inconsistent", i)
		}
	}
	if t.Keys[0].Session == t.Keys[1].Session {
		return fmt.Errorf("request template repeats a session")
	}
	return nil
}

func checkDeviceNames(names []string) error {
	if len(names) != sessionCount || names[0] == names[1] {
		return fmt.Errorf("require %d distinct guest device names in domain order", sessionCount)
	}
	for _, name := range names {
		if !guestDeviceName.MatchString(name) {
			return fmt.Errorf("guest device name %q is not a virtio device name (virtioN)", name)
		}
	}
	return nil
}

func observerFrom(o ObserverInput) (controlledruntime.GuestObserver, error) {
	var g controlledruntime.GuestObserver
	if o.Version != ObserverVersion || !cleanAbsolute(o.SSH) || !cleanAbsolute(o.KnownHosts) ||
		!cleanAbsolute(o.Identity) || !observerHost.MatchString(o.Host) {
		return g, fmt.Errorf("observer input requires version %q, absolute paths and a plain host", ObserverVersion)
	}
	g = controlledruntime.GuestObserver{SSH: o.SSH, KnownHosts: o.KnownHosts, Identity: o.Identity, Host: o.Host}
	for _, field := range []struct {
		text string
		into *[32]byte
	}{{o.SSHSHA256, &g.SSHHash}, {o.KnownHostsSHA256, &g.KnownHostsHash}, {o.ManifestSHA256, &g.ManifestHash}} {
		raw, err := decodeHex(field.text, 32, "observer hash")
		if err != nil {
			return g, err
		}
		*field.into = [32]byte(raw)
	}
	if g.ManifestHash == ([32]byte{}) {
		return g, fmt.Errorf("observer manifest hash must be the actual installed-admission hash")
	}
	return g, nil
}

// matchProposal checks the proposal's domains and ticket families against the
// template. The runtime leaves GuestBoot empty for the signer to fill.
func matchProposal(t RequestTemplate, p viewcontrol.Enrollment, boot string) error {
	if err := matchDomains(t, p); err != nil {
		return err
	}
	if len(p.TicketFamilies) != sessionCount {
		return fmt.Errorf("proposal ticket families differ from its domains")
	}
	for i, d := range p.Domains {
		if d.Scope.GuestBoot != "" && d.Scope.GuestBoot != boot {
			return fmt.Errorf("proposal domain %d names a different guest boot", i)
		}
		families := p.TicketFamilies[i]
		ordinary := viewcontrol.TicketFamily{Anchor: t.Mounts[i].Anchor, Nonce: t.Mounts[i].Nonce}
		if !validUUID(families.Control.Nonce) || ordinary == families.Acquisition || ordinary == families.Control {
			return fmt.Errorf("proposal domain %d ticket families collide with the ordinary family", i)
		}
	}
	return nil
}

// matchDomains requires the exact generated sessions, keys and volume in order.
func matchDomains(t RequestTemplate, e viewcontrol.Enrollment) error {
	if len(e.Domains) != sessionCount {
		return fmt.Errorf("enrollment has %d domains; this generation has %d", len(e.Domains), sessionCount)
	}
	for i, d := range e.Domains {
		if d.Scope.Session != t.Keys[i].Session || d.Scope.Volume != t.Volume ||
			hex.EncodeToString(d.HostKey) != t.HostPublicKeys[i] || hex.EncodeToString(d.GuestKey) != t.GuestPublicKeys[i] {
			return fmt.Errorf("enrollment domain %d does not belong to this generation", i)
		}
	}
	return nil
}

// inspectProvision recomputes the runtime's provision hash (SHA256 of the
// canonical payload: generic decode with exact numbers, sorted keys, no HTML
// escaping) and decodes the single original enrollment group.
func inspectProvision(raw []byte) (string, viewcontrol.Enrollment, error) {
	var enrollment viewcontrol.Enrollment
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var artifact map[string]any
	if err := d.Decode(&artifact); err != nil {
		return "", enrollment, fmt.Errorf("provision: %w", err)
	}
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(artifact["payload"]); err != nil {
		return "", enrollment, err
	}
	var parsed struct {
		Payload struct {
			Groups []struct {
				EnrollmentBytes string `json:"enrollment_bytes"`
			} `json:"groups"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Payload.Groups) != 1 {
		return "", enrollment, fmt.Errorf("provision must contain exactly one original group")
	}
	original, err := hex.DecodeString(parsed.Payload.Groups[0].EnrollmentBytes)
	if err == nil {
		err = json.Unmarshal(original, &enrollment)
	}
	if err != nil {
		return "", enrollment, fmt.Errorf("provision enrollment bytes: %w", err)
	}
	return sha256Hex(bytes.TrimSuffix(out.Bytes(), []byte{'\n'})), enrollment, nil
}

func validUUID(text string) bool {
	_, err := fsapi.ParseUUID(text)
	return err == nil
}
