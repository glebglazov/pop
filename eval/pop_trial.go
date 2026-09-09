package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

type trialFS struct {
	deps.FileSystem
	data string
}

func (f trialFS) Getenv(key string) string {
	if key == "XDG_DATA_HOME" {
		return f.data
	}
	return f.FileSystem.Getenv(key)
}

func runPopTrial(binary, caseDir, clone string, arm armFile, ceiling time.Duration, record *trialRecord) error {
	if strings.ContainsRune(binary, filepath.Separator) {
		var err error
		binary, err = filepath.Abs(binary)
		if err != nil {
			return err
		}
	}
	dataDir := filepath.Join(record.WorkDir, "data")
	configDir := filepath.Join(record.WorkDir, "config")
	d := *tasks.DefaultDeps()
	d.FS = trialFS{FileSystem: d.FS, data: dataDir}
	identity, err := tasks.ResolveRepositoryIdentity(&d, clone)
	if err != nil {
		return err
	}
	setDir := filepath.Join(identity.TasksDir, record.Case)
	if err := os.CopyFS(setDir, os.DirFS(filepath.Join(caseDir, "tasks"))); err != nil {
		return fmt.Errorf("copy Case Task set: %w", err)
	}
	if err := writePopTrialConfig(configDir, clone, arm); err != nil {
		return err
	}
	manifestPath := filepath.Join(setDir, tasks.ManifestFileName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	for key, value := range arm.Manifest {
		manifest[key] = value
	}
	for _, phase := range []string{"verifier", "refiner", "explorer"} {
		manifest[phase] = map[string]any{"agents": []string{arm.agentSpec()}}
	}
	raw, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := tasks.WriteAtomic(manifestPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	env := []string{"XDG_DATA_HOME=" + dataDir, "XDG_CONFIG_HOME=" + configDir, "NO_COLOR=1", "TERM=dumb", "TMUX=", "TMUX_PANE="}
	ctx, cancel := context.WithTimeout(context.Background(), ceiling)
	defer cancel()
	if _, err := popTrialCommand(ctx, binary, clone, env, "register", record.Case); err != nil {
		return err
	}
	_, implementErr := popTrialCommand(ctx, binary, clone, env, "implement", record.Case)
	timedOut := errors.Is(implementErr, context.DeadlineExceeded)
	// Collection gets a separate bound after the drain's ceiling expires.
	collect, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	status, statusErr := popTrialCommand(collect, binary, clone, env, "status", record.Case)
	spend, spendErr := popTrialCommand(collect, binary, clone, env, "spend", record.Case, "--json")
	if spendErr == nil {
		if !json.Valid(spend) {
			spendErr = errors.New("Pop returned invalid Spend JSON")
		} else {
			record.SetSpend = json.RawMessage(spend)
		}
	}
	if statusErr == nil {
		for _, line := range strings.Split(string(status), "\n") {
			if !strings.HasPrefix(line, record.Case+"  [") {
				continue
			}
			value, _, found := strings.Cut(strings.TrimPrefix(line, record.Case+"  ["), "]")
			if fields := strings.Fields(value); found && len(fields) > 0 {
				record.SetStatus = fields[0]
			}
			break
		}
		if record.SetStatus == "" {
			statusErr = errors.New("Pop status output has no Task-set header")
		}
	}
	if timedOut {
		record.Outcome, record.Reason = outcomeTimedOut, "Trial ceiling reached"
	} else {
		switch record.SetStatus {
		case string(tasks.StatusDone), string(tasks.StatusFailed), string(tasks.StatusVerifyFailed):
			record.Outcome = outcomeCompleted
		default:
			if implementErr == nil {
				implementErr = fmt.Errorf("Pop stopped at nonterminal status %q", record.SetStatus)
			}
			return errors.Join(implementErr, statusErr, spendErr)
		}
	}
	return errors.Join(statusErr, spendErr)
}

func savePopTrialReports(clone, resultDir string, record *trialRecord) error {
	dataDir := filepath.Join(record.WorkDir, "data")
	d := *tasks.DefaultDeps()
	d.FS = trialFS{FileSystem: d.FS, data: dataDir}
	identity, err := tasks.ResolveRepositoryIdentity(&d, clone)
	if err != nil {
		return fmt.Errorf("resolve Pop Trial reports: %w", err)
	}
	setDir := filepath.Join(identity.TasksDir, record.Case)
	var copyErr error
	for _, name := range []string{tasks.VerifyDirName, tasks.RefineDirName} {
		source := filepath.Join(setDir, name)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			copyErr = errors.Join(copyErr, fmt.Errorf("read %s reports: %w", name, err))
			continue
		}
		if err := os.CopyFS(filepath.Join(resultDir, "reports", name), os.DirFS(source)); err != nil {
			copyErr = errors.Join(copyErr, fmt.Errorf("copy %s reports: %w", name, err))
		}
	}
	return copyErr
}

func writePopTrialConfig(root, clone string, arm armFile) error {
	var contents bytes.Buffer
	if err := toml.NewEncoder(&contents).Encode(arm.Config); err != nil {
		return err
	}
	var config map[string]any
	if _, err := toml.Decode(contents.String(), &config); err != nil {
		return err
	}
	work, ok := config["work"].(map[string]any)
	if !ok {
		return errors.New("Pop arm requires config.work")
	}
	for _, phase := range []string{"implement", "verify", "refine"} {
		group, ok := work[phase].(map[string]any)
		if !ok {
			return fmt.Errorf("Pop arm requires config.work.%s", phase)
		}
		group["agents"] = []string{arm.agentSpec()}
	}
	config["repo"] = map[string]any{clone: map[string]any{"turn_cap": 0}}
	contents.Reset()
	if err := toml.NewEncoder(&contents).Encode(config); err != nil {
		return err
	}
	dir := filepath.Join(root, "pop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Keep the effective override layer inside the Trial as well.
	for _, name := range []string{"config.toml", "config.override.toml"} {
		if err := tasks.WriteAtomic(filepath.Join(dir, name), contents.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func popTrialCommand(ctx context.Context, binary, clone string, env []string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	proc, err := (tasks.RealCommandRunner{}).StartWithEnv(context.Background(), clone, env, &stdout, &stderr, binary, append([]string{"tasks"}, args...)...)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		code, err := proc.Wait()
		if err == nil && code != 0 {
			err = fmt.Errorf("pop tasks %s exited %d: %s", strings.Join(args, " "), code, stderr.String())
		}
		done <- err
	}()
	select {
	case err := <-done:
		return stdout.Bytes(), err
	case <-ctx.Done():
		_ = proc.SignalGroup(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = proc.SignalGroup(syscall.SIGKILL)
			<-done
		}
		return stdout.Bytes(), ctx.Err()
	}
}
