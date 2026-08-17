package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is the gate every value passes on its way into the override layer,
// at either scope (ADR-0202 decision 8, ADR-0212 decision 6). The Config
// dashboard is the only *interactive* writer of the layer, not the only writer:
// `--trunk`, `--no-<component>`, `pop config repo set` and `pop workbench
// prefer` state intent with no terminal to open an editor in. What the model
// forbids is a second destination — and a second gate would be one in all but
// name, because a value only one door refuses is a value pop can still write.
// So the gate sits on the write itself: no entry point of the layer can skip it.
//
// It is stricter than the config loader on purpose. Pop's own validation is
// Finding-based and non-fatal (ADR-0054): a malformed agent entry becomes a
// finding while the list around it loads. That is right for a file a human wrote
// and wrong for a file pop writes — so a value that would produce a finding is
// refused here and never reaches the disk.
//
// A refusal is a sentence rather than an error because the editor front-end
// shows it and re-opens the buffer on it. The write entry points turn the same
// sentence into an error for the front-ends with no buffer to go back to.

// overrideBufferLabel stands in for a file path in the findings this gate
// reuses. The load-time validators name the file they are judging, and the file
// this text would become is config.override.toml.
const overrideBufferLabel = "config.override.toml"

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
	findings = append(findings, spendModelRateFindings(cfg, md)...)
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

// OverrideValueProblem reports why value cannot be stored as the whole value of
// a global config key, in the words a load of that value would use, and "" when
// it can. The value is judged as the config it would become: it is rendered as
// the `key = value` statement the editor seeds and decoded through the schema,
// so what a scripted writer may state and what a human may type are the same set.
func OverrideValueProblem(key string, value any) string {
	if !IsOverridableKey(key) {
		return fmt.Sprintf("%s is not a key pop can override.", key)
	}
	return overrideSchemaProblem(key, renderKeyTOML(key, value))
}

// RepoOverrideValueProblem is the repository-scope twin: why value cannot be
// stated for one leaf of a [repo] block, and "" when it can. key is the leaf
// ("turn_cap") or the row spelling the dashboard addresses it by
// ("repo.turn_cap"); the sentence names it back the way the caller said it.
func RepoOverrideValueProblem(key string, value any) string {
	leaf := strings.TrimPrefix(key, repoScopeKeyPrefix)
	if !repoBlockLegalKeys()[leaf] {
		return fmt.Sprintf("%s is not a key pop can override.", key)
	}
	block, err := decodeRepoOverrideBlock(map[string]any{leaf: value})
	if err != nil {
		return fmt.Sprintf("%s %v", key, err)
	}
	for _, finding := range repoBlockFindings(block) {
		if !findingSpeaksAbout(finding, leaf) {
			continue
		}
		return "Loading this value would report: " + finding.Message
	}
	return ""
}

// repoBlockFindings runs the load-time validators that judge a value a [repo]
// block can hold. Only the blueprint library has any: the other repo-scope keys
// are scalars whose whole judgement is whether the block decodes them at all.
func repoBlockFindings(block RepoOverrideConfig) []Finding {
	findings, _ := workbenchFindings(overrideBufferLabel, block.Workbenches)
	return findings
}

// decodeRepoOverrideBlock re-encodes a block and decodes it through the struct
// that decodes a hand-authored [repo."<path>"] block, so a value the config
// could not read back is refused at the write rather than ignored at the next
// load. The two surfaces share one shape (ADR-0212 decision 7), so this is the
// same judgement config.toml would pass on the same text.
func decodeRepoOverrideBlock(block map[string]any) (RepoOverrideConfig, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(block); err != nil {
		return RepoOverrideConfig{}, fmt.Errorf("cannot be written as config: %w", err)
	}
	var probe RepoOverrideConfig
	if _, err := toml.Decode(buf.String(), &probe); err != nil {
		return RepoOverrideConfig{}, fmt.Errorf("is not a value this key can hold: %w", err)
	}
	return probe, nil
}

// overrideRefusal turns a gate sentence into the error a writer with no editor
// reports. The sentence is the same one the dashboard shows, so a scripted
// writer and an interactive one refuse a bad value in the same words.
func overrideRefusal(problem string) error {
	return fmt.Errorf("%s", strings.TrimSuffix(problem, "."))
}

// unknownRepoOverrideKeyError refuses a key that has no repository home, naming
// the ones that do so the caller does not have to go looking. It is the gate's
// unknown-key sentence in the fuller words a command line can carry.
func unknownRepoOverrideKeyError(key string) error {
	legal := repoBlockLegalKeys()
	names := make([]string, 0, len(legal))
	for name := range legal {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("config key %q has no repository scope; repo-scope keys: %s",
		key, strings.Join(names, ", "))
}
