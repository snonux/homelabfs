// Package model holds bounded synthetic probe data, never workload data.
package model

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// Size is the maximum size of each synthetic file.
const Size = 4096

// State owns a small fixed set of synthetic files.
type State struct {
	mu   sync.Mutex
	data map[string][]byte
}

// New creates an isolated in-memory fixture.
func New() *State {
	s := &State{data: make(map[string][]byte)}
	for _, name := range []string{"data", "direct", "delayed", "cancel", "credentials", "lock"} {
		s.data[name] = bytes.Repeat([]byte("A"), Size)
	}
	return s
}

// Exists reports whether the fixture contains name.
func (s *State) Exists(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[name]
	return ok
}

// Read returns an owned copy of at most Size bytes.
func (s *State) Read(name string, off int64, size int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if off < 0 || off >= int64(len(s.data[name])) {
		return nil
	}
	end := min(int(off)+size, len(s.data[name]))
	return bytes.Clone(s.data[name][int(off):end])
}

// Write updates only existing bytes in a fixture file.
func (s *State) Write(name string, off int64, data []byte) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if off < 0 || off >= int64(len(s.data[name])) {
		return 0
	}
	return copy(s.data[name][int(off):], data)
}

// Mutate changes the cached file without going through the mounted kernel.
func (s *State) Mutate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data["data"] = bytes.Repeat([]byte("B"), Size/2)
}

// Len reports the current synthetic file length.
func (s *State) Len(name string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.data[name]))
}

// Credentials logs request identity and a best-effort Linux procfs observation.
// Procfs groups are diagnostic only: PID reuse/races make them unsuitable as an
// authorization source. Neither binding supplies supplementary groups here.
func Credentials(uid, gid, pid uint32, name string) {
	groups := "unavailable"
	if status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Groups:") {
				groups = line
			}
		}
	}
	log.Printf("event=credentials name=%s uid=%d gid=%d pid=%d proc_groups=%q", name, uid, gid, pid, groups)
}
