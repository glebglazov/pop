package routine

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

// runGateMenu is the seam every Routine gate prompt calls. Production points at
// ui.RunGateMenu; tests may swap it. Same injected-seam shape as tasks' gate
// menus so the override picker (ADR-0196) wraps both call sites alike.
var runGateMenu = ui.RunGateMenu

// runAgentOverridePicker is the seam for the gate-side override picker.
var runAgentOverridePicker = ui.RunAgentOverridePicker

// promptRoutineGateMenu runs the shared inline gate menu and returns the chosen
// key. reader is the shared per-gate tty.Reader used on the non-TTY line path
// so schedule edits after a menu choice do not lose queued input.
//
// alt+a opens the same two-level agent-override picker as the dashboard and
// Task-set gates; a pick promotes into the process-lived bag on d.Tasks and the
// menu re-opens with an updated AttendedLabel (ADR-0196 decisions 5 and 9).
func promptRoutineGateMenu(out io.Writer, in io.Reader, reader *tty.Reader, spec ui.GateMenuSpec, d *Deps) (string, error) {
	if in == nil {
		in = os.Stdin
	}
	warn := func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
	}
	taskDeps := ensureRoutineAgentOverrides(d)
	cfg, _ := d.LoadConfig()
	for {
		spec.AttendedLabel = tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg, taskDeps.AgentOverrides))
		cfgRun := ui.GateMenuRunConfig{
			LineReader: reader,
			Warn:       warn,
		}
		res, err := runGateMenu(spec, in, out, cfgRun)
		if err != nil {
			return "", fmt.Errorf("read gate selection: %w", err)
		}
		if res.OpenOverride {
			if err := promptRoutineAgentOverride(out, in, d, warn); err != nil {
				return "", err
			}
			cfg, _ = d.LoadConfig()
			continue
		}
		return res.Key, nil
	}
}

func promptRoutineAgentOverride(out io.Writer, in io.Reader, d *Deps, warn func(string, ...any)) error {
	taskDeps := ensureRoutineAgentOverrides(d)
	cfg, err := d.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config for agent override: %w", err)
	}
	choice, err := runAgentOverridePicker(tasks.AgentOverridePickerGroups(cfg), in, out, warn)
	if err != nil {
		return fmt.Errorf("read agent override: %w", err)
	}
	if choice != nil {
		taskDeps.AgentOverrides.Promote(choice.Group, choice.Cmd)
	}
	return nil
}

func ensureRoutineAgentOverrides(d *Deps) *tasks.Deps {
	td := d.taskDeps()
	if d != nil && d.Tasks == nil {
		d.Tasks = td
	}
	if td.AgentOverrides == nil {
		td.AgentOverrides = tasks.NewAgentOverrides()
	}
	return td
}

func refineGateSpec(id string, r *Routine, lastRun string) ui.GateMenuSpec {
	state := "resumed"
	if r.Manifest.Paused {
		state = pausedStatusLabel(r.Manifest.PauseReason)
	}
	return ui.GateMenuSpec{
		Headline: fmt.Sprintf("Refine routine %q — %s, schedule %q, %s", id, state, r.Manifest.Schedule, lastRun),
		Tone:     ui.GateMenuToneDefault,
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Agent session (default)", Default: true, Assists: true},
			{Key: "2", Label: "Fire test run", Aliases: []string{"fire"}},
			{Key: "3", Label: "View last report"},
			{Key: "4", Label: "Edit prompt"},
			{Key: "5", Label: "Edit schedule"},
			{Key: "6", Label: "Resume routine & exit", Aliases: []string{"resume"}},
			{Key: "0", Label: "Exit (stay paused)"},
		},
	}
}

func projectRefineGateSpec(name, lastRun string) ui.GateMenuSpec {
	return ui.GateMenuSpec{
		Headline: fmt.Sprintf("Refine Project routine %q — manual-fire-only, %s", ProjectOrigin+name, lastRun),
		Tone:     ui.GateMenuToneDefault,
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Agent session (default)", Default: true, Assists: true},
			{Key: "2", Label: "Fire test run", Aliases: []string{"fire"}},
			{Key: "3", Label: "View last report"},
			{Key: "4", Label: "Edit prompt"},
			{Key: "0", Label: "Exit"},
		},
	}
}
