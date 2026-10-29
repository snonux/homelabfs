package labrunner

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// NativeScenario defines an acceptance family, not an executed test or oracle.
// Every variant rotates owner/sync/async roles over three native guests and
// records acknowledged/uncertain client history outside the guests.
type NativeScenario struct {
	ID         string          `json:"id"`
	Faults     []ScenarioFault `json:"faults"`
	Requires   []string        `json:"requires"`
	Assertions []string        `json:"assertions"`
	BlockedBy  []string        `json:"blocked_by"`
}

// ScenarioFault distinguishes etcd traffic from HFS replication traffic. Ports
// are dedicated private-test ports; zero means no network mutation is implied.
type ScenarioFault struct {
	Scope    string   `json:"scope"`
	Action   string   `json:"action"`
	Role     string   `json:"role"`
	Boundary string   `json:"boundary,omitempty"`
	Ports    []uint16 `json:"ports,omitempty"`
}

// ScenarioCapabilities must be observed from the exact reviewed binary inside
// each native guest by a pinned probe; it is not a plan-time user assertion.
type ScenarioCapabilities struct {
	ArtifactSHA256 string   `json:"artifact_sha256"`
	Guest          string   `json:"guest"`
	Features       []string `json:"features"`
}

// ScenarioProbe supplies actual binary capability inspection only. A shell
// exit status, fault receipt or process readiness is never protocol evidence.
type ScenarioProbe interface {
	ProbeScenario(context.Context, Artifact, string) (ScenarioCapabilities, error)
}

// ScenarioPreparation contains a schedule or the explicit blockers. There is
// intentionally no scenario pass result: histories and native assertions must
// be checked by the future protocol/recovery driver and independent oracle.
type ScenarioPreparation struct {
	Definition NativeScenario `json:"definition"`
	Outcome    string         `json:"outcome"`
	Reasons    []string       `json:"reasons"`
	Rotations  [][3]string    `json:"owner_sync_async_rotations"`
}

// NativeScenarios returns R01-R10/F01-F05 definitions. fq3/zq3 are development
// prerequisites; capability inspection can never waive these known open gates.
func NativeScenarios() []NativeScenario {
	common := []string{"normal-replication-v1", "normal-fuse-publication-v1", "external-client-history-v1", "independent-history-oracle-v1", "exact-workspace-failpoints-v1"}
	makeCase := func(id, scope, action, role, boundary string, ports []uint16, requires, assertions []string) NativeScenario {
		return NativeScenario{ID: id, Faults: []ScenarioFault{{Scope: scope, Action: action, Role: role, Boundary: boundary, Ports: ports}}, Requires: append(slices.Clone(common), requires...), Assertions: assertions, BlockedBy: []string{"fq3 checked commit model", "zq3 real-daemon native compatibility evidence", "normal replication and takeover implementation"}}
	}
	r := []NativeScenario{
		makeCase("R01", "daemon", "kill", "owner", "local-sync-and-partner-memory-receipt-before-partner-durability", nil, []string{"failpoint:partner-before-durable", "volatile-tail-takeover-v1"}, []string{"acknowledged history survives from partner RAM", "takeover persists recovered tail before activation"}),
		makeCase("R02", "daemon", "kill-and-lost-reply", "owner", "before-local-sync;each-one-branch-complete;before-client-reply", nil, []string{"commit-boundary-failpoints-v1", "lost-reply-injection-v1"}, []string{"no premature acknowledgement", "uncertain operations allow exactly the admitted outcomes", "no duplicate append or rename"}),
		makeCase("R03", "daemon", "restart-clearing-ram", "sync", "partner-receipt-before-durable", nil, []string{"enrolled-daemon-restart-v1", "incarnation-backfill-v1"}, []string{"returning partner remains recovering until backfilled", "old incarnation never reports readiness"}),
		makeCase("R04", "daemon", "promote-lagging", "async", "before-catch-up", nil, []string{"takeover-v1", "positive-native-vm-fence-v1"}, []string{"all destination I/O blocks until authoritative history applied", "ready partner required before acknowledgement"}),
		makeCase("R05", "replication", "partition-and-reconnect", "async", "sustained-writes-and-owner-change", []uint16{PrivateReplicationPort}, []string{"mandatory-async-retry-v1", "takeover-v1"}, []string{"new owner retains every async delivery obligation", "reconnect recovers complete prefix or verified baseline"}),
		makeCase("R06", "guest", "abrupt-stop", "each", "three-volumes-independent-owners", nil, []string{"three-independent-volumes-v1", "takeover-v1"}, []string{"lost owner/sync/async roles handled independently", "unrelated volumes continue without global barrier"}),
		makeCase("R07", "daemon", "restart-divergent-tail", "former-owner", "after-replacement-activation", nil, []string{"divergent-tail-recovery-v1", "positive-native-vm-fence-v1"}, []string{"no stale serving or automatic failback", "preserve/reconcile suffix before follower catch-up"}),
		makeCase("R08", "guest", "owner-disk-loss-and-sync-ram-loss", "owner-and-sync", "before-partner-durable", nil, []string{"disposable-disk-loss-v1", "uncertain-history-blocking-v1"}, []string{"missing acknowledged history blocks promotion", "no silent stale-async or local-only downgrade"}),
		makeCase("R09", "guest", "all-abrupt-stop-and-ordered-restart", "all", "all-restart-orders-and-delayed-original-owner", nil, []string{"enrolled-guest-restart-v1", "cold-start-history-recovery-v1"}, []string{"reconstruct actual durable history", "maximum sequence or first pair never proves readiness"}),
		makeCase("R10", "daemon", "kill-at-every-takeover-step", "each", "takeover-boundaries", nil, []string{"takeover-v1", "takeover-boundary-failpoints-v1", "positive-native-vm-fence-v1"}, []string{"idempotent resume or explicit reconciliation", "no double activation, history loss or restart loop"}),
		makeCase("F01", "replication", "partition", "owner", "old-owner-process-still-alive", []uint16{PrivateReplicationPort}, []string{"positive-native-vm-fence-v1", "takeover-v1", "stale-read-cache-probe-v1"}, []string{"positive fencing before replacement activation", "old mounts cannot return stale reads or acknowledge writes"}),
		makeCase("F02", "daemon-and-guest", "pause-and-resume", "owner", "during-revocation-with-old-messages", nil, []string{"enrolled-guest-pause-v1", "positive-native-vm-fence-v1", "stale-read-cache-probe-v1"}, []string{"unconfirmed stop keeps promotion blocked", "no TTL/time-based fence proof", "old descriptors and mmap cannot revive access"}),
		makeCase("F03", "etcd", "partition", "each", "authority-only-then-data-only", []uint16{PrivateEtcdClientPort, PrivateEtcdPeerPort}, []string{"authority-uncertainty-gating-v1", "positive-native-vm-fence-v1"}, []string{"etcd partition leaves replication links intact", "replication partition leaves etcd links intact", "authority never substitutes for data readiness"}),
		makeCase("F04", "etcd", "leader-then-quorum-loss-and-recovery", "each", "authority-detection-and-positive-fence-completion", []uint16{PrivateEtcdClientPort, PrivateEtcdPeerPort}, []string{"authority-uncertainty-gating-v1", "positive-native-vm-fence-v1"}, []string{"leader loss recovers; quorum loss gates affected I/O", "record detection and fence completion separately", "no promotion before authority/history/fence revalidation"}),
		makeCase("F05", "configuration", "race-promotion-replacement-relocation-admission", "all", "fourth-node-joint-membership", nil, []string{"fourth-disposable-node-v1", "joint-reconfiguration-v1", "takeover-v1"}, []string{"one valid configuration and history", "arbitrary pair never grants serving authority"}),
	}
	for i := range r {
		if r[i].ID == "F03" {
			r[i].Faults = append(r[i].Faults, ScenarioFault{Scope: "replication", Action: "partition", Role: "each", Boundary: "separate-data-only-variant", Ports: []uint16{PrivateReplicationPort}})
		}
	}
	return r
}

// PrepareNativeScenario inspects all three actual binaries before preparing a
// schedule. Missing capabilities return unsupported/blocked without side effects.
func PrepareNativeScenario(ctx context.Context, p Plan, id, candidate string, probe ScenarioProbe) (ScenarioPreparation, error) {
	if err := p.Validate(); err != nil {
		return ScenarioPreparation{}, err
	}
	if p.PrivateEtcd == nil {
		return ScenarioPreparation{}, errors.New("native R/F scenarios require the private test cluster")
	}
	var definition NativeScenario
	for _, s := range NativeScenarios() {
		if s.ID == id {
			definition = s
		}
	}
	if definition.ID == "" {
		return ScenarioPreparation{}, errors.New("unknown native R/F scenario")
	}
	out := ScenarioPreparation{Definition: definition, Outcome: "blocked", Reasons: slices.Clone(definition.BlockedBy), Rotations: [][3]string{{"hfs-test0", "hfs-test1", "hfs-test2"}, {"hfs-test1", "hfs-test2", "hfs-test0"}, {"hfs-test2", "hfs-test0", "hfs-test1"}}}
	if id == "F05" {
		out.Outcome = "unsupported"
		out.Reasons = append(out.Reasons, "prepared lab has exactly three guests; fourth-node placement requires a separately reviewed profile")
	}
	a := etcdArtifact(p, candidate)
	if probe == nil || a.ID == "" {
		out.Outcome = "unsupported"
		out.Reasons = append(out.Reasons, "actual pinned binary capability probe unavailable")
		return out, nil
	}
	if err := p.VerifyArtifacts(); err != nil {
		return out, err
	}
	for i := range 3 {
		guest := fmt.Sprintf("hfs-test%d", i)
		caps, err := probe.ProbeScenario(ctx, a, guest)
		if err != nil || caps.Guest != guest || caps.ArtifactSHA256 != a.SHA256 {
			out.Outcome = "unsupported"
			out.Reasons = append(out.Reasons, "missing or foreign capability evidence for "+guest)
			continue
		}
		for _, required := range definition.Requires {
			if !slices.Contains(caps.Features, required) {
				out.Outcome = "unsupported"
				out.Reasons = append(out.Reasons, guest+" lacks "+required)
			}
		}
	}
	return out, nil
}
