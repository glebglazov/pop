package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRunTaskSetQuotaPauseRecordsTerminal(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installClaudeQuotaAgent(t, env.root)
	opts := env.runTaskSetOpts(true, "", nil)
	opts.AgentPreset = "claude"

	d := env.deps()
	result, err := RunTaskSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused {
		t.Fatalf("result = %#v", result)
	}

	rec, err := ReadDrainOutcome(d, result.RuntimePath)
	if err != nil {
		t.Fatalf("read terminal: %v", err)
	}
	if rec.Outcome != DrainOutcomeQuotaPaused {
		t.Fatalf("outcome = %q, want quota_paused", rec.Outcome)
	}
	if rec.ExhaustedPreset != "claude" {
		t.Fatalf("exhausted preset = %q, want claude", rec.ExhaustedPreset)
	}
	if rec.SetID != "demo" {
		t.Fatalf("set id = %q, want demo", rec.SetID)
	}
	if rec.RuntimePath == "" || rec.PID == 0 || rec.WrittenAt.IsZero() {
		t.Fatalf("terminal missing fields: %#v", rec)
	}
	if rec.Outcome.Abnormal() {
		t.Fatalf("quota pause must be a clean stop, got abnormal")
	}
}

func TestRunTaskSetCodexQuotaPauseRecordsResetAt(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installCodexQuotaAgent(t, env.root)
	opts := env.runTaskSetOpts(true, "", nil)
	opts.AgentPreset = "codex"

	d := env.deps()
	result, err := RunTaskSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || result.PauseResetAt.IsZero() {
		t.Fatalf("result = %#v", result)
	}

	rec, err := ReadDrainOutcome(d, result.RuntimePath)
	if err != nil {
		t.Fatalf("read terminal: %v", err)
	}
	if rec.Outcome != DrainOutcomeQuotaPaused || rec.ExhaustedPreset != "codex" || rec.ExhaustedResetAt.IsZero() {
		t.Fatalf("terminal missing codex reset: %#v", rec)
	}
	if !rec.ExhaustedResetAt.Equal(result.PauseResetAt) {
		t.Fatalf("terminal reset = %s, result reset = %s", rec.ExhaustedResetAt, result.PauseResetAt)
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

	rec, err := ReadDrainOutcome(d, result.RuntimePath)
	if err != nil {
		t.Fatalf("read terminal: %v", err)
	}
	if rec.Outcome != DrainOutcomeFinished {
		t.Fatalf("outcome = %q, want finished", rec.Outcome)
	}
	if rec.ExhaustedPreset != "" {
		t.Fatalf("finished terminal should carry no exhausted preset, got %q", rec.ExhaustedPreset)
	}
	if rec.SetID != "demo" {
		t.Fatalf("set id = %q, want demo", rec.SetID)
	}
	if rec.Outcome.Abnormal() {
		t.Fatalf("finished must be a clean stop, got abnormal")
	}
}

func TestDrainTerminal(t *testing.T) {
	resetAt := time.Date(2026, 6, 15, 2, 28, 0, 0, time.UTC)
	cases := []struct {
		name         string
		declined     bool
		quotaPaused  bool
		preset       string
		pinned       bool
		resetAt      time.Time
		err          error
		wantTerminal DrainOutcome
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
			wantTerminal: DrainOutcomeQuotaPaused,
			wantPreset:   "claude",
			wantPinned:   true,
			wantExecuted: true,
		},
		{
			name:         "done is a finished process",
			wantTerminal: DrainOutcomeFinished,
			wantExecuted: true,
		},
		{
			name:         "failure is a finished process",
			err:          exitErr(ExitOperational, "task failed"),
			wantTerminal: DrainOutcomeFinished,
			wantExecuted: true,
		},
		{
			name:         "no-runnable block is a finished process",
			err:          exitErr(ExitNoRunnable, "no eligible AFK task"),
			wantTerminal: DrainOutcomeFinished,
			wantExecuted: true,
		},
		{
			name:         "interrupted is abnormal",
			err:          exitErr(ExitInterrupted, "interrupted"),
			wantTerminal: DrainOutcomeInterrupted,
			wantExecuted: true,
			wantAbnorm:   true,
		},
		{
			name:         "declined never executed",
			declined:     true,
			wantExecuted: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terminal, preset, pinned, gotReset, executed := drainTerminal(tc.declined, tc.quotaPaused, tc.preset, tc.pinned, tc.resetAt, tc.err)
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
			if terminal.Abnormal() != tc.wantAbnorm {
				t.Fatalf("abnormal = %v, want %v", terminal.Abnormal(), tc.wantAbnorm)
			}
		})
	}
}

func TestDrainOutcomeAwaitingApprovalIsClean(t *testing.T) {
	if DrainOutcomeAwaitingApproval.Abnormal() {
		t.Fatal("DrainOutcomeAwaitingApproval must be a clean (non-abnormal) terminal stop")
	}
}

// TestReadDrainOutcomeProjectsLatestTerminal round-trips a quota-paused terminal
// through the store: BeginDrain → Finish → ReadDrainOutcome.
func TestReadDrainOutcomeProjectsLatestTerminal(t *testing.T) {
	d, repo := drainTestRepo(t)
	resetAt := time.Date(2026, 6, 15, 2, 28, 0, 0, time.UTC)

	h, err := BeginDrain(d, repo, "demo", nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := h.Finish(DrainOutcomeQuotaPaused, "codex", true, resetAt); err != nil {
		t.Fatalf("finish: %v", err)
	}

	got, err := ReadDrainOutcome(d, repo)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SetID != "demo" || got.Outcome != DrainOutcomeQuotaPaused || got.ExhaustedPreset != "codex" || !got.ExhaustedPinned || !got.ExhaustedResetAt.Equal(resetAt) {
		t.Fatalf("terminal mismatch: %#v", got)
	}
	if got.WrittenAt.IsZero() {
		t.Fatalf("Finish should stamp the terminal time")
	}
}

// TestDrainOutcomeVocabularyValues locks the durable string spellings retired to
// (and added by) ADR-0087: the terminal-HITL disposition persists as
// "awaiting_approval", and verify_failed is a recognized value.
func TestDrainOutcomeVocabularyValues(t *testing.T) {
	if DrainOutcomeAwaitingApproval != "awaiting_approval" {
		t.Fatalf("DrainOutcomeAwaitingApproval = %q, want awaiting_approval", DrainOutcomeAwaitingApproval)
	}
	if DrainOutcomeVerifyFailed != "verify_failed" {
		t.Fatalf("DrainOutcomeVerifyFailed = %q, want verify_failed", DrainOutcomeVerifyFailed)
	}
	// verify_failed is a clean terminal (the drain finished; the Verifier just
	// could not clear the set), not an abnormal teardown.
	if DrainOutcomeVerifyFailed.Abnormal() {
		t.Fatal("DrainOutcomeVerifyFailed must be a clean (non-abnormal) terminal stop")
	}
}

// TestReadDrainOutcomeReadsLegacyUnverifiedForward verifies the durable,
// append-only guarantee of ADR-0087: a record written with the pre-rename
// "unverified" value stays on disk verbatim and reads forward to
// DrainOutcomeAwaitingApproval.
func TestReadDrainOutcomeReadsLegacyUnverifiedForward(t *testing.T) {
	d, repo := drainTestRepo(t)

	h, err := BeginDrain(d, repo, "demo", nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := h.Finish(drainOutcomeLegacyUnverified, "", false, time.Time{}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	got, err := ReadDrainOutcome(d, repo)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Outcome != DrainOutcomeAwaitingApproval {
		t.Fatalf("Outcome = %q, want %q (legacy unverified must read forward)", got.Outcome, DrainOutcomeAwaitingApproval)
	}
}

func TestReadDrainOutcomeMissingIsNotExist(t *testing.T) {
	d, repo := drainTestRepo(t)
	if _, err := ReadDrainOutcome(d, repo); !os.IsNotExist(err) {
		t.Fatalf("err = %v, want os.ErrNotExist for a checkout with no terminal", err)
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
