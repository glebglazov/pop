package wayfinder

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// RegisterResult reports the outcome of ending charting on one Map.
type RegisterResult struct {
	MapID string
	// AlreadyRegistered reports that the row was already there. Registration is
	// idempotent so the MALFORMED fix loop can be re-run until it comes back
	// clean without a second run being an error.
	AlreadyRegistered bool
	// Warnings are the manifest's advisory problems. They are reported rather than
	// refused over — registration's fix list is the set of things that stop pop
	// reading the Map, and a draft nothing references does not.
	Warnings []string
}

// MapMalformedError refuses registration and names every problem separately, so
// one run of `pop map register` tells the human the whole fix list rather than
// the first item of it.
type MapMalformedError struct {
	MapID    string
	Problems []string
}

func (e *MapMalformedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "map %q is MALFORMED (%d problem(s)):", e.MapID, len(e.Problems))
	for _, p := range e.Problems {
		fmt.Fprintf(&b, "\n  - %s", p)
	}
	fmt.Fprintf(&b, "\nfix the problems above and re-run `pop map register %s`", e.MapID)
	return b.String()
}

// RegisterMap ends charting: it validates the Map's manifest and, when it is
// clean, writes the Map's Work registry row. Registration is the moment a Map
// becomes Work pop looks after, and it is explicit — no verb registers a Map as
// a side effect, so there is exactly one place a Map can enter the registry.
//
// A Map is always registered plain, never managed: wayfinding writes nothing
// into the repository, so there is no checkout for a worktree to hold.
func RegisterMap(d *Deps, cwd, mapID string) (*RegisterResult, error) {
	m, err := FindMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	problems, warnings := mapRegistrationProblems(d, m)
	if len(problems) > 0 {
		return nil, &MapMalformedError{MapID: m.ID, Problems: problems}
	}
	s, err := openWorkRegistry(d)
	if err != nil {
		return nil, err
	}
	_, already, err := s.FindWorkContainer(MapRef(m.ID))
	if err != nil {
		return nil, err
	}
	if err := s.RegisterWorkContainer(MapRef(m.ID), time.Now().UTC()); err != nil {
		return nil, err
	}
	return &RegisterResult{MapID: m.ID, AlreadyRegistered: already, Warnings: warnings}, nil
}

// mapRegistrationProblems is the registration gate: every consumer downstream of
// charting reads the Map through its manifest, so registration is where a Map
// that cannot be read that way is caught and handed back as a fix list. It runs
// the same validation the load path runs, plus the two conditions only
// registration cares about — a missing manifest and an empty one — and returns
// the manifest's advisory warnings alongside, to be printed rather than refused
// over.
func mapRegistrationProblems(d *Deps, m Map) (problems, warnings []string) {
	if _, ok := tasks.IDCreatedAt(m.ID); !ok {
		problems = append(problems, fmt.Sprintf(
			"%s: id has no YYYY-MM-DD date prefix; a Map id is <YYYY-MM-DD-slug>", m.ID))
	}
	manifest, err := LoadMapManifest(d, m.Dir)
	switch {
	case os.IsNotExist(err):
		return append(problems, fmt.Sprintf(
			"%s: missing; a Map registers from its manifest, so chart its Decision tickets first",
			MapManifestFileName)), nil
	case err != nil:
		return append(problems, fmt.Sprintf("%s: %v", MapManifestFileName, err)), nil
	case !manifest.Valid:
		return append(problems, manifest.Errors...), manifest.Warnings
	}
	// The manifest reads, so anything still rendering the Map BROKEN is a map.md
	// problem — an unreadable file or an unrecognised Status: line.
	if m.Broken {
		return append(problems, m.BrokenReason), manifest.Warnings
	}
	if len(manifest.Tickets) == 0 {
		return append(problems, fmt.Sprintf(
			"%s: no Decision tickets; charting has produced nothing to register",
			MapManifestFileName)), manifest.Warnings
	}
	return problems, manifest.Warnings
}
