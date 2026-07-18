package routine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// breakManifest overwrites a routine's manifest.json with unparseable JSON so it
// fails to load.
func breakManifest(t *testing.T, dataHome, id string) {
	t.Helper()
	path := filepath.Join(dataHome, "pop", "routines", id, "manifest.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListRoutinesSkipsBrokenManifest(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "broken", "charlie"} {
		if _, err := AddWith(d, id, "daily at 10:00", home); err != nil {
			t.Fatal(err)
		}
	}
	breakManifest(t, dataHome, "broken")

	routines, warnings, err := ListRoutines(d)
	if err != nil {
		t.Fatalf("ListRoutines: %v", err)
	}
	gotIDs := make([]string, len(routines))
	for i, r := range routines {
		gotIDs[i] = r.ID
	}
	want := []string{"alpha", "charlie"}
	if strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("healthy routines = %v, want %v", gotIDs, want)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if warnings[0].ID != "broken" {
		t.Fatalf("warning id = %q, want %q", warnings[0].ID, "broken")
	}
	if warnings[0].Err == nil {
		t.Fatalf("warning err is nil")
	}
}

func TestListRoutinesAllHealthyNoWarnings(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "beta"} {
		if _, err := AddWith(d, id, "daily at 10:00", home); err != nil {
			t.Fatal(err)
		}
	}

	routines, warnings, err := ListRoutines(d)
	if err != nil {
		t.Fatalf("ListRoutines: %v", err)
	}
	if len(routines) != 2 {
		t.Fatalf("routines = %d, want 2", len(routines))
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestListRoutinesRootUnreadableHardError(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	// Place a regular file where the routines directory is expected so ReadDir
	// fails with a non-IsNotExist error (a directory-level read failure).
	popDir := filepath.Join(dataHome, "pop")
	if err := os.MkdirAll(popDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(popDir, "routines"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)

	routines, warnings, err := ListRoutines(d)
	if err == nil {
		t.Fatalf("expected hard error, got routines=%v warnings=%v", routines, warnings)
	}
}

func TestListWithPrintsHealthyAndWarns(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "broken"} {
		if _, err := AddWith(d, id, "daily at 10:00", home); err != nil {
			t.Fatal(err)
		}
	}
	breakManifest(t, dataHome, "broken")

	var out bytes.Buffer
	if err := ListWith(d, &out); err != nil {
		t.Fatalf("ListWith returned error (must stay exit 0): %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "alpha") {
		t.Fatalf("output missing healthy routine:\n%s", text)
	}
	if !strings.Contains(text, "warning") || !strings.Contains(text, "broken") {
		t.Fatalf("output missing warning naming broken routine:\n%s", text)
	}
}

func TestBuildDashboardWarnsBrokenManifest(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "broken"} {
		if _, err := AddWith(d, id, "daily at 10:00", home); err != nil {
			t.Fatal(err)
		}
	}
	breakManifest(t, dataHome, "broken")

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatalf("BuildDashboardWith: %v", err)
	}
	if len(snap.Rows) != 1 || snap.Rows[0].ID != "alpha" {
		t.Fatalf("rows = %v, want just alpha", snap.Rows)
	}
	if len(snap.Warnings) != 1 || !strings.Contains(snap.Warnings[0], "broken") {
		t.Fatalf("warnings = %v, want one naming broken", snap.Warnings)
	}
}
