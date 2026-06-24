package tasks

import (
	"io"
	"os/exec"

	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the task package.
type Deps struct {
	FS           deps.FileSystem
	Git          deps.Git
	Runner       CommandRunner
	LookPath     func(file string) (string, error)
	ProcessAlive func(pid int) bool
	// ProcessStartToken returns an opaque token capturing the start instant of
	// the process with the given PID, and whether it could be determined. Paired
	// with ProcessAlive it defeats PID reuse in drain liveness. A nil seam falls
	// back to the platform default (defaultProcStartToken).
	ProcessStartToken func(pid int) (string, bool)
	NoticeOut         io.Writer
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:       deps.NewRealFileSystem(),
		Git:      deps.NewRealGit(),
		Runner:   RealCommandRunner{},
		LookPath: exec.LookPath,
	}
}

var defaultDeps = DefaultDeps()
