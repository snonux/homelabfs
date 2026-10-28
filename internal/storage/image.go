package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type attrWire struct {
	Object   idWire          `json:"object_id"`
	Kind     fsapi.Kind      `json:"type"`
	UID      uint32          `json:"uid"`
	GID      uint32          `json:"gid"`
	Mode     uint16          `json:"mode"`
	Size     int64           `json:"size"`
	Nlink    uint64          `json:"nlink"`
	Revision uint64          `json:"revision"`
	Atime    fsapi.Timestamp `json:"atime"`
	Mtime    fsapi.Timestamp `json:"mtime"`
	Ctime    fsapi.Timestamp `json:"ctime"`
}
type referenceWire struct {
	Parent   idWire `json:"parent"`
	Name     string `json:"name_base64"`
	Revision uint64 `json:"revision"`
}
type parentWire struct {
	Revision uint64 `json:"revision"`
	Object   idWire `json:"object_id"`
	Entry    uint64 `json:"entry_revision"`
}
type objectWire struct {
	Descriptor descriptorWire  `json:"descriptor"`
	Attr       attrWire        `json:"attributes"`
	Text       string          `json:"symlink_base64"`
	Ledger     uint64          `json:"ledger_revision"`
	References []referenceWire `json:"references"`
	Parent     *parentWire     `json:"parent"`
}
type entryWire struct {
	Parent   idWire  `json:"parent"`
	Name     string  `json:"name_base64"`
	Revision uint64  `json:"revision"`
	Target   *idWire `json:"target"`
}
type pinWire struct {
	ID          string `json:"id"`
	Object      idWire `json:"object_id"`
	Generation  string `json:"generation"`
	Incarnation string `json:"incarnation"`
	Active      bool   `json:"active"`
}
type positionWire struct {
	Volume        string `json:"volume_uuid"`
	Generation    string `json:"generation"`
	Configuration string `json:"configuration"`
	Epoch         uint64 `json:"epoch"`
	Sequence      uint64 `json:"sequence"`
}
type receiptWire struct {
	Sequence uint64 `json:"sequence"`
	ID       string `json:"id"`
	Hash     string `json:"sha256"`
}
type payloadWire struct {
	Object idWire `json:"object_id"`
	Size   int64  `json:"size"`
	Hash   string `json:"sha256"`
}
type imageWire struct {
	Magic    string           `json:"magic"`
	Major    uint16           `json:"major"`
	Minor    uint16           `json:"minor"`
	Volume   string           `json:"volume_uuid"`
	Root     idWire           `json:"root_object_id"`
	Position positionWire     `json:"position"`
	Objects  []objectWire     `json:"objects"`
	Entries  []entryWire      `json:"entries"`
	Pins     []pinWire        `json:"pins"`
	Retired  []descriptorWire `json:"retired_allocations"`
	Receipts []receiptWire    `json:"receipts"`
	Payloads []payloadWire    `json:"payloads"`
}

// Export syncs all current payloads and identity/name dependencies and captures a
// clean image at the installed boundary. The caller must hold closure gates and
// preserve an exact matching payload baseline before applying later operations.
// Export alone does not publish a checkpoint or authorize retention advancement.
func (s *Store) Export(ctx context.Context) (Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return Image{}, err
	}
	image := Image{Version: 1, Volume: s.config.Volume, Root: s.config.Root, Position: s.position}
	for _, o := range s.objects {
		image.Objects = append(image.Objects, cloneObject(o))
		if err := s.syncObject(ctx, o); err != nil {
			s.ready = false
			return Image{}, err
		}
		if o.Attr.Kind == fsapi.Regular {
			p, err := s.hashPayload(ctx, o)
			if err != nil {
				s.ready = false
				return Image{}, err
			}
			image.Payloads = append(image.Payloads, p)
		}
	}
	for _, e := range s.entries {
		image.Entries = append(image.Entries, cloneEntry(e))
	}
	for _, p := range s.pins {
		image.Pins = append(image.Pins, p)
	}
	for _, d := range s.retired {
		image.Retired = append(image.Retired, d)
	}
	for _, r := range s.receipts {
		image.Receipts = append(image.Receipts, r)
	}
	sortImage(&image)
	return image, nil
}

// EncodeImage serializes a clean image using deterministic version 1.0 JSON.
// Raw names and symlink text use canonical padded base64; IDs use UUID text.
// Limits apply symmetrically to encoding and decoding; zero selects defaults.
func EncodeImage(image Image, limits Limits) ([]byte, error) {
	copyImage := image
	copyImage.Objects = slices.Clone(image.Objects)
	copyImage.Entries = slices.Clone(image.Entries)
	copyImage.Pins = slices.Clone(image.Pins)
	copyImage.Retired = slices.Clone(image.Retired)
	copyImage.Receipts = slices.Clone(image.Receipts)
	copyImage.Payloads = slices.Clone(image.Payloads)
	sortImage(&copyImage)
	image = copyImage
	if err := validateImage(image, limits); err != nil {
		return nil, err
	}
	w := imageWire{Magic: "HOMELABFS-CATALOG", Major: 1, Minor: 0, Volume: image.Volume.String(), Root: toID(image.Root), Position: positionWire{image.Position.Volume.String(), image.Position.Generation.String(), image.Position.Configuration.String(), image.Position.Epoch, image.Position.Sequence}, Objects: []objectWire{}, Entries: []entryWire{}, Pins: []pinWire{}, Retired: []descriptorWire{}, Receipts: []receiptWire{}, Payloads: []payloadWire{}}
	for _, o := range image.Objects {
		a := o.Attr
		obj := objectWire{Descriptor: toDescriptor(o.Descriptor), Attr: attrWire{toID(a.Object), a.Kind, a.UID, a.GID, a.Mode, a.Size, a.Nlink, a.Revision, a.Atime, a.Mtime, a.Ctime}, Text: base64.StdEncoding.EncodeToString(o.Text), Ledger: o.LedgerRevision, References: []referenceWire{}}
		for _, r := range o.References {
			obj.References = append(obj.References, referenceWire{toID(r.Parent), base64.StdEncoding.EncodeToString([]byte(r.Name)), r.Revision})
		}
		if o.Parent != nil {
			obj.Parent = &parentWire{o.Parent.Revision, toID(o.Parent.Object), o.Parent.EntryRevision}
		}
		w.Objects = append(w.Objects, obj)
	}
	for _, e := range image.Entries {
		v := entryWire{Parent: toID(e.Parent), Name: base64.StdEncoding.EncodeToString([]byte(e.Name)), Revision: e.Revision}
		if e.Target != nil {
			id := toID(*e.Target)
			v.Target = &id
		}
		w.Entries = append(w.Entries, v)
	}
	for _, p := range image.Pins {
		w.Pins = append(w.Pins, pinWire{p.ID.String(), toID(p.Object), p.Generation.String(), p.Incarnation.String(), p.Active})
	}
	for _, d := range image.Retired {
		w.Retired = append(w.Retired, toDescriptor(d))
	}
	for _, r := range image.Receipts {
		w.Receipts = append(w.Receipts, receiptWire{r.Sequence, r.ID.String(), hex.EncodeToString(r.SHA256[:])})
	}
	for _, p := range image.Payloads {
		w.Payloads = append(w.Payloads, payloadWire{toID(p.Object), p.Size, hex.EncodeToString(p.SHA256[:])})
	}
	raw, err := jsonBytes(w)
	if err != nil {
		return nil, err
	}
	maxBytes := limits.FileBytes
	if maxBytes == 0 {
		maxBytes = 16 << 20
	}
	if int64(len(raw)) > maxBytes {
		return nil, fsapi.NoSpace
	}
	return raw, nil
}

// DecodeImage rejects unknown fields/versions, duplicate keys/identities, unsafe
// ranges and noncanonical IDs/base64. It does not authorize history or serving.
func DecodeImage(raw []byte, limits Limits) (Image, error) {
	maxBytes := limits.FileBytes
	if maxBytes == 0 {
		maxBytes = 16 << 20
	}
	if maxBytes < 1 || int64(len(raw)) > maxBytes {
		return Image{}, fsapi.NoSpace
	}
	var w imageWire
	if err := strictJSON(raw, &w); err != nil {
		return Image{}, err
	}
	if w.Magic != "HOMELABFS-CATALOG" || w.Major != 1 || w.Minor != 0 {
		return Image{}, fsapi.Invalid
	}
	var image Image
	image.Version = 1
	var err error
	image.Volume, err = fsapi.ParseUUID(w.Volume)
	if err != nil {
		return Image{}, err
	}
	image.Root, err = fromID(w.Root)
	if err != nil {
		return Image{}, err
	}
	image.Position, err = fromPosition(w.Position)
	if err != nil {
		return Image{}, err
	}
	for _, v := range w.Objects {
		o, e := fromObject(v)
		if e != nil {
			return Image{}, e
		}
		image.Objects = append(image.Objects, o)
	}
	for _, v := range w.Entries {
		p, e := fromID(v.Parent)
		if e != nil {
			return Image{}, e
		}
		name, e := decodeBase64(v.Name)
		if e != nil {
			return Image{}, e
		}
		entry := Entry{Parent: p, Name: string(name), Revision: v.Revision}
		if v.Target != nil {
			id, e := fromID(*v.Target)
			if e != nil {
				return Image{}, e
			}
			entry.Target = &id
		}
		image.Entries = append(image.Entries, entry)
	}
	for _, v := range w.Pins {
		p, e := fromPin(v)
		if e != nil {
			return Image{}, e
		}
		image.Pins = append(image.Pins, p)
	}
	for _, v := range w.Retired {
		d, e := fromDescriptor(v)
		if e != nil {
			return Image{}, e
		}
		image.Retired = append(image.Retired, d)
	}
	for _, v := range w.Receipts {
		id, e := fsapi.ParseUUID(v.ID)
		if e != nil {
			return Image{}, e
		}
		h, e := decodeHash(v.Hash)
		if e != nil {
			return Image{}, e
		}
		image.Receipts = append(image.Receipts, Receipt{v.Sequence, id, h})
	}
	for _, v := range w.Payloads {
		id, e := fromID(v.Object)
		if e != nil {
			return Image{}, e
		}
		h, e := decodeHash(v.Hash)
		if e != nil {
			return Image{}, e
		}
		image.Payloads = append(image.Payloads, Payload{id, v.Size, h})
	}
	if err = validateImage(image, limits); err != nil {
		return Image{}, err
	}
	return image, nil
}

func validateImage(image Image, limits Limits) error {
	if !slices.IsSortedFunc(image.Objects, func(a, b Object) int { return lessID(a.Attr.Object, b.Attr.Object) }) || !slices.IsSortedFunc(image.Entries, func(a, b Entry) int {
		if c := lessID(a.Parent, b.Parent); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	}) || !slices.IsSortedFunc(image.Pins, func(a, b Pin) int { return bytesCompare(a.ID, b.ID) }) || !slices.IsSortedFunc(image.Retired, func(a, b Descriptor) int { return lessID(a.Object, b.Object) }) || !slices.IsSortedFunc(image.Payloads, func(a, b Payload) int { return lessID(a.Object, b.Object) }) {
		return fmt.Errorf("%w: noncanonical catalog order", fsapi.Invalid)
	}
	s, err := newStore(noopFS{}, noopFS{}, Config{Volume: image.Volume, Root: image.Root, Limits: limits})
	if err != nil {
		return err
	}
	if image.Version != 1 || !validPosition(image.Position, image.Volume) {
		return fsapi.Invalid
	}
	s.position = image.Position
	for _, o := range image.Objects {
		if _, ok := s.objects[o.Attr.Object]; ok {
			return fsapi.Invalid
		}
		s.objects[o.Attr.Object] = o
	}
	for _, e := range image.Entries {
		k := entryKey{e.Parent, e.Name}
		if _, ok := s.entries[k]; ok {
			return fsapi.Invalid
		}
		s.entries[k] = e
	}
	for _, p := range image.Pins {
		if _, ok := s.pins[p.ID]; ok {
			return fsapi.Invalid
		}
		s.pins[p.ID] = p
	}
	for _, d := range image.Retired {
		if _, ok := s.retired[d.Object]; ok {
			return fsapi.Invalid
		}
		s.retired[d.Object] = d
	}
	if len(image.Receipts) > s.config.Limits.Operations || uint64(len(image.Receipts)) != image.Position.Sequence {
		return fsapi.Invalid
	}
	ids := map[fsapi.UUID]bool{}
	for i, r := range image.Receipts {
		if r.Sequence != uint64(i)+1 || !r.ID.Valid() || ids[r.ID] {
			return fsapi.Invalid
		}
		ids[r.ID] = true
	}
	payloads := map[fsapi.ObjectID]bool{}
	for _, p := range image.Payloads {
		o, ok := s.objects[p.Object]
		if !ok || o.Attr.Kind != fsapi.Regular || p.Size != o.Attr.Size || payloads[p.Object] {
			return fsapi.Invalid
		}
		payloads[p.Object] = true
	}
	for id, o := range s.objects {
		if o.Attr.Kind == fsapi.Regular && !payloads[id] {
			return fsapi.Invalid
		}
	}
	return s.validateCatalog()
}

// noopFS is used only for pure image validation; no method is called.
type noopFS struct{ Filesystem }

func (s *Store) loadImage(ctx context.Context, image Image) error {
	if image.Volume != s.config.Volume || image.Root != s.config.Root {
		return fsapi.Invalid
	}
	if err := validateImage(image, s.config.Limits); err != nil {
		return err
	}
	for _, o := range image.Objects {
		if err := s.verifyDescriptor(ctx, o.Descriptor); err != nil {
			return err
		}
		s.objects[o.Attr.Object] = cloneObject(o)
	}
	for _, d := range image.Retired {
		if err := s.verifyDescriptor(ctx, d); err != nil {
			return err
		}
		s.retired[d.Object] = d
	}
	for _, e := range image.Entries {
		s.entries[entryKey{e.Parent, e.Name}] = cloneEntry(e)
	}
	for _, p := range image.Pins {
		s.pins[p.ID] = p
	}
	for _, r := range image.Receipts {
		s.receipts[r.Sequence] = r
	}
	s.position = image.Position
	for _, p := range image.Payloads {
		actual, err := s.hashPayload(ctx, s.objects[p.Object])
		if err != nil {
			return err
		}
		if actual != p {
			return fmt.Errorf("%w: payload is not the selected clean baseline", fsapi.Unavailable)
		}
	}
	return s.validateTree(ctx)
}
func (s *Store) verifyDescriptor(ctx context.Context, d Descriptor) error {
	if err := s.checkPrivate(ctx, objectPath(d.Object), true); err != nil {
		return err
	}
	raw, err := readPrivate(ctx, s.data, objectPath(d.Object)+"/identity.json", s.config.OwnerUID, s.config.Limits.FileBytes)
	if err != nil {
		return err
	}
	var v descriptorWire
	if err = strictJSON(raw, &v); err != nil {
		return err
	}
	actual, err := fromDescriptor(v)
	if err != nil {
		return err
	}
	if actual != d {
		return fmt.Errorf("%w: immutable descriptor mismatch", fsapi.IO)
	}
	return nil
}
func (s *Store) syncObject(ctx context.Context, o Object) error {
	dir := objectPath(o.Attr.Object)
	files := []string{"identity.json"}
	if o.Attr.Kind == fsapi.Regular {
		files = append(files, "payload")
	}
	for _, name := range files {
		if err := s.checkPrivate(ctx, dir+"/"+name, false); err != nil {
			return err
		}
		f, err := s.data.Open(ctx, dir+"/"+name, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		err = f.Sync(ctx)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for _, p := range []string{dir, "objects/" + o.Attr.Object.Allocation.String()[:2], "objects", "."} {
		if err := s.data.SyncDir(ctx, p); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) hashPayload(ctx context.Context, o Object) (Payload, error) {
	p := objectPath(o.Attr.Object) + "/payload"
	if err := s.checkPrivate(ctx, p, false); err != nil {
		return Payload{}, err
	}
	info, err := s.data.Stat(ctx, p)
	if err != nil {
		return Payload{}, err
	}
	if info.Size != o.Attr.Size {
		return Payload{}, fmt.Errorf("%w: baseline payload size", fsapi.Unavailable)
	}
	f, err := s.data.Open(ctx, p, os.O_RDONLY, 0)
	if err != nil {
		return Payload{}, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	buf := make([]byte, 64<<10)
	for off := int64(0); off < info.Size; {
		n := min(int64(len(buf)), info.Size-off)
		count, e := f.ReadAt(ctx, buf[:n], off)
		if e != nil {
			return Payload{}, e
		}
		if int64(count) != n {
			return Payload{}, io.ErrUnexpectedEOF
		}
		_, _ = h.Write(buf[:n])
		off += n
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return Payload{o.Attr.Object, info.Size, sum}, nil
}
func sortImage(i *Image) {
	slices.SortFunc(i.Objects, func(a, b Object) int { return lessID(a.Attr.Object, b.Attr.Object) })
	slices.SortFunc(i.Entries, func(a, b Entry) int {
		if c := lessID(a.Parent, b.Parent); c != 0 {
			return c
		}
		return compareRef(Reference{Name: a.Name}, Reference{Name: b.Name})
	})
	slices.SortFunc(i.Pins, func(a, b Pin) int { return bytesCompare(a.ID, b.ID) })
	slices.SortFunc(i.Retired, func(a, b Descriptor) int { return lessID(a.Object, b.Object) })
	sort.Slice(i.Receipts, func(a, b int) bool { return i.Receipts[a].Sequence < i.Receipts[b].Sequence })
	slices.SortFunc(i.Payloads, func(a, b Payload) int { return lessID(a.Object, b.Object) })
}
func bytesCompare(a, b fsapi.UUID) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
func toDescriptor(d Descriptor) descriptorWire {
	return descriptorWire{"HOMELABFS-OBJECT", 1, toID(d.Object), d.Kind, d.Creation.String()}
}
func fromDescriptor(v descriptorWire) (Descriptor, error) {
	id, err := fromID(v.Object)
	if err != nil {
		return Descriptor{}, err
	}
	creation, err := fsapi.ParseUUID(v.Creation)
	if err != nil {
		return Descriptor{}, err
	}
	if v.Magic != "HOMELABFS-OBJECT" || v.Major != 1 || v.Kind < fsapi.Regular || v.Kind > fsapi.Symlink {
		return Descriptor{}, fsapi.Invalid
	}
	return Descriptor{id, v.Kind, creation}, nil
}
func fromID(v idWire) (fsapi.ObjectID, error) {
	h, err := fsapi.ParseUUID(v.Home)
	if err != nil {
		return fsapi.ObjectID{}, err
	}
	a, err := fsapi.ParseUUID(v.Allocation)
	return fsapi.ObjectID{Home: h, Allocation: a}, err
}
func fromPosition(v positionWire) (fsapi.Position, error) {
	volume, err := fsapi.ParseUUID(v.Volume)
	if err != nil {
		return fsapi.Position{}, err
	}
	g, err := fsapi.ParseUUID(v.Generation)
	if err != nil {
		return fsapi.Position{}, err
	}
	c, err := fsapi.ParseUUID(v.Configuration)
	return fsapi.Position{Volume: volume, Generation: g, Configuration: c, Epoch: v.Epoch, Sequence: v.Sequence}, err
}
func fromObject(v objectWire) (Object, error) {
	d, err := fromDescriptor(v.Descriptor)
	if err != nil {
		return Object{}, err
	}
	id, err := fromID(v.Attr.Object)
	if err != nil {
		return Object{}, err
	}
	a := v.Attr
	o := Object{Descriptor: d, Attr: fsapi.Attr{Object: id, Kind: a.Kind, UID: a.UID, GID: a.GID, Mode: a.Mode, Size: a.Size, Nlink: a.Nlink, Revision: a.Revision, Atime: a.Atime, Mtime: a.Mtime, Ctime: a.Ctime}, LedgerRevision: v.Ledger}
	o.Text, err = decodeBase64(v.Text)
	if len(o.Text) == 0 {
		o.Text = nil
	}
	if err != nil {
		return Object{}, err
	}
	for _, r := range v.References {
		p, e := fromID(r.Parent)
		if e != nil {
			return Object{}, e
		}
		n, e := decodeBase64(r.Name)
		if e != nil {
			return Object{}, e
		}
		o.References = append(o.References, Reference{p, string(n), r.Revision})
	}
	if v.Parent != nil {
		p, e := fromID(v.Parent.Object)
		if e != nil {
			return Object{}, e
		}
		o.Parent = &Parent{v.Parent.Revision, p, v.Parent.Entry}
	}
	return o, nil
}
func fromPin(v pinWire) (Pin, error) {
	id, e := fsapi.ParseUUID(v.ID)
	if e != nil {
		return Pin{}, e
	}
	o, e := fromID(v.Object)
	if e != nil {
		return Pin{}, e
	}
	g, e := fsapi.ParseUUID(v.Generation)
	if e != nil {
		return Pin{}, e
	}
	inc, e := fsapi.ParseUUID(v.Incarnation)
	return Pin{id, o, g, inc, v.Active}, e
}
func decodeBase64(v string) ([]byte, error) {
	b, err := base64.StdEncoding.Strict().DecodeString(v)
	if err != nil || base64.StdEncoding.EncodeToString(b) != v {
		return nil, fsapi.Invalid
	}
	return b, nil
}
func decodeHash(v string) ([32]byte, error) {
	var h [32]byte
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != 32 || hex.EncodeToString(b) != v {
		return h, fsapi.Invalid
	}
	copy(h[:], b)
	return h, nil
}
