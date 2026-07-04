package tasks

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
)

// verifyEnabled reports whether Agent verification is enabled in user config
// (ADR-0086). The full [tasks.verify] surface — the agent fallback list, effort,
// and remediation cap — lands in a later slice; until then this reads only the
// master opt-in switch, which defaults off, so status derives from the manifest
// alone exactly as before the feature.
func verifyEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Task != nil && cfg.Task.Verify != nil && cfg.Task.Verify.Enabled
}

// ApplyVerifyVerdicts re-derives each row's status through the SHA-gated Verify
// verdict (ADR-0086) when Agent verification is enabled, then restores the
// display order so a formerly-Done set that now needs verification lands with
// the active sets. It is a no-op when the feature is disabled — status then
// derives from the manifest alone, exactly as before.
//
// The verdict lookup is keyed by the runtime checkout's current work SHA, so a
// verdict recorded at an earlier SHA simply misses (stale → nil → NEEDS-VERIFY).
// The pass is best-effort: an unresolvable repo or work SHA, or the absence of a
// store, resolves every terminal set to NEEDS-VERIFY (a nil verdict) rather than
// leaving it falsely Done — the gate never fails open.
func ApplyVerifyVerdicts(d *Deps, result *RefreshResult, cfg *config.Config, runtimePath string) {
	if result == nil || !verifyEnabled(cfg) {
		return
	}

	repo := ""
	if id, err := ResolveRepositoryIdentity(d, runtimePath); err == nil {
		repo = id.CommonDir
	}
	workSHA := verifyWorkSHA(d, runtimePath)

	var s *store.Store
	if st, ok, err := openDrainStoreIfExists(d); err == nil && ok {
		s = st
		defer func() { _ = s.Close() }()
	}

	changed := false
	for i := range result.Rows {
		row := &result.Rows[i]
		var verdict *store.VerifyVerdict
		if s != nil && repo != "" {
			if v, err := s.GetVerifyVerdict(repo, row.ID, workSHA); err == nil {
				verdict = v
			}
		}
		if decorateRowWithVerdict(row, result.Manifests[row.ID], verdict) {
			changed = true
		}
	}
	if changed {
		result.Rows = orderStatusRows(result.Rows)
	}
}

// decorateRowWithVerdict re-derives one row's status through its Verify verdict
// (nil = absent or stale) and refreshes the status-dependent display fields.
// It reports whether the status changed, so the caller can re-order the table.
//
// The verdict gates only the terminal zone — a set whose manifest status is
// already DONE or AWAITING-APPROVAL. Every other row (missing, malformed,
// ready, failed, deferred, blocked) is left untouched; in particular a missing
// row carries no manifest, so re-deriving it would wrongly read as MALFORMED.
func decorateRowWithVerdict(row *Row, m *Manifest, verdict *store.VerifyVerdict) bool {
	if row.Status != StatusDone && row.Status != StatusAwaitingApproval {
		return false
	}
	var v *Verdict
	if verdict != nil {
		vv := Verdict(verdict.Verdict)
		v = &vv
	}
	status := DeriveStatusWithVerdict(m, true, v)
	if status == row.Status {
		return false
	}
	row.Status = status
	row.Progress = BuildProgress(m, status)
	row.VerifyFindings = ""
	if status == StatusVerifyFailed && verdict != nil {
		row.VerifyFindings = verdict.Findings
	}
	return true
}
