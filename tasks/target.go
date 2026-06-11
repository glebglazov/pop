package tasks

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// matchTaskFileField resolves an task file name to its manifest task ID.
func matchTaskFileField(m *Manifest, fileName string) (string, error) {
	if m == nil {
		return "", exitErr(ExitNoRunnable, "unknown task %q", fileName)
	}
	base := filepath.Base(fileName)
	for _, task := range m.Tasks {
		if task.File == base {
			return task.ID, nil
		}
	}
	return "", exitErr(ExitNoRunnable, "%s", unknownTaskMessage(m, base))
}

// resolveTaskSetIdentifier resolves a bare Task set identifier to its canonical
// ID, scoped to the current repository's Task storage.
func resolveTaskSetIdentifier(refresh *RefreshResult, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	if findRow(refresh, raw) != nil {
		return raw, nil
	}
	if refresh.Manifests != nil {
		if _, ok := refresh.Manifests[raw]; ok {
			return raw, nil
		}
	}
	return "", exitErr(ExitNoRunnable, "%s", unknownTaskSetTargetMessage(refresh, raw))
}

// validIdentifierList returns the discovered Task set identifiers, sorted.
func validIdentifierList(refresh *RefreshResult) []string {
	if refresh == nil {
		return nil
	}
	var ids []string
	for _, row := range refresh.Rows {
		if row.Status != StatusMissing {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// invalidTargetMessage explains why raw is not a valid Task target reference
// and lists the valid Task set identifiers.
func invalidTargetMessage(refresh *RefreshResult, raw, reason string) string {
	ids := validIdentifierList(refresh)
	if len(ids) == 0 {
		return fmt.Sprintf("invalid target %q: %s", raw, reason)
	}
	return fmt.Sprintf("invalid target %q: %s; valid: %s", raw, reason, strings.Join(ids, ", "))
}

// rejectPathForms rejects absolute and relative filesystem paths, which are no
// longer valid Task target references (see ADR 0012).
func rejectPathForms(refresh *RefreshResult, raw string) error {
	slash := filepath.ToSlash(raw)
	if filepath.IsAbs(raw) || raw == "~" || strings.HasPrefix(slash, "~/") {
		return exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "absolute paths are not task target references"))
	}
	if raw == "." || raw == ".." || strings.HasPrefix(slash, "./") || strings.HasPrefix(slash, "../") {
		return exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "relative paths are not task target references"))
	}
	return nil
}

// resolveTaskSetRelativeTask resolves an <task-set>/<file>.md reference to its
// canonical Task set and task identifiers.
func resolveTaskSetRelativeTask(refresh *RefreshResult, raw string) (taskSetID, taskID string, err error) {
	slash := filepath.ToSlash(raw)
	head, tail, found := strings.Cut(slash, "/")
	if !found || head == "" || tail == "" || strings.Contains(tail, "/") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected <task-set>/<file>.md"))
	}
	if !strings.HasSuffix(tail, ".md") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "task file must end with .md"))
	}

	taskSetID, err = resolveTaskSetIdentifier(refresh, head)
	if err != nil {
		return "", "", err
	}
	taskID, err = matchTaskFileField(refresh.Manifests[taskSetID], tail)
	if err != nil {
		return "", "", err
	}
	return taskSetID, taskID, nil
}

// ResolveTaskSetTarget resolves a bare Task set identifier to its canonical ID,
// scoped to the current repository's Task storage. Empty input selects the
// auto-pick set. File references and path forms are rejected.
func ResolveTaskSetTarget(refresh *RefreshResult, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if err := rejectPathForms(refresh, raw); err != nil {
		return "", err
	}
	// The trailing-slash <task-set>/ spelling is a whole-set synonym for a
	// bare <task-set>; trim one trailing slash and resolve the remainder. A
	// form that still contains an inner slash (e.g. a/b/) stays rejected.
	slash := strings.TrimSuffix(filepath.ToSlash(raw), "/")
	if strings.Contains(slash, "/") {
		return "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected a bare task set identifier"))
	}
	if strings.HasSuffix(slash, ".md") {
		return "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected a bare task set identifier, not a file name"))
	}
	return resolveTaskSetIdentifier(refresh, slash)
}

// ResolveTaskTarget resolves a bare Task set identifier or an
// <task-set>/<file>.md reference to canonical identifiers. Empty input selects
// the auto-pick set. Path forms and bare filenames are rejected.
func ResolveTaskTarget(refresh *RefreshResult, raw string) (taskSetID, taskID string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if err := rejectPathForms(refresh, raw); err != nil {
		return "", "", err
	}
	slash := filepath.ToSlash(raw)
	// A trailing slash marks the whole-set <task-set>/ spelling: trim it and
	// resolve a bare set (no task). A remainder with an inner slash (e.g.
	// a/b/) is neither a set nor a file reference and stays rejected.
	if strings.HasSuffix(slash, "/") {
		trimmed := strings.TrimSuffix(slash, "/")
		if trimmed == "" || strings.Contains(trimmed, "/") {
			return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected <task-set> or <task-set>/<file>.md"))
		}
		taskSetID, err = resolveTaskSetIdentifier(refresh, trimmed)
		return taskSetID, "", err
	}
	if strings.Contains(slash, "/") {
		return resolveTaskSetRelativeTask(refresh, slash)
	}
	if strings.HasSuffix(slash, ".md") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "bare filenames are not task target references; use <task-set>/<file>.md"))
	}
	taskSetID, err = resolveTaskSetIdentifier(refresh, raw)
	return taskSetID, "", err
}

// ResolveTaskFileTarget requires an <task-set>/<file>.md reference and resolves
// it to canonical identifiers. Bare identifiers, bare filenames, and path forms
// are rejected.
func ResolveTaskFileTarget(refresh *RefreshResult, raw string) (taskSetID, taskID string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected <task-set>/<file>.md"))
	}
	if err := rejectPathForms(refresh, raw); err != nil {
		return "", "", err
	}
	slash := filepath.ToSlash(raw)
	if !strings.Contains(slash, "/") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected <task-set>/<file>.md"))
	}
	return resolveTaskSetRelativeTask(refresh, slash)
}

// taskTargetIdentifierCompletions offers <task-set>/ candidates at the
// set-identifier stage and, once an identifier and slash are typed, the
// set-relative task files for that set. It never offers filesystem path
// segments.
func taskTargetIdentifierCompletions(refresh *RefreshResult, toComplete string) []string {
	return taskTargetCompletions(refresh, toComplete, false)
}

// actionableTaskTargetCompletions is the filtered variant for implement and
// the override verbs: it never offers a done thing — Done Task sets are
// omitted at the set stage and done tasks at the file stage, since neither is
// actionable by any of those verbs. Explicitly typed done targets still
// resolve; the filter narrows completion, not resolution.
func actionableTaskTargetCompletions(refresh *RefreshResult, toComplete string) []string {
	return taskTargetCompletions(refresh, toComplete, true)
}

func taskTargetCompletions(refresh *RefreshResult, toComplete string, omitDone bool) []string {
	if refresh == nil || refresh.Manifests == nil {
		return nil
	}
	slash := filepath.ToSlash(toComplete)
	if head, _, found := strings.Cut(slash, "/"); found {
		m := refresh.Manifests[head]
		if m == nil {
			return nil
		}
		prefix := head + "/"
		var out []string
		for _, task := range m.Tasks {
			if omitDone && task.Status == "done" {
				continue
			}
			candidate := prefix + task.File
			if strings.HasPrefix(candidate, slash) {
				out = append(out, candidate)
			}
		}
		sort.Strings(out)
		return out
	}

	// At the set-identifier stage, offer each set as <task-set>/ so the file
	// stage's no-space drill leaves the cursor right after the slash. The
	// <task-set>/ form is itself a valid whole-set target.
	var ids []string
	for id, m := range refresh.Manifests {
		if omitDone && DeriveStatus(m) == StatusDone {
			continue
		}
		candidate := id + "/"
		if strings.HasPrefix(candidate, slash) {
			ids = append(ids, candidate)
		}
	}
	sort.Strings(ids)
	return ids
}
