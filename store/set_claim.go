package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// ErrSetClaimed reports that StartDrain refused because a live Set claim already
// exists: one drain of a (repository, task set) across *every* checkout of that
// repository. It is the sibling of ErrCheckoutClaimed — that one says "this tree
// is busy", this one says "this set is already being drained, somewhere else" —
// and the two are distinct so a refusal can tell a human which resource to go
// and look at. A *SetClaimedError carries the claiming drain and satisfies
// errors.Is(err, ErrSetClaimed).
var ErrSetClaimed = errors.New("set already being drained")

// SetClaim is the live drain that holds a (repository, task set) pair, derived at
// read time from the running Drain rows. It names the checkout the drain is
// running in — the answer to "where do I go to look?" — plus the owner's PID and
// when the drain started, so a refusal or a wait line can say how long it has
// been held. Set is a Work ref for the same reason CheckoutClaim.Holder is: the
// claim is a piece of Work, and every source of it derives from a Task set today.
type SetClaim struct {
	Set         ref.WorkRef
	RuntimePath string
	PID         int
	Since       time.Time
}

// Sentence renders the Set claim as the refusal (and, later, wait) line: which
// set is held, in which checkout, and by whom. It is the single wording for the
// set-keyed refusal so BeginDrain and every other admission chokepoint say the
// same thing.
func (c SetClaim) Sentence() string {
	s := fmt.Sprintf("set %s is already being drained in %s (PID %d", c.Set.ContainerID, c.RuntimePath, c.PID)
	if !c.Since.IsZero() {
		s += " since " + c.Since.UTC().Format(time.RFC3339)
	}
	return s + ")"
}

// SetClaimedError carries the Set claim that caused a StartDrain refusal so the
// caller can name the set and the checkout draining it. It unwraps to
// ErrSetClaimed so errors.Is(err, ErrSetClaimed) holds.
type SetClaimedError struct {
	Claim SetClaim
}

func (e *SetClaimedError) Error() string { return e.Claim.Sentence() }

func (e *SetClaimedError) Is(target error) bool { return target == ErrSetClaimed }

func (e *SetClaimedError) Unwrap() error { return ErrSetClaimed }

// ReadSetClaim derives the live Set claim on (repo, setID), or nil when the set
// is being drained nowhere. It applies the store's liveness policy, so a
// dead-owner running row (a crash the reconcile heals) never claims.
func (s *Store) ReadSetClaim(repo, setID string) (*SetClaim, error) {
	return s.liveSetClaim(s.db, repo, setID)
}

// liveSetClaim returns the first live running Drain on (repo, setID) as a Set
// claim, or nil when none is live. Rows are fully read and the cursor closed
// before liveness is evaluated so the store's single connection is free for the
// next statement — the same discipline the Checkout claim derivations follow,
// which is what lets this run inside StartDrain's open transaction.
func (s *Store) liveSetClaim(q claimQuerier, repo, setID string) (*SetClaim, error) {
	rows, err := q.Query(
		`SELECT set_id, runtime_path, pid, proc_start, started_at FROM drains
		 WHERE state = ? AND repo = ? AND set_id = ?`,
		StateRunning, repo, setID)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		setID       string
		runtimePath string
		pid         int
		procStart   string
		since       time.Time
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var procStart sql.NullString
		var started string
		if err := rows.Scan(&c.setID, &c.runtimePath, &c.pid, &procStart, &started); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.procStart = procStart.String
		c.since = parseTime(started)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, c := range cands {
		if s.alive(c.pid, c.procStart) {
			return &SetClaim{Set: taskSetHolder(c.setID), RuntimePath: c.runtimePath, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}
