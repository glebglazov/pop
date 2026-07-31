package tasks

import (
	"io"
	"os/exec"
	"sync"
	"time"

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

	// Recovery-wait cadence (WaitForRecovery, ADR-0100/ADR-0144). These are the
	// one production time seam this package exposes: zero values fall back to
	// the production defaults (see recovery.go), so real runs keep the 2s
	// fast-check and 5s/30s poll intervals. Tests inject small values so the
	// wait loop advances without real wall-clock waits.
	RecoveryFastCheckInterval    time.Duration
	RecoveryPollInterval         time.Duration
	RecoveryPollImminentInterval time.Duration

	// RetryDelayWait, when set, replaces the real attempt-retry countdown sleep
	// (waitAttemptRetryDelay). Nil keeps production behaviour. Tests inject a
	// no-sleep hook so retries advance without wall-clock waits (ADR-0145).
	RetryDelayWait func(out io.Writer, delay time.Duration) bool

	// ClipboardCopy places text on the system clipboard for an attended
	// assistance briefing with no positional prompt form (kimi, ADR-0164). A nil
	// seam falls back to clipboard.Copy (tmux buffer / OSC 52); tests inject a
	// fake to assert delivery and failure messaging without a real clipboard.
	ClipboardCopy func(text string) error

	// store is the lazily-opened, process-cached execution-state store handle
	// holder. DefaultDeps pre-allocates it so production copies of Deps share one
	// handle; a Deps built from a bare literal (tests) gets its holder lazily on
	// first store touch. See Deps.Store.
	store *storeCache
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:                           deps.NewRealFileSystem(),
		Git:                          deps.NewRealGit(),
		Runner:                       RealCommandRunner{},
		LookPath:                     exec.LookPath,
		RecoveryFastCheckInterval:    defaultRecoveryFastCheckInterval,
		RecoveryPollInterval:         defaultRecoveryPollInterval,
		RecoveryPollImminentInterval: defaultRecoveryPollImminentInterval,
		store:                        &storeCache{},
	}
}

var defaultDeps = DefaultDeps()
