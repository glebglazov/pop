package routine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/prompt"
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
	spec, cfg, err := resolveAuthoringAgentSpec(d, agentOverride)
	if err != nil {
		fmt.Fprintf(out, "Could not resolve the authoring agent: %v\n", err)
		return
	}
	prompt := buildAuthoringPrompt(d, id, r)
	invocation, err := tasks.ResolveAgentAssistanceInvocation(d.taskDeps(), cfg, spec, "", prompt, r.Manifest.BoundDirectory)
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

// resolveAuthoringAgentSpec picks the attended agent override for a gate
// authoring session and loads the config the invocation resolves against. A
// refinement session is a human-facing session like any other, so it launches
// from [work.attended].agents rather than from the [work.routine].agents list
// scheduled Fires walk (ADR-0195); an explicit --agent still wins for the
// session. There is no headless quota fall-through — the human switches agents
// by hand if one is unavailable.
func resolveAuthoringAgentSpec(d *Deps, override string) (string, *config.Config, error) {
	cfg, err := d.LoadConfig()
	if err != nil {
		return "", nil, fmt.Errorf("load config: %w", err)
	}
	return strings.TrimSpace(override), cfg, nil
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

// isCreateModePrompt reports whether prompt.md is still unauthored: the
// scaffolded new stub, or blank/whitespace-only (ADR-0132). Anything else is
// revise mode.
func isCreateModePrompt(content string) bool {
	if strings.TrimSpace(content) == "" {
		return true
	}
	return content == promptStub
}

// buildAuthoringPrompt returns the front-loaded rules the authoring agent starts
// with (ADR-0125, ADR-0132): the routine framework contract, this routine's
// concrete paths, and either an interview checklist (create mode) or the
// current prompt plus an audit checklist (revise mode). It directs the agent
// to edit prompt.md directly but to change the schedule only through
// `pop routine edit --schedule` so the parser's validation is never bypassed.
func buildAuthoringPrompt(d *Deps, id string, r *Routine) string {
	dir := routineDir(d, id)
	memoryDir := filepath.Join(dir, memoryDirName)
	runsDir := filepath.Join(dir, runsDirName)

	// The frontmatter carries settings (ADR-0139); the authoring agent edits and
	// reasons about the body, so create-mode detection and the "current prompt"
	// echo both work from the body below the fence.
	_, promptBody, _ := readPromptFrontmatter(d, dir, id)
	createMode := isCreateModePrompt(promptBody)

	return prompt.MustRender(promptTemplates, "authoring.tmpl.md", authoringPromptView{
		ID:               id,
		QuotedID:         strconv.Quote(id),
		CreateMode:       createMode,
		ReviseMode:       !createMode,
		BoundDirectory:   r.Manifest.BoundDirectory,
		PromptPath:       filepath.Join(dir, promptFileName),
		MemoryDir:        memoryDir,
		RunsDir:          runsDir,
		ScheduleGrammar:  ScheduleGrammar,
		ScheduleLabel:    ScheduleLabel(r.Manifest.Schedule),
		Unscheduled:      !r.Manifest.IsScheduled(),
		PromptBody:       endWithNewline(promptBody),
		PromptNoun:       "your prompt.md",
		WrappedExample:   frameworkContractExample(memoryDir, runsDir),
	})
}

// authoringPromptView carries every path, label and mode decision the authoring
// template needs, so the template itself holds prose and section conditionals
// only (ADR-0208).
type authoringPromptView struct {
	ID       string
	QuotedID string

	// CreateMode and ReviseMode are the two halves of one decision, named
	// separately so the template selects a whole section by name rather than
	// negating a flag inline.
	CreateMode bool
	ReviseMode bool

	BoundDirectory  string
	PromptPath      string
	MemoryDir       string
	RunsDir         string
	ScheduleGrammar string
	ScheduleLabel   string
	Unscheduled     bool

	PromptBody string

	// PromptNoun and WrappedExample feed the shared framework-contract partial:
	// the noun each prompt uses for the file being authored, and the real
	// wrapper's output rendered with placeholders.
	PromptNoun     string
	WrappedExample string
}

// endWithNewline keeps an echoed prompt body from running into the heading that
// follows it when the body on disk has no final newline.
func endWithNewline(body string) string {
	if body == "" || strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}
