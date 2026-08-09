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

func TestPromptRoutineGateMenuOverridePromotes(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
			{DisplayName: "Cursor", Cmd: "cursor"},
		}},
	}}
	d := &Deps{
		LoadConfig: func() (*config.Config, error) { return cfg, nil },
		Tasks:      &tasks.Deps{},
	}

	origMenu := runGateMenu
	origPicker := runAgentOverridePicker
	defer func() {
		runGateMenu = origMenu
		runAgentOverridePicker = origPicker
	}()

	calls := 0
	runGateMenu = func(spec ui.GateMenuSpec, in io.Reader, out io.Writer, cfg ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		calls++
		if calls == 1 {
			return ui.GateMenuResult{OpenOverride: true}, nil
		}
		if !strings.Contains(spec.AttendedLabel, "Cursor") {
			t.Fatalf("label after override = %q", spec.AttendedLabel)
		}
		return ui.GateMenuResult{Key: "0"}, nil
	}
	runAgentOverridePicker = func(groups []ui.AgentOverrideGroup, in io.Reader, out io.Writer, warn func(string, ...any)) (*ui.AgentOverrideChoice, error) {
		return &ui.AgentOverrideChoice{Group: "attended", Cmd: "cursor"}, nil
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
	if d.Tasks.AgentOverrides == nil || d.Tasks.AgentOverrides.AttendedCmd() != "cursor" {
		t.Fatalf("override = %#v", d.Tasks.AgentOverrides)
	}
}
