package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	allowedTaskTypes    = map[string]bool{"AFK": true, "HITL": true}
	allowedTaskStatuses = map[TaskStatus]bool{TaskOpen: true, TaskDone: true, TaskFailed: true, TaskSkipped: true}
	allowedTaskEfforts  = map[string]bool{"light": true, "standard": true, "heavy": true}
	acHeaderPattern     = regexp.MustCompile(`(?i)^##\s+Acceptance criteria\s*$`)
	checkboxPattern     = regexp.MustCompile(`^-\s+\[[ xX]\]`)
)

const DefaultTaskEffort = "standard"

// ValidEfforts returns the accepted effort tier names in ladder order.
func ValidEfforts() []string { return []string{"light", "standard", "heavy"} }

// IsValidEffort reports whether effort names an accepted model-strength tier.
func IsValidEffort(effort string) bool { return allowedTaskEfforts[effort] }

// Task represents one entry in an task manifest.
type Task struct {
	ID          string     `json:"id"`
	File        string     `json:"file"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	Status      TaskStatus `json:"status"`
	BlockedBy   []string   `json:"blocked_by"`
	FailedAfter *int       `json:"failed_after,omitempty"`
	// Effort selects the model-strength tier for this task. Missing manifests
	// resolve to DefaultTaskEffort; EffortExplicit records whether the key was
	// present so legacy manifests keep their previous invocation shape.
	Effort         string `json:"-"`
	EffortExplicit bool   `json:"-"`
	// Origin tags a Remediation task's provenance (ADR-0105): auto = Verifier
	// spawned on FIXABLE, human = spawned via the Remediate disposition. Empty on
	// non-remediation tasks and on legacy Remediation entries; an absent origin
	// reads as auto so old sets keep their prior depth-cap behavior. It rides
	// through as `origin`, omitted when empty so unrelated rewrites stay quiet.
	Origin string `json:"-"`
}

type taskJSON struct {
	ID          string     `json:"id"`
	File        string     `json:"file"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	Status      TaskStatus `json:"status"`
	BlockedBy   []string   `json:"blocked_by"`
	FailedAfter *int       `json:"failed_after,omitempty"`
	Effort      *string    `json:"effort,omitempty"`
	Origin      string     `json:"origin,omitempty"`
}

// UnmarshalJSON preserves the difference between an absent effort key and an
// explicit effort: "standard" while presenting both as standard to callers.
func (t *Task) UnmarshalJSON(data []byte) error {
	var raw taskJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.ID = raw.ID
	t.File = raw.File
	t.Title = raw.Title
	t.Type = raw.Type
	t.Status = raw.Status
	t.BlockedBy = raw.BlockedBy
	t.FailedAfter = raw.FailedAfter
	t.Origin = raw.Origin
	t.Effort = DefaultTaskEffort
	t.EffortExplicit = false
	if raw.Effort != nil {
		t.Effort = *raw.Effort
		t.EffortExplicit = true
	}
	return nil
}

// MarshalJSON omits effort unless it was explicitly present or set by code,
// avoiding churn when older manifests are rewritten for unrelated state.
func (t Task) MarshalJSON() ([]byte, error) {
	raw := taskJSON{
		ID:          t.ID,
		File:        t.File,
		Title:       t.Title,
		Type:        t.Type,
		Status:      t.Status,
		BlockedBy:   t.BlockedBy,
		FailedAfter: t.FailedAfter,
		Origin:      t.Origin,
	}
	if t.EffortExplicit || (t.Effort != "" && t.Effort != DefaultTaskEffort) {
		effort := t.Effort
		raw.Effort = &effort
	}
	return json.Marshal(raw)
}

// Manifest is a parsed and validated task manifest.
type Manifest struct {
	Stem    string
	Dir     string
	Path    string
	Tasks   []Task
	Raw     json.RawMessage
	Errors  []string
	Valid   bool
	Unknown map[string]json.RawMessage
	// DeprecatedKeys names retired set-level keys (`worktree`, `auto_drain`) that
	// are still present in the manifest but no longer read (ADR-0115). They are
	// ignored — never MALFORMED — and preserved verbatim in Unknown; register
	// surfaces them as a deprecation warning. Sorted for a stable warning order.
	DeprecatedKeys []string
}

// WorktreeDirective is a set-level worktree intent persisted with a set's
// registration. Exactly one arm is set: Managed requests a pop-provisioned
// managed worktree, Name adopts the existing worktree of that name on this
// machine. It is no longer read from the manifest (ADR-0115) — it survives as
// the store-backed registration intent shape (see tasks.RegisteredTaskSet).
type WorktreeDirective struct {
	Managed bool   `json:"managed,omitempty"`
	Name    string `json:"name,omitempty"`
}

// LoadManifest reads and validates an task manifest.
func LoadManifest(d *Deps, stem, manifestPath string) *Manifest {
	m := &Manifest{
		Stem: stem,
		Path: manifestPath,
		Dir:  filepath.Dir(manifestPath),
	}

	data, err := d.FS.ReadFile(manifestPath)
	if err != nil {
		m.Errors = append(m.Errors, fmt.Sprintf("read manifest: %v", err))
		return m
	}
	m.Raw = append(json.RawMessage(nil), data...)

	if err := parseManifestJSON(data, m); err != nil {
		m.Errors = append(m.Errors, err.Error())
		return m
	}

	validateManifest(d, m)
	if len(m.Errors) == 0 {
		m.Valid = true
	}
	return m
}

func parseManifestJSON(data []byte, m *Manifest) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	tasksRaw, ok := raw["tasks"]
	if !ok {
		return fmt.Errorf("missing tasks array")
	}
	if err := json.Unmarshal(tasksRaw, &m.Tasks); err != nil {
		return fmt.Errorf("parse tasks: %w", err)
	}

	m.Unknown = make(map[string]json.RawMessage)
	for k, v := range raw {
		switch k {
		case "tasks":
			continue
		case "auto_drain", "worktree":
			// Retired set-level keys (ADR-0115): no longer read as registration
			// seeds. They are ignored with a deprecation warning at register — never
			// MALFORMED, regardless of value — and preserved verbatim (in Unknown) so
			// no forced migration rewrites the file.
			m.DeprecatedKeys = append(m.DeprecatedKeys, k)
			m.Unknown[k] = v
		default:
			m.Unknown[k] = v
		}
	}
	sort.Strings(m.DeprecatedKeys)
	return nil
}

func validateManifest(d *Deps, m *Manifest) {
	if len(m.Tasks) == 0 {
		m.Errors = append(m.Errors, "tasks array is empty")
	}

	ids := make(map[string]int)
	files := make(map[string]int)
	idSet := make(map[string]bool)

	for i, task := range m.Tasks {
		if task.ID == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("task[%d]: missing id", i))
			continue
		}
		if ids[task.ID] > 0 {
			m.Errors = append(m.Errors, fmt.Sprintf("duplicate task id %q", task.ID))
		}
		ids[task.ID]++
		idSet[task.ID] = true

		if task.File == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: missing file", task.ID))
		} else {
			if strings.Contains(task.File, "/") || strings.Contains(task.File, "\\") {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: file must be root-level markdown name, got %q", task.ID, task.File))
			}
			if files[task.File] > 0 {
				m.Errors = append(m.Errors, fmt.Sprintf("duplicate task file %q", task.File))
			}
			files[task.File]++

			mdPath := filepath.Join(m.Dir, task.File)
			if _, err := d.FS.Stat(mdPath); os.IsNotExist(err) {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: missing markdown file %q", task.ID, task.File))
			} else if err != nil {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: stat markdown %q: %v", task.ID, task.File, err))
			} else if err := validateAcceptanceCriteria(d, mdPath); err != nil {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: %v", task.ID, err))
			}
		}

		if !allowedTaskTypes[task.Type] {
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: invalid type %q", task.ID, task.Type))
		}

		if task.Effort == "" {
			m.Tasks[i].Effort = DefaultTaskEffort
			task.Effort = DefaultTaskEffort
		}
		if !allowedTaskEfforts[task.Effort] {
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: invalid effort %q", task.ID, task.Effort))
		}

		switch task.Status {
		case "in_progress":
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: persisted in_progress status is malformed", task.ID))
		case "":
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: missing status", task.ID))
		default:
			if !allowedTaskStatuses[task.Status] {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: invalid status %q", task.ID, task.Status))
			}
		}
	}

	for _, task := range m.Tasks {
		for _, blocker := range task.BlockedBy {
			if !idSet[blocker] {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: unresolved blocker %q", task.ID, blocker))
			}
		}
	}
}

func validateAcceptanceCriteria(d *Deps, mdPath string) error {
	data, err := d.FS.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read markdown: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	inSection := false
	sectionCount := 0
	checkboxCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if acHeaderPattern.MatchString(trimmed) {
			sectionCount++
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "## ") {
			inSection = false
		}
		if inSection && checkboxPattern.MatchString(trimmed) {
			checkboxCount++
		}
	}

	if sectionCount == 0 {
		return fmt.Errorf("missing acceptance criteria section")
	}
	if sectionCount > 1 {
		return fmt.Errorf("multiple acceptance criteria sections")
	}
	if checkboxCount == 0 {
		return fmt.Errorf("acceptance criteria has no checkboxes")
	}
	return nil
}

// WriteManifestAtomic writes a manifest JSON file atomically, preserving unknown
// fields — including retired keys (`worktree`, `auto_drain`) that ride through in
// Unknown so a rewrite never strips a legacy manifest (ADR-0115).
func WriteManifestAtomic(d *Deps, m *Manifest) error {
	out := make(map[string]json.RawMessage)
	for k, v := range m.Unknown {
		out[k] = v
	}
	tasksData, err := json.Marshal(m.Tasks)
	if err != nil {
		return err
	}
	out["tasks"] = tasksData

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, m.Path, data, 0o644)
}

// VerifyOptedOut reports whether the set explicitly opted out of Agent
// verification with `"verify": false` in its manifest (ADR-0086). Verification
// is a per-set opt-out only: user config is the master gate, so an absent or
// truthy `verify` key means the set participates when the feature is globally
// enabled, and there is no per-set opt-*in* while the feature is off. A
// malformed value is treated as participating (fail toward verifying); the key
// rides through WriteManifestAtomic in Unknown, so a rewrite preserves it.
func (m *Manifest) VerifyOptedOut() bool {
	if m == nil {
		return false
	}
	raw, ok := m.Unknown["verify"]
	if !ok {
		return false
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return false
	}
	return !enabled
}

// VerifierDirective is a set's per-set Verifier override, read from the
// manifest's `"verifier": {"agents": [...], "effort": "..."}` object (ADR-0086).
// It overrides the [tasks.verify] config default (agents, effort) for that
// set, but it is opt-out only for participation: user config is the master gate,
// so a directive can steer *how* a set is verified but never opt it *in* while
// the feature is globally off (that stays VerifyOptedOut / the config switch).
type VerifierDirective struct {
	Agents []string `json:"agents,omitempty"`
	Effort string   `json:"effort,omitempty"`
}

// VerifierOverride returns the set's per-set Verifier override, or nil when the
// manifest carries no `verifier` object (or a malformed one — a bad value is
// ignored so it falls through to the config default). The key rides through
// WriteManifestAtomic in Unknown, so a rewrite preserves it.
func (m *Manifest) VerifierOverride() *VerifierDirective {
	if m == nil {
		return nil
	}
	raw, ok := m.Unknown["verifier"]
	if !ok {
		return nil
	}
	var over VerifierDirective
	if err := json.Unmarshal(raw, &over); err != nil {
		return nil
	}
	return &over
}
