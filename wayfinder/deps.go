package wayfinder

import (
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// Deps holds external dependencies for the wayfinder package.
type Deps struct {
	FS    deps.FileSystem
	Tasks *tasks.Deps
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:    deps.NewRealFileSystem(),
		Tasks: tasks.DefaultDeps(),
	}
}

func (d *Deps) taskDeps() *tasks.Deps {
	if d.Tasks != nil {
		return d.Tasks
	}
	return tasks.DefaultDeps()
}
