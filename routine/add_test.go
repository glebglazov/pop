package routine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func routineDeps(t *testing.T, dataHome string) *Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dataHome)
	return &Deps{
		FS:            deps.NewRealFileSystem(),
		IsInteractive: func() bool { return false },
		Now: func() time.Time {
			ts, err := time.Parse(timeRFC3339, "2026-07-18T12:00:00Z")
			if err != nil {
				t.Fatal(err)
			}
			return ts
		},
	}
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
	if res.Manifest.Paused {
		t.Fatal("expected paused=false")
	}

	for _, rel := range []string{
		"manifest.json",
		"prompt.md",
		"memory",
		"runs",
	} {
		path := filepath.Join(wantDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(wantDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var loaded Manifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.BoundDirectory != wantBound {
		t.Fatalf("loaded bound dir = %q", loaded.BoundDirectory)
	}
	if loaded.Schedule != "daily at 10:00" {
		t.Fatalf("loaded schedule = %q", loaded.Schedule)
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

func TestAddOpensEditorWhenInteractive(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	var opened string
	d.IsInteractive = func() bool { return true }
	d.OpenEditor = func(path string) error {
		opened = path
		return nil
	}

	if _, err := AddWith(d, "edit-me", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataHome, "pop", "routines", "edit-me", "prompt.md")
	if opened != want {
		t.Fatalf("opened = %q, want %q", opened, want)
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
		"no",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
