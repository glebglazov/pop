package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

func TestRunTaskSetQuotaPauseRegistersRecoveryWaiter(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installClaudeQuotaAgent(t, env.root)
	opts := env.runTaskSetOpts(true, "", nil)
	opts.AgentPreset = "claude"

	d := env.deps()
	
	// The new recovery flow parks the drain and registers a waiter instead of
	// immediately returning with QuotaPaused. Run the task set in a goroutine
	// and verify the recovery waiter is registered.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = RunTaskSetWith(d, nil, nil, opts)
	}()
	
	// Wait for the recovery waiter to be registered.
	// Poll the store a few times to give the goroutine time to register.
	var waiter *RecoveryWaiter
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		var err error
		waiter, err = GetRecoveryWaiter(d, "demo")
		if err != nil {
			t.Fatalf("get recovery waiter: %v", err)
		}
		if waiter != nil {
			break
		}
	}
	
	if waiter == nil {
		t.Fatal("recovery waiter not registered after 2 seconds")
	}
	if waiter.Preset != "claude" {
		t.Fatalf("waiter preset = %q, want claude", waiter.Preset)
	}
	if waiter.SetID != "demo" {
		t.Fatalf("waiter set_id = %q, want demo", waiter.SetID)
	}
	if waiter.RuntimePath == "" {
		t.Fatal("waiter runtime_path is empty")
	}
	if waiter.RegisteredAt.IsZero() {
		t.Fatal("waiter registered_at is zero")
	}
	
	// Clean up: deregister the waiter so the goroutine exits
	if err := DeregisterRecoveryWaiter(d, "demo"); err != nil {
		t.Fatalf("deregister recovery waiter: %v", err)
	}
	
	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine did not exit after waiter deregistration")
	}
}

func TestRunTaskSetCodexQuotaPauseRegistersWaiterWithResetAt(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installCodexQuotaAgent(t, env.root)
	opts := env.runTaskSetOpts(true, "", nil)
	opts.AgentPreset = "codex"

	d := env.deps()
	
	// Run the task set in a goroutine - it will enter the recovery wait loop
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = RunTaskSetWith(d, nil, nil, opts)
	}()
	
	// Wait for the recovery waiter to be registered
	var waiter *RecoveryWaiter
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		var err error
		waiter, err = GetRecoveryWaiter(d, "demo")
		if err != nil {
			t.Fatalf("get recovery waiter: %v", err)
		}
		if waiter != nil {
			break
		}
	}
	
	if waiter == nil {
		t.Fatal("recovery waiter not registered after 2 seconds")
	}
	if waiter.Preset != "codex" {
		t.Fatalf("waiter preset = %q, want codex", waiter.Preset)
	}
	if waiter.SetID != "demo" {
		t.Fatalf("waiter set_id = %q, want demo", waiter.SetID)
	}
	if waiter.ResetAt.IsZero() {
		t.Fatal("waiter reset_at is zero, expected codex reset time")
	}
	if waiter.RuntimePath == "" {
		t.Fatal("waiter runtime_path is empty")
	}
	
	// Clean up: deregister the waiter so the goroutine exits
	if err := DeregisterRecoveryWaiter(d, "demo"); err != nil {
		t.Fatalf("deregister recovery waiter: %v", err)
	}
	
	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine did not exit after waiter deregistration")
	}
}

func installCodexQuotaAgent(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"t\"}'\n" +
		"printf '%s\\n' '{\"type\":\"turn.started\"}'\n" +
		"printf '%s\\n' '{\"type\":\"error\",\"message\":\"You'\"'\"'ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 2:28 AM.\"}'\n" +
		"printf '%s\\n' '{\"type\":\"turn.failed\",\"error\":{\"message\":\"You'\"'\"'ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 2:28 AM.\"}}'\n" +
		"exit 1\n"
	writeFile(t, filepath.Join(dir, "codex"), script)
	if err := os.Chmod(filepath.Join(dir, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRunTaskSetDoneRecordsFinished proves the Drain records the process exit
// reason (finished), never the set's work disposition (ADR-0056): a set that
// drained to Done leaves a finished Drain, with done-ness read from the manifest.
func TestRunTaskSetDoneRecordsFinished(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	opts := env.runTaskSetOpts(true, agent, nil)

	d := env.deps()
	result, err := RunTaskSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %#v", result)
	}

	rec := latestTerminalDrain(t, d, result.RuntimePath)
	if rec == nil {
		t.Fatal("no terminal drain recorded")
	}
	if rec.State != store.StateFinished {
		t.Fatalf("state = %q, want finished", rec.State)
	}
	if rec.ExhaustedPreset != "" {
		t.Fatalf("finished terminal should carry no exhausted preset, got %q", rec.ExhaustedPreset)
	}
	if rec.SetID != "demo" {
		t.Fatalf("set id = %q, want demo", rec.SetID)
	}
	// finished is a clean stop, never an abnormal teardown (crashed).
	if rec.State == store.StateCrashed {
		t.Fatalf("finished must be a clean stop, got %q", rec.State)
	}
}

func TestDrainTerminal(t *testing.T) {
	resetAt := time.Date(2026, 6, 15, 2, 28, 0, 0, time.UTC)
	cases := []struct {
		name         string
		declined     bool
		quotaPaused  bool
		verifyFailed bool
		preset       string
		pinned       bool
		resetAt      time.Time
		err          error
		wantTerminal string
		wantPreset   string
		wantPinned   bool
		wantExecuted bool
		wantAbnorm   bool
	}{
		{
			name:         "quota pause carries preset and reset",
			quotaPaused:  true,
			preset:       "claude",
			pinned:       true,
			resetAt:      resetAt,
			wantTerminal: store.StateQuotaPaused,
			wantPreset:   "claude",
			wantPinned:   true,
			wantExecuted: true,
		},
		{
			name:         "done is a finished process",
			wantTerminal: store.StateFinished,
			wantExecuted: true,
		},
		{
			name:         "failure is a finished process",
			err:          exitErr(ExitOperational, "task failed"),
			wantTerminal: store.StateFinished,
			wantExecuted: true,
		},
		{
			name:         "no-runnable block is a finished process",
			err:          exitErr(ExitNoRunnable, "no eligible AFK task"),
			wantTerminal: store.StateFinished,
			wantExecuted: true,
		},
		{
			name:         "verify failure records verify_failed",
			verifyFailed: true,
			err:          exitErr(ExitNoRunnable, "verification failed"),
			wantTerminal: store.StateVerifyFailed,
			wantExecuted: true,
		},
		{
			name:         "interrupted is a clean stop",
			err:          exitErr(ExitInterrupted, "interrupted"),
			wantTerminal: store.StateInterrupted,
			wantExecuted: true,
			wantAbnorm:   false,
		},
		{
			name:         "declined never executed",
			declined:     true,
			wantExecuted: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terminal, preset, pinned, gotReset, executed := drainTerminal(tc.declined, tc.quotaPaused, tc.verifyFailed, tc.preset, tc.pinned, tc.resetAt, tc.err)
			if executed != tc.wantExecuted {
				t.Fatalf("executed = %v, want %v", executed, tc.wantExecuted)
			}
			if !executed {
				return
			}
			if terminal != tc.wantTerminal {
				t.Fatalf("terminal = %q, want %q", terminal, tc.wantTerminal)
			}
			if preset != tc.wantPreset {
				t.Fatalf("preset = %q, want %q", preset, tc.wantPreset)
			}
			if pinned != tc.wantPinned {
				t.Fatalf("pinned = %v, want %v", pinned, tc.wantPinned)
			}
			if tc.quotaPaused && !gotReset.Equal(tc.resetAt) {
				t.Fatalf("reset = %v, want %v", gotReset, tc.resetAt)
			}
			// Only crashed is abnormal (ADR-0120); interrupted is now a clean stop.
			gotAbnorm := terminal == store.StateCrashed
			if gotAbnorm != tc.wantAbnorm {
				t.Fatalf("abnormal = %v, want %v", gotAbnorm, tc.wantAbnorm)
			}
		})
	}
}

// TestReadTerminalDrainProjectsLatestTerminal round-trips a quota-paused terminal
// through the store: BeginDrain → Finish → read the latest terminal drain.
func TestReadTerminalDrainProjectsLatestTerminal(t *testing.T) {
	d, repo := drainTestRepo(t)
	resetAt := time.Date(2026, 6, 15, 2, 28, 0, 0, time.UTC)

	h, err := BeginDrain(d, repo, "demo", nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := h.Finish(store.StateQuotaPaused, "codex", true, resetAt); err != nil {
		t.Fatalf("finish: %v", err)
	}

	got := latestTerminalDrain(t, d, repo)
	if got == nil {
		t.Fatal("no terminal drain recorded")
	}
	if got.SetID != "demo" || got.State != store.StateQuotaPaused || got.ExhaustedPreset != "codex" || !got.ExhaustedPinned || !got.ExhaustedResetAt.Equal(resetAt) {
		t.Fatalf("terminal mismatch: %#v", got)
	}
	if got.FinishedAt.IsZero() {
		t.Fatalf("Finish should stamp the terminal time")
	}
}

func TestReadTerminalDrainMissingIsNil(t *testing.T) {
	d, repo := drainTestRepo(t)
	if got := latestTerminalDrain(t, d, repo); got != nil {
		t.Fatalf("got = %#v, want nil for a checkout with no terminal", got)
	}
}

func TestPrintHITLGateAdviceFraming(t *testing.T) {
	task := &Task{ID: "02-gate", File: "02-gate.md", Type: "HITL", Status: "open"}
	d := &Deps{FS: deps.NewRealFileSystem()}
	var buf strings.Builder
	printHITLGateAdvice(d, &buf, "demo", t.TempDir(), task)
	got := buf.String()
	if !strings.Contains(got, "Human-blocked") {
		t.Errorf("gating HITL advice missing 'Human-blocked': %s", got)
	}
	if strings.Contains(got, "Agents done") {
		t.Errorf("gating HITL advice must not say 'Agents done': %s", got)
	}
}

func TestPrintTerminalHITLAdviceFraming(t *testing.T) {
	task := &Task{ID: "03-verify", File: "03-verify.md", Type: "HITL", Status: "open"}
	d := &Deps{FS: deps.NewRealFileSystem()}
	var buf strings.Builder
	printTerminalHITLAdvice(d, &buf, "demo", t.TempDir(), task)
	got := buf.String()
	if !strings.Contains(got, "Agents done") {
		t.Errorf("terminal HITL advice missing 'Agents done': %s", got)
	}
	if strings.Contains(got, "Human-blocked") {
		t.Errorf("terminal HITL advice must not say 'Human-blocked': %s", got)
	}
}
