package routine

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

// A Routine gate names the attended entry its default choice will launch, read
// from the merged config — the override layer included (ADR-0202 decision 5).
func TestPromptRoutineGateMenuNamesTheAttendedEntry(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Cursor", Cmd: "cursor"},
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	d := &Deps{
		LoadConfig: func() (*config.Config, error) { return cfg, nil },
		Tasks:      &tasks.Deps{},
	}

	orig := runGateMenu
	defer func() { runGateMenu = orig }()
	calls := 0
	runGateMenu = func(spec ui.GateMenuSpec, in io.Reader, out io.Writer, _ ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		calls++
		if !strings.Contains(spec.AttendedLabel, "Cursor") {
			t.Fatalf("label = %q, want the merged head", spec.AttendedLabel)
		}
		return ui.GateMenuResult{Key: "0"}, nil
	}

	var out bytes.Buffer
	in := strings.NewReader("")
	key, err := promptRoutineGateMenu(&out, in, tty.NewReader(in), refineGateSpec("demo", &Routine{Manifest: Manifest{Schedule: "every 1h"}}, "no runs yet"), d)
	if err != nil {
		t.Fatal(err)
	}
	if key != "0" {
		t.Fatalf("key = %q", key)
	}
	// One pass: there is no side-trip to re-open the menu for any more.
	if calls != 1 {
		t.Fatalf("gate menu shown %d times, want 1", calls)
	}
}
