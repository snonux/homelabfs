package viewcontrol

import (
	"encoding/json"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Read-only OPEN conflicts like a read (task z94, zq3-r11 R11-1, zq3-r14
// R14-1).
//
// OPEN used to be a non-read for conflicts, so it conflicted with every
// active lease, the caller's own DOMAIN READ observation lease included.
// guest.py opens each mount root with open(O_RDONLY|O_DIRECTORY) under that
// very lease, so every root OPEN started an all-domain recall against
// itself; in zq3-r14 T1's m0 root OPEN waited ~14 s behind three recalls and
// tripped guest.py's 10 s alarm.
//
// A read-only OPEN (access READ, no create, exclusive, truncate or append;
// O_DIRECTORY is guest-local and never reaches the host) changes nothing a
// lease holder of any domain can have cached:
//   - fsservice plans no Mutation for it: only Truncate (refused with READ
//     access by OpenFlags.Validate) would add one. The service has no
//     access-time update on open or read; Atime moves only through an
//     explicit SETATTR/TRUNCATE or at creation (mutation.go), so no
//     attribute, directory entry or data byte changes.
//   - Its only effects are lifetime state: a HandlePin when the object has
//     no lifetime yet and one open handle (fsservice acquire). That is the
//     same class of effect a LOOKUP has, and LOOKUP has always conflicted as
//     a read. A pin only keeps an object alive longer; no lease observes it.
//     The volume root journals no pin at all (fsservice pinlessLifetime), so
//     a read-only root OPEN and its final DESCRIPTION_RELEASE leave the
//     source cut unchanged.
//   - The description capability the bridge issues is the calling domain's
//     own grant state; no peer can see it.
//
// So it conflicts exactly like a read (conflicts(true)): with any active
// EXCLUSIVE_DIRTY lease, whose dirty overlay may make the returned
// attributes (size) stale, and with any open dirty reservation, but never
// with a READ lease of any domain, the caller's own included. The caller's
// own EXCLUSIVE_DIRTY lease still conflicts, as it does for every read:
// same-domain dirty overlays participate exactly like other domains' (Admit).
//
// What stays non-read: the pin may move the source cut, so a read-only OPEN
// is not cutPreservingRead and every recall still holds it (recallHoldsCall):
// it waits for an in-progress recall's REOPEN like LOOKUP. admitCut keeps
// charging it source capacity as a non-read, because its cut may be new.
// Native (FUSE adapter) OPENs are unchanged: nativeRead still classifies
// fsapi.Open as a non-read.
//
// The conflict rule alone did not close R14-1 (review round 1 of z94): the
// guest acquires each observation with expected_target = its installed view,
// and the root's HandlePin (first OPEN) and HandleRelease (last RELEASE)
// moved the cut, so the next observation of every domain, the caller's
// included, hit expectedMismatch and recalled all domains. The root is never
// reaped and cannot be unlinked, so its pins had no consumer; fsservice now
// keeps the root's lifetime pinless and a root OPEN/close cycle keeps the
// cut (TestZQ3CredentialsAdmissionKeepsLeases, which observes with the real
// installed targets). The general case stays as it was (e84): a read-only
// OPEN of an unpinned non-root object still journals its pin, so a peer's
// next observation hits the mismatch, exactly as after a pinning LOOKUP.
// The fix therefore relies on each guest mount root being the volume root;
// a mount of a subtree directory would still pin on its root OPEN and move
// the cut (the e84 behaviour).

// readConflict reports whether intent conflicts like a read: a read opcode
// or a read-only OPEN.
func readConflict(intent guestbridge.Intent) bool {
	return readOperation(intent.Opcode) || readOnlyOpen(intent)
}

// readOnlyOpen reports whether intent is an OPEN that only reads: access
// READ and no create, exclusive, truncate or append flag. Arguments that do
// not decode count as a non-read, the conservative answer: they conflict
// with every lease, as every OPEN did before.
func readOnlyOpen(intent guestbridge.Intent) bool {
	if intent.Opcode != "OPEN" {
		return false
	}
	var args struct {
		Flags *struct {
			Access                              string
			Append, Create, Exclusive, Truncate bool
		} `json:"flags"`
	}
	if json.Unmarshal(intent.Args, &args) != nil || args.Flags == nil {
		return false
	}
	f := args.Flags
	return f.Access == "READ" && !f.Create && !f.Exclusive && !f.Truncate && !f.Append
}
