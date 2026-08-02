package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

func mapTicket(container, item string) ref.WorkRef {
	return ref.WorkRef{Kind: ref.KindMap, ContainerID: container, ItemID: item}
}

// TestWorkClaimHoldsRenewsAndExpires walks one ticket's whole claim life: taken,
// refused to a second window, renewed by its owner, then stealable once the TTL
// has run out.
func TestWorkClaimHoldsRenewsAndExpires(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	ticket := mapTicket("2026-08-03-demo", "01")

	first, err := s.ClaimWorkItem(ticket, "pane:%3", at)
	if err != nil {
		t.Fatalf("ClaimWorkItem: %v", err)
	}
	if first.Stole != nil || !first.Claim.ClaimedAt.Equal(at) || first.Claim.Owner != "pane:%3" {
		t.Fatalf("first claim = %+v", first)
	}

	_, err = s.ClaimWorkItem(ticket, "pid:4242", at.Add(time.Minute))
	if !errors.Is(err, ErrWorkItemClaimed) {
		t.Fatalf("second window's claim error = %v, want ErrWorkItemClaimed", err)
	}
	var claimed *WorkItemClaimedError
	if !errors.As(err, &claimed) || claimed.Claim.Owner != "pane:%3" {
		t.Fatalf("refusal does not name the holder: %v", err)
	}

	renewed, err := s.ClaimWorkItem(ticket, "pane:%3", at.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("owner renewing its own claim: %v", err)
	}
	if renewed.Stole != nil || !renewed.Claim.ClaimedAt.Equal(at.Add(3*time.Hour)) {
		t.Fatalf("renewal = %+v, want the TTL reset with no steal", renewed)
	}

	// Still held four hours after the *renewal*, not after the original claim.
	if _, err := s.ClaimWorkItem(ticket, "pid:4242", at.Add(6*time.Hour)); !errors.Is(err, ErrWorkItemClaimed) {
		t.Fatalf("renewed claim expired early: %v", err)
	}

	stealAt := at.Add(3*time.Hour + WorkClaimTTL)
	stolen, err := s.ClaimWorkItem(ticket, "pid:4242", stealAt)
	if err != nil {
		t.Fatalf("stealing an expired claim: %v", err)
	}
	if stolen.Stole == nil || stolen.Stole.Owner != "pane:%3" {
		t.Fatalf("steal did not report the displaced claim: %+v", stolen)
	}
	if stolen.Claim.Owner != "pid:4242" {
		t.Fatalf("claim after steal = %+v", stolen.Claim)
	}
}

// TestClaimFirstWorkItemWalksCandidateOrder pins the pick rule `pop map next`
// leans on: first candidate not held by a live claim, expired holds taken over
// and reported, and a clear refusal when everything is held.
func TestClaimFirstWorkItemWalksCandidateOrder(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	demo := ref.WorkRef{Kind: ref.KindMap, ContainerID: "2026-08-03-demo"}
	candidates := []string{"01", "02", "03"}

	first, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%1", at)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.Claim.Ref.ItemID != "01" {
		t.Fatalf("first next took %q, want 01", first.Claim.Ref.ItemID)
	}

	second, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%2", at.Add(time.Second))
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Claim.Ref.ItemID != "02" {
		t.Fatalf("second next took %q, want 02", second.Claim.Ref.ItemID)
	}

	third, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%3", at.Add(2*time.Second))
	if err != nil || third.Claim.Ref.ItemID != "03" {
		t.Fatalf("third next = %+v (%v), want 03", third.Claim, err)
	}

	if _, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%4", at.Add(3*time.Second)); !errors.Is(err, ErrNoClaimableWorkItem) {
		t.Fatalf("fourth next error = %v, want ErrNoClaimableWorkItem", err)
	}

	// Once 01's claim ages out it is the first candidate again, and the steal
	// names the window that abandoned it.
	stolen, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%4", at.Add(WorkClaimTTL))
	if err != nil {
		t.Fatalf("steal through next: %v", err)
	}
	if stolen.Claim.Ref.ItemID != "01" || stolen.Stole == nil || stolen.Stole.Owner != "pane:%1" {
		t.Fatalf("steal through next = %+v, stole %+v", stolen.Claim, stolen.Stole)
	}
}

// TestConcurrentClaimFirstWorkItemNeverHandsOutTheSameItem is the parallel
// grilling guarantee: eight windows racing one Map's frontier get eight distinct
// tickets. Each window opens its own handle, so they contend through SQLite the
// way separate processes do.
func TestConcurrentClaimFirstWorkItemNeverHandsOutTheSameItem(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	demo := ref.WorkRef{Kind: ref.KindMap, ContainerID: "2026-08-03-demo"}
	candidates := []string{"01", "02", "03", "04", "05", "06", "07", "08"}
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	const windows = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	taken := map[string]string{}
	start := make(chan struct{})
	errs := make([]error, windows)
	for i := 0; i < windows; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(path, allAlive(true))
			if err != nil {
				errs[i] = err
				return
			}
			defer func() { _ = s.Close() }()
			owner := "pane:%" + string(rune('a'+i))
			<-start
			res, err := s.ClaimFirstWorkItem(demo, candidates, owner, at)
			if err != nil {
				errs[i] = err
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if prev, ok := taken[res.Claim.Ref.ItemID]; ok {
				errs[i] = errors.New("item " + res.Claim.Ref.ItemID + " handed to both " + prev + " and " + owner)
				return
			}
			taken[res.Claim.Ref.ItemID] = owner
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(taken) != windows {
		t.Fatalf("%d windows took %d distinct items: %v", windows, len(taken), taken)
	}
}

func TestLiveWorkClaimsOfKindDropsExpiredAndOtherKinds(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	for _, r := range []ref.WorkRef{
		mapTicket("demo", "01"),
		mapTicket("other", "02"),
		{Kind: ref.KindTaskSet, ContainerID: "2026-08-02-foo", ItemID: "01"},
	} {
		if _, err := s.ClaimWorkItem(r, "pane:%1", at); err != nil {
			t.Fatalf("ClaimWorkItem(%s): %v", r, err)
		}
	}
	if _, err := s.ClaimWorkItem(mapTicket("demo", "09"), "pane:%2", at.Add(-WorkClaimTTL)); err != nil {
		t.Fatal(err)
	}

	live, err := s.LiveWorkClaimsOfKind(ref.KindMap, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("LiveWorkClaimsOfKind: %v", err)
	}
	got := map[string]bool{}
	for _, claim := range live {
		got[claim.Ref.String()] = true
	}
	if len(got) != 2 || !got["map:demo/01"] || !got["map:other/02"] {
		t.Fatalf("live map claims = %v", got)
	}

	claim, found, err := s.FindWorkClaim(mapTicket("demo", "09"))
	if err != nil || !found || !claim.Expired(at) {
		t.Fatalf("FindWorkClaim(expired) = %+v, %v, %v", claim, found, err)
	}
}

// TestWorkClaimRefusesContainerRefs keeps the table's key honest: a claim names
// an item, never the whole container.
func TestWorkClaimRefusesContainerRefs(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Now().UTC()
	container := ref.WorkRef{Kind: ref.KindMap, ContainerID: "demo"}
	if _, err := s.ClaimWorkItem(container, "pane:%1", at); err == nil {
		t.Fatal("claiming a container ref was accepted")
	}
	if _, err := s.ClaimWorkItem(mapTicket("demo", "01"), "", at); err == nil {
		t.Fatal("an ownerless claim was accepted")
	}
	if _, err := s.ClaimFirstWorkItem(mapTicket("demo", "01"), []string{"01"}, "pane:%1", at); err == nil {
		t.Fatal("ClaimFirstWorkItem accepted an item ref as its container")
	}
}
