// Package daemoncli implements the local disposable daemon lifecycle. It does
// not implement normal replication, ownership admission or production mounts.
package daemoncli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

const formatVersion = 1

// Config selects exact local identities and disjoint administrative paths.
// Paths must be absolute, clean and contain no symlink components.
type Config struct {
	FormatVersion int    `json:"format_version"`
	NodeID        string `json:"node_id"`
	VolumeID      string `json:"volume_id"`
	ControlDir    string `json:"control_dir"`
	VolumeDir     string `json:"volume_dir"`
	Mountpoint    string `json:"mountpoint"`
}

type nodeIdentity struct {
	FormatVersion int    `json:"format_version"`
	NodeID        string `json:"node_id"`
}

type volumeIdentity struct {
	FormatVersion      int    `json:"format_version"`
	Profile            string `json:"profile"`
	NodeID             string `json:"node_id"`
	VolumeID           string `json:"volume_id"`
	Generation         string `json:"generation"`
	Configuration      string `json:"configuration"`
	RootAllocation     string `json:"root_allocation"`
	FixturePartner     string `json:"fixture_partner"`
	FixtureAsync       string `json:"fixture_async"`
	OwnerIncarnation   string `json:"owner_incarnation"`
	PartnerIncarnation string `json:"partner_incarnation"`
}

func loadConfig(ctx context.Context, name string) (Config, error) {
	var c Config
	if err := ctx.Err(); err != nil {
		return c, err
	}
	if err := readJSON(name, &c); err != nil {
		return c, fmt.Errorf("configuration: %w", err)
	}
	if c.FormatVersion != formatVersion {
		return c, fmt.Errorf("unsupported configuration format %d", c.FormatVersion)
	}
	for _, id := range []string{c.NodeID, c.VolumeID} {
		if _, err := fsapi.ParseUUID(id); err != nil {
			return c, err
		}
	}
	paths := []string{c.ControlDir, c.VolumeDir, c.Mountpoint}
	for i, p := range paths {
		if err := checkPath(p, true); err != nil {
			return c, err
		}
		for _, other := range paths[:i] {
			if containsPath(p, other) || containsPath(other, p) {
				return c, fmt.Errorf("administrative paths overlap: %s and %s", p, other)
			}
		}
	}
	return c, nil
}

func checkPath(name string, missingFinal bool) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name || name == "/" {
		return fmt.Errorf("path must be clean, absolute and below root: %q", name)
	}
	parts := strings.Split(strings.TrimPrefix(name, "/"), "/")
	p := "/"
	for i, part := range parts {
		p = filepath.Join(p, part)
		st, err := os.Lstat(p)
		if missingFinal && i == len(parts)-1 && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fmt.Errorf("path component is not a real directory: %s", p)
		}
	}
	return nil
}

func containsPath(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, parent+string(os.PathSeparator))
}

func privateDir(name string) error {
	if err := checkPath(name, false); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Lstat(name, &st); err != nil {
		return err
	}
	if st.Mode != unix.S_IFDIR|0o700 || st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("directory must be private mode 0700 and owned by daemon uid: %s", name)
	}
	return nil
}

func readJSON(name string, dest any) error {
	fields, err := jsonFields(dest)
	if err != nil {
		return err
	}
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode != unix.S_IFREG|0o600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Size > 64<<10 {
		return fmt.Errorf("JSON file must be a private, singly linked, bounded regular file: %s", name)
	}
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 || !utf8.Valid(raw) {
		return fmt.Errorf("JSON must be valid UTF-8 and at most 64 KiB")
	}
	// All local configuration/identity records are flat objects. Check exact
	// schema names before decoding: encoding/json otherwise matches tags without
	// regard to case, permitting a later differently cased identity/path override.
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	seen := make(map[string]bool)
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			return fmt.Errorf("duplicate or invalid JSON key %v", t)
		}
		if !slices.Contains(fields, key) {
			return fmt.Errorf("unknown JSON key %q; field names are case-sensitive", key)
		}
		seen[key] = true
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return err
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func jsonFields(dest any) ([]string, error) {
	switch dest.(type) {
	case *Config:
		return []string{"format_version", "node_id", "volume_id", "control_dir", "volume_dir", "mountpoint"}, nil
	case *nodeIdentity:
		return []string{"format_version", "node_id"}, nil
	case *volumeIdentity:
		return []string{"format_version", "profile", "node_id", "volume_id", "generation", "configuration", "root_allocation", "fixture_partner", "fixture_async", "owner_incarnation", "partner_incarnation"}, nil
	default:
		return nil, fmt.Errorf("unsupported local JSON schema %T", dest)
	}
}

func writeNewJSON(name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(name))
}

func syncDir(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func freshID() (fsapi.UUID, error) {
	var id fsapi.UUID
	if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
		return id, err
	}
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	return id, nil
}
