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

func resolveIssueSetReference(d *Deps, refresh *RefreshResult, cwd, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !isPathLike(raw) {
		return resolveIssueSetIdentifier(refresh, raw)
	}
	return resolveIssueSetPath(d, refresh, cwd, raw)
}

func resolveIssueSetRelativeIssue(refresh *RefreshResult, raw string) (issueSetID, issueID string, ok bool, err error) {
	raw = strings.TrimSpace(filepath.ToSlash(raw))
	if raw == "" || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || strings.HasPrefix(raw, "thoughts/") {
		return "", "", false, nil
	}
	head, tail, found := strings.Cut(raw, "/")
	if !found {
		return "", "", false, nil
	}
	issueSetID, err = resolveIssueSetIdentifier(refresh, head)
	if err != nil {
		return "", "", false, nil
	}
	if tail == "" {
		return issueSetID, "", true, nil
	}
	issueID, err = matchIssueFileField(refresh.Manifests[issueSetID], tail)
	if err != nil {
		return "", "", true, err
	}
	return issueSetID, issueID, true, nil
}

func resolveIssueSetPath(d *Deps, refresh *RefreshResult, cwd, raw string) (string, error) {
	if filepath.IsAbs(expandHome(d, raw)) {
		return "", exitErr(ExitSetup, "Issue set target %q must be a CWD-relative path", raw)
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

// ResolveIssueSetTarget normalizes an Issue set identifier or CWD-relative path to its identifier.
func ResolveIssueSetTarget(d *Deps, refresh *RefreshResult, cwd, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if filepath.IsAbs(expandHome(d, raw)) {
		return "", exitErr(ExitSetup, "Issue set target %q must be a CWD-relative path", raw)
	}
	return resolveIssueSetReference(d, refresh, cwd, raw)
}

// ResolveIssueTarget normalizes an Issue set identifier, Issue set path, or issue markdown path to canonical identifiers.
func ResolveIssueTarget(d *Deps, refresh *RefreshResult, cwd, raw string) (issueSetID, issueID string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if filepath.IsAbs(expandHome(d, raw)) {
		return "", "", exitErr(ExitSetup, "issue target %q must be a CWD-relative path", raw)
	}
	if !isIssueFileReference(raw) {
		issueSetID, err = resolveIssueSetIdentifier(refresh, raw)
		return issueSetID, "", err
	}

	issueSetID, issueID, ok, err := resolveIssueSetRelativeIssue(refresh, raw)
	if err != nil || ok {
		return issueSetID, issueID, err
	}

	if isPathLike(raw) {
		abs, absErr := absFromCWD(d, cwd, raw)
		if absErr != nil {
			return "", "", exitErr(ExitSetup, "resolve issue path: %v", absErr)
		}
		if issueSetID := matchIssueSetDir(refresh, abs); issueSetID != "" {
			return issueSetID, "", nil
		}
	}

	issueSetID, issueID, ok, err = resolveIssueFromPath(d, refresh, cwd, raw)
	if err != nil {
		return "", "", err
	}
	if ok {
		return issueSetID, issueID, nil
	}
	return "", "", exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, raw))
}

// ResolveIssueFileTarget normalizes a CWD-relative issue markdown path to canonical identifiers.
func ResolveIssueFileTarget(d *Deps, refresh *RefreshResult, cwd, raw string) (issueSetID, issueID string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if !isIssueFileReference(raw) || filepath.IsAbs(expandHome(d, raw)) {
		return "", "", exitErr(ExitSetup, "issue target %q must be a CWD-relative path", raw)
	}

	issueSetID, issueID, ok, err := resolveIssueFromPath(d, refresh, cwd, raw)
	if err != nil {
		return "", "", err
	}
	if ok {
		return issueSetID, issueID, nil
	}
	return "", "", exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, raw))
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
			resolvedSet, resolveErr := resolveIssueSetReference(d, refresh, cwd, issueSetRaw)
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

	issueSetID, err = resolveIssueSetReference(d, refresh, cwd, issueSetRaw)
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
	slash := filepath.ToSlash(toComplete)
	if strings.HasPrefix(slash, "./") || strings.HasPrefix(slash, "../") {
		return true
	}
	return strings.Contains(slash, "thoughts/") || strings.HasPrefix("thoughts/", slash)
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

func issueSetPathCompletionsFromCWD(refresh *RefreshResult, cwd, toComplete string) []string {
	if refresh == nil || refresh.Manifests == nil {
		return nil
	}
	var out []string
	for _, m := range refresh.Manifests {
		if m == nil {
			continue
		}
		candidate, err := filepath.Rel(cwd, m.Dir)
		if err != nil {
			continue
		}
		candidate = completionPathStyle(filepath.ToSlash(candidate), toComplete)
		if strings.HasPrefix(candidate, filepath.ToSlash(toComplete)) {
			out = append(out, candidate)
		}
	}
	sort.Strings(out)
	return out
}

func issuePathCompletionsFromCWD(refresh *RefreshResult, cwd, toComplete string) []string {
	if refresh == nil || refresh.Manifests == nil {
		return nil
	}
	var out []string
	for _, m := range refresh.Manifests {
		if m == nil {
			continue
		}
		dir, err := filepath.Rel(cwd, m.Dir)
		if err != nil {
			continue
		}
		dir = filepath.ToSlash(dir)
		styledDir := completionPathStyle(dir, toComplete)
		if strings.HasPrefix(styledDir, filepath.ToSlash(toComplete)) {
			out = append(out, styledDir)
		}
		for _, issue := range m.Issues {
			candidate := filepath.ToSlash(filepath.Join(dir, issue.File))
			candidate = completionPathStyle(candidate, toComplete)
			if strings.HasPrefix(candidate, filepath.ToSlash(toComplete)) {
				out = append(out, candidate)
			}
		}
	}
	sort.Strings(out)
	return out
}

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

func completionPathStyle(candidate, toComplete string) string {
	if strings.HasPrefix(filepath.ToSlash(toComplete), "./") && candidate != "." && !strings.HasPrefix(candidate, "../") {
		return "./" + candidate
	}
	return candidate
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

	issueSetID, _ := resolveIssueSetReference(defaultDeps, refresh, cwd, issueSetRaw)
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
