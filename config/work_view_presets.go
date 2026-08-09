package config

import (
	"fmt"
	"strings"
	"time"
)

// Archived-row modes for a Work view preset (ADR-0197). Empty means unset, which
// resolves to ArchivedExclude at evaluation time.
const (
	ArchivedExclude = "exclude"
	ArchivedInclude = "include"
	ArchivedOnly    = "only"
)

// Sort modes a Work view preset may declare. Absent sort leaves the ADR-0121
// status scheme in place under the membership tiers.
const (
	PresetSortCreatedDesc = "created_desc"
	PresetSortCreatedAsc  = "created_asc"
)

var (
	validArchivedModes = map[string]bool{
		ArchivedExclude: true,
		ArchivedInclude: true,
		ArchivedOnly:    true,
	}
	validPresetSorts = map[string]bool{
		PresetSortCreatedDesc: true,
		PresetSortCreatedAsc:  true,
	}
	presetEntryKeys = map[string]bool{
		"name": true, "label": true, "status": true, "unfolded": true,
		"archived": true, "created_within": true, "sort": true, "hide": true,
		"system": true,
	}
	presetFilterKeys = map[string]bool{
		"status": true, "unfolded": true, "archived": true,
		"created_within": true, "sort": true,
	}
)

// WorkViewPresetFilter is the closed declarative field set a preset (or its
// one-level hide clause) may carry. Fields AND-combine; hide subtracts its
// match from the result (ADR-0197).
type WorkViewPresetFilter struct {
	Status        []string `toml:"status,omitempty" desc:"Status values to match (array)."`
	Unfolded      *bool    `toml:"unfolded,omitempty" desc:"Match unfolded (true) or folded (false) rows."`
	Archived      string   `toml:"archived,omitempty" desc:"Archived-row mode: exclude|include|only (default exclude)."`
	CreatedWithin string   `toml:"created_within,omitempty" desc:"Only rows whose id date prefix falls within this duration."`
	Sort          string   `toml:"sort,omitempty" desc:"Row sort under the membership tiers: created_desc|created_asc."`
}

// WorkViewPreset is one Work view preset entry at [[work.dashboard.tasks.presets]]
// (ADR-0197). A { system = "<name>" } reference carries only System until
// ResolveWorkViewPresets expands it to pop's shipped definition.
type WorkViewPreset struct {
	Name   string `toml:"name" desc:"Preset name (required unless system is set)."`
	Label  string `toml:"label,omitempty" desc:"Optional display label (defaults to name)."`
	System string `toml:"system,omitempty" desc:"Reference a shipped preset by name instead of declaring fields."`
	WorkViewPresetFilter
	Hide *WorkViewPresetFilter `toml:"hide,omitempty" desc:"Subtract rows matching this conjunction (one level only)."`

	// Number is the 1-based position in the resolved roster. Positional: the
	// first entry is the default, and 1–9 map to digit shortcuts.
	Number int `toml:"-"`

	// problems records non-fatal decode issues for this entry (ADR-0054).
	problems []string
}

// DisplayLabel returns the preset's human-facing label, falling back to Name
// when Label is empty (ADR-0197).
func (p WorkViewPreset) DisplayLabel() string {
	if s := strings.TrimSpace(p.Label); s != "" {
		return s
	}
	return p.Name
}

// WorkDashboardConfig holds [work.dashboard] settings for Work read surfaces.
type WorkDashboardConfig struct {
	Tasks *WorkDashboardTasksConfig `toml:"tasks" include:"fields" desc:"Task-set view settings ([work.dashboard.tasks] table)."`
}

// WorkDashboardTasksConfig holds [work.dashboard.tasks] settings.
type WorkDashboardTasksConfig struct {
	// Presets is the Work view preset roster. Absent (nil) ⇒ the shipped
	// roster; a declared list (including empty) replaces it wholesale
	// (include:"replace", ADR-0197).
	Presets []WorkViewPreset `toml:"presets" include:"replace" desc:"Work view presets; a declared list replaces the shipped roster wholesale."`
}

// ShippedWorkViewPresets returns pop's built-in Work view preset roster, written
// entirely in the declarative vocabulary (ADR-0197 decision 2). The first entry
// is the default.
func ShippedWorkViewPresets() []WorkViewPreset {
	folded := false
	unfolded := true
	return []WorkViewPreset{
		{
			Name: "active",
			Hide: &WorkViewPresetFilter{
				Status:   []string{"done"},
				Unfolded: &folded,
			},
		},
		{
			Name: "unfolded",
			WorkViewPresetFilter: WorkViewPresetFilter{
				Unfolded: &unfolded,
			},
		},
		{
			Name: "recent-7d",
			WorkViewPresetFilter: WorkViewPresetFilter{
				CreatedWithin: "168h",
				Sort:          PresetSortCreatedDesc,
			},
		},
		{
			Name: "recent-30d",
			WorkViewPresetFilter: WorkViewPresetFilter{
				CreatedWithin: "720h",
				Sort:          PresetSortCreatedDesc,
			},
		},
		{
			Name: "all",
			WorkViewPresetFilter: WorkViewPresetFilter{
				Archived: ArchivedInclude,
			},
		},
	}
}

// ShippedWorkViewPreset returns a deep copy of the named shipped preset, or
// ok=false when the name is not a shipped preset.
func ShippedWorkViewPreset(name string) (WorkViewPreset, bool) {
	return shippedPresetByName(name)
}

// shippedPresetByName returns a deep copy of the named shipped preset, or
// ok=false when the name is not a shipped preset.
func shippedPresetByName(name string) (WorkViewPreset, bool) {
	for _, p := range ShippedWorkViewPresets() {
		if p.Name == name {
			return cloneWorkViewPreset(p), true
		}
	}
	return WorkViewPreset{}, false
}

// WorkViewPresetNamed returns the resolved preset with the given name, or
// ok=false when the roster has no such entry.
func (c *Config) WorkViewPresetNamed(name string) (WorkViewPreset, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return WorkViewPreset{}, false
	}
	for _, p := range c.ResolveWorkViewPresets() {
		if p.Name == name {
			return cloneWorkViewPreset(p), true
		}
	}
	return WorkViewPreset{}, false
}

// WorkViewPresetNames returns the names of the resolved roster, in order.
func (c *Config) WorkViewPresetNames() []string {
	presets := c.ResolveWorkViewPresets()
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

// ResolveWorkViewPresets returns the effective Work view preset roster: the
// shipped list when the user has not declared one, otherwise the user's list
// with { system = "…" } references expanded. Numbers are 1-based positions.
// Decode and validation findings are recorded separately at load time.
func (c *Config) ResolveWorkViewPresets() []WorkViewPreset {
	raw, declared := c.rawWorkViewPresets()
	if !declared {
		return numberWorkViewPresets(ShippedWorkViewPresets())
	}
	return numberWorkViewPresets(expandWorkViewPresets(raw))
}

// DefaultWorkViewPreset returns the first resolved preset (the default), or
// zero when the resolved roster is empty.
func (c *Config) DefaultWorkViewPreset() WorkViewPreset {
	presets := c.ResolveWorkViewPresets()
	if len(presets) == 0 {
		return WorkViewPreset{}
	}
	return presets[0]
}

func (c *Config) rawWorkViewPresets() (presets []WorkViewPreset, declared bool) {
	if c == nil || c.Work == nil || c.Work.Dashboard == nil || c.Work.Dashboard.Tasks == nil {
		return nil, false
	}
	// A non-nil Tasks table that never set presets leaves the slice nil — that
	// is "undeclared". An explicit empty array is a declared empty roster.
	if c.Work.Dashboard.Tasks.Presets == nil {
		return nil, false
	}
	return c.Work.Dashboard.Tasks.Presets, true
}

// materializeWorkViewPresets writes the resolved roster back onto cfg so
// pop config show / EffectiveTOML surface the effective presets (including the
// shipped defaults when the user declared none).
func materializeWorkViewPresets(cfg *Config) {
	if cfg == nil {
		return
	}
	resolved := cfg.ResolveWorkViewPresets()
	if cfg.Work == nil {
		cfg.Work = &WorkConfig{}
	}
	if cfg.Work.Dashboard == nil {
		cfg.Work.Dashboard = &WorkDashboardConfig{}
	}
	if cfg.Work.Dashboard.Tasks == nil {
		cfg.Work.Dashboard.Tasks = &WorkDashboardTasksConfig{}
	}
	cfg.Work.Dashboard.Tasks.Presets = resolved
}

// expandWorkViewPresets resolves system references and drops entries that
// cannot form a usable preset (missing name, unresolvable system). The input
// is not modified.
func expandWorkViewPresets(raw []WorkViewPreset) []WorkViewPreset {
	out := make([]WorkViewPreset, 0, len(raw))
	for _, entry := range raw {
		if sys := strings.TrimSpace(entry.System); sys != "" {
			shipped, ok := shippedPresetByName(sys)
			if !ok {
				continue
			}
			out = append(out, shipped)
			continue
		}
		if strings.TrimSpace(entry.Name) == "" {
			continue
		}
		p := cloneWorkViewPreset(entry)
		p.System = ""
		p.problems = nil
		out = append(out, p)
	}
	return out
}

func numberWorkViewPresets(presets []WorkViewPreset) []WorkViewPreset {
	out := make([]WorkViewPreset, len(presets))
	for i, p := range presets {
		p.Number = i + 1
		out[i] = p
	}
	return out
}

func cloneWorkViewPreset(p WorkViewPreset) WorkViewPreset {
	out := p
	if p.Status != nil {
		out.Status = append([]string(nil), p.Status...)
	}
	if p.Unfolded != nil {
		v := *p.Unfolded
		out.Unfolded = &v
	}
	if p.Hide != nil {
		h := *p.Hide
		if p.Hide.Status != nil {
			h.Status = append([]string(nil), p.Hide.Status...)
		}
		if p.Hide.Unfolded != nil {
			v := *p.Hide.Unfolded
			h.Unfolded = &v
		}
		out.Hide = &h
	}
	if p.problems != nil {
		out.problems = append([]string(nil), p.problems...)
	}
	return out
}

// UnmarshalTOML tolerantly decodes one preset entry. Unknown keys, bad enum
// values, and wrong-typed fields become entry problems rather than aborting the
// load (ADR-0054). Only a non-table entry is a hard error.
func (p *WorkViewPreset) UnmarshalTOML(v interface{}) error {
	m, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Errorf("work view preset must be a table, got %T", v)
	}
	*p = WorkViewPreset{}
	for key := range m {
		if !presetEntryKeys[key] {
			p.problems = append(p.problems, fmt.Sprintf("unknown key %q", key))
		}
	}
	if raw, ok := m["system"]; ok {
		s, ok := raw.(string)
		if !ok {
			p.problems = append(p.problems, fmt.Sprintf("system must be a string, got %T", raw))
		} else {
			p.System = strings.TrimSpace(s)
		}
	}
	if raw, ok := m["name"]; ok {
		s, ok := raw.(string)
		if !ok {
			p.problems = append(p.problems, fmt.Sprintf("name must be a string, got %T", raw))
		} else {
			p.Name = strings.TrimSpace(s)
		}
	}
	if raw, ok := m["label"]; ok {
		s, ok := raw.(string)
		if !ok {
			p.problems = append(p.problems, fmt.Sprintf("label must be a string, got %T", raw))
		} else {
			p.Label = s
		}
	}
	decodePresetFilter(m, &p.WorkViewPresetFilter, &p.problems, "")
	if raw, ok := m["hide"]; ok {
		hm, ok := raw.(map[string]interface{})
		if !ok {
			p.problems = append(p.problems, fmt.Sprintf("hide must be a table, got %T", raw))
		} else {
			for key := range hm {
				if key == "hide" {
					p.problems = append(p.problems, `hide may not nest another hide clause`)
					continue
				}
				if !presetFilterKeys[key] {
					p.problems = append(p.problems, fmt.Sprintf("hide has unknown key %q", key))
				}
			}
			var filter WorkViewPresetFilter
			decodePresetFilter(hm, &filter, &p.problems, "hide.")
			p.Hide = &filter
		}
	}
	return nil
}

func decodePresetFilter(m map[string]interface{}, f *WorkViewPresetFilter, problems *[]string, prefix string) {
	if raw, ok := m["status"]; ok {
		list, err := stringList(raw)
		if err != nil {
			*problems = append(*problems, prefix+err.Error())
		} else {
			f.Status = list
		}
	}
	if raw, ok := m["unfolded"]; ok {
		b, ok := raw.(bool)
		if !ok {
			*problems = append(*problems, fmt.Sprintf("%sunfolded must be a bool, got %T", prefix, raw))
		} else {
			f.Unfolded = &b
		}
	}
	if raw, ok := m["archived"]; ok {
		s, ok := raw.(string)
		if !ok {
			*problems = append(*problems, fmt.Sprintf("%sarchived must be a string, got %T", prefix, raw))
		} else if !validArchivedModes[s] {
			*problems = append(*problems, fmt.Sprintf("%sarchived %q is not exclude|include|only", prefix, s))
		} else {
			f.Archived = s
		}
	}
	if raw, ok := m["created_within"]; ok {
		s, ok := raw.(string)
		if !ok {
			*problems = append(*problems, fmt.Sprintf("%screated_within must be a string, got %T", prefix, raw))
		} else if _, err := parsePresetDuration(s); err != nil {
			*problems = append(*problems, fmt.Sprintf("%screated_within %q is not a valid duration", prefix, s))
		} else {
			f.CreatedWithin = s
		}
	}
	if raw, ok := m["sort"]; ok {
		s, ok := raw.(string)
		if !ok {
			*problems = append(*problems, fmt.Sprintf("%ssort must be a string, got %T", prefix, raw))
		} else if !validPresetSorts[s] {
			*problems = append(*problems, fmt.Sprintf("%ssort %q is not created_desc|created_asc", prefix, s))
		} else {
			f.Sort = s
		}
	}
}

func stringList(v interface{}) ([]string, error) {
	switch val := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(val))
		for i, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("status[%d] must be a string, got %T", i, item)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return append([]string(nil), val...), nil
	default:
		return nil, fmt.Errorf("status must be an array of strings, got %T", v)
	}
}

// ParsePresetDuration accepts Go duration strings and a trailing day unit
// ("7d", "30d") so recency presets can be written in day terms.
func ParsePresetDuration(s string) (time.Duration, error) {
	return parsePresetDuration(s)
}

// parsePresetDuration accepts Go duration strings and a trailing day unit
// ("7d", "30d") so recency presets can be written in day terms.
func parsePresetDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		n, err := time.ParseDuration(days + "h")
		if err != nil {
			return 0, err
		}
		return n * 24, nil
	}
	return time.ParseDuration(s)
}

// workViewPresetFindings collects non-fatal problems for the Work view preset
// roster: per-entry decode issues, duplicate names, unresolvable system refs,
// missing names, and a tenth-or-later preset (ADR-0197). The load still
// succeeds; findings mirror into Warnings.
func workViewPresetFindings(path string, cfg *Config) []Finding {
	raw, declared := cfg.rawWorkViewPresets()
	if !declared {
		return nil
	}
	var findings []Finding
	seen := map[string]int{}
	resolvedIndex := 0
	for i, entry := range raw {
		basePath := fmt.Sprintf("work.dashboard.tasks.presets[%d]", i)
		for _, problem := range entry.problems {
			findings = append(findings, Finding{
				Path:    basePath,
				Message: fmt.Sprintf("%s: [%s] %s", path, basePath, problem),
			})
		}
		if sys := strings.TrimSpace(entry.System); sys != "" {
			if _, ok := shippedPresetByName(sys); !ok {
				findings = append(findings, Finding{
					Path:    basePath + ".system",
					Message: fmt.Sprintf("%s: [%s] system %q does not name a shipped preset", path, basePath, sys),
				})
				continue
			}
		} else if strings.TrimSpace(entry.Name) == "" {
			findings = append(findings, Finding{
				Path:    basePath + ".name",
				Message: fmt.Sprintf("%s: [%s] preset has no name; excluding", path, basePath),
			})
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if sys := strings.TrimSpace(entry.System); sys != "" {
			name = sys
		}
		if prev, ok := seen[name]; ok {
			findings = append(findings, Finding{
				Path: basePath + ".name",
				Message: fmt.Sprintf(
					"%s: [%s] duplicate preset name %q (also at work.dashboard.tasks.presets[%d])",
					path, basePath, name, prev,
				),
			})
		} else {
			seen[name] = i
		}
		if resolvedIndex >= 9 {
			findings = append(findings, Finding{
				Path: basePath,
				Message: fmt.Sprintf(
					"%s: [%s] preset %q is 10th or later; reachable by j/k but has no digit shortcut",
					path, basePath, name,
				),
			})
		}
		resolvedIndex++
	}
	return findings
}
