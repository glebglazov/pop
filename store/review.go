package store

import (
	"database/sql"
	"time"
)

// ReviewEpisode is the stored arming state of automatic Code review for one Task
// set (ADR-0214). Composition fingerprints the done-AFK work the review judged:
// a reader arms the next automatic review by comparing the set's current
// composition against it, so reviewing disarms and new done-AFK work re-arms.
// Repo is the repository's git common dir, the same identity verify_verdicts
// keys by. WorkSHA and Document say which commit the document was written
// against and where it went; neither arms anything, because a review that only
// moved the SHA judged the same work.
//
// There is no verdict here and nothing to invalidate: a review gates nothing and
// spawns no work, so the episode cannot be ended by anything but new work.
type ReviewEpisode struct {
	Repo        string
	SetID       string
	WorkSHA     string
	Composition string
	Document    string
	ReviewedAt  time.Time
}

// GetReviewEpisode returns the Review episode recorded for (repo, set), or nil
// when the set has never been reviewed — which reads as armed.
func (s *Store) GetReviewEpisode(repo, setID string) (*ReviewEpisode, error) {
	row := s.db.QueryRow(
		`SELECT repo, set_id, work_sha, composition, document, reviewed_at
		 FROM review_episodes
		 WHERE repo = ? AND set_id = ?`,
		repo, setID)
	var e ReviewEpisode
	var reviewed string
	err := row.Scan(&e.Repo, &e.SetID, &e.WorkSHA, &e.Composition, &e.Document, &reviewed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ReviewedAt = parseTime(reviewed)
	return &e, nil
}

// PutReviewEpisode records the review just written for (repo, set), replacing
// the set's previous episode. The row is the disarm: the composition it carries
// is what the next drain compares against.
func (s *Store) PutReviewEpisode(e ReviewEpisode) error {
	_, err := s.db.Exec(
		`INSERT INTO review_episodes (repo, set_id, work_sha, composition, document, reviewed_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo, set_id) DO UPDATE SET
		   work_sha=excluded.work_sha, composition=excluded.composition,
		   document=excluded.document, reviewed_at=excluded.reviewed_at`,
		e.Repo, e.SetID, e.WorkSHA, e.Composition, e.Document, mergeTime(e.ReviewedAt))
	return err
}
