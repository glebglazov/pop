package tasks

import (
	"strings"

	"github.com/glebglazov/pop/config"
)

// Refine pass outcomes the Refiner reports on REFINE-OUTCOME: (ADR-0248
// decision 14). Missing or unrecognised values fall through to refined — the
// same trust model as VERDICT:, caught one step later by the verify phase.
const (
	refineOutcomeRefined     = "refined"
	refineOutcomeGateBlocked = "gate-blocked"
	refineOutcomeAbandoned   = "abandoned"
)

// refineTreeSnapshot is the working-tree state captured before the Refiner
// runs, so an abandoned pass can discard only what the pass wrote and leave
// any pre-existing dirt untouched (ADR-0248 decision 10).
type refineTreeSnapshot struct {
	// stash is a `git stash create` object of the pre-pass tree, empty when the
	// tree was already clean.
	stash string
}

// splitRefinerReply peels the machine-read channel lines off the front of the
// Refiner's reply — `REFINE-OUTCOME:` and `COMMIT-SUBJECT:` — and returns the
// prose that is left as the report body. Both ride the reply rather than a
// second invocation because the Refiner is the only party that knows how the
// pass ended and what it fixed.
//
// The scan stops at the report's first Markdown heading: everything before the
// document begins is pop's channel, and a line inside the report that happens
// to open with either label is the Refiner writing about the pass, not asking
// pop to act on it.
//
// Outcome defaults to refined. A missing line, or a value that is not one of
// the three canonical tokens, leaves the report whole and treats the pass as
// refined — self-reported like VERDICT:, and caught later by the verify phase.
// A recognised outcome line is always lifted, even when it is the whole reply,
// so it never lands in the document a human reads. A subject line that is the
// whole reply stays put: with no report body behind it, dropping it would
// leave an empty document that records less than the line it removed, and the
// subject falls back to pop's default format either way.
func splitRefinerReply(raw string) (subject, outcome, body string) {
	outcome = refineOutcomeRefined
	lines := strings.Split(raw, "\n")
	drop := make([]bool, len(lines))
	subjectIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		if value, ok := refineOutcomeLabelValue(trimmed); ok {
			if canon, ok := canonicalRefineOutcome(value); ok {
				outcome = canon
				drop[i] = true
			}
			continue
		}
		if value, ok := commitSubjectLabelValue(trimmed); ok {
			if subjectIdx < 0 {
				subject = sanitizeAgentCommitSubject(value)
				subjectIdx = i
			}
		}
	}
	if subjectIdx >= 0 {
		drop[subjectIdx] = true
	}
	body = joinUndroppedLines(lines, drop)
	if body == "" && subjectIdx >= 0 {
		// Subject-only (aside from any lifted outcome): keep the subject line in
		// the document and clear the parsed subject so the commit falls back to
		// pop's default — matching the pre-outcome behaviour. Outcomes stay out.
		drop[subjectIdx] = false
		subject = ""
		body = joinUndroppedLines(lines, drop)
	}
	return subject, outcome, body
}

// joinUndroppedLines joins lines whose drop flag is false, then trims.
func joinUndroppedLines(lines []string, drop []bool) string {
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if drop[i] {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// refineOutcomeLabelValue reports whether a line is the `REFINE-OUTCOME:` label
// line and, if so, returns the text after the label. Markdown decoration and
// separator variants (`REFINE OUTCOME`, `REFINE_OUTCOME`) are tolerated; a
// colon delimiter is required so prose that opens with the words is not read
// as the label.
func refineOutcomeLabelValue(line string) (string, bool) {
	stripped := stripMarkdown(line)
	up := strings.ToUpper(stripped)
	if !strings.HasPrefix(up, "REFINE") {
		return "", false
	}
	rest := stripped[len("REFINE"):]
	rest = strings.TrimLeft(rest, "-_ \t")
	if !strings.HasPrefix(strings.ToUpper(rest), "OUTCOME") {
		return "", false
	}
	rest = rest[len("OUTCOME"):]
	if rest == "" || (rest[0] != ':' && rest[0] != '*') {
		return "", false
	}
	return strings.TrimLeft(rest, "*: \t"), true
}

// canonicalRefineOutcome maps an outcome token to its canonical form.
func canonicalRefineOutcome(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(stripMarkdown(s))) {
	case refineOutcomeRefined:
		return refineOutcomeRefined, true
	case refineOutcomeGateBlocked:
		return refineOutcomeGateBlocked, true
	case refineOutcomeAbandoned:
		return refineOutcomeAbandoned, true
	}
	return "", false
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

// captureRefineTree records the working-tree state before the Refiner runs, so
// an abandoned pass can restore exactly that state — pre-existing dirt included
// — rather than sweeping whatever is dirty at commit time. A clean tree yields
// an empty snapshot; the capture stages briefly to fold untracked files into
// the stash object, then unstages so the Refiner still sees a dirty tree.
func captureRefineTree(d *Deps, runtimePath string) (refineTreeSnapshot, error) {
	dirty, err := runtimeHasChanges(d, runtimePath)
	if err != nil {
		return refineTreeSnapshot{}, exitErr(ExitOperational, "check runtime changes: %v", err)
	}
	if !dirty {
		return refineTreeSnapshot{}, nil
	}
	if _, err := d.Git.CommandInDir(runtimePath, "add", "-A"); err != nil {
		return refineTreeSnapshot{}, exitErr(ExitOperational, "capture refine tree: %v", err)
	}
	sha, err := d.Git.CommandInDir(runtimePath, "stash", "create", "pop-refine-pre")
	if err != nil {
		_, _ = d.Git.CommandInDir(runtimePath, "reset")
		return refineTreeSnapshot{}, exitErr(ExitOperational, "capture refine tree: %v", err)
	}
	if _, err := d.Git.CommandInDir(runtimePath, "reset"); err != nil {
		return refineTreeSnapshot{}, exitErr(ExitOperational, "capture refine tree: %v", err)
	}
	return refineTreeSnapshot{stash: strings.TrimSpace(sha)}, nil
}

// discardRefinePassChanges restores the tree to the snapshot taken before the
// Refiner ran: hard-reset and clean wipe the pass's edits, then re-apply any
// pre-existing dirt the snapshot held. A clean pre-pass tree stays clean.
func discardRefinePassChanges(d *Deps, runtimePath string, snap refineTreeSnapshot) error {
	if _, err := d.Git.CommandInDir(runtimePath, "reset", "--hard", "HEAD"); err != nil {
		return exitErr(ExitOperational, "discard refine edits: %v", err)
	}
	if _, err := d.Git.CommandInDir(runtimePath, "clean", "-fd"); err != nil {
		return exitErr(ExitOperational, "discard refine edits: %v", err)
	}
	if snap.stash == "" {
		return nil
	}
	if _, err := d.Git.CommandInDir(runtimePath, "stash", "apply", snap.stash); err != nil {
		return exitErr(ExitOperational, "discard refine edits: %v", err)
	}
	// stash create after add -A stages formerly-untracked files; mixed reset
	// restores the unstaged / untracked shape the pass found.
	if _, err := d.Git.CommandInDir(runtimePath, "reset"); err != nil {
		return exitErr(ExitOperational, "discard refine edits: %v", err)
	}
	return nil
}

// commitRefinePass captures whatever the Refiner left in the working tree as one
// commit and returns its SHA, or the empty string when the pass edited nothing
// (ADR-0240). Agents never commit, so this is the only place a refine pass
// reaches git history — and it runs on both pass paths, the drain's refine phase
// and `pop tasks refine`, because both leave the same tree behind.
//
// Outcome gates the write (ADR-0248 decision 14/15): gate-blocked commits
// nothing; abandoned discards the pass's edits against the pre-pass snapshot
// and commits nothing; refined is today's path. A pass that fixed nothing
// commits nothing either: an empty commit would put a subject claiming a
// refinement on a tree identical to its parent. The staged check is the second
// gate for the same reason the executor keeps one — `add -A` on a tree whose
// only change is ignored by git stages nothing.
func commitRefinePass(d *Deps, cfg *config.Config, runtimePath, setID, subject, outcome string, snap refineTreeSnapshot) (string, error) {
	switch outcome {
	case refineOutcomeGateBlocked:
		return "", nil
	case refineOutcomeAbandoned:
		if err := discardRefinePassChanges(d, runtimePath, snap); err != nil {
			return "", err
		}
		return "", nil
	}
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
