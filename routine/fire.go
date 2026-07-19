package routine

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// FireResult is the outcome of a successful Routine run.
type FireResult struct {
	ID          int64
	RoutineID   string
	ReportPath  string
	AgentPreset string
}

// Fire runs one Routine using default dependencies.
func Fire(id string) (*FireResult, error) {
	return FireWith(defaultDeps, id)
}

// FireWith executes one Routine run to completion in the foreground.
func FireWith(d *Deps, id string) (*FireResult, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return nil, err
	}

	cfg, err := d.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	promptPath := filepath.Join(routineDir(d, id), promptFileName)
	domainPrompt, err := d.FS.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("read routine prompt: %w", err)
	}

	firedAt := nowUTC(d)
	reportRel := filepath.Join(runsDirName, firedAt.Format("2006-01-02T15-04-05Z")+".md")
	reportAbs := filepath.Join(routineDir(d, id), reportRel)
	memoryDir := filepath.Join(routineDir(d, id), memoryDirName)
	wrappedPrompt := wrapRoutinePrompt(memoryDir, reportAbs, string(domainPrompt))

	s, err := openExecutionStore(d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	pid := d.PID()
	procStart, _ := d.ProcStartToken(pid)
	run, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: id,
		FiredAt:   firedAt,
		PID:       pid,
		ProcStart: procStart,
	}, func(live store.RoutineRun) bool {
		return d.ProcessAlive(live.PID, live.ProcStart)
	})
	if err != nil {
		if errors.Is(err, store.ErrRoutineRunInProgress) {
			return nil, fmt.Errorf("routine %q is already running", id)
		}
		return nil, fmt.Errorf("record routine run start: %w", err)
	}

	finish := func(outcome, failReason string) error {
		return s.FinishRoutineRun(run.ID, outcome, reportAbs, failReason, nowUTC(d))
	}
	// A failed run — daemon-fired or manual — pauses its Routine with reason
	// `failure` (ADR-0128). The latest cause is the useful one, so an
	// already-paused Routine is overwritten to `failure`.
	failAndPause := func(reason string) {
		_ = finish(store.RoutineRunFailed, reason)
		r.Manifest.Paused = true
		r.Manifest.PauseReason = PauseReasonFailure
		_ = writeManifest(d, id, r.Manifest)
	}

	out := d.Stdout
	if out == nil {
		out = io.Discard
	}
	timeout := d.AttemptTimeout
	if timeout <= 0 {
		timeout = tasks.DefaultAttemptTimeout
	}

	taskDeps := d.taskDeps()
	attempt := func(agentSpec string) (*tasks.RoutineAgentAttempt, error) {
		return tasks.RunRoutineAgentInvocation(taskDeps, r.Manifest.BoundDirectory, out, timeout, agentSpec, wrappedPrompt)
	}

	specs := resolveRoutineRunSpecs(cfg, r.Manifest)
	result, preset, execErr := runRoutineWithAgentFallback(d, cfg, specs, out, attempt)
	if execErr != nil {
		reason := execErr.Error()
		if result != nil && result.ExitCode != 0 {
			reason = fmt.Sprintf("agent exited with status %d", result.ExitCode)
		}
		failAndPause(reason)
		return nil, fmt.Errorf("routine run failed: %w", errors.New(reason))
	}
	if result == nil || result.ExitCode != 0 {
		reason := "agent exited with non-zero status"
		if result != nil {
			reason = fmt.Sprintf("agent exited with status %d", result.ExitCode)
		}
		failAndPause(reason)
		return nil, fmt.Errorf("routine run failed: %s", reason)
	}

	// Clean exit: the outcome is sentinel-assessed, not exit-status-inferred
	// (ADR-0127). An agent that exits 0 without ROUTINE_COMPLETE, or without
	// writing its report, is recorded failed.
	outcome := assessRoutineOutput(result.Output, reportExists(d, reportAbs))
	if !outcome.Succeeded {
		failAndPause(outcome.FailReason)
		return nil, fmt.Errorf("routine run failed: %s", outcome.FailReason)
	}

	if err := finish(store.RoutineRunSucceeded, ""); err != nil {
		return nil, fmt.Errorf("record routine run success: %w", err)
	}

	return &FireResult{
		ID:          run.ID,
		RoutineID:   id,
		ReportPath:  reportAbs,
		AgentPreset: preset,
	}, nil
}

// reportExists reports whether the run's report file landed on disk.
func reportExists(d *Deps, reportAbs string) bool {
	if _, err := d.FS.Stat(reportAbs); err != nil {
		return false
	}
	return true
}

// LoadConfigFunc loads pop configuration.
type LoadConfigFunc func() (*config.Config, error)

// DefaultLoadConfig loads config from the default config path.
func DefaultLoadConfig() (*config.Config, error) {
	return config.Load(config.DefaultConfigPath())
}
