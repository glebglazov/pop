package routine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/tasks"
)

// projectRoutineFilePath resolves the in-repo `.pop/routines/<name>.md` that a
// Project routine's edit and refinement surfaces target (ADR-0138). The file is
// the sole source of truth; a missing checkout or a missing file is an error,
// mirroring findProjectRoutine.
func projectRoutineFilePath(d *Deps, name string) (string, error) {
	if err := validateID(name); err != nil {
		return "", err
	}
	root, ok := checkoutRoot(d)
	if !ok {
		return "", fmt.Errorf("not in a git checkout; Project routine %q can only be edited from inside its checkout", name)
	}
	path := filepath.Join(root, ".pop", projectRoutinesDirName, name+projectRoutineExt)
	if _, err := d.FS.Stat(path); err != nil {
		return "", fmt.Errorf("project routine %q not found in this checkout", name)
	}
	return path, nil
}

// editProjectPrompt opens a Project routine's committed prompt file in $EDITOR
// (ADR-0138). Unlike an authored routine's prompt edit, it never pauses — a
// Project routine has no pause state — and it writes nothing but the file the
// human then reviews and commits. A non-interactive session names the path so
// the caller can edit it directly.
func editProjectPrompt(d *Deps, name string) (*EditResult, error) {
	path, err := projectRoutineFilePath(d, name)
	if err != nil {
		return nil, err
	}
	if d.IsInteractive == nil || !d.IsInteractive() {
		return nil, fmt.Errorf("cannot open an editor in a non-interactive session; edit the prompt directly at %s", path)
	}
	if d.OpenEditor == nil {
		return nil, fmt.Errorf("no editor available; edit the prompt directly at %s", path)
	}
	if err := d.OpenEditor(path); err != nil {
		return nil, fmt.Errorf("open prompt in editor: %w", err)
	}
	return &EditResult{RoutineID: ProjectOrigin + name, PromptPath: path, Opened: true}, nil
}

// writeProjectRuntime is the edit chokepoint for a Project routine's agents and
// effort (ADR-0138): it validates the requested values and rewrites the
// committed `.pop/routines/<name>.md` frontmatter in place, leaving the prompt
// body verbatim. There is no state.json and no pause — a Project routine has no
// pause state — and pop never stages or commits: the human reviews the diff.
func writeProjectRuntime(d *Deps, id string, agents []string, agentsSet bool, effort string, effortSet bool) (*RuntimeResult, error) {
	name := projectRoutineName(id)
	path, err := projectRoutineFilePath(d, name)
	if err != nil {
		return nil, err
	}
	// An explicitly-empty set is a clear back to unset (config-resolved), mirroring
	// the authored write path; only a non-empty value is validated.
	clearAgents := agentsSet && len(nonEmptyAgentSpecs(agents)) == 0
	if agentsSet && !clearAgents {
		if err := validateAgentPresets(agents); err != nil {
			return nil, err
		}
	}
	trimmedEffort := strings.TrimSpace(effort)
	clearEffort := effortSet && trimmedEffort == ""
	if effortSet && !clearEffort {
		if err := validateEffort(trimmedEffort); err != nil {
			return nil, err
		}
	}
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project routine: %w", err)
	}
	fields, body, err := frontmatter.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse project routine frontmatter: %w", err)
	}
	if agentsSet {
		fields.Agents = nonEmptyAgentSpecs(agents)
	}
	if effortSet {
		fields.Effort = trimmedEffort
	}
	out, err := frontmatter.Marshal(fields, body)
	if err != nil {
		return nil, fmt.Errorf("encode project routine frontmatter: %w", err)
	}
	if err := d.FS.WriteFile(path, []byte(out), 0o644); err != nil {
		return nil, fmt.Errorf("write project routine: %w", err)
	}
	return &RuntimeResult{
		RoutineID: ProjectOrigin + name,
		Agents:    fields.Agents,
		Effort:    fields.Effort,
		Paused:    false,
	}, nil
}

// refineProjectRoutine runs the HITL refinement loop for a Project routine
// (ADR-0138). Its gate is the shared inline GateMenu (ADR-0196) mirroring the
// authored loop but dropping the schedule-edit and resume items — a Project
// routine is manual-fire-only and has no pause state — and its authoring session
// runs in the checkout with a project-aware briefing that states pop never
// commits. The loop is interactive-only; a non-interactive call names the
// prompt path so the caller can edit it directly.
func refineProjectRoutine(d *Deps, id, agentOverride string) error {
	name := projectRoutineName(id)
	pr, err := findProjectRoutine(d, name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(agentOverride) != "" {
		if _, err := tasks.ResolveAgentAdapter(agentOverride); err != nil {
			return err
		}
	}

	out := d.Stdout
	if out == nil {
		out = os.Stdout
	}
	path, err := projectRoutineFilePath(d, name)
	if err != nil {
		return err
	}
	if d.IsInteractive == nil || !d.IsInteractive() {
		return fmt.Errorf("cannot refine Project routine %q in a non-interactive session; edit the prompt directly at %s", ProjectOrigin+name, path)
	}

	in := d.Stdin
	if in == nil {
		in = os.Stdin
	}
	reader := tty.NewReader(in)

	key := checkoutKey(pr.Dir)
	storeID := projectStoreID(key, name)
	runsDir := filepath.Join(projectRoutineDataDir(d, key, name), runsDirName)

	for {
		pr, err := findProjectRoutine(d, name)
		if err != nil {
			return err
		}
		choice, err := promptRoutineGateMenu(out, in, reader, projectRefineGateSpec(name, lastRunSummary(d, storeID)), d)
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			projectAuthoringSessionFromGate(d, out, pr, agentOverride)
		case "2":
			fireFromGate(d, out, ProjectOrigin+name)
		case "3":
			viewLastReport(d, out, storeID, runsDir)
		case "4":
			if _, err := EditWith(d, ProjectOrigin+name, "", false); err != nil {
				fmt.Fprintf(out, "Could not open the prompt: %v\n", err)
			}
		case "0", "":
			fmt.Fprintf(out, "Leaving Project routine %q.\n", ProjectOrigin+name)
			return nil
		}
	}
}

// projectAuthoringSessionFromGate spawns the interactive authoring agent for a
// Project routine's refinement gate (ADR-0138). It mirrors
// authoringSessionFromGate but runs in the checkout and front-loads a
// project-aware briefing: manual-only, no schedule, and pop never commits.
func projectAuthoringSessionFromGate(d *Deps, out io.Writer, pr *ProjectRoutine, agentOverride string) {
	spec, cfg, err := resolveAuthoringAgentSpec(d, agentOverride)
	if err != nil {
		fmt.Fprintf(out, "Could not resolve the authoring agent: %v\n", err)
		return
	}
	prompt := buildProjectAuthoringPrompt(d, pr)
	invocation, err := tasks.ResolveAgentAssistanceInvocation(d.taskDeps(), cfg, spec, "", prompt, pr.Dir)
	if err != nil {
		fmt.Fprintf(out, "Could not prepare the authoring agent: %v\n", err)
		return
	}
	fmt.Fprintf(out, "Starting authoring session (agent %s) in %s\n", invocation.AgentPreset, pr.Dir)
	exitCode, err := runRoutineAttendedAgent(d, pr.Dir, out, invocation)
	if err != nil {
		fmt.Fprintf(out, "Could not start the authoring session: %v\n", err)
		return
	}
	if exitCode != 0 {
		fmt.Fprintf(out, "Authoring session exited with status %d.\n", exitCode)
	}
	fmt.Fprintln(out, "Authoring session ended; returning to the menu.")
}

// buildProjectAuthoringPrompt returns the front-loaded rules the authoring agent
// starts with when refining a Project routine (ADR-0138). It reuses the shared
// framework contract but is explicit about the Project-routine boundary: the
// prompt is a committed file, there is no schedule and none may be set, and pop
// never commits — the human reviews the diff and commits if they like it.
func buildProjectAuthoringPrompt(d *Deps, pr *ProjectRoutine) string {
	key := checkoutKey(pr.Dir)
	dataDir := projectRoutineDataDir(d, key, pr.Name)
	promptPath := filepath.Join(pr.Dir, ".pop", projectRoutinesDirName, pr.Name+projectRoutineExt)
	memoryDir := filepath.Join(dataDir, memoryDirName)
	runsDir := filepath.Join(dataDir, runsDirName)

	createMode := isCreateModePrompt(pr.Prompt)

	var b strings.Builder
	if createMode {
		fmt.Fprintf(&b, "You are helping author the prompt for a pop Project routine (id %q). A\n", ProjectOrigin+pr.Name)
		b.WriteString("Project routine is a prompt committed to this repo — everyone who checks it out\n")
		b.WriteString("gets it. Your job in this session is to interview me and write a good prompt.\n\n")
	} else {
		fmt.Fprintf(&b, "You are helping refine an existing pop Project routine (id %q). A Project\n", ProjectOrigin+pr.Name)
		b.WriteString("routine is a prompt committed to this repo — everyone who checks it out gets\n")
		b.WriteString("it. This session changes its committed prompt file.\n\n")
	}

	b.WriteString("## Framework contract\n\n")
	b.WriteString("When the routine fires, pop wraps your prompt — it does NOT run it verbatim. The\n")
	b.WriteString("wrapping is:\n")
	fmt.Fprintf(&b, "  - PREAMBLE: \"Before starting, read the routine memory directory at %s and\n", memoryDir)
	b.WriteString("    incorporate any prior context.\"\n")
	b.WriteString("  - then the verbatim contents of the prompt file's body\n")
	fmt.Fprintf(&b, "  - POSTAMBLE: \"When finished, write your report to <runs>/<timestamp>.md and\n")
	fmt.Fprintf(&b, "    update the routine memory directory at %s with what you learned.\"\n", memoryDir)
	b.WriteString("  - SENTINEL: the postamble also requires the run to end its output with\n")
	fmt.Fprintf(&b, "    %s (report written, run done) or %s: <reason>. A run that exits\n", routineCompleteSentinel, routineFailedSentinel)
	fmt.Fprintf(&b, "    cleanly without %s is recorded FAILED, so do not have the prompt\n", routineCompleteSentinel)
	b.WriteString("    fight this — leave the sentinel to the framework.\n")
	b.WriteString("So the prompt should assume the memory has already been read and a report will be\n")
	b.WriteString("written for it; write it as the routine's task, not as setup/teardown.\n\n")

	b.WriteString("## This routine is a Project routine\n\n")
	b.WriteString("  - It is manual-fire-only by design: it has NO schedule and none may be set\n")
	b.WriteString("    (a shared routine on a shared schedule would fire redundantly for everyone).\n")
	b.WriteString("    Do not add a `schedule:` key — pop ignores it and warns.\n")
	b.WriteString("  - The frontmatter may carry `agents` and `effort` only.\n")
	b.WriteString("  - The prompt file is committed to the repo, but pop NEVER commits your edit.\n")
	b.WriteString("    When we are done, I review the diff and commit it myself if I like it.\n\n")

	b.WriteString("## This routine's concrete paths\n\n")
	fmt.Fprintf(&b, "  - Checkout (cwd for every run, incl. this session): %s\n", pr.Dir)
	fmt.Fprintf(&b, "  - Prompt file to edit: %s\n", promptPath)
	fmt.Fprintf(&b, "  - Memory directory (per-checkout, not committed): %s\n", memoryDir)
	fmt.Fprintf(&b, "  - Reports directory (per-checkout, not committed): %s\n\n", runsDir)

	if createMode {
		b.WriteString("## Interview checklist\n\n")
		b.WriteString("Interview me until you can answer each of these, then write the prompt:\n")
	} else {
		b.WriteString("## Current prompt\n\n")
		b.WriteString(pr.Prompt)
		if len(pr.Prompt) > 0 && pr.Prompt[len(pr.Prompt)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteString("\n## Refinement checklist\n\n")
		b.WriteString("Review the current prompt above and work out which of these items it already\n")
		b.WriteString("settles. Ask me only about what I want changed or what the prompt genuinely\n")
		b.WriteString("leaves ambiguous:\n")
	}
	writeAuthoringChecklistItems(&b)

	b.WriteString("## How to apply your work\n\n")
	fmt.Fprintf(&b, "  - %s opens with a YAML frontmatter block fenced by `---` lines that\n", promptPath)
	b.WriteString("    carries this routine's settings (agents, effort only — no schedule); the\n")
	b.WriteString("    prompt itself is the body below the closing fence.\n")
	b.WriteString("  - Edit the prompt by rewriting the body below the fence directly; leave the\n")
	b.WriteString("    frontmatter block in place.\n")
	b.WriteString("  - Do NOT run git — pop never commits and neither should this session. When you\n")
	b.WriteString("    exit, control returns to the pop refinement menu, where I can fire a test run\n")
	b.WriteString("    and, if I like the result, commit the file myself.\n")

	return b.String()
}
