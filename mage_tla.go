//go:build mage

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	tlaVersion = "1.8.0"
	tlaSHA256  = "fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e"
	tlaCache   = ".cache/tla"
)

// TLA exhaustively checks the bounded normal replication safety and liveness models.
func TLA(ctx context.Context) error {
	return runTLAConfigs(ctx, false, "Safety", "Liveness", "LivenessOwner", "Ordered")
}

// TLANegative checks that deliberate protocol defects produce safety counterexamples.
func TLANegative(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "NegativeGap", "NegativeSession", "NegativeDedup")
}

// TLATakeover checks the bounded positive-fence ownership transfer profiles.
func TLATakeover(ctx context.Context) error {
	return runTLAConfigs(ctx, false, "TakeoverPreflight", "TakeoverFaults", "TakeoverEnvironment", "TakeoverCombined",
		"TakeoverLiveness", "TakeoverLivenessEnvironment")
}

// TLATakeoverNegative requires counterexamples for deliberately broken takeover gates.
func TLATakeoverNegative(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "TakeoverNegativeFence", "TakeoverNegativeEpoch", "TakeoverNegativeAbsence")
}

// TLATakeoverCoverage requires concrete successful and safely blocked takeover traces.
func TLATakeoverCoverage(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "TakeoverWitnessSync", "TakeoverWitnessAsync",
		"TakeoverWitnessGap", "TakeoverWitnessIntactWait")
}

func runTLAConfigs(ctx context.Context, negative bool, configs ...string) error {
	jar, err := tlaJar(ctx)
	if err != nil {
		return err
	}
	for _, config := range configs {
		if err := runTLC(ctx, jar, config, negative); err != nil {
			return err
		}
	}
	return nil
}

func runTLC(ctx context.Context, jar, config string, negative bool) error {
	// Keep logs and counterexamples even when TLC fails. Each run gets private
	// metadata so interrupted or concurrent invocations cannot share state files.
	dir, err := os.MkdirTemp(tlaCache, config+"-")
	if err != nil {
		return fmt.Errorf("create TLC metadata directory: %w", err)
	}
	log, err := os.Create(filepath.Join(dir, "output.log"))
	if err != nil {
		return fmt.Errorf("create TLC log: %w", err)
	}
	defer func() { _ = log.Close() }()
	fmt.Printf("TLC %s; evidence: %s\n", config, dir)
	module := "Replication.tla"
	heap := "-Xmx2048m"
	if strings.HasPrefix(config, "Takeover") {
		module = "Takeover.tla"
		heap = "-Xmx512m"
	}
	cmd := exec.CommandContext(ctx, "java", heap, "-XX:ActiveProcessorCount=1", "-XX:+UseParallelGC",
		"-cp", jar, "tlc2.TLC", "-workers", "1", "-noGenerateSpecTE",
		"-lncheck", "final",
		"-metadir", dir, "-config", filepath.Join("spec", "tla", config+".cfg"),
		filepath.Join("spec", "tla", module))
	cmd.Stdout = io.MultiWriter(os.Stdout, log)
	cmd.Stderr = cmd.Stdout
	runErr := cmd.Run()
	if err := log.Close(); err != nil {
		return fmt.Errorf("close TLC log: %w", err)
	}
	if negative {
		return checkTLCCounterexample(config, filepath.Join(dir, "output.log"), runErr)
	}
	if runErr != nil {
		return fmt.Errorf("TLC %s failed (evidence %s): %w", config, dir, runErr)
	}
	return nil
}

func checkTLCCounterexample(config, path string, runErr error) error {
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 12 {
		return fmt.Errorf("TLC %s: expected invariant violation (exit 12), got %v", config, runErr)
	}
	output, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read negative TLC evidence: %w", err)
	}
	invariant := "Safety"
	if strings.HasPrefix(config, "TakeoverWitness") {
		invariant = "WitnessAbsent"
	}
	if !strings.Contains(string(output), "Invariant "+invariant+" is violated") {
		return fmt.Errorf("TLC %s failed without the expected %s counterexample", config, invariant)
	}
	return nil
}

func tlaJar(ctx context.Context) (string, error) {
	if err := os.MkdirAll(tlaCache, 0o755); err != nil {
		return "", fmt.Errorf("create TLA cache: %w", err)
	}
	path := filepath.Join(tlaCache, "tla2tools-"+tlaVersion+".jar")
	data, err := os.ReadFile(path)
	if err == nil {
		if fmt.Sprintf("%x", sha256.Sum256(data)) != tlaSHA256 {
			return "", fmt.Errorf("cached %s checksum mismatch; preserve/inspect or remove it", path)
		}
		return path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read cached TLC jar: %w", err)
	}
	return downloadTLAJar(ctx, path)
}

func downloadTLAJar(ctx context.Context, path string) (string, error) {
	url := "https://github.com/tlaplus/tlaplus/releases/download/v" + tlaVersion + "/tla2tools.jar"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create TLC download: %w", err)
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download TLC: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download TLC: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", fmt.Errorf("read TLC download: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != tlaSHA256 {
		return "", fmt.Errorf("downloaded TLC checksum mismatch")
	}
	file, err := os.CreateTemp(tlaCache, "download-*.jar")
	if err != nil {
		return "", fmt.Errorf("create TLC download file: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write TLC download: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close TLC download: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", fmt.Errorf("publish TLC download: %w", err)
	}
	return path, nil
}
