package wayfinder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// mapsDirName is the Map container inside a repository's Task storage, sibling of
// tasks/.
const mapsDirName = "maps"

// legacyMapsDirName is the retired name of that container. Every verb calls the
// thing a map, so a store still carrying the old name is folded on the first read.
const legacyMapsDirName = "wayfinder"

// mapsDir returns the Map container inside storageDir, folding a pre-rename
// wayfinder/ directory into maps/ first. Like MigrateStorageLayout it is
// idempotent and never merges: a store that already has maps/ is left alone,
// whatever sits beside it.
func mapsDir(d *Deps, storageDir string) (string, error) {
	root := filepath.Join(storageDir, mapsDirName)
	if dirExists(d, root) {
		return root, nil
	}
	legacy := filepath.Join(storageDir, legacyMapsDirName)
	if !dirExists(d, legacy) {
		return root, nil
	}
	if err := d.FS.Rename(legacy, root); err != nil {
		return "", fmt.Errorf("rename %s to %s: %w", legacy, root, err)
	}
	return root, nil
}

func dirExists(d *Deps, path string) bool {
	_, err := d.FS.ReadDir(path)
	return err == nil
}

// foldMapManifest mints a Map's index.json from the Status: / Type: / Blocked by:
// header lines its tickets still carry, then strips those lines from each ticket
// markdown so a ticket's status has exactly one source. It runs on the ordinary
// read path, guarded by the absence of a manifest, and returns the tickets the
// caller should read this scan from.
//
// The manifest is written before any markdown is touched: an interrupted fold then
// leaves a Map that reads from its manifest with dead headers behind it, never one
// whose statuses were erased along with the headers.
//
// A ticket sitting at claimed drops to open — claims live in pop.db keyed by owner,
// and the file format names none, so a synthesized claim would be a lock nothing
// can release by identity.
//
// A Map whose manifest pop had to synthesize predates registration too, so the
// mint also writes the Map's Work registry row — before the manifest, not after.
// RegisterWorkContainer is idempotent, so a crash between the two leaves a
// registered Map the next scan folds cleanly; the other order would leave a Map
// that never folds again and never registers.
func foldMapManifest(d *Deps, id, dir string) ([]Ticket, error) {
	issuesDir := filepath.Join(dir, issuesDirName)
	names, err := ticketMarkdownNames(d, issuesDir)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	// Nothing to mint from: a Map still being charted keeps the header read path
	// rather than gaining an empty manifest that its first hand-written ticket
	// would immediately contradict.
	if len(names) == 0 {
		return nil, nil
	}

	parsed := make([]Ticket, 0, len(names))
	entries := make([]ManifestTicket, 0, len(names))
	contents := make(map[string]string, len(names))
	var errs []string
	for _, name := range names {
		path := filepath.Join(issuesDir, name)
		data, err := d.FS.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		ticket, err := ParseTicketMarkdown(name, string(data))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		parsed = append(parsed, ticket)
		contents[name] = string(data)

		status := ticket.Status
		if status == TicketClaimed {
			status = TicketOpen
		}
		entries = append(entries, ManifestTicket{
			ID:        ticket.ID,
			File:      name,
			Type:      ticket.Type,
			Status:    status,
			BlockedBy: ticket.BlockedBy,
		})
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	sortTickets(parsed)

	manifest := &MapManifest{
		Dir:         dir,
		Path:        MapManifestPath(dir),
		Tickets:     entries,
		SpawnedSets: []string{},
		Unknown:     map[string]json.RawMessage{},
	}
	validateMapManifest(d, manifest)
	// Headers that do not add up to a valid manifest — a missing Type: line, a
	// blocker naming no ticket — are left where they are. Minting from them would
	// turn a Map that reads today into a MALFORMED one; charting's registration
	// gate is where a human fixes the Map instead.
	if len(manifest.Errors) > 0 {
		return parsed, nil
	}
	manifest.Valid = true

	s, err := openWorkRegistry(d)
	if err != nil {
		return nil, err
	}
	if err := s.RegisterWorkContainer(MapRef(id), time.Now().UTC()); err != nil {
		return nil, err
	}

	if err := WriteMapManifest(d, manifest); err != nil {
		return nil, fmt.Errorf("write %s: %w", MapManifestFileName, err)
	}
	for _, name := range names {
		stripped := StripTicketHeaders(contents[name])
		if stripped == contents[name] {
			continue
		}
		path := filepath.Join(issuesDir, name)
		if err := tasks.WriteAtomicWith(&tasks.Deps{FS: d.FS}, path, []byte(stripped), 0o644); err != nil {
			return nil, fmt.Errorf("strip headers from %s: %w", name, err)
		}
	}
	return manifest.ToTickets(), nil
}

// ticketMarkdownNames lists the ticket markdown filenames under a Map's issues/
// directory, sorted. A missing directory yields no names and no error.
func ticketMarkdownNames(d *Deps, issuesDir string) ([]string, error) {
	entries, err := d.FS.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
