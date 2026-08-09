package config

import (
	"fmt"
	"strings"
)

// AgentEntry is one entry in a Work group's agent list (ADR-0194 decision 5):
// one entry type, everywhere, as a table. A bare string is sugar for
// { cmd = "<string>" }, so the two spellings below decode identically:
//
//	agents = ["claude --model opus"]
//	agents = [{ display_name = "Claude Usual", cmd = "claude --model opus" }]
//
// Cmd is the agent-CLI command in full — the preset name plus whatever
// arguments augment it, passed through to the agent untouched.
type AgentEntry struct {
	// DisplayName is what a human-facing surface calls this entry. Empty means
	// the surface falls back to the command itself.
	DisplayName string `toml:"display_name" desc:"Human-facing name for this entry (defaults to the command)."`
	// Cmd is the agent-CLI command this entry stands for.
	Cmd string `toml:"cmd" desc:"Agent-CLI command, e.g. \"claude --model opus\"."`
	// problem records why this entry failed to decode. Per ADR-0054 a malformed
	// entry keeps its position and is reported as a config finding rather than
	// aborting the whole load or vanishing silently.
	problem string
}

// Problem returns the decode problem for a malformed entry, or "" when the
// entry decoded cleanly.
func (e AgentEntry) Problem() string { return e.problem }

// Valid reports whether the entry decoded cleanly and names a command.
func (e AgentEntry) Valid() bool {
	return e.problem == "" && strings.TrimSpace(e.Cmd) != ""
}

// AgentEntries is one Work group's ordered agent list.
type AgentEntries []AgentEntry

// Commands returns the commands of the entries that decoded cleanly, in
// configured order. It is what every caller that only needs agent specs reads,
// so the entry type is invisible to the run paths.
func (entries AgentEntries) Commands() []string {
	var out []string
	for _, entry := range entries {
		if entry.Valid() {
			out = append(out, strings.TrimSpace(entry.Cmd))
		}
	}
	return out
}

// AgentEntriesFromCommands builds an agent list from bare commands, the same
// shape a string-only TOML list decodes to.
func AgentEntriesFromCommands(cmds ...string) AgentEntries {
	entries := make(AgentEntries, 0, len(cmds))
	for _, cmd := range cmds {
		entries = append(entries, AgentEntry{Cmd: strings.TrimSpace(cmd)})
	}
	return entries
}

// UnmarshalTOML decodes an agent list as a mixed string-or-table array. Like
// TopicSteps, both decoder shapes are handled: []interface{} for a mixed or
// inline-table array, and []map[string]interface{} for the all-tables case that
// [[work.<kind>.agents]] blocks produce. Per-entry problems are recorded on the
// entry instead of failing the decode (ADR-0054); only a non-array value is an
// error, since then there is no list to keep positions in.
func (entries *AgentEntries) UnmarshalTOML(v interface{}) error {
	var arr []interface{}
	switch val := v.(type) {
	case []interface{}:
		arr = val
	case []map[string]interface{}:
		arr = make([]interface{}, 0, len(val))
		for _, item := range val {
			arr = append(arr, item)
		}
	default:
		return fmt.Errorf("agents must be an array, got %T", v)
	}
	decoded := make(AgentEntries, 0, len(arr))
	for _, item := range arr {
		decoded = append(decoded, decodeAgentEntry(item))
	}
	*entries = decoded
	return nil
}

func decodeAgentEntry(v interface{}) AgentEntry {
	switch val := v.(type) {
	case string:
		cmd := strings.TrimSpace(val)
		if cmd == "" {
			return AgentEntry{problem: "entry is an empty command"}
		}
		return AgentEntry{Cmd: cmd}
	case map[string]interface{}:
		var entry AgentEntry
		if raw, ok := val["display_name"]; ok {
			s, ok := raw.(string)
			if !ok {
				return AgentEntry{problem: fmt.Sprintf("display_name must be a string, got %T", raw)}
			}
			entry.DisplayName = strings.TrimSpace(s)
		}
		if raw, ok := val["cmd"]; ok {
			s, ok := raw.(string)
			if !ok {
				return AgentEntry{problem: fmt.Sprintf("cmd must be a string, got %T", raw)}
			}
			entry.Cmd = strings.TrimSpace(s)
		}
		if entry.Cmd == "" {
			entry.problem = "entry table has no cmd"
		}
		return entry
	default:
		return AgentEntry{problem: fmt.Sprintf("entry must be a string or table, got %T", v)}
	}
}

// agentEntryFindings reports every malformed Work-group agent entry, naming the
// group and the entry's position in the configured list.
func agentEntryFindings(path string, cfg *Config) []Finding {
	if cfg == nil || cfg.Work == nil {
		return nil
	}
	groups := []struct {
		name    string
		entries AgentEntries
	}{}
	if cfg.Work.Implement != nil {
		groups = append(groups, struct {
			name    string
			entries AgentEntries
		}{"work.implement", cfg.Work.Implement.Agents})
	}
	if cfg.Work.Verify != nil {
		groups = append(groups, struct {
			name    string
			entries AgentEntries
		}{"work.verify", cfg.Work.Verify.Agents})
	}
	if cfg.Work.Routine != nil {
		groups = append(groups, struct {
			name    string
			entries AgentEntries
		}{"work.routine", cfg.Work.Routine.Agents})
	}
	if cfg.Work.Attended != nil {
		groups = append(groups, struct {
			name    string
			entries AgentEntries
		}{"work.attended", cfg.Work.Attended.Agents})
	}

	var findings []Finding
	for _, group := range groups {
		for i, entry := range group.entries {
			if entry.problem == "" {
				continue
			}
			findings = append(findings, Finding{
				Path: fmt.Sprintf("%s.agents[%d]", group.name, i),
				Message: fmt.Sprintf("%s: [%s] agents entry %d is malformed (%s); it is skipped",
					path, group.name, i+1, entry.problem),
			})
		}
	}
	return findings
}
