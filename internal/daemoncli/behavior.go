package daemoncli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/snonux/homelabfs/internal/behavior"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"golang.org/x/sys/unix"
)

type behaviorOptions struct {
	layout                                                       behaviorLayout
	mountpoint, binary, digest, profile, role, jsonPath, xmlPath string
	initialize                                                   bool
}

type behaviorReady struct {
	readyMessage
	Role           string   `json:"role"`
	DaemonUID      int      `json:"daemon_uid"`
	ClientUID      uint32   `json:"client_uid"`
	ClientGID      uint32   `json:"client_gid"`
	ClientGroups   []uint32 `json:"client_groups"`
	DataRoot       string   `json:"data_root"`
	JournalRoot    string   `json:"journal_root"`
	ControlRoot    string   `json:"control_root"`
	BehaviorSHA256 string   `json:"behavior_sha256"`
}

func runBehaviorCommand(ctx context.Context, args []string, output, stderr io.Writer) (retErr error) {
	f := flag.NewFlagSet("behavior", flag.ContinueOnError)
	f.SetOutput(stderr)
	var o behaviorOptions
	approved := f.Bool("disposable-validation", false, "explicit disposable controlled behavior fixture; does not authorize a test run")
	f.StringVar(&o.layout.DataRoot, "data-root", "", "existing empty private disposable data root")
	f.StringVar(&o.layout.JournalRoot, "journal-root", "", "existing empty private disposable journal root")
	f.StringVar(&o.layout.ControlRoot, "control-root", "", "existing empty private disposable control root")
	f.StringVar(&o.mountpoint, "mountpoint", "", "existing empty private mountpoint")
	f.StringVar(&o.binary, "behavior-binary", "", "trusted hfs-behavior executable, fixed throughout this run")
	f.StringVar(&o.digest, "behavior-sha256", "", "exact SHA256 of trusted helper binary")
	f.StringVar(&o.profile, "profile", "", "hfs-direct or hfs-buffered (separate invocations)")
	f.StringVar(&o.role, "role", "", "root or unprivileged fixed-credential helpers; daemon must run as root")
	f.StringVar(&o.jsonPath, "json", "", "new JSON report outside all fixture roots")
	f.StringVar(&o.xmlPath, "junit", "", "new JUnit report outside all fixture roots")
	f.BoolVar(&o.initialize, "initialize", false, "exclusively initialize three empty roots; omit to replay the exact marked fixture")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !*approved || f.NArg() != 0 {
		return fmt.Errorf("behavior requires --disposable-validation and explicit roots, role, profile, pinned helper and reports; see docs/spec/Configuration.md")
	}
	if err := o.validate(); err != nil {
		return err
	}
	// Open evidence exclusively before persistent initialization; failed results
	// never overwrite earlier evidence or trigger automatic resource deletion.
	jf, err := os.OpenFile(o.jsonPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, jf.Close()) }()
	xf, err := os.OpenFile(o.xmlPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, xf.Close()) }()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var report behavior.Report
	defer func() {
		if report.Profile == "" {
			report.Profile = o.profile
			report.Target = o.mountpoint
			report.Started = time.Now().UTC()
			report.Scope = "disposable fixture setup failed before behavior execution"
		}
		if retErr != nil {
			report.Results = append(report.Results, behavior.Result{Name: "fixture-lifecycle", Family: "infrastructure", Status: "fail", Detail: retErr.Error()})
		}
		if report.Fixture == nil {
			report.Fixture = map[string]string{}
		}
		for k, v := range map[string]string{"role": o.role, "daemon_uid": fmt.Sprint(os.Geteuid()), "client_uid": fmt.Sprint(roleCredentials(o.role).Uid), "client_gid": fmt.Sprint(roleCredentials(o.role).Gid), "client_groups": fmt.Sprint(roleCredentials(o.role).Groups), "data_root": o.layout.DataRoot, "journal_root": o.layout.JournalRoot, "control_root": o.layout.ControlRoot, "behavior_sha256": o.digest} {
			report.Fixture[k] = v
		}
		retErr = errors.Join(retErr, report.JSON(jf), report.JUnit(xf), jf.Sync(), xf.Sync())
	}()
	c, err := prepareBehavior(ctx, o.layout, o.mountpoint, o.initialize)
	if err != nil {
		return err
	}
	report, retErr = serveBehavior(ctx, c, o, output, slog.New(slog.NewJSONHandler(stderr, nil)))
	return retErr
}
func (o behaviorOptions) validate() error {
	if err := validateBehaviorRole(o.role, os.Geteuid()); err != nil {
		return err
	}
	return o.validateInputs()
}
func validateBehaviorRole(role string, daemonUID int) error {
	if daemonUID != 0 || (role != "root" && role != "unprivileged") {
		return fmt.Errorf("behavior requires root-owned daemon and explicit root or unprivileged helper role")
	}
	return nil
}
func (o behaviorOptions) validateInputs() error {
	if o.profile != "hfs-direct" && o.profile != "hfs-buffered" {
		return fmt.Errorf("explicit hfs-direct or hfs-buffered profile required")
	}

	if err := o.layout.validate(o.mountpoint, o.initialize); err != nil {
		return err
	}
	for _, out := range []string{o.jsonPath, o.xmlPath} {
		if err := checkPath(filepath.Dir(out), false); err != nil {
			return err
		}
		if !filepath.IsAbs(out) || filepath.Clean(out) != out {
			return fmt.Errorf("absolute clean evidence paths required")
		}
		for _, root := range append(o.layout.roots(), o.mountpoint) {
			if containsPath(root, out) {
				return fmt.Errorf("evidence must be outside fixture roots and mount")
			}
		}
	}
	return verifyBehaviorBinary(o.binary, o.digest)
}
func verifyBehaviorBinary(name, digest string) error {
	expected, err := hex.DecodeString(digest)
	if err != nil || len(expected) != sha256.Size || hex.EncodeToString(expected) != digest {
		return fmt.Errorf("canonical helper SHA256 required")
	}
	if err := checkPath(filepath.Dir(name), false); err != nil {
		return err
	}
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return fmt.Errorf("helper path must be absolute and clean")
	}
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { _ = file.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || st.Mode&0111 == 0 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Size > 128<<20 {
		return fmt.Errorf("helper must be bounded singly linked executable owned by fixture UID, without group/other write")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, (128<<20)+1)); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("helper SHA256 mismatch")
	}
	return nil
}

func serveBehavior(ctx context.Context, c Config, o behaviorOptions, output io.Writer, logger *slog.Logger) (report behavior.Report, retErr error) {
	participants, err := fuseadapter.NewParticipants()
	if err != nil {
		return report, err
	}
	defer func() { retErr = errors.Join(retErr, participants.Close()) }()
	local, err := openLocal(ctx, c)
	if err != nil {
		return report, err
	}
	defer func() {
		report.Fixture = map[string]string{"source": local.paths[0], "reservations": local.paths[1], "workspace_data": local.paths[2], "workspace_journal": local.paths[3], "workspace_progress": local.paths[4]}
	}()
	lifetime := &mountLifetime{local: local}
	defer func() { retErr = errors.Join(retErr, lifetime.close()) }()
	lifetime.admin, err = startAdmin(ctx, c, local)
	if err != nil {
		return report, err
	}
	waiting, cancelled := make(chan struct{}, 8), make(chan struct{}, 8)
	observer := func(op fsapi.Operation) {
		switch op {
		case fsapi.WaitLock:
			select {
			case waiting <- struct{}{}:
			default:
			}
		case fsapi.CancelLock:
			select {
			case cancelled <- struct{}{}:
			default:
			}
		}
	}
	buffered := o.profile == "hfs-buffered"
	multiuser := buffered && o.role == "root"
	unprivileged := o.role == "unprivileged"
	a, err := fuseadapter.New(fuseadapter.Config{Service: local.service, Validation: local.service.FrontendInfo().Validation, Credentials: participants, BufferedValidation: buffered, MultiuserValidation: multiuser, UnprivilegedValidation: unprivileged, ValidationLockEvent: observer})
	if err != nil {
		return report, err
	}
	if !buffered && !unprivileged {
		if err := participants.BindMount(a.Identity()); err != nil {
			return report, err
		}
	}
	spawn := func(ctx context.Context, in io.Reader, out io.Writer, cred *syscall.Credential) (behavior.Process, error) {
		// Administrative exclusivity is required for all roots and the helper path.
		// Recheck the exact artifact before each prepublication helper enrollment.
		if err := verifyBehaviorBinary(o.binary, o.digest); err != nil {
			return nil, err
		}
		return participants.Start(ctx, fuseadapter.ParticipantSpec{Executable: o.binary, Args: []string{"-helper"}, Env: os.Environ(), Credentials: cred, Stdin: in, Stdout: out, Stderr: os.Stderr})
	}
	bc := behavior.Config{Path: c.Mountpoint, Profile: o.profile, Timeout: 8 * time.Second, RootSkip: "root-only synthetic multiuser coverage requires root hfs-buffered companion"}
	caps := a.Capabilities()
	bc.Capabilities = map[string]bool{"direct_io": caps.DirectIO, "mmap": caps.Mmap, "default_permissions": caps.DefaultPermissions, "local_locks": caps.LocalLocks, "file_sync": caps.FileSync, "directory_sync": caps.DirectorySync, "normal_publication": caps.NormalPublication, "arbitrary_credentials": caps.ArbitraryCredentials, "writeback": caps.Writeback}
	var mount *fuseadapter.Mount
	unmountAttempted := false
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var stopErr error
		for _, child := range append(bc.Clients, bc.Users...) {
			stopErr = errors.Join(stopErr, child.Stop(stopCtx))
		}
		retErr = errors.Join(retErr, stopErr)
		if stopErr != nil {
			local.service.BlockAdmission("behavior-helper-stop-failed")
			lifetime.retained = true
			return
		}
		if mount != nil && !unmountAttempted {
			if err := lifetime.unmount(func() error { return boundedBehaviorUnmount(context.Background(), mount.Unmount) }); err != nil {
				logger.Error("normal behavior unmount failed; preserve resources", "error", err)
				retErr = errors.Join(retErr, err)
			}
		}
	}()
	for range 3 {
		cred := roleCredentials(o.role)
		child, err := behavior.Start(ctx, spawn, cred)
		if err != nil {
			return report, err
		}
		bc.Clients = append(bc.Clients, child)
		if err := readyBehaviorHelper(ctx, child); err != nil {
			return report, err
		}
	}
	if multiuser {
		for _, cred := range []*syscall.Credential{{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}, {Uid: 20003, Gid: 20003, Groups: []uint32{}}} {
			child, err := behavior.Start(ctx, spawn, cred)
			if err != nil {
				return report, err
			}
			bc.Users = append(bc.Users, child)
			if err := readyBehaviorHelper(ctx, child); err != nil {
				return report, err
			}
		}
	}
	if multiuser || unprivileged {
		// Logical root search permission is a fixture mutation, never backing chmod.
		info := local.service.FrontendInfo()
		mode := uint16(0755)
		if unprivileged {
			mode = 0777
		}
		request := fsapi.NewRequest(fsapi.Cred{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()), PID: uint32(os.Getpid())})
		if err := local.service.Dispatch(ctx, request, fsapi.Command{Operation: fsapi.Setattr, Mount: info.Volume, Generation: info.Generation, Object: info.Root, Attributes: fsapi.Attributes{Mode: &mode}}); err != nil {
			return report, err
		}
		result, err := request.Completion().Wait(ctx)
		if err != nil {
			return report, err
		}
		if result.Code != fsapi.OK {
			return report, result.Code
		}
	}
	wait := func(ch <-chan struct{}) func(context.Context) error {
		return func(ctx context.Context) error {
			select {
			case <-ch:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	bc.LockBlocked, bc.LockCancelled = wait(waiting), wait(cancelled)
	lifetime.retained = true
	mount, err = a.Mount(c.Mountpoint)
	if err != nil {
		local.service.BlockAdmission("behavior-native-mount-uncertain")
		return report, err
	}
	if err := fuseadapter.CheckMount(c.Mountpoint, a.Identity()); err != nil {
		return report, err
	}
	id := a.Identity()
	bc.Identity = "homelabfs-validation-v1\n" + id.Volume.String() + "\n" + id.Generation.String() + "\n" + id.Session.String() + "\n"
	if err := json.NewEncoder(output).Encode(behaviorReady{readyMessage: readyMessage{Profile: o.profile, Mountpoint: c.Mountpoint, VolumeID: id.Volume.String(), Generation: id.Generation.String(), Session: id.Session.String()}, Role: o.role, DaemonUID: os.Geteuid(), ClientUID: roleCredentials(o.role).Uid, ClientGID: roleCredentials(o.role).Gid, ClientGroups: roleCredentials(o.role).Groups, DataRoot: c.layout.DataRoot, JournalRoot: c.layout.JournalRoot, ControlRoot: c.layout.ControlRoot, BehaviorSHA256: o.digest}); err != nil {
		return report, err
	}
	bc.Unmount = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		unmountAttempted = true
		if err := lifetime.unmount(func() error { return boundedBehaviorUnmount(context.Background(), mount.Unmount) }); err != nil {
			return err
		}
		mount = nil
		return nil
	}
	report = behavior.Run(ctx, bc)
	if report.Failed() {
		return report, fmt.Errorf("portable behavior failed; preserve roots and evidence")
	}
	return report, nil
}

func readyBehaviorHelper(ctx context.Context, child *behavior.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	reply, err := child.Call(ctx, behavior.Command{Op: "ready"})
	if err != nil {
		return fmt.Errorf("prepublication helper barrier: %w", err)
	}
	if reply.Error != "" {
		return fmt.Errorf("prepublication helper barrier: %s", reply.Error)
	}
	return nil
}

func roleCredentials(role string) *syscall.Credential {
	if role == "unprivileged" {
		return &syscall.Credential{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}
	}
	return &syscall.Credential{Uid: 0, Gid: 0, Groups: []uint32{}}
}

// A timed-out normal unmount remains uncertain. The caller retains its source
// writer/admin resources and never retries, force-unmounts or deletes backing.
func boundedBehaviorUnmount(ctx context.Context, normal func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- normal() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return fmt.Errorf("normal unmount uncertain: %w", ctx.Err())
	}
}
