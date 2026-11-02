package fsservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

// LogicalConfig supplies owner RAM planning and observation, independently of
// the legacy local owner-stream Service. It never configures a writer, disk
// reservation, native mount or normal serving grant. View must have passed its
// explicit external baseline/source authority verifier.
type LogicalConfig struct {
	View          *logicalview.Store
	Permissions   PermissionPolicy
	Clock         fsapi.Clock
	Limits        Limits
	StorageLimits storage.Limits
}

// LogicalPlanner uses the same redo planner, Unix policy and read paths as the
// local service, against one immutable committed RAM cut. The replication
// sequencer must serialize Plan/Stage/Select/Publish and route admitted origins
// here; no protocol or platform implementation is supplied by this seam.
type LogicalPlanner struct{ config LogicalConfig }

// Planned owns the exact input binding, redo, afterimages and stable result.
// Cut identifies the predecessor used for planning and permissions. Claim and
// allocation identities are reserved in RAM, separately from backing creation.
type Planned struct {
	Claim     fsapi.UUID
	Cut       *record.SelectedRef
	Intent    []byte
	Mutation  record.Mutation
	Operation storage.Operation
	Result    fsapi.Result
}

// ErrDelegatedFileTooLarge is a pre-effect delegated write rejection. It is
// deliberately outside the closed fsapi/v1 errno and journal schemas.
var ErrDelegatedFileTooLarge = errors.New("delegated write exceeds captured file end")

// NewLogicalPlanner requires an explicit policy; privileged daemon credentials
// never replace workload credentials. Its bounded limits mirror Service.
func NewLogicalPlanner(config LogicalConfig) (*LogicalPlanner, error) {
	if config.View == nil || config.Permissions == nil {
		return nil, fsapi.Invalid
	}
	if config.Clock == nil {
		config.Clock = fsapi.WallClock{}
	}
	if config.Limits.IOBytes == 0 {
		config.Limits.IOBytes = 1 << 20
	}
	// The shared record.Write representation and legacy Service admit at
	// most 1 MiB per operation. Reject an unencodable profile before clamping
	// to the view's smaller transient budget or reserving any request identity.
	if config.Limits.IOBytes < 1 || config.Limits.IOBytes > 1<<20 {
		return nil, fsapi.Invalid
	}
	config.Limits.IOBytes = min(config.Limits.IOBytes, int(config.View.PlanningLimit()))
	if config.Limits.LogicalBytes == 0 {
		config.Limits.LogicalBytes = 64 << 20
	}
	if config.Limits.IOBytes < 1 || config.Limits.IOBytes > 1<<20 || config.Limits.LogicalBytes < 1 || config.Limits.LogicalBytes > 1<<30 {
		return nil, fsapi.Invalid
	}
	return &LogicalPlanner{config: config}, nil
}

// Plan resolves a mutation and permissions at one captured visible cut. Claim
// and allocation are chosen once by the caller's immutable origin request.
// A creation requires a never-used home allocation; other operations require a
// zero allocation. It performs no source append, sync or backing installation.
// Ordinary cross-mount/foreign-home mutations remain closed until typed 8w3.
func (p *LogicalPlanner) Plan(ctx context.Context, observation *logicalview.Observation, cred fsapi.Cred, c fsapi.Command, claim fsapi.UUID, allocation fsapi.ObjectID) (Planned, error) {
	return p.plan(ctx, observation, cred, c, claim, allocation, nil)
}

// PlanDelegatedWrite limits the selected prefix after resolving the authoritative
// append offset. Execution must serialize this cut with publication across all
// origins. The original full command and bound remain the immutable binding.
func (p *LogicalPlanner) PlanDelegatedWrite(ctx context.Context, observation *logicalview.Observation, cred fsapi.Cred, c fsapi.Command, claim fsapi.UUID, allocation fsapi.ObjectID, maxEnd int64) (Planned, error) {
	if c.Operation != fsapi.Write || maxEnd < 0 {
		return Planned{}, fsapi.Invalid
	}
	return p.plan(ctx, observation, cred, c, claim, allocation, &maxEnd)
}

func (p *LogicalPlanner) plan(ctx context.Context, observation *logicalview.Observation, cred fsapi.Cred, c fsapi.Command, claim fsapi.UUID, allocation fsapi.ObjectID, maxEnd *int64) (Planned, error) {
	if err := p.boundCommand(cred, c); err != nil {
		return Planned{}, err
	}
	original := c.Clone()
	image, plan, err := p.prepare(ctx, observation, &c)
	if err != nil {
		return Planned{}, err
	}
	if (c.Operation == fsapi.RenameOp || c.Operation == fsapi.LinkOp) && c.DestinationMount != c.Mount {
		return Planned{}, fsapi.CrossDevice
	}
	for _, id := range []fsapi.ObjectID{c.Object, c.Parent, c.Destination} {
		if id.Valid() && id.Home != c.Mount {
			return Planned{}, fsapi.Unsupported
		}
	}
	for _, e := range []storage.Entry{plan.entry(c.Parent, c.Name), plan.entry(c.Destination, c.NewName)} {
		if e.Target != nil && e.Target.Home != c.Mount {
			return Planned{}, fsapi.Unsupported
		}
	}
	if !claim.Valid() {
		return Planned{}, fsapi.Invalid
	}
	if c.Operation != fsapi.Create && c.Operation != fsapi.Mkdir && c.Operation != fsapi.SymlinkOp && allocation != (fsapi.ObjectID{}) {
		return Planned{}, fsapi.Invalid
	}
	if c.Operation == fsapi.Write && len(c.Data) == 0 {
		return Planned{}, fsapi.Invalid
	}
	if err = checkPermissions(ctx, p.config.Permissions, cred, original, c, plan); err != nil {
		return Planned{}, err
	}
	if maxEnd != nil {
		if c.Offset < 0 {
			return Planned{}, fsapi.Invalid
		}
		if c.Offset >= *maxEnd {
			return Planned{}, ErrDelegatedFileTooLarge
		}
		if remaining := *maxEnd - c.Offset; int64(len(c.Data)) > remaining {
			c.Data = c.Data[:remaining]
		}
	}
	config := planningConfig{root: image.Root, storageLimits: p.config.StorageLimits, logicalBytes: p.config.Limits.LogicalBytes, identities: func(ctx context.Context, creation bool) (fsapi.UUID, fsapi.ObjectID, error) {
		if creation && (!allocation.Valid() || allocation.Home != c.Mount) {
			return fsapi.UUID{}, fsapi.ObjectID{}, fsapi.Invalid
		}
		if err := p.config.View.ReserveIdentity(ctx, claim, allocation); err != nil {
			return fsapi.UUID{}, fsapi.ObjectID{}, err
		}
		return claim, allocation, nil
	}}
	prepared, err := prepareMutation(ctx, cred, c, plan, config)
	if err != nil {
		return Planned{}, err
	}
	if prepared.Mutation == nil {
		return Planned{}, fsapi.Invalid
	}
	if image.Position.Sequence == math.MaxUint64 {
		return Planned{}, fsapi.NoSpace
	}
	position := image.Position
	position.Sequence++
	operation, err := apply.MutationOperation(ctx, observation, claim, position, *prepared.Mutation)
	if err != nil {
		return Planned{}, err
	}
	// This is an internal immutable request binding, not a wire or normal3 codec.
	bound := original.Clone()
	bound.Name = base64.StdEncoding.EncodeToString([]byte(bound.Name))
	bound.NewName = base64.StdEncoding.EncodeToString([]byte(bound.NewName))
	bound.After = base64.StdEncoding.EncodeToString([]byte(bound.After))
	intent, err := json.Marshal(struct {
		Version     uint16
		Credentials fsapi.Cred
		Command     fsapi.Command
		MaxEnd      *int64 `json:",omitempty"`
	}{1, cred.Clone(), bound, maxEnd})
	if err != nil {
		return Planned{}, err
	}
	cut, err := observation.Cut()
	if err != nil {
		return Planned{}, err
	}
	result := prepared.Result
	result.Generation = p.config.View.Generation()
	if object, ok := plan.objects[prepared.Object]; ok {
		a := object.Attr
		result.Attr = &a
	}
	return Planned{Claim: claim, Cut: cut, Intent: intent, Mutation: *prepared.Mutation, Operation: operation, Result: result}, nil
}

// Observe authorizes and reads one atomic owner RAM cut. Open preserves a
// generation-bound access grant; Release retires that exact handle. Lookup
// resolves foreign aliases to their retained home object. Mutations and Sync
// require their separate ordered protocol paths and cannot bypass Plan.
func (p *LogicalPlanner) Observe(ctx context.Context, o *logicalview.Observation, cred fsapi.Cred, c fsapi.Command) (fsapi.Result, error) {
	if err := p.boundCommand(cred, c); err != nil {
		return fsapi.Result{}, err
	}
	original := c.Clone()
	image, plan, err := p.prepare(ctx, o, &c)
	if err != nil {
		return fsapi.Result{}, err
	}
	if err = checkPermissions(ctx, p.config.Permissions, cred, original, c, plan); err != nil {
		return fsapi.Result{}, err
	}
	result := fsapi.Result{Generation: p.config.View.Generation()}
	switch c.Operation {
	case fsapi.Lookup:
		if _, err = plan.directory(c.Parent); err != nil {
			return result, err
		}
		id := c.Parent
		switch c.Name {
		case ".":
		case "..":
			id = plan.objects[c.Parent].Parent.Object
		default:
			if err = validName(c.Name); err != nil {
				return result, err
			}
			entry := plan.entry(c.Parent, c.Name)
			if entry.Target == nil {
				return result, fsapi.NotFound
			}
			id = *entry.Target
		}
		object, err := o.Stat(ctx, id)
		if err != nil {
			return result, err
		}
		result.Attr = &object.Attr
	case fsapi.Open:
		if err = c.Flags.Validate(); err != nil {
			return result, err
		}
		if c.Flags.Create || c.Flags.Truncate || c.Flags.Exclusive {
			return result, fsapi.Invalid
		}
		object, err := o.Stat(ctx, c.Object)
		if err != nil {
			return result, err
		}
		result.Handle, err = o.Open(ctx, c.Object, c.Flags)
		if err != nil {
			return result, err
		}
		result.Attr = &object.Attr
	case fsapi.Release:
		if err = p.config.View.ReleaseHandle(ctx, c.Generation, c.Handle); err != nil {
			return result, err
		}
	default:
		var handled bool
		result, handled, err = observeView(ctx, c, plan, image, o, p.config.Limits, p.config.StorageLimits, true)
		if !handled {
			return result, fsapi.Unsupported
		}
		if err != nil {
			return result, err
		}
		result.Generation = p.config.View.Generation()
	}
	return result, nil
}

func (p *LogicalPlanner) prepare(ctx context.Context, o *logicalview.Observation, c *fsapi.Command) (storage.Image, *plan, error) {
	if err := ctx.Err(); err != nil {
		return storage.Image{}, nil, err
	}
	if o == nil || !o.BelongsTo(p.config.View) {
		return storage.Image{}, nil, fsapi.Stale
	}
	if len(c.Data) > p.config.Limits.IOBytes || c.Size < 0 || len(c.Name) > 255 || len(c.NewName) > 255 || len(c.After) > 255 {
		return storage.Image{}, nil, fsapi.Invalid
	}
	if c.Operation != fsapi.Truncate && c.TruncateTimes != (fsapi.TruncateTimes{}) {
		return storage.Image{}, nil, fsapi.Invalid
	}
	if c.Required != (fsapi.Position{}) {
		return storage.Image{}, nil, fsapi.Unsupported
	} // v1 source positions are not selected cuts.
	if c.Generation.Valid() && c.Generation != p.config.View.Generation() || (c.Handle != 0 || c.DirectoryRevision != 0) && !c.Generation.Valid() {
		return storage.Image{}, nil, fsapi.Stale
	}
	if c.Handle != 0 {
		id, flags, err := p.config.View.ResolveHandle(c.Generation, c.Handle)
		if err != nil {
			return storage.Image{}, nil, err
		}
		if c.Object.Valid() && c.Object != id {
			return storage.Image{}, nil, fsapi.Stale
		}
		c.Object = id
		if c.Operation == fsapi.Read && flags.Access == fsapi.WriteOnly || (c.Operation == fsapi.Write || c.Operation == fsapi.Truncate) && flags.Access == fsapi.ReadOnly {
			return storage.Image{}, nil, fsapi.Permission
		}
		if c.Operation == fsapi.Write && flags.Append {
			object, err := o.Stat(ctx, id)
			if err != nil {
				return storage.Image{}, nil, err
			}
			c.Offset = object.Attr.Size
		}
	}
	image, err := o.Image(ctx, c.Mount)
	if err != nil {
		return storage.Image{}, nil, err
	}
	combined := image
	volumes, err := o.Volumes(ctx)
	if err != nil {
		return storage.Image{}, nil, err
	}
	for _, volume := range volumes {
		if volume == c.Mount {
			continue
		}
		other, err := o.Image(ctx, volume)
		if err != nil {
			return storage.Image{}, nil, err
		}
		combined.Objects = append(combined.Objects, other.Objects...)
		combined.Entries = append(combined.Entries, other.Entries...)
	}
	plan := newPlan(combined, p.config.Clock.Now())
	if !plan.now.Valid() {
		return storage.Image{}, nil, fsapi.Invalid
	}
	return image, plan, nil
}

func (p *LogicalPlanner) boundCommand(cred fsapi.Cred, c fsapi.Command) error {
	if len(cred.Groups) > 1024 || len(c.Data) > p.config.Limits.IOBytes || c.Size < 0 || c.Operation < fsapi.Lookup || c.Operation > fsapi.Reap {
		return fsapi.Invalid
	}
	if len(c.Name) > 255 || len(c.NewName) > 255 || len(c.After) > 255 {
		return fsapi.NameTooLong
	}
	if c.DescriptionOnly && c.Operation != fsapi.Release {
		return fsapi.Invalid
	}
	return nil
}

// DelegatedGeneration verifies the built-in Unix policy and actual authority
// member's volume/history generation against a captured committed RAM image.
// The returned serving generation is the separate handle/visibility lifetime;
// neither it nor the member generation establishes normal serving authority.
func (p *LogicalPlanner) DelegatedGeneration(ctx context.Context, volume, generation fsapi.UUID) (fsapi.UUID, error) {
	if p == nil || p.config.View == nil {
		return fsapi.UUID{}, fsapi.Stale
	}
	if _, ok := p.config.Permissions.(UnixPermissions); !ok {
		return fsapi.UUID{}, fsapi.Permission
	}
	o, err := p.config.View.Begin(ctx)
	if err != nil {
		return fsapi.UUID{}, err
	}
	defer func() { _ = o.Close() }()
	image, err := o.Image(ctx, volume)
	if err != nil {
		return fsapi.UUID{}, err
	}
	if image.Volume != volume || image.Position.Generation != generation {
		return fsapi.UUID{}, fsapi.Stale
	}
	return p.config.View.Generation(), nil
}
