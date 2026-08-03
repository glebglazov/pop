package routine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

// RefinePaneWith spawns the whole refinement loop (`pop routine edit <id>`) into
// a tmux window named after the Routine (ADR-0132) and returns the pane holding
// it, so a caller that hands the operator off can focus it. The session is derived
// from the Routine's bound directory. When a window of that name already exists,
// the client switches to it and no command is sent — never typing into a live gate
// or agent. Outside tmux the call refuses and names the equivalent CLI command.
func RefinePaneWith(d *Deps, routineID, refineAgent string) (string, error) {
	if err := validateID(routineID); err != nil {
		return "", err
	}
	if d == nil {
		d = DefaultDeps()
	}
	inTmux := d.InTmux
	if inTmux == nil {
		inTmux = func() bool { return os.Getenv("TMUX") != "" }
	}
	if !inTmux() {
		return "", fmt.Errorf("refine requires tmux; run: %s", refineCLICommand("pop", routineID, refineAgent))
	}
	// A Project routine's pane runs in its checkout (ADR-0138); paneBoundDir
	// resolves either world's bound directory.
	boundDir, err := paneBoundDir(d, routineID)
	if err != nil {
		return "", err
	}
	exeFn := d.Executable
	if exeFn == nil {
		exeFn = os.Executable
	}
	exe, err := exeFn()
	if err != nil {
		return "", fmt.Errorf("resolve pop executable: %w", err)
	}
	session, dir := sessionAndDir(d, boundDir)
	command := refineCLICommand(exe, routineID, refineAgent)
	return spawnRoutineWindow(tmuxDeps(d), session, dir, refineWindowName(routineID), command)
}

// EditPromptPaneWith spawns `$EDITOR` on a Routine's prompt into a tmux window of
// its own and returns the pane holding it. The editor runs in a pane rather than
// suspending the caller because the Work dashboard hands off instead of execing
// (ADR-0158): the edit survives the dashboard exiting, and a window that already
// holds an editor is switched to rather than typed into.
func EditPromptPaneWith(d *Deps, routineID string) (string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	boundDir, err := paneBoundDir(d, routineID)
	if err != nil {
		return "", err
	}
	session, dir := sessionAndDir(d, boundDir)
	command := editorCommandLine(promptPathForEdit(d, routineID))
	return spawnRoutineWindow(tmuxDeps(d), session, dir, editWindowName(routineID), command)
}

// editorCommandLine is the shell command line that opens path in the operator's
// editor, read from $EDITOR and falling back to vi.
func editorCommandLine(path string) string {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	return editor + " " + shellQuote(path)
}

// promptPathForEdit resolves the file the edit-prompt verb opens: a Project
// routine's committed in-repo file, else the authored routine's data-dir
// prompt.md. A Project routine whose file cannot be resolved falls back to the
// authored path so the editor still opens something rather than nothing.
func promptPathForEdit(d *Deps, id string) string {
	if resolvesToProjectRoutine(d, id) {
		if path, err := projectRoutineFilePath(d, projectRoutineName(id)); err == nil {
			return path
		}
	}
	return filepath.Join(routineDir(d, id), promptFileName)
}

// editWindowName is the tmux window an edit-prompt pane lands in — the Routine's
// own window name with an `-edit` suffix, so an editor never shares a window with
// the refinement gate.
func editWindowName(routineID string) string {
	return refineWindowName(routineID) + "-edit"
}

// refineWindowName is the tmux window name a refine pane lands in. A Project
// routine's `project:<name>` id carries a `:`, which tmux would misread inside a
// `session:window` target, so it is folded to `_`. Authored ids are unchanged.
func refineWindowName(routineID string) string {
	if _, ok := parseProjectRef(routineID); ok {
		return strings.ReplaceAll(routineID, ":", "_")
	}
	return routineID
}

// refineCLICommand builds the shell command for the refinement loop.
func refineCLICommand(exe, routineID, refineAgent string) string {
	parts := []string{shellQuote(exe), "routine", "edit", shellQuote(routineID)}
	if agent := strings.TrimSpace(refineAgent); agent != "" {
		parts = append(parts, "--refine-agent", shellQuote(agent))
	}
	return strings.Join(parts, " ")
}

// spawnRoutineWindow creates the repo/routines session when absent and lands
// command in a window named after the Routine id. Existing windows are switched
// to with no send-keys — never typing into a live gate, agent or editor — and the
// pane is returned either way so the caller can hand the operator off to it.
func spawnRoutineWindow(tmux tmuxmod.Tmux, session, dir, windowName, command string) (string, error) {
	paneID, created, err := tmuxmod.EnsureWindow(tmux, session, windowName, dir)
	if err != nil {
		return "", err
	}
	if !created {
		if err := tmuxmod.FocusPane(tmux, paneID); err != nil {
			return "", err
		}
		return paneID, nil
	}
	if err := tmux.SendKeys(paneID, command, "Enter"); err != nil {
		return "", fmt.Errorf("send routine command: %w", err)
	}
	return paneID, nil
}
