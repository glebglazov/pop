package tasks

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/store"
)

// Mergeability verdicts. A stored row is always one of clean/conflicts; unknown
// is a read-time fallback shown only between a set going Done and the next
// reconcile, never persisted as a steady state (ADR-0055).
const (
	MergeVerdictClean     = "clean"
	MergeVerdictConflicts = "conflicts"
	MergeVerdictUnknown   = "unknown"
)

// MergeabilityEntry is the layer-2 merge verdict for one Done set, keyed (by the
// caller) per repository identity plus set id. It carries the working and
// runtime HEADs the verdict was computed from so reconcile can SHA-gate
// recomputation (ADR-0051/0055).
type MergeabilityEntry struct {
	Project     string
	RuntimePath string
	WorkingPath string
	SetID       string
	Verdict     string
	BaseSHA     string
	BranchSHA   string
	ComputedAt  time.Time
}

// LoadMergeabilityEntries returns every stored mergeability entry keyed by its
// scoped key. It opens the store only when it already exists, so a pure reader
// never materialises an empty database.
func LoadMergeabilityEntries(d *Deps) (map[string]MergeabilityEntry, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return map[string]MergeabilityEntry{}, err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.AllMergeability()
	if err != nil {
		return nil, err
	}
	out := make(map[string]MergeabilityEntry, len(rows))
	for key, m := range rows {
		out[key] = entryFromStore(m)
	}
	return out, nil
}

// SaveMergeabilityEntries replaces the entire stored mergeability set with all,
// creating the store on first write. It mirrors the whole-store rewrite the
// file-backed store used.
func SaveMergeabilityEntries(d *Deps, all map[string]MergeabilityEntry) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	rows := make(map[string]store.Mergeability, len(all))
	for key, e := range all {
		rows[key] = storeFromEntry(key, e)
	}
	return s.ReplaceAllMergeability(rows)
}

// PutMergeabilityEntry upserts one mergeability entry under scopedKey.
func PutMergeabilityEntry(d *Deps, scopedKey string, e MergeabilityEntry) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.PutMergeability(storeFromEntry(scopedKey, e))
}

// DeleteMergeabilityEntry forgets the entry under scopedKey. It opens the store
// only when it already exists.
func DeleteMergeabilityEntry(d *Deps, scopedKey string) error {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.DeleteMergeability(scopedKey)
}

func entryFromStore(m store.Mergeability) MergeabilityEntry {
	return MergeabilityEntry{
		Project:     m.Project,
		RuntimePath: m.RuntimePath,
		WorkingPath: m.WorkingPath,
		SetID:       m.SetID,
		Verdict:     m.Verdict,
		BaseSHA:     m.BaseSHA,
		BranchSHA:   m.BranchSHA,
		ComputedAt:  m.ComputedAt,
	}
}

func storeFromEntry(key string, e MergeabilityEntry) store.Mergeability {
	return store.Mergeability{
		ScopedKey:   key,
		Project:     e.Project,
		RuntimePath: e.RuntimePath,
		WorkingPath: e.WorkingPath,
		SetID:       e.SetID,
		Verdict:     e.Verdict,
		BaseSHA:     e.BaseSHA,
		BranchSHA:   e.BranchSHA,
		ComputedAt:  e.ComputedAt,
	}
}

// ComputeMergeVerdict dry-runs merging a runtime branch into the working
// checkout. It forks `git rev-parse` for the two HEADs and one `git merge-tree`
// for the verdict. Used when a Drain reaches Done and when reconcile must
// recompute a stale verdict.
func ComputeMergeVerdict(d *Deps, workingPath, runtimePath string) (verdict, base, branch string, err error) {
	if d == nil || d.Git == nil {
		return "", "", "", errors.New("missing git dependencies")
	}
	base, err = revParseHead(d, workingPath)
	if err != nil {
		return "", "", "", err
	}
	branch, err = revParseHead(d, runtimePath)
	if err != nil {
		return "", "", "", err
	}
	verdict, err = MergeTreeVerdict(d, workingPath, base, branch)
	if err != nil {
		return "", "", "", err
	}
	return verdict, base, branch, nil
}

func revParseHead(d *Deps, checkout string) (string, error) {
	out, err := d.Git.CommandInDir(checkout, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// MergeTreeVerdict runs `git merge-tree --write-tree base branch` and maps its
// exit code to a verdict: 0 is clean, 1 is conflicts, anything else is an error.
func MergeTreeVerdict(d *Deps, workingPath, base, branch string) (string, error) {
	if _, err := d.Git.CommandInDir(workingPath, "merge-tree", "--write-tree", base, branch); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", err
		}
		return MergeVerdictConflicts, nil
	}
	return MergeVerdictClean, nil
}

// ResolveHeadSHA reads the commit a checkout's HEAD points at directly from its
// git ref files through the filesystem seam — no `git` fork. This is what lets
// the reconcile SHA gate stay cheap: an unchanged set is detected without
// forking git, and only a changed set pays for a `git merge-tree` (ADR-0055).
// It returns false when HEAD cannot be resolved (caller skips that entry).
func ResolveHeadSHA(d *Deps, checkout string) (string, bool) {
	if d == nil || d.FS == nil || checkout == "" {
		return "", false
	}
	gitDir := filepath.Join(checkout, ".git")
	// A linked worktree's .git is a file "gitdir: <path>" pointing at the
	// per-worktree git dir; a normal checkout's .git is a directory.
	if data, err := d.FS.ReadFile(gitDir); err == nil {
		line := strings.TrimSpace(string(data))
		if rest, ok := strings.CutPrefix(line, "gitdir:"); ok {
			dir := strings.TrimSpace(rest)
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(checkout, dir)
			}
			gitDir = filepath.Clean(dir)
		}
	}

	head, err := d.FS.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	headLine := strings.TrimSpace(string(head))
	rest, ok := strings.CutPrefix(headLine, "ref:")
	if !ok {
		// Detached HEAD: the line is the commit SHA itself.
		if headLine == "" {
			return "", false
		}
		return headLine, true
	}
	ref := strings.TrimSpace(rest)

	// Branch refs live in the common dir, which a linked worktree names via its
	// `commondir` file; a normal checkout's common dir is the git dir itself.
	commonDir := gitDir
	if data, err := d.FS.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		cd := strings.TrimSpace(string(data))
		if cd != "" {
			if !filepath.IsAbs(cd) {
				cd = filepath.Join(gitDir, cd)
			}
			commonDir = filepath.Clean(cd)
		}
	}

	// Loose ref: try the per-worktree dir first (per-worktree HEAD targets), then
	// the common dir (shared branch refs).
	for _, base := range []string{gitDir, commonDir} {
		if data, err := d.FS.ReadFile(filepath.Join(base, filepath.FromSlash(ref))); err == nil {
			if sha := strings.TrimSpace(string(data)); sha != "" {
				return sha, true
			}
		}
	}
	// Packed refs live in the common dir.
	if data, err := d.FS.ReadFile(filepath.Join(commonDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == ref {
				return fields[0], true
			}
		}
	}
	return "", false
}

// reconcileMergeability is the SHA-gated mergeability refresh every layer-2
// reader runs alongside crash reconciliation (ADR-0055). For each stored entry
// it reads the current working and runtime HEADs from the filesystem (no git
// fork); only when a HEAD no longer matches the stored SHA does it fork
// `git merge-tree` to recompute the verdict and rewrite the row. An unchanged
// set forks nothing. This both fills a transient `unknown` and flips a
// once-clean verdict to conflicts after trunk advances into a conflicting state.
func reconcileMergeability(d *Deps, s *store.Store, now time.Time) error {
	entries, err := s.AllMergeability()
	if err != nil {
		return err
	}
	for key, m := range entries {
		if m.RuntimePath == "" || m.WorkingPath == "" {
			continue
		}
		base, ok := ResolveHeadSHA(d, m.WorkingPath)
		if !ok {
			continue
		}
		branch, ok := ResolveHeadSHA(d, m.RuntimePath)
		if !ok {
			continue
		}
		if base == m.BaseSHA && branch == m.BranchSHA {
			continue // SHA gate: HEADs unchanged, fork no git
		}
		verdict, err := MergeTreeVerdict(d, m.WorkingPath, base, branch)
		if err != nil {
			continue // advisory: a recompute failure leaves the prior verdict
		}
		m.ScopedKey = key
		m.Verdict = verdict
		m.BaseSHA = base
		m.BranchSHA = branch
		m.ComputedAt = now
		if err := s.PutMergeability(m); err != nil {
			return err
		}
	}
	return nil
}
