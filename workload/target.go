package workload

import (
	"path/filepath"
	"sort"
	"strings"
)

func isPathLike(raw string) bool {
	if raw == "" {
		return false
	}
	if raw == "." || raw == ".." {
		return true
	}
	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return true
	}
	if filepath.IsAbs(raw) {
		return true
	}
	return strings.Contains(raw, string(filepath.Separator))
}

func isIssueFileReference(raw string) bool {
	return isPathLike(raw) || strings.HasSuffix(raw, ".md")
}

func cwdOrDefault(d *Deps, cwd string) (string, error) {
	if cwd != "" {
		return cwd, nil
	}
	return d.FS.Getwd()
}

func absFromCWD(d *Deps, cwd, path string) (string, error) {
	cwd, err := cwdOrDefault(d, cwd)
	if err != nil {
		return "", err
	}

	expanded := expandHome(d, path)
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(cwd, expanded)
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	resolved, err := d.FS.EvalSymlinks(clean)
	if err != nil {
		resolved = clean
	}
	return filepath.Clean(resolved), nil
}

func matchIssueSetDir(refresh *RefreshResult, absPath string) string {
	if refresh == nil || refresh.Manifests == nil {
		return ""
	}
	target, err := filepath.Abs(filepath.Clean(absPath))
	if err != nil {
		return ""
	}
	for id, m := range refresh.Manifests {
		if m == nil {
			continue
		}
		dir, err := filepath.Abs(filepath.Clean(m.Dir))
		if err != nil {
			continue
		}
		if dir == target {
			return id
		}
	}
	return ""
}

func matchIssueFileField(m *Manifest, fileName string) (string, error) {
	if m == nil {
		return "", exitErr(ExitNoRunnable, "unknown issue %q", fileName)
	}
	base := filepath.Base(fileName)
	for _, issue := range m.Issues {
		if issue.File == base {
			return issue.ID, nil
		}
	}
	return "", exitErr(ExitNoRunnable, "%s", unknownIssueMessage(m, base))
}

// ResolveIssueSetTarget normalizes a Workload target reference to an Issue set identifier.
func ResolveIssueSetTarget(d *Deps, refresh *RefreshResult, cwd, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	if !isPathLike(raw) {
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

	abs, err := absFromCWD(d, cwd, raw)
	if err != nil {
		return "", exitErr(ExitSetup, "resolve Issue set path: %v", err)
	}

	if id := matchIssueSetDir(refresh, abs); id != "" {
		return id, nil
	}
	return "", exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, raw))
}

func resolveIssueFromPath(d *Deps, refresh *RefreshResult, cwd, raw string) (issueSetID, issueID string, ok bool, err error) {
	abs, err := absFromCWD(d, cwd, raw)
	if err != nil {
		return "", "", false, exitErr(ExitSetup, "resolve issue path: %v", err)
	}

	info, err := d.FS.Stat(abs)
	if err != nil {
		return "", "", false, nil
	}
	if info.IsDir() {
		return "", "", false, nil
	}

	issueSetID = matchIssueSetDir(refresh, filepath.Dir(abs))
	if issueSetID == "" {
		return "", "", false, nil
	}

	issueID, err = matchIssueFileField(refresh.Manifests[issueSetID], filepath.Base(abs))
	if err != nil {
		return "", "", true, err
	}
	return issueSetID, issueID, true, nil
}

// ResolveWorkloadTargets normalizes Workload target references to canonical Issue set and issue identifiers.
func ResolveWorkloadTargets(d *Deps, refresh *RefreshResult, cwd, issueSetRaw, issueRaw string) (issueSetID, issueID string, err error) {
	issueSetRaw = strings.TrimSpace(issueSetRaw)
	issueRaw = strings.TrimSpace(issueRaw)

	if issueRaw != "" && isPathLike(issueRaw) {
		spanSet, spanIssue, ok, resolveErr := resolveIssueFromPath(d, refresh, cwd, issueRaw)
		if resolveErr != nil {
			return "", "", resolveErr
		}
		if ok {
			resolvedSet, resolveErr := ResolveIssueSetTarget(d, refresh, cwd, issueSetRaw)
			if resolveErr != nil {
				return "", "", resolveErr
			}
			if issueSetRaw != "" && resolvedSet != spanSet {
				return "", "", exitErr(ExitNoRunnable, "Issue set %q does not match issue path Issue set %q", resolvedSet, spanSet)
			}
			return spanSet, spanIssue, nil
		}
		return "", "", exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, issueRaw))
	}

	issueSetID, err = ResolveIssueSetTarget(d, refresh, cwd, issueSetRaw)
	if err != nil {
		return "", "", err
	}

	if issueRaw == "" {
		return issueSetID, "", nil
	}

	if isIssueFileReference(issueRaw) {
		if issueSetID == "" {
			return "", "", exitErr(ExitSetup, "issue path %q requires --issue-set or a spanning path", issueRaw)
		}
		m := refresh.Manifests[issueSetID]
		issueID, err = matchIssueFileField(m, issueRaw)
		return issueSetID, issueID, err
	}

	if issueSetID == "" {
		return "", "", exitErr(ExitSetup, "explicit --issue requires --issue-set")
	}
	m := refresh.Manifests[issueSetID]
	if m == nil {
		return "", "", exitErr(ExitNoRunnable, "Issue set %q has no issue manifest", issueSetID)
	}
	for _, issue := range m.Issues {
		if issue.ID == issueRaw {
			return issueSetID, issueRaw, nil
		}
	}
	return "", "", exitErr(ExitNoRunnable, "%s", unknownIssueMessage(m, issueRaw))
}

func completionLooksPathLike(toComplete string) bool {
	if toComplete == "" {
		return false
	}
	if strings.HasPrefix(toComplete, "./") || strings.HasPrefix(toComplete, "../") {
		return true
	}
	return strings.Contains(toComplete, "thoughts/")
}

func issueSetDirPrefix(issueSetID string) string {
	return filepath.ToSlash(filepath.Join("thoughts", "issues", issueSetID)) + "/"
}

func issueSetPathCompletions(refresh *RefreshResult, toComplete string) []string {
	if refresh == nil || refresh.Manifests == nil {
		return nil
	}
	slash := filepath.ToSlash(toComplete)
	var ids []string
	for id := range refresh.Manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []string
	for _, id := range ids {
		candidate := filepath.ToSlash(filepath.Join("thoughts", "issues", id))
		if slash == "" || strings.HasPrefix(candidate, slash) {
			out = append(out, candidate)
		}
	}
	return out
}

func issuePathCompletions(refresh *RefreshResult, issueSetRaw, cwd, toComplete string) []string {
	if refresh == nil {
		return nil
	}
	if completionLooksPathLike(toComplete) {
		slash := filepath.ToSlash(toComplete)
		for id := range refresh.Manifests {
			prefix := issueSetDirPrefix(id)
			if !strings.HasPrefix(slash, prefix) {
				continue
			}
			m := refresh.Manifests[id]
			if m == nil {
				return nil
			}
			var out []string
			for _, issue := range m.Issues {
				candidate := prefix + issue.File
				if strings.HasPrefix(candidate, slash) {
					out = append(out, candidate)
				}
			}
			return out
		}
		return issueSetPathCompletions(refresh, toComplete)
	}

	issueSetID, _ := ResolveIssueSetTarget(defaultDeps, refresh, cwd, issueSetRaw)
	if issueSetID == "" {
		return nil
	}
	m := refresh.Manifests[issueSetID]
	if m == nil {
		return nil
	}
	var ids []string
	for _, issue := range m.Issues {
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	return ids
}
