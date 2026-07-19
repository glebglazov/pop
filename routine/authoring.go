package routine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// authoringSessionFromGate spawns the interactive authoring agent for menu item
// 1 of the refinement gate (ADR-0125). Unlike a scheduled Fire, this is an
// attended, interactive invocation — not a headless print-mode run — launched in
// the Routine's bound directory so the agent can probe repo context and MCP
// tooling (e.g. test a JQL query live). It is front-loaded with rules embedded
// in the binary. Any failure to resolve or start the agent is reported and the
// gate loop continues; it never crashes the gate.
func authoringSessionFromGate(d *Deps, out io.Writer, id, agentOverride string) {
	r, err := loadManifest(d, id)
	if err != nil {
		fmt.Fprintf(out, "Could not load the routine: %v\n", err)
		return
	}
	spec, err := resolveAuthoringAgentSpec(d, agentOverride)
	if err != nil {
		fmt.Fprintf(out, "Could not resolve the authoring agent: %v\n", err)
		return
	}
	prompt := buildAuthoringPrompt(d, id, r)
	invocation, err := tasks.ResolveAgentAssistanceInvocation(spec, "", prompt, r.Manifest.BoundDirectory)
	if err != nil {
		fmt.Fprintf(out, "Could not prepare the authoring agent: %v\n", err)
		return
	}
	fmt.Fprintf(out, "Starting authoring session (agent %s) in %s\n", invocation.AgentPreset, r.Manifest.BoundDirectory)
	exitCode, err := runRoutineAttendedAgent(d, r.Manifest.BoundDirectory, out, invocation)
	if err != nil {
		fmt.Fprintf(out, "Could not start the authoring session: %v\n", err)
		return
	}
	if exitCode != 0 {
		fmt.Fprintf(out, "Authoring session exited with status %d.\n", exitCode)
	}
	fmt.Fprintln(out, "Authoring session ended; returning to the menu.")
}

// resolveAuthoringAgentSpec picks the agent preset spec for a gate authoring
// session. An explicit --agent override wins for the session; otherwise
// resolution follows [routines].agents with the same fall-through as scheduled
// runs (ResolveRoutineAgentPresets), taking the highest-priority preset. The
// session is interactive with a human present, so there is no headless quota
// fall-through — the human switches agents by hand if one is unavailable.
func resolveAuthoringAgentSpec(d *Deps, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}
	cfg, err := d.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	specs := ResolveRoutineAgentPresets(cfg)
	if len(specs) == 0 {
		return "", fmt.Errorf("no agent preset is configured")
	}
	return specs[0], nil
}

// runRoutineAttendedAgent runs the resolved attended agent command in the bound
// directory, wiring the raw stdin through so a TTY-requiring agent inherits the
// controlling terminal (see tasks.RunAttended). It mirrors the tasks-package
// attended-assistance runner rather than the headless Fire path.
func runRoutineAttendedAgent(d *Deps, dir string, out io.Writer, invocation *tasks.AgentAssistanceInvocation) (int, error) {
	runner := d.taskDeps().Runner
	if runner == nil {
		runner = tasks.RealCommandRunner{}
	}
	stdin := d.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	if attended, ok := runner.(tasks.AttendedCommandRunner); ok {
		return attended.RunAttended(context.Background(), dir, stdin, out, out, invocation.Command.Name, invocation.Command.Args...)
	}
	return runner.Run(context.Background(), dir, out, out, invocation.Command.Name, invocation.Command.Args...)
}

// buildAuthoringPrompt returns the front-loaded rules the authoring agent starts
// with (ADR-0125): the routine framework contract, this routine's concrete
// paths, and an interview checklist. It directs the agent to edit prompt.md
// directly but to change the schedule only through `pop routine edit --schedule`
// so the parser's validation is never bypassed.
func buildAuthoringPrompt(d *Deps, id string, r *Routine) string {
	dir := routineDir(d, id)
	promptPath := filepath.Join(dir, promptFileName)
	memoryDir := filepath.Join(dir, memoryDirName)
	runsDir := filepath.Join(dir, runsDirName)

	var b strings.Builder
	fmt.Fprintf(&b, "You are helping author the prompt for a pop routine (id %q). Pop routines are\n", id)
	b.WriteString("directory-bound schedules that fire an unattended agent run over time. Your job\n")
	b.WriteString("in this session is to interview me and write a good prompt.md for this routine.\n\n")

	b.WriteString("## Framework contract\n\n")
	b.WriteString("When the routine fires, pop wraps your prompt.md — it does NOT run prompt.md\n")
	b.WriteString("verbatim. The wrapping is:\n")
	fmt.Fprintf(&b, "  - PREAMBLE: \"Before starting, read the routine memory directory at %s and\n", memoryDir)
	b.WriteString("    incorporate any prior context.\"\n")
	b.WriteString("  - then the verbatim contents of prompt.md\n")
	fmt.Fprintf(&b, "  - POSTAMBLE: \"When finished, write your report to <runs>/<timestamp>.md and\n")
	fmt.Fprintf(&b, "    update the routine memory directory at %s with what you learned.\"\n", memoryDir)
	b.WriteString("So prompt.md should assume the memory has already been read and a report will be\n")
	b.WriteString("written for it; write it as the routine's task, not as setup/teardown.\n\n")
	fmt.Fprintf(&b, "  - Memory directory: %s (persists across runs; you define its format)\n", memoryDir)
	fmt.Fprintf(&b, "  - Reports directory: %s (one timestamped .md report per run)\n", runsDir)
	b.WriteString("  - Schedule grammar: \"every <duration>\" (e.g. \"every 6h\", \"every 30m\") or\n")
	b.WriteString("    \"daily at H[:MM][ utc]\" (e.g. \"daily at 11\", \"daily at 10:00\", \"daily at\n")
	b.WriteString("    11:00 utc\"). Daily uses the machine's local wall clock unless a \"utc\"\n")
	b.WriteString("    suffix is given.\n\n")

	b.WriteString("## This routine's concrete paths\n\n")
	fmt.Fprintf(&b, "  - Bound directory (cwd for every run, incl. this session): %s\n", r.Manifest.BoundDirectory)
	fmt.Fprintf(&b, "  - Prompt file to edit: %s\n", promptPath)
	fmt.Fprintf(&b, "  - Memory directory: %s\n", memoryDir)
	fmt.Fprintf(&b, "  - Reports directory: %s\n", runsDir)
	fmt.Fprintf(&b, "  - Current schedule: %s\n\n", r.Manifest.Schedule)

	b.WriteString("## Interview checklist\n\n")
	b.WriteString("Interview me until you can answer each of these, then write prompt.md:\n")
	b.WriteString("  1. Goal — what should each run accomplish?\n")
	b.WriteString("  2. Data source — where does the data come from? Test it live now (this\n")
	b.WriteString("     session runs in the bound directory with repo context and MCP tooling; e.g.\n")
	b.WriteString("     run the actual JQL query rather than guessing).\n")
	b.WriteString("  3. Definition of seen/new — how does a run tell already-processed items from\n")
	b.WriteString("     fresh ones (usually via the memory directory)?\n")
	b.WriteString("  4. Memory format — what should the routine record in the memory directory,\n")
	b.WriteString("     and in what shape?\n")
	b.WriteString("  5. Report format — what should each run's report contain?\n")
	b.WriteString("  6. Empty-run behavior — what should a run do when there is nothing new?\n\n")

	b.WriteString("## How to apply your work\n\n")
	fmt.Fprintf(&b, "  - Edit the prompt directly: write %s.\n", promptPath)
	fmt.Fprintf(&b, "  - Change the schedule ONLY via `pop routine edit %s --schedule \"<expr>\"`\n", id)
	b.WriteString("    (do not hand-edit the manifest — that command validates the expression\n")
	b.WriteString("    through the parser, so validation is never bypassed).\n")
	b.WriteString("  - When you exit, control returns to the pop refinement menu, where I can fire\n")
	b.WriteString("    a test run and resume the routine.\n")

	return b.String()
}
