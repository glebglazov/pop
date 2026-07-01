package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ClearRuntimeIntegrations removes the [integrations] section from
// config.runtime.toml so merged config re-inherits embedded defaults unless
// the user config solidifies a smaller list (ADR 0065 bare integrate).
func ClearRuntimeIntegrations() error {
	return ClearRuntimeIntegrationsWith(defaultDeps)
}

// ClearRuntimeIntegrationsWith removes runtime [integrations] overrides.
func ClearRuntimeIntegrationsWith(d *Deps) error {
	doc, _, err := loadRuntimeDocument(d)
	if err != nil {
		return err
	}
	if len(doc) == 0 {
		return nil
	}
	delete(doc, "integrations")
	return saveRuntimeDocument(d, doc)
}

// RemoveRuntimeIntegrationSkills removes the given Integration skill aliases
// from the runtime layer's skills list. When the runtime file is absent, the
// embedded defaults are the starting baseline. Writes config.runtime.toml
// atomically; deletes the file when nothing remains.
func RemoveRuntimeIntegrationSkills(aliases ...string) error {
	return RemoveRuntimeIntegrationSkillsWith(defaultDeps, aliases...)
}

// RemoveRuntimeIntegrationSkillsWith is the injectable variant.
func RemoveRuntimeIntegrationSkillsWith(d *Deps, aliases ...string) error {
	if len(aliases) == 0 {
		return nil
	}
	doc, md, err := loadRuntimeDocument(d)
	if err != nil {
		return err
	}
	skills := runtimeSkillsBaseline(doc, md)
	skills = removeIntegrationSkillAliases(skills, aliases)
	setRuntimeIntegrationsSkills(doc, skills)
	return saveRuntimeDocument(d, doc)
}

// RuntimePreferredWorkbench reads the Preferred workbench stored in
// config.runtime.toml under [workbench.preferred], keyed by the exact worktree
// path (ADR-0078). The value is three-valued: absent (present=false), a name
// (present=true, name != ""), or explicit none (present=true, name == "") —
// the last meaning "flat/prompt here", distinct from absent.
func RuntimePreferredWorkbench(path string) (name string, present bool, err error) {
	return RuntimePreferredWorkbenchWith(defaultDeps, path)
}

// RuntimePreferredWorkbenchWith is the injectable variant.
func RuntimePreferredWorkbenchWith(d *Deps, path string) (string, bool, error) {
	doc, _, err := loadRuntimeDocument(d)
	if err != nil {
		return "", false, err
	}
	preferred := runtimePreferredTable(doc)
	if preferred == nil {
		return "", false, nil
	}
	raw, ok := preferred[path]
	if !ok {
		return "", false, nil
	}
	name, _ := raw.(string)
	return name, true, nil
}

// SetRuntimePreferredWorkbench records name as the Preferred workbench for the
// exact worktree path in config.runtime.toml [workbench.preferred] (ADR-0078).
// An empty name is the explicit-none sentinel ("flat/prompt here"), stored
// distinctly from an absent entry. Writes the file atomically and creates the
// [workbench.preferred] table as needed.
func SetRuntimePreferredWorkbench(path, name string) error {
	return SetRuntimePreferredWorkbenchWith(defaultDeps, path, name)
}

// SetRuntimePreferredWorkbenchWith is the injectable variant.
func SetRuntimePreferredWorkbenchWith(d *Deps, path, name string) error {
	doc, _, err := loadRuntimeDocument(d)
	if err != nil {
		return err
	}
	setRuntimePreferred(doc, path, name)
	return saveRuntimeDocument(d, doc)
}

// ClearRuntimePreferredWorkbench removes any Preferred workbench entry for the
// exact worktree path ("reset to default"). Prunes the now-empty
// [workbench.preferred] / [workbench] tables and deletes config.runtime.toml
// when nothing remains.
func ClearRuntimePreferredWorkbench(path string) error {
	return ClearRuntimePreferredWorkbenchWith(defaultDeps, path)
}

// ClearRuntimePreferredWorkbenchWith is the injectable variant.
func ClearRuntimePreferredWorkbenchWith(d *Deps, path string) error {
	doc, _, err := loadRuntimeDocument(d)
	if err != nil {
		return err
	}
	preferred := runtimePreferredTable(doc)
	if preferred == nil {
		return nil
	}
	if _, ok := preferred[path]; !ok {
		return nil
	}
	delete(preferred, path)
	pruneRuntimePreferred(doc)
	return saveRuntimeDocument(d, doc)
}

// runtimePreferredTable returns the decoded [workbench.preferred] table, or nil
// when the runtime document has no such table.
func runtimePreferredTable(doc map[string]any) map[string]any {
	workbench, ok := doc["workbench"].(map[string]any)
	if !ok || workbench == nil {
		return nil
	}
	preferred, ok := workbench["preferred"].(map[string]any)
	if !ok {
		return nil
	}
	return preferred
}

// setRuntimePreferred sets doc[workbench][preferred][path] = name, creating the
// intermediate tables on demand.
func setRuntimePreferred(doc map[string]any, path, name string) {
	workbench, ok := doc["workbench"].(map[string]any)
	if !ok || workbench == nil {
		workbench = map[string]any{}
		doc["workbench"] = workbench
	}
	preferred, ok := workbench["preferred"].(map[string]any)
	if !ok || preferred == nil {
		preferred = map[string]any{}
		workbench["preferred"] = preferred
	}
	preferred[path] = name
}

// pruneRuntimePreferred drops the [workbench.preferred] table and its parent
// [workbench] table once they empty out, so saveRuntimeDocument can remove the
// file when the whole document empties (delete-file-when-empty).
func pruneRuntimePreferred(doc map[string]any) {
	workbench, ok := doc["workbench"].(map[string]any)
	if !ok || workbench == nil {
		return
	}
	if preferred, ok := workbench["preferred"].(map[string]any); ok && len(preferred) == 0 {
		delete(workbench, "preferred")
	}
	if len(workbench) == 0 {
		delete(doc, "workbench")
	}
}

func loadRuntimeDocument(d *Deps) (map[string]any, toml.MetaData, error) {
	path := DefaultRuntimeConfigPathWith(d)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, toml.MetaData{}, nil
		}
		return nil, toml.MetaData{}, fmt.Errorf("read runtime config %q: %w", path, err)
	}
	var doc map[string]any
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return nil, toml.MetaData{}, fmt.Errorf("parse runtime config %q: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, md, nil
}

func runtimeSkillsBaseline(doc map[string]any, md toml.MetaData) []string {
	if md.IsDefined("integrations", "skills") {
		return integrationSkillsFromDocument(doc)
	}
	return append([]string(nil), DefaultIntegrationSkills...)
}

func integrationSkillsFromDocument(doc map[string]any) []string {
	integrations, ok := doc["integrations"].(map[string]any)
	if !ok || integrations == nil {
		return nil
	}
	raw, ok := integrations["skills"].([]any)
	if !ok {
		return nil
	}
	skills := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			continue
		}
		skills = append(skills, s)
	}
	return skills
}

func removeIntegrationSkillAliases(skills []string, aliases []string) []string {
	remove := make(map[string]bool, len(aliases))
	for _, alias := range aliases {
		remove[alias] = true
	}
	out := skills[:0]
	for _, skill := range skills {
		if !remove[skill] {
			out = append(out, skill)
		}
	}
	return out
}

func setRuntimeIntegrationsSkills(doc map[string]any, skills []string) {
	raw := make([]any, len(skills))
	for i, skill := range skills {
		raw[i] = skill
	}
	doc["integrations"] = map[string]any{"skills": raw}
}

func saveRuntimeDocument(d *Deps, doc map[string]any) error {
	path := DefaultRuntimeConfigPathWith(d)
	if len(doc) == 0 {
		if err := d.FS.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove runtime config %q: %w", path, err)
		}
		return nil
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode runtime config: %w", err)
	}
	return writeRuntimeConfigAtomic(d, path, data)
}

func writeRuntimeConfigAtomic(d *Deps, path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create runtime config dir: %w", err)
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf(".config.runtime.tmp-%d", os.Getpid()))
	if err := d.FS.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write runtime config temp file: %w", err)
	}
	if err := d.FS.Rename(tmpPath, path); err != nil {
		_ = d.FS.RemoveAll(tmpPath)
		return fmt.Errorf("commit runtime config: %w", err)
	}
	return nil
}

// RuntimeIntegrationsSkills reads the skills list stored in config.runtime.toml
// without merge. The bool is false when the runtime file or integrations.skills
// key is absent.
func RuntimeIntegrationsSkills() ([]string, bool, error) {
	return RuntimeIntegrationsSkillsWith(defaultDeps)
}

// RuntimeIntegrationsSkillsWith is the injectable variant.
func RuntimeIntegrationsSkillsWith(d *Deps) ([]string, bool, error) {
	doc, md, err := loadRuntimeDocument(d)
	if err != nil {
		return nil, false, err
	}
	if !md.IsDefined("integrations", "skills") {
		return nil, false, nil
	}
	skills := integrationSkillsFromDocument(doc)
	return skills, true, nil
}
