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

// grilling names the owners whose grilling process is still running. Liveness is
// the store's only claim-ending rule beside resolution, so every claim test says
// which sessions are alive rather than which clock it is.
func grilling(owners ...string) OwnerLive {
	live := map[string]bool{}
	for _, owner := range owners {
		live[owner] = true
	}
	return func(owner string) bool { return live[owner] }
}

func allOwnersLive(string) bool { return true }

// TestWorkClaimHoldsUntilItsOwnerDies walks one ticket's whole claim life: taken,
// refused to a second window while the first is grilling, re-claimed by its own
// owner, and reclaimed by anyone once that owner's session is gone. No clock is
// advanced anywhere — a claim ends when its process does, not on a timer.
func TestWorkClaimHoldsUntilItsOwnerDies(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	ticket := mapTicket("2026-08-03-demo", "01")
	alive := grilling("pane:%3")

	first, err := s.ClaimWorkItem(ticket, "pane:%3", at, alive)
	if err != nil {
		t.Fatalf("ClaimWorkItem: %v", err)
	}
	if first.Reclaimed != nil || !first.Claim.ClaimedAt.Equal(at) || first.Claim.Owner != "pane:%3" {
		t.Fatalf("first claim = %+v", first)
	}

	_, err = s.ClaimWorkItem(ticket, "pid:4242", at.Add(time.Minute), alive)
	if !errors.Is(err, ErrWorkItemClaimed) {
		t.Fatalf("second window's claim error = %v, want ErrWorkItemClaimed", err)
	}
	var claimed *WorkItemClaimedError
	if !errors.As(err, &claimed) || claimed.Claim.Owner != "pane:%3" {
		t.Fatalf("refusal does not name the holder: %v", err)
	}

	renewed, err := s.ClaimWorkItem(ticket, "pane:%3", at.Add(3*time.Hour), alive)
	if err != nil {
		t.Fatalf("owner re-claiming its own ticket: %v", err)
	}
	if renewed.Reclaimed != nil || !renewed.Claim.ClaimedAt.Equal(at.Add(3*time.Hour)) {
		t.Fatalf("re-claim = %+v, want the same owner and nothing reclaimed", renewed)
	}

	// The pane's agent dies. The very next claim attempt — same minute — takes
	// the ticket over and names who left it behind.
	reclaimed, err := s.ClaimWorkItem(ticket, "pid:4242", at.Add(3*time.Hour), grilling("pid:4242"))
	if err != nil {
		t.Fatalf("reclaiming a dead owner's ticket: %v", err)
	}
	if reclaimed.Reclaimed == nil || reclaimed.Reclaimed.Owner != "pane:%3" {
		t.Fatalf("reclaim did not report the dead owner: %+v", reclaimed)
	}
	if reclaimed.Claim.Owner != "pid:4242" {
		t.Fatalf("claim after reclaim = %+v", reclaimed.Claim)
	}
}

// TestClaimFirstWorkItemWalksCandidateOrder pins the pick rule `pop map next`
// leans on: first candidate whose owner is not still grilling, dead owners taken
// over and reported, and a clear refusal when every candidate is held.
func TestClaimFirstWorkItemWalksCandidateOrder(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	demo := ref.WorkRef{Kind: ref.KindMap, ContainerID: "2026-08-03-demo"}
	candidates := []string{"01", "02", "03"}
	alive := grilling("pane:%1", "pane:%2", "pane:%3", "pane:%4")

	first, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%1", at, alive)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.Claim.Ref.ItemID != "01" {
		t.Fatalf("first next took %q, want 01", first.Claim.Ref.ItemID)
	}

	second, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%2", at.Add(time.Second), alive)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Claim.Ref.ItemID != "02" {
		t.Fatalf("second next took %q, want 02", second.Claim.Ref.ItemID)
	}

	third, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%3", at.Add(2*time.Second), alive)
	if err != nil || third.Claim.Ref.ItemID != "03" {
		t.Fatalf("third next = %+v (%v), want 03", third.Claim, err)
	}

	if _, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%4", at.Add(3*time.Second), alive); !errors.Is(err, ErrNoClaimableWorkItem) {
		t.Fatalf("fourth next error = %v, want ErrNoClaimableWorkItem", err)
	}

	// 01's pane dies, so it leads the candidate order again and the reclaim names
	// the session that abandoned it.
	reclaimed, err := s.ClaimFirstWorkItem(demo, candidates, "pane:%4", at.Add(4*time.Second),
		grilling("pane:%2", "pane:%3", "pane:%4"))
	if err != nil {
		t.Fatalf("reclaim through next: %v", err)
	}
	if reclaimed.Claim.Ref.ItemID != "01" || reclaimed.Reclaimed == nil || reclaimed.Reclaimed.Owner != "pane:%1" {
		t.Fatalf("reclaim through next = %+v, reclaimed %+v", reclaimed.Claim, reclaimed.Reclaimed)
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
			res, err := s.ClaimFirstWorkItem(demo, candidates, owner, at, allOwnersLive)
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

// TestLiveWorkClaimsOfKindDropsDeadOwnersAndOtherKinds: the read that overlays
// claims onto a Map listing returns only the rows whose session is still there.
// A dead owner's row stays on disk and simply stops being live, which is how the
// ticket reappears on the frontier with nothing swept.
func TestLiveWorkClaimsOfKindDropsDeadOwnersAndOtherKinds(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	for _, r := range []ref.WorkRef{
		mapTicket("demo", "01"),
		mapTicket("other", "02"),
		{Kind: ref.KindTaskSet, ContainerID: "2026-08-02-foo", ItemID: "01"},
	} {
		if _, err := s.ClaimWorkItem(r, "pane:%1", at, allOwnersLive); err != nil {
			t.Fatalf("ClaimWorkItem(%s): %v", r, err)
		}
	}
	if _, err := s.ClaimWorkItem(mapTicket("demo", "09"), "pane:%2", at, allOwnersLive); err != nil {
		t.Fatal(err)
	}

	live, err := s.LiveWorkClaimsOfKind(ref.KindMap, grilling("pane:%1"))
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
	if err != nil || !found || claim.Owner != "pane:%2" {
		t.Fatalf("FindWorkClaim(dead owner) = %+v, %v, %v", claim, found, err)
	}
}

// TestWorkClaimRefusesContainerRefs keeps the table's key honest: a claim names
// an item, never the whole container. A missing liveness policy is refused the
// same way, rather than quietly holding every dead owner's claim forever.
func TestWorkClaimRefusesContainerRefs(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Now().UTC()
	container := ref.WorkRef{Kind: ref.KindMap, ContainerID: "demo"}
	if _, err := s.ClaimWorkItem(container, "pane:%1", at, allOwnersLive); err == nil {
		t.Fatal("claiming a container ref was accepted")
	}
	if _, err := s.ClaimWorkItem(mapTicket("demo", "01"), "", at, allOwnersLive); err == nil {
		t.Fatal("an ownerless claim was accepted")
	}
	if _, err := s.ClaimFirstWorkItem(mapTicket("demo", "01"), []string{"01"}, "pane:%1", at, allOwnersLive); err == nil {
		t.Fatal("ClaimFirstWorkItem accepted an item ref as its container")
	}
	if _, err := s.ClaimWorkItem(mapTicket("demo", "01"), "pane:%1", at, nil); err == nil {
		t.Fatal("a claim with no liveness policy was accepted")
	}
	if _, err := s.LiveWorkClaimsOfKind(ref.KindMap, nil); err == nil {
		t.Fatal("a live-claim read with no liveness policy was accepted")
	}
}
