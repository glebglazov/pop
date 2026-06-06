package tasks

import (
	"io"

	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the task package.
type Deps struct {
	FS           deps.FileSystem
	Git          deps.Git
	Runner       CommandRunner
	ProcessAlive func(pid int) bool
	NoticeOut    io.Writer
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
