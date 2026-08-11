package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is the edit side of the override layer: what happens to the text a
// human hands back from $EDITOR, and what "copy the source down" writes
// (ADR-0202 decisions 2, 7 and 8). The Config dashboard drives it; nothing here
// knows about a terminal.
//
// The gate is stricter than the config loader on purpose. Pop's own validation
// is Finding-based and non-fatal (ADR-0054): a malformed agent entry becomes a
// finding while the list around it loads. That is right for a file a human
// wrote and wrong for a file pop writes — so a buffer that would produce a
// finding is refused here and never reaches the disk.

// overrideBufferLabel stands in for a file path in the findings this gate
// reuses. The load-time validators name the file they are judging, and the file
// this text would become is config.override.toml.
const overrideBufferLabel = "config.override.toml"

// StoreOverrideBuffer parses the text a human handed back from $EDITOR as the
// whole value of key and writes it to the override layer.
//
// The two returns are two different outcomes. A non-empty problem is something
// the human must fix — unparseable TOML, the wrong key, a value the schema
// refuses — and the caller re-opens the editor showing it; nothing is written.
// An error is a genuine failure of the write itself.
func StoreOverrideBuffer(key, buffer string) (string, error) {
	return StoreOverrideBufferWith(defaultDeps, key, buffer)
}

// StoreOverrideBufferWith is the injectable variant.
func StoreOverrideBufferWith(d *Deps, key, buffer string) (string, error) {
	value, problem := parseOverrideBuffer(key, buffer)
	if problem != "" {
		return problem, nil
	}
	return "", SetOverrideValueWith(d, key, value)
}

// CopyOverrideFromSource writes the value the layers below the override still
// say as the override itself, so a human can start from what is in force
// without opening an editor — and so removing an override is one keystroke away
// from being undone (ADR-0202 decision 6).
func CopyOverrideFromSource(configPath, key string) error {
	return CopyOverrideFromSourceWith(defaultDeps, configPath, key)
}

// CopyOverrideFromSourceWith is the injectable variant. The source is the
// hand-authored value where there is one and the built-in default otherwise;
// where no layer at all defines the key, it is that key's empty value, which is
// what the preview already renders for it. For the two agent lists that fall
// through when empty, copying that emptiness down is a real value and disables
// the fallthrough — the same thing typing `agents = []` into the editor does.
func CopyOverrideFromSourceWith(d *Deps, configPath, key string) error {
	if _, exposed := OverrideKeyScope(key); !exposed {
		return fmt.Errorf("config key %q is not exposed for overriding", key)
	}
	layers, err := overrideValueLayers(d, configPath)
	if err != nil {
		return err
	}
	// Layer 0 is the override itself; the source is what would be in force with
	// that layer gone.
	value, idx := topmostValue(key, layers[1:])
	if idx < 0 {
		value = zeroTOMLValue(globalKeyTypes()[key])
	}
	return SetOverrideValueWith(d, key, value)
}

// parseOverrideBuffer turns one editor buffer into the value to store, or into
// the problem to show the human. It returns the value read out of a generic TOML
// document rather than off the decoded schema, because an override is stored as
// the key's whole value and the schema decode exists only to judge it.
func parseOverrideBuffer(key, buffer string) (any, string) {
	if _, exposed := OverrideKeyScope(key); !exposed {
		return nil, fmt.Sprintf("%s is not a key pop can override.", key)
	}
	if strings.TrimSpace(buffer) == "" {
		return nil, fmt.Sprintf("The buffer is empty; it has to set %s.", key)
	}

	doc := map[string]any{}
	if _, err := toml.Decode(buffer, &doc); err != nil {
		return nil, fmt.Sprintf("This is not valid TOML: %v", err)
	}
	value, ok := documentValue(doc, key)
	if !ok {
		return nil, fmt.Sprintf("The buffer has to set %s, and it sets no such key.", key)
	}
	if extra := documentKeysBesides(doc, key); len(extra) > 0 {
		return nil, fmt.Sprintf(
			"The buffer also sets %s. One buffer overrides one key: %s.",
			strings.Join(extra, ", "), key)
	}
	if problem := overrideSchemaProblem(key, buffer); problem != "" {
		return nil, problem
	}
	return value, ""
}

// documentKeysBesides lists every key the buffer sets other than the one being
// edited. The buffer carries its own key so the editor never has to infer what
// came back (ADR-0202 decision 7); the other half of that bargain is that a
// buffer which quietly sets a second key is refused rather than half-applied.
func documentKeysBesides(doc map[string]any, key string) []string {
	var extra []string
	for _, leaf := range documentLeafKeys(doc, "") {
		if leaf != key {
			extra = append(extra, leaf)
		}
	}
	return extra
}

// documentLeafKeys walks a generic TOML document to the dotted path of every
// value it sets. A table with nothing in it is a leaf of its own: `[work.verify]`
// alone still names a key the buffer touched.
func documentLeafKeys(doc map[string]any, prefix string) []string {
	var keys []string
	for name, value := range doc {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if table, ok := value.(map[string]any); ok && len(table) > 0 {
			keys = append(keys, documentLeafKeys(table, path)...)
			continue
		}
		keys = append(keys, path)
	}
	sort.Strings(keys)
	return keys
}

// overrideSchemaProblem decodes the buffer against the config schema and reports
// the first finding that speaks about this key, in the words a load would use.
// Reusing the load-time validators is the point: the gate refuses exactly what
// pop would later complain about, so the two can never disagree about what a
// good value is.
func overrideSchemaProblem(key, buffer string) string {
	var cfg Config
	md, err := toml.Decode(buffer, &cfg)
	if err != nil {
		return fmt.Sprintf("%s cannot hold this value: %v", key, err)
	}
	for _, finding := range overrideBufferFindings(&cfg, md) {
		if !findingSpeaksAbout(finding, key) {
			continue
		}
		return "Loading this value would report: " + finding.Message
	}
	return ""
}

// overrideBufferFindings runs the load-time validators that judge a *value*.
// The ones left out judge a file's shape instead — retired sections, renamed
// blocks, deprecated spellings — and a buffer that sets one key has no file
// shape to judge.
func overrideBufferFindings(cfg *Config, md toml.MetaData) []Finding {
	findings := agentEntryFindings(overrideBufferLabel, cfg)
	findings = append(findings, projectEntryFindings(overrideBufferLabel, cfg.Projects)...)
	findings = append(findings, effortConfigFindings(overrideBufferLabel, md)...)
	findings = append(findings, agentConfigFindings(overrideBufferLabel, md)...)
	findings = append(findings, integrationsSkillsFindings(overrideBufferLabel, cfg.Integrations, md)...)
	findings = append(findings, workViewPresetFindings(overrideBufferLabel, cfg)...)
	workbench, _ := workbenchFindings(overrideBufferLabel, cfg.Workbenches)
	findings = append(findings, workbench...)
	findings = append(findings, worktreeDisplayFindings(overrideBufferLabel, cfg.projectConfig())...)
	return findings
}

// findingSpeaksAbout reports whether a finding is about the key being edited —
// the key itself, an element of it, or something inside it. Findings about
// anything else belong to config the buffer did not set and are none of this
// gate's business.
func findingSpeaksAbout(f Finding, key string) bool {
	return f.Path == key ||
		strings.HasPrefix(f.Path, key+".") ||
		strings.HasPrefix(f.Path, key+"[")
}

// zeroTOMLValue is the empty value of a TOML type, the value side of the literal
// zeroTOMLLiteral renders.
func zeroTOMLValue(tomlType string) any {
	switch tomlType {
	case "array", "array of tables":
		return []any{}
	case "table":
		return map[string]any{}
	case "boolean":
		return false
	case "integer":
		return int64(0)
	case "float":
		return float64(0)
	default:
		return ""
	}
}
