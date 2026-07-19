package routine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readManifestPaused(t *testing.T, d *Deps, id string) bool {
	t.Helper()
	data, err := d.FS.ReadFile(filepath.Join(routineDir(d, id), manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m.Paused
}

func TestPauseResumePersistBitAndListReflects(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Routines are created paused; resume so the pause/resume cycle starts armed.
	if _, err := ResumeWith(d, "daily"); err != nil {
		t.Fatal(err)
	}

	res, err := PauseWith(d, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyPaused {
		t.Fatal("expected first pause to change state")
	}
	if !readManifestPaused(t, d, "daily") {
		t.Fatal("expected paused=true in manifest")
	}

	var listOut bytes.Buffer
	if err := ListWith(d, &listOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), "yes") {
		t.Fatalf("list should show paused=yes:\n%s", listOut.String())
	}

	res, err = PauseWith(d, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyPaused {
		t.Fatal("expected already paused on second pause")
	}

	resume, err := ResumeWith(d, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if resume.NotPaused {
		t.Fatal("expected resume to change state")
	}
	if readManifestPaused(t, d, "daily") {
		t.Fatal("expected paused=false after resume")
	}

	listOut.Reset()
	if err := ListWith(d, &listOut); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listOut.String(), "\tyes\n") {
		t.Fatalf("list should show paused=no:\n%s", listOut.String())
	}

	resume, err = ResumeWith(d, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if !resume.NotPaused {
		t.Fatal("expected not paused on second resume")
	}
}

func TestPauseResumeUnknownID(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	if _, err := PauseWith(d, "missing"); err == nil {
		t.Fatal("expected pause error")
	} else if !strings.Contains(err.Error(), `routine "missing" not found`) {
		t.Fatalf("pause err = %v", err)
	}

	if _, err := ResumeWith(d, "missing"); err == nil {
		t.Fatal("expected resume error")
	} else if !strings.Contains(err.Error(), `routine "missing" not found`) {
		t.Fatalf("resume err = %v", err)
	}
}

func TestFireWorksWhilePaused(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "paused-run", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := PauseWith(d, "paused-run"); err != nil {
		t.Fatal(err)
	}

	if _, err := FireWith(d, "paused-run"); err != nil {
		t.Fatalf("fire while paused should succeed: %v", err)
	}
}
