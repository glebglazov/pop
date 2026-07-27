package cmd

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
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
	Queue     *queue.Deps
	Routine   *routine.Deps
	Wayfinder *wayfinder.Deps
}

// cmdLayerDeps is the production edge for verb entrypoints. Tests override it
// to inject Dir and FS without mutating process-global state (ADR-0145).
var cmdLayerDeps = DefaultDeps

// DefaultDeps returns cmd-layer dependencies wired to real implementations.
func DefaultDeps() *Deps {
	fs := deps.NewRealFileSystem()
	return &Deps{
		FS:        fs,
		Tasks:     tasks.DefaultDeps(),
		Config:    config.DefaultDeps(),
		Project:   project.DefaultDeps(),
		Queue:     queue.DefaultDeps(),
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

func (d *Deps) queueDeps() *queue.Deps {
	if d != nil && d.Queue != nil {
		return d.Queue
	}
	qd := queue.DefaultDeps()
	if d != nil && d.FS != nil {
		if qd.Tasks != nil {
			qd.Tasks.FS = d.FS
		}
	}
	return qd
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
