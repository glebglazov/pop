package wayfinder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func wayfinderTestDeps(t *testing.T, dataHome, commonDir string, files map[string]string) *Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dataHome)
	fs := &deps.MockFileSystem{
		GetwdFunc: func() (string, error) { return "/mock/cwd", nil },
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return "/mock/home", nil },
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			entries := dirEntriesFor(path, files)
			if entries == nil {
				return nil, os.ErrNotExist
			}
			return entries, nil
		},
		ReadFileFunc: func(path string) ([]byte, error) {
			if content, ok := files[path]; ok {
				return []byte(content), nil
			}
			return nil, os.ErrNotExist
		},
	}
	taskDeps := &tasks.Deps{
		FS: fs,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir" {
					return commonDir, nil
				}
				return "", nil
			},
		},
	}
	return &Deps{FS: fs, Tasks: taskDeps}
}

func dirEntriesFor(path string, files map[string]string) []os.DirEntry {
	children := map[string]bool{}
	dirs := map[string]bool{}
	for filePath := range files {
		if !strings.HasPrefix(filePath, path+string(os.PathSeparator)) && filePath != path {
			continue
		}
		rel := strings.TrimPrefix(filePath, path+string(os.PathSeparator))
		if rel == "" {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		name := parts[0]
		if len(parts) == 1 {
			children[name] = false
			continue
		}
		children[name] = true
		dirs[name] = true
	}
	if len(children) == 0 {
		return nil
	}
	var out []os.DirEntry
	for name, isDir := range children {
		out = append(out, deps.MockDirEntry{NameVal: name, IsDirVal: isDir || dirs[name]})
	}
	return out
}

func TestScanMapsEmptyDirectory(t *testing.T) {
	d := wayfinderTestDeps(t, "/data", "/repo/.git", nil)
	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 0 {
		t.Fatalf("maps = %d, want 0", len(maps))
	}
}

func TestScanMapsParsesMapAndTickets(t *testing.T) {
	files := map[string]string{
		"maps/2026-07-19-demo/map.md":              "Status: active\n\n## Destination\nShip it",
		"maps/2026-07-19-demo/issues/01-first.md":  "Type: research\nStatus: resolved\n",
		"maps/2026-07-19-demo/issues/02-second.md": "Type: task\nBlocked by: 01\n",
	}
	d, _ := registryFixture(t, files)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 {
		t.Fatalf("maps = %d, want 1", len(maps))
	}
	m := maps[0]
	if m.Status != MapActive || m.Destination != "Ship it" {
		t.Fatalf("map = %+v", m)
	}
	if len(m.Tickets) != 2 {
		t.Fatalf("tickets = %d, want 2", len(m.Tickets))
	}
	if len(Frontier(m.Tickets)) != 1 || Frontier(m.Tickets)[0].ID != "02" {
		t.Fatalf("frontier = %+v", Frontier(m.Tickets))
	}
}

func TestScanMapsBrokenFolder(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "maps", "broken-map")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"): "Status: wandering\n",
	}
	d := wayfinderTestDeps(t, dataHome, commonDir, files)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || !maps[0].Broken {
		t.Fatalf("expected one malformed map, got %+v", maps)
	}
}

// An arrived Map is deliberately not hidden: it is the lineage view for the sets
// it spawned, so only archiving and abandonment take a Map off the table.
func TestBuildStatusHidesAbandonedAndArchivedButKeepsArrived(t *testing.T) {
	files := oneTicketMap("2026-08-03-archived-map")
	files["maps/active/map.md"] = "## Destination\nactive"
	files["maps/arrived-map/map.md"] = "Status: arrived\n\n## Destination\narrived"
	files["maps/quit-map/map.md"] = "Status: abandoned\n\n## Destination\nquit"
	d, _ := registryFixture(t, files)
	mustRegister(t, d, "2026-08-03-archived-map")
	if _, err := ArchiveMap(d, "", "2026-08-03-archived-map"); err != nil {
		t.Fatal(err)
	}

	snap, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var visible []string
	for _, row := range snap.Rows {
		visible = append(visible, row.ID)
	}
	if len(visible) != 2 || visible[0] != "active" || visible[1] != "arrived-map" {
		t.Fatalf("default rows = %v, want [active arrived-map]", visible)
	}

	all, err := BuildStatus(d, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Rows) != 4 {
		t.Fatalf("all rows = %d, want 4", len(all.Rows))
	}
}

func TestRenderStatusTable(t *testing.T) {
	snap := StatusSnapshot{Rows: []StatusRow{{
		ID:              "demo",
		Status:          MapActive,
		DestinationGist: "Ship it",
		Counts:          TicketCounts{Open: 1, Claimed: 0, Resolved: 1},
		FrontierSize:    1,
	}}}
	var buf strings.Builder
	if err := RenderStatus(&buf, snap); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"MAP", "demo", "active", "Ship it", "1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
