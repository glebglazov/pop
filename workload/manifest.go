package workload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	allowedIssueTypes    = map[string]bool{"AFK": true, "HITL": true}
	allowedIssueStatuses = map[string]bool{"open": true, "done": true, "failed": true, "skipped": true}
	acHeaderPattern      = regexp.MustCompile(`(?i)^##\s+Acceptance criteria\s*$`)
	checkboxPattern      = regexp.MustCompile(`^-\s+\[[ xX]\]`)
)

// Issue represents one entry in an issue manifest.
type Issue struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	BlockedBy   []string `json:"blocked_by"`
	FailedAfter *int     `json:"failed_after,omitempty"`
}

// Manifest is a parsed and validated issue manifest.
type Manifest struct {
	Stem   string
	Dir    string
	Path   string
	Issues []Issue
	Raw    json.RawMessage
	Errors []string
	Valid  bool
	Unknown map[string]json.RawMessage
}

// LoadManifest reads and validates an issue manifest.
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

	issuesRaw, ok := raw["issues"]
	if !ok {
		return fmt.Errorf("missing issues array")
	}
	if err := json.Unmarshal(issuesRaw, &m.Issues); err != nil {
		return fmt.Errorf("parse issues: %w", err)
	}

	m.Unknown = make(map[string]json.RawMessage)
	for k, v := range raw {
		if k != "issues" {
			m.Unknown[k] = v
		}
	}
	return nil
}

func validateManifest(d *Deps, m *Manifest) {
	if len(m.Issues) == 0 {
		m.Errors = append(m.Errors, "issues array is empty")
	}

	ids := make(map[string]int)
	files := make(map[string]int)
	idSet := make(map[string]bool)

	for i, issue := range m.Issues {
		if issue.ID == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("issue[%d]: missing id", i))
			continue
		}
		if ids[issue.ID] > 0 {
			m.Errors = append(m.Errors, fmt.Sprintf("duplicate issue id %q", issue.ID))
		}
		ids[issue.ID]++
		idSet[issue.ID] = true

		if issue.File == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("issue %q: missing file", issue.ID))
		} else {
			if strings.Contains(issue.File, "/") || strings.Contains(issue.File, "\\") {
				m.Errors = append(m.Errors, fmt.Sprintf("issue %q: file must be root-level markdown name, got %q", issue.ID, issue.File))
			}
			if files[issue.File] > 0 {
				m.Errors = append(m.Errors, fmt.Sprintf("duplicate issue file %q", issue.File))
			}
			files[issue.File]++

			mdPath := filepath.Join(m.Dir, issue.File)
			if _, err := d.FS.Stat(mdPath); os.IsNotExist(err) {
				m.Errors = append(m.Errors, fmt.Sprintf("issue %q: missing markdown file %q", issue.ID, issue.File))
			} else if err != nil {
				m.Errors = append(m.Errors, fmt.Sprintf("issue %q: stat markdown %q: %v", issue.ID, issue.File, err))
			} else if err := validateAcceptanceCriteria(d, mdPath); err != nil {
				m.Errors = append(m.Errors, fmt.Sprintf("issue %q: %v", issue.ID, err))
			}
		}

		if !allowedIssueTypes[issue.Type] {
			m.Errors = append(m.Errors, fmt.Sprintf("issue %q: invalid type %q", issue.ID, issue.Type))
		}

		switch issue.Status {
		case "in_progress":
			m.Errors = append(m.Errors, fmt.Sprintf("issue %q: persisted in_progress status is malformed", issue.ID))
		case "":
			m.Errors = append(m.Errors, fmt.Sprintf("issue %q: missing status", issue.ID))
		default:
			if !allowedIssueStatuses[issue.Status] {
				m.Errors = append(m.Errors, fmt.Sprintf("issue %q: invalid status %q", issue.ID, issue.Status))
			}
		}
	}

	for _, issue := range m.Issues {
		for _, blocker := range issue.BlockedBy {
			if !idSet[blocker] {
				m.Errors = append(m.Errors, fmt.Sprintf("issue %q: unresolved blocker %q", issue.ID, blocker))
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
	issuesData, err := json.Marshal(m.Issues)
	if err != nil {
		return err
	}
	out["issues"] = issuesData

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, m.Path, data, 0o644)
}
