package routine

import (
	"fmt"
	"os"
	"strings"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

// RefinePaneWith spawns the whole refinement loop (`pop routine edit <id>`) into
// a tmux window named after the Routine (ADR-0132). The session is derived from
// the Routine's bound directory. When a window of that name already exists, the
// client switches to it and no command is sent — never typing into a live gate
// or agent. Outside tmux the call refuses and names the equivalent CLI command.
func RefinePaneWith(d *Deps, routineID, refineAgent string) error {
	if err := validateID(routineID); err != nil {
		return err
	}
	if d == nil {
		d = DefaultDeps()
	}
	inTmux := d.InTmux
	if inTmux == nil {
		inTmux = func() bool { return os.Getenv("TMUX") != "" }
	}
	if !inTmux() {
		return fmt.Errorf("refine requires tmux; run: %s", refineCLICommand("pop", routineID, refineAgent))
	}
	// A Project routine's pane runs in its checkout (ADR-0138); paneBoundDir
	// resolves either world's bound directory.
	boundDir, err := paneBoundDir(d, routineID)
	if err != nil {
		return err
	}
	exeFn := d.Executable
	if exeFn == nil {
		exeFn = os.Executable
	}
	exe, err := exeFn()
	if err != nil {
		return fmt.Errorf("resolve pop executable: %w", err)
	}
	session, dir := sessionAndDir(d, boundDir)
	command := refineCLICommand(exe, routineID, refineAgent)
	return spawnRefineWindow(tmuxDeps(d), session, dir, refineWindowName(routineID), command)
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

// spawnRefineWindow creates the repo/routines session when absent and lands the
// refine command in a window named after the Routine id. Existing windows are
// switched to with no send-keys — never typing into a live gate or agent.
func spawnRefineWindow(tmux tmuxmod.Tmux, session, dir, windowName, command string) error {
	paneID, created, err := tmuxmod.EnsureWindow(tmux, session, windowName, dir)
	if err != nil {
		return err
	}
	if !created {
		return tmuxmod.FocusPane(tmux, paneID)
	}
	if err := tmux.SendKeys(paneID, command, "Enter"); err != nil {
		return fmt.Errorf("send refine command: %w", err)
	}
	return nil
}
