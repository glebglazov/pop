package routine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/tasks"
)

func routineDeps(t *testing.T, dataHome string) *Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dataHome)
	d := &Deps{
		FS:            deps.NewRealFileSystem(),
		IsInteractive: func() bool { return false },
		Now: func() time.Time {
			ts, err := time.Parse(timeRFC3339, "2026-07-18T12:00:00Z")
			if err != nil {
				t.Fatal(err)
			}
			return ts
		},
		LoadConfig:     func() (*config.Config, error) { return &config.Config{}, nil },
		Tasks:          tasks.DefaultDeps(),
		PID:            func() int { return 4242 },
		ProcStartToken: func(pid int) (string, bool) { return "test", true },
		ProcessAlive:   func(pid int, procStart string) bool { return processAlivePID(pid) },
	}
	// Borrowers never close the process-cached store handle (ADR-0140); close it
	// once at test end through the accessor's closer rather than per call.
	t.Cleanup(func() {
		if d.Tasks != nil {
			_ = d.Tasks.CloseStore()
		}
	})
	return d
}

func TestAddScaffoldsRoutineFromNonGitDirectory(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)

	res, err := AddWith(d, "daily-report", "daily at 10:00", home)
	if err != nil {
		t.Fatal(err)
	}

	wantDir := filepath.Join(dataHome, "pop", "routines", "daily-report")
	if res.Dir != wantDir {
		t.Fatalf("Dir = %q, want %q", res.Dir, wantDir)
	}
	wantBound := canonical(t, home)
	if res.Manifest.BoundDirectory != wantBound {
		t.Fatalf("BoundDirectory = %q, want %q", res.Manifest.BoundDirectory, wantBound)
	}
	if res.Manifest.Schedule != "daily at 10:00" {
		t.Fatalf("Schedule = %q", res.Manifest.Schedule)
	}
	if !res.Manifest.Paused {
		t.Fatal("expected paused=true (routines are created paused)")
	}

	// The scaffold splits the manifest by ownership (ADR-0139): intent into
	// prompt.md frontmatter, machine state into state.json — and never writes a
	// manifest.json.
	for _, rel := range []string{
		"state.json",
		"prompt.md",
		"memory",
		"runs",
	} {
		path := filepath.Join(wantDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(wantDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("scaffold must not create manifest.json, stat err = %v", err)
	}

	// state.json holds the bound directory (a registry fact), the schedule rides
	// in prompt.md frontmatter — readManifest reassembles both halves.
	loaded := readManifest(t, d, "daily-report")
	if loaded.BoundDirectory != wantBound {
		t.Fatalf("loaded bound dir = %q", loaded.BoundDirectory)
	}
	if loaded.Schedule != "daily at 10:00" {
		t.Fatalf("loaded schedule = %q", loaded.Schedule)
	}

	// The schedule is genuinely persisted in the prompt.md frontmatter fence.
	promptData, err := os.ReadFile(filepath.Join(wantDir, "prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(promptData), "---\n") {
		t.Fatalf("prompt.md missing frontmatter fence:\n%s", promptData)
	}
	if !strings.Contains(string(promptData), "schedule: daily at 10:00") {
		t.Fatalf("prompt.md frontmatter missing schedule:\n%s", promptData)
	}
}

func TestAddScaffoldsUnscheduledRoutine(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)

	res, err := AddWith(d, "manual-only", "", home)
	if err != nil {
		t.Fatalf("creating without a schedule should succeed: %v", err)
	}
	if res.Manifest.Schedule != "" {
		t.Fatalf("Schedule = %q, want empty", res.Manifest.Schedule)
	}
	if res.Manifest.IsScheduled() {
		t.Fatal("IsScheduled() = true, want false for an unscheduled routine")
	}

	// The manifest persists and reloads with no schedule and no parser error.
	r, err := loadManifest(d, "manual-only")
	if err != nil {
		t.Fatalf("reload of an unscheduled routine should not error: %v", err)
	}
	if r.Manifest.Schedule != "" {
		t.Fatalf("reloaded schedule = %q, want empty", r.Manifest.Schedule)
	}
	if r.Manifest.IsScheduled() {
		t.Fatal("reloaded IsScheduled() = true, want false")
	}
	if ScheduleLabel(r.Manifest.Schedule) != "manual" {
		t.Fatalf("ScheduleLabel = %q, want %q", ScheduleLabel(r.Manifest.Schedule), "manual")
	}
}

// setRoutineBody replaces a routine's prompt.md body while preserving its
// frontmatter, mirroring an agent (or human) editing the prompt below the fence
// without disturbing the authored intent (ADR-0139).
func setRoutineBody(t *testing.T, d *Deps, id, body string) {
	t.Helper()
	promptPath := filepath.Join(routineDir(d, id), promptFileName)
	data, err := d.FS.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	fields, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := frontmatter.Marshal(fields, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.FS.WriteFile(promptPath, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}

func TestAddRejectsInvalidSchedule(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)

	_, err := AddWith(d, "bad-schedule", "every week", home)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid schedule") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "pop", "routines", "bad-schedule")); !os.IsNotExist(err) {
		t.Fatalf("expected no scaffold on invalid schedule, stat err = %v", err)
	}
}

func TestAddRejectsDuplicateID(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)

	if _, err := AddWith(d, "dup", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, "dup", "every 2h", home); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestAddDoesNotOpenEditor(t *testing.T) {
	// Scaffolding no longer opens $EDITOR: the interactive editing entry point is
	// the refinement gate (RefineWith), which AddWith deliberately does not run.
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	editorCalled := false
	d.IsInteractive = func() bool { return true }
	d.OpenEditor = func(path string) error {
		editorCalled = true
		return nil
	}

	if _, err := AddWith(d, "edit-me", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if editorCalled {
		t.Fatal("AddWith must not open an editor; the refinement gate handles editing")
	}
}

func TestListRoutinesEmptyHint(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)
	var out bytes.Buffer
	if err := ListWith(d, &out); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != emptyListHint {
		t.Fatalf("got %q", got)
	}
}

func TestListRoutinesShowsFields(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, "beta", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := ListWith(d, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	wantBound := canonical(t, home)
	for _, want := range []string{
		"ID",
		"DIRECTORY",
		"SCHEDULE",
		"PAUSED",
		"alpha",
		wantBound,
		"every 6h",
		"beta",
		"daily at 10:00",
		"yes",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
