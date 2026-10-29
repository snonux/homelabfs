package transactions

import (
	"sort"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// SortReferences orders exact references using the journal's full stream key.
// This is useful when constructing protocol dependency sets for Propose.
func SortReferences(refs []record.FrameRef) {
	sort.Slice(refs, func(i, j int) bool { return referenceLess(refs[i], refs[j]) })
}

func referenceLess(left, right record.FrameRef) bool {
	a, b := frameKey(left), frameKey(right)
	if a.kind != b.kind {
		return a.kind < b.kind
	}
	if a.reporter != b.reporter {
		return uuidLess(a.reporter, b.reporter)
	}
	if a.incarnation != b.incarnation {
		return uuidLess(a.incarnation, b.incarnation)
	}
	for _, pair := range [][2]record.UUID{{a.volume, b.volume}, {a.generation, b.generation}, {a.configuration, b.configuration}} {
		if pair[0] != pair[1] {
			return uuidLess(pair[0], pair[1])
		}
	}
	if a.epoch != b.epoch {
		return a.epoch < b.epoch
	}
	return a.sequence < b.sequence
}
