package routine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// seedLegacyRoutine writes a pre-ADR-0139 routine directory directly: a
// manifest.json carrying intent + state, and a plain prompt.md with no
// frontmatter — the exact shape the migration must handle.
func seedLegacyRoutine(t *testing.T, d *Deps, id string, lm legacyManifest, promptBody string) {
	t.Helper()
	dir := routineDir(d, id)
	if err := os.MkdirAll(filepath.Join(dir, memoryDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, runsDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, promptFileName), []byte(promptBody), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(lm, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyManifestFileName), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateManifestsMigratesLegacyDir(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	lm := legacyManifest{
		BoundDirectory: filepath.Join(root, "home"),
		Schedule:       "every 6h",
		Agents:         []string{"claude", "codex"},
		Effort:         "heavy",
		Paused:         true,
		PauseReason:    PauseReasonManual,
		CreatedAt:      "2026-01-01T00:00:00Z",
	}
	seedLegacyRoutine(t, d, "legacy-one", lm, "# Prompt\n\nDo the thing.\n")

	var out bytes.Buffer
	if err := MigrateManifestsWith(d, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `migrated routine "legacy-one"`) {
		t.Fatalf("output = %q", out.String())
	}

	dir := routineDir(d, "legacy-one")
	if _, err := os.Stat(filepath.Join(dir, legacyManifestFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected manifest.json removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); err != nil {
		t.Fatalf("expected state.json written: %v", err)
	}

	got := readManifest(t, d, "legacy-one")
	if got.BoundDirectory != lm.BoundDirectory {
		t.Fatalf("BoundDirectory = %q, want %q", got.BoundDirectory, lm.BoundDirectory)
	}
	if got.Schedule != lm.Schedule {
		t.Fatalf("Schedule = %q, want %q", got.Schedule, lm.Schedule)
	}
	if strings.Join(got.Agents, ",") != strings.Join(lm.Agents, ",") {
		t.Fatalf("Agents = %v, want %v", got.Agents, lm.Agents)
	}
	if got.Effort != lm.Effort {
		t.Fatalf("Effort = %q, want %q", got.Effort, lm.Effort)
	}
	if got.Paused != lm.Paused {
		t.Fatalf("Paused = %v, want %v", got.Paused, lm.Paused)
	}
	if got.PauseReason != lm.PauseReason {
		t.Fatalf("PauseReason = %q, want %q", got.PauseReason, lm.PauseReason)
	}
	if got.CreatedAt != lm.CreatedAt {
		t.Fatalf("CreatedAt = %q, want %q", got.CreatedAt, lm.CreatedAt)
	}

	promptData, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(promptData), "---\n") {
		t.Fatalf("prompt.md missing frontmatter fence:\n%s", promptData)
	}
	if !strings.HasSuffix(string(promptData), "# Prompt\n\nDo the thing.\n") {
		t.Fatalf("prompt.md body not preserved:\n%s", promptData)
	}
}

func TestMigrateManifestsIdempotent(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	lm := legacyManifest{
		BoundDirectory: filepath.Join(root, "home"),
		Schedule:       "every 6h",
		CreatedAt:      "2026-01-01T00:00:00Z",
	}
	seedLegacyRoutine(t, d, "legacy-two", lm, "prompt body\n")

	var out1 bytes.Buffer
	if err := MigrateManifestsWith(d, &out1); err != nil {
		t.Fatal(err)
	}
	dir := routineDir(d, "legacy-two")
	promptAfterFirst, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		t.Fatal(err)
	}
	stateAfterFirst, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}

	var out2 bytes.Buffer
	if err := MigrateManifestsWith(d, &out2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out2.String(), "migrated routine") {
		t.Fatalf("second run should be a no-op, got: %q", out2.String())
	}
	if !strings.Contains(out2.String(), "nothing to migrate") {
		t.Fatalf("second run output = %q", out2.String())
	}

	promptAfterSecond, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		t.Fatal(err)
	}
	stateAfterSecond, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(promptAfterFirst, promptAfterSecond) {
		t.Fatalf("prompt.md changed on second run:\nfirst:\n%s\nsecond:\n%s", promptAfterFirst, promptAfterSecond)
	}
	if !bytes.Equal(stateAfterFirst, stateAfterSecond) {
		t.Fatalf("state.json changed on second run:\nfirst:\n%s\nsecond:\n%s", stateAfterFirst, stateAfterSecond)
	}
}

func TestMigrateManifestsAlreadyMigratedDirUntouched(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "fresh", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	dir := routineDir(d, "fresh")
	promptBefore, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := MigrateManifestsWith(d, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "fresh") {
		t.Fatalf("already-new-format routine should not be touched: %q", out.String())
	}

	promptAfter, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		t.Fatal(err)
	}
	stateAfter, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(promptBefore, promptAfter) {
		t.Fatal("prompt.md of already-migrated routine changed")
	}
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("state.json of already-migrated routine changed")
	}
}

func TestMigrateManifestsUnparseableManifestReportedAndSkipped(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	dir := routineDir(d, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	promptBody := "prompt body\n"
	if err := os.WriteFile(filepath.Join(dir, promptFileName), []byte(promptBody), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestBody := "{not valid json"
	if err := os.WriteFile(filepath.Join(dir, legacyManifestFileName), []byte(manifestBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := MigrateManifestsWith(d, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `warning: routine "broken"`) {
		t.Fatalf("expected warning in output, got %q", out.String())
	}

	// Never half-written: manifest.json stays, no state.json appears, prompt.md
	// is untouched.
	gotManifest, err := os.ReadFile(filepath.Join(dir, legacyManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotManifest) != manifestBody {
		t.Fatalf("manifest.json was modified: %q", gotManifest)
	}
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no state.json written, stat err = %v", err)
	}
	gotPrompt, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPrompt) != promptBody {
		t.Fatalf("prompt.md was modified: %q", gotPrompt)
	}
}

func TestMigrateManifestsUnparseableScheduleReportedAndSkipped(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	lm := legacyManifest{
		BoundDirectory: filepath.Join(root, "home"),
		Schedule:       "every week",
		CreatedAt:      "2026-01-01T00:00:00Z",
	}
	seedLegacyRoutine(t, d, "bad-schedule", lm, "prompt body\n")

	var out bytes.Buffer
	if err := MigrateManifestsWith(d, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `warning: routine "bad-schedule"`) {
		t.Fatalf("expected warning in output, got %q", out.String())
	}
	dir := routineDir(d, "bad-schedule")
	if _, err := os.Stat(filepath.Join(dir, legacyManifestFileName)); err != nil {
		t.Fatalf("expected manifest.json left in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no state.json written, stat err = %v", err)
	}
}

// TestMigrateManifestsRoundTrip verifies that a routine migrated from the
// legacy manifest.json format lists, edits, and fires identically to a freshly
// created (already-new-format) routine.
func TestMigrateManifestsRoundTrip(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)

	boundDir := canonical(t, home)
	lm := legacyManifest{
		BoundDirectory: boundDir,
		Schedule:       "every 6h",
		Paused:         false,
		CreatedAt:      "2026-01-01T00:00:00Z",
	}
	seedLegacyRoutine(t, d, "legacy-roundtrip", lm, "Assess the service.")

	var out bytes.Buffer
	if err := MigrateManifestsWith(d, &out); err != nil {
		t.Fatal(err)
	}

	// list: the migrated routine shows up like any other.
	var listOut bytes.Buffer
	if err := ListWith(d, &listOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), "legacy-roundtrip") {
		t.Fatalf("list output missing migrated routine:\n%s", listOut.String())
	}
	if !strings.Contains(listOut.String(), "every 6h") {
		t.Fatalf("list output missing schedule:\n%s", listOut.String())
	}

	// edit: schedule update works exactly as on a freshly created routine.
	updated, err := UpdateScheduleWith(d, "legacy-roundtrip", "daily at 09:00")
	if err != nil {
		t.Fatalf("edit migrated routine: %v", err)
	}
	if updated.Schedule != "daily at 09:00" {
		t.Fatalf("Schedule = %q, want %q", updated.Schedule, "daily at 09:00")
	}

	// fire: runs to completion and produces a report, same as a fresh routine.
	res, err := FireWith(d, "legacy-roundtrip")
	if err != nil {
		t.Fatalf("fire migrated routine: %v", err)
	}
	if res.ReportPath == "" {
		t.Fatal("expected report path")
	}
	if _, err := os.Stat(res.ReportPath); err != nil {
		t.Fatalf("report file: %v", err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.LastRoutineRun("legacy-roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunSucceeded {
		t.Fatalf("row = %+v", row)
	}
}
