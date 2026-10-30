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
	"strconv"
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

// TLATransactions checks bounded request authority, transaction and namespace safety.
func TLATransactions(ctx context.Context) error {
	return runTLAConfigs(ctx, false, "TransactionsAuthority", "TransactionsProtocol",
		"TransactionsNamespace", "TransactionsLiveness", "TransactionsRecoveryLiveness")
}

// TLATransactionsNegative requires counterexamples for removed transaction and home-coverage guards.
func TLATransactionsNegative(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "TransactionsNegativePrepare", "TransactionsNegativeFence",
		"TransactionsNegativeDedup", "TransactionsNegativeVisibility", "TransactionsNegativeRetention", "TransactionsNegativeHomeCoverage")
}

// TLATransactionsCoverage requires integrated, recovery, gap and namespace witness traces.
func TLATransactionsCoverage(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "TransactionsWitnessIntegrated", "TransactionsWitnessRecovery",
		"TransactionsWitnessGap", "TransactionsWitnessPartial", "TransactionsWitnessLifetime",
		"TransactionsWitnessStaleCoverage", "TransactionsWitnessReclaim")
}

// TLAEmergency checks bounded cluster-wide emergency activation and explicit exit.
func TLAEmergency(ctx context.Context) error {
	return runTLAConfigs(ctx, false, "EmergencySafety", "EmergencyFaults")
}

// TLAEmergencyNegative requires counterexamples for removed emergency safety gates.
func TLAEmergencyNegative(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "EmergencyNegativeRegistry", "EmergencyNegativeLineage",
		"EmergencyNegativeFresh", "EmergencyNegativeApproval", "EmergencyNegativeLaunch", "EmergencyNegativeFacts",
		"EmergencyNegativeFence", "EmergencyNegativeCommit", "EmergencyNegativeClaim",
		"EmergencyNegativeRetirement", "EmergencyNegativePeer", "EmergencyNegativeRegrant", "EmergencyNegativeExit", "EmergencyNegativeFailback")
}

// TLAEmergencyCoverage requires complete, interrupted, competing and blocked traces.
func TLAEmergencyCoverage(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "EmergencyWitnessExit", "EmergencyWitnessRestore",
		"EmergencyWitnessRestart", "EmergencyWitnessPeerRestart", "EmergencyWitnessPartial", "EmergencyWitnessConflict", "EmergencyWitnessAmbiguous", "EmergencyWitnessContinuity")
}

// TLACoordination checks bounded allocation CAS, crashes and offline restore boundaries.
func TLACoordination(ctx context.Context) error {
	return runTLAConfigs(ctx, false, "CoordinationSafety", "CoordinationPolicy",
		"CoordinationCrash", "CoordinationRestore", "CoordinationFreshRestore")
}

// TLACoordinationNegative requires counterexamples for removed coordination guards.
func TLACoordinationNegative(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "CoordinationNegativePolicyCAS", "CoordinationNegativeRegistry",
		"CoordinationNegativePolicyRace", "CoordinationNegativeGrantCAS", "CoordinationNegativeReservation",
		"CoordinationNegativeEpoch", "CoordinationNegativeAuthority", "CoordinationNegativeRestore")
}

// TLACoordinationCoverage requires concrete contention, uncertainty, restart and restore traces.
func TLACoordinationCoverage(ctx context.Context) error {
	return runTLAConfigs(ctx, true, "CoordinationWitnessCompeting", "CoordinationWitnessUnknown",
		"CoordinationWitnessRestart", "CoordinationWitnessRestore", "CoordinationWitnessFreshRestore",
		"CoordinationWitnessPolicy")
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
	modulePath := filepath.Join("spec", "tla", module)
	configPath := filepath.Join("spec", "tla", config+".cfg")
	transactions := strings.HasPrefix(config, "Transactions")
	emergency := strings.HasPrefix(config, "Emergency")
	coordination := strings.HasPrefix(config, "Coordination")
	if transactions || emergency || coordination {
		heap = "-Xmx512m"
		if emergency {
			module = "Emergency.tla"
		} else if coordination {
			module = "Coordination.tla"
		} else {
			module = "Transactions.tla"
		}
		modulePath, configPath, err = snapshotTLCModel(dir, module, config)
		if err != nil {
			return err
		}
	}
	if emergency || coordination {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		bounds := "heapMiB=512\nworkers=1\nactiveProcessors=1\nwallTimeout=3m\nstateSpace=finite configuration; no depth or state cutoff\n"
		if err := os.WriteFile(filepath.Join(dir, "bounds.txt"), []byte(bounds), 0o600); err != nil {
			return fmt.Errorf("write TLC bounds: %w", err)
		}
	}
	cmd := exec.CommandContext(ctx, "java", heap, "-XX:ActiveProcessorCount=1", "-XX:+UseParallelGC",
		"-cp", jar, "tlc2.TLC", "-workers", "1", "-noGenerateSpecTE",
		"-lncheck", "final",
		"-metadir", dir, "-config", configPath, modulePath)
	cmd.Stdout = io.MultiWriter(os.Stdout, log)
	cmd.Stderr = cmd.Stdout
	if transactions || emergency || coordination {
		if err := os.WriteFile(filepath.Join(dir, "argv.txt"), []byte(strings.Join(cmd.Args, "\n")+"\n"), 0o600); err != nil {
			return fmt.Errorf("write TLC command: %w", err)
		}
	}
	runErr := cmd.Run()
	if transactions || emergency || coordination {
		if err := recordTLCExit(dir, runErr); err != nil {
			return err
		}
	}
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
	if strings.HasPrefix(config, "TakeoverWitness") || strings.HasPrefix(config, "TransactionsWitness") || strings.HasPrefix(config, "EmergencyWitness") || strings.HasPrefix(config, "CoordinationWitness") {
		invariant = "WitnessAbsent"
	}
	if !strings.Contains(string(output), "Invariant "+invariant+" is violated") {
		return fmt.Errorf("TLC %s failed without the expected %s counterexample", config, invariant)
	}
	return nil
}

func snapshotTLCModel(dir, module, config string) (string, string, error) {
	modulePath := filepath.Join(dir, module)
	configPath := filepath.Join(dir, config+".cfg")
	var hashes strings.Builder
	for _, name := range []string{module, config + ".cfg"} {
		data, err := os.ReadFile(filepath.Join("spec", "tla", name))
		if err != nil {
			return "", "", fmt.Errorf("read TLC source: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return "", "", fmt.Errorf("snapshot TLC source: %w", err)
		}
		_, _ = fmt.Fprintf(&hashes, "%x  %s\n", sha256.Sum256(data), name)
	}
	_, _ = fmt.Fprintf(&hashes, "%s  tla2tools-%s.jar\n", tlaSHA256, tlaVersion)
	if err := os.WriteFile(filepath.Join(dir, "sha256.txt"), []byte(hashes.String()), 0o600); err != nil {
		return "", "", fmt.Errorf("write TLC source hashes: %w", err)
	}
	return modulePath, configPath, nil
}

func recordTLCExit(dir string, runErr error) error {
	status := "0"
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			status = strconv.Itoa(exitErr.ExitCode())
		} else {
			status = "launch-error: " + runErr.Error()
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "exit.txt"), []byte(status+"\n"), 0o600); err != nil {
		return fmt.Errorf("write TLC exit receipt: %w", err)
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
