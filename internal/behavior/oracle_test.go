package behavior

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRenameOracleRejectsMissingOrWrongReplacement(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		replacement bool
		wrongObject bool
		wantError   string
	}{
		{name: "actual-replacement", replacement: true},
		{name: "omitted-replacement", wantError: "still exists"},
		{name: "wrong-object-same-data", replacement: true, wrongObject: true, wantError: "wrong object identity"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			dir := int(root.Fd())
			original := createOracleFile(t, dir, "original", "original")
			defer func() { _ = unix.Close(original) }()
			replaced := createOracleFile(t, dir, "replacement", "replaced")
			defer func() { _ = unix.Close(replaced) }()
			if err := renameAt(dir, "original", dir, "renamed"); err != nil {
				t.Fatal(err)
			}
			if err := verifyRename(dir, "original", "renamed", original, []byte("original")); err != nil {
				t.Fatal(err)
			}
			if scenario.wrongObject {
				imposter := createOracleFile(t, dir, "imposter", "original")
				if err := unix.Close(imposter); err != nil {
					t.Fatal(err)
				}
				if err := renameAt(dir, "imposter", dir, "replacement"); err != nil {
					t.Fatal(err)
				}
				if err := unlinkAt(dir, "renamed", 0); err != nil {
					t.Fatal(err)
				}
			} else if scenario.replacement {
				if err := renameAt(dir, "renamed", dir, "replacement"); err != nil {
					t.Fatal(err)
				}
			}
			err = verifyRename(dir, "renamed", "replacement", original, []byte("original"))
			if scenario.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if scenario.wantError != "" && (err == nil || !strings.Contains(err.Error(), scenario.wantError)) {
				t.Fatalf("replacement oracle returned %v; want %q", err, scenario.wantError)
			}
			// The old destination fd alone passes even when replacement is omitted;
			// the production oracle above must also inspect the actual namespace.
			if err := readEqual(replaced, []byte("replaced")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAppendOracleRejectsActualFileCorruption(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		offset    int
		corrupt   byte
		duplicate bool
	}{
		{name: "valid", offset: -1},
		{name: "final-delimiter", offset: appendCount*2*appendRecordBytes - 1, corrupt: '!'},
		{name: "interior-delimiter", offset: appendRecordBytes - 1, corrupt: '!'},
		{name: "record-content", offset: 9, corrupt: '!'},
		{name: "duplicate-and-drop", offset: -1, duplicate: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := t.TempDir()
			root, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			if err := appendRecords(int(root.Fd()), "A"); err != nil {
				t.Fatal(err)
			}
			if err := appendRecords(int(root.Fd()), "B"); err != nil {
				t.Fatal(err)
			}
			if scenario.offset >= 0 || scenario.duplicate {
				file, err := os.OpenFile(filepath.Join(path, "append"), os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if scenario.offset >= 0 {
					n, err := file.WriteAt([]byte{scenario.corrupt}, int64(scenario.offset))
					if err != nil || n != 1 {
						_ = file.Close()
						t.Fatalf("corrupt file: %d %v", n, err)
					}
				}
				if scenario.duplicate {
					data := []byte(appendRecord("A", 0) + "\n")
					n, err := file.WriteAt(data, appendRecordBytes)
					if err != nil || n != len(data) {
						_ = file.Close()
						t.Fatalf("duplicate record: %d %v", n, err)
					}
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = verifyAppend(int(root.Fd()))
			if scenario.name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("actual verifier accepted corrupted exact-size append file")
			}
		})
	}
}

func createOracleFile(t *testing.T, dir int, name, data string) int {
	t.Helper()
	fd, err := openAt(dir, name, unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFull(fd, []byte(data)); err != nil {
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	return fd
}
