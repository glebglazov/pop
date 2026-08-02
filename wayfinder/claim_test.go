package wayfinder

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func at(hour int) time.Time {
	return time.Date(2026, 8, 3, hour, 0, 0, 0, time.UTC)
}

func asWindow(d *Deps, owner string, now time.Time) {
	d.Owner = func() string { return owner }
	d.Clock = func() time.Time { return now }
}

// TestNextHandsOutTheFrontierInOrderThenRefuses is the parallel-grilling loop as
// two windows live it: each `next` returns a different frontier ticket in
// manifest order, the blocked one is never offered, and the empty frontier is an
// error rather than a silent success.
func TestNextHandsOutTheFrontierInOrderThenRefuses(t *testing.T) {
	t.Parallel()
	d, storageDir := claimFixture(t)

	asWindow(d, "pane:%1", at(9))
	first, err := NextTicket(d, "", claimMapID)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	wantPath := filepath.Join(storageDir, "maps", claimMapID, "issues", "01-first.md")
	if first.Ticket.ID != "01" || first.Path != wantPath || first.Owner != "pane:%1" {
		t.Fatalf("first next = %+v, want ticket 01 at %s", first, wantPath)
	}
	if first.Stole != nil {
		t.Fatalf("a free ticket reported a steal: %+v", first.Stole)
	}

	asWindow(d, "pane:%2", at(9).Add(time.Minute))
	second, err := NextTicket(d, "", claimMapID)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Ticket.ID != "03" {
		t.Fatalf("second next took %q, want 03 — 02 is blocked by an unresolved 01", second.Ticket.ID)
	}

	asWindow(d, "pane:%3", at(9).Add(2*time.Minute))
	_, err = NextTicket(d, "", claimMapID)
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
	if _, err := NextTicket(d, "", claimMapID); err != nil {
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
	if len(claimed) != 2 || claimed["01"] != "pane:%1" || claimed["03"] != "pane:%1" {
		t.Fatalf("scanned claims = %v, want 01 and 03 held by pane:%%1", claimed)
	}
	if counts := CountTickets(m.Tickets); counts.Claimed != 2 || counts.Open != 1 {
		t.Fatalf("counts = %+v, want 2 claimed and 1 open", counts)
	}
}

// TestNextStealsAnExpiredClaim: a grilling window that died would otherwise hold
// its ticket forever, so the TTL hands it back — loudly.
func TestNextStealsAnExpiredClaim(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)

	asWindow(d, "pane:%dead", at(9))
	if _, err := NextTicket(d, "", claimMapID); err != nil {
		t.Fatalf("first next: %v", err)
	}

	// Still held while the claim is fresh: the next window gets 03 instead.
	asWindow(d, "pane:%live", at(11))
	fresh, err := NextTicket(d, "", claimMapID)
	if err != nil || fresh.Ticket.ID != "03" {
		t.Fatalf("next over a live claim = %+v (%v), want 03", fresh, err)
	}

	asWindow(d, "pane:%new", at(9).Add(5*time.Hour))
	stolen, err := NextTicket(d, "", claimMapID)
	if err != nil {
		t.Fatalf("next after the TTL: %v", err)
	}
	if stolen.Ticket.ID != "01" {
		t.Fatalf("next after the TTL took %q, want the abandoned 01", stolen.Ticket.ID)
	}
	if stolen.Stole == nil || stolen.Stole.Owner != "pane:%dead" {
		t.Fatalf("steal was not reported: %+v", stolen.Stole)
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
	next, err := NextTicket(d, "", claimMapID)
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
	_, err = NextTicket(d, "", "2026-08-03-charting")
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

	sole, err := NextTicket(d, "", "")
	if err != nil || sole.Ticket.ID != "01" {
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
	_, err = NextTicket(d, "", "")
	if err == nil || !strings.Contains(err.Error(), "several active maps") {
		t.Fatalf("bare next with two active maps = %v", err)
	}
}

// TestDefaultClaimOwnerPrefersThePane pins the identity rule: the pane the
// command runs in when there is one, the pid otherwise. No configuration either
// way.
func TestDefaultClaimOwnerPrefersThePane(t *testing.T) {
	t.Setenv("TMUX_PANE", "%17")
	if got := DefaultClaimOwner(); got != "pane:%17" {
		t.Fatalf("DefaultClaimOwner() = %q inside tmux, want pane:%%17", got)
	}
	t.Setenv("TMUX_PANE", "")
	want := "pid:" + strconv.Itoa(os.Getpid())
	if got := DefaultClaimOwner(); got != want {
		t.Fatalf("DefaultClaimOwner() = %q outside tmux, want %q", got, want)
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
