package routine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// writeHandoffPrompt overwrites a scaffolded routine's prompt.md with known
// framing text so tests can assert it is embedded inline.
func writeHandoffPrompt(t *testing.T, d *Deps, id, content string) {
	t.Helper()
	promptPath := filepath.Join(routineDir(d, id), promptFileName)
	if err := d.FS.WriteFile(promptPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffNoRunsYet(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "bugfinder", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}
	writeHandoffPrompt(t, d, "bugfinder", "Find bugs in the codebase and log them.")

	var out bytes.Buffer
	if err := HandoffWith(d, "bugfinder", &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()

	memoryDir := filepath.Join(routineDir(d, "bugfinder"), memoryDirName)
	for _, want := range []string{
		"Find bugs in the codebase and log them.", // prompt embedded inline
		"No runs yet",                             // no-runs-yet note
		memoryDir,                                 // memory dir absolute path
		home,                                      // bound directory named
		"the user",                                // closing hands off to user
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("handoff missing %q:\n%s", want, text)
		}
	}
	// No baked-in task: the memory dir path is absolute.
	if !filepath.IsAbs(memoryDir) {
		t.Fatalf("memory dir not absolute: %s", memoryDir)
	}
	// A no-runs routine has no report reference.
	if strings.Contains(text, runsDirName+string(filepath.Separator)) {
		t.Fatalf("unexpected report reference for no-runs routine:\n%s", text)
	}
}

func TestHandoffNotesFailedRun(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "bugfinder", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}
	writeHandoffPrompt(t, d, "bugfinder", "Find bugs.")

	firedAt := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	report := filepath.Join(routineDir(d, "bugfinder"), runsDirName, "2026-07-18T16-00-00Z.md")
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.StartRoutineRun(store.RoutineRun{RoutineID: "bugfinder", FiredAt: firedAt, PID: 100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run.ID, store.RoutineRunFailed, report, "agent exited with status 1", firedAt); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	if err := HandoffWith(d, "bugfinder", &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		report,                       // report by absolute path
		store.RoutineRunFailed,       // outcome noted
		"agent exited with status 1", // fail reason noted
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("handoff missing %q:\n%s", want, text)
		}
	}
}

func TestHandoffNotesSucceededRunWithoutFailReason(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "bugfinder", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}
	writeHandoffPrompt(t, d, "bugfinder", "Find bugs.")

	firedAt := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	report := filepath.Join(routineDir(d, "bugfinder"), runsDirName, "2026-07-18T16-00-00Z.md")
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.StartRoutineRun(store.RoutineRun{RoutineID: "bugfinder", FiredAt: firedAt, PID: 100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run.ID, store.RoutineRunSucceeded, report, "", firedAt); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	if err := HandoffWith(d, "bugfinder", &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, report) {
		t.Fatalf("handoff missing report path %q:\n%s", report, text)
	}
	if !strings.Contains(text, store.RoutineRunSucceeded) {
		t.Fatalf("handoff missing outcome:\n%s", text)
	}
	if strings.Contains(text, "Fail reason") {
		t.Fatalf("succeeded run should have no fail reason:\n%s", text)
	}
}

func TestHandoffUnknownID(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	var out bytes.Buffer
	if err := HandoffWith(d, "missing", &out); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), `routine "missing" not found`) {
		t.Fatalf("err = %v", err)
	}
}
