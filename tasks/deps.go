package tasks

import (
	"io"
	"os/exec"
	"sync"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

// storeCache is the process-cached execution-state store handle holder. It sits
// behind a pointer on Deps so a shallow copy of Deps (e.g. the queue scan
// memoization in queue.go) shares one handle rather than opening its own, and so
// Deps stays copy-safe (the mutex never rides a value copy). Access is guarded by
// mu; see Deps.Store and Deps.CloseStore.
//
// path records the store path the cached handle was opened against. In production
// the derived path is constant, so the handle is reused for the process. Tests
// redirect the data dir per test (XDG_DATA_HOME), so the shared package-global
// defaultDeps would otherwise hand back a handle pointing at a previous test's
// (since-removed) database; when the derived path changes the accessor drops the
// stale handle and reopens against the new path.
type storeCache struct {
	mu     sync.Mutex
	path   string
	handle *store.Store
}

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

	// store is the lazily-opened, process-cached execution-state store handle
	// holder. DefaultDeps pre-allocates it so production copies of Deps share one
	// handle; a Deps built from a bare literal (tests) gets its holder lazily on
	// first store touch. See Deps.Store.
	store *storeCache
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:       deps.NewRealFileSystem(),
		Git:      deps.NewRealGit(),
		Runner:   RealCommandRunner{},
		LookPath: exec.LookPath,
		store:    &storeCache{},
	}
}

var defaultDeps = DefaultDeps()
