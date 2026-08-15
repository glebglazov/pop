// Package conventions owns the Repo convention: the prose answer to "how does
// this repository do X" for one Convention kind, resolved for the repository
// the current checkout belongs to.
//
// A kind never resolves to a single document. It resolves to a Convention
// stack of four layers that compose, and pop's whole job here is mechanical:
// derive the four paths, read what exists, order and label it, and state the
// override rule. It never merges prose — the agent reading the output
// reconciles the layers (ADR-0211).
package conventions

import (
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
