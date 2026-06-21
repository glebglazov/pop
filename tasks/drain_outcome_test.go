package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// readOnlyDrainOutcome reads the single drain-outcome record written under the
// fixture's data dir, keyed lookup sidestepped so a canonicalized runtime path
// (/var vs /private/var on macOS) cannot cause a spurious miss.
func readOnlyDrainOutcome(t *testing.T, d *Deps) *DrainOutcomeRecord {
	t.Helper()
	dir := DrainOutcomeDirWith(d)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read drain-outcome dir: %v", err)
	}
	var records []*DrainOutcomeRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var rec DrainOutcomeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatal(err)
		}
		records = append(records, &rec)
	}
	if len(records) != 1 {
		t.Fatalf("drain-outcome records = %d, want 1", len(records))
	}
	return records[0]
}

func TestRunTaskSetQuotaPauseWritesDrainOutcome(t *testing.T) {
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

	rec := readOnlyDrainOutcome(t, d)
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
		t.Fatalf("record missing fields: %#v", rec)
	}
	if rec.Outcome.Abnormal() {
		t.Fatalf("quota pause must be a clean stop, got abnormal")
	}
}

func TestRunTaskSetCodexQuotaPauseWritesResetAt(t *testing.T) {
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

	rec := readOnlyDrainOutcome(t, d)
	if rec.Outcome != DrainOutcomeQuotaPaused || rec.ExhaustedPreset != "codex" || rec.ExhaustedResetAt.IsZero() {
		t.Fatalf("record missing codex reset: %#v", rec)
	}
	if !rec.ExhaustedResetAt.Equal(result.PauseResetAt) {
		t.Fatalf("record reset = %s, result reset = %s", rec.ExhaustedResetAt, result.PauseResetAt)
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

func TestRunTaskSetDoneWritesDrainOutcome(t *testing.T) {
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

	rec := readOnlyDrainOutcome(t, d)
	if rec.Outcome != DrainOutcomeDone {
		t.Fatalf("outcome = %q, want done", rec.Outcome)
	}
	if rec.ExhaustedPreset != "" {
		t.Fatalf("done record should carry no exhausted preset, got %q", rec.ExhaustedPreset)
	}
	if rec.SetID != "demo" {
		t.Fatalf("set id = %q, want demo", rec.SetID)
	}
	if rec.Outcome.Abnormal() {
		t.Fatalf("done must be a clean stop, got abnormal")
	}
}

func TestClassifyDrainOutcome(t *testing.T) {
	cases := []struct {
		name        string
		result      *RunTaskSetResult
		err         error
		wantOutcome DrainOutcome
		wantPreset  string
		wantPinned  bool
		wantWrite   bool
		wantAbnorm  bool
	}{
		{
			name:        "quota pause carries preset",
			result:      &RunTaskSetResult{QuotaPaused: true, PausePreset: "claude", PausePinnedAgent: true},
			wantOutcome: DrainOutcomeQuotaPaused,
			wantPreset:  "claude",
			wantPinned:  true,
			wantWrite:   true,
		},
		{
			name:        "done",
			result:      &RunTaskSetResult{TaskSetDone: true},
			wantOutcome: DrainOutcomeDone,
			wantWrite:   true,
		},
		{
			name:        "deferred",
			result:      &RunTaskSetResult{TaskSetDeferred: true},
			wantOutcome: DrainOutcomeDeferred,
			wantWrite:   true,
		},
		{
			name:        "blocked via reason",
			result:      &RunTaskSetResult{BlockedReason: "needs human"},
			wantOutcome: DrainOutcomeBlocked,
			wantWrite:   true,
		},
		{
			name:        "blocked via no-runnable error",
			err:         exitErr(ExitNoRunnable, "no eligible AFK task"),
			wantOutcome: DrainOutcomeBlocked,
			wantWrite:   true,
		},
		{
			name:        "unverified — terminal HITL, no open AFK work",
			result:      &RunTaskSetResult{TaskSetUnverified: true, BlockedReason: "HITL: 03-verify"},
			err:         exitErr(ExitNoRunnable, "agents done — verify"),
			wantOutcome: DrainOutcomeUnverified,
			wantWrite:   true,
		},
		{
			name:        "failed via operational error",
			err:         exitErr(ExitOperational, "task failed"),
			wantOutcome: DrainOutcomeFailed,
			wantWrite:   true,
		},
		{
			name:        "interrupted is abnormal",
			result:      &RunTaskSetResult{},
			err:         exitErr(ExitInterrupted, "interrupted"),
			wantOutcome: DrainOutcomeInterrupted,
			wantWrite:   true,
			wantAbnorm:  true,
		},
		{
			name:      "declined writes nothing",
			result:    &RunTaskSetResult{Declined: true},
			wantWrite: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, preset, pinned, ok := classifyDrainOutcome(tc.result, tc.err)
			if ok != tc.wantWrite {
				t.Fatalf("write = %v, want %v", ok, tc.wantWrite)
			}
			if !ok {
				return
			}
			if outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", outcome, tc.wantOutcome)
			}
			if preset != tc.wantPreset {
				t.Fatalf("preset = %q, want %q", preset, tc.wantPreset)
			}
			if pinned != tc.wantPinned {
				t.Fatalf("pinned = %v, want %v", pinned, tc.wantPinned)
			}
			if outcome.Abnormal() != tc.wantAbnorm {
				t.Fatalf("abnormal = %v, want %v", outcome.Abnormal(), tc.wantAbnorm)
			}
		})
	}
}

func TestDrainOutcomeUnverifiedIsClean(t *testing.T) {
	if DrainOutcomeUnverified.Abnormal() {
		t.Fatal("DrainOutcomeUnverified must be a clean (non-abnormal) terminal stop")
	}
}

func TestWriteReadDrainOutcomeUnverifiedRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	d := &Deps{FS: deps.NewRealFileSystem()}

	want := DrainOutcomeRecord{
		SetID:       "demo",
		Outcome:     DrainOutcomeUnverified,
		RuntimePath: "/some/checkout",
	}
	if err := WriteDrainOutcome(d, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadDrainOutcome(d, "/some/checkout")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Outcome != DrainOutcomeUnverified {
		t.Fatalf("outcome = %q, want unverified", got.Outcome)
	}
	if got.Outcome.Abnormal() {
		t.Fatal("round-tripped unverified must still be clean")
	}
}

func TestWriteReadDrainOutcomeRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	d := &Deps{FS: deps.NewRealFileSystem()}
	resetAt := time.Date(2026, 6, 15, 2, 28, 0, 0, time.UTC)

	want := DrainOutcomeRecord{
		SetID:            "demo",
		Outcome:          DrainOutcomeQuotaPaused,
		ExhaustedPreset:  "codex",
		ExhaustedPinned:  true,
		ExhaustedResetAt: resetAt,
		RuntimePath:      "/some/checkout",
	}
	if err := WriteDrainOutcome(d, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadDrainOutcome(d, "/some/checkout")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SetID != want.SetID || got.Outcome != want.Outcome || got.ExhaustedPreset != want.ExhaustedPreset || got.ExhaustedPinned != want.ExhaustedPinned || !got.ExhaustedResetAt.Equal(want.ExhaustedResetAt) || got.RuntimePath != want.RuntimePath {
		t.Fatalf("round-trip mismatch: got %#v want %#v", got, want)
	}
	if got.WrittenAt.IsZero() {
		t.Fatalf("WriteDrainOutcome should stamp WrittenAt")
	}
}

func TestWriteDrainOutcomeOmitsZeroExhaustedResetAt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	d := &Deps{FS: deps.NewRealFileSystem()}

	rec := DrainOutcomeRecord{
		SetID:           "demo",
		Outcome:         DrainOutcomeQuotaPaused,
		ExhaustedPreset: "claude",
		RuntimePath:     "/some/checkout",
	}
	if err := WriteDrainOutcome(d, rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(DrainOutcomePathFor(d, rec.RuntimePath))
	if err != nil {
		t.Fatalf("read raw record: %v", err)
	}
	if strings.Contains(string(data), "exhausted_reset_at") {
		t.Fatalf("zero reset should be omitted from JSON: %s", data)
	}
}

func TestRuntimeLockMetadataSetIDOptional(t *testing.T) {
	// Round-trip: a lock written with a set id reads it back.
	withSet := RuntimeLockMetadata{
		PID:         1234,
		RuntimePath: "/checkout",
		StartedAt:   time.Now().UTC().Truncate(time.Second),
		SetID:       "demo",
	}
	data, err := json.Marshal(withSet)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseRuntimeLockMetadata(data)
	if err != nil {
		t.Fatalf("parse with set id: %v", err)
	}
	if meta.SetID != "demo" {
		t.Fatalf("set id = %q, want demo", meta.SetID)
	}

	// An older lock file without set_id must still parse (optional field).
	legacy := []byte(`{"pid":1234,"runtime_path":"/checkout","started_at":"2026-06-14T00:00:00Z"}`)
	legacyMeta, err := parseRuntimeLockMetadata(legacy)
	if err != nil {
		t.Fatalf("parse legacy lock: %v", err)
	}
	if legacyMeta.SetID != "" {
		t.Fatalf("legacy set id = %q, want empty", legacyMeta.SetID)
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

func TestAcquireRuntimeLockForSetRecordsSetID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	d := &Deps{FS: deps.NewRealFileSystem()}
	runtimeRoot := filepath.Join(root, "checkout")

	lock, err := AcquireRuntimeLockForSet(d, runtimeRoot, "demo", nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lock.Release()

	status := ReadRuntimeLockStatus(d, runtimeRoot)
	if status.Metadata == nil {
		t.Fatalf("missing metadata: %#v", status)
	}
	if status.Metadata.SetID != "demo" {
		t.Fatalf("set id = %q, want demo", status.Metadata.SetID)
	}
}
