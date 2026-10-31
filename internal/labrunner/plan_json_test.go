package labrunner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/labrunner"
)

func TestReadJSONSizeLimit(t *testing.T) {
	const limit = 4 << 20
	for _, tc := range []struct {
		name   string
		prefix string
		fill   string
		suffix string
		size   int
	}{
		{"value below limit", `{"name":"`, "x", `"}`, limit - 1},
		{"value at limit", `{"name":"`, "x", `"}`, limit},
		{"value over limit", `{"name":"`, "x", `"}`, limit + 1},
		{"whitespace below limit", `{"name":"ok"}`, " ", "", limit - 1},
		{"whitespace at limit", `{"name":"ok"}`, " ", "", limit},
		{"whitespace over limit", `{"name":"ok"}`, " ", "", limit + 1},
		{"hidden trailing junk", `{"name":"ok"}`, " ", "x", limit + 1},
		{"hidden second value", `{"name":"ok"}`, " ", "{}", limit + 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.prefix + strings.Repeat(tc.fill, tc.size-len(tc.prefix)-len(tc.suffix)) + tc.suffix
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			value := struct {
				Name string `json:"name"`
			}{Name: "unchanged"}
			err := labrunner.ReadJSON(path, &value)
			if tc.size > limit {
				if err == nil || !strings.Contains(err.Error(), "JSON exceeds 4194304 bytes") {
					t.Fatalf("expected size rejection, got %v", err)
				}
				if value.Name != "unchanged" {
					t.Fatal("oversized input was decoded before rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "ok"
			if tc.fill == "x" {
				want = strings.Repeat(tc.fill, tc.size-len(tc.prefix)-len(tc.suffix))
			}
			if value.Name != want {
				t.Fatal("decoded value does not match input")
			}
		})
	}
}

func TestReadJSONStrictDecoding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"single value and whitespace", " \n{\"name\":\"ok\"}\t\r\n", ""},
		{"unknown field", `{"name":"ok","unknown":true}`, `unknown field "unknown"`},
		{"second value", `{"name":"ok"} {}`, "expected exactly one JSON value"},
		{"trailing junk", `{"name":"ok"} x`, "expected exactly one JSON value"},
		{"incomplete value", `{"name":`, "unexpected EOF"},
		{"empty input", "", "EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			var value struct {
				Name string `json:"name"`
			}
			err := labrunner.ReadJSON(path, &value)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %q, got %v", tc.wantErr, err)
				}
			} else if err != nil || value.Name != "ok" {
				t.Fatalf("valid input rejected or decoded incorrectly: %v", err)
			}
		})
	}
}
