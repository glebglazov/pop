package store

import "time"

const (
	ErrandQueued  = "queued"
	ErrandRunning = "running"
	ErrandFailed  = "failed"
)

// CheckoutRemoval identifies the checkout and, for managed teardown, its branch.
// It holds no commands or precomputed filesystem walk.
type CheckoutRemoval struct {
	Path        string
	WorkingPath string
	Branch      string
	Force       bool
}

type Errand struct {
	CheckoutRemoval
	State      string
	QueuedAt   time.Time
	OutputPath string
}

// QueueCheckoutRemoval replaces a failed request. Repeated requests while the
// subject is queued or running cannot schedule a second destructive attempt.
func (s *Store) QueueCheckoutRemoval(subject CheckoutRemoval, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO errands(path, working_path, branch, force, state, queued_at)
		VALUES (?, ?, ?, ?, 'queued', ?)
		ON CONFLICT(path) DO UPDATE SET working_path=excluded.working_path,
		branch=excluded.branch, force=excluded.force, state='queued',
		queued_at=excluded.queued_at, output_path=''
		WHERE errands.state='failed'`, subject.Path, subject.WorkingPath, subject.Branch,
		boolToInt(subject.Force), at.UTC().Format(timeLayout))
	return err
}

func (s *Store) ListErrands() ([]Errand, error) {
	rows, err := s.db.Query(`SELECT path, working_path, branch, force, state, queued_at, output_path
		FROM errands ORDER BY queued_at, path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Errand
	for rows.Next() {
		var e Errand
		var at string
		if err := rows.Scan(&e.Path, &e.WorkingPath, &e.Branch, &e.Force, &e.State, &at, &e.OutputPath); err != nil {
			return nil, err
		}
		e.QueuedAt = parseTime(at)
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *Store) StartErrand(path, outputPath string) (bool, error) {
	result, err := s.db.Exec(`UPDATE errands SET state='running', output_path=? WHERE path=? AND state='queued'`, outputPath, path)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) FailErrand(path string) error {
	_, err := s.db.Exec(`UPDATE errands SET state='failed' WHERE path=? AND state='running'`, path)
	return err
}

// RetryErrand returns one failed subject to the queue. A stale dashboard row
// cannot disturb an Errand that has already moved on.
func (s *Store) RetryErrand(path string, at time.Time) (bool, error) {
	result, err := s.db.Exec(`UPDATE errands SET state='queued', queued_at=?, output_path=''
		WHERE path=? AND state='failed'`, at.UTC().Format(timeLayout), path)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// DismissErrand removes one failed subject without running it. A stale
// dashboard row cannot dismiss a queued or running Errand.
func (s *Store) DismissErrand(path string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM errands WHERE path=? AND state='failed'`, path)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// FinishErrand clears the request and writes its journal event atomically.
func (s *Store) FinishErrand(path string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO errand_completions(path, finished_at)
		SELECT path, ? FROM errands WHERE path=? AND state='running'`, at.UTC().Format(timeLayout), path); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM errands WHERE path=? AND state='running'`, path); err != nil {
		return err
	}
	return tx.Commit()
}

type ErrandCompletion struct {
	Path       string
	FinishedAt time.Time
}

func (s *Store) ListErrandCompletions() ([]ErrandCompletion, error) {
	rows, err := s.db.Query(`SELECT path, finished_at FROM errand_completions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ErrandCompletion
	for rows.Next() {
		var e ErrandCompletion
		var at string
		if err := rows.Scan(&e.Path, &at); err != nil {
			return nil, err
		}
		e.FinishedAt = parseTime(at)
		result = append(result, e)
	}
	return result, rows.Err()
}
