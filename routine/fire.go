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

// FireWith fires the Routine addressed by id. Addressing follows ADR-0138: an
// explicit `project:<name>` fires the current checkout's Project routine; a bare
// name resolves to an authored Routine first on exact match, else the current
// checkout's Project routine of that name. When an authored id shadows a Project
// routine of the same name, the authored one wins and a warning names the
// `project:` escape hatch.
func FireWith(d *Deps, id string) (*FireResult, error) {
	if name, ok := parseProjectRef(id); ok {
		return fireProjectRoutine(d, name)
	}
	if authoredRoutineExists(d, id) {
		if projectRoutineExists(d, id) {
			fmt.Fprintf(fireWarnWriter(d), "warning: authored routine %q shadows a Project routine of the same name; fire the Project one with `pop routine fire %s%s`\n", id, ProjectOrigin, id)
		}
		return fireAuthored(d, id)
	}
	if projectRoutineExists(d, id) {
		return fireProjectRoutine(d, id)
	}
	// No authored routine and no Project routine: fall through to the authored
	// path so the caller gets the familiar "not found" error.
	return fireAuthored(d, id)
}

// fireAuthored executes one authored Routine run to completion in the foreground.
func fireAuthored(d *Deps, id string) (*FireResult, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return nil, err
	}

	// The frontmatter carries settings only (ADR-0139); the run's actual prompt
	// is the body below the fence. Strip the frontmatter so the agent never sees
	// the YAML and the fingerprint hashes the same body the daemon would.
	_, domainPrompt, err := readPromptFrontmatter(d, routineDir(d, id), id)
	if err != nil {
		return nil, err
	}

	// A failed run — daemon-fired or manual — pauses its Routine with reason
	// `failure` (ADR-0128). The latest cause is the useful one, so an
	// already-paused Routine is overwritten to `failure`.
	onFail := func(string) {
		r.Manifest.Paused = true
		r.Manifest.PauseReason = PauseReasonFailure
		_ = writeState(d, id, r.Manifest)
	}

	return executeFire(d, firePlan{
		storeID:   id,
		displayID: id,
		boundDir:  r.Manifest.BoundDirectory,
		prompt:    domainPrompt,
		root:      routineDir(d, id),
		agents:    r.Manifest.Agents,
		effort:    r.Manifest.Effort,
		// Every run records the fingerprint in effect when it fired (ADR-0128).
		// A manual fire re-proves an edited Routine by recording the new value;
		// the daemon compares this against the last run's before firing.
		fingerprint: fingerprintOf(domainPrompt, r.Manifest),
		onFail:      onFail,
	})
}

// firePlan is the resolved surface a single Routine run needs, independent of
// whether it came from an authored Routine or a Project routine (ADR-0138). The
// two differ only in where storeID/root point and whether onFail pauses.
type firePlan struct {
	// storeID is the routine_id run rows key on; displayID is the human-facing
	// id (a Project routine renders `project:<name>`).
	storeID   string
	displayID string
	// boundDir is the directory the agent runs in.
	boundDir string
	// prompt is the frontmatter-stripped body.
	prompt string
	// root is the directory holding the run's memory/ and runs/ subdirectories.
	root        string
	agents      []string
	effort      string
	fingerprint string
	// onFail, when set, runs after a failed run is recorded — authored Routines
	// pause; Project routines have no pause state so leave it nil.
	onFail func(reason string)
}

// executeFire runs one plan to completion in the foreground: it enforces
// per-storeID exclusivity, invokes the agent with the standard wrapper, and
// sentinel-assesses the outcome.
func executeFire(d *Deps, p firePlan) (*FireResult, error) {
	cfg, err := d.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	firedAt := nowUTC(d)
	reportRel := filepath.Join(runsDirName, firedAt.Format("2006-01-02T15-04-05Z")+".md")
	reportAbs := filepath.Join(p.root, reportRel)
	memoryDir := filepath.Join(p.root, memoryDirName)
	wrappedPrompt := wrapRoutinePrompt(memoryDir, reportAbs, p.prompt)

	s, err := openExecutionStore(d)
	if err != nil {
		return nil, err
	}

	pid := d.PID()
	procStart, _ := d.ProcStartToken(pid)
	run, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID:   p.storeID,
		FiredAt:     firedAt,
		PID:         pid,
		ProcStart:   procStart,
		Fingerprint: p.fingerprint,
	}, func(live store.RoutineRun) bool {
		return d.ProcessAlive(live.PID, live.ProcStart)
	})
	if err != nil {
		if errors.Is(err, store.ErrRoutineRunInProgress) {
			return nil, fmt.Errorf("routine %q is already running", p.displayID)
		}
		return nil, fmt.Errorf("record routine run start: %w", err)
	}

	finish := func(outcome, failReason string) error {
		return s.FinishRoutineRun(run.ID, outcome, reportAbs, failReason, nowUTC(d))
	}
	fail := func(reason string) {
		_ = finish(store.RoutineRunFailed, reason)
		if p.onFail != nil {
			p.onFail(reason)
		}
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
		return tasks.RunRoutineAgentInvocation(taskDeps, p.boundDir, out, timeout, agentSpec, wrappedPrompt)
	}

	specs, err := resolveRoutineRunSpecs(cfg, Manifest{Agents: p.agents, Effort: p.effort})
	if err != nil {
		// An explicit empty agent override is a configuration the run cannot
		// proceed under, so the run is filed as failed with the sentence naming
		// the key rather than started on a preset nobody asked for.
		fail(err.Error())
		return nil, fmt.Errorf("routine run failed: %w", err)
	}
	result, preset, execErr := runRoutineWithAgentFallback(d, cfg, specs, out, attempt)
	if execErr != nil {
		reason := execErr.Error()
		if result != nil && result.ExitCode != 0 {
			reason = fmt.Sprintf("agent exited with status %d", result.ExitCode)
		}
		fail(reason)
		return nil, fmt.Errorf("routine run failed: %w", errors.New(reason))
	}
	if result == nil || result.ExitCode != 0 {
		reason := "agent exited with non-zero status"
		if result != nil {
			reason = fmt.Sprintf("agent exited with status %d", result.ExitCode)
		}
		fail(reason)
		return nil, fmt.Errorf("routine run failed: %s", reason)
	}

	// Clean exit: the outcome is sentinel-assessed, not exit-status-inferred
	// (ADR-0127). An agent that exits 0 without ROUTINE_COMPLETE, or without
	// writing its report, is recorded failed.
	outcome := assessRoutineOutput(result.Output, reportExists(d, reportAbs))
	if !outcome.Succeeded {
		fail(outcome.FailReason)
		return nil, fmt.Errorf("routine run failed: %s", outcome.FailReason)
	}

	if err := finish(store.RoutineRunSucceeded, ""); err != nil {
		return nil, fmt.Errorf("record routine run success: %w", err)
	}

	return &FireResult{
		ID:          run.ID,
		RoutineID:   p.displayID,
		ReportPath:  reportAbs,
		AgentPreset: preset,
	}, nil
}

// fireWarnWriter is where addressing warnings (e.g. a shadowed Project routine)
// are printed. It falls back to discarding when no stdout is wired.
func fireWarnWriter(d *Deps) io.Writer {
	if d.Stdout != nil {
		return d.Stdout
	}
	return io.Discard
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
