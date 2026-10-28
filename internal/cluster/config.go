package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

const maxConfigBytes = 1 << 20

// JSONFile loads the separate static-cluster schema, not the local validation
// CLI configuration. The existing file must be private and administratively
// exclusive; callers may not replace its ancestors while loading.
type JSONFile struct{ Path string }

type fileConfig struct {
	FormatVersion uint64       `json:"format_version"`
	Cluster       string       `json:"cluster_id"`
	Revision      uint64       `json:"registry_revision"`
	Nodes         []fileNode   `json:"nodes"`
	Volumes       []fileVolume `json:"volumes"`
}

type fileNode struct {
	ID        string    `json:"node_id"`
	Admission Admission `json:"admission"`
	Addresses []string  `json:"replication_addresses"`
}

type fileVolume struct {
	ID              string       `json:"volume_id"`
	Generation      string       `json:"generation"`
	Configuration   string       `json:"configuration"`
	Epoch           uint64       `json:"epoch"`
	CoordinatorTerm uint64       `json:"coordinator_term"`
	Members         []fileMember `json:"members"`
}

type fileMember struct {
	Node string `json:"node_id"`
	Role Role   `json:"role"`
}

var _ Loader = JSONFile{}

// Load reads one bounded, strictly named JSON snapshot. Every field is required;
// duplicate/unknown keys, case aliases, nulls and trailing values are rejected.
func (f JSONFile) Load(ctx context.Context) (_ Config, retErr error) {
	var raw fileConfig
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	file, err := openPrivate(f.Path)
	if err != nil {
		return Config{}, err
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, err
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("cluster JSON exceeds 1 MiB")
	}
	if err := decodeStrict(data, &raw); err != nil {
		return Config{}, fmt.Errorf("cluster configuration: %w", err)
	}
	if raw.FormatVersion != 1 {
		return Config{}, fmt.Errorf("unsupported static cluster format %d", raw.FormatVersion)
	}
	c, err := convertConfig(raw)
	if err != nil {
		return Config{}, err
	}
	if _, err := New(c); err != nil {
		return Config{}, err
	}
	return c, ctx.Err()
}

func convertConfig(raw fileConfig) (Config, error) {
	c := Config{Revision: raw.Revision}
	var err error
	if c.Cluster, err = fsapi.ParseUUID(raw.Cluster); err != nil {
		return c, err
	}
	for _, n := range raw.Nodes {
		node := Node{Admission: n.Admission}
		if node.ID, err = fsapi.ParseUUID(n.ID); err != nil {
			return c, err
		}
		for _, s := range n.Addresses {
			a, err := netip.ParseAddr(s)
			if err != nil || a.String() != s {
				return c, fmt.Errorf("noncanonical replication address %q", s)
			}
			node.Addresses = append(node.Addresses, a)
		}
		c.Nodes = append(c.Nodes, node)
	}
	for _, v := range raw.Volumes {
		volume := Volume{History: History{Epoch: v.Epoch, CoordinatorTerm: v.CoordinatorTerm}}
		if volume.ID, err = fsapi.ParseUUID(v.ID); err != nil {
			return c, err
		}
		if volume.History.Generation, err = fsapi.ParseUUID(v.Generation); err != nil {
			return c, err
		}
		if volume.History.Configuration, err = fsapi.ParseUUID(v.Configuration); err != nil {
			return c, err
		}
		for _, m := range v.Members {
			id, err := fsapi.ParseUUID(m.Node)
			if err != nil {
				return c, err
			}
			volume.Members = append(volume.Members, Member{Node: id, Role: m.Role})
		}
		c.Volumes = append(c.Volumes, volume)
	}
	return c, nil
}

func decodeStrict(raw []byte, dest any) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("JSON is not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := checkShape(d, reflect.TypeOf(dest).Elem()); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return json.Unmarshal(raw, dest)
}

func checkShape(d *json.Decoder, typ reflect.Type) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	switch typ.Kind() {
	case reflect.Struct:
		if tok != json.Delim('{') {
			return fmt.Errorf("expected JSON object")
		}
		fields := make(map[string]reflect.Type, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			fields[f.Tag.Get("json")] = f.Type
		}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			field, exists := fields[name]
			if !ok || !exists {
				return fmt.Errorf("unknown or duplicate JSON field %q", key)
			}
			delete(fields, name)
			if err := checkShape(d, field); err != nil {
				return fmt.Errorf("field %s: %w", name, err)
			}
		}
		if len(fields) != 0 {
			return fmt.Errorf("missing required JSON fields")
		}
		_, err = d.Token()
		return err
	case reflect.Slice:
		if tok != json.Delim('[') {
			return fmt.Errorf("expected JSON array")
		}
		for d.More() {
			if err := checkShape(d, typ.Elem()); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case reflect.String:
		if _, ok := tok.(string); !ok {
			return fmt.Errorf("expected JSON string")
		}
	case reflect.Uint64:
		if _, ok := tok.(json.Number); !ok {
			return fmt.Errorf("expected JSON unsigned integer")
		}
	default:
		return fmt.Errorf("unsupported JSON schema type %s", typ)
	}
	return nil
}

func openPrivate(name string) (*os.File, error) {
	if err := checkParents(name); err != nil {
		return nil, err
	}
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if st.Mode != unix.S_IFREG|0o600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Size > maxConfigBytes {
		return nil, errors.Join(fmt.Errorf("file must be private, singly linked, bounded and owned by daemon UID: %s", name), f.Close())
	}
	return f, nil
}

func checkParents(name string) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name || name == "/" {
		return fmt.Errorf("path must be clean and absolute: %q", name)
	}
	p := "/"
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(name), "/"), "/") {
		p = filepath.Join(p, part)
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fmt.Errorf("non-directory or symlink ancestor %s", p)
		}
	}
	return nil
}
