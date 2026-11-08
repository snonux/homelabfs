//go:build linux || freebsd

package runtimeprep

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime/debug"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/retained9plab"
)

// RequestTemplate holds every enrollment/bind request field known before the
// runtime starts. enroll-request and bind-request add the actual proposal,
// measured boot/provider nonce, guest device names, observer and controller.
type RequestTemplate struct {
	Version, RunID, Volume, OperatorKey string
	Keys                                []retained9plab.DomainKeys
	HostPublicKeys, GuestPublicKeys     []string
	Mounts                              []controlledruntime.GuestArtifactMount
	Budget                              controlledruntime.GuestArtifactBudget
}

// VolumeInitPlan is the input of tools/lab/runtime-volume-init.py (G3). It
// uses snake_case like the other Python lab plans.
type VolumeInitPlan struct {
	Version      string `json:"version"`
	RunID        string `json:"run_id"`
	Helper       string `json:"helper"`
	HelperSHA256 string `json:"helper_sha256"`
	Config       string `json:"config"`
	ControlDir   string `json:"control_dir"`
	VolumeDir    string `json:"volume_dir"`
	Mountpoint   string `json:"mountpoint"`
	VolumeID     string `json:"volume_id"`
}

// Record is the reviewable summary of one generation. It contains public
// keys and hashes only; private key bytes live solely in the key files and
// (host keys) in the runtime config.
type Record struct {
	Version, RunID, Created, Source string
	VM, Volume, ExporterArgsSHA256  string
	OperatorPublicKey, OperatorKey  string
	OperatorPrivateKeyRead          bool
	Validation                      string
	Sessions                        []SessionRecord
	Files                           map[string]string
}

// SessionRecord describes one original domain of the run.
type SessionRecord struct {
	Index                                 int
	Session, DeviceTag, PCISlot           string
	HostPublicKey, GuestPublicKey         string
	HostKeyFile, GuestKeyFile, GuestMount string
}

// bundle serialises config, keys, plan, template and the generation record.
func (g *generator) bundle(config controlledruntime.RuntimeConfig) (Bundle, error) {
	files := map[string][]byte{}
	var err error
	if files[ConfigFile], err = marshalFile(config); err != nil {
		return Bundle{}, err
	}
	if files[VolumePlanFile], err = marshalFile(g.volumePlan()); err != nil {
		return Bundle{}, err
	}
	if files[TemplateFile], err = marshalFile(g.template()); err != nil {
		return Bundle{}, err
	}
	for i, keys := range g.keys {
		// Lowercase hex seeds without newline: the signer's accepted key form.
		files[keyFile("host", i)] = []byte(hex.EncodeToString(keys.host.Seed()))
		files[keyFile("guest", i)] = []byte(hex.EncodeToString(keys.guest.Seed()))
	}
	record := g.record(files)
	if files[RecordFile], err = marshalFile(record); err != nil {
		return Bundle{}, err
	}
	return Bundle{Files: files, Record: record}, nil
}

func (g *generator) volumePlan() VolumeInitPlan {
	v := g.spec.Volume
	return VolumeInitPlan{Version: VolumeInitVersion, RunID: g.spec.RunID,
		Helper: g.spec.InitHelper.Path, HelperSHA256: g.spec.InitHelper.SHA256,
		Config: v.Config, ControlDir: v.ControlDirectory, VolumeDir: v.VolumeDirectory,
		Mountpoint: v.Mountpoint, VolumeID: g.ids.volume}
}

// template records the signer inputs known now. Mount DeviceName and
// ViewControlNonce stay empty: the first is the guest's actual virtio device
// name, the second comes from the runtime's actual proposal ticket families.
func (g *generator) template() RequestTemplate {
	t := RequestTemplate{Version: TemplateVersion, RunID: g.spec.RunID, Volume: g.ids.volume,
		OperatorKey: g.spec.OperatorKey, Budget: g.spec.Budget.Guest}
	subject := hex.EncodeToString([]byte("homelabfs-controlled-" + g.spec.RunID))
	idmap := sha256Hex([]byte(idmapCanonical))
	for i, session := range g.ids.sessions {
		t.Keys = append(t.Keys, retained9plab.DomainKeys{Session: session,
			Host: filepath.Join(g.output, keyFile("host", i)), Guest: filepath.Join(g.output, keyFile("guest", i))})
		t.HostPublicKeys = append(t.HostPublicKeys, hex.EncodeToString(g.keys[i].host.Public().(ed25519.PublicKey)))
		t.GuestPublicKeys = append(t.GuestPublicKeys, hex.EncodeToString(g.keys[i].guest.Public().(ed25519.PublicKey)))
		t.Mounts = append(t.Mounts, controlledruntime.GuestArtifactMount{DomainIndex: i, Session: session,
			DeviceTag: g.layout.devices[i].tag, UserNamespace: g.ids.userNamespace, IDMapHash: idmap,
			Realm: g.ids.realm, Subject: subject, Anchor: g.ids.anchors[i], Nonce: g.ids.nonces[i]})
	}
	return t
}

func (g *generator) record(files map[string][]byte) Record {
	r := Record{Version: RecordVersion, RunID: g.spec.RunID, Created: utcNow(), Source: buildSource(),
		VM: g.layout.vm, Volume: g.ids.volume, OperatorPublicKey: g.spec.OperatorPublicKey,
		OperatorKey: g.spec.OperatorKey, OperatorPrivateKeyRead: false,
		Validation: "controlledruntime validateRuntimeConfig passed via controlledruntime.Run on Linux with sandbox VolumeConfig/OwnerDirectory/EnrollmentFile; real paths checked by the generator",
		Files:      map[string]string{}}
	args, _ := marshalFile(g.args)
	r.ExporterArgsSHA256 = sha256Hex(args)
	for name, raw := range files {
		r.Files[name] = sha256Hex(raw)
	}
	for i, d := range g.layout.devices {
		r.Sessions = append(r.Sessions, SessionRecord{Index: i, Session: g.ids.sessions[i], DeviceTag: d.tag,
			PCISlot:        fmt.Sprintf("%d:%d:%d", d.slot.bus, d.slot.slot, d.slot.function),
			HostPublicKey:  hex.EncodeToString(g.keys[i].host.Public().(ed25519.PublicKey)),
			GuestPublicKey: hex.EncodeToString(g.keys[i].guest.Public().(ed25519.PublicKey)),
			HostKeyFile:    keyFile("host", i), GuestKeyFile: keyFile("guest", i), GuestMount: g.ids.guestMounts[i]})
	}
	return r
}

// buildSource reports the generator's own VCS revision when the binary was
// built with VCS stamping, so a reviewer can compare it with the candidate.
func buildSource() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return "unknown (built without VCS stamping)"
}
