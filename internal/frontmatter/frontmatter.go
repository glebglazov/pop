// Package frontmatter parses and serializes the YAML frontmatter that carries a
// routine's authored intent at the top of its prompt file (ADR-0139). Authored
// Routines and Project routines (ADR-0138) share this one shape, so the split
// between settings (frontmatter) and prompt (body) is expressed in a single
// reusable seam rather than duplicated per routine world.
package frontmatter

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// fence is the `---` line that opens and closes a frontmatter block.
const fence = "---"

// Fields are the routine intent fields carried in YAML frontmatter (ADR-0139):
// the optional schedule, the ordered runtime agent-preset list, and the effort
// tier. Project routines use the same shape minus schedule (they are manual-only
// by design), so an absent schedule is a normal, valid state — never an error.
type Fields struct {
	Schedule string   `yaml:"schedule,omitempty"`
	Agents   []string `yaml:"agents,omitempty"`
	Effort   string   `yaml:"effort,omitempty"`
}

// IsEmpty reports whether no intent field is set. An empty Fields serializes to
// a bare fence with no keys, and prompt files with no leading fence parse to it.
func (f Fields) IsEmpty() bool {
	return strings.TrimSpace(f.Schedule) == "" && len(f.Agents) == 0 && strings.TrimSpace(f.Effort) == ""
}

// Parse splits a prompt file into its frontmatter Fields and body. A file whose
// first line is not the `---` fence has no frontmatter: Fields are empty and the
// whole content is the body. A file that opens a fence but never closes it, or
// whose fenced block is not parseable YAML, is a hard error — the caller
// suspends only that routine with a warning rather than treating it as
// silently unscheduled (ADR-0139).
func Parse(content string) (Fields, string, error) {
	f, body, _, err := ParseWithKeys(content)
	return f, body, err
}

// ParseWithKeys is Parse plus the sorted set of top-level keys present in the
// frontmatter block. Callers that accept only a subset of the intent keys —
// Project routines take agents/effort only and treat a schedule (or any unknown
// key) as warn-and-ignore (ADR-0138) — use the key set to report what they
// dropped without re-implementing the fence scan.
func ParseWithKeys(content string) (Fields, string, []string, error) {
	yamlText, body, hadFence, err := split(content)
	if err != nil {
		return Fields{}, "", nil, err
	}
	if !hadFence {
		return Fields{}, body, nil, nil
	}
	f, err := decodeFields(yamlText)
	if err != nil {
		return Fields{}, "", nil, err
	}
	keys, err := topLevelKeys(yamlText)
	if err != nil {
		return Fields{}, "", nil, err
	}
	return f, body, keys, nil
}

// split scans the fenced frontmatter block, returning the raw YAML text between
// the fences and the body after the closing fence. hadFence is false when the
// content has no opening `---` fence (the whole content is the body).
func split(content string) (yamlText, body string, hadFence bool, err error) {
	first, rest, hadNewline := cutLine(content)
	if strings.TrimRight(first, "\r") != fence {
		// No opening fence: the entire file is the body.
		return "", content, false, nil
	}
	if !hadNewline {
		return "", "", false, fmt.Errorf("unterminated frontmatter: opening %q fence has no closing fence", fence)
	}

	var yamlLines []string
	remaining := rest
	for {
		line, next, hadNewline := cutLine(remaining)
		if strings.TrimRight(line, "\r") == fence {
			b := ""
			if hadNewline {
				b = next
			}
			return strings.Join(yamlLines, "\n"), b, true, nil
		}
		if !hadNewline {
			return "", "", false, fmt.Errorf("unterminated frontmatter: opening %q fence has no closing fence", fence)
		}
		yamlLines = append(yamlLines, line)
		remaining = next
	}
}

// topLevelKeys returns the sorted top-level mapping keys of the frontmatter
// block. An empty block has no keys. A non-mapping block is a parse error,
// consistent with decodeFields rejecting it.
func topLevelKeys(yamlText string) ([]string, error) {
	if strings.TrimSpace(yamlText) == "" {
		return nil, nil
	}
	var m map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &m); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// Marshal renders Fields and body back into a prompt file. The frontmatter fence
// is always emitted — even when Fields is empty — so the authored format is
// stable and discoverable to the humans and refinement agents that edit it.
// The body is written verbatim, so Marshal ∘ Parse round-trips.
func Marshal(f Fields, body string) (string, error) {
	var b strings.Builder
	b.WriteString(fence)
	b.WriteByte('\n')
	if !f.IsEmpty() {
		data, err := yaml.Marshal(f)
		if err != nil {
			return "", fmt.Errorf("encode frontmatter: %w", err)
		}
		b.Write(data) // yaml.Marshal already terminates with a newline.
	}
	b.WriteString(fence)
	b.WriteByte('\n')
	b.WriteString(body)
	return b.String(), nil
}

// decodeFields parses the YAML between the fences. An empty or whitespace-only
// block is a valid, field-less frontmatter, not a parse error.
func decodeFields(yamlText string) (Fields, error) {
	var f Fields
	if strings.TrimSpace(yamlText) == "" {
		return f, nil
	}
	if err := yaml.Unmarshal([]byte(yamlText), &f); err != nil {
		return Fields{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	return f, nil
}

// cutLine splits s at the first newline, returning the line (without the
// newline), the remainder after it, and whether a newline was present. When no
// newline is present the whole of s is the line and found is false.
func cutLine(s string) (line, rest string, found bool) {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}
