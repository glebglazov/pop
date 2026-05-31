package workload

import (
	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the workload package.
type Deps struct {
	FS     deps.FileSystem
	Git    deps.Git
	Runner CommandRunner
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
}

var defaultDeps = DefaultDeps()
