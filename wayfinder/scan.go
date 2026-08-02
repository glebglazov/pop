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

	var maps []Map
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mapDir := filepath.Join(root, entry.Name())
		m, err := loadMap(d, entry.Name(), mapDir)
		if err != nil {
			maps = append(maps, Map{
				ID:              entry.Name(),
				Dir:             mapDir,
				Status:          MapMalformed,
				Malformed:       true,
				MalformedReason: err.Error(),
			})
			continue
		}
		m.Archived = archived[m.ID]
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

	tickets, err := loadMapTickets(d, dir)
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
	}, nil
}

// loadMapTickets prefers the manifest and folds a Map that has none — index.json
// is the single source of a ticket's status, type and blocking wherever it exists,
// and the fold is what makes it exist for Maps charted before the manifest.
func loadMapTickets(d *Deps, dir string) ([]Ticket, error) {
	manifest, err := LoadMapManifest(d, dir)
	if err == nil {
		if !manifest.Valid {
			return nil, fmt.Errorf("%s: %s", MapManifestFileName, manifest.MalformedReason())
		}
		return manifest.ToTickets(), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return foldMapManifest(d, dir)
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
