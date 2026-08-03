package wayfinder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/glebglazov/pop/tasks"
)

const mapFileName = "map.md"
const issuesDirName = "issues"

// ScanMaps lists maps non-recursively under the current repository's
// <task-storage-root>/maps/*/. A missing maps directory yields an empty slice,
// never an error. Unparseable map folders are returned as malformed rows rather
// than failing the scan.
func ScanMaps(d *Deps, cwd string) ([]Map, error) {
	id, err := tasks.ResolveRepositoryIdentity(d.taskDeps(), cwd)
	if err != nil {
		return nil, err
	}
	return ScanMapsInStorage(d, id.StorageDir)
}

// ScanMapsInStorage lists maps under <storageDir>/maps/*/ without resolving git
// identity from cwd. It is the bulk seam the Work dashboard uses when walking
// every registered repository's Task storage. A missing maps directory yields an
// empty slice, never an error; a store still carrying the pre-rename wayfinder/
// directory, or the retired archive side-file, is folded here.
func ScanMapsInStorage(d *Deps, storageDir string) ([]Map, error) {
	root, err := mapsDir(d, storageDir)
	if err != nil {
		return nil, err
	}
	entries, err := d.FS.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	archived, err := archivedMapIDs(d, storageDir)
	if err != nil {
		return nil, err
	}
	claims, err := liveMapClaims(d)
	if err != nil {
		return nil, err
	}

	var maps []Map
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mapDir := filepath.Join(root, entry.Name())
		m, err := loadMap(d, entry.Name(), mapDir)
		if err != nil {
			maps = append(maps, Map{
				ID:           entry.Name(),
				Dir:          mapDir,
				Status:       MapBroken,
				Broken:       true,
				BrokenReason: err.Error(),
			})
			continue
		}
		m.Archived = archived[m.ID]
		applyClaims(m.Tickets, claims[m.ID])
		maps = append(maps, m)
	}
	sort.Slice(maps, func(i, j int) bool { return maps[i].ID < maps[j].ID })
	return maps, nil
}

func loadMap(d *Deps, id, dir string) (Map, error) {
	mapPath := filepath.Join(dir, mapFileName)
	data, err := d.FS.ReadFile(mapPath)
	if err != nil {
		return Map{}, fmt.Errorf("read map.md: %w", err)
	}
	status, destination, err := ParseMapMarkdown(string(data))
	if err != nil {
		return Map{}, err
	}

	tickets, spawned, err := loadMapContents(d, id, dir)
	if err != nil {
		return Map{}, err
	}

	return Map{
		ID:             id,
		Dir:            dir,
		Status:         status,
		Destination:    destination,
		DecisionsSoFar: ParseDecisionsSoFar(string(data)),
		Tickets:        tickets,
		SpawnedSets:    spawned,
	}, nil
}

// loadMapContents reads everything index.json owns — the tickets and the spawned
// set ids. It prefers the manifest and folds a Map that has none: index.json is
// the single source of a ticket's status, type and blocking wherever it exists,
// and the fold is what makes it exist for Maps charted before the manifest. A
// folded Map has spawned nothing yet, because the field the ids live in is the
// one the fold is minting.
func loadMapContents(d *Deps, id, dir string) ([]Ticket, []string, error) {
	manifest, err := LoadMapManifest(d, dir)
	if err == nil {
		if !manifest.Valid {
			return nil, nil, fmt.Errorf("%s: %s", MapManifestFileName, manifest.MalformedReason())
		}
		return manifest.ToTickets(), manifest.SpawnedSets, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, err
	}
	tickets, err := foldMapManifest(d, id, dir)
	return tickets, nil, err
}

// sortTickets orders tickets by number, falling back to id for the numberless.
func sortTickets(tickets []Ticket) {
	sort.Slice(tickets, func(i, j int) bool {
		if tickets[i].Number != tickets[j].Number {
			return tickets[i].Number < tickets[j].Number
		}
		return tickets[i].ID < tickets[j].ID
	})
}
