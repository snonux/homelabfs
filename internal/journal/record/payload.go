package record

// VolumeInit describes explicit initial root state; it never authorizes format-on-open.
type VolumeInit struct {
	Root          ObjectID
	Initial       Metadata
	RootParent    ParentState
	RootLedger    LedgerEdit
	Authority     Authority
	Authorization Artifact
}

// Commit selects an ordinary mutation candidate.
type Commit struct {
	Mutation     FrameRef
	Authority    Authority
	Dependencies Vector
}

// Barrier captures prior admitted operations in a scope.
type Barrier struct {
	Scope      uint8 `wire:"enum=1:4"`
	Object     *ObjectID
	DataOnly   bool
	Covered    Vector
	Decisions  []DecisionRef `wire:"sort"`
	Visibility Visibility
}

// HandlePin conservatively protects an exposed object.
type HandlePin struct {
	Pin                            UUID
	Object                         ObjectID
	ServingGeneration, Incarnation UUID
	Reasons                        uint8 `wire:"min=1,max=7"`
}

// HandleRelease releases observed drained or fenced obligations.
type HandleRelease struct {
	Pin                            UUID
	Object                         ObjectID
	ServingGeneration, Incarnation UUID
	Reason                         uint8 `wire:"enum=1:2"`
	Fence                          *FrameRef
}

// Reclaim permanently tombstones an allocation after protected retention.
type Reclaim struct {
	Object                                         ObjectID
	ExpectedObjectRevision, ExpectedLedgerRevision uint64 `wire:"min=1"`
	Retention                                      FrameRef
	PinsReleased                                   []FrameRef `wire:"sort"`
}

// Pin records a nonexpiring lifetime obligation.
type Pin struct {
	Pin         UUID
	Action      uint8      `wire:"enum=1:2"`
	Purpose     uint8      `wire:"enum=1:6"`
	Objects     []ObjectID `wire:"sort"`
	History     Vector
	Artifacts   []Artifact `wire:"sort"`
	Predecessor *FrameRef
}

// OrdinaryResult preserves the stable result of an ordinary operation.
type OrdinaryResult struct {
	Mutation      FrameRef
	Commit        *FrameRef
	Outcome       uint8  `wire:"enum=1:2"`
	Code          uint16 `wire:"max=15"`
	Object        *ObjectID
	AcceptedBytes uint32 `wire:"max=1048576"`
	Resolution    *FrameRef
	Dependencies  []FrameRef `wire:"sort"`
	Evidence      Evidence
}

// Session registers an anchored identity.
type Session struct {
	Principal     Principal
	Anchor, Nonce UUID
	Floor         uint64 `wire:"max=0"`
	State         uint8  `wire:"enum=1:1"`
}

// RequestClaim binds a canonical document and its domain-correct digest.
type RequestClaim struct {
	Document RequestDocument
	Digest   Hash
}

// RequestBind protects one immutable resolved binding.
type RequestBind struct {
	Claim          FrameRef
	Binding        Binding
	Authorizations []Authorization `wire:"sort"`
}

// RequestResult retains a terminal anchored receipt.
type RequestResult struct {
	Request      RequestID
	Digest       Hash
	Claim        FrameRef
	Binding      *FrameRef
	Result       TerminalResult
	Dependencies []FrameRef `wire:"sort"`
	Evidence     Evidence
}

// SessionRetire advances a protected request floor.
type SessionRetire struct {
	Principal               Principal
	Anchor, Nonce           UUID
	PreviousFloor, NewFloor uint64
	TerminalResults         []FrameRef `wire:"sort"`
	Replacement             *Artifact
}

// SessionClose retains a permanent closed-session tombstone.
type SessionClose struct {
	Principal     Principal
	Anchor, Nonce UUID
	Floor         uint64
	Retirement    *FrameRef
}

// AnchorRetire retains permanent anchor rejection identity.
type AnchorRetire struct {
	Anchor             UUID
	ClosedSessions     []FrameRef `wire:"sort"`
	PermanentRejection Artifact
	Authority          Authority
}

// Affinity records an identical growing closure manifest on each member.
type Affinity struct {
	Closure         UUID
	ManifestVersion uint64        `wire:"min=1"`
	Members         []UUID        `wire:"min=1,max=64,sort"`
	Authorities     []Authority   `wire:"max=64,sort"`
	Predecessors    []AffinityRef `wire:"max=64,sort"`
}

// Begin starts the immutable coordinator operation.
type Begin struct {
	Binding        Binding
	Bind           FrameRef
	Document       RequestDocument
	Authorizations []Authorization `wire:"sort"`
	Term           uint64          `wire:"min=1"`
}

// TxAnchor makes a partial begin fan-out discoverable.
type TxAnchor struct {
	Request         RequestID
	Digest          Hash
	Binding         Binding
	Begin           FrameRef
	CoordinatorTerm uint64 `wire:"min=1"`
}

// Prepared retains participant-local effects and their complete dependencies.
type Prepared struct {
	Request    RequestID
	Digest     Hash
	Begin      FrameRef
	Binding    Binding
	Term       uint64 `wire:"min=1"`
	Operation  Mutation
	RedoSHA256 Hash
	Retained   []FrameRef `wire:"sort"`
	Artifacts  []Artifact `wire:"sort"`
}

// Decision records one provisional commit or abort outcome.
type Decision struct {
	ID        DecisionID
	Digest    Hash
	Begin     FrameRef
	Outcome   uint8        `wire:"enum=1:2"`
	Prepares  []PrepareRef `wire:"max=64,sort"`
	Result    TerminalResult
	Authority Authority
	Recovery  *FrameRef
}

// Install retains a recoverable decision and optional exact local redo.
type Install struct {
	Request                  RequestID
	Digest                   Hash
	Binding                  Binding
	Decision                 DecisionRef
	Outcome                  uint8 `wire:"enum=1:2"`
	Prepare                  *PrepareRef
	Redo                     *Redo
	Applied                  Position
	Acceptance, Dependencies []FrameRef `wire:"sort"`
	Evidence                 Evidence
}

// Complete records closure-wide application and visibility.
type Complete struct {
	Request       RequestID
	Digest        Hash
	Decision      DecisionRef
	Installs      []FrameRef `wire:"sort"`
	Installed     Vector
	Result        TerminalResult
	Visibility    Visibility
	CacheBarriers []FrameRef `wire:"sort"`
}

// Promise retains recovery promise and accepted tails.
type Promise struct {
	RecoveryID                    UUID
	Authority                     Authority
	PromisedEpoch                 uint64
	PromisedTerm                  uint64 `wire:"min=1"`
	Reporter, ReporterIncarnation UUID
	Tails                         []Tail     `wire:"max=64,sort"`
	PriorAcceptances              []FrameRef `wire:"sort"`
	Authorization                 Artifact
}

// Accept asserts exact candidate and prefix retention by one reporter.
type Accept struct {
	SlotKind                      uint8 `wire:"enum=1:2"`
	Candidate                     FrameRef
	Request                       *RequestID
	Term                          uint64 `wire:"min=1"`
	Authority                     Authority
	Reporter, ReporterIncarnation UUID
	Retained                      Vector
	Dependencies                  []FrameRef `wire:"sort"`
	Durable                       Vector
}

// TailReport preserves authenticated recovery tails and candidates.
type TailReport struct {
	RecoveryID, Reporter, ReporterIncarnation UUID
	Promises                                  []FrameRef `wire:"sort"`
	Tails                                     []Tail     `wire:"max=64,sort"`
	Candidates, Dependencies                  []FrameRef `wire:"sort"`
}

// RecoverySelect retains bounded evidence of a selected recovery outcome.
type RecoverySelect struct {
	RecoveryID          UUID
	SlotKind            uint8 `wire:"enum=1:2"`
	Operation           UUID
	Binding             *Binding
	NewAuthorities      []Authority `wire:"max=64,sort"`
	Promises, Reports   []FrameRef  `wire:"sort"`
	Selected            *FrameRef
	AbsenceAbort        bool
	Dependencies, Fence []FrameRef `wire:"sort"`
	Evidence            Evidence
}

// Fence records positive external fencing evidence.
type Fence struct {
	RecoveryID                UUID
	Authorities               []Authority `wire:"max=64,sort"`
	OldOwner, OldIncarnation  UUID
	ServingGenerations, Views []UUID `wire:"sort"`
	Mechanism                 UUID
	Attestation               Artifact
	CompletedAt               Time
	Dependencies              []FrameRef `wire:"sort"`
}

// Configuration records an authorized closure transition.
type Configuration struct {
	RecoveryID                     UUID
	Predecessors, Successors       []Authority `wire:"max=64,sort"`
	Closure                        AffinityRef
	Recovered                      Vector
	Promises, Fences, PartnerReady []FrameRef `wire:"sort"`
	Authorization                  Artifact
}

// LossRecovery records an explicitly selected new history with uncertain suffixes.
type LossRecovery struct {
	RecoveryID          UUID
	Previous            []Authority `wire:"max=64,sort"`
	Selected            Vector
	NewGeneration       UUID
	UnavailableRequests []RequestID `wire:"sort"`
	UnavailableRanges   []Position  `wire:"sort"`
	Baselines           []Artifact  `wire:"sort"`
	Fences              []FrameRef  `wire:"sort"`
	Authorization       Artifact
}

// ViewBarrier records positive invalidation and observation drain evidence.
type ViewBarrier struct {
	Affinity                       AffinityRef
	OldVisibility, NewVisibility   Visibility
	ServingGeneration, Incarnation UUID
	Aliases                        []Alias `wire:"sort"`
	Drained                        bool
	Mechanism                      UUID
	Attestation                    Artifact
	Dependencies                   []FrameRef `wire:"sort"`
}

// CheckpointAccept pairs complete data/catalog state with retained dependencies.
type CheckpointAccept struct {
	Checkpoint                       UUID
	Root                             ObjectID
	DataManifest, Catalog            Artifact
	Predecessor                      *UUID
	Committed, Applied               Vector
	Visibility                       Visibility
	ClosureCheckpoint                UUID
	ClosureMembers                   []UUID     `wire:"min=1,max=64,sort"`
	Dependencies, Replacements, Pins []FrameRef `wire:"sort"`
	RequiredSuffix                   Vector
}

// Retention advances only a fully verified protected cleanup boundary.
type Retention struct {
	RetentionID        UUID
	Predecessor        *FrameRef
	CheckpointAccepts  []FrameRef `wire:"sort"`
	PublishedManifests []Artifact `wire:"sort"`
	ClosureCheckpoint  UUID
	ReclaimThrough     Vector
	Coverage           []Coverage  `wire:"sort"`
	Pins, Replacements []FrameRef  `wire:"sort"`
	Authorities        []Authority `wire:"max=64,sort"`
}

// ReplicaProgress preserves offline replica and catch-up obligations.
type ReplicaProgress struct {
	Replica, Incarnation                UUID
	Received, Durable, Decided, Applied Vector
	Baseline                            *Artifact
	RequiredSuffix                      Vector
	TransferPins                        []FrameRef `wire:"sort"`
}

// SegmentOpen describes stream ownership and bounded capacity.
type SegmentOpen struct {
	Segment                       UUID
	StreamKind                    uint8 `wire:"enum=1:2"`
	Reporter, ReporterIncarnation *UUID
	First                         uint64 `wire:"min=1"`
	PreviousSeal                  *FrameRef
	MaximumBytes                  uint64 `wire:"max=268435456"`
}

// SegmentSeal hashes every complete predecessor frame in this segment.
type SegmentSeal struct {
	Segment                                      UUID
	First, LastBeforeSeal, FrameCount, ByteCount uint64
	ContentSHA256                                Hash
}
