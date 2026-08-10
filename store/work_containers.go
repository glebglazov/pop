package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// ErrWorkContainerUnregistered reports that a (kind, id) has no registry row, so
// there is nothing to archive, unarchive or read. Archival never registers on the
// caller's behalf: a mistyped id would otherwise leave a ghost row that renders
// as a container nothing on disk backs.
var ErrWorkContainerUnregistered = errors.New("work container is not registered")

// ErrWorkContainerNotMutable reports a Mute written against a kind that has no
// mute (a Routine). It is the durable half of ADR-0200 decision 7: the Routine
// kind not offering the verb keeps the dashboard honest, and this keeps every
// other writer honest.
var ErrWorkContainerNotMutable = errors.New("work kind cannot be muted")

// WorkContainer is one Work container's registry row: its cross-kind identity,
// the archived bit (the one registration bit that means the same thing for every
// kind), the human's Mute, and when it was registered on this machine. It
// deliberately carries no status — status is derived from files on every read,
// and a cached copy here would be a second source of truth (ADR-0056).
type WorkContainer struct {
	Ref      ref.WorkRef
	Archived bool
	// MutedUntil is the instant a Mute on this container expires, zero when none
	// was ever written. It is stored, not interpreted: whether the mute is still
	// live is the reader's comparison against its own `now` (ADR-0200 decision 1),
	// so a mute that has already ended still reads back here.
	MutedUntil time.Time
	// MuteSecret marks a mute taken with the random default window, whose instant
	// no read surface may disclose (ADR-0200 decision 6). The instant stays
	// legible in pop.db: the secret protects against the human's own glance, not
	// their database.
	MuteSecret   bool
	RegisteredAt time.Time
}

// RegisterWorkContainer records membership for one container, keyed
// (kind, id). It is idempotent: re-registering an already-known container keeps
// the original registered_at and its archived bit, so a second `register` after
// a crash is a no-op rather than a reset. r must name a container — an item ref
// is refused, since items get registry rows only when something must point at
// them, never as a side effect of registering their container.
func (s *Store) RegisterWorkContainer(r ref.WorkRef, at time.Time) error {
	if err := validContainerRef(r); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO work_containers (kind, id, registered_at) VALUES (?, ?, ?)
		 ON CONFLICT(kind, id) DO NOTHING`,
		string(r.Kind), r.ContainerID, at.UTC().Format(timeLayout))
	return err
}

// FindWorkContainer returns the registry row for one (kind, id), reporting
// whether it exists at all — an unregistered container is an ordinary answer
// here, not an error, because callers ask precisely to find out.
func (s *Store) FindWorkContainer(r ref.WorkRef) (WorkContainer, bool, error) {
	if err := validContainerRef(r); err != nil {
		return WorkContainer{}, false, err
	}
	row := s.db.QueryRow(
		`SELECT archived, muted_until, mute_secret, registered_at
		   FROM work_containers WHERE kind = ? AND id = ?`,
		string(r.Kind), r.ContainerID)
	var archived, muteSecret int
	var mutedUntil, registeredAt sql.NullString
	switch err := row.Scan(&archived, &mutedUntil, &muteSecret, &registeredAt); {
	case errors.Is(err, sql.ErrNoRows):
		return WorkContainer{}, false, nil
	case err != nil:
		return WorkContainer{}, false, err
	}
	return WorkContainer{
		Ref:          r.Container(),
		Archived:     archived != 0,
		MutedUntil:   parseTime(mutedUntil.String),
		MuteSecret:   muteSecret != 0,
		RegisteredAt: parseTime(registeredAt.String),
	}, true, nil
}

// AllWorkContainers returns every registered container across every kind in
// registration order, archived ones included. Filtering is the caller's — the
// dashboard hides archived rows, `unarchive` needs to see them.
func (s *Store) AllWorkContainers() ([]WorkContainer, error) {
	return s.queryWorkContainers(
		`SELECT kind, id, archived, muted_until, mute_secret, registered_at
		   FROM work_containers ORDER BY seq`)
}

// WorkContainersOfKind returns one kind's registered containers in registration
// order, archived ones included.
func (s *Store) WorkContainersOfKind(kind ref.Kind) ([]WorkContainer, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("store: unknown work kind %q", string(kind))
	}
	return s.queryWorkContainers(
		`SELECT kind, id, archived, muted_until, mute_secret, registered_at
		   FROM work_containers WHERE kind = ? ORDER BY seq`, string(kind))
}

// ArchiveWorkContainer hides a registered container from the active listings.
// Archival is cross-kind and lives here, so a Map archives through the same bit
// a Task set does.
func (s *Store) ArchiveWorkContainer(r ref.WorkRef) error {
	return s.setWorkContainerArchived(r, true)
}

// UnarchiveWorkContainer restores an archived container to the active listings.
func (s *Store) UnarchiveWorkContainer(r ref.WorkRef) error {
	return s.setWorkContainerArchived(r, false)
}

func (s *Store) setWorkContainerArchived(r ref.WorkRef, archived bool) error {
	if err := validContainerRef(r); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE work_containers SET archived = ? WHERE kind = ? AND id = ?`,
		boolToInt(archived), string(r.Kind), r.ContainerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", r.Container(), ErrWorkContainerUnregistered)
	}
	return nil
}

// MuteWorkContainer records a human's "not now" on a registered container until
// the given instant, secret marking the random default window. Re-muting an
// already-muted container overwrites the window rather than extending it: the
// human picked a date off a list, and the last date they picked is the answer.
//
// It refuses a kind that has no mute, which is what makes the invariant durable
// rather than a convention of the dashboard's (ADR-0200 decision 7).
func (s *Store) MuteWorkContainer(r ref.WorkRef, until time.Time, secret bool) error {
	if err := validContainerRef(r); err != nil {
		return err
	}
	if !r.Kind.Mutable() {
		return fmt.Errorf("store: %s: %w", r.Container(), ErrWorkContainerNotMutable)
	}
	return s.writeMute(r, until.UTC().Format(timeLayout), secret)
}

// UnmuteWorkContainer clears a container's mute outright. It restores nothing
// else: muting destroyed the container's Auto-drain consent (ADR-0200
// decision 2), and giving it back here would be the surface guessing at an
// instruction only the human can give.
func (s *Store) UnmuteWorkContainer(r ref.WorkRef) error {
	if err := validContainerRef(r); err != nil {
		return err
	}
	return s.writeMute(r, "", false)
}

func (s *Store) writeMute(r ref.WorkRef, until string, secret bool) error {
	res, err := s.db.Exec(
		`UPDATE work_containers SET muted_until = ?, mute_secret = ? WHERE kind = ? AND id = ?`,
		until, boolToInt(secret), string(r.Kind), r.ContainerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", r.Container(), ErrWorkContainerUnregistered)
	}
	return nil
}

func (s *Store) queryWorkContainers(query string, args ...any) ([]WorkContainer, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WorkContainer
	for rows.Next() {
		var kind, id string
		var archived, muteSecret int
		var mutedUntil, registeredAt sql.NullString
		if err := rows.Scan(&kind, &id, &archived, &mutedUntil, &muteSecret, &registeredAt); err != nil {
			return nil, err
		}
		out = append(out, WorkContainer{
			Ref:          ref.WorkRef{Kind: ref.Kind(kind), ContainerID: id},
			Archived:     archived != 0,
			MutedUntil:   parseTime(mutedUntil.String),
			MuteSecret:   muteSecret != 0,
			RegisteredAt: parseTime(registeredAt.String),
		})
	}
	return out, rows.Err()
}

// validContainerRef guards the registry's key: a well-formed kind, a non-empty
// container id, and no item segment. Every accessor runs it so a malformed ref
// fails at the call rather than silently addressing a row that can never exist.
func validContainerRef(r ref.WorkRef) error {
	if !r.Kind.Valid() {
		return fmt.Errorf("store: unknown work kind %q", string(r.Kind))
	}
	if r.ContainerID == "" {
		return errors.New("store: work container ref has no container id")
	}
	if r.IsItem() {
		return fmt.Errorf("store: %s names an item; the registry keys containers", r)
	}
	return nil
}
