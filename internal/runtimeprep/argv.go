//go:build linux || freebsd

package runtimeprep

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// The FreeBSD 15.1 amd64 bhyve getopt string is
// "aehuwxACDHIPSWYk:f:o:p:G:c:s:m:n:l:K:U:" (bhyverun_machdep.c). Parsing the
// argv exactly like getopt keeps the device checks below from being evaded
// by grouped flags or attached option values.
const (
	bhyveValueOptions = "kfopGcsmnlKU"
	bhyveFlagOptions  = "aehuwxACDHIPSWY"
	// The runtime pins at most 64 exporter arguments and 64 KiB in total.
	maxExporterArgs  = 64
	maxExporterBytes = 65536
)

var (
	sessionPlaceholder = regexp.MustCompile(`^\{\{session:([0-9]+)\}\}$`)
	pciNode            = regexp.MustCompile(`^pci\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.([^=]+)=(.*)$`)
	shareTag           = regexp.MustCompile(`^[A-Za-z0-9._-]{1,31}$`)
)

type bhyveOption struct {
	name  byte
	value string
}

type pciSlot struct{ bus, slot, function int }

// controlledDevice is one virtio-9p device bound to one original session.
type controlledDevice struct {
	slot    pciSlot
	session int
	tag     string
}

// exporterLayout is what the generator learns from the argv template.
type exporterLayout struct {
	devices []controlledDevice // indexed by session number
	vm      string             // bhyve -U UUID, used as the scope VM identity
}

// parseBhyveArgs applies BSD getopt semantics (no permutation): options end at
// "--" or the first non-option, and exactly one VM name must follow.
func parseBhyveArgs(args []string) ([]bhyveOption, error) {
	var options []bhyveOption
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if len(a) < 2 || a[0] != '-' {
			break
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte(bhyveFlagOptions, c) >= 0 {
				options = append(options, bhyveOption{name: c})
				continue
			}
			if strings.IndexByte(bhyveValueOptions, c) < 0 {
				return nil, fmt.Errorf("unknown bhyve option -%c", c)
			}
			value := a[j+1:]
			if value == "" {
				if i++; i >= len(args) {
					return nil, fmt.Errorf("bhyve option -%c lacks its value", c)
				}
				value = args[i]
			}
			options = append(options, bhyveOption{c, value})
			break
		}
	}
	if len(args)-i != 1 {
		return nil, fmt.Errorf("bhyve argv must end with exactly one VM name")
	}
	return options, nil
}

// parseExporterTemplate validates the argv template and extracts the
// controlled devices and the VM UUID. bhyve's virtio-9p legacy option parser
// treats every "name=value" in "-s B:S:F,virtio-9p,..." as a share name, so
// "sharename=TAG,hfs-controller=UUID" there is refused by bhyve itself ("more
// than one share name"). The only working form is "-s B:S:F,virtio-9p" followed
// by "-o pci.B.S.F.sharename=TAG" and "-o pci.B.S.F.hfs-controller=UUID".
func parseExporterTemplate(args []string) (exporterLayout, error) {
	var layout exporterLayout
	if len(args) > maxExporterArgs {
		return layout, fmt.Errorf("exporter argv exceeds %d arguments", maxExporterArgs)
	}
	options, err := parseBhyveArgs(args)
	if err != nil {
		return layout, err
	}
	nodes, err := collectNinePDevices(options)
	if err != nil {
		return layout, err
	}
	if layout.devices, err = controlledDevices(nodes); err != nil {
		return layout, err
	}
	if layout.vm, err = vmUUID(options); err != nil {
		return layout, err
	}
	return layout, checkPlaceholders(args, len(layout.devices))
}

// collectNinePDevices returns the -o configuration nodes of every virtio-9p
// slot. Anything that could create or retarget a device outside the checked
// -s list is refused: a config file (-k), a PCI node before (or without) its
// -s (bhyve's later -s would fail "already occupied"), a "device" override,
// and a repeated -s slot.
func collectNinePDevices(options []bhyveOption) (map[pciSlot]map[string][]string, error) {
	emulation := map[pciSlot]string{}
	nodes := map[pciSlot]map[string][]string{}
	for _, o := range options {
		switch o.name {
		case 'k':
			return nil, fmt.Errorf("-k configuration files are refused; every device must be explicit argv")
		case 's':
			if err := addSlot(o.value, emulation, nodes); err != nil {
				return nil, err
			}
		case 'o':
			m := pciNode.FindStringSubmatch(o.value)
			if m == nil {
				continue // not a PCI device node; other bhyve configuration passes through
			}
			slot := pciSlot{atoi(m[1]), atoi(m[2]), atoi(m[3])}
			if _, ok := emulation[slot]; !ok || m[4] == "device" {
				return nil, fmt.Errorf("-o %s: PCI node before its -s or device override", o.value)
			}
			if nodes[slot] != nil {
				nodes[slot][m[4]] = append(nodes[slot][m[4]], m[5])
			}
		}
	}
	return nodes, nil
}

func addSlot(value string, emulation map[pciSlot]string, nodes map[pciSlot]map[string][]string) error {
	slot, emul, config, err := parseSlot(value)
	if err != nil {
		return err
	}
	if _, duplicate := emulation[slot]; duplicate {
		return fmt.Errorf("-s %s: PCI slot already used", value)
	}
	if emul == "virtio-9p" && config != "" {
		return fmt.Errorf("-s %s: virtio-9p must carry no inline options; use -o pci.B.S.F.sharename/hfs-controller (stock or legacy 9p exports are refused)", value)
	}
	emulation[slot] = emul
	if emul == "virtio-9p" {
		nodes[slot] = map[string][]string{}
	}
	return nil
}

// controlledDevices requires every virtio-9p slot to be exactly one dedicated
// controlled export: one sharename, one session placeholder, no path/ro.
func controlledDevices(nodes map[pciSlot]map[string][]string) ([]controlledDevice, error) {
	devices := make([]controlledDevice, len(nodes))
	seen := make([]bool, len(nodes))
	tags := map[string]bool{}
	for slot, keys := range nodes {
		for key := range keys {
			if key != "sharename" && key != "hfs-controller" {
				return nil, fmt.Errorf("controlled 9p slot %v: unexpected node %q (path/ro and stock exports refused)", slot, key)
			}
		}
		if len(keys["sharename"]) != 1 || len(keys["hfs-controller"]) != 1 {
			return nil, fmt.Errorf("9p slot %v needs exactly one sharename and one hfs-controller", slot)
		}
		tag, controller := keys["sharename"][0], keys["hfs-controller"][0]
		m := sessionPlaceholder.FindStringSubmatch(controller)
		if !shareTag.MatchString(tag) || tags[tag] || m == nil {
			return nil, fmt.Errorf("9p slot %v: distinct tag and {{session:N}} controller required", slot)
		}
		n := atoi(m[1])
		if n >= len(devices) || seen[n] {
			return nil, fmt.Errorf("9p slot %v: session %d out of range or duplicated", slot, n)
		}
		seen[n], tags[tag] = true, true
		devices[n] = controlledDevice{slot: slot, session: n, tag: tag}
	}
	if len(devices) != sessionCount {
		return nil, fmt.Errorf("exporter argv defines %d controlled 9p devices; the signer requires exactly %d", len(devices), sessionCount)
	}
	return devices, nil
}

func vmUUID(options []bhyveOption) (string, error) {
	var found []string
	for _, o := range options {
		if o.name == 'U' {
			found = append(found, o.value)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("exporter argv needs exactly one -U VM UUID")
	}
	if _, err := fsapi.ParseUUID(found[0]); err != nil {
		return "", fmt.Errorf("-U: %w", err)
	}
	return found[0], nil
}

// checkPlaceholders refuses session placeholders outside the hfs-controller
// nodes (already counted there) and any other unresolved template text.
func checkPlaceholders(args []string, sessions int) error {
	count := 0
	for _, a := range args {
		count += strings.Count(a, "{{")
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("exporter argument contains NUL")
		}
	}
	if count != sessions {
		return fmt.Errorf("exporter argv has template text outside the controlled devices")
	}
	return nil
}

// resolveArgs substitutes each session UUID and re-checks the runtime's argv bounds.
func resolveArgs(args []string, devices []controlledDevice, sessions []string) ([]string, error) {
	resolved := make([]string, len(args))
	total := 0
	for i, a := range args {
		for _, d := range devices {
			a = strings.ReplaceAll(a, fmt.Sprintf("{{session:%d}}", d.session), sessions[d.session])
		}
		resolved[i] = a
		total += len(a)
	}
	if total > maxExporterBytes {
		return nil, fmt.Errorf("resolved exporter argv exceeds %d bytes", maxExporterBytes)
	}
	return resolved, nil
}

// parseSlot follows pci_parse_slot: "B:S:F", "S:F" or "S", then emulation and
// the optional configuration remainder.
func parseSlot(value string) (pciSlot, string, string, error) {
	location, rest, ok := strings.Cut(value, ",")
	if !ok {
		return pciSlot{}, "", "", fmt.Errorf("-s %s: missing emulation", value)
	}
	emul, config, _ := strings.Cut(rest, ",")
	parts := strings.Split(location, ":")
	numbers := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || len(parts) > 3 {
			return pciSlot{}, "", "", fmt.Errorf("-s %s: invalid PCI location", value)
		}
		numbers[i] = n
	}
	switch len(numbers) {
	case 1:
		return pciSlot{0, numbers[0], 0}, emul, config, nil
	case 2:
		return pciSlot{0, numbers[0], numbers[1]}, emul, config, nil
	}
	return pciSlot{numbers[0], numbers[1], numbers[2]}, emul, config, nil
}

func atoi(text string) int {
	n, _ := strconv.Atoi(text) // callers pass regexp-checked decimal digits
	return n
}
