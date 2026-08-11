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
// menus.
var runGateMenu = ui.RunGateMenu

// promptRoutineGateMenu runs the shared inline gate menu and returns the chosen
// key. reader is the shared per-gate tty.Reader used on the non-TTY line path
// so schedule edits after a menu choice do not lose queued input.
//
// The Assists item names the attended entry the merged config resolves to — the
// override layer included, so a change made in the Config dashboard shows here
// (ADR-0196 decision 9, ADR-0202 decision 5).
func promptRoutineGateMenu(out io.Writer, in io.Reader, reader *tty.Reader, spec ui.GateMenuSpec, d *Deps) (string, error) {
	if in == nil {
		in = os.Stdin
	}
	warn := func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
	}
	cfg, _ := d.LoadConfig()
	spec.AttendedLabel = tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg))
	res, err := runGateMenu(spec, in, out, ui.GateMenuRunConfig{
		LineReader: reader,
		Warn:       warn,
	})
	if err != nil {
		return "", fmt.Errorf("read gate selection: %w", err)
	}
	return res.Key, nil
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
