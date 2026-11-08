//go:build linux || freebsd

package runtimeprep

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// inputLimit bounds every JSON input, matching the runtime's own 512 KiB
// configuration/handoff bound and the external signer's request bound.
const inputLimit = 512 << 10

// readPrivate reads a clean absolute, private (no group/other bits), singly
// linked regular file owned by the caller, without following a final symlink.
// The same ownership rule as the external signer keeps a request or template
// that another account could have replaced from ever being consumed.
func readPrivate(path string, limit int64) ([]byte, error) {
	if !cleanAbsolute(path) {
		return nil, fmt.Errorf("require a clean absolute input path: %q", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) ||
		st.Mode&0o077 != 0 || st.Nlink != 1 || st.Size > limit {
		return nil, fmt.Errorf("%s must be a bounded private singly linked regular file owned by this account", path)
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, limit)
	}
	return raw, nil
}

// readStrict reads one private JSON document into a closed schema.
func readStrict(path string, into any) ([]byte, error) {
	raw, err := readPrivate(path, inputLimit)
	if err != nil {
		return nil, err
	}
	if err := decodeStrict(raw, into); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return raw, nil
}

// decodeStrict refuses invalid UTF-8, duplicate keys, trailing data, unknown
// fields and the case-insensitive field aliases that encoding/json would
// otherwise accept, so the bytes a reviewer reads are the bytes consumed.
func decodeStrict(raw []byte, into any) error {
	if !utf8.Valid(raw) {
		return errors.New("JSON is not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	nodes := 0
	if err := uniqueKeys(json.NewDecoder(bytes.NewReader(raw)), 0, &nodes); err != nil {
		return err
	}
	if err := exactSchema(value, reflect.TypeOf(into).Elem()); err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// uniqueKeys walks the token stream once; Decode into a map silently keeps the
// last duplicate, which would hide a conflicting second value from review.
// Depth and node bounds match the runtime's own configuration decoder.
func uniqueKeys(d *json.Decoder, depth int, nodes *int) error {
	*nodes++
	if depth > 32 || *nodes > 65536 {
		return errors.New("JSON complexity bound")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, _ := key.(string)
			if seen[name] {
				return fmt.Errorf("duplicate JSON key %q", name)
			}
			seen[name] = true
		}
		if err := uniqueKeys(d, depth+1, nodes); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

// exactSchema requires every object key to be the exact Go field name (or its
// json tag) of the destination struct. Values are type-checked by Unmarshal.
func exactSchema(value any, t reflect.Type) error {
	if value == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return exactObject(value, t)
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil // []byte is base64 text; [32]byte is a number array checked by Unmarshal.
		}
		array, ok := value.([]any)
		if !ok {
			return errors.New("require a JSON array")
		}
		for _, child := range array {
			if err := exactSchema(child, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("require a JSON object")
		}
		for _, child := range object {
			if err := exactSchema(child, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// exactObject checks one JSON object against the exact field names of struct t.
func exactObject(value any, t reflect.Type) error {
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("require a JSON object for %s", t.Name())
	}
	fields := structFields(t)
	for name, child := range object {
		field, ok := fields[name]
		if !ok {
			return fmt.Errorf("unknown or aliased JSON field %q in %s", name, t.Name())
		}
		if err := exactSchema(child, field); err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), name, err)
		}
	}
	return nil
}

func structFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields
}

// writeFreshDirectory creates a new private directory tree and writes each
// relative file exclusively with mode 0600. A failed attempt deliberately
// leaves its partial directory in place; it cannot be reused, so a retry needs
// a new output path and nothing half-written is ever silently completed.
func writeFreshDirectory(root string, files map[string][]byte) error {
	if !cleanAbsolute(root) {
		return fmt.Errorf("require a clean absolute output directory: %q", root)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return fmt.Errorf("create fresh output directory: %w", err)
	}
	directories := map[string]bool{root: true}
	for name, raw := range files {
		path := filepath.Join(root, name)
		if !strings.HasPrefix(path, root+"/") || filepath.Clean(name) != name {
			return fmt.Errorf("output name escapes the bundle: %q", name)
		}
		parent := filepath.Dir(path)
		if !directories[parent] {
			if err := os.Mkdir(parent, 0o700); err != nil {
				return err
			}
			directories[parent] = true
		}
		if err := writeExclusive(path, raw); err != nil {
			return err
		}
	}
	for directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return syncDirectory(filepath.Dir(root))
}

// writeExclusive creates one new 0600 file, never replacing an existing name.
func writeExclusive(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("write %s (partial file preserved): %w", path, err)
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/"
}
