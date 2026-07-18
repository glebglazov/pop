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

func TestRunsEmptyHint(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "empty", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := RunsWith(d, "empty", &out); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != emptyRunsHint {
		t.Fatalf("got %q", got)
	}
}

func TestRunsListsNewestFirstWithReportPath(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "history", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	firstAt := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	secondAt := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	firstReport := filepath.Join(routineDir(d, "history"), runsDirName, "2026-07-18T10-00-00Z.md")
	secondReport := filepath.Join(routineDir(d, "history"), runsDirName, "2026-07-18T16-00-00Z.md")

	run1, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "history",
		FiredAt:   firstAt,
		PID:       100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run1.ID, store.RoutineRunSucceeded, firstReport, "", firstAt); err != nil {
		t.Fatal(err)
	}
	run2, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "history",
		FiredAt:   secondAt,
		PID:       101,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run2.ID, store.RoutineRunFailed, secondReport, "agent exited with status 1", secondAt); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	if err := RunsWith(d, "history", &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"FIRED AT",
		"OUTCOME",
		"REPORT",
		"2026-07-18T16:00:00Z",
		"2026-07-18T10:00:00Z",
		store.RoutineRunSucceeded,
		store.RoutineRunFailed,
		secondReport,
		firstReport,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "2026-07-18T16:00:00Z") > strings.Index(text, "2026-07-18T10:00:00Z") {
		t.Fatalf("expected newest-first ordering:\n%s", text)
	}
}

func TestRunsUnknownID(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := routineDeps(t, dataHome)

	var out bytes.Buffer
	if err := RunsWith(d, "missing", &out); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), `routine "missing" not found`) {
		t.Fatalf("err = %v", err)
	}
}
