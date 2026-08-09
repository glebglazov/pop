package tmux

import (
	"strings"
)

// ActivityPane is a pane that carries at least one Work-dashboard activity tag
// (@pop_set / @pop_verify / @pop_fold / @pop_assist), with its current foreground
// command. It is the unit of the live-pane affordance poll (ADR-0158).
type ActivityPane struct {
	Session string
	PaneID  string
	Command string
	// Set, Verify, Fold, Assist are the tag values written on the pane (a set
	// id). Empty means that tag is unset.
	Set, Verify, Fold, Assist string
}

// ListActivityPanes returns every pane across all sessions that carries at least
// one activity tag, with its current foreground command — one list-panes -a
// round-trip for the Work dashboard live-pane affordance (ADR-0158). An absent
// server is an empty list, not an error (ADR-0199 decision 8).
func (t *realTmux) ListActivityPanes() ([]ActivityPane, error) {
	format := strings.Join([]string{
		"#{session_name}",
		"#{pane_id}",
		"#{@pop_set}",
		"#{@pop_verify}",
		"#{@pop_fold}",
		"#{@pop_assist}",
		"#{pane_current_command}",
	}, "\t")
	out, err := t.run.output("list-panes", "-a", "-F", format)
	if err != nil {
		if absentServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var panes []ActivityPane
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 7)
		if len(parts) != 7 {
			continue
		}
		p := ActivityPane{
			Session: parts[0],
			PaneID:  parts[1],
			Set:     parts[2],
			Verify:  parts[3],
			Fold:    parts[4],
			Assist:  parts[5],
			Command: parts[6],
		}
		if p.Set == "" && p.Verify == "" && p.Fold == "" && p.Assist == "" {
			continue
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// IsBareShell reports whether pane_current_command names a login or interactive
// shell — the grey live-pane state where the tagged command has finished and the
// key should respawn rather than jump (ADR-0158).
func IsBareShell(cmd string) bool {
	c := strings.ToLower(strings.TrimSpace(cmd))
	c = strings.TrimPrefix(c, "-")
	switch c {
	case "bash", "zsh", "fish", "sh", "dash", "ksh", "tcsh", "csh":
		return true
	default:
		return false
	}
}
