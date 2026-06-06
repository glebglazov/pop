package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	allowedTaskTypes    = map[string]bool{"AFK": true, "HITL": true}
	allowedTaskStatuses = map[string]bool{"open": true, "done": true, "failed": true, "skipped": true}
	acHeaderPattern     = regexp.MustCompile(`(?i)^##\s+Acceptance criteria\s*$`)
	checkboxPattern     = regexp.MustCompile(`^-\s+\[[ xX]\]`)
)

// Task represents one entry in an task manifest.
type Task struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	BlockedBy   []string `json:"blocked_by"`
	FailedAfter *int     `json:"failed_after,omitempty"`
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
		if k != "tasks" {
			m.Unknown[k] = v
		}
	}
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

// WriteManifestAtomic writes a manifest JSON file atomically, preserving unknown fields.
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
