package workload

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// matchIssueFileField resolves an issue file name to its manifest issue ID.
func matchIssueFileField(m *Manifest, fileName string) (string, error) {
	if m == nil {
		return "", exitErr(ExitNoRunnable, "unknown task %q", fileName)
	}
	base := filepath.Base(fileName)
	for _, issue := range m.Issues {
		if issue.File == base {
			return issue.ID, nil
		}
	}
	return "", exitErr(ExitNoRunnable, "%s", unknownIssueMessage(m, base))
}

// resolveIssueSetIdentifier resolves a bare Issue set identifier to its canonical
// ID, scoped to the current repository's Workload storage.
func resolveIssueSetIdentifier(refresh *RefreshResult, raw string) (string, error) {
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
	return "", exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, raw))
}

// validIdentifierList returns the discovered Issue set identifiers, sorted.
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

// invalidTargetMessage explains why raw is not a valid Workload target reference
// and lists the valid Issue set identifiers.
func invalidTargetMessage(refresh *RefreshResult, raw, reason string) string {
	ids := validIdentifierList(refresh)
	if len(ids) == 0 {
		return fmt.Sprintf("invalid target %q: %s", raw, reason)
	}
	return fmt.Sprintf("invalid target %q: %s; valid: %s", raw, reason, strings.Join(ids, ", "))
}

// rejectPathForms rejects absolute and relative filesystem paths, which are no
// longer valid Workload target references (see ADR 0012).
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

// resolveIssueSetRelativeIssue resolves an <issue-set>/<file>.md reference to its
// canonical Issue set and issue identifiers.
func resolveIssueSetRelativeIssue(refresh *RefreshResult, raw string) (issueSetID, issueID string, err error) {
	slash := filepath.ToSlash(raw)
	head, tail, found := strings.Cut(slash, "/")
	if !found || head == "" || tail == "" || strings.Contains(tail, "/") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected <task-set>/<file>.md"))
	}
	if !strings.HasSuffix(tail, ".md") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "task file must end with .md"))
	}

	issueSetID, err = resolveIssueSetIdentifier(refresh, head)
	if err != nil {
		return "", "", err
	}
	issueID, err = matchIssueFileField(refresh.Manifests[issueSetID], tail)
	if err != nil {
		return "", "", err
	}
	return issueSetID, issueID, nil
}

// ResolveIssueSetTarget resolves a bare Issue set identifier to its canonical ID,
// scoped to the current repository's Workload storage. Empty input selects the
// auto-pick set. File references and path forms are rejected.
func ResolveIssueSetTarget(refresh *RefreshResult, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if err := rejectPathForms(refresh, raw); err != nil {
		return "", err
	}
	slash := filepath.ToSlash(raw)
	if strings.Contains(slash, "/") {
		return "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected a bare task set identifier"))
	}
	if strings.HasSuffix(slash, ".md") {
		return "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "expected a bare task set identifier, not a file name"))
	}
	return resolveIssueSetIdentifier(refresh, raw)
}

// ResolveIssueTarget resolves a bare Issue set identifier or an
// <issue-set>/<file>.md reference to canonical identifiers. Empty input selects
// the auto-pick set. Path forms and bare filenames are rejected.
func ResolveIssueTarget(refresh *RefreshResult, raw string) (issueSetID, issueID string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if err := rejectPathForms(refresh, raw); err != nil {
		return "", "", err
	}
	slash := filepath.ToSlash(raw)
	if strings.Contains(slash, "/") {
		return resolveIssueSetRelativeIssue(refresh, slash)
	}
	if strings.HasSuffix(slash, ".md") {
		return "", "", exitErr(ExitSetup, "%s", invalidTargetMessage(refresh, raw, "bare filenames are not task target references; use <task-set>/<file>.md"))
	}
	issueSetID, err = resolveIssueSetIdentifier(refresh, raw)
	return issueSetID, "", err
}

// ResolveIssueFileTarget requires an <issue-set>/<file>.md reference and resolves
// it to canonical identifiers. Bare identifiers, bare filenames, and path forms
// are rejected.
func ResolveIssueFileTarget(refresh *RefreshResult, raw string) (issueSetID, issueID string, err error) {
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
	return resolveIssueSetRelativeIssue(refresh, slash)
}

// issueTargetIdentifierCompletions offers bare Issue set identifiers and, once an
// identifier and slash are typed, the set-relative issue files for that set. It
// never offers filesystem path segments.
func issueTargetIdentifierCompletions(refresh *RefreshResult, toComplete string) []string {
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
		for _, issue := range m.Issues {
			candidate := prefix + issue.File
			if strings.HasPrefix(candidate, slash) {
				out = append(out, candidate)
			}
		}
		sort.Strings(out)
		return out
	}

	var ids []string
	for id := range refresh.Manifests {
		if strings.HasPrefix(id, slash) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
