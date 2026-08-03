package wayfinder

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// SpawnResult is one recorded handoff: which set the Map now lists, and whether
// this call is what put it there.
type SpawnResult struct {
	MapID string
	SetID string
	// AlreadyRecorded reports that the set was on the manifest before this call.
	// `to-tasks` calls the verb after every registration, including a re-run over
	// a set it already published, so the second call is a no-op and says so.
	AlreadyRecorded bool
	// SpawnedSets is the Map's list after the call, in the order the sets were
	// spawned — the order `## Spawned sets` renders in.
	SpawnedSets []string
}

// RecordSpawnedSet records that a Map handed off to a Task set: it appends the
// set id to the Map manifest's spawned_sets and re-renders map.md's generated
// `Spawned sets` region from it.
//
// This is the whole of pop's lineage model. Only one relationship between Work
// containers is ever traversed by a human — Map to the sets it spawned — it runs
// one way, and a one-way relationship owned by one side is a field on that side,
// so there is no edge table and no edge type to reason about. The write lives
// here rather than behind a `pop tasks register` flag because the storage being
// written is a Map's, and `tasks` must not learn wayfinder's layout.
//
// The id is recorded bare. A set that later moves, is archived or is deleted
// still reads back as what this Map spawned, and rendering resolves its live
// status fresh; caching a title or a status here would be a second copy of
// another file's truth.
func RecordSpawnedSet(d *Deps, cwd, mapID, setID string) (*SpawnResult, error) {
	id := strings.TrimSpace(setID)
	if id == "" {
		return nil, errors.New("recording a spawned set needs a task-set id")
	}
	if strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("invalid task-set id %q: name the set's folder, not a path", setID)
	}
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}

	var result *SpawnResult
	err = withMapLock(d, m.ID, func() error {
		var inner error
		result, inner = writeSpawnedSet(d, m, id)
		return inner
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// writeSpawnedSet appends under the Map's lock, re-reading the manifest inside it
// so a concurrent resolve's entry is never dropped. It re-renders map.md even
// when the id was already listed: the section is generated, so a re-run is also
// how a hand-edited one is repaired.
func writeSpawnedSet(d *Deps, m Map, setID string) (*SpawnResult, error) {
	manifest, err := LoadMapManifest(d, m.Dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("map %q has no %s; run `pop map register %s` first", m.ID, MapManifestFileName, m.ID)
	}
	if err != nil {
		return nil, err
	}
	if !manifest.Valid {
		return nil, fmt.Errorf("map %q is MALFORMED: %s", m.ID, manifest.MalformedReason())
	}

	already := false
	for _, existing := range manifest.SpawnedSets {
		if existing == setID {
			already = true
			break
		}
	}
	if !already {
		manifest.SpawnedSets = append(manifest.SpawnedSets, setID)
		if err := WriteMapManifest(d, manifest); err != nil {
			return nil, err
		}
	}
	if err := renderMapIndex(d, m, manifest); err != nil {
		return nil, err
	}
	return &SpawnResult{
		MapID:           m.ID,
		SetID:           setID,
		AlreadyRecorded: already,
		SpawnedSets:     append([]string{}, manifest.SpawnedSets...),
	}, nil
}
