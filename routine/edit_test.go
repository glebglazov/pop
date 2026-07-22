package routine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditOpensEditorWhenInteractive(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "edit-me", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	var opened string
	d.IsInteractive = func() bool { return true }
	d.OpenEditor = func(path string) error {
		opened = path
		return nil
	}

	res, err := EditWith(d, "edit-me", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataHome, "pop", "routines", "edit-me", "prompt.md")
	if opened != want {
		t.Fatalf("opened = %q, want %q", opened, want)
	}
	if !res.Opened || res.PromptPath != want {
		t.Fatalf("result = %+v", res)
	}
	if res.ScheduleUpdated {
		t.Fatal("plain edit should not update schedule")
	}
}

func TestEditNonInteractiveErrorsWithPromptPath(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome) // IsInteractive returns false
	if _, err := AddWith(d, "edit-me", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	editorCalled := false
	d.OpenEditor = func(path string) error {
		editorCalled = true
		return nil
	}

	_, err := EditWith(d, "edit-me", "", false)
	if err == nil {
		t.Fatal("expected error on non-interactive plain edit")
	}
	promptPath := filepath.Join(dataHome, "pop", "routines", "edit-me", "prompt.md")
	if !strings.Contains(err.Error(), promptPath) {
		t.Fatalf("error should name prompt path %q, got %v", promptPath, err)
	}
	if editorCalled {
		t.Fatal("editor must not open in a non-interactive session")
	}
}

func TestEditWritesSchedule(t *testing.T) {
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

	editorCalled := false
	d.IsInteractive = func() bool { return true }
	d.OpenEditor = func(path string) error {
		editorCalled = true
		return nil
	}

	res, err := EditWith(d, "sched", "daily at 09:30", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ScheduleUpdated || res.Schedule != "daily at 09:30" {
		t.Fatalf("result = %+v", res)
	}
	if editorCalled {
		t.Fatal("schedule edit must not open an editor")
	}

	manifestPath := filepath.Join(dataHome, "pop", "routines", "sched", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Schedule != "daily at 09:30" {
		t.Fatalf("persisted schedule = %q", m.Schedule)
	}
	if m.BoundDirectory == "" {
		t.Fatal("bound directory should be preserved")
	}
}

func TestEditClearsScheduleToUnset(t *testing.T) {
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

	res, err := EditWith(d, "sched", "", true)
	if err != nil {
		t.Fatalf("clearing schedule should succeed, got %v", err)
	}
	if !res.ScheduleUpdated || res.Schedule != "" {
		t.Fatalf("result = %+v, want cleared schedule", res)
	}

	r, err := loadManifest(d, "sched")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.IsScheduled() {
		t.Fatalf("manifest still scheduled after clear: %q", r.Manifest.Schedule)
	}
	if ScheduleLabel(r.Manifest.Schedule) != "manual" {
		t.Fatalf("schedule label = %q, want manual", ScheduleLabel(r.Manifest.Schedule))
	}
	if !r.Manifest.Paused || r.Manifest.PauseReason != PauseReasonChanged {
		t.Fatalf("clear should pause with reason changed: paused=%v reason=%q", r.Manifest.Paused, r.Manifest.PauseReason)
	}
}

func TestEditRejectsInvalidScheduleLeavingManifest(t *testing.T) {
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

	manifestPath := filepath.Join(dataHome, "pop", "routines", "sched", "manifest.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = EditWith(d, "sched", "every week", true)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "invalid schedule") {
		t.Fatalf("error = %v", err)
	}

	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("manifest changed on invalid schedule:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestEditUnknownIDErrors(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	if _, err := EditWith(d, "ghost", "every 6h", true); err == nil {
		t.Fatal("expected unknown id error (schedule path)")
	}
	d.IsInteractive = func() bool { return true }
	d.OpenEditor = func(path string) error { return nil }
	if _, err := EditWith(d, "ghost", "", false); err == nil {
		t.Fatal("expected unknown id error (editor path)")
	}
}
