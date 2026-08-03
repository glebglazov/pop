package wayfinder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

func mapStatusOnDisk(t *testing.T, d *Deps, storageDir, mapID string) MapStatus {
	t.Helper()
	m, err := FindMap(d, "", mapID)
	if err != nil {
		t.Fatalf("FindMap(%s): %v", mapID, err)
	}
	if m.Broken {
		t.Fatalf("map %s is BROKEN: %s", mapID, m.BrokenReason)
	}
	return m.Status
}

// The whole arrival round trip against real files: arrive writes the status and
// takes the Map's tmux session with it, open puts the Map back on the frontier.
func TestArriveWritesStatusAndTearsDownSessionThenOpenReverses(t *testing.T) {
	d, storageDir := registryFixture(t, oneTicketMap("demo-map"))
	fake := &tmuxtest.Fake{Live: map[string]string{
		MapSessionName("demo-map"): "/repo",
		"pop-map-other":            "/repo",
	}}
	d.Tmux = fake
	mustRegister(t, d, "demo-map")

	arrived, err := ArriveMap(d, "", "demo-map")
	if err != nil {
		t.Fatalf("ArriveMap: %v", err)
	}
	if arrived.Status != MapArrived || arrived.Previous != MapActive || arrived.Unchanged {
		t.Fatalf("result = %+v, want active -> arrived", arrived)
	}
	if arrived.KilledSession != MapSessionName("demo-map") {
		t.Fatalf("killed session = %q, want %q", arrived.KilledSession, MapSessionName("demo-map"))
	}
	if fake.HasSession(MapSessionName("demo-map")) {
		t.Fatal("the map's session survived arrival")
	}
	if !fake.HasSession("pop-map-other") {
		t.Fatal("arrival killed another map's session")
	}
	if got := mapStatusOnDisk(t, d, storageDir, "demo-map"); got != MapArrived {
		t.Fatalf("on-disk status = %q, want arrived", got)
	}
	body, err := os.ReadFile(filepath.Join(storageDir, "maps", "demo-map", "map.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Status: arrived") || strings.Contains(string(body), "Status: active") {
		t.Fatalf("map.md still carries the old status:\n%s", body)
	}
	// Arrival is a declaration, so re-declaring it is a no-op and not an error.
	again, err := ArriveMap(d, "", "demo-map")
	if err != nil {
		t.Fatalf("second ArriveMap: %v", err)
	}
	if !again.Unchanged || again.KilledSession != "" {
		t.Fatalf("second arrive = %+v, want unchanged with no session to kill", again)
	}

	reopened, err := OpenMap(d, "", "demo-map")
	if err != nil {
		t.Fatalf("OpenMap: %v", err)
	}
	if reopened.Status != MapActive || reopened.Previous != MapArrived {
		t.Fatalf("open result = %+v, want arrived -> active", reopened)
	}
	// Open is both halves: the Map is grillable again *and* its session is back.
	if reopened.Session == nil || !reopened.Session.Created {
		t.Fatalf("open session = %+v, want a freshly created one", reopened.Session)
	}
	if !fake.HasSession(MapSessionName("demo-map")) {
		t.Fatal("open did not bring the map's session back")
	}
	if len(fake.Attached) == 0 && len(fake.Switched) == 0 {
		t.Fatal("open did not put the caller in the session")
	}
	if got := mapStatusOnDisk(t, d, storageDir, "demo-map"); got != MapActive {
		t.Fatalf("status after open = %q, want active", got)
	}
}

// The gate is the destination, not empty fog: unfinished tickets are listed and
// the arrival goes through, because refusing would only buy fake resolutions.
func TestArriveWarnsAboutUnfinishedTicketsAndProceeds(t *testing.T) {
	files := oneTicketMap("fog-map")
	files["maps/fog-map/issues/02-second.md"] = "## Question\nAnd?\n"
	files["maps/fog-map/index.json"] = `{"tickets":[` +
		`{"id":"01","file":"01-first.md","title":"First","type":"grilling","status":"open","blocked_by":[]},` +
		`{"id":"02","file":"02-second.md","title":"Second","type":"grilling","status":"open","blocked_by":[]}` +
		`],"spawned_sets":[]}`
	d, storageDir := registryFixture(t, files)
	d.Tmux = &tmuxtest.Fake{}
	d.Owner = func() string { return "pane:%9" }
	mustRegister(t, d, "fog-map")
	if _, err := ClaimTicket(d, "", "fog-map", "02"); err != nil {
		t.Fatalf("ClaimTicket: %v", err)
	}

	result, err := ArriveMap(d, "", "fog-map")
	if err != nil {
		t.Fatalf("ArriveMap: %v", err)
	}
	if got := mapStatusOnDisk(t, d, storageDir, "fog-map"); got != MapArrived {
		t.Fatalf("status = %q, want arrived despite open tickets", got)
	}
	if len(result.Unfinished) != 2 {
		t.Fatalf("unfinished = %+v, want both tickets", result.Unfinished)
	}
	byID := map[string]Ticket{}
	for _, ticket := range result.Unfinished {
		byID[ticket.ID] = ticket
	}
	if byID["01"].Status != TicketOpen {
		t.Fatalf("ticket 01 = %+v, want open", byID["01"])
	}
	if byID["02"].Status != TicketClaimed || byID["02"].ClaimOwner != "pane:%9" {
		t.Fatalf("ticket 02 = %+v, want claimed by pane:%%9", byID["02"])
	}
}

// `done` is a hard cut with no read-fold, so the retired word reads as BROKEN and
// the row carries the corrective a human acts on.
func TestUnknownMapStatusRendersBrokenWithCorrective(t *testing.T) {
	files := oneTicketMap("stale-map")
	files["maps/stale-map/map.md"] = "Status: done\n\n## Destination\nShip it"
	d, _ := registryFixture(t, files)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || !maps[0].Broken || maps[0].Status != MapBroken {
		t.Fatalf("maps = %+v, want one BROKEN map", maps)
	}
	for _, want := range []string{`unknown map status "done"`, "active | arrived | abandoned", "pop map arrive"} {
		if !strings.Contains(maps[0].BrokenReason, want) {
			t.Fatalf("reason %q missing %q", maps[0].BrokenReason, want)
		}
	}

	// A BROKEN Map stays on the default table — it exists to be fixed — and its
	// row prints the corrective in place of counts it cannot have.
	snap, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 1 || !snap.Rows[0].Broken {
		t.Fatalf("rows = %+v, want the BROKEN row visible", snap.Rows)
	}
	var out strings.Builder
	if err := RenderStatus(&out, snap); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "BROKEN") || !strings.Contains(out.String(), "pop map arrive") {
		t.Fatalf("status table missing the verdict or the fix:\n%s", out.String())
	}

	// The retired word is not something a verb walks past: registration hands back
	// the same corrective as its fix list, so the Map is repaired by hand first.
	if _, err := RegisterMap(d, "", "stale-map"); err == nil || !strings.Contains(err.Error(), "active | arrived | abandoned") {
		t.Fatalf("RegisterMap on a BROKEN map: err = %v, want the status corrective", err)
	}
}

func TestReplaceMapStatusInsertsAndReplaces(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "replaces the existing line in place",
			in:   "Status: active\n\n## Destination\nShip it\n",
			want: "Status: arrived\n\n## Destination\nShip it\n",
		},
		{
			name: "inserts under the title when the map carries no status",
			in:   "# Demo map\n\n## Destination\nShip it\n",
			want: "# Demo map\n\nStatus: arrived\n\n## Destination\nShip it\n",
		},
		{
			name: "inserts at the top of a headingless map",
			in:   "## Destination\nShip it\n",
			want: "Status: arrived\n\n## Destination\nShip it\n",
		},
		{
			name: "leaves a Status: line inside prose alone",
			in:   "Status: active\n\n## Notes\nStatus: done was the old word\n",
			want: "Status: arrived\n\n## Notes\nStatus: done was the old word\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplaceMapStatus(tc.in, MapArrived); got != tc.want {
				t.Fatalf("ReplaceMapStatus:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// Registration is the gate on every mutating Map verb, arrival included: a status
// declaration against a Map pop does not look after has nowhere to land.
func TestArriveRefusesAnUnregisteredMap(t *testing.T) {
	d, storageDir := registryFixture(t, oneTicketMap("loose-map"))
	d.Tmux = &tmuxtest.Fake{}

	if _, err := ArriveMap(d, "", "loose-map"); err == nil || !strings.Contains(err.Error(), "pop map register") {
		t.Fatalf("err = %v, want a refusal naming pop map register", err)
	}
	if got := mapStatusOnDisk(t, d, storageDir, "loose-map"); got != MapActive {
		t.Fatalf("status = %q, want an untouched active map", got)
	}
}
