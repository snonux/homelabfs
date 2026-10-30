package behavior

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	build "github.com/snonux/homelabfs/internal"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// CLI runs the portable binary interface; it never creates mounts or controls infrastructure.
func CLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hfs-behavior", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("path", "", "existing disposable directory or mount to test")
	profile := flags.String("profile", "filesystem", "filesystem, hfs-direct or hfs-buffered")
	volume := flags.String("volume", "", "expected HomelabFS volume UUID")
	generation := flags.String("generation", "", "expected HomelabFS generation UUID")
	session := flags.String("session", "", "expected HomelabFS connection session UUID")
	jsonPath := flags.String("json", "behavior.json", "JSON report outside tested filesystem (new file)")
	xmlPath := flags.String("junit", "behavior.xml", "JUnit report outside tested filesystem (new file)")
	helper := flags.Bool("helper", false, "private idle fixed-credential helper entry point")
	version := flags.Bool("version", false, "print version")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *version {
		_, _ = fmt.Fprintln(stdout, build.Version)
		return 0
	}
	if *helper {
		if err := serveHelper(ctx, os.Stdin, stdout, stderr); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	if *path == "" {
		_, _ = fmt.Fprintln(stderr, "-path must name a disposable target; no mounts or lab operations are created")
		return 2
	}
	identity := ""
	if *profile != "filesystem" {
		var canonical []string
		for _, raw := range []string{*volume, *generation, *session} {
			id, err := fsapi.ParseUUID(raw)
			if err != nil || !id.Valid() {
				_, _ = fmt.Fprintln(stderr, "HomelabFS profile requires valid -volume, -generation, -session")
				return 2
			}
			canonical = append(canonical, id.String())
		}
		identity = "homelabfs-validation-v1\n" + canonical[0] + "\n" + canonical[1] + "\n" + canonical[2] + "\n"
	} else if *volume != "" || *generation != "" || *session != "" {
		_, _ = fmt.Fprintln(stderr, "identity flags require a HomelabFS profile")
		return 2
	}
	absolute, err := filepath.Abs(*path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	report, err := executeReports(ctx, absolute, *profile, identity, *jsonPath, *xmlPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	_, _ = fmt.Fprintf(stderr, "profile %s: JSON %s; JUnit %s; run directory %s\n", report.Profile, *jsonPath, *xmlPath, report.RunDirectory)
	if report.Failed() {
		return 1
	}
	return 0
}

func execute(parent context.Context, path, profile, identity string) (Report, error) {
	executable, err := os.Executable()
	if err != nil {
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	spawn := ExecutableSpawner(executable, "-helper")
	config := Config{Path: path, Profile: profile, Identity: identity, RootSkip: "root-only synthetic users/groups/sticky case skipped: run as root to enable"}
	for range 3 {
		client, err := Start(ctx, spawn, nil)
		if err != nil {
			return Report{}, errors.Join(err, cleanup(config))
		}
		config.Clients = append(config.Clients, client)
	}
	if os.Geteuid() == 0 {
		for _, credential := range []*syscall.Credential{{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}, {Uid: 20003, Gid: 20003, Groups: []uint32{}}} {
			client, err := Start(ctx, spawn, credential)
			if err != nil {
				return Report{}, errors.Join(err, cleanup(config))
			}
			config.Users = append(config.Users, client)
		}
	}
	return Run(ctx, config), nil
}

func cleanup(c Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var errs []error
	for _, client := range append(c.Clients, c.Users...) {
		errs = append(errs, client.Stop(ctx))
	}
	return errors.Join(errs...)
}

func executeReports(ctx context.Context, path, profile, identity, jsonPath, xmlPath string) (report Report, err error) {
	for _, output := range []string{jsonPath, xmlPath} {
		if err := outsideTarget(path, output); err != nil {
			return Report{}, err
		}
	}
	jsonFile, err := os.OpenFile(jsonPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Report{}, err
	}
	defer func() { err = errors.Join(err, jsonFile.Close()) }()
	xmlFile, err := os.OpenFile(xmlPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Report{}, err
	}
	defer func() { err = errors.Join(err, xmlFile.Close()) }()
	report, err = execute(ctx, path, profile, identity)
	if err != nil {
		return report, err
	}
	err = errors.Join(report.JSON(jsonFile), report.JUnit(xmlFile))
	return report, err
}

func outsideTarget(target, output string) error {
	absolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		resolved = target
	}
	relative, err := filepath.Rel(resolved, filepath.Join(parent, filepath.Base(absolute)))
	if err != nil {
		return err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("report %s must be outside disposable target %s", output, target)
	}
	return nil
}
