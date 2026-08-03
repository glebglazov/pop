package wayfinder

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	spawnMapID        = "2026-08-03-spawning"
	unregisteredMapID = "2026-08-03-charting"
)

// spawnMapMarkdown carries a hand-written line under `## Spawned sets` — the
// convention as a skill used to write it, before pop owned the section.
const spawnMapMarkdown = `Status: active

## Destination

Ship the thing.

## Spawned sets

- 2026-01-01-hand-written
`

func spawnFixture(t *testing.T) (*Deps, string) {
	t.Helper()
	dir := "maps/" + spawnMapID + "/"
	d, storageDir := registryFixture(t, map[string]string{
		dir + "map.md":             spawnMapMarkdown,
		dir + "issues/01-first.md": "## Question\n\nWhich database?\n",
		dir + "index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"Database","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":["2026-08-04-first-handoff"]}`,
		// A Map still being charted: it has a manifest, but no registry row.
		"maps/" + unregisteredMapID + "/map.md":             "Status: active\n\n## Destination\n\nStill charting.\n",
		"maps/" + unregisteredMapID + "/issues/01-first.md": "## Question\n\nWhich client?\n",
		"maps/" + unregisteredMapID + "/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"Client","type":"research","status":"open","blocked_by":[]}]}`,
	})
	mustRegister(t, d, spawnMapID)
	asWindow(d, "pane:%1", at(9))
	return d, storageDir
}

func spawnedSets(t *testing.T, d *Deps, mapDir string) []string {
	t.Helper()
	manifest, err := LoadMapManifest(d, mapDir)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Valid {
		t.Fatalf("manifest is invalid: %s", manifest.MalformedReason())
	}
	return manifest.SpawnedSets
}

// TestRecordSpawnedSetAppendsAndGeneratesTheSection is the verb end to end: the
// id lands on the manifest after the sets already there, `## Spawned sets` is
// rebuilt from the manifest — taking the hand-written line with it — and a second
// call is a no-op that says so.
func TestRecordSpawnedSetAppendsAndGeneratesTheSection(t *testing.T) {
	t.Parallel()
	d, storageDir := spawnFixture(t)
	mapDir := filepath.Join(storageDir, "maps", spawnMapID)

	result, err := RecordSpawnedSet(d, "", spawnMapID, "2026-08-05-second-handoff")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if result.AlreadyRecorded {
		t.Fatalf("first record reported an existing entry: %+v", result)
	}
	want := []string{"2026-08-04-first-handoff", "2026-08-05-second-handoff"}
	if got := spawnedSets(t, d, mapDir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spawned_sets = %v, want %v", got, want)
	}

	mapMD := readFile(t, filepath.Join(mapDir, "map.md"))
	for _, line := range []string{
		"<!-- pop:generated spawned-sets -->",
		"- 2026-08-04-first-handoff",
		"- 2026-08-05-second-handoff",
		"<!-- /pop:generated spawned-sets -->",
	} {
		if !strings.Contains(mapMD, line) {
			t.Fatalf("map.md missing %q:\n%s", line, mapMD)
		}
	}
	if strings.Contains(mapMD, "hand-written") {
		t.Fatalf("the hand-written section survived the generated rewrite:\n%s", mapMD)
	}

	again, err := RecordSpawnedSet(d, "", spawnMapID, "2026-08-05-second-handoff")
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if !again.AlreadyRecorded || len(again.SpawnedSets) != 2 {
		t.Fatalf("re-recording the same set = %+v, want an idempotent no-op", again)
	}
	if got := strings.Count(readFile(t, filepath.Join(mapDir, "map.md")), "- 2026-08-05-second-handoff"); got != 1 {
		t.Fatalf("the set renders %d times, want once", got)
	}
}

// TestRecordSpawnedSetSurvivesAResolve pins the two writers of map.md against
// each other: resolving a ticket rebuilds every generated region from the
// manifest, so the lineage recorded earlier has to come back with it.
func TestRecordSpawnedSetSurvivesAResolve(t *testing.T) {
	t.Parallel()
	d, storageDir := spawnFixture(t)
	mapDir := filepath.Join(storageDir, "maps", spawnMapID)

	if _, err := RecordSpawnedSet(d, "", spawnMapID, "2026-08-05-second-handoff"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID:      spawnMapID,
		Ticket:     "01",
		AnswerFile: answerFile(t, "Postgres, because the data is relational.\n"),
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	mapMD := readFile(t, filepath.Join(mapDir, "map.md"))
	for _, line := range []string{
		"- [Database](issues/01-first.md) — Postgres, because the data is relational.",
		"- 2026-08-05-second-handoff",
	} {
		if !strings.Contains(mapMD, line) {
			t.Fatalf("map.md missing %q after a resolve:\n%s", line, mapMD)
		}
	}
	if got := spawnedSets(t, d, mapDir); len(got) != 2 {
		t.Fatalf("spawned_sets after a resolve = %v, want both entries", got)
	}
}

// TestRecordSpawnedSetRefusesWithoutWriting covers the three ways the verb says
// no. Each refusal has to leave the Map byte-identical: a half-recorded handoff
// is worse than none, because the section is generated from what it wrote.
func TestRecordSpawnedSetRefusesWithoutWriting(t *testing.T) {
	t.Parallel()
	d, storageDir := spawnFixture(t)
	mapDir := filepath.Join(storageDir, "maps", spawnMapID)
	before := readFile(t, filepath.Join(mapDir, "index.json")) + readFile(t, filepath.Join(mapDir, "map.md"))

	for _, tc := range []struct {
		name  string
		mapID string
		setID string
		want  string
	}{
		{"empty id", spawnMapID, "  ", "needs a task-set id"},
		{"a path, not an id", spawnMapID, "tasks/2026-08-05-set", "name the set's folder"},
		{"unregistered map", unregisteredMapID, "2026-08-05-set", "is not registered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RecordSpawnedSet(d, "", tc.mapID, tc.setID)
			if err == nil {
				t.Fatalf("recording %q on %q was accepted", tc.setID, tc.mapID)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}

	after := readFile(t, filepath.Join(mapDir, "index.json")) + readFile(t, filepath.Join(mapDir, "map.md"))
	if after != before {
		t.Fatalf("a refused record wrote to the map:\n%s", after)
	}
}
