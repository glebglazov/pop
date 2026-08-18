// Package conventions owns the Repo convention: the prose answer to "how does
// this repository do X" for one Convention kind, resolved for the repository
// the current checkout belongs to.
//
// A kind resolves to exactly one answer plus the human's overlay: the human's
// own document, else the repository's committed one, else pop's memory, with
// ~/.agents/docs/<kind>.overlay.md appended whenever it exists. Nothing else
// composes (ADR-0223). Pop's job here is mechanical — derive the four paths,
// read what exists, decide which one answers, and label it. It never merges
// prose.
package conventions

import (
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// Deps is this package's process seam: the filesystem the layers are read
// through, and the tasks seam that answers Repository identity — the key the
// Convention memory layer is filed under, and what makes every worktree of a
// repository read one file.
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

// now is when a write says it happened. It rides the tasks bag rather than a
// clock of this package's own: that bag already answers Repository identity for
// the same write, so a test that fixes one fixes both.
func (d *Deps) now() time.Time { return d.tasksDeps().Now() }

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
