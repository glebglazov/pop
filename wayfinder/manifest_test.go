package wayfinder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// realFSMapDir lays out a Map folder on disk and returns deps reading it.
func realFSMapDir(t *testing.T, files map[string]string) (*Deps, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Deps{FS: deps.NewRealFileSystem()}, dir
}

func TestMapManifestRoundTrip(t *testing.T) {
	d, dir := realFSMapDir(t, map[string]string{
		"issues/01-first.md":  "## Question\nwhy?\n",
		"issues/02-second.md": "## Question\nhow?\n",
		"index.json": `{
  "tickets": [
    {"id": "01", "file": "01-first.md", "title": "First", "type": "research", "status": "resolved",
     "blocked_by": [], "adr_drafts": ["adrs/a1b2c3d4-thing.md"], "context_drafts": ["context/01-thing.md"]},
    {"id": "02", "file": "02-second.md", "title": "Second", "type": "task", "status": "open", "blocked_by": ["01"]}
  ],
  "spawned_sets": ["2026-08-01-demo"],
  "notes": "kept verbatim"
}`,
	})

	m, err := LoadMapManifest(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Valid {
		t.Fatalf("manifest invalid: %v", m.Errors)
	}
	if len(m.Tickets) != 2 {
		t.Fatalf("tickets = %d, want 2", len(m.Tickets))
	}
	first := m.Tickets[0]
	if first.Title != "First" || first.Type != TicketResearch || first.Status != TicketResolved {
		t.Fatalf("first = %+v", first)
	}
	if len(first.ADRDrafts) != 1 || first.ADRDrafts[0] != "adrs/a1b2c3d4-thing.md" {
		t.Fatalf("adr drafts = %v", first.ADRDrafts)
	}
	if len(first.ContextDrafts) != 1 || first.ContextDrafts[0] != "context/01-thing.md" {
		t.Fatalf("context drafts = %v", first.ContextDrafts)
	}
	if len(m.SpawnedSets) != 1 || m.SpawnedSets[0] != "2026-08-01-demo" {
		t.Fatalf("spawned sets = %v", m.SpawnedSets)
	}

	m.Tickets[1].Status = TicketResolved
	if err := WriteMapManifest(d, m); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadMapManifest(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Valid {
		t.Fatalf("reloaded invalid: %v", reloaded.Errors)
	}
	if reloaded.Tickets[1].Status != TicketResolved {
		t.Fatalf("status did not round-trip: %+v", reloaded.Tickets[1])
	}
	if len(reloaded.SpawnedSets) != 1 || reloaded.SpawnedSets[0] != "2026-08-01-demo" {
		t.Fatalf("spawned sets lost: %v", reloaded.SpawnedSets)
	}
	if got := string(reloaded.Unknown["notes"]); got != `"kept verbatim"` {
		t.Fatalf("unknown key lost: %q", got)
	}
	if len(reloaded.Tickets[0].ADRDrafts) != 1 {
		t.Fatalf("adr drafts lost: %+v", reloaded.Tickets[0])
	}
}

func TestMapManifestSpawnedSetsDefaultsToEmptyArray(t *testing.T) {
	d, dir := realFSMapDir(t, map[string]string{
		"issues/01-first.md": "## Question\nwhy?\n",
		"index.json":         `{"tickets": [{"id": "01", "file": "01-first.md", "title": "First", "type": "task", "status": "open"}]}`,
	})

	m, err := LoadMapManifest(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Valid {
		t.Fatalf("manifest invalid: %v", m.Errors)
	}
	if m.SpawnedSets == nil || len(m.SpawnedSets) != 0 {
		t.Fatalf("spawned sets = %#v, want empty non-nil", m.SpawnedSets)
	}
	if err := WriteMapManifest(d, m); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(MapManifestPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if got := string(raw["spawned_sets"]); got != "[]" {
		t.Fatalf("spawned_sets = %s, want []", got)
	}
	if !strings.Contains(string(data), `"blocked_by": []`) {
		t.Fatalf("blocked_by not written as array:\n%s", data)
	}
}

func TestMapManifestValidationNamesEveryProblem(t *testing.T) {
	d, dir := realFSMapDir(t, map[string]string{
		"issues/01-first.md":  "## Question\nwhy?\n",
		"issues/02-second.md": "## Question\nhow?\n",
		"issues/03-orphan.md": "## Question\nwho?\n",
		"index.json": `{
  "tickets": [
    {"id": "01", "file": "01-first.md", "title": "First", "type": "guessing", "status": "open"},
    {"id": "02", "file": "02-second.md", "title": "Second", "type": "task", "status": "pending", "blocked_by": ["09"]},
    {"id": "04", "file": "04-ghost.md", "title": "Ghost", "type": "task", "status": "open"}
  ]
}`,
	})

	m, err := LoadMapManifest(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Valid {
		t.Fatal("expected an invalid manifest")
	}
	reason := m.MalformedReason()
	for _, want := range []string{
		`ticket "01": unknown type "guessing"`,
		`ticket "02": unknown status "pending"`,
		`ticket "02": unresolved blocker "09"`,
		`ticket "04": missing markdown file "04-ghost.md"`,
		`03-orphan.md: no manifest entry`,
	} {
		if !strings.Contains(reason, want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, reason)
		}
	}
}

func TestLoadMapManifestMissingIsNotExist(t *testing.T) {
	d, dir := realFSMapDir(t, map[string]string{"map.md": "## Destination\nShip it\n"})

	if _, err := LoadMapManifest(d, dir); !os.IsNotExist(err) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

func TestScanMapsPrefersManifestOverHeaders(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "2026-07-19-demo")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"): "Status: active\n\n## Destination\nShip it",
		// Stale headers left behind by a pre-manifest Map: the manifest wins.
		filepath.Join(mapDir, "issues", "01-first.md"):  "Type: research\nStatus: open\n",
		filepath.Join(mapDir, "issues", "02-second.md"): "Type: task\nStatus: open\n",
		filepath.Join(mapDir, "index.json"): `{"tickets": [
			{"id": "01", "file": "01-first.md", "title": "First", "type": "grilling", "status": "resolved"},
			{"id": "02", "file": "02-second.md", "title": "Second", "type": "task", "status": "open", "blocked_by": ["01"]}
		]}`,
	}
	d := wayfinderTestDeps(t, dataHome, commonDir, files)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0].Malformed {
		t.Fatalf("maps = %+v", maps)
	}
	tickets := maps[0].Tickets
	if len(tickets) != 2 {
		t.Fatalf("tickets = %d, want 2", len(tickets))
	}
	if tickets[0].Status != TicketResolved || tickets[0].Type != TicketGrilling || tickets[0].Title != "First" {
		t.Fatalf("ticket 01 = %+v", tickets[0])
	}
	if tickets[1].Number != 2 || tickets[1].Slug != "second" || len(tickets[1].BlockedBy) != 1 {
		t.Fatalf("ticket 02 = %+v", tickets[1])
	}

	frontier := Frontier(tickets)
	if len(frontier) != 1 || frontier[0].ID != "02" {
		t.Fatalf("frontier = %+v", frontier)
	}
}

func TestScanMapsManifestBlockedTicketIsOffTheFrontier(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "blocked-map")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"):                 "## Destination\nShip it",
		filepath.Join(mapDir, "issues", "01-first.md"):  "## Question\nwhy?\n",
		filepath.Join(mapDir, "issues", "02-second.md"): "## Question\nhow?\n",
		filepath.Join(mapDir, "index.json"): `{"tickets": [
			{"id": "01", "file": "01-first.md", "title": "First", "type": "research", "status": "open"},
			{"id": "02", "file": "02-second.md", "title": "Second", "type": "task", "status": "open", "blocked_by": ["01"]}
		]}`,
	}
	d := wayfinderTestDeps(t, dataHome, commonDir, files)

	snap, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 1 {
		t.Fatalf("rows = %+v", snap.Rows)
	}
	row := snap.Rows[0]
	if row.Counts.Open != 2 || row.Counts.Resolved != 0 {
		t.Fatalf("counts = %+v", row.Counts)
	}
	if row.FrontierSize != 1 {
		t.Fatalf("frontier size = %d, want 1", row.FrontierSize)
	}
}

func TestScanMapsMalformedManifestRendersMapMalformed(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "bad-manifest")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"):                "## Destination\nShip it",
		filepath.Join(mapDir, "issues", "01-first.md"): "## Question\nwhy?\n",
		filepath.Join(mapDir, "index.json"): `{"tickets": [
			{"id": "01", "file": "01-first.md", "title": "First", "type": "research", "status": "claimed"}
		]}`,
	}
	d := wayfinderTestDeps(t, dataHome, commonDir, files)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || !maps[0].Malformed {
		t.Fatalf("maps = %+v", maps)
	}
	if !strings.Contains(maps[0].MalformedReason, `unknown status "claimed"`) {
		t.Fatalf("reason = %q", maps[0].MalformedReason)
	}
}
