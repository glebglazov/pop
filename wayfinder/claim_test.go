package wayfinder

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

const claimMapID = "2026-08-03-parallel"

// claimFixture is a registered Map whose frontier is 01 and 03: 02 waits on 01.
func claimFixture(t *testing.T) (*Deps, string) {
	t.Helper()
	files := map[string]string{
		"maps/" + claimMapID + "/map.md":              "Status: active\n\n## Destination\nShip it\n",
		"maps/" + claimMapID + "/issues/01-first.md":  "## Question\nFirst?\n",
		"maps/" + claimMapID + "/issues/02-second.md": "## Question\nSecond?\n",
		"maps/" + claimMapID + "/issues/03-third.md":  "## Question\nThird?\n",
		"maps/" + claimMapID + "/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]},` +
			`{"id":"02","file":"02-second.md","type":"grilling","status":"open","blocked_by":["01"]},` +
			`{"id":"03","file":"03-third.md","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	}
	d, storageDir := registryFixture(t, files)
	if _, err := RegisterMap(d, "", claimMapID); err != nil {
		t.Fatalf("RegisterMap: %v", err)
	}
	return d, storageDir
}

// ownerOfPane is the owner string a claim taken for one of the fake's panes
// carries: the pane and the process tmux reports in it.
func ownerOfPane(fake *tmuxtest.Fake, paneID string) string {
	return paneOwner(paneID, fake.PanePIDs[paneID])
}

func at(hour int) time.Time {
	return time.Date(2026, 8, 3, hour, 0, 0, 0, time.UTC)
}

// asWindow is the test speaking as one grilling window: the owner its claims
// carry, the clock it claims at, and — since a claim lives exactly as long as
// the process in its owner — a pane with an agent running in it for that owner
// to be alive in. A test that wants the window gone says so afterwards.
func asWindow(d *Deps, owner string, now time.Time) {
	fake := atTime(d, now)
	d.Owner = func() string { return owner }
	if paneID, _, ok := parsePaneOwner(owner); ok {
		grillingPane(fake, paneID)
	}
}

// atTime gives a claim fixture a clock and a tmux to spawn panes into, which is
// what the spawn path needs beyond the store: a claim's owner is no longer
// injected, it is whichever pane the agent landed in.
func atTime(d *Deps, now time.Time) *tmuxtest.Fake {
	fake, ok := d.Tmux.(*tmuxtest.Fake)
	if !ok {
		fake = &tmuxtest.Fake{}
		d.Tmux = fake
	}
	d.Clock = func() time.Time { return now }
	return fake
}

// nextSpawn is `pop map next`: the whole spawn path, pane before claim.
func nextSpawn(t *testing.T, d *Deps) (*SpawnedTicket, error) {
	t.Helper()
	out, err := NextFrontierTicket(d, nil, "", claimMapID)
	if err != nil {
		return nil, err
	}
	if len(out.Spawned) != 1 {
		t.Fatalf("next spawned %d panes, want exactly one", len(out.Spawned))
	}
	return &out.Spawned[0], nil
}

// TestNextHandsOutTheFrontierInOrderThenRefuses is the parallel-grilling loop as
// two panes live it: each `next` returns a different frontier ticket in manifest
// order, the blocked one is never offered, the claim lands on the pane that was
// spawned for it, and the empty frontier is an error rather than a silent success.
func TestNextHandsOutTheFrontierInOrderThenRefuses(t *testing.T) {
	t.Parallel()
	d, storageDir := claimFixture(t)
	atTime(d, at(9))

	first, err := nextSpawn(t, d)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	wantPath := filepath.Join(storageDir, "maps", claimMapID, "issues", "01-first.md")
	if first.Ticket.ID != "01" || first.Claim.Path != wantPath {
		t.Fatalf("first next = %+v, want ticket 01 at %s", first.Claim, wantPath)
	}
	if first.Claim.Owner != ownerOfPane(d.Tmux.(*tmuxtest.Fake), first.Pane.PaneID) {
		t.Fatalf("claim owner = %q, want the spawned pane %q", first.Claim.Owner, first.Pane.PaneID)
	}
	if first.Claim.Reclaimed != nil {
		t.Fatalf("a free ticket reported a reclaim: %+v", first.Claim.Reclaimed)
	}

	atTime(d, at(9).Add(time.Minute))
	second, err := nextSpawn(t, d)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Ticket.ID != "03" {
		t.Fatalf("second next took %q, want 03 — 02 is blocked by an unresolved 01", second.Ticket.ID)
	}
	if second.Pane.PaneID == first.Pane.PaneID || second.Claim.Owner == first.Claim.Owner {
		t.Fatalf("second ticket shares the first's pane: %+v", second.Pane)
	}

	atTime(d, at(9).Add(2*time.Minute))
	_, err = nextSpawn(t, d)
	if !errors.Is(err, ErrFrontierEmpty) {
		t.Fatalf("third next error = %v, want ErrFrontierEmpty", err)
	}
	if !strings.Contains(err.Error(), claimMapID) {
		t.Fatalf("empty-frontier message does not name the map: %v", err)
	}
}

// TestClaimsLiveOnlyInTheStore is the format guarantee: after two claims, neither
// the manifest nor any ticket markdown has changed, and the claim is visible only
// because the scan overlays pop.db.
func TestClaimsLiveOnlyInTheStore(t *testing.T) {
	t.Parallel()
	d, storageDir := claimFixture(t)
	mapDir := filepath.Join(storageDir, "maps", claimMapID)
	before := snapshotDir(t, mapDir)

	asWindow(d, "pane:%1", at(9))
	atTime(d, at(9))
	spawned, err := nextSpawn(t, d)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := ClaimTicket(d, "", claimMapID, "3"); err != nil {
		t.Fatalf("claim 3: %v", err)
	}

	for path, content := range snapshotDir(t, mapDir) {
		if before[path] != content {
			t.Fatalf("%s changed when a ticket was claimed:\n%s", path, content)
		}
		if strings.Contains(strings.ToLower(content), "claim") {
			t.Fatalf("%s records a claim:\n%s", path, content)
		}
	}

	m, err := FindMap(d, "", claimMapID)
	if err != nil {
		t.Fatal(err)
	}
	claimed := map[string]string{}
	for _, ticket := range m.Tickets {
		if ticket.Status == TicketClaimed {
			claimed[ticket.ID] = ticket.ClaimOwner
		}
	}
	if len(claimed) != 2 || claimed["01"] != ownerOfPane(d.Tmux.(*tmuxtest.Fake), spawned.Pane.PaneID) || claimed["03"] != "pane:%1" {
		t.Fatalf("scanned claims = %v, want 01 held by the spawned pane and 03 by pane:%%1", claimed)
	}
	if counts := CountTickets(m.Tickets); counts.Claimed != 2 || counts.Open != 1 {
		t.Fatalf("counts = %+v, want 2 claimed and 1 open", counts)
	}
}

// TestNextRespawnsIntoADeadSessionsIdlePane is the whole recovery story: the
// human closes a grilling session, and the ordinary spawn verb hands the ticket
// back and respawns into the pane it left behind. No release verb, no timer, and
// the clock never moves.
func TestNextRespawnsIntoADeadSessionsIdlePane(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))

	dead, err := nextSpawn(t, d)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}

	// Still held while the agent is running: the next pane gets 03 instead.
	fresh, err := nextSpawn(t, d)
	if err != nil || fresh.Ticket.ID != "03" {
		t.Fatalf("next over a live claim = %+v (%v), want 03", fresh, err)
	}

	dropToShell(fake, dead.Pane.PaneID)
	reclaimed, err := nextSpawn(t, d)
	if err != nil {
		t.Fatalf("next after the session died: %v", err)
	}
	if reclaimed.Ticket.ID != "01" {
		t.Fatalf("next after the session died took %q, want the abandoned 01", reclaimed.Ticket.ID)
	}
	if reclaimed.Pane.PaneID != dead.Pane.PaneID {
		t.Fatalf("reclaim spawned pane %q, want the dead session's idle pane %q",
			reclaimed.Pane.PaneID, dead.Pane.PaneID)
	}
	// Nothing was taken from anybody: the respawned pane is the same pane, so it
	// re-takes its own row rather than displacing a competitor.
	if reclaimed.Claim.Reclaimed != nil {
		t.Fatalf("respawning into its own pane reported a reclaim: %+v", reclaimed.Claim.Reclaimed)
	}
}

func TestClaimNamesOneTicketAndRefusesALiveHolder(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)

	asWindow(d, "pane:%1", at(9))
	claimed, err := ClaimTicket(d, "", claimMapID, "01")
	if err != nil {
		t.Fatalf("claim 01: %v", err)
	}
	if claimed.Ticket.ID != "01" || len(claimed.UnresolvedBlockers) != 0 {
		t.Fatalf("claim 01 = %+v", claimed)
	}

	asWindow(d, "pane:%2", at(10))
	_, err = ClaimTicket(d, "", claimMapID, "01")
	if err == nil || !strings.Contains(err.Error(), "pane:%1") {
		t.Fatalf("claiming a held ticket = %v, want a refusal naming the holder", err)
	}

	// The override reaches past manifest order, blockers and all — but says so.
	blocked, err := ClaimTicket(d, "", claimMapID, "02")
	if err != nil {
		t.Fatalf("claim 02: %v", err)
	}
	if len(blocked.UnresolvedBlockers) != 1 || blocked.UnresolvedBlockers[0] != "01" {
		t.Fatalf("claim 02 blockers = %v, want [01]", blocked.UnresolvedBlockers)
	}

	// Its own holder may renew, and an unknown ticket is named as such.
	asWindow(d, "pane:%1", at(11))
	if _, err := ClaimTicket(d, "", claimMapID, "01"); err != nil {
		t.Fatalf("renewing an own claim: %v", err)
	}
	if _, err := ClaimTicket(d, "", claimMapID, "77"); err == nil || !strings.Contains(err.Error(), "no ticket") {
		t.Fatalf("claiming an unknown ticket = %v", err)
	}
}

func TestClaimRefusesResolvedAndUnregisteredMaps(t *testing.T) {
	t.Parallel()
	d, storageDir := claimFixture(t)
	asWindow(d, "pane:%1", at(9))

	manifest := filepath.Join(storageDir, "maps", claimMapID, MapManifestFileName)
	resolved := `{"tickets":[` +
		`{"id":"01","file":"01-first.md","type":"grilling","status":"resolved","blocked_by":[]},` +
		`{"id":"02","file":"02-second.md","type":"grilling","status":"open","blocked_by":["01"]},` +
		`{"id":"03","file":"03-third.md","type":"grilling","status":"open","blocked_by":[]}` +
		`],"spawned_sets":[]}`
	if err := os.WriteFile(manifest, []byte(resolved), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimTicket(d, "", claimMapID, "01"); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("claiming a resolved ticket = %v", err)
	}
	// With 01 resolved, 02 is unblocked and leads the frontier.
	atTime(d, at(9))
	next, err := nextSpawn(t, d)
	if err != nil || next.Ticket.ID != "02" {
		t.Fatalf("next after resolving 01 = %+v (%v), want 02", next, err)
	}

	unregistered := oneTicketMap("2026-08-03-charting")
	for rel, content := range unregistered {
		path := filepath.Join(storageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err = NextFrontierTicket(d, nil, "", "2026-08-03-charting")
	if err == nil || !strings.Contains(err.Error(), "pop map register 2026-08-03-charting") {
		t.Fatalf("next on an unregistered map = %v", err)
	}
}

// TestNextWithoutAMapIDTakesTheOneBeingWayfound: a repository grilling a single
// Map should not have to name it, and one grilling two must.
func TestNextWithoutAMapIDTakesTheOneBeingWayfound(t *testing.T) {
	t.Parallel()
	d, storageDir := claimFixture(t)
	asWindow(d, "pane:%1", at(9))

	atTime(d, at(9))
	sole, err := NextFrontierTicket(d, nil, "", "")
	if err != nil || len(sole.Spawned) != 1 || sole.Spawned[0].Ticket.ID != "01" {
		t.Fatalf("bare next = %+v (%v), want ticket 01 of the sole active map", sole, err)
	}

	for rel, content := range oneTicketMap("2026-08-03-other") {
		path := filepath.Join(storageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RegisterMap(d, "", "2026-08-03-other"); err != nil {
		t.Fatal(err)
	}
	_, err = NextFrontierTicket(d, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "several active maps") {
		t.Fatalf("bare next with two active maps = %v", err)
	}
}

// TestDefaultClaimOwnerPrefersThePane pins the identity rule: the pane the
// command runs in and the process running it when there is one, the pid
// otherwise. No configuration either way.
func TestDefaultClaimOwnerPrefersThePane(t *testing.T) {
	fake := &tmuxtest.Fake{
		PaneCommandMap: map[string]string{"%17": "claude"},
		PanePIDs:       map[string]int{"%17": 4242},
	}
	t.Setenv("TMUX_PANE", "%17")
	if got := DefaultClaimOwner(fake); got != "pane:%17/4242" {
		t.Fatalf("DefaultClaimOwner() = %q inside tmux, want pane:%%17/4242", got)
	}
	t.Setenv("TMUX_PANE", "")
	want := "pid:" + strconv.Itoa(os.Getpid())
	if got := DefaultClaimOwner(fake); got != want {
		t.Fatalf("DefaultClaimOwner() = %q outside tmux, want %q", got, want)
	}
}

// TestPaneOwnerWithoutAPIDNamesThePaneBare pins the fallback: a pane pop cannot
// read a pid for is still owned, by pane id alone.
func TestPaneOwnerWithoutAPIDNamesThePaneBare(t *testing.T) {
	fake := &tmuxtest.Fake{PaneCommandMap: map[string]string{"%17": "claude"}}
	t.Setenv("TMUX_PANE", "%17")
	if got := DefaultClaimOwner(fake); got != "pane:%17" {
		t.Fatalf("DefaultClaimOwner() with no pane pid = %q, want pane:%%17", got)
	}
}

func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
