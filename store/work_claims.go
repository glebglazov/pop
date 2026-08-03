package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// WorkClaimTTL is how long a claim on a Work item stays live. It is a timeout
// rather than the store's process-liveness policy because a claim's owner is
// usually a tmux pane, not a process pop can ask about: the window that took the
// claim may be sitting idle at a prompt, and the shell that ran the claiming
// command exited long ago. Four hours is longer than a grilling session and
// shorter than a working day, so an abandoned claim frees itself before it
// strands the ticket while a live one is never pulled out from under its window.
const WorkClaimTTL = 4 * time.Hour

// WorkClaim is one live hold on one item of a Work container — a Map's Decision
// ticket, and later a Task set's task. Owner is opaque here: the claiming layer
// resolves it (a tmux pane id, else a pid) and the store only compares it.
type WorkClaim struct {
	Ref       ref.WorkRef
	Owner     string
	ClaimedAt time.Time
}

// Expired reports whether the claim has aged past the TTL and so may be taken
// over by another owner.
func (c WorkClaim) Expired(now time.Time) bool {
	return !now.Before(c.ClaimedAt.Add(WorkClaimTTL))
}

// WorkClaimResult is the outcome of taking a claim: the claim now held, and the
// expired claim it displaced when there was one. Callers report the steal —
// silently taking a ticket another window still believes it owns is how two
// sessions end up grilling the same question.
type WorkClaimResult struct {
	Claim WorkClaim
	Stole *WorkClaim
}

// ErrWorkItemClaimed reports that an item is already held by a live claim
// belonging to someone else. A *WorkItemClaimedError carries that claim and
// satisfies errors.Is(err, ErrWorkItemClaimed).
var ErrWorkItemClaimed = errors.New("work item is already claimed")

// ErrNoClaimableWorkItem reports that every candidate offered to
// ClaimFirstWorkItem is held by a live claim.
var ErrNoClaimableWorkItem = errors.New("no claimable work item")

// WorkItemClaimedError names the live claim that refused a claim attempt.
type WorkItemClaimedError struct {
	Claim WorkClaim
}

func (e *WorkItemClaimedError) Error() string {
	return fmt.Sprintf("%s is claimed by %s since %s",
		e.Claim.Ref, e.Claim.Owner, e.Claim.ClaimedAt.Format(time.RFC3339))
}

func (e *WorkItemClaimedError) Is(target error) bool { return target == ErrWorkItemClaimed }

func (e *WorkItemClaimedError) Unwrap() error { return ErrWorkItemClaimed }

// ClaimWorkItem takes the claim on one named item for owner. An owner renewing
// its own claim resets the TTL rather than being refused, so a long grilling
// session can hold a ticket by re-claiming it; a live claim held by anyone else
// refuses; an expired one is taken over and reported as a steal.
func (s *Store) ClaimWorkItem(r ref.WorkRef, owner string, now time.Time) (WorkClaimResult, error) {
	if err := validItemRef(r); err != nil {
		return WorkClaimResult{}, err
	}
	if owner == "" {
		return WorkClaimResult{}, errors.New("store: a work claim needs an owner")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return WorkClaimResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	held, found, err := readWorkClaim(tx, r)
	if err != nil {
		return WorkClaimResult{}, err
	}
	var stole *WorkClaim
	if found && held.Owner != owner {
		if !held.Expired(now) {
			return WorkClaimResult{}, &WorkItemClaimedError{Claim: held}
		}
		displaced := held
		stole = &displaced
	}
	if err := writeWorkClaim(tx, r, owner, now); err != nil {
		return WorkClaimResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkClaimResult{}, err
	}
	return WorkClaimResult{
		Claim: WorkClaim{Ref: r, Owner: owner, ClaimedAt: now.UTC()},
		Stole: stole,
	}, nil
}

// ClaimFirstWorkItem claims the first of itemIDs that no live claim holds, in the
// order given, and reports ErrNoClaimableWorkItem when every candidate is held.
// The read and the write share one transaction, and the store opens transactions
// BEGIN IMMEDIATE, so two windows racing the same container serialise: the second
// sees the first's row and moves to the next candidate rather than handing out the
// same item twice.
func (s *Store) ClaimFirstWorkItem(container ref.WorkRef, itemIDs []string, owner string, now time.Time) (WorkClaimResult, error) {
	if err := validContainerRef(container); err != nil {
		return WorkClaimResult{}, err
	}
	if owner == "" {
		return WorkClaimResult{}, errors.New("store: a work claim needs an owner")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return WorkClaimResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	held, err := readContainerClaims(tx, container)
	if err != nil {
		return WorkClaimResult{}, err
	}
	for _, itemID := range itemIDs {
		if itemID == "" {
			continue
		}
		var stole *WorkClaim
		if existing, ok := held[itemID]; ok {
			if !existing.Expired(now) {
				continue
			}
			displaced := existing
			stole = &displaced
		}
		r := ref.WorkRef{Kind: container.Kind, ContainerID: container.ContainerID, ItemID: itemID}
		if err := writeWorkClaim(tx, r, owner, now); err != nil {
			return WorkClaimResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return WorkClaimResult{}, err
		}
		return WorkClaimResult{
			Claim: WorkClaim{Ref: r, Owner: owner, ClaimedAt: now.UTC()},
			Stole: stole,
		}, nil
	}
	return WorkClaimResult{}, ErrNoClaimableWorkItem
}

// ReleaseWorkItem drops the claim on one item, whoever holds it. Releasing is
// what a terminal outcome does — a resolved Decision ticket is never handed out
// again, so leaving its row behind would only age into a phantom hold. Releasing
// an unclaimed item is a no-op: the caller wants the item free, not a report of
// how it got that way.
func (s *Store) ReleaseWorkItem(r ref.WorkRef) error {
	if err := validItemRef(r); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`DELETE FROM work_item_claims WHERE kind = ? AND container_id = ? AND item_id = ?`,
		string(r.Kind), r.ContainerID, r.ItemID)
	return err
}

// LiveWorkClaimsOfKind returns every unexpired claim on one kind's items. One
// query serves a whole scan: the Map listing overlays claims onto tickets it read
// from disk, and asking per Map would put a query behind every row.
func (s *Store) LiveWorkClaimsOfKind(kind ref.Kind, now time.Time) ([]WorkClaim, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("store: unknown work kind %q", string(kind))
	}
	rows, err := s.db.Query(
		`SELECT container_id, item_id, owner, claimed_at FROM work_item_claims WHERE kind = ?`,
		string(kind))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WorkClaim
	for rows.Next() {
		var containerID, itemID, owner string
		var claimedAt sql.NullString
		if err := rows.Scan(&containerID, &itemID, &owner, &claimedAt); err != nil {
			return nil, err
		}
		claim := WorkClaim{
			Ref:       ref.WorkRef{Kind: kind, ContainerID: containerID, ItemID: itemID},
			Owner:     owner,
			ClaimedAt: parseTime(claimedAt.String),
		}
		if claim.Expired(now) {
			continue
		}
		out = append(out, claim)
	}
	return out, rows.Err()
}

// FindWorkClaim returns the claim recorded for one item, expired or not, so a
// caller can say how long a stale hold has been sitting there.
func (s *Store) FindWorkClaim(r ref.WorkRef) (WorkClaim, bool, error) {
	if err := validItemRef(r); err != nil {
		return WorkClaim{}, false, err
	}
	return readWorkClaim(s.db, r)
}

// claimRowQuerier is the subset of *sql.DB and *sql.Tx the claim reads use, so the
// same read serves a plain lookup and the check half of a check-then-write.
type claimRowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func readWorkClaim(q claimRowQuerier, r ref.WorkRef) (WorkClaim, bool, error) {
	row := q.QueryRow(
		`SELECT owner, claimed_at FROM work_item_claims
		 WHERE kind = ? AND container_id = ? AND item_id = ?`,
		string(r.Kind), r.ContainerID, r.ItemID)
	var owner string
	var claimedAt sql.NullString
	switch err := row.Scan(&owner, &claimedAt); {
	case errors.Is(err, sql.ErrNoRows):
		return WorkClaim{}, false, nil
	case err != nil:
		return WorkClaim{}, false, err
	}
	return WorkClaim{Ref: r, Owner: owner, ClaimedAt: parseTime(claimedAt.String)}, true, nil
}

// readContainerClaims reads one container's claims by item id. Rows are drained
// before the caller writes: the store runs on a single connection, so an open
// cursor would block the INSERT that follows.
func readContainerClaims(tx *sql.Tx, container ref.WorkRef) (map[string]WorkClaim, error) {
	rows, err := tx.Query(
		`SELECT item_id, owner, claimed_at FROM work_item_claims
		 WHERE kind = ? AND container_id = ?`,
		string(container.Kind), container.ContainerID)
	if err != nil {
		return nil, err
	}
	out := map[string]WorkClaim{}
	for rows.Next() {
		var itemID, owner string
		var claimedAt sql.NullString
		if err := rows.Scan(&itemID, &owner, &claimedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out[itemID] = WorkClaim{
			Ref:       ref.WorkRef{Kind: container.Kind, ContainerID: container.ContainerID, ItemID: itemID},
			Owner:     owner,
			ClaimedAt: parseTime(claimedAt.String),
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return out, rows.Close()
}

func writeWorkClaim(tx *sql.Tx, r ref.WorkRef, owner string, now time.Time) error {
	_, err := tx.Exec(
		`INSERT INTO work_item_claims (kind, container_id, item_id, owner, claimed_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(kind, container_id, item_id) DO UPDATE SET owner = excluded.owner, claimed_at = excluded.claimed_at`,
		string(r.Kind), r.ContainerID, r.ItemID, owner, now.UTC().Format(timeLayout))
	return err
}

// validItemRef guards the claim table's key: claims name items, so a container ref
// is refused rather than writing a row keyed on an empty item id.
func validItemRef(r ref.WorkRef) error {
	if !r.Kind.Valid() {
		return fmt.Errorf("store: unknown work kind %q", string(r.Kind))
	}
	if r.ContainerID == "" {
		return errors.New("store: work claim ref has no container id")
	}
	if !r.IsItem() {
		return fmt.Errorf("store: %s names a container; claims are taken on items", r)
	}
	return nil
}
