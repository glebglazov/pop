package tasks

import (
	"encoding/json"
	"strings"
)

// firstNonEmpty returns the first non-empty value, in argument order.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// collapseHint single-lines and truncates a raw tool-hint value to ~80 chars,
// the shared tail of every per-agent tool-hint probe.
func collapseHint(hint string) string {
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
}

// toolHint decodes raw tool-call JSON into a per-agent probe type P and
// returns pick(probe) collapsed to one line and truncated to 80 chars. It is
// shared by every agent's tool-hint probe; only P (the field list) and pick
// (the field probe order) differ per agent. A decode failure or empty input
// yields "", matching every prior per-agent implementation.
func toolHint[P any](input json.RawMessage, pick func(P) string) string {
	if len(input) == 0 {
		return ""
	}
	var probe P
	if err := json.Unmarshal(input, &probe); err != nil {
		return ""
	}
	return collapseHint(pick(probe))
}

// toolTick formats the shared compact "→ name hint" tick line rendered live
// for a tool invocation across every agent.
func toolTick(name, hint string) string {
	tick := "→ " + name
	if hint != "" {
		tick += " " + hint
	}
	return tick
}
