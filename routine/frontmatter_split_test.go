package routine

import (
	"os"
	"path/filepath"
	"testing"
)

// newFrontmatterSplitRoutine scaffolds a routine and returns the deps plus its
// bound home directory.
func newFrontmatterSplitRoutine(t *testing.T, id string) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, id, "every 6h", home); err != nil {
		t.Fatal(err)
	}
	return d, home
}

// TestFingerprintSplitsFrontmatterFromBody proves the run-affecting fingerprint
// treats the frontmatter as settings and the body as prompt: a body-only edit
// and a settings-only edit each move the fingerprint, and they move it to
// distinct values (ADR-0139).
func TestFingerprintSplitsFrontmatterFromBody(t *testing.T) {
	d, _ := newFrontmatterSplitRoutine(t, "split")

	r, err := loadManifest(d, "split")
	if err != nil {
		t.Fatal(err)
	}
	base, err := Fingerprint(d, r)
	if err != nil {
		t.Fatal(err)
	}

	// Prompt-only edit: change the body, leave the frontmatter untouched.
	setRoutineBody(t, d, "split", "# Edited body\n\nDo something else.\n")
	rBody, err := loadManifest(d, "split")
	if err != nil {
		t.Fatal(err)
	}
	bodyFP, err := Fingerprint(d, rBody)
	if err != nil {
		t.Fatal(err)
	}
	if bodyFP == base {
		t.Fatal("a prompt-body edit must move the fingerprint")
	}

	// Restore the body, then make a settings-only edit (schedule via the
	// frontmatter chokepoint).
	setRoutineBody(t, d, "split", promptStub)
	if _, err := UpdateScheduleWith(d, "split", "daily at 09:30"); err != nil {
		t.Fatal(err)
	}
	rSettings, err := loadManifest(d, "split")
	if err != nil {
		t.Fatal(err)
	}
	settingsFP, err := Fingerprint(d, rSettings)
	if err != nil {
		t.Fatal(err)
	}
	if settingsFP == base {
		t.Fatal("a settings-only edit must move the fingerprint")
	}
	if settingsFP == bodyFP {
		t.Fatal("a settings-only edit and a prompt-only edit must fingerprint differently")
	}
}

// TestSettingsEditLeavesBodyIdentical proves a frontmatter-only edit
// (schedule, then agents/effort) rewrites the settings while preserving the
// prompt body below the fence byte-for-byte (ADR-0139).
func TestSettingsEditLeavesBodyIdentical(t *testing.T) {
	d, _ := newFrontmatterSplitRoutine(t, "keep-body")

	body := "# Triage\n\nReview open PRs and summarize blockers.\n\n- step one\n- step two\n"
	setRoutineBody(t, d, "keep-body", body)

	if _, err := UpdateScheduleWith(d, "keep-body", "daily at 09:30"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateRuntimeWith(d, "keep-body", []string{"claude"}, true, "heavy", true); err != nil {
		t.Fatal(err)
	}

	// The frontmatter now carries the edited settings...
	r, err := loadManifest(d, "keep-body")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Schedule != "daily at 09:30" || r.Manifest.Effort != "heavy" {
		t.Fatalf("settings not applied: %+v", r.Manifest)
	}
	// ...while the prompt body below the fence is byte-for-byte the original.
	_, gotBody, err := readPromptFrontmatter(d, routineDir(d, "keep-body"), "keep-body")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != body {
		t.Fatalf("body changed by settings edit:\n got %q\nwant %q", gotBody, body)
	}
}

// TestListWarnsInvalidFrontmatter proves unparseable YAML frontmatter suspends
// only that routine with a warning; healthy siblings still load (ADR-0139).
func TestListWarnsInvalidFrontmatter(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "bad-fm"} {
		if _, err := AddWith(d, id, "every 6h", home); err != nil {
			t.Fatal(err)
		}
	}
	// An unterminated / malformed YAML frontmatter block.
	badPath := filepath.Join(routineDir(d, "bad-fm"), promptFileName)
	if err := os.WriteFile(badPath, []byte("---\nagents: [unclosed\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	routines, warnings, err := ListRoutines(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(routines) != 1 || routines[0].ID != "alpha" {
		t.Fatalf("healthy routines = %v, want just alpha", routines)
	}
	if len(warnings) != 1 || warnings[0].ID != "bad-fm" {
		t.Fatalf("warnings = %v, want one for bad-fm", warnings)
	}
}

// TestListWarnsUnparseableSchedule proves a schedule the parser rejects — even
// with valid YAML around it — suspends only that routine with a warning, never
// treating it as silently unscheduled (ADR-0139).
func TestListWarnsUnparseableSchedule(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "bad-sched"} {
		if _, err := AddWith(d, id, "every 6h", home); err != nil {
			t.Fatal(err)
		}
	}
	// Valid YAML, but "every week" is not a schedule the parser accepts.
	badPath := filepath.Join(routineDir(d, "bad-sched"), promptFileName)
	if err := os.WriteFile(badPath, []byte("---\nschedule: every week\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	routines, warnings, err := ListRoutines(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(routines) != 1 || routines[0].ID != "alpha" {
		t.Fatalf("healthy routines = %v, want just alpha", routines)
	}
	if len(warnings) != 1 || warnings[0].ID != "bad-sched" {
		t.Fatalf("warnings = %v, want one for bad-sched", warnings)
	}
}

// TestLegacyManifestOnlyWarnsNotLoaded proves a directory carrying only the
// pre-ADR-0139 manifest.json is warned about, never loaded — its intent is not
// read (ADR-0139).
func TestLegacyManifestOnlyWarnsNotLoaded(t *testing.T) {
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
	// A leftover routine dir with only the legacy manifest.json, no state.json.
	legacyDir := routineDir(d, "legacy-only")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"bound_directory":"` + canonical(t, home) + `","schedule":"every 6h","paused":true}` + "\n"
	if err := os.WriteFile(filepath.Join(legacyDir, legacyManifestFileName), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	routines, warnings, err := ListRoutines(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(routines) != 1 || routines[0].ID != "alpha" {
		t.Fatalf("healthy routines = %v, want just alpha (legacy dir must not load)", routines)
	}
	if len(warnings) != 1 || warnings[0].ID != "legacy-only" {
		t.Fatalf("warnings = %v, want one for legacy-only", warnings)
	}
}

// TestLeftoverManifestIgnoredWhenStatePresent proves a healthy routine that also
// still carries a stray manifest.json loads fine from its state.json — the
// legacy file is never read for intent (ADR-0139).
func TestLeftoverManifestIgnoredWhenStatePresent(t *testing.T) {
	d, home := newFrontmatterSplitRoutine(t, "keep")
	// Drop a stray legacy manifest.json beside the valid state.json/prompt.md.
	stray := `{"bound_directory":"/somewhere/else","schedule":"daily at 03:00","paused":false}` + "\n"
	if err := os.WriteFile(filepath.Join(routineDir(d, "keep"), legacyManifestFileName), []byte(stray), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := loadManifest(d, "keep")
	if err != nil {
		t.Fatalf("routine with a stray manifest.json must still load: %v", err)
	}
	// Intent comes from the frontmatter/state, not the stray legacy file.
	if r.Manifest.Schedule != "every 6h" {
		t.Fatalf("schedule = %q, want every 6h (from frontmatter, not stray manifest)", r.Manifest.Schedule)
	}
	if r.Manifest.BoundDirectory != canonical(t, home) {
		t.Fatalf("bound dir = %q, want %q (from state.json, not stray manifest)", r.Manifest.BoundDirectory, canonical(t, home))
	}
}
