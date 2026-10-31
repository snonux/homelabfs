package record

// NormalMajor reserves the all-origin normal representation independently of
// v1 and the already specified emergency-capable 2.0 namespace.
const NormalMajor uint16 = 3

// Launch names a node and one daemon lifetime, never an authentication proof.
type Launch struct{ Node, Incarnation UUID }

// OriginStream separates durable origin history from sequencer order. Journal
// incarnation survives daemon restart only after external reconciliation.
type OriginStream struct {
	Cluster, Volume, Generation, Configuration UUID
	Epoch                                      uint64 `wire:"min=1"`
	Origin, Journal                            UUID
}

// NormalRef names exact immutable creator bytes; it never names a forwarder.
// Kind is one of NormalOpen through NormalPromiseKind.
type NormalRef struct {
	Stream   OriginStream
	Sequence uint64 `wire:"min=1"`
	Creator  Launch
	Kind     uint16 `wire:"enum=1:13"`
	SHA256   Hash
}

// NormalVolume binds one closure member's immutable history and placement.
type NormalVolume struct {
	Volume, Generation, Configuration UUID
	Epoch                             uint64 `wire:"min=1"`
	Replicas                          []UUID `wire:"min=3,max=4096,sort"`
}

// NormalAuthority is installed control evidence, not a serving grant. Admitted
// is the complete origin set S; it is deliberately independent of Replicas R.
type NormalAuthority struct {
	Cluster, Registry           UUID
	RegistryRevision            uint64 `wire:"min=1"`
	RecoveryGeneration, Closure UUID
	ManifestVersion             uint64 `wire:"min=1"`
	ManifestSHA256              Hash
	Admitted                    []UUID         `wire:"min=3,max=4096,sort"`
	Volumes                     []NormalVolume `wire:"min=1,max=64,sort"`
	Sequencer, OwnerPartner     Launch
	FenceLineage                []Hash `wire:"min=1,max=4096,sort"`
}

// OrderedSlot identifies one serial reservation in a closure authority.
type OrderedSlot struct {
	Authority Hash
	Sequencer Launch
	Serial    uint64 `wire:"min=1"`
}

// SelectedRef binds an exact earlier selection certificate to its ordered slot.
type SelectedRef struct {
	Slot        OrderedSlot
	Certificate NormalRef
}

// NormalCut is a per-volume selected cut, never an origin sequence offset.
type NormalCut struct {
	Volume   UUID
	Selected SelectedRef
}

// GrantResult fixes the exact terminal syscall output before durable choice.
// Objects and Data preserve operation-specific outputs without re-execution.
type GrantResult struct {
	Code          uint16     `wire:"max=15"`
	AcceptedBytes uint64     `wire:"max=9223372036854775807"`
	Objects       []ObjectID `wire:"max=256"`
	Data          []byte     `wire:"max=4194304"`
}

// NormalGrant is one exact immutable grant. The issuer is A; Origin is O;
// Recipient is M=A for O!=A, and M=B for O=A. Baseline and predecessor are
// explicit so neither serial zero nor absent result can mean a selected slot.
type NormalGrant struct {
	Authority      NormalAuthority
	Slot           OrderedSlot
	Predecessor    *SelectedRef
	Baseline       Hash
	Origin         OriginStream
	OriginLaunch   Launch
	Recipient      Launch
	Operation      UUID
	Document       RequestDocument
	Credentials    Credentials
	Mutation       Mutation
	Result         GrantResult
	NoEffectResult GrantResult
	Dependencies   []NormalRef `wire:"max=4096,sort"`
	Cuts           []NormalCut `wire:"max=64,sort"`
}

// OriginChoice is an irrevocable durable COMMIT (1) or DECLINE (2).
// Only COMMIT has Prepared; absence of a choice is neither value.
type OriginChoice struct {
	Grant    NormalRef
	Slot     OrderedSlot
	Choice   uint8 `wire:"enum=1:2"`
	Prepared *NormalRef
	Fragment []NormalRef `wire:"min=1,max=4096,sort"`
}

// OriginFragment owns the complete original sequencer grant bytes on O. The
// enclosed frame must be exactly a NormalGrant, never another fragment.
type OriginFragment struct {
	GrantBytes []byte `wire:"min=200,max=8388408"`
}

// MemoryAcceptance is retained evidence for prepared (1), choice (2), or
// result (3), including the exact distinct recipient and durable origin prefix.
type MemoryAcceptance struct {
	Grant          NormalRef
	Slot           OrderedSlot
	Origin         OriginStream
	Recipient      Launch
	Phase          uint8 `wire:"enum=1:3"`
	Target         NormalRef
	DurableThrough uint64
	Dependencies   []NormalRef `wire:"min=1,max=4096,sort"`
}

// SlotSelection resolves COMMIT (1) or NO_EFFECT (2) from exact choice and
// retained evidence. It is representation, not a runtime selection function.
type SlotSelection struct {
	Grant              NormalRef
	Slot               OrderedSlot
	Predecessor        *SelectedRef
	Outcome            uint8 `wire:"enum=1:2"`
	Choice, Acceptance NormalRef
	Volumes            []UUID `wire:"min=1,max=64,sort"`
}

// NormalResult preserves terminal output and its original grant/selection.
type NormalResult struct {
	Grant    NormalRef
	Selected SelectedRef
	Value    GrantResult
}

// SourceCoverage records an actual durable prefix on one named storage node.
// Memory acceptance is deliberately a different type, never source coverage.
type SourceCoverage struct {
	Stream       OriginStream
	Holder       Launch
	Through      uint64 `wire:"min=1"`
	Last         NormalRef
	Dependencies []NormalRef `wire:"max=4096,sort"`
}

// NormalFrontier separates received/durable origin prefixes from selected,
// visible, backing-installed and backing-durable cuts. Empty cuts mean unknown.
type NormalFrontier struct {
	Stream                                   OriginStream
	Received, Durable                        uint64
	Selected, Visible, Installed, Checkpoint *SelectedRef
	Sources                                  []SourceCoverage `wire:"max=4096,sort"`
	Acceptances                              []NormalRef      `wire:"max=4096,sort"`
}

// RecoveryNode enumerates one complete immutable fragment's transitive inputs.
// Presence and hash verification require the corresponding actual bytes.
type RecoveryNode struct {
	Frame        NormalRef
	Dependencies []NormalRef `wire:"max=4096,sort"`
}

// NormalRecovery represents the exact selected graph, unresolved grants and
// stable results. An unknown result remains explicit and does not negate COMMIT.
type NormalRecovery struct {
	Authority     NormalAuthority
	Predecessors  []NormalAuthority `wire:"max=4096,sort"`
	IssuedThrough uint64
	Selected      *SelectedRef
	Sources       []SourceCoverage    `wire:"max=4096,sort"`
	Graph         []RecoveryNode      `wire:"max=4096,sort"`
	Unresolved    []NormalRef         `wire:"max=4096,sort"`
	Results       []ResultObservation `wire:"max=4096,sort"`
	Reports       []OriginCloseReport `wire:"max=4096,sort"`
	Equivalents   []ReportEquivalent  `wire:"max=4096,sort"`
}

// TransitionPromise is nondecreasing control evidence. A decoder cannot install
// it or prove positive fencing; consumers compare it to retained predecessor.
type TransitionPromise struct {
	Transition                    UUID
	PredecessorAuthority          Hash
	RegistryRevision, Epoch, Term uint64      `wire:"min=1"`
	Exclusions                    []NormalRef `wire:"min=1,max=4096,sort"`
}

// OriginCloseReport makes UNKNOWN (1), SEALED_EMPTY (2), and COMPLETE (3)
// distinct. It closes one admitted origin lifetime's complete issued frontier,
// rather than inferring absence from selected/durable maxima.
type OriginCloseReport struct {
	Origin              Launch
	Reporter            Launch
	Promise             TransitionPromise
	State               uint8 `wire:"enum=1:3"`
	IssuedThrough       uint64
	Unresolved          []NormalRef      `wire:"max=4096,sort"`
	Frontiers           []NormalFrontier `wire:"max=4096,sort"`
	Retained            []NormalRef      `wire:"max=4096,sort"`
	Continuity, LostRAM bool
	Obligations         []NormalRef `wire:"max=4096,sort"`
}

// ReportEquivalent names a missing origin/report and positively excluded old
// lifetime, complete issued frontier and exact replacement source obligations.
// Merely encoding it never establishes that equivalence was verified.
type ReportEquivalent struct {
	Missing       Launch
	Report        *NormalRef
	FrontierProof []NormalRef `wire:"min=1,max=4096,sort"`
	Promise       TransitionPromise
	IssuedThrough uint64
	Sources       []SourceCoverage `wire:"min=1,max=4096,sort"`
	Obligations   []NormalRef      `wire:"max=4096,sort"`
}

// ResultObservation distinguishes pending (1), indeterminate (2), terminal (3).
type ResultObservation struct {
	Grant  NormalRef
	State  uint8 `wire:"enum=1:3"`
	Result *NormalRef
}

// NormalBarrier protects a nonmutating all-origin cut without a recursive commit.
type NormalBarrier struct {
	Authority NormalAuthority
	Origin    OriginStream
	Recipient Launch
	Cuts      []NormalCut      `wire:"min=1,max=64,sort"`
	Sources   []SourceCoverage `wire:"min=1,max=4096,sort"`
	Result    GrantResult
}

// OriginOpen begins a new immutable segment in a v3 origin stream.
type OriginOpen struct {
	Segment      UUID
	Previous     *NormalRef
	MaximumBytes uint64 `wire:"min=1024,max=268435456"`
}

// OriginSeal binds every exact byte preceding it in its segment.
type OriginSeal struct {
	Segment            UUID
	First, Last, Bytes uint64 `wire:"min=1"`
	SHA256             Hash
}

// NormalRecord is an owned v3 creator record; transport identity is separate.
type NormalRecord struct {
	Stream    OriginStream
	Sequence  uint64 `wire:"min=1"`
	Operation UUID
	Creator   Launch
	Observed  Time
	Payload   any
}

// Normal record tags have meaning only in the 3.0 envelope.
const (
	NormalOpen uint16 = iota + 1
	NormalSeal
	NormalGrantKind
	NormalChoiceKind
	NormalAcceptanceKind
	NormalSelectionKind
	NormalResultKind
	NormalBarrierKind
	NormalFrontierKind
	NormalRecoveryKind
	NormalFragmentKind
	NormalCloseKind
	NormalPromiseKind
)
