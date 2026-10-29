package labrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"time"
)

// EtcdControllerPlacement binds bootstrap to f3's existing isolated bridge.
// This grants authority only over its exact private run controller directory.
type EtcdControllerPlacement struct {
	Target        string `json:"target"`
	Hostname      string `json:"hostname"`
	Interface     string `json:"interface"`
	SourceAddress string `json:"source_address"`
	Resource      string `json:"resource"`
}

// EtcdControllerFile is a controller-generated request binding for one private
// PKI file. It is generated under the approved plan, not separately approved.
type EtcdControllerFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// EtcdControllerEnrollment records observed f3 placement and exact staged inputs.
// It contains no credential bytes. Configured establishes infrastructure only.
type EtcdControllerEnrollment struct {
	RunID        string                  `json:"run_id"`
	PlanHash     string                  `json:"plan_hash"`
	Cluster      string                  `json:"cluster"`
	Placement    EtcdControllerPlacement `json:"placement"`
	Resource     Resource                `json:"resource"`
	HelperSHA256 string                  `json:"helper_sha256"`
	PKISHA256    string                  `json:"pki_sha256"`
	Device       uint64                  `json:"device"`
	Inode        uint64                  `json:"inode"`
	Configured   bool                    `json:"configured"`
}

type etcdControllerRuntime struct {
	OS, Arch, Hostname, Executable string
	Interfaces                     []etcdControllerInterface
}
type etcdControllerInterface struct {
	Name      string
	Up        bool
	Addresses []string
}

// PrepareEtcdBootstrapStep verifies workstation artifact sources before transfer
// and binds only controller client files to the exact f3 bootstrap request.
func PrepareEtcdBootstrapStep(p Plan, a Approval, directory string, now time.Time) (EtcdStepRequest, error) {
	req, err := prepareEtcdStep(p, a, "etcd-rbac", now)
	if err != nil {
		return EtcdStepRequest{}, err
	}
	if err := privateDirectory(directory); err != nil {
		return EtcdStepRequest{}, err
	}
	var binding struct {
		PlanHash string `json:"plan_hash"`
		Cluster  string `json:"cluster"`
	}
	if err := readOwnedJSON(filepath.Join(directory, "plan.json"), &binding); err != nil {
		return EtcdStepRequest{}, err
	}
	if binding.PlanHash != req.PlanHash || binding.Cluster != p.PrivateEtcd.Cluster {
		return EtcdStepRequest{}, errors.New("controller PKI belongs to another plan")
	}
	names, err := etcdControllerPKINames(p)
	if err != nil {
		return EtcdStepRequest{}, err
	}
	for _, name := range names {
		file := filepath.Join(directory, name)
		if _, err := privateEtcdFile(file); err != nil {
			return EtcdStepRequest{}, err
		}
		hash, size, err := HashEtcdInput(file)
		if err != nil {
			return EtcdStepRequest{}, err
		}
		req.ControllerPKI = append(req.ControllerPKI, EtcdControllerFile{Name: name, SHA256: hash, Bytes: size})
	}
	if err := validateEtcdControllerPins(p, req.ControllerPKI); err != nil {
		return EtcdStepRequest{}, err
	}
	return req, nil
}

func validateEtcdControllerPlacement(p Plan) error {
	c := p.PrivateEtcd.Controller
	if c.Target != "f3" || (c.Hostname != "f3" && c.Hostname != "f3.lan.buetow.org") || c.Interface != "vm-hfslab" || c.SourceAddress != "172.31.253.1" || !validID(c.Resource) {
		return errors.New("private etcd controller must explicitly use f3 and vm-hfslab 172.31.253.1")
	}
	for _, r := range p.Resources {
		if isEtcdControllerResource(p, r) {
			return nil
		}
	}
	return errors.New("exact bounded f3 private controller resource required")
}
func isEtcdControllerResource(p Plan, r Resource) bool {
	if p.PrivateEtcd == nil {
		return false
	}
	c := p.PrivateEtcd.Controller
	return c.Target == "f3" && r.Target == "f3" && r.Name == c.Resource && r.Kind == "directory" && r.Location == etcdControllerRoot(p) && r.MaxBytes >= p.PrivateEtcd.HelperBytes+(2<<20) && r.MaxBytes <= 128<<20
}
func etcdControllerRoot(p Plan) string { return etcdRoot(p) + "/etcd-controller" }
func etcdControllerPKINames(p Plan) ([]string, error) {
	roles, err := EtcdRoles(p)
	if err != nil {
		return nil, err
	}
	names := []string{"client-ca.pem", "plan.json", "root.pem", "root-key.pem"}
	for _, role := range roles {
		names = append(names, role.User+".pem", role.User+"-key.pem")
	}
	slices.Sort(names)
	return names, nil
}
func validateEtcdControllerPins(p Plan, pins []EtcdControllerFile) error {
	names, err := etcdControllerPKINames(p)
	if err != nil {
		return err
	}
	if len(pins) != len(names) {
		return errors.New("exact controller-only PKI file set required")
	}
	var total int64
	for i, pin := range pins {
		if pin.Name != names[i] || !sha256Text(pin.SHA256) || pin.Bytes <= 0 || pin.Bytes > 65536 {
			return errors.New("foreign, unordered, unbounded or missing controller PKI pin")
		}
		total += pin.Bytes
	}
	if total > 1<<20 {
		return errors.New("controller PKI byte budget exceeded")
	}
	return nil
}
func etcdControllerPKIHash(pins []EtcdControllerFile) string {
	raw, _ := json.Marshal(pins)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func inspectEtcdControllerRuntime() (etcdControllerRuntime, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return etcdControllerRuntime{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return etcdControllerRuntime{}, err
	}
	r := etcdControllerRuntime{OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: hostname, Executable: executable}
	interfaces, err := net.Interfaces()
	if err != nil {
		return r, err
	}
	for _, iface := range interfaces {
		item := etcdControllerInterface{Name: iface.Name, Up: iface.Flags&net.FlagUp != 0}
		addresses, err := iface.Addrs()
		if err != nil {
			return r, err
		}
		for _, address := range addresses {
			item.Addresses = append(item.Addresses, address.String())
		}
		r.Interfaces = append(r.Interfaces, item)
	}
	return r, nil
}
func checkEtcdControllerRoute(c EtcdControllerPlacement, r etcdControllerRuntime) error {
	if r.OS != "freebsd" || r.Arch != "amd64" || r.Hostname != c.Hostname {
		return errors.New("bootstrap requires the exact approved native f3 controller host")
	}
	for _, iface := range r.Interfaces {
		if iface.Name == c.Interface && iface.Up && slices.Contains(iface.Addresses, c.SourceAddress+"/24") {
			return nil
		}
	}
	return errors.New("approved f3 isolated bridge source route is missing; no workstation or ambient-route fallback")
}

func enrollEtcdController(req EtcdStepRequest, directory string, runtime etcdControllerRuntime) (EtcdControllerEnrollment, error) {
	p := req.Plan
	var result EtcdControllerEnrollment
	if err := validateEtcdStepRequest(req, time.Now()); err != nil {
		return result, err
	}
	if req.Operation != "rbac" || req.Target != "f3" {
		return result, errors.New("exact f3 bootstrap request required")
	}
	if err := checkEtcdControllerRoute(p.PrivateEtcd.Controller, runtime); err != nil {
		return result, err
	}
	root := etcdControllerRoot(p)
	if directory != root+"/pki" || runtime.Executable != root+"/hfs-lab" {
		return result, errors.New("controller helper/PKI paths differ from enrolled f3 resource")
	}
	if err := privateDirectory(root); err != nil {
		return result, err
	}
	if err := verifyEtcdInput(runtime.Executable, etcdArtifact(p, p.PrivateEtcd.Helper).SHA256, p.PrivateEtcd.HelperBytes); err != nil {
		return result, err
	}
	if err := privateDirectory(directory); err != nil {
		return result, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return result, err
	}
	if len(entries) != len(req.ControllerPKI) {
		return result, errors.New("staged controller PKI contains foreign/missing files")
	}
	for _, pin := range req.ControllerPKI {
		if err := verifyEtcdInput(filepath.Join(directory, pin.Name), pin.SHA256, pin.Bytes); err != nil {
			return result, err
		}
	}
	var resource Resource
	for _, candidate := range p.Resources {
		if isEtcdControllerResource(p, candidate) {
			resource = candidate
		}
	}
	if _, err := boundedEtcdTree(root, resource.MaxBytes); err != nil {
		return result, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return result, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return result, errors.New("controller directory identity unavailable")
	}
	result = EtcdControllerEnrollment{RunID: p.RunID, PlanHash: req.PlanHash, Cluster: p.PrivateEtcd.Cluster, Placement: p.PrivateEtcd.Controller, Resource: resource, HelperSHA256: etcdArtifact(p, p.PrivateEtcd.Helper).SHA256, PKISHA256: etcdControllerPKIHash(req.ControllerPKI), Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}
	if err := WriteJSON(root+"/controller-enrollment.json", result); err != nil {
		return result, err
	}
	return result, nil
}
func validateEtcdControllerReceipt(req EtcdStepRequest, e *EtcdControllerEnrollment) error {
	var resource Resource
	for _, candidate := range req.Plan.Resources {
		if isEtcdControllerResource(req.Plan, candidate) {
			resource = candidate
		}
	}
	if e == nil || !e.Configured || e.RunID != req.Plan.RunID || e.PlanHash != req.PlanHash || e.Cluster != req.Plan.PrivateEtcd.Cluster || e.Placement != req.Plan.PrivateEtcd.Controller || e.Resource != resource || e.HelperSHA256 != etcdArtifact(req.Plan, req.Plan.PrivateEtcd.Helper).SHA256 || e.PKISHA256 != etcdControllerPKIHash(req.ControllerPKI) || e.Inode == 0 {
		return errors.New("missing or foreign f3 controller enrollment receipt")
	}
	return nil
}

func privateEtcdControllerDialer(p Plan) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if port != "32379" || !slices.Contains([]string{"172.31.253.11", "172.31.253.12", "172.31.253.13"}, host) {
			return nil, errors.New("controller dial target is not an approved private etcd endpoint")
		}
		dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(p.PrivateEtcd.Controller.SourceAddress)}, Timeout: 2 * time.Second}
		return dialer.DialContext(ctx, "tcp", address)
	}
}
