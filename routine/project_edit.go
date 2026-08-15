package routine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/internal/prompt"
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
	dataDir := projectRoutineDataDir(d, checkoutKey(pr.Dir), pr.Name)
	createMode := isCreateModePrompt(pr.Prompt)
	memoryDir := filepath.Join(dataDir, memoryDirName)
	runsDir := filepath.Join(dataDir, runsDirName)

	return prompt.MustRender(promptTemplates, "project-authoring.tmpl.md", projectAuthoringPromptView{
		QuotedID:       strconv.Quote(ProjectOrigin + pr.Name),
		CreateMode:     createMode,
		ReviseMode:     !createMode,
		CheckoutDir:    pr.Dir,
		PromptPath:     filepath.Join(pr.Dir, ".pop", projectRoutinesDirName, pr.Name+projectRoutineExt),
		MemoryDir:      memoryDir,
		RunsDir:        runsDir,
		PromptBody:     endWithNewline(pr.Prompt),
		PromptNoun:     "your prompt",
		WrappedExample: frameworkContractExample(memoryDir, runsDir),
	})
}

// projectAuthoringPromptView is the Project-routine counterpart of
// authoringPromptView: same shape, minus everything schedule-shaped, because a
// Project routine has no schedule to label or offer.
type projectAuthoringPromptView struct {
	QuotedID string

	CreateMode bool
	ReviseMode bool

	CheckoutDir string
	PromptPath  string
	MemoryDir   string
	RunsDir     string

	PromptBody string

	// The framework contract is one shared partial across both authoring
	// prompts (ADR-0208), so both views carry its two fields.
	PromptNoun     string
	WrappedExample string
}
