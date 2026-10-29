package fuseadapter

import (
	"encoding/binary"
	"slices"
	"syscall"
	"testing"
)

func freeBSDProcessVector() []byte {
	b := make([]byte, 1088)
	for at, n := range map[int]uint32{0: 1088, 72: 1234, 168: 20001, 172: 20001, 176: 20001, 180: 20001, 184: 20001, 192: 20001, 196: 20002} {
		binary.LittleEndian.PutUint32(b[at:], n)
	}
	binary.LittleEndian.PutUint16(b[188:], 2)
	binary.LittleEndian.PutUint64(b[336:], 12345678)
	binary.LittleEndian.PutUint64(b[344:], 987654)
	return b
}
func TestFreeBSDParticipantABI(t *testing.T) {
	good := freeBSDProcessVector()
	start, err := decodeFreeBSDParticipant(good, 1234, 20001, 20001, []uint32{20002})
	if err != nil || start != [2]uint64{12345678, 987654} {
		t.Fatal(start, err)
	}
	for _, scenario := range []string{"short", "long", "size", "layout", "pid", "euid", "ruid", "svuid", "rgid", "svgid", "egid", "missing-group", "overflow-count", "overflow-flag", "capability", "unknown-flags", "group-mismatch", "start-zero", "start-microseconds"} {
		t.Run(scenario, func(t *testing.T) {
			b := freeBSDProcessVector()
			switch scenario {
			case "short":
				b = b[:1087]
			case "long":
				b = append(b, 0)
			case "missing-group":
				binary.LittleEndian.PutUint16(b[188:], 1)
			case "overflow-count":
				binary.LittleEndian.PutUint16(b[188:], 17)
			case "overflow-flag":
				binary.LittleEndian.PutUint32(b[588:], 0x80000000)
			case "capability":
				binary.LittleEndian.PutUint32(b[588:], 1)
			case "unknown-flags":
				binary.LittleEndian.PutUint32(b[588:], 2)
			case "start-zero":
				binary.LittleEndian.PutUint64(b[336:], 0)
			case "start-microseconds":
				binary.LittleEndian.PutUint64(b[344:], 1000000)
			default:
				at := map[string]int{"size": 0, "layout": 4, "pid": 72, "euid": 168, "ruid": 172, "svuid": 176, "rgid": 180, "svgid": 184, "egid": 192, "group-mismatch": 196}[scenario]
				binary.LittleEndian.PutUint32(b[at:], 999)
			}
			if _, err := decodeFreeBSDParticipant(b, 1234, 20001, 20001, []uint32{20002}); err == nil {
				t.Fatal("unsupported/mutated credentials accepted")
			}
		})
	}
	// No supplementary groups is complete only with the separate effective GID.
	b := freeBSDProcessVector()
	binary.LittleEndian.PutUint16(b[188:], 1)
	if _, err := decodeFreeBSDParticipant(b, 1234, 20001, 20001, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFreeBSDParticipantABISupport(t *testing.T) {
	for _, c := range []struct {
		release, arch string
		want          bool
	}{{"15.1-RELEASE", "amd64", true}, {"15.1-RELEASE-p12", "amd64", true}, {"15.0-RELEASE", "amd64", false}, {"15.1-STABLE", "amd64", false}, {"15.1-RELEASE-p", "amd64", false}, {"15.1-RELEASE-p1-extra", "amd64", false}, {"15.1-RELEASE", "arm64", false}} {
		if got := supportedFreeBSDParticipantABI(c.release, c.arch); got != c.want {
			t.Fatalf("%s/%s supported=%v", c.release, c.arch, got)
		}
	}
}

func TestFreeBSDParticipantCompatGroups(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  []int
		gid  uint32
		want []uint32
		bad  bool
	}{
		{name: "parent supplementary group", raw: []int{20001, 20002}, gid: 20001, want: []uint32{20002}},
		{name: "root empty supplementary", raw: []int{0}, gid: 0, want: []uint32{}},
		{name: "preserve supplementary primary", raw: []int{20001, 20002, 20001}, gid: 20001, want: []uint32{20001, 20002}},
		{name: "wrong leading egid", raw: []int{20002, 20001}, gid: 20001, bad: true},
		{name: "missing leading egid", raw: []int{}, gid: 20001, bad: true},
		{name: "negative leading egid", raw: []int{-1}, gid: 20001, bad: true},
		{name: "negative supplementary", raw: []int{20001, -1}, gid: 20001, bad: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			original := slices.Clone(c.raw)
			got, err := freeBSDParticipantGroups(c.raw, c.gid)
			if c.bad {
				if err == nil {
					t.Fatal("invalid COMPAT14 group prefix accepted")
				}
				return
			}
			if err != nil || !slices.Equal(got, c.want) {
				t.Fatal(got, err)
			}
			if !slices.Equal(c.raw, original) {
				t.Fatal("parent syscall result mutated")
			}
			if len(got) > 0 {
				got[0] = 999
				if !slices.Equal(c.raw, original) {
					t.Fatal("normalized groups alias syscall result")
				}
			}
		})
	}
}
func TestFreeBSDParticipantCompatCredentialPreservation(t *testing.T) {
	for _, c := range []syscall.Credential{{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}, {Uid: 0, Gid: 0, Groups: []uint32{}}, {Uid: 20001, Gid: 20001, Groups: []uint32{20001, 20002}}} {
		expected := slices.Clone(c.Groups)
		native := freeBSDParticipantCredential(c)
		if native.Uid != c.Uid || native.Gid != c.Gid || native.NoSetGroups != c.NoSetGroups || !slices.Equal(c.Groups, expected) {
			t.Fatal("exec constructor changed trusted input", native, c)
		}
		if len(native.Groups) != len(expected)+1 || native.Groups[0] != c.Gid || !slices.Equal(native.Groups[1:], expected) {
			t.Fatal("COMPAT14 setgroups would drop supplementary credentials", native)
		}
		// Model the actual compat getgroups/setgroups prefix and compare against
		// the kinfo_proc oracle used at admission, including the zero-group role.
		raw := make([]int, len(native.Groups))
		for i, g := range native.Groups {
			raw[i] = int(g)
		}
		got, err := freeBSDParticipantGroups(raw, c.Gid)
		if err != nil || !slices.Equal(got, expected) {
			t.Fatal("compat syscall round trip", got, expected, err)
		}
		vector := freeBSDProcessVector()
		for _, offset := range []int{168, 172, 176} {
			binary.LittleEndian.PutUint32(vector[offset:], c.Uid)
		}
		for _, offset := range []int{180, 184} {
			binary.LittleEndian.PutUint32(vector[offset:], c.Gid)
		}
		binary.LittleEndian.PutUint16(vector[188:], uint16(len(native.Groups)))
		for i, g := range native.Groups {
			binary.LittleEndian.PutUint32(vector[192+4*i:], g)
		}
		if _, err := decodeFreeBSDParticipant(vector, 1234, c.Uid, c.Gid, expected); err != nil {
			t.Fatal("compat-normalized constructor cannot enroll", err)
		}
		if len(expected) > 0 {
			native.Groups[1] = 999
			if !slices.Equal(c.Groups, expected) {
				t.Fatal("exec prefix aliases trusted expected groups")
			}
		}
	}
}
