package drain

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// countingConfigFS records how often each repository's git config file is read.
// That file is the expensive half of the repository-shape probe behind
// configured-path expansion — the read whose repetition within one load ADR-0189
// records as a tier-one defect.
type countingConfigFS struct {
	deps.FileSystem
	mu    sync.Mutex
	reads map[string]int
}

func newCountingConfigFS(inner deps.FileSystem) *countingConfigFS {
	return &countingConfigFS{FileSystem: inner, reads: map[string]int{}}
}

func (fs *countingConfigFS) ReadFile(path string) ([]byte, error) {
	if base := filepath.Base(path); base == "config" && strings.Contains(path, ".git") {
		fs.mu.Lock()
		fs.reads[filepath.Clean(path)]++
		fs.mu.Unlock()
	}
	return fs.FileSystem.ReadFile(path)
}

func (fs *countingConfigFS) counts() map[string]int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make(map[string]int, len(fs.reads))
	for k, n := range fs.reads {
		out[k] = n
	}
	return out
}

func (fs *countingConfigFS) total() int {
	n := 0
	for _, c := range fs.counts() {
		n += c
	}
	return n
}

func (fs *countingConfigFS) reset() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.reads = map[string]int{}
}

// shapeMemoFixture is gitMemoFixture with the project filesystem counting git
// config reads, so a real two-kind page-A load can be billed for its
// repository-shape probes.
func shapeMemoFixture(t *testing.T) (*Deps, *config.Config, *countingConfigFS) {
	t.Helper()
	d, cfg, _, _ := gitMemoFixture(t)
	fs := newCountingConfigFS(d.Project.FS)
	pd := *d.Project
	pd.FS = fs
	d.Project = &pd
	return d, cfg, fs
}

// TestWorkLoadReadsRepoShapeOncePerLoad pins the tier-one fix: one page-A load
// resolves its repo groups once, so each configured repository's git config is
// read at most once for the whole load however many kinds the load lists. The
// wiring list here is the real one — the Task-set kind and the Map kind — because
// the defect was precisely that each kind's Load re-expanded every configured
// path from scratch.
func TestWorkLoadReadsRepoShapeOncePerLoad(t *testing.T) {
	d, cfg, fs := shapeMemoFixture(t)
	// Each kind resolves the repository groups for itself here — the shape the
	// defect had, and the shape a kind still takes whenever it is wired without
	// the build's shared group seam. What must hold is the load's bill, not which
	// caller happens to resolve first.
	d.Kinds = func(load *Deps, cfg *config.Config) []work.Kind {
		return []work.Kind{
			load.TaskSetKind(cfg, nil),
			wayfinder.NewMapKind(load.MapKindDeps(cfg, nil)),
		}
	}

	kinds := d.WorkKinds(cfg)
	if len(kinds) < 2 {
		t.Fatalf("wiring list has %d kinds, want more than one", len(kinds))
	}
	if _, err := work.BuildSnapshot(kinds); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	for path, n := range fs.counts() {
		if n > 1 {
			t.Fatalf("load read %s %d times, want at most once: %v", path, n, fs.counts())
		}
	}
	if fs.total() == 0 {
		t.Fatal("load read no git config: the fixture no longer probes repository shape through this seam")
	}
}

// TestStatusVerbProbesRepoShapeOnce pins the multi-builder half of the fix.
// `pop work status` is one load across two builders — the status snapshot and the
// project scan — each of which scopes a load of its own inside the verb's. The
// verb therefore paid for the repository-shape probe once per builder; with the
// memo threaded through the nesting it pays once in total.
func TestStatusVerbProbesRepoShapeOnce(t *testing.T) {
	d, cfg, fs := shapeMemoFixture(t)

	load := d.WithGitMemo()
	if _, err := BuildStatus(load, cfg); err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if _, err := Scan(load, cfg); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for path, n := range fs.counts() {
		if n > 1 {
			t.Fatalf("the status verb read %s %d times, want at most once: %v", path, n, fs.counts())
		}
	}
	if fs.total() == 0 {
		t.Fatal("the verb read no git config: the fixture no longer probes repository shape through this seam")
	}
}

// TestRepoShapeMemoLastsOneLoad pins the memo's lifetime, which is a correctness
// requirement rather than a tuning choice: the answer names which projects exist
// and which repositories are bare, both of which can change, so a second load
// must probe again rather than replay the first load's picture.
func TestRepoShapeMemoLastsOneLoad(t *testing.T) {
	d, cfg, fs := shapeMemoFixture(t)

	if _, err := work.BuildSnapshot(d.WorkKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	first := fs.total()
	if first == 0 {
		t.Fatal("load read no git config: the fixture no longer probes repository shape through this seam")
	}

	fs.reset()
	if _, err := work.BuildSnapshot(d.WorkKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot (second load): %v", err)
	}
	if got := fs.total(); got != first {
		t.Fatalf("second load read git config %d times, first read %d: the memo outlived its load", got, first)
	}
}

// TestShapeMemoIsNotSharedAcrossLoads pins the same lifetime at the seam that
// owns the scope: each load gets its own memo instance, so nothing a caller keeps
// hold of can serve a previous load's answer.
func TestShapeMemoIsNotSharedAcrossLoads(t *testing.T) {
	d := &Deps{Project: &project.Deps{Git: deps.NewRealGit(), FS: deps.NewRealFileSystem()}}
	first, second := d.WithGitMemo().Project.Shape, d.WithGitMemo().Project.Shape
	if first == nil || second == nil {
		t.Fatal("a load was scoped no shape memo")
	}
	if first == second {
		t.Fatal("two loads share one shape memo")
	}
	if d.Project.Shape != nil {
		t.Fatal("scoping a load's memo mutated the caller's own deps")
	}
}
