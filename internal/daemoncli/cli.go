package daemoncli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/snonux/homelabfs/internal"
	"github.com/snonux/homelabfs/internal/admin"
)

// Run parses a complete invocation and runs foreground until ctx is canceled.
// It never invokes a shell, manipulates datasets, or admits normal publication.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "-version" || args[0] == "--version" || args[0] == "version") {
		_, err := fmt.Fprintln(stdout, internal.Version)
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabfs node-init --control DIR | volume-init|inspect|status|mount|serve --config FILE (see docs/spec/Configuration.md)")
	}
	if args[0] == "node-init" {
		return runNodeInit(ctx, args[1:], stdout, stderr)
	}
	return runVolumeCommand(ctx, args, stdout, stderr)
}

func runNodeInit(ctx context.Context, args []string, output, stderr io.Writer) error {
	f := flag.NewFlagSet("node-init", flag.ContinueOnError)
	f.SetOutput(stderr)
	dir := f.String("control", "", "new private control directory (parent must exist)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *dir == "" || f.NArg() != 0 {
		return fmt.Errorf("node-init requires --control DIR and no positional arguments")
	}
	n, err := initNode(ctx, *dir)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(n)
}

func runVolumeCommand(ctx context.Context, args []string, output, stderr io.Writer) error {
	command := args[0]
	switch command {
	case "volume-init", "inspect", "status", "mount", "serve":
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(stderr)
	configPath := f.String("config", "", "private version 1 JSON configuration file")
	validation := f.Bool("disposable-validation", false, "explicit local-only disposable validation; no partner ACK or normal publication")
	selfClient := f.Bool("self-client", false, "admit only this executable's controlled immutable-credential client")
	selfTest := f.Bool("self-test", false, "perform bounded built-in synthetic I/O then normally unmount")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if *configPath == "" || f.NArg() != 0 {
		return fmt.Errorf("%s requires --config FILE and no positional arguments", command)
	}
	if (command == "inspect" || command == "status") && (*validation || *selfClient || *selfTest) {
		return fmt.Errorf("%s accepts only --config", command)
	}
	if command == "volume-init" && (!*validation || *selfClient || *selfTest) {
		return fmt.Errorf("volume-init requires --disposable-validation; normal volume initialization is not implemented")
	}
	if (command == "mount" || command == "serve") && (!*validation || !*selfClient) {
		return fmt.Errorf("normal publication is unsupported: real Protocol and native credential/traversal/cache/mmap admission remain unimplemented; validation requires --disposable-validation --self-client")
	}
	if command == "status" {
		c, err := loadStatusConfig(ctx, *configPath)
		if err != nil {
			return err
		}
		if err := checkNode(c); err != nil {
			return err
		}
		response, err := admin.Call(ctx, socketPath(c), admin.Request{Version: 1, Operation: "status"})
		if err != nil {
			return err
		}
		if response.Status == nil || response.Status.NodeID != c.NodeID || len(response.Status.Volumes) != 1 || response.Status.Volumes[0].VolumeID != c.VolumeID {
			return fmt.Errorf("admin status target mismatch")
		}
		return json.NewEncoder(output).Encode(response.Status)
	}
	c, err := loadConfig(ctx, *configPath)
	if err != nil {
		return err
	}
	if command == "volume-init" {
		v, err := initVolume(ctx, c)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(v)
	}
	if command == "inspect" {
		v, err := inspectVolume(c)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(v)
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	return serve(ctx, c, *selfTest, output, logger)
}
