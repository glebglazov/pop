package store

import "time"

// SpentRetryCap is one agent that burned its whole retry cap on one piece of
// work without finishing it: the event a human coming back to the machine is
// paying for, whatever the drain went on to do afterwards. A refusal that steps
// aside costs nothing and records nothing here; only attempts actually spent do
// (ADR-0231).
type SpentRetryCap struct {
	ID          int64
	Repo        string
	SetID       string
	RuntimePath string
	// Phase is the Work group whose cap was spent — implement, verify or
	// review — because "the Reviewer burned three tries" and "the task burned
	// three tries" are different pieces of news.
	Phase string
	// TaskID is the task the attempts were spent on, empty for a set-level phase
	// (verify and review judge the set, not a task).
	TaskID string
	// Preset is the agent that spent the cap: the one the journal names, and the
	// one whose configuration a human looks at first.
	Preset string
	// Attempts is how many tries this agent spent, which is the cap unless the
	// cap was reached by a shorter route.
	Attempts int
	// Reason is what the last of those attempts ended on, in the provider's own
	// words wherever it left any.
	Reason string
	SpentAt time.Time
}

// RecordSpentRetryCap appends one spent-cap event. It is append-only: the row
// records something that happened rather than a state to reconcile, so nothing
// ever updates or expires it.
func (s *Store) RecordSpentRetryCap(c SpentRetryCap) error {
	_, err := s.db.Exec(
		`INSERT INTO spent_retry_caps
		   (repo, set_id, runtime_path, phase, task_id, preset, attempts, reason, spent_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Repo, c.SetID, c.RuntimePath, c.Phase, c.TaskID, c.Preset, c.Attempts, c.Reason,
		c.SpentAt.UTC().Format(timeLayout))
	return err
}

// AllSpentRetryCaps returns every spent-cap event ordered oldest-first by id,
// for the Work journal view.
func (s *Store) AllSpentRetryCaps() ([]SpentRetryCap, error) {
	rows, err := s.db.Query(
		`SELECT id, repo, set_id, runtime_path, phase, task_id, preset, attempts, reason, spent_at
		   FROM spent_retry_caps ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SpentRetryCap
	for rows.Next() {
		var c SpentRetryCap
		var spentAt string
		if err := rows.Scan(&c.ID, &c.Repo, &c.SetID, &c.RuntimePath, &c.Phase, &c.TaskID,
			&c.Preset, &c.Attempts, &c.Reason, &spentAt); err != nil {
			return nil, err
		}
		c.SpentAt = parseTime(spentAt)
		out = append(out, c)
	}
	return out, rows.Err()
}
