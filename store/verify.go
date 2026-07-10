package store

import (
	"database/sql"
	"time"
)

// VerifyVerdict is the stored, SHA-gated Verify verdict for a Task set
// (ADR-0086): an independent Verifier agent's judgment of the set's completed
// AFK work. Verdict is "PASS", "FIXABLE", or "NEEDS-HUMAN"; WorkSHA is the
// runtime HEAD the verdict was computed from, so a reader gates on a SHA change
// (a verdict at a different SHA is stale). Findings carries the Verifier's
// human-facing reasons (empty for PASS). Repo is the repository's git common
// dir, the same identity the drains table keys by. Scope is the count of AFK
// tasks the verdict certified (ADR-0101): a reader compares the set's current
// AFK count against it to tell an incidental SHA move (coast on the immunizing
// PASS per ADR-0096) apart from a scope increase (re-verify the enlarged set).
// A zero Scope means unknown (legacy rows, or a verdict written before the
// scope was recorded) and disables the growth check.
//
// HumanAuthored marks an Accepted verdict (ADR-0103): a human overriding a
// non-PASS Verifier judgment by recording a PASS themselves. Note carries the
// human's rationale — fed forward as context into later Verifier prompts so the
// known non-issue is not re-flagged, without suppressing a fresh judgment. An
// agent-authored verdict has HumanAuthored=false and an empty Note.
type VerifyVerdict struct {
	Repo          string
	SetID         string
	WorkSHA       string
	Verdict       string
	Findings      string
	Scope         int
	HumanAuthored bool
	Note          string
	ComputedAt    time.Time
}

// GetVerifyVerdict returns the verdict stored for (repo, set, work SHA), or nil
// when none is recorded. A caller gates verification on the current work SHA: a
// nil result means the set has no fresh verdict (absent or stale) and must be
// re-verified.
func (s *Store) GetVerifyVerdict(repo, setID, workSHA string) (*VerifyVerdict, error) {
	row := s.db.QueryRow(
		`SELECT repo, set_id, work_sha, verdict, findings, scope, human_authored, note, computed_at
		 FROM verify_verdicts
		 WHERE repo = ? AND set_id = ? AND work_sha = ?`,
		repo, setID, workSHA)
	return scanVerifyVerdict(row)
}

// GetLatestPassVerifyVerdict returns the most recent PASS verdict stored for
// (repo, set), regardless of work SHA. It is used by status derivation to
// immunize a terminal Task set against later commits: once a PASS verdict has
// been recorded for the set, the set does not regress to NEEDS-VERIFY when the
// work SHA moves, unless a fresh non-PASS verdict at HEAD overrides it
// (ADR-0096). Returns nil when no PASS verdict exists for the set.
func (s *Store) GetLatestPassVerifyVerdict(repo, setID string) (*VerifyVerdict, error) {
	row := s.db.QueryRow(
		`SELECT repo, set_id, work_sha, verdict, findings, scope, human_authored, note, computed_at
		 FROM verify_verdicts
		 WHERE repo = ? AND set_id = ? AND verdict = 'PASS'
		 ORDER BY computed_at DESC, work_sha DESC
		 LIMIT 1`,
		repo, setID)
	return scanVerifyVerdict(row)
}

// GetLatestAcceptedNote returns the note from the most recent human-authored
// verdict (an Accepted verdict, ADR-0103) recorded for (repo, set), or an empty
// string when none carries a note. A Verifier that re-fires later folds this
// note into its prompt as context so a human-accepted non-issue is not
// re-flagged — the note outlives the row that carried it only when the caller
// captures it before an invalidation, so this read is best-effort by design.
func (s *Store) GetLatestAcceptedNote(repo, setID string) (string, error) {
	row := s.db.QueryRow(
		`SELECT note FROM verify_verdicts
		 WHERE repo = ? AND set_id = ? AND human_authored = 1 AND note != ''
		 ORDER BY computed_at DESC, work_sha DESC
		 LIMIT 1`,
		repo, setID)
	var note string
	err := row.Scan(&note)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return note, nil
}

// scanVerifyVerdict maps a verify_verdicts row into a VerifyVerdict, returning
// (nil, nil) for the no-rows case so callers gate on absence uniformly.
func scanVerifyVerdict(row *sql.Row) (*VerifyVerdict, error) {
	var v VerifyVerdict
	var computed string
	err := row.Scan(&v.Repo, &v.SetID, &v.WorkSHA, &v.Verdict, &v.Findings, &v.Scope, &v.HumanAuthored, &v.Note, &computed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.ComputedAt = parseTime(computed)
	return &v, nil
}

// PutVerifyVerdict upserts the verdict for (repo, set, work SHA). Re-running the
// Verifier at the same SHA overwrites the row (force semantics).
func (s *Store) PutVerifyVerdict(v VerifyVerdict) error {
	_, err := s.db.Exec(
		`INSERT INTO verify_verdicts (repo, set_id, work_sha, verdict, findings, scope, human_authored, note, computed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo, set_id, work_sha) DO UPDATE SET
		   verdict=excluded.verdict, findings=excluded.findings, scope=excluded.scope,
		   human_authored=excluded.human_authored, note=excluded.note, computed_at=excluded.computed_at`,
		v.Repo, v.SetID, v.WorkSHA, v.Verdict, v.Findings, v.Scope, v.HumanAuthored, v.Note, mergeTime(v.ComputedAt))
	return err
}

// InvalidateVerifyVerdicts deletes every cached Verify verdict for (repo, set_id).
// It is called when a verification episode ends — a set leaves the terminal zone
// through reopen or remediation — so the set must re-verify from scratch
// (ADR-0096). Returns nil when no rows exist.
func (s *Store) InvalidateVerifyVerdicts(repo, setID string) error {
	_, err := s.db.Exec(
		`DELETE FROM verify_verdicts WHERE repo = ? AND set_id = ?`,
		repo, setID)
	return err
}
