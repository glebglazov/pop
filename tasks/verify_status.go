package tasks

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
)

// verifyEnabled reports whether Agent verification is enabled in user config
// (ADR-0086). This is the master opt-in switch, which defaults off, so an
// unconfigured or disabled [tasks.verify] leaves status deriving from the
// manifest alone exactly as before the feature. The rest of the surface (the
// agent fallback list, effort, and remediation cap) drives selection only once
// this gate is on.
func verifyEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Task != nil && cfg.Task.Verify != nil && cfg.Task.Verify.Enabled
}

// ApplyVerifyVerdicts re-derives each row's status through the Verify verdict
// (ADR-0086/0096) when Agent verification is enabled, then restores the
// display order so a formerly-Done set that now needs verification lands with
// the active sets. It is a no-op when the feature is disabled — status then
// derives from the manifest alone, exactly as before.
//
// Every row shares one runtime checkout — the cwd checkout `pop tasks status`
// resolves. Call ApplyVerifyVerdictsWith when each set may drain in its own
// checkout (the queue dashboard, queue scan).
//
// The verdict lookup first checks the current work SHA. A PASS verdict at HEAD
// lets the terminal status stand; any non-PASS verdict at HEAD forces
// VERIFY-FAILED. When there is no verdict at HEAD, the most recent PASS
// verdict for the set immunizes the terminal status against later commits
// (ADR-0096). Only when no PASS verdict exists in the episode does the set
// regress to NEEDS-VERIFY. The pass is best-effort: an unresolvable repo or work
// SHA, or the absence of a store, resolves every terminal set to NEEDS-VERIFY
// rather than leaving it falsely Done — the gate never fails open.
func ApplyVerifyVerdicts(d *Deps, result *RefreshResult, cfg *config.Config, runtimePath string) {
	ApplyVerifyVerdictsWith(d, result, cfg, func(string) string { return runtimePath })
}

// ApplyVerifyVerdictsWith is ApplyVerifyVerdicts with a per-set runtime checkout
// resolver. runtimeForSet returns the checkout whose HEAD gates Verify verdict
// lookup for that set — typically its Worktree binding path when bound, else
// the repo's representative checkout.
//
// The pass is also a no-op on an archived view (result.ShowArchived): an
// Archived Task set is outside the verification loop (ADR-0026), so
// `pop tasks status --archived` lists each set at its manifest-derived status
// rather than regressing a formerly-Done set to NEEDS-VERIFY. Only
// RefreshArchivedWith sets ShowArchived; the active, register, and queue
// callers all leave it false.
func ApplyVerifyVerdictsWith(d *Deps, result *RefreshResult, cfg *config.Config, runtimeForSet func(setID string) string) {
	if result == nil || result.ShowArchived || !verifyEnabled(cfg) {
		return
	}
	if runtimeForSet == nil {
		runtimeForSet = func(string) string { return "" }
	}

	var s *store.Store
	if st, ok, err := openDrainStoreIfExists(d); err == nil && ok {
		s = st
	}

	// Resolving a checkout's repo identity and HEAD each forks git, and sets
	// that share a checkout (the common case: every unbound set resolves the
	// repo's representative path) resolve the identical pair. Memoize per
	// runtimePath so a dashboard with N terminal sets on one checkout forks git
	// twice, not 2N times.
	type checkoutInfo struct {
		repo    string
		workSHA string
	}
	checkoutCache := map[string]checkoutInfo{}
	resolveCheckout := func(runtimePath string) checkoutInfo {
		if info, ok := checkoutCache[runtimePath]; ok {
			return info
		}
		info := checkoutInfo{}
		if runtimePath != "" {
			if id, err := ResolveRepositoryIdentity(d, runtimePath); err == nil {
				info.repo = id.CommonDir
			}
		}
		info.workSHA = verifyWorkSHA(d, runtimePath)
		checkoutCache[runtimePath] = info
		return info
	}

	changed := false
	for i := range result.Rows {
		row := &result.Rows[i]
		// Only a terminal row (DONE/AWAITING-APPROVAL) consults the verdict;
		// decorateRowWithVerdict is a no-op for every other status. Skip the
		// git-forking checkout resolution and store lookup for non-terminal rows
		// entirely, mirroring decorateRowWithVerdict's own non-terminal branch by
		// clearing the immunized-SHA badge.
		if row.Status != StatusDone && row.Status != StatusAwaitingApproval {
			row.VerifiedAtSHA = ""
			continue
		}
		info := resolveCheckout(runtimeForSet(row.ID))
		var current *store.VerifyVerdict
		var latestPass *store.VerifyVerdict
		if s != nil && info.repo != "" && info.workSHA != "" {
			if v, err := s.GetVerifyVerdict(info.repo, row.ID, info.workSHA); err == nil {
				current = v
			}
			if v, err := s.GetLatestPassVerifyVerdict(info.repo, row.ID); err == nil {
				latestPass = v
			}
		}
		if decorateRowWithVerdict(row, result.Manifests[row.ID], info.workSHA, current, latestPass) {
			changed = true
		}
	}
	if changed {
		result.Rows = orderStatusRows(result.Rows)
	}
}

// decorateRowWithVerdict re-derives one row's status through its Verify verdict
// and refreshes the status-dependent display fields. It reports whether the
// status changed, so the caller can re-order the table.
//
// The verdict gates only the terminal zone — a set whose manifest status is
// already DONE or AWAITING-APPROVAL. Every other row (missing, malformed,
// ready, failed, deferred, blocked) is left untouched; in particular a missing
// row carries no manifest, so re-deriving it would wrongly read as MALFORMED.
func decorateRowWithVerdict(row *Row, m *Manifest, workSHA string, currentVerdict, latestPass *store.VerifyVerdict) bool {
	if row.Status != StatusDone && row.Status != StatusAwaitingApproval {
		row.VerifiedAtSHA = ""
		return false
	}
	status, verifiedAtSHA := ResolveVerifiedStatus(m, workSHA, currentVerdict, latestPass)
	row.VerifiedAtSHA = verifiedAtSHA

	if status == row.Status {
		return false
	}
	row.Status = status
	row.Progress = BuildProgress(m, status)
	row.VerifyFindings = ""
	if status == StatusVerifyFailed && currentVerdict != nil {
		row.VerifyFindings = currentVerdict.Findings
	}
	return true
}
