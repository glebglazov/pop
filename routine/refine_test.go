package routine

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// refineDeps builds injected deps for the gate: a fake claude on PATH (so Fire
// is real but hermetic), an interactive flag, scripted stdin, a captured stdout,
// and no-op editor/pager so nothing spawns a real process.
func refineDeps(t *testing.T, dataHome, input string, out io.Writer) *Deps {
	t.Helper()
	d := fireDeps(t, dataHome)
	d.IsInteractive = func() bool { return true }
	d.Stdin = strings.NewReader(input)
	d.Stdout = out
	d.OpenEditor = func(string) error { return nil }
	d.OpenPager = func(string) error { return nil }
	return d
}

func addRoutineForGate(t *testing.T, d *Deps, id, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, id, "every 6h", home); err != nil {
		t.Fatal(err)
	}
}

func TestRefineMenuRendersHouseGrammar(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		`Refine routine "gate"`,
		"paused",
		"no runs yet",
		"1. Agent session (default)",
		"2. Fire test run",
		"3. View last report",
		"4. Edit prompt",
		"5. Edit schedule",
		"6. Resume routine & exit",
		"0. Exit (stay paused)",
		"Choose [1]: ",
		"Leaving routine \"gate\" paused.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("menu missing %q:\n%s", want, text)
		}
	}
}

func TestRefineInvalidInputReprompts(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "9\n0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Choose 1, 2, 3, 4, 5, 6, or 0.") {
		t.Fatalf("expected reprompt naming valid choices:\n%s", text)
	}
	if strings.Count(text, "Choose [1]: ") < 2 {
		t.Fatalf("menu should re-render after invalid input:\n%s", text)
	}
}

func TestRefineFireIsARealRun(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	installFakeClaude(t, root, 0)
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "2\n0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Fire succeeded") {
		t.Fatalf("expected fire success line:\n%s", out.String())
	}

	// The run row is recorded and its report kept.
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.LastRoutineRun("gate")
	_ = s.Close()
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunSucceeded {
		t.Fatalf("row = %+v", row)
	}
	if _, err := os.Stat(row.ReportPath); err != nil {
		t.Fatalf("report kept: %v", err)
	}

	// Visible via the view verb (`pop routine runs`) afterwards.
	var runsOut bytes.Buffer
	if err := RunsWith(d, "gate", &runsOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runsOut.String(), "succeeded") {
		t.Fatalf("runs output should list the gate fire:\n%s", runsOut.String())
	}
}

func TestRefineFireFailureLoopsBack(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	installFakeClaude(t, root, 2)
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "2\n0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Fire failed:") {
		t.Fatalf("expected fire failure report:\n%s", text)
	}
	if strings.Count(text, "Choose [1]: ") < 2 {
		t.Fatalf("gate should loop back after a failed fire:\n%s", text)
	}
}

func TestRefineResumeUnpausesAndExits(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "6\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Resumed routine") {
		t.Fatalf("expected resume confirmation:\n%s", out.String())
	}
	if paused := manifestPaused(t, dataHome, "gate"); paused {
		t.Fatal("resume verb should clear the pause bit")
	}
}

func TestRefineExitStaysPaused(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	if paused := manifestPaused(t, dataHome, "gate"); !paused {
		t.Fatal("exit verb should leave the routine paused")
	}
}

func TestRefineEditPromptOpensEditor(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "4\n0\n", &out)
	var opened string
	d.OpenEditor = func(path string) error {
		opened = path
		return nil
	}
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataHome, "pop", "routines", "gate", "prompt.md")
	if opened != want {
		t.Fatalf("opened = %q, want %q", opened, want)
	}
}

func TestRefineEditScheduleValidatesAndReedits(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	// First expression is invalid → inline error + re-edit; second is valid.
	d := refineDeps(t, dataHome, "5\nevery week\nevery 12h\n0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "invalid schedule") {
		t.Fatalf("expected inline parse error:\n%s", text)
	}
	if !strings.Contains(text, `Schedule updated to "every 12h".`) {
		t.Fatalf("expected schedule update confirmation:\n%s", text)
	}
	r, err := loadManifest(d, "gate")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Schedule != "every 12h" {
		t.Fatalf("persisted schedule = %q", r.Manifest.Schedule)
	}
}

func TestRefineViewReportNoReportPrintsPath(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "3\n0\n", &out)
	pagerCalled := false
	d.OpenPager = func(string) error {
		pagerCalled = true
		return nil
	}
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No runs yet") {
		t.Fatalf("expected no-report message:\n%s", out.String())
	}
	if pagerCalled {
		t.Fatal("pager must not open when there is no report")
	}
}

func TestRefineViewReportOpensPagerAfterFire(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	installFakeClaude(t, root, 0)
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "2\n3\n0\n", &out)
	var paged string
	d.OpenPager = func(path string) error {
		paged = path
		return nil
	}
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate"); err != nil {
		t.Fatal(err)
	}
	if paged == "" {
		t.Fatalf("expected pager to open the report after a fire:\n%s", out.String())
	}
	if _, err := os.Stat(paged); err != nil {
		t.Fatalf("paged report path should exist: %v", err)
	}
}

func TestRefineNonInteractiveErrors(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "", &out)
	d.IsInteractive = func() bool { return false }
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	err := RefineWith(d, "gate")
	if err == nil {
		t.Fatal("expected non-interactive refine to error")
	}
	promptPath := filepath.Join(dataHome, "pop", "routines", "gate", "prompt.md")
	if !strings.Contains(err.Error(), promptPath) {
		t.Fatalf("error should name the prompt path %q, got %v", promptPath, err)
	}
}

func TestRefineUnknownIDErrors(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "0\n", &out)
	if err := RefineWith(d, "ghost"); err == nil {
		t.Fatal("expected unknown id error")
	}
}

func manifestPaused(t *testing.T, dataHome, id string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dataHome, "pop", "routines", id, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m.Paused
}
