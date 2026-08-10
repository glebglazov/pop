package wayfinder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work/ref"
)

// legacyArchiveStateFile is the retired per-repository side-file that listed
// archived map ids. Archival is now the Work registry's `archived` bit, so a Map
// is hidden through the same mechanism a Task set is; the file folds into that
// bit on the first read that finds it.
const legacyArchiveStateFile = "wayfinder-archive.json"

type legacyArchiveState struct {
	Archived []string `json:"archived"`
}

// ArchiveResult is the outcome of toggling one map's archived flag.
type ArchiveResult struct {
	MapID    string
	Archived bool
}

// MapRef names one Map in the cross-kind Work registry, so a Map's registration
// row is addressed the same way every other kind's is.
func MapRef(mapID string) ref.WorkRef {
	return ref.WorkRef{Kind: ref.KindMap, ContainerID: mapID}
}

// mapRegistryFacts returns every registered Map's registry row, keyed by id —
// the archived bit and the Mute (ADR-0200 decision 7) alike, both cross-kind
// registration facts read the one way every kind reads them. It answers for the
// whole machine rather than one storage, so a load walking many of them reads it
// once; the per-storage fold of the retired side-file is its caller's, and must
// already have run. A machine with no pop.db has registered nothing, so a
// missing store is an empty map rather than an error — a pure read never
// materialises the database.
func mapRegistryFacts(d *Deps) (map[string]store.WorkContainer, error) {
	out := map[string]store.WorkContainer{}
	s, ok, err := d.taskDeps().Store(false)
	if err != nil || !ok {
		return out, err
	}
	rows, err := s.WorkContainersOfKind(ref.KindMap)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.Ref.ContainerID] = row
	}
	return out, nil
}

// foldLegacyArchiveState moves the retired side-file's ids onto the registry's
// archived bit and deletes the file. It registers what it archives: the bit only
// exists on a registry row, and a Map filed away before Maps registered has none,
// so folding without registering would silently restore it to the default views.
// The file goes last, so an interrupted fold re-runs from a file that still names
// every archived Map.
func foldLegacyArchiveState(d *Deps, storageDir string) error {
	path := filepath.Join(storageDir, legacyArchiveStateFile)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var state legacyArchiveState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("read %s: %w", legacyArchiveStateFile, err)
	}
	var s *store.Store
	for _, id := range state.Archived {
		if id == "" {
			continue
		}
		if s == nil {
			if s, err = openWorkRegistry(d); err != nil {
				return err
			}
		}
		if err := s.RegisterWorkContainer(MapRef(id), time.Now().UTC()); err != nil {
			return err
		}
		if err := s.ArchiveWorkContainer(MapRef(id)); err != nil {
			return err
		}
	}
	return d.FS.RemoveAll(path)
}

// openWorkRegistry resolves the machine-global execution-state store for a write.
// A Map's registration row and its archived bit live there beside a Task set's.
func openWorkRegistry(d *Deps) (*store.Store, error) {
	s, _, err := d.taskDeps().Store(true)
	return s, err
}

// ArchiveMap marks one map as archived. The operation is idempotent.
func ArchiveMap(d *Deps, cwd, mapID string) (*ArchiveResult, error) {
	return setMapArchived(d, cwd, mapID, true)
}

// UnarchiveMap clears one map's archived flag.
func UnarchiveMap(d *Deps, cwd, mapID string) (*ArchiveResult, error) {
	return setMapArchived(d, cwd, mapID, false)
}

func setMapArchived(d *Deps, cwd, mapID string, archived bool) (*ArchiveResult, error) {
	m, err := FindMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	s, err := openWorkRegistry(d)
	if err != nil {
		return nil, err
	}
	row, registered, err := s.FindWorkContainer(MapRef(m.ID))
	if err != nil {
		return nil, err
	}
	// The archived bit rides a registration, so archival needs one. Creating it
	// here would be exactly the hidden second registration path that RegisterMap
	// exists to be the only alternative to.
	if !registered {
		return nil, fmt.Errorf("map %q is not registered; run `pop map register %s` first", m.ID, m.ID)
	}
	if row.Archived == archived {
		if !archived {
			return nil, fmt.Errorf("map %q is not archived", m.ID)
		}
		return &ArchiveResult{MapID: m.ID, Archived: true}, nil
	}
	if archived {
		err = s.ArchiveWorkContainer(MapRef(m.ID))
	} else {
		err = s.UnarchiveWorkContainer(MapRef(m.ID))
	}
	if err != nil {
		return nil, err
	}
	return &ArchiveResult{MapID: m.ID, Archived: archived}, nil
}
