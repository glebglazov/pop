package routine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFingerprintOmitsUnsetFields proves that a future criterion, left unset,
// leaves an existing fingerprint unchanged — the safety net stays quiet until a
// human opts a routine into the new field.
func TestFingerprintOmitsUnsetFields(t *testing.T) {
	base := Manifest{Schedule: "every 6h"}
	got := fingerprintOf("do the thing", base)

	// Adding optional criteria while leaving them unset must not move the hash.
	// Simulate "a new version added agents/effort but this routine set neither"
	// by confirming the empty-valued manifest hashes identically.
	same := Manifest{Schedule: "every 6h", Agents: nil, Effort: ""}
	if fingerprintOf("do the thing", same) != got {
		t.Fatal("unset optional fields moved the fingerprint")
	}

	// An empty agent slice is treated as unset too (nonEmptyAgentSpecs prunes it).
	emptySlice := Manifest{Schedule: "every 6h", Agents: []string{"", "  "}}
	if fingerprintOf("do the thing", emptySlice) != got {
		t.Fatal("all-empty agent slice moved the fingerprint")
	}
}

// TestFingerprintMovesOnEverySetInput confirms each explicitly-set run-affecting
// input contributes to the hash.
func TestFingerprintMovesOnEverySetInput(t *testing.T) {
	base := Manifest{Schedule: "every 6h"}
	baseFP := fingerprintOf("prompt A", base)

	cases := map[string]struct {
		prompt string
		m      Manifest
	}{
		"prompt":   {"prompt B", base},
		"schedule": {"prompt A", Manifest{Schedule: "daily at 10:00"}},
		"agents":   {"prompt A", Manifest{Schedule: "every 6h", Agents: []string{"claude"}}},
		"effort":   {"prompt A", Manifest{Schedule: "every 6h", Effort: "heavy"}},
	}
	for name, c := range cases {
		if fingerprintOf(c.prompt, c.m) == baseFP {
			t.Fatalf("%s change did not move the fingerprint", name)
		}
	}
}

// TestFingerprintReadsPromptFromDisk checks the exported Fingerprint reads the
// routine's prompt.md and folds it in.
func TestFingerprintReadsPromptFromDisk(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "fp", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	r, err := loadManifest(d, "fp")
	if err != nil {
		t.Fatal(err)
	}
	before, err := Fingerprint(d, r)
	if err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(routineDir(d, "fp"), promptFileName)
	if err := os.WriteFile(promptPath, []byte("brand new prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Fingerprint(d, r)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("editing prompt.md must move the fingerprint")
	}
}

// TestUpdateScheduleWithPausesChanged verifies the schedule-edit chokepoint
// pauses with reason changed.
func TestUpdateScheduleWithPausesChanged(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "sched", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Start from resumed so the pause is attributable to the edit.
	if _, err := ResumeWith(d, "sched"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateScheduleWith(d, "sched", "daily at 09:30"); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, d, "sched")
	if !m.Paused || m.PauseReason != PauseReasonChanged {
		t.Fatalf("manifest = {paused:%v reason:%q}, want paused reason changed", m.Paused, m.PauseReason)
	}
}

// TestEditPromptPausesChanged verifies the prompt-edit chokepoint pauses with
// reason changed after the editor returns.
func TestEditPromptPausesChanged(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "prompt", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeWith(d, "prompt"); err != nil {
		t.Fatal(err)
	}
	d.IsInteractive = func() bool { return true }
	d.OpenEditor = func(string) error { return nil }
	if _, err := EditWith(d, "prompt", "", false); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, d, "prompt")
	if !m.Paused || m.PauseReason != PauseReasonChanged {
		t.Fatalf("manifest = {paused:%v reason:%q}, want paused reason changed", m.Paused, m.PauseReason)
	}
}
