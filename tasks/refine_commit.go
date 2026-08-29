package tasks

import (
	"strings"

	"github.com/glebglazov/pop/config"
)

// splitRefinerReply separates the subject the Refiner rendered for its own
// commit from the Refine report that follows it. The subject rides the reply
// rather than a second invocation because the Refiner is the only party that
// knows what it fixed, and it has already read the set's Commit convention by
// the time it writes the line.
//
// The scan stops at the report's first Markdown heading: everything before the
// document begins is pop's channel, and a line inside the report that happens
// to open with the label is the Refiner writing about a commit, not asking for
// one. A reply with no subject at all is not malformed — it falls back to pop's
// default format — so the whole body is returned unchanged.
func splitRefinerReply(raw string) (string, string) {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		value, ok := commitSubjectLabelValue(trimmed)
		if !ok {
			continue
		}
		body := strings.TrimSpace(strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n"))
		if body == "" {
			break
		}
		return sanitizeAgentCommitSubject(value), body
	}
	return "", strings.TrimSpace(raw)
}

// refineCommitSubject picks the subject the pass's commit is written under: the
// Refiner's rendered line used verbatim, or pop's default refine format. This is
// the Planned commit subject's rule applied to a commit pop makes for itself —
// the agent writes the prose because only it knows what changed, and pop owns
// the fallback so no pass can fail for want of a subject.
//
// A set with no recorded Commit convention gets the default outright: the
// Refiner was never shown a convention, so any line it rendered is a guess at a
// house style nobody stated.
func refineCommitSubject(m *Manifest, setID, rendered string) string {
	if m != nil && strings.TrimSpace(m.CommitConvention) != "" {
		if subject := sanitizeAgentCommitSubject(rendered); subject != "" {
			return subject
		}
	}
	return RefineCommitSubject(setID)
}

// commitRefinePass captures whatever the Refiner left in the working tree as one
// commit and returns its SHA, or the empty string when the pass edited nothing
// (ADR-0240). Agents never commit, so this is the only place a refine pass
// reaches git history — and it runs on both pass paths, the drain's refine phase
// and `pop tasks refine`, because both leave the same tree behind.
//
// A pass that fixed nothing commits nothing: an empty commit would put a subject
// claiming a refinement on a tree identical to its parent. The staged check is
// the second gate for the same reason the executor keeps one — `add -A` on a tree
// whose only change is ignored by git stages nothing.
func commitRefinePass(d *Deps, cfg *config.Config, runtimePath, setID, subject string) (string, error) {
	dirty, err := runtimeHasChanges(d, runtimePath)
	if err != nil {
		return "", exitErr(ExitOperational, "check runtime changes: %v", err)
	}
	if !dirty {
		return "", nil
	}
	overrides, err := cfg.ResolveCommitConfigOverrides()
	if err != nil {
		return "", exitErr(ExitSetup, "%v", err)
	}
	if _, err := d.Git.CommandInDir(runtimePath, "add", "-A"); err != nil {
		return "", exitErr(ExitOperational, "stage refine edits: %v", err)
	}
	staged, err := d.Git.CommandInDir(runtimePath, "diff", "--cached", "--name-only")
	if err != nil {
		return "", exitErr(ExitOperational, "stage refine edits: %v", err)
	}
	if strings.TrimSpace(staged) == "" {
		return "", nil
	}
	// A second `-m` puts the trailer in a paragraph of its own, which is what
	// makes git read it as a trailer rather than as the subject's continuation.
	args := commitGitArgs(overrides, "commit", "-m", subject, "-m", RefineTrailer(setID))
	if _, err := d.Git.CommandInDir(runtimePath, args...); err != nil {
		return "", exitErr(ExitOperational, "refine commit: %v", err)
	}
	sha, err := d.Git.CommandInDir(runtimePath, "rev-parse", "HEAD")
	if err != nil {
		return "", exitErr(ExitOperational, "refine commit: %v", err)
	}
	return strings.TrimSpace(sha), nil
}
