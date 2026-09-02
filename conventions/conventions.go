// Package conventions owns the Repo convention: the prose answer to "how does
// this repository do X" for one Convention kind, resolved for the repository
// the current checkout belongs to.
//
// A kind resolves to exactly one answer plus any Overlay: their document for
// this project, else their document for every repository, else the
// repository's committed one, else pop's own shipped answer, with
// ~/.agents/docs/<name>.overlay.md and docs/agents/<name>.overlay.md appended
// whenever they exist. Nothing else composes (ADR-0223, ADR-0226, ADR-0247).
// Pop's job here is mechanical — derive the paths, read what exists, decide
// which one answers, and label it. It never merges prose.
package conventions

import (
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// Deps is this package's process seam: the filesystem the layers are read
// through, and the tasks seam that answers both the git questions the paths
// derive from — the project's remote, and the Repository identity a project with
// no remote falls back to.
type Deps struct {
	FS    deps.FileSystem
	Tasks *tasks.Deps
}

// DefaultDeps wires the seam to real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:    deps.NewRealFileSystem(),
		Tasks: tasks.DefaultDeps(),
	}
}

func (d *Deps) fs() deps.FileSystem {
	if d == nil || d.FS == nil {
		return deps.NewRealFileSystem()
	}
	return d.FS
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
