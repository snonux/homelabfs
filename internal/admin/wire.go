package admin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Call verifies the private socket and its kernel peer before exchanging one request.
func Call(ctx context.Context, path string, request Request) (Response, error) {
	var response Response
	if err := privateSocket(path); err != nil {
		return response, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return response, err
	}
	defer func() { _ = conn.Close() }()
	c := conn.(*net.UnixConn)
	peer, err := credentials(c)
	if err != nil {
		return response, err
	}
	if peer.UID != uint32(os.Geteuid()) {
		return response, fmt.Errorf("admin server uid mismatch")
	}
	deadline := time.Now().Add(ioTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.SetDeadline(deadline); err != nil {
		return response, err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if err := writeFrame(c, request); err != nil {
		return response, err
	}
	raw, err := readFrame(c)
	if err != nil {
		return response, err
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return response, err
	}
	if response.Version != protocolVersion {
		return response, fmt.Errorf("unsupported admin response")
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	return response, nil
}

// MarshalJSON always carries an explicit enabled value for a fault toggle,
// including false, while status carries no control fields.
func (r Request) MarshalJSON() ([]byte, error) {
	type wire struct {
		Version   int    `json:"version"`
		Operation string `json:"operation"`
		Fault     string `json:"fault,omitempty"`
		Enabled   *bool  `json:"enabled,omitempty"`
	}
	var enabled *bool
	if r.Operation == "test-fault" {
		value := r.Enabled
		enabled = &value
	}
	return json.Marshal(wire{Version: r.Version, Operation: r.Operation, Fault: r.Fault, Enabled: enabled})
}

func readFrame(r io.Reader) ([]byte, error) {
	raw, err := bufio.NewReaderSize(io.LimitReader(r, maxFrame+1), maxFrame+1).ReadSlice('\n')
	if err != nil || len(raw) > maxFrame {
		return nil, fmt.Errorf("admin frame must be one newline-terminated JSON object, at most %d bytes", maxFrame)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid admin UTF-8")
	}
	return raw, nil
}
func writeFrame(w io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw)+1 > maxFrame {
		return fmt.Errorf("admin response exceeds bound")
	}
	n, err := w.Write(append(raw, '\n'))
	if err == nil && n != len(raw)+1 {
		return io.ErrShortWrite
	}
	return err
}
func decodeRequest(raw []byte) (Request, error) {
	var request Request
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return request, fmt.Errorf("expected request object")
	}
	seen := map[string]bool{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return request, err
		}
		k, ok := t.(string)
		if !ok || seen[k] || k != "version" && k != "operation" && k != "fault" && k != "enabled" {
			return request, fmt.Errorf("unknown/duplicate request field")
		}
		seen[k] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return request, err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return request, fmt.Errorf("null request field")
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&request); err != nil {
		return request, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return request, fmt.Errorf("trailing request data")
	}
	if request.Version != protocolVersion || request.Operation == "" || len(request.Operation) > 64 || len(request.Fault) > 64 {
		return request, fmt.Errorf("invalid request version/operation")
	}
	if request.Operation == "status" && (seen["fault"] || seen["enabled"]) {
		return request, fmt.Errorf("status has no control fields")
	}
	if request.Operation == "test-fault" && (!seen["fault"] || !seen["enabled"] || request.Fault == "") {
		return request, fmt.Errorf("test-fault requires fault and enabled")
	}
	return request, nil
}
func privateParent(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("admin socket path must be clean and absolute")
	}
	parent := filepath.Dir(path)
	for p := parent; ; p = filepath.Dir(p) {
		var st unix.Stat_t
		if err := unix.Lstat(p, &st); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("admin parent is not a real directory")
		}
		if p == parent && (st.Mode != unix.S_IFDIR|0o700 || st.Uid != uint32(os.Geteuid())) {
			return fmt.Errorf("admin directory must be owned mode 0700")
		}
		if p == "/" {
			break
		}
	}
	return nil
}
func privateSocket(path string) error {
	if err := privateParent(path); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Mode != unix.S_IFSOCK|0o600 || st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("admin socket must be owned mode 0600")
	}
	return nil
}
