package wayfinder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
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
	// A scan reads the archived bit out of the Work registry, so even a fold
	// fixture needs the store pointed somewhere disposable.
	fs := realFSWithDataHome(filepath.Join(storageDir, "xdg"))
	td := &tasks.Deps{FS: fs}
	t.Cleanup(func() { _ = td.CloseStore() })
	return &Deps{FS: fs, Tasks: td}, storageDir
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
	if len(maps) != 1 || maps[0].Broken {
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
			if len(maps) != 1 || maps[0].Broken {
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

// TestScanMapsFoldRegistersTheMap covers the follow-up defect: a Map charted
// before registration existed scans fine but had no work_containers row, so
// pop map archive/next/claim refused it forever. The fold is the only place
// left that ever sees such a Map, so it is the fold's job to register it.
func TestScanMapsFoldRegistersTheMap(t *testing.T) {
	d, _ := registryFixture(t, map[string]string{
		"wayfinder/2026-07-01-map/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"wayfinder/2026-07-01-map/issues/01-first.md": "Type: research\nStatus: open\n\n## Question\nA\n",
	})

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatalf("ScanMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].Broken {
		t.Fatalf("maps = %+v, want one well-formed map", maps)
	}

	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	_, registered, err := s.FindWorkContainer(MapRef("2026-07-01-map"))
	if err != nil {
		t.Fatal(err)
	}
	if !registered {
		t.Fatal("folded map has no work_containers row")
	}

	// The workaround this slice exists to remove: `pop map register` should not
	// be needed before archive, next or claim work on a folded Map.
	if _, err := NextTicket(d, "", "2026-07-01-map"); err != nil {
		t.Fatalf("NextTicket after fold: %v", err)
	}
	if _, err := ArchiveMap(d, "", "2026-07-01-map"); err != nil {
		t.Fatalf("ArchiveMap after fold: %v", err)
	}
}

// TestScanMapsFoldRegistersBeforeWritingManifest pins the ordering the task
// calls out: registering after the mint would leave a crash between the two
// writes unrecoverable — a Map that never folds again (no manifest to trigger
// a second attempt) and never registers (the row was never written). Making
// registration idempotent and first means the same crash instead leaves a
// registered Map the next scan folds cleanly.
func TestScanMapsFoldRegistersBeforeWritingManifest(t *testing.T) {
	storageDir := t.TempDir()
	files := map[string]string{
		"maps/2026-07-01-map/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"maps/2026-07-01-map/issues/01-first.md": "Type: research\nStatus: open\n\n## Question\nA\n",
	}
	for rel, content := range files {
		path := filepath.Join(storageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	dataHome := filepath.Join(storageDir, "xdg")
	workingFS := realFSWithDataHome(dataHome)
	td := &tasks.Deps{FS: workingFS}
	t.Cleanup(func() { _ = td.CloseStore() })

	// The manifest write goes through a same-directory temp file and rename
	// (WriteAtomicWith); failing every write to such a temp file fails the mint
	// without touching anything else, including the registry, which never reads
	// d.FS's content — only Tasks.FS's Getenv, still wired to the working fs.
	failingFS := realFSWithDataHome(dataHome)
	if mfs, ok := failingFS.(*deps.MockFileSystem); ok {
		real := mfs.WriteFileFunc
		mfs.WriteFileFunc = func(path string, data []byte, perm os.FileMode) error {
			if strings.Contains(filepath.Base(path), ".task-tmp-") {
				return fmt.Errorf("simulated write failure")
			}
			return real(path, data, perm)
		}
	} else {
		t.Fatal("realFSWithDataHome did not return *deps.MockFileSystem")
	}

	// A failed mint surfaces as a BROKEN row, not a scan-level error: ScanMapsInStorage
	// never fails the whole scan over one bad folder.
	first := &Deps{FS: failingFS, Tasks: td}
	maps, err := ScanMapsInStorage(first, storageDir)
	if err != nil {
		t.Fatalf("ScanMapsInStorage: %v", err)
	}
	if len(maps) != 1 || !maps[0].Broken {
		t.Fatalf("maps = %+v, want the simulated manifest-write failure to surface as BROKEN", maps)
	}

	mapDir := filepath.Join(storageDir, "maps", "2026-07-01-map")
	if _, err := os.Stat(MapManifestPath(mapDir)); !os.IsNotExist(err) {
		t.Fatalf("manifest present despite the failed write: %v", err)
	}

	s, err := openWorkRegistry(&Deps{FS: workingFS, Tasks: td})
	if err != nil {
		t.Fatal(err)
	}
	_, registered, err := s.FindWorkContainer(MapRef("2026-07-01-map"))
	if err != nil {
		t.Fatal(err)
	}
	if !registered {
		t.Fatal("registration did not survive the failed mint that followed it")
	}

	second := &Deps{FS: workingFS, Tasks: td}
	maps, err = ScanMapsInStorage(second, storageDir)
	if err != nil {
		t.Fatalf("second ScanMapsInStorage: %v", err)
	}
	if len(maps) != 1 || maps[0].Broken || len(maps[0].Tickets) != 1 {
		t.Fatalf("second scan = %+v, want a clean fold this time", maps)
	}
	if _, err := os.Stat(MapManifestPath(mapDir)); err != nil {
		t.Fatalf("manifest still missing after the clean scan: %v", err)
	}
}

// TestScanMapsWithManifestNeverOpensRegistry pins the read-purity criterion: a
// Map that already carries a manifest never triggers the fold, so a scan over
// it opens no store. dataHome points at a real, read-only-for-mkdir path (no
// XDG dir exists and none is creatable under it), so if the fold's registry
// open ever fired here it would fail loudly rather than being silently skipped.
func TestScanMapsWithManifestNeverOpensRegistry(t *testing.T) {
	dataHome := "/nonexistent-readonly-marker/xdg"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "maps", "2026-07-01-map")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"):                "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(mapDir, "issues", "01-first.md"): "## Question\nA\n",
		filepath.Join(mapDir, MapManifestFileName): `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"First","type":"research","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	}
	d := wayfinderTestDeps(t, dataHome, commonDir, files)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0].Broken || len(maps[0].Tickets) != 1 {
		t.Fatalf("maps = %+v, want the one manifest-backed map", maps)
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
