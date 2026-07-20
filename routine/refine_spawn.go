package routine

import (
	"fmt"
	"os"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
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
	r, err := loadManifest(d, routineID)
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
	session, dir := sessionAndDir(d, r.Manifest.BoundDirectory)
	command := refineCLICommand(exe, routineID, refineAgent)
	return spawnRefineWindow(tmuxDeps(d), session, dir, routineID, command)
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
// switched to with no send-keys.
func spawnRefineWindow(tmux deps.Tmux, session, dir, windowName, command string) error {
	if !tmux.HasSession(session) {
		if err := tmux.NewSession(session, dir); err != nil {
			return fmt.Errorf("create session %q: %w", session, err)
		}
	}

	windowTarget := session + ":" + windowName
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return fmt.Errorf("list windows in %q: %w", session, err)
	}
	windowExists := false
	for _, line := range splitLines(out) {
		if line == windowName {
			windowExists = true
			break
		}
	}

	if windowExists {
		paneOut, err := tmux.Command("list-panes", "-t", windowTarget, "-F", "#{pane_id}")
		if err != nil {
			return fmt.Errorf("list panes in %q: %w", windowTarget, err)
		}
		paneID := ""
		for _, line := range splitLines(paneOut) {
			if id := strings.TrimSpace(line); id != "" {
				paneID = id
				break
			}
		}
		if paneID == "" {
			return fmt.Errorf("window %q has no pane", windowTarget)
		}
		if _, err := tmux.Command("select-pane", "-t", paneID); err != nil {
			return fmt.Errorf("select refine pane: %w", err)
		}
		if _, err := tmux.Command("switch-client", "-t", paneID); err != nil {
			return fmt.Errorf("switch to refine window: %w", err)
		}
		return nil
	}

	out, err = tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", windowName, "-c", dir)
	if err != nil {
		return fmt.Errorf("create refine window in %q: %w", session, err)
	}
	paneID := strings.TrimSpace(out)
	if paneID == "" {
		return fmt.Errorf("create refine window in %q: tmux returned no pane id", session)
	}
	if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
		return fmt.Errorf("send refine command: %w", err)
	}
	return nil
}
