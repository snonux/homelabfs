//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/ed25519"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

type kernelCredentialRoute struct {
	scope     guestbridge.Context
	admission *Admission
	mount     GuestArtifactMount
}

// kernelAttestor trusts assertions only from the already adopted C6 kernel/root
// through a real authenticated bridge request. It does not independently measure
// a guest process's IDs or namespace mapping from the host.
type kernelAttestor struct {
	routes map[string]kernelCredentialRoute
}

func newKernelAttestor(admission *Admission, authorization viewcontrol.Authorization, provision []byte, operator ed25519.PublicKey, budget GuestArtifactBudget) (*kernelAttestor, error) {
	if admission == nil || len(provision) == 0 || len(provision) > guestArtifactLimit {
		return nil, fsapi.Permission
	}
	var artifact guestProvisionArtifact
	if err := strictJSON(provision, &artifact); err != nil {
		return nil, err
	}
	canonical, err := canonicalJSON(artifact)
	if err != nil || string(canonical) != string(provision) {
		return nil, fsapi.Permission
	}
	payload, err := canonicalJSON(artifact.Payload)
	if err != nil || artifact.Payload.Version != "homelabfs-guest-provision/1" || !guestVerify(operator, payload, artifact.OperatorSignature) || len(artifact.Payload.Groups) != 1 || len(artifact.GroupSignatures) != 1 || artifact.Payload.Budget != budget {
		return nil, fsapi.Permission
	}
	admission.mu.Lock()
	boot, nonce := admission.installed.BootID, admission.installed.Admission.ProviderNonce
	admission.mu.Unlock()
	if artifact.Payload.BootID != boot || artifact.Payload.ProviderNonce != nonce {
		return nil, fsapi.Permission
	}
	group := artifact.Payload.Groups[0]
	actual, err := guestDecodeAuthorization(group)
	if err != nil || !sameJSON(actual, authorization) {
		return nil, fsapi.Permission
	}
	if _, err = guestAuthorization(actual, operator); err != nil {
		return nil, err
	}
	e := actual.Enrollment
	if len(artifact.GroupSignatures[0]) != len(e.Domains) {
		return nil, fsapi.Permission
	}
	inventory := guestArtifactInventory{ids: map[string]bool{}, sessions: map[string]bool{}, tags: map[string]bool{}, names: map[string]bool{}}
	if err = inventory.check(e, group.LocalMounts, boot); err != nil {
		return nil, err
	}
	a := &kernelAttestor{routes: map[string]kernelCredentialRoute{}}
	for i, domain := range e.Domains {
		signature := artifact.GroupSignatures[0][i]
		if !guestVerify(domain.HostKey, payload, signature.Host) || !guestVerify(domain.GuestKey, payload, signature.Guest) {
			return nil, fsapi.Permission
		}
		if err := admission.check(domain.Scope); err != nil {
			return nil, err
		}
	}
	for _, mount := range group.LocalMounts {
		scope := e.Domains[mount.DomainIndex].Scope
		a.routes[scope.Session] = kernelCredentialRoute{scope, admission, mount}
	}
	if len(a.routes) != len(e.Domains) {
		return nil, fsapi.Unsupported
	}
	return a, nil
}

func (a *kernelAttestor) Verify(ctx context.Context, scope guestbridge.Context, cred guestbridge.Credentials) error {
	observed, ok := guestbridge.VerifiedRequest(ctx)
	route, exists := a.routes[scope.Session]
	if !ok || !exists || route.scope != scope || observed.Scope != scope || route.admission.check(scope) != nil || route.admission.supervisor.Verify(ctx, observed.Native, observed.Controller) != nil || cred.IDMapHash != route.mount.IDMapHash || cred.UserNamespace != route.mount.UserNamespace || cred.Revision == 0 || !validUUIDString(cred.Process) || cred.FSUID == ^uint32(0) || cred.FSGID == ^uint32(0) || len(cred.Groups) > 1024 {
		return fsapi.Permission
	}
	for i, id := range cred.Groups {
		if id == ^uint32(0) || i > 0 && cred.Groups[i-1] >= id {
			return fsapi.Permission
		}
	}
	// Ensure the complete immutable credentials are part of this authenticated
	// command's payload, rather than a separate caller-created credential object.
	// The bridge closes the operation union and checks the exact request hash.
	if observed.PayloadHash == "" || observed.CommandHash == "" {
		return fsapi.Permission
	}
	return ctx.Err()
}
