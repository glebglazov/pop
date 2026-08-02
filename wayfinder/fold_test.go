package wayfinder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// foldFixture lays down a pre-manifest store on disk — maps under the retired
// wayfinder/ directory, ticket metadata in markdown headers — and returns real-FS
// deps plus the storage root, so a scan exercises the fold end to end.
func foldFixture(t *testing.T, files map[string]string) (*Deps, string) {
	t.Helper()
	storageDir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(storageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return &Deps{FS: deps.NewRealFileSystem()}, storageDir
}

func readFoldedManifest(t *testing.T, mapDir string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(MapManifestPath(mapDir))
	if err != nil {
		t.Fatalf("read minted manifest: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse minted manifest: %v", err)
	}
	return raw
}

func TestScanMapsFoldsLegacyMapIntoManifest(t *testing.T) {
	d, storageDir := foldFixture(t, map[string]string{
		"wayfinder/2026-07-01-map/map.md":              "Status: active\n\n## Destination\nShip it\n",
		"wayfinder/2026-07-01-map/issues/01-first.md":  "# 01 — First\n\nType: research\nStatus: resolved\n\n## Question\nA\n",
		"wayfinder/2026-07-01-map/issues/02-second.md": "Type: grilling\nStatus: claimed\nBlocked by: 01\n\n## Question\nB\n",
	})

	maps, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("ScanMapsInStorage: %v", err)
	}
	if len(maps) != 1 || maps[0].Malformed {
		t.Fatalf("maps = %+v, want one well-formed map", maps)
	}

	// The directory rename rides the same read: maps/ exists, wayfinder/ is gone.
	mapDir := filepath.Join(storageDir, "maps", "2026-07-01-map")
	if maps[0].Dir != mapDir {
		t.Fatalf("map dir = %q, want %q", maps[0].Dir, mapDir)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "wayfinder")); !os.IsNotExist(err) {
		t.Fatalf("legacy wayfinder directory still present: %v", err)
	}

	// A claimed ticket drops to open: the file format names no owner.
	tickets := maps[0].Tickets
	if len(tickets) != 2 {
		t.Fatalf("tickets = %+v, want 2", tickets)
	}
	if tickets[0].Status != TicketResolved || tickets[1].Status != TicketOpen {
		t.Fatalf("ticket statuses = %q/%q, want resolved/open", tickets[0].Status, tickets[1].Status)
	}
	if tickets[1].Type != TicketGrilling || len(tickets[1].BlockedBy) != 1 || tickets[1].BlockedBy[0] != "01" {
		t.Fatalf("ticket 02 = %+v, want grilling blocked by 01", tickets[1])
	}

	raw := readFoldedManifest(t, mapDir)
	if got := string(raw["spawned_sets"]); got != "[]" {
		t.Fatalf("spawned_sets = %s, want []", got)
	}
	var minted []ManifestTicket
	if err := json.Unmarshal(raw["tickets"], &minted); err != nil {
		t.Fatalf("parse minted tickets: %v", err)
	}
	want := []ManifestTicket{
		{ID: "01", File: "01-first.md", Type: TicketResearch, Status: TicketResolved, BlockedBy: []string{}},
		{ID: "02", File: "02-second.md", Type: TicketGrilling, Status: TicketOpen, BlockedBy: []string{"01"}},
	}
	if len(minted) != len(want) {
		t.Fatalf("minted tickets = %+v, want %+v", minted, want)
	}
	for i := range want {
		if minted[i].ID != want[i].ID || minted[i].File != want[i].File ||
			minted[i].Type != want[i].Type || minted[i].Status != want[i].Status ||
			strings.Join(minted[i].BlockedBy, ",") != strings.Join(want[i].BlockedBy, ",") {
			t.Fatalf("minted[%d] = %+v, want %+v", i, minted[i], want[i])
		}
	}

	// The headers are gone from the markdown; the body is untouched.
	for name, wantBody := range map[string]string{
		"01-first.md":  "# 01 — First\n\n## Question\nA\n",
		"02-second.md": "## Question\nB\n",
	} {
		data, err := os.ReadFile(filepath.Join(mapDir, issuesDirName, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != wantBody {
			t.Fatalf("%s after fold = %q, want %q", name, data, wantBody)
		}
	}
}

func TestScanMapsFoldIsIdempotent(t *testing.T) {
	d, storageDir := foldFixture(t, map[string]string{
		"wayfinder/2026-07-01-map/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"wayfinder/2026-07-01-map/issues/01-first.md": "Type: research\nStatus: open\n\n## Question\nA\n",
	})

	first, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	mapDir := filepath.Join(storageDir, "maps", "2026-07-01-map")
	manifest, err := os.ReadFile(MapManifestPath(mapDir))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	ticket, err := os.ReadFile(filepath.Join(mapDir, issuesDirName, "01-first.md"))
	if err != nil {
		t.Fatalf("read ticket: %v", err)
	}

	second, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second) != 1 || len(second[0].Tickets) != 1 || second[0].Tickets[0].Status != TicketOpen {
		t.Fatalf("second scan = %+v, want the same one open ticket", second)
	}
	if second[0].Dir != first[0].Dir {
		t.Fatalf("second scan dir = %q, want %q", second[0].Dir, first[0].Dir)
	}

	manifestAgain, err := os.ReadFile(MapManifestPath(mapDir))
	if err != nil {
		t.Fatalf("re-read manifest: %v", err)
	}
	ticketAgain, err := os.ReadFile(filepath.Join(mapDir, issuesDirName, "01-first.md"))
	if err != nil {
		t.Fatalf("re-read ticket: %v", err)
	}
	if string(manifestAgain) != string(manifest) || string(ticketAgain) != string(ticket) {
		t.Fatalf("second scan rewrote the Map:\nmanifest %q -> %q\nticket %q -> %q",
			manifest, manifestAgain, ticket, ticketAgain)
	}
}

func TestScanMapsNeverRefoldsMapCarryingManifest(t *testing.T) {
	// The markdown still carries a contradicting Status: header, the shape a
	// half-migrated Map has. The manifest is the only source that counts, and the
	// human's file is not rewritten behind a manifest that already exists.
	headers := "Type: research\nStatus: open\n\n## Question\nA\n"
	d, storageDir := foldFixture(t, map[string]string{
		"maps/2026-07-01-map/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"maps/2026-07-01-map/issues/01-first.md": headers,
		"maps/2026-07-01-map/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"First","type":"research","status":"resolved"}],` +
			`"spawned_sets":[]}`,
	})

	maps, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("ScanMapsInStorage: %v", err)
	}
	if len(maps) != 1 || len(maps[0].Tickets) != 1 || maps[0].Tickets[0].Status != TicketResolved {
		t.Fatalf("maps = %+v, want the manifest's resolved ticket", maps)
	}
	data, err := os.ReadFile(filepath.Join(storageDir, "maps", "2026-07-01-map", issuesDirName, "01-first.md"))
	if err != nil {
		t.Fatalf("read ticket: %v", err)
	}
	if string(data) != headers {
		t.Fatalf("ticket markdown = %q, want untouched %q", data, headers)
	}
}

func TestScanMapsSkipsFoldWhenHeadersCannotFormManifest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ticket string
	}{
		{name: "blocker names no ticket", ticket: "Type: research\nStatus: open\nBlocked by: 99\n\n## Question\nA\n"},
		{name: "no type header", ticket: "Status: open\n\n## Question\nA\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, storageDir := foldFixture(t, map[string]string{
				"maps/2026-07-01-map/map.md":             "Status: active\n\n## Destination\nShip it\n",
				"maps/2026-07-01-map/issues/01-first.md": tc.ticket,
			})

			maps, err := ScanMapsInStorage(d, storageDir)
			if err != nil {
				t.Fatalf("ScanMapsInStorage: %v", err)
			}
			if len(maps) != 1 || maps[0].Malformed {
				t.Fatalf("maps = %+v, want one readable map", maps)
			}
			if len(maps[0].Tickets) != 1 || maps[0].Tickets[0].Status != TicketOpen {
				t.Fatalf("tickets = %+v, want the header-parsed open ticket", maps[0].Tickets)
			}
			mapDir := filepath.Join(storageDir, "maps", "2026-07-01-map")
			if _, err := os.Stat(MapManifestPath(mapDir)); !os.IsNotExist(err) {
				t.Fatalf("manifest minted from invalid headers: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(mapDir, issuesDirName, "01-first.md"))
			if err != nil {
				t.Fatalf("read ticket: %v", err)
			}
			if string(data) != tc.ticket {
				t.Fatalf("ticket markdown = %q, want untouched %q", data, tc.ticket)
			}
		})
	}
}

func TestScanMapsFoldsDirectoryForTicketlessMap(t *testing.T) {
	d, storageDir := foldFixture(t, map[string]string{
		"wayfinder/2026-07-01-map/map.md": "Status: active\n\n## Destination\nShip it\n",
	})

	maps, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("ScanMapsInStorage: %v", err)
	}
	if len(maps) != 1 || len(maps[0].Tickets) != 0 {
		t.Fatalf("maps = %+v, want one ticketless map", maps)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "wayfinder")); !os.IsNotExist(err) {
		t.Fatalf("legacy directory still present: %v", err)
	}
	// Nothing to mint from, so no manifest is invented for a Map still being charted.
	if _, err := os.Stat(MapManifestPath(filepath.Join(storageDir, "maps", "2026-07-01-map"))); !os.IsNotExist(err) {
		t.Fatalf("manifest minted for a ticketless map: %v", err)
	}
}

func TestStripTicketHeaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "headers at the top",
			content: "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nA\n",
			want:    "## Question\nA\n",
		},
		{
			name:    "headers under a title",
			content: "# 01 — First\n\nType: research\nStatus: open\n\n## Question\nA\n",
			want:    "# 01 — First\n\n## Question\nA\n",
		},
		{
			name:    "body keeps its own key-shaped lines",
			content: "Status: open\n\nThe answer.\n\nStatus: whatever the human wrote\n",
			want:    "The answer.\n\nStatus: whatever the human wrote\n",
		},
		{
			name:    "nothing to strip",
			content: "## Question\nA\n",
			want:    "## Question\nA\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripTicketHeaders(tc.content); got != tc.want {
				t.Fatalf("StripTicketHeaders(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}
