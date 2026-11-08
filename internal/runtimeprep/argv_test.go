//go:build linux

package runtimeprep

import (
	"reflect"
	"testing"
)

func TestParseBhyveArgsFollowsGetopt(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []bhyveOption
		ok   bool
	}{
		{"separate value", []string{"-c", "4", "vm"}, []bhyveOption{{'c', "4"}}, true},
		{"attached value", []string{"-s0:7:0,virtio-9p", "vm"}, []bhyveOption{{'s', "0:7:0,virtio-9p"}}, true},
		{"grouped flags then value", []string{"-AHPs", "1,lpc", "vm"},
			[]bhyveOption{{'A', ""}, {'H', ""}, {'P', ""}, {'s', "1,lpc"}}, true},
		{"terminator", []string{"-A", "--", "-vm"}, []bhyveOption{{'A', ""}}, true},
		{"stops at first operand", []string{"vm", "-s", "1,lpc"}, nil, false},
		{"missing value", []string{"vm", "-c"}, nil, false},
		{"dangling option", []string{"-c"}, nil, false},
		{"unknown flag", []string{"-AZ", "vm"}, nil, false},
		{"no vm", []string{"-A"}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseBhyveArgs(c.args)
			if (err == nil) != c.ok || (c.ok && !reflect.DeepEqual(got, c.want)) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestParseSlotForms(t *testing.T) {
	cases := map[string]pciSlot{"7,virtio-9p": {0, 7, 0}, "7:1,virtio-9p": {0, 7, 1}, "1:7:2,virtio-9p": {1, 7, 2}}
	for value, want := range cases {
		got, emul, config, err := parseSlot(value)
		if err != nil || got != want || emul != "virtio-9p" || config != "" {
			t.Fatalf("%s: %v %s %q %v", value, got, emul, config, err)
		}
	}
	for _, bad := range []string{"7", "a:7,virtio-9p", "1:2:3:4,virtio-9p", "-1,virtio-9p"} {
		if _, _, _, err := parseSlot(bad); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}

// An attached "-s" or grouped option cannot smuggle a legacy 9p device past
// the device checks: getopt parsing normalises it first.
func TestAttachedLegacyNinePRefused(t *testing.T) {
	args := exporterArgs([]string{"-U", exampleVM, "vm"}, ninePDevice(7, "hfs0", "{{session:0}}"),
		[]string{"-As0:8:0,virtio-9p,hfs1=/tmp/x"})
	if _, err := parseExporterTemplate(args); err == nil {
		t.Fatal("attached legacy 9p device accepted")
	}
}
