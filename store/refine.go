package store

import (
	"database/sql"
	"time"
)

// RefineEpisode is the stored arming state of automatic Refine for one Task
// set (ADR-0214). Composition fingerprints the done-AFK work the refine pass judged:
// a reader arms the next automatic refine pass by comparing the set's current
// composition against it, so refining disarms and new done-AFK work re-arms.
// Repo is the repository's git common dir, the same identity verify_verdicts
// keys by. WorkSHA and Document say which commit the document was written
// against and where it went; neither arms anything, because a refine pass that only
// moved the SHA judged the same work.
//
// There is no verdict here and nothing to invalidate: Refine gates nothing and
// spawns no work, so the episode cannot be ended by anything but new work.
type RefineEpisode struct {
	Repo        string
	SetID       string
	WorkSHA     string
	Composition string
	Document    string
	RefinedAt   time.Time
}

// GetRefineEpisode returns the Refine episode recorded for (repo, set), or nil
// when the set has never been refined — which reads as armed.
func (s *Store) GetRefineEpisode(repo, setID string) (*RefineEpisode, error) {
	row := s.db.QueryRow(
		`SELECT repo, set_id, work_sha, composition, document, refined_at
		 FROM refine_episodes
		 WHERE repo = ? AND set_id = ?`,
		repo, setID)
	var e RefineEpisode
	var refined string
	err := row.Scan(&e.Repo, &e.SetID, &e.WorkSHA, &e.Composition, &e.Document, &refined)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.RefinedAt = parseTime(refined)
	return &e, nil
}

// PutRefineEpisode records the refine pass just written for (repo, set), replacing
// the set's previous episode. The row is the disarm: the composition it carries
// is what the next drain compares against.
func (s *Store) PutRefineEpisode(e RefineEpisode) error {
	_, err := s.db.Exec(
		`INSERT INTO refine_episodes (repo, set_id, work_sha, composition, document, refined_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo, set_id) DO UPDATE SET
		   work_sha=excluded.work_sha, composition=excluded.composition,
		   document=excluded.document, refined_at=excluded.refined_at`,
		e.Repo, e.SetID, e.WorkSHA, e.Composition, e.Document, mergeTime(e.RefinedAt))
	return err
}
