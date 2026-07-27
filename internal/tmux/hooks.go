package tmux

import "strings"

// Hook is one installed global tmux hook: its indexed selector (e.g.
// "after-select-pane[0]", the token used to unset exactly this entry) and its
// command body. Callers decide which hooks are theirs by inspecting Command;
// the indexed selector parsing lives here.
type Hook struct {
	Index   string
	Command string
}

// InstallHook appends a global hook for event (set-hook -ga, so multiple hooks
// on one event coexist).
func (t *realTmux) InstallHook(event, command string) error {
	_, err := t.run.output("set-hook", "-ga", event, command)
	return err
}

// UninstallHook removes the global hook at the given indexed selector
// (set-hook -gu "event[N]").
func (t *realTmux) UninstallHook(indexed string) error {
	_, err := t.run.output("set-hook", "-gu", indexed)
	return err
}

// GlobalHooks reads every installed global hook, parsed into typed entries.
func (t *realTmux) GlobalHooks() ([]Hook, error) {
	out, err := t.run.output("show-hooks", "-g")
	if err != nil {
		return nil, err
	}
	return parseHooks(out), nil
}

// parseHooks turns show-hooks output into typed entries. Each line reads
// "event[N] command..."; a line without the "]" delimiter is skipped.
func parseHooks(out string) []Hook {
	var hooks []Hook
	for _, line := range strings.Split(out, "\n") {
		bracketEnd := strings.Index(line, "]")
		if bracketEnd == -1 {
			continue
		}
		hooks = append(hooks, Hook{
			Index:   line[:bracketEnd+1],
			Command: strings.TrimSpace(line[bracketEnd+1:]),
		})
	}
	return hooks
}
