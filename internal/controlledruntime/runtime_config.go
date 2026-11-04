//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/daemoncli"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
)

// RuntimeConfig is a separate explicit disposable profile. It contains host
// receipt keys only; operator and guest signatures arrive through the bounded
// staged EnrollmentFile handoff after unpredictable native identities exist.
type RuntimeConfig struct {
	Version                                                 string
	VolumeConfig, OwnerDirectory, EnrollmentFile            string
	Exporter                                                Executable
	Operator                                                ed25519.PublicKey
	HostKeys                                                map[string]ed25519.PrivateKey
	GuestKeys                                               map[string]ed25519.PublicKey
	Scopes                                                  []guestbridge.Context
	Views                                                   viewcontrol.Bounds
	Service                                                 fsservice.Limits
	Providers                                               ProviderLimits
	Capabilities, CapabilityIdentities, CredentialRevisions int
	Budget                                                  RuntimeBudget
	BootstrapSeconds                                        int
}

// RuntimeEnrollment is external consent plus exact guest provisioning policy.
// It supplies no observed guest/native/source authority by itself.
type RuntimeEnrollment struct {
	Authorization viewcontrol.Authorization
	Providers     []controlledvalidation.Authorization
	Provision     []byte
	Observer      GuestObserver
}

func readRuntimeFile(path string, into any) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fsapi.Invalid
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	if err = checkOwned(f, false); err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > 512<<10 {
		return fsapi.NoSpace
	}
	raw, err := io.ReadAll(io.LimitReader(f, (512<<10)+1))
	if err != nil {
		return err
	}
	if len(raw) > 512<<10 || !utf8.Valid(raw) {
		return fsapi.Invalid
	}
	return decodeRuntimeJSON(raw, into)
}

func decodeRuntimeJSON(raw []byte, into any) error {
	if !utf8.Valid(raw) {
		return fsapi.Invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	value, err := runtimeJSONValue(d, 0, &nodes)
	if err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		return fsapi.Invalid
	}
	if err := runtimeSchema(value, reflect.TypeOf(into).Elem()); err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func runtimeSchema(value any, t reflect.Type) error {
	if value == nil {
		if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			return nil
		}
		return fsapi.Invalid
	}
	if t.Kind() == reflect.Pointer {
		return runtimeSchema(value, t.Elem())
	}
	switch t.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return fsapi.Invalid
		}
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
		for name, child := range object {
			typ, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown configuration field %q", name)
			}
			if err := runtimeSchema(child, typ); err != nil {
				return fmt.Errorf("%s.%s: %w", t.Name(), name, err)
			}
		}
	case reflect.Slice, reflect.Array:
		// []byte uses the encoding/json base64 representation.
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return nil
		}
		array, ok := value.([]any)
		if !ok {
			return fsapi.Invalid
		}
		for _, child := range array {
			if err := runtimeSchema(child, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return fsapi.Invalid
		}
		for _, child := range object {
			if err := runtimeSchema(child, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRuntimeConfig(ctx context.Context, c RuntimeConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Version != "homelabfs-controlled-runtime/1" || len(c.Operator) != ed25519.PublicKeySize || len(c.Scopes) < 1 || len(c.Scopes) > 16 || len(c.HostKeys) != len(c.Scopes) || len(c.GuestKeys) != len(c.Scopes) || c.BootstrapSeconds < 1 || c.BootstrapSeconds > 120 || c.Capabilities < 1 || c.CapabilityIdentities < c.Capabilities || c.CredentialRevisions < 2 || !guestViewBounds(c.Views) {
		return fsapi.Invalid
	}
	if c.Service.LogicalBytes != 4<<20 || c.Service.Queue < 1 || c.Service.Queue > 64 || c.Service.IOBytes < 1 || c.Service.IOBytes > 65536 || c.Service.Handles < 1 || c.Service.Handles > 1024 || c.Service.Lookups < 1 || c.Service.Lookups > 4096 || c.Service.Reservations < 1 || c.Service.Reservations > 4096 || c.Service.Locks < 1 || c.Service.Locks > 4096 {
		return fsapi.Invalid
	}
	paths := []string{c.VolumeConfig, c.OwnerDirectory, c.EnrollmentFile}
	for i, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
			return fsapi.Invalid
		}
		for _, other := range paths[:i] {
			if p == other || strings.HasPrefix(p, other+"/") || strings.HasPrefix(other, p+"/") {
				return fsapi.Invalid
			}
		}
	}
	if err := daemoncli.CheckControlledRuntimePaths(c.OwnerDirectory, c.EnrollmentFile); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, scope := range c.Scopes {
		if !validUUIDString(scope.Session) || seen[scope.Session] || scope.GuestBoot != c.Scopes[0].GuestBoot || scope.VM != c.Scopes[0].VM || scope.Volume != c.Scopes[0].Volume || scope.Export != c.Scopes[0].Export || len(c.HostKeys[scope.Session]) != ed25519.PrivateKeySize || len(c.GuestKeys[scope.Session]) != ed25519.PublicKeySize {
			return fsapi.Permission
		}
		if _, err := guestPrivateKey(c.HostKeys[scope.Session], nil); err != nil {
			return err
		}
		seen[scope.Session] = true
	}
	funding, err := fundRuntime(c.Budget, c.Providers, c.Views, len(c.Scopes))
	if err != nil {
		return err
	}
	return funding.owner.Release()
}

func runtimeJSONValue(d *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > 32 || *nodes > 65536 {
		return nil, fsapi.Invalid
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fsapi.Invalid
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate configuration field %q", name)
			}
			child, err := runtimeJSONValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			object[name] = child
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fsapi.Invalid
		}
		return object, nil
	case '[':
		array := []any{}
		for d.More() {
			child, err := runtimeJSONValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			array = append(array, child)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fsapi.Invalid
		}
		return array, nil
	}
	return nil, fsapi.Invalid
}
