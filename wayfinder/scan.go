package wayfinder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

const mapFileName = "map.md"
const issuesDirName = "issues"

// ScanMaps lists maps non-recursively under <task-storage-root>/wayfinder/*/.
// A missing wayfinder directory yields an empty slice, never an error.
// Unparseable map folders are returned as malformed rows rather than failing the scan.
func ScanMaps(d *Deps, cwd string) ([]Map, error) {
	id, err := tasks.ResolveRepositoryIdentity(d.taskDeps(), cwd)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(id.StorageDir, "wayfinder")
	entries, err := d.FS.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	archived, err := LoadArchivedMapIDs(d, id.StorageDir)
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

	tickets, ticketErrs := loadTickets(d, filepath.Join(dir, issuesDirName))
	if len(ticketErrs) > 0 {
		return Map{}, fmt.Errorf("%s", strings.Join(ticketErrs, "; "))
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

func loadTickets(d *Deps, issuesDir string) ([]Ticket, []string) {
	entries, err := d.FS.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("list issues: %v", err)}
	}

	var tickets []Ticket
	var errs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(issuesDir, entry.Name())
		data, err := d.FS.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}
		ticket, err := ParseTicketMarkdown(entry.Name(), string(data))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}
		tickets = append(tickets, ticket)
	}
	sort.Slice(tickets, func(i, j int) bool {
		if tickets[i].Number != tickets[j].Number {
			return tickets[i].Number < tickets[j].Number
		}
		return tickets[i].ID < tickets[j].ID
	})
	return tickets, errs
}
