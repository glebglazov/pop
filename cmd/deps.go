package cmd

import (
	"bytes"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// Deps is the cmd-layer seam for explicit working-directory and env routing
// (ADR-0145). Production wires real cwd and process env at the edge; tests
// inject a temp dir and fake FileSystem without t.Setenv or os.Chdir.
type Deps struct {
	// Dir is the explicit working directory for verb entrypoints. Empty means
	// domain calls fall back to FS.Getwd (production behavior).
	Dir string

	// FS routes XDG_DATA_HOME / XDG_CONFIG_HOME for cmd-local path resolution
	// and is shared into bundled sub-deps when they are unset.
	FS deps.FileSystem

	Tasks     *tasks.Deps
	Config    *config.Deps
	Project   *project.Deps
	Queue     *drain.Deps
	Routine   *routine.Deps
	Wayfinder *wayfinder.Deps
}

// cmdLayerDepsLocal holds per-goroutine test overrides so parallel cmd tests can
// inject Dir and FS without racing on a package-global (ADR-0145).
var cmdLayerDepsLocal sync.Map // uint64 → *Deps

// cmdLayerDepsGroups maps a root test name to deps for t.Run subtests, which
// execute on a different goroutine than the parent's setCmdLayerDeps call.
var cmdLayerDepsGroups sync.Map // string → *Deps

func goroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i >= 0 {
		b = b[:i]
	}
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}

func rootTestName(t *testing.T) string {
	name := t.Name()
	if i := strings.Index(name, "/"); i >= 0 {
		return name[:i]
	}
	return name
}

// rootTestNameFromStack extracts the root TestXxx name from the call stack so
// t.Run subtests can inherit cmd-layer deps registered on the parent test.
func rootTestNameFromStack() string {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if idx := strings.LastIndex(frame.Function, ".Test"); idx >= 0 {
			rest := frame.Function[idx+1:]
			if dot := strings.Index(rest, "."); dot >= 0 {
				return rest[:dot]
			}
			return rest
		}
		if !more {
			break
		}
	}
	return ""
}

// cmdLayerDeps is the production edge for verb entrypoints. Tests override it
// per goroutine via setCmdLayerDeps without mutating process-global state.
func cmdLayerDeps() *Deps {
	if v, ok := cmdLayerDepsLocal.Load(goroutineID()); ok {
		return v.(*Deps)
	}
	if root := rootTestNameFromStack(); root != "" {
		if v, ok := cmdLayerDepsGroups.Load(root); ok {
			return v.(*Deps)
		}
	}
	return DefaultDeps()
}

// DefaultDeps returns cmd-layer dependencies wired to real implementations.
func DefaultDeps() *Deps {
	fs := deps.NewRealFileSystem()
	return &Deps{
		FS:        fs,
		Tasks:     tasks.DefaultDeps(),
		Config:    config.DefaultDeps(),
		Project:   project.DefaultDeps(),
		Queue:     drain.DefaultDeps(),
		Routine:   routine.DefaultDeps(),
		Wayfinder: wayfinder.DefaultDeps(),
	}
}

// FileSystem returns the deps filesystem seam, defaulting to the real FS.
func (d *Deps) FileSystem() deps.FileSystem {
	if d == nil || d.FS == nil {
		return deps.NewRealFileSystem()
	}
	return d.FS
}

// DirOrGetwd returns the explicit working dir when set, otherwise FS.Getwd.
func (d *Deps) DirOrGetwd() (string, error) {
	if d != nil && d.Dir != "" {
		return d.Dir, nil
	}
	return d.FileSystem().Getwd()
}

// WorkDir returns the explicit working dir when set, or "" so domain helpers
// keep their existing "" → Getwd fallback.
func (d *Deps) WorkDir() string {
	if d == nil {
		return ""
	}
	return d.Dir
}

func (d *Deps) tasksDeps() *tasks.Deps {
	if d != nil && d.Tasks != nil {
		return d.Tasks
	}
	td := tasks.DefaultDeps()
	if d != nil && d.FS != nil {
		td.FS = d.FS
	}
	return td
}

func (d *Deps) configDeps() *config.Deps {
	if d != nil && d.Config != nil {
		return d.Config
	}
	cd := config.DefaultDeps()
	if d != nil && d.FS != nil {
		cd.FS = d.FS
	}
	return cd
}

func (d *Deps) projectDeps() *project.Deps {
	if d != nil && d.Project != nil {
		return d.Project
	}
	return project.DefaultDeps()
}

func (d *Deps) queueDeps() *drain.Deps {
	qd := d.Queue
	if d == nil || qd == nil {
		qd = drain.DefaultDeps()
		if d != nil && d.FS != nil {
			if qd.Tasks != nil {
				qd.Tasks.FS = d.FS
			}
		}
	}
	if qd.Kinds == nil {
		qd.Kinds = workKinds()
	}
	return qd
}

// workKinds is the Work-kind wiring list: which kinds a read surface sees, in
// what order, each constructed with its own dependencies captured. It lives at
// the CLI edge on purpose — `work` defines the seam and imports no kind, so
// something has to name them, and an explicit list here is the accepted cost of
// keeping the seam free of a per-kind import (ADR-0173). Adding a kind is one
// entry here plus its adapter.
// The list is a function of the deps it is handed, not of the deps that
// installed it: a load hands it a git seam memoized for that load (ADR-0060's
// fork budget), and a captured pointer would read past it.
func workKinds() func(qd *drain.Deps, cfg *config.Config) []work.Kind {
	return func(qd *drain.Deps, cfg *config.Config) []work.Kind {
		// One repository-group resolution, shared by every kind of this build.
		groups := qd.RepoGroups(cfg)
		return []work.Kind{
			// The Task-set entry is built through queue because it wears the advance
			// seam as well as the read one: the supervisor drives whatever list it is
			// handed, so the list must not hand it a task-set kind that cannot advance.
			qd.TaskSetKind(cfg, groups),
			wayfinder.NewMapKind(qd.MapKindDeps(cfg, groups)),
		}
	}
}

func (d *Deps) routineDeps() *routine.Deps {
	if d != nil && d.Routine != nil {
		return d.Routine
	}
	rd := routine.DefaultDeps()
	if d != nil && d.FS != nil {
		rd.FS = d.FS
		if rd.Tasks != nil {
			rd.Tasks.FS = d.FS
		}
	}
	return rd
}

func (d *Deps) wayfinderDeps() *wayfinder.Deps {
	if d != nil && d.Wayfinder != nil {
		return d.Wayfinder
	}
	wd := wayfinder.DefaultDeps()
	if d != nil && d.FS != nil {
		if wd.Tasks != nil {
			wd.Tasks.FS = d.FS
		}
	}
	return wd
}

func (d *Deps) monitorDeps() *monitor.Deps {
	md := monitor.DefaultDeps()
	if d != nil && d.FS != nil {
		md.FS = d.FS
	}
	return md
}

func (d *Deps) historyDeps() *history.Deps {
	hd := history.DefaultDeps()
	if d != nil && d.FS != nil {
		hd.FS = d.FS
	}
	// History rows live in the execution-state store, so the seam has to carry the
	// same store handle the rest of the cmd layer borrows — otherwise a test's
	// isolated data dir would be read through the cmd FS and written through the
	// real one (ADR-0140/0188).
	hd.Tasks = d.tasksDeps()
	return hd
}

// conventionsDeps resolves the Repo convention seam for a verb entrypoint. Like
// history, it borrows the cmd layer's tasks deps rather than building its own:
// the Convention memory layer is filed under Repository identity, so it has to
// resolve through the same data dir the rest of the cmd layer routes to.
func (d *Deps) conventionsDeps() *conventions.Deps {
	cd := conventions.DefaultDeps()
	if d != nil && d.FS != nil {
		cd.FS = d.FS
	}
	cd.Tasks = d.tasksDeps()
	return cd
}

func cmdMonitorStatePath() string {
	return monitor.DefaultStatePathWith(cmdLayerDeps().monitorDeps())
}

func cmdMonitorPIDPath() string {
	return monitor.DefaultPIDPathWith(cmdLayerDeps().monitorDeps())
}

// cmdHistoryDeps resolves the history seam for a verb entrypoint, so a History
// read or write lands in the same data dir the rest of the cmd layer routes to.
func cmdHistoryDeps() *history.Deps {
	return cmdLayerDeps().historyDeps()
}
