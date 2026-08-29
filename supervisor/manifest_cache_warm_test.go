package supervisor

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
)

// The supervisor warms the persisted tier of the Manifest memo as it ticks
// (ADR-0243 decision 3), so the surface a human waits on — a first dashboard
// paint, a `pop work status` — opens onto validation the daemon already did. A
// cache warmed only by the surface paying the cost would help the second open,
// not the first.
//
// Nothing in the supervisor opts in: LoadManifest writes the tier for whoever
// calls it, and the daemon holds one *tasks.Deps — one cache handle — for its
// whole life. What these tests pin is that this stays true through the daemon's
// wiring, which deliberately opts *out* of the Git fact memo, and that the
// process tier's bound does not quietly turn the persisted tier into dead code.

// cacheWarmDeps builds the daemon a real repo with one registered, drainable set
// and points the machine-local cache at a throwaway directory. It returns the
// supervisor's deps, the config it ticks over, the set's definition directory and
// the cache database path.
func cacheWarmDeps(t *testing.T, stem string, rows []queuetest.SpawnTask) (*drain.Deps, *config.Config, string, string) {
	t.Helper()
	repo, setID, _ := queuetest.SetupSpawnRepo(t, stem, rows)
	// The cache directory is the one env var SetupSpawnRepo does not isolate; left
	// alone, cacheDBAllowed refuses the developer's real cache and every read here
	// would miss for the wrong reason.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	td := queuetest.TasksDeps(t, true)
	t.Cleanup(func() { _ = td.CloseCacheDB() })
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       queuetest.NewRecordingTmux(false, "0"),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	// Symlinks evaluated, because the persisted tier is keyed by path and the
	// daemon's own resolution records the real one — a temp dir on macOS is
	// reached through /var, which is a link to /private/var.
	setDir, err := filepath.EvalSymlinks(filepath.Join(id.TasksDir, stem))
	if err != nil {
		t.Fatalf("resolve the set directory: %v", err)
	}
	return d, cfg, setDir, tasks.CacheDBPathWith(td)
}

// openCache opens the cache database the way an outside observer would: its own
// connection, so what it reads is what landed on disk rather than what a handle
// in this process is holding.
func openCache(t *testing.T, cachePath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+cachePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open the cache database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// manifestRow reads dir's persisted entry: the content key it was written under
// and the manifest payload.
func manifestRow(t *testing.T, cachePath, dir string) (string, []byte, bool) {
	t.Helper()
	var key string
	var payload []byte
	err := openCache(t, cachePath).QueryRow(
		`SELECT content_key, manifest FROM manifest_entries WHERE dir = ?`, dir,
	).Scan(&key, &payload)
	if err == sql.ErrNoRows {
		return "", nil, false
	}
	if err != nil {
		t.Fatalf("read the persisted entry for %s: %v", dir, err)
	}
	return key, payload, true
}

// evictProcessTier walks more throwaway set directories than the in-process tier
// holds, so the entries a tick left in it are gone. This is not a test-only
// switch: it is the daemon's own bound, and walking past it is exactly what
// stops the memo growing for as long as the daemon lives. A fresh dashboard's
// tier is cold for the simpler reason that it is a fresh process.
func evictProcessTier(t *testing.T, d *tasks.Deps, root string) {
	t.Helper()
	// More than manifestMemoCapacity (512). Every fill set is keyed and memoized
	// even though it is malformed — an empty tasks array is a validated answer.
	const fill = 560
	for i := 0; i < fill; i++ {
		dir := filepath.Join(root, fmt.Sprintf("set-%04d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, tasks.ManifestFileName)
		if err := os.WriteFile(path, []byte(`{"tasks":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		tasks.LoadManifest(d, filepath.Base(dir), path)
	}
}

// countingMarkdownFS counts the task markdown reads under one set directory —
// the read a served manifest must not perform.
type countingMarkdownFS struct {
	deps.FileSystem
	setDir string
	mu     sync.Mutex
	reads  int
}

func (c *countingMarkdownFS) ReadFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, c.setDir) && strings.HasSuffix(path, ".md") {
		c.mu.Lock()
		c.reads++
		c.mu.Unlock()
	}
	return c.FileSystem.ReadFile(path)
}

func (c *countingMarkdownFS) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// laterProcessDeps is what a dashboard opened after the daemon has ticked has:
// the same machine, its own cache handle, and a filesystem that reports what it
// reads.
func laterProcessDeps(t *testing.T, setDir string) (*tasks.Deps, *countingMarkdownFS) {
	t.Helper()
	td := queuetest.TasksDeps(t, true)
	t.Cleanup(func() { _ = td.CloseCacheDB() })
	counting := &countingMarkdownFS{FileSystem: td.FS, setDir: setDir}
	td.FS = counting
	return td, counting
}

// TestSupervisorTickWarmsThePersistedManifestTier is the plain claim: a tick
// leaves the set's validated manifest on disk, under the key the directory
// carries right now.
func TestSupervisorTickWarmsThePersistedManifestTier(t *testing.T) {
	d, _, setDir, cachePath := cacheWarmDeps(t, "warm-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	key, payload, ok := manifestRow(t, cachePath, setDir)
	if !ok {
		t.Fatalf("a tick left no persisted entry for %s; the daemon warms nothing", setDir)
	}
	if key == "" || len(payload) == 0 {
		t.Fatalf("persisted entry is empty: key=%q payload=%d bytes", key, len(payload))
	}
	for _, want := range []string{`"01-a"`, `"02-b"`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("persisted manifest missing %s:\n%s", want, payload)
		}
	}
}

// TestDashboardServesTheManifestTheSupervisorWarmed is the point of the whole
// decision: the first open pays nothing the daemon already paid. It is checked
// both ways round — with the daemon's row, no task markdown is opened; with that
// row deleted, the same load opens it — because a passing zero would otherwise
// prove nothing about the persisted tier if the process tier had answered.
func TestDashboardServesTheManifestTheSupervisorWarmed(t *testing.T) {
	d, _, setDir, cachePath := cacheWarmDeps(t, "served-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	manifestPath := filepath.Join(setDir, tasks.ManifestFileName)

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())
	if _, _, ok := manifestRow(t, cachePath, setDir); !ok {
		t.Fatalf("a tick left no persisted entry for %s", setDir)
	}

	fillRoot := t.TempDir()
	evictProcessTier(t, d.Tasks, filepath.Join(fillRoot, "a"))

	dash, counting := laterProcessDeps(t, setDir)
	served := tasks.LoadManifest(dash, "served-set", manifestPath)
	if !served.Valid {
		t.Fatalf("served manifest invalid: %v", served.Errors)
	}
	if len(served.Tasks) != 2 || served.Tasks[0].ID != "01-a" || served.Tasks[1].ID != "02-b" {
		t.Fatalf("served manifest is not the set the daemon validated: %#v", served.Tasks)
	}
	if got := counting.count(); got != 0 {
		t.Fatalf("markdown reads on the dashboard's first load = %d, want 0 — the daemon's row must serve it", got)
	}

	// The control: without the row, the same load re-reads the markdown. If this
	// fails, the process tier answered above and the zero meant nothing.
	if _, err := openCache(t, cachePath).Exec(`DELETE FROM manifest_entries WHERE dir = ?`, setDir); err != nil {
		t.Fatalf("delete the persisted entry: %v", err)
	}
	evictProcessTier(t, d.Tasks, filepath.Join(fillRoot, "b"))
	cold, coldCounting := laterProcessDeps(t, setDir)
	if !tasks.LoadManifest(cold, "served-set", manifestPath).Valid {
		t.Fatal("cold load invalid")
	}
	if coldCounting.count() == 0 {
		t.Fatal("a load with no persisted entry opened no markdown: the process tier is still serving, so this test proves nothing")
	}
}

// TestTickWritesMoveTheSetsPersistedEntry covers what the supervisor's Git fact
// memo opt-out exists for and this tier does not need: a tick's own writes.
// Manifest validation is a pure function of files, so a rewritten set directory
// moves the content key honestly and the next serve re-validates against it —
// the row can describe a set as it was, but it can never be served for one.
func TestTickWritesMoveTheSetsPersistedEntry(t *testing.T) {
	rows := []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	}
	d, _, setDir, cachePath := cacheWarmDeps(t, "rewritten-set", rows)

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())
	before, beforePayload, ok := manifestRow(t, cachePath, setDir)
	if !ok {
		t.Fatalf("a tick left no persisted entry for %s", setDir)
	}
	if strings.Contains(string(beforePayload), `"done"`) {
		t.Fatalf("the first tick's row already says done:\n%s", beforePayload)
	}

	// What a drain's status transition writes into the set directory, on the same
	// machine, while the daemon keeps ticking.
	rows[0].Status = "done"
	queuetest.WriteSpawnManifest(t, setDir, rows)

	tick(d, &out, newRunOutputState())
	after, afterPayload, ok := manifestRow(t, cachePath, setDir)
	if !ok {
		t.Fatalf("the second tick left no persisted entry for %s", setDir)
	}
	if after == before {
		t.Fatalf("content key unchanged across a write to the set directory: %s", after)
	}
	if !strings.Contains(string(afterPayload), `"done"`) {
		t.Fatalf("persisted manifest does not carry the tick-time write:\n%s", afterPayload)
	}

	var rowsForDir int
	if err := openCache(t, cachePath).QueryRow(
		`SELECT count(*) FROM manifest_entries WHERE dir = ?`, setDir,
	).Scan(&rowsForDir); err != nil {
		t.Fatalf("count the rows for %s: %v", setDir, err)
	}
	if rowsForDir != 1 {
		t.Fatalf("rows for %s = %d, want 1 — the table is keyed by path, not by edit history", setDir, rowsForDir)
	}
}

// TestDaemonAndDashboardWriteTheCacheConcurrently pins the concurrency the
// design expects rather than avoids: two processes writing one cache file. The
// write-ahead log and the busy timeout carry it, a lost race is dropped, and no
// surface is ever handed an error about a cache.
func TestDaemonAndDashboardWriteTheCacheConcurrently(t *testing.T) {
	d, _, setDir, cachePath := cacheWarmDeps(t, "contended-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	manifestPath := filepath.Join(setDir, tasks.ManifestFileName)
	dash, _ := laterProcessDeps(t, setDir)

	var wg sync.WaitGroup
	var daemonOut bytes.Buffer
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 4; i++ {
			tick(d, &daemonOut, newRunOutputState())
		}
	}()
	invalid := 0
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			if !tasks.LoadManifest(dash, "contended-set", manifestPath).Valid {
				invalid++
			}
		}
	}()
	wg.Wait()

	if invalid != 0 {
		t.Fatalf("%d of the dashboard's loads came back invalid while the daemon wrote the cache", invalid)
	}
	if got := daemonOut.String(); strings.Contains(got, "cache") || strings.Contains(got, "database") {
		t.Fatalf("the daemon reported a cache problem to its operator:\n%s", got)
	}
	var integrity string
	if err := openCache(t, cachePath).QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("cache integrity after contention = %q", integrity)
	}
	if _, _, ok := manifestRow(t, cachePath, setDir); !ok {
		t.Fatalf("no persisted entry survived the contention for %s", setDir)
	}
}

// TestTickRewarmsACacheDeletedUnderTheDaemon is why the tier is written on a
// process-tier hit and not only on a miss. The daemon holds its in-process tier
// for days, so a set it validated on its first tick is never re-derived; if
// offering it to the cache happened only there, `rm cache.db` — a supported
// repair on a running pop — would leave every dashboard opening cold until the
// operator restarted the daemon.
func TestTickRewarmsACacheDeletedUnderTheDaemon(t *testing.T) {
	d, _, setDir, cachePath := cacheWarmDeps(t, "rewarmed-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())
	if _, _, ok := manifestRow(t, cachePath, setDir); !ok {
		t.Fatalf("a tick left no persisted entry for %s", setDir)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(cachePath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("delete %s: %v", cachePath+suffix, err)
		}
	}

	tick(d, &out, newRunOutputState())
	if _, _, ok := manifestRow(t, cachePath, setDir); !ok {
		t.Fatalf("the daemon never re-warmed %s after its cache was deleted", setDir)
	}
}
