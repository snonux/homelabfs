package viewcontrol

import (
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// admissionReplay replays guest.py's credentials admission on both mounts
// (zq3-r14..r17 T1): realpath, lstat, open and fstat of each mount root,
// each syscall one guest observation (hfs_mmap_begin's DOMAIN READ VIEW_ACQUIRE,
// the CALL, then hfs_mmap_end's VIEW_RELEASE), and finally the roots' close.
// targeted sends the guest's real expected_target (the installed view);
// without it every acquisition is target:null, the shape whose release keeps
// the recall, i.e. what every observation release cost before task je4.
// installed tracks each mount's view: a recall reinstalls the current cut.
type admissionReplay struct {
	r         *rig
	targeted  bool
	installed [2]guestbridge.ViewTarget
}

// observation runs one syscall under its own observation lease on mount i
// and requires the acquisition GRANTED and the release RELEASED (r.release
// completes the release's recall where one starts).
func (a *admissionReplay) observation(i int, op string, args map[string]any) result {
	a.r.t.Helper()
	var target any
	if a.targeted {
		target = a.installed[i]
	}
	reply := a.r.view(i, "VIEW_ACQUIRE", map[string]any{"request": a.r.ticket(i), "domain": a.r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": target})
	if reply.Status != "GRANTED" {
		a.r.t.Fatalf("m%d %s observation: %+v", i, op, reply)
	}
	res := a.r.call(i, op, args)
	if released := a.r.release(i, reply.Lease); released.Status != "RELEASED" {
		a.r.t.Fatalf("m%d %s release: %+v", i, op, released)
	}
	a.installed[i] = a.r.currentTarget(i)
	return res
}

// run replays the admission and returns how many recalls it started.
func (a *admissionReplay) run() uint64 {
	before := a.r.recallTotal()
	descriptions := [2]guestbridge.Capability{}
	for i, root := range a.r.roots {
		getattr := map[string]any{"object": root}
		a.observation(i, "GETATTR", getattr) // realpath
		a.observation(i, "GETATTR", getattr) // lstat
		opened := a.observation(i, "OPEN", map[string]any{"object": root, "flags": flags("READ", false)})
		descriptions[i] = opened.Capabilities[0]
		a.observation(i, "GETATTR", getattr) // fstat
	}
	for i := range a.r.roots {
		a.r.call(i, "DESCRIPTION_RELEASE", map[string]any{"description": descriptions[i], "release_sequence": 1})
	}
	return a.r.recallTotal() - before
}

// TestZQ3CredentialsAdmissionReleasesWithoutRecall replays T1's credentials
// admission at the zq3 spec bounds twice: with target:null acquisitions
// every one of its eight observation releases starts a scoped recall (what
// each release cost before je4, 4-7 s in the lab); with the guest's real
// expected targets none does, and every observation is still granted on the
// view installed before the admission (the root OPEN/close keeps the cut,
// z94), so no expected-target recall replaces them.
func TestZQ3CredentialsAdmissionReleasesWithoutRecall(t *testing.T) {
	baseline := (&admissionReplay{r: newZQ3Rig(t, 0)}).run()
	r := newZQ3Rig(t, 0)
	targeted := &admissionReplay{r: r, targeted: true, installed: [2]guestbridge.ViewTarget{r.currentTarget(0), r.currentTarget(1)}}
	start := targeted.installed
	je4 := targeted.run()
	t.Logf("credentials admission recalls: %d with every release recalling (before je4), %d with observation releases (je4)", baseline, je4)
	if baseline != 8 || je4 != 0 {
		t.Fatalf("recalls: baseline %d (want 8), je4 %d (want 0)", baseline, je4)
	}
	for i := range start {
		if !equal(targeted.installed[i], start[i]) || r.controller.fence(r.scopes[i].Session) != 1 {
			t.Fatalf("m%d view moved during the admission", i)
		}
	}
}
