// Package retained9plab prepares external signatures for disposable lab runs.
// It has no native, installed, guest, publication or execution authority.
package retained9plab

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
)

const inputLimit = 512 << 10

// DomainKeys names private files held by the external operator, in domain order.
type DomainKeys struct {
	Session, Host, Guest string
}

// EnrollmentRequest names actual proposal/measurement inputs and signing keys.
// BootID is the only field that may fill an empty scope in the emitted proposal.
type EnrollmentRequest struct {
	Proposal, OperatorKey, BootID, ProviderNonce string
	Keys                                         []DomainKeys
	Mounts                                       []controlledruntime.GuestArtifactMount
	Budget                                       controlledruntime.GuestArtifactBudget
	Observer                                     controlledruntime.GuestObserver
}

// BindRequest signs the emitted provision against the actual runtime controller.
type BindRequest struct {
	Provision, OperatorKey, GroupID, Controller string
	Keys                                        []DomainKeys
}

// Run writes fresh inert handoff files; it never contacts a guest or starts I/O.
func Run(args []string) error {
	f := flag.NewFlagSet("hfs-retained9p-operator", flag.ContinueOnError)
	request := f.String("request", "", "private absolute request JSON")
	output := f.String("output", "", "fresh absolute output directory")
	if len(args) == 0 || (args[0] != "enroll" && args[0] != "bind") {
		return fmt.Errorf("requires enroll|bind --request FILE --output NEW_DIRECTORY")
	}
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || !absolute(*request) || !absolute(*output) {
		return fmt.Errorf("require absolute clean request and output paths")
	}
	var files map[string][]byte
	var err error
	if args[0] == "enroll" {
		files, err = enrollmentFiles(*request)
	} else {
		files, err = bindingFiles(*request)
	}
	if err != nil {
		return err
	}
	return writeFresh(*output, files)
}

func enrollmentFiles(path string) (map[string][]byte, error) {
	var request EnrollmentRequest
	if err := readJSON(path, &request); err != nil {
		return nil, err
	}
	var proposal viewcontrol.Enrollment
	if err := readJSON(request.Proposal, &proposal); err != nil {
		return nil, err
	}
	if len(proposal.Domains) != 2 || len(request.Keys) != 2 || len(request.Mounts) != 2 {
		return nil, fmt.Errorf("base gate requires exactly two original guest domains")
	}
	operator, err := readKey(request.OperatorKey)
	if err != nil {
		return nil, err
	}
	keys, err := orderedKeys(request.Keys)
	if err != nil {
		return nil, err
	}
	for i := range proposal.Domains {
		d := &proposal.Domains[i]
		if d.Scope.Session != request.Keys[i].Session ||
			d.Scope.Session != request.Mounts[i].Session || request.Mounts[i].DomainIndex != i {
			return nil, fmt.Errorf("domain %d: key/mount ordering differs from actual proposal", i)
		}
		if d.Scope.GuestBoot == "" {
			d.Scope.GuestBoot = request.BootID
		}
		if d.Scope.GuestBoot != request.BootID {
			return nil, fmt.Errorf("domain %d: proposal boot differs", i)
		}
	}
	a := viewcontrol.Authorization{Enrollment: proposal}
	raw, err := json.Marshal(proposal)
	if err != nil {
		return nil, err
	}
	a.Operator = ed25519.Sign(operator, raw)
	for _, key := range keys {
		a.Domains = append(a.Domains, viewcontrol.Signatures{
			Host: ed25519.Sign(key.Host, raw), Guest: ed25519.Sign(key.Guest, raw),
		})
	}
	group := controlledruntime.GuestProvisionGroup{
		Authorization: a, LocalMounts: request.Mounts, Signers: keys,
	}
	provision, err := controlledruntime.SignGuestProvision(request.BootID,
		request.ProviderNonce, []controlledruntime.GuestProvisionGroup{group},
		request.Budget, operator)
	if err != nil {
		return nil, fmt.Errorf("sign complete guest provision: %w", err)
	}
	handoff := controlledruntime.RuntimeEnrollment{
		Authorization: a, Provision: provision, Observer: request.Observer,
	}
	for i, domain := range proposal.Domains {
		domainBytes, err := json.Marshal(domain)
		if err != nil {
			return nil, err
		}
		handoff.Providers = append(handoff.Providers, controlledvalidation.Authorization{
			Enrollment: domain, OperatorSignature: ed25519.Sign(operator, domainBytes),
			HostSignature:  ed25519.Sign(keys[i].Host, domainBytes),
			GuestSignature: ed25519.Sign(keys[i].Guest, domainBytes),
		})
	}
	encoded, err := json.Marshal(handoff)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"enrollment.json": encoded, "provision.json": provision}, nil
}

func bindingFiles(path string) (map[string][]byte, error) {
	var request BindRequest
	if err := readJSON(path, &request); err != nil {
		return nil, err
	}
	operator, err := readKey(request.OperatorKey)
	if err != nil {
		return nil, err
	}
	keys, err := orderedKeys(request.Keys)
	if err != nil {
		return nil, err
	}
	provision, err := privateRead(request.Provision, 128<<10)
	if err != nil {
		return nil, err
	}
	bind, err := controlledruntime.SignGuestBind(provision, request.GroupID,
		request.Controller, operator, keys)
	if err != nil {
		return nil, fmt.Errorf("sign exact group controller binding: %w", err)
	}
	if err := bindingSessions(provision, request.GroupID, request.Keys); err != nil {
		return nil, err
	}
	return map[string][]byte{"bind.json": bind}, nil
}

func bindingSessions(provision []byte, groupID string, keys []DomainKeys) error {
	// SignGuestBind has already validated the complete canonical artifact,
	// original enrollment representations and every ordered signature.
	var artifact struct {
		Payload struct {
			Groups []struct {
				EnrollmentBytes string `json:"enrollment_bytes"`
			} `json:"groups"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(provision, &artifact); err != nil {
		return err
	}
	for _, group := range artifact.Payload.Groups {
		raw, err := hex.DecodeString(group.EnrollmentBytes)
		if err != nil {
			return err
		}
		var enrollment viewcontrol.Enrollment
		if err := json.Unmarshal(raw, &enrollment); err != nil {
			return err
		}
		if enrollment.ID.String() != groupID {
			continue
		}
		if len(enrollment.Domains) != len(keys) {
			return fmt.Errorf("binding original domain count differs")
		}
		for i, domain := range enrollment.Domains {
			if domain.Scope.Session != keys[i].Session {
				return fmt.Errorf("binding key labels differ from original domain order")
			}
		}
		return nil
	}
	return fmt.Errorf("binding group missing")
}

func orderedKeys(paths []DomainKeys) ([]controlledruntime.GuestArtifactSigners, error) {
	if len(paths) != 2 || paths[0].Session == paths[1].Session {
		return nil, fmt.Errorf("require two distinct original domain key records")
	}
	keys := make([]controlledruntime.GuestArtifactSigners, len(paths))
	for i, path := range paths {
		var err error
		if keys[i].Host, err = readKey(path.Host); err != nil {
			return nil, err
		}
		if keys[i].Guest, err = readKey(path.Guest); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func readKey(path string) (ed25519.PrivateKey, error) {
	raw, err := privateRead(path, 4096)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(raw, []byte("-----BEGIN PRIVATE KEY-----")) {
		block, rest := pem.Decode(raw)
		if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
			return nil, fmt.Errorf("require one unencrypted PKCS#8 private key")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#8 key: %w", err)
		}
		key, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("require Ed25519 PKCS#8 key")
		}
		return key, nil
	}
	decoded, err := hex.DecodeString(string(raw))
	if err != nil || hex.EncodeToString(decoded) != string(raw) {
		return nil, fmt.Errorf("private key must be exact lowercase hex, without newline")
	}
	if len(decoded) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(decoded), nil
	}
	if len(decoded) != ed25519.PrivateKeySize ||
		!bytes.Equal(decoded, ed25519.NewKeyFromSeed(decoded[:ed25519.SeedSize])) {
		return nil, fmt.Errorf("private key length/derivation mismatch")
	}
	return ed25519.PrivateKey(decoded), nil
}

func absolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/"
}

func privateRead(path string, maximum int64) ([]byte, error) {
	if !absolute(path) {
		return nil, fmt.Errorf("require absolute clean input path")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) ||
		stat.Mode&0077 != 0 || stat.Nlink != 1 || stat.Size > maximum {
		return nil, fmt.Errorf("input must be bounded private singly linked owned regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, errors.Join(fmt.Errorf("input read/bound"), err)
	}
	return raw, nil
}

func readJSON(path string, result any) error {
	raw, err := privateRead(path, inputLimit)
	if err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return fmt.Errorf("JSON is not UTF-8")
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	nodes := 0
	if err := uniqueJSON(tokens, 0, &nodes); err != nil {
		return err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON input")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return err
	}
	if err := exactSchema(value, reflect.TypeOf(result).Elem()); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(result)
}

func exactSchema(value any, typ reflect.Type) error {
	if value == nil {
		return nil // The actual signing validators reject required missing values.
	}
	if typ.Kind() == reflect.Pointer {
		return exactSchema(value, typ.Elem())
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("require JSON object for %s", typ.Name())
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
		for name, child := range object {
			field, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown or aliased JSON field %q", name)
			}
			if err := exactSchema(child, field); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
			return nil
		}
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("require JSON array")
		}
		for _, child := range array {
			if err := exactSchema(child, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func uniqueJSON(d *json.Decoder, depth int, nodes *int) error {
	*nodes++
	if depth > 32 || *nodes > 65536 {
		return fmt.Errorf("JSON complexity bound")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	if delim != '{' && delim != '[' {
		return fmt.Errorf("invalid JSON delimiter")
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate/nonstring JSON key")
			}
			seen[name] = true
		}
		if err := uniqueJSON(d, depth+1, nodes); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func writeFresh(directory string, files map[string][]byte) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	// Failed partial directories deliberately remain and cannot be reused.
	for name, raw := range files {
		f, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(raw)
		err = errors.Join(writeErr, f.Sync(), f.Close())
		if err != nil {
			return err
		}
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
