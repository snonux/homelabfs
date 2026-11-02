package sim

import (
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// ConcurrentPlan is deterministic workload INPUT, never an executor or expected
// outcome. Start every listed attempt before releasing the closure's grant gate;
// retain actual invocation/response ordinals in a ConcurrentTrace. All operations
// target the same volume/file from independently named origins. The adapter owns
// controlled barriers/fault schedules; these inputs do not implement replication.
type ConcurrentPlan struct {
	Seed  uint64
	Calls []ConcurrentCall
}

// GenerateConcurrentPlan chooses bounded overlapping conflicting operations on
// a predeclared file, using a random stream separate from service/network choices.
// Replaying a reduced counterexample uses its preserved trace, not regeneration.
func GenerateConcurrentPlan(seed uint64, count int, volume fsapi.UUID, origins []ConcurrentOrigin) (ConcurrentPlan, error) {
	plan := ConcurrentPlan{Seed: seed}
	if count < 2 || count > 12 || !volume.Valid() || len(origins) < 2 || len(origins) > 8 {
		return plan, fmt.Errorf("concurrent plan bounds")
	}
	seen := make(map[ConcurrentOrigin]bool, len(origins))
	for _, origin := range origins {
		if !validConcurrentOrigin(origin) {
			return plan, fmt.Errorf("concurrent plan origin")
		}
		if seen[origin] {
			return plan, fmt.Errorf("duplicate concurrent plan origin lifetime")
		}
		seen[origin] = true
	}
	random := NewRandom(seed, "concurrent-plan/v1")
	for index := range count {
		var value [16]byte
		if _, err := random.Read(value[:]); err != nil {
			return plan, fmt.Errorf("concurrent plan random: %w", err)
		}
		intent := ConcurrentIntent{Source: ConcurrentPath{Volume: volume, Name: "file"}, Cred: fsapi.Cred{UID: 1000, GID: 1000}}
		switch value[0] % 5 {
		case 0:
			intent.Kind = "append"
			intent.Data = []byte(fmt.Sprintf("%x/%d", value[1:5], index))
		case 1:
			intent.Kind = "write"
			intent.Offset = int64(value[1] % 8)
			intent.Data = []byte{value[2], value[3]}
		case 2:
			intent.Kind = "truncate"
			intent.Size = int64(value[1] % 12)
		case 3:
			intent.Kind = "chmod"
			intent.Mode = 0o600 | uint16(value[1]&7)
		case 4:
			intent.Kind = "rename"
			intent.Destination = ConcurrentPath{Volume: volume, Name: fmt.Sprintf("renamed-%d", index)}
		}
		call := ConcurrentCall{Attempt: fmt.Sprintf("concurrent/%d", index), Identity: ConcurrentIdentity{Principal: "workload", Operation: UUID(seed, fmt.Sprintf("concurrent/operation/%d", index))}, Origin: origins[index%len(origins)], Intent: intent}
		plan.Calls = append(plan.Calls, call)
	}
	return plan, nil
}
