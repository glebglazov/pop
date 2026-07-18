package routine

import (
	"io"
	"os"
	"syscall"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// Deps holds external dependencies for the routine package.
type Deps struct {
	FS            deps.FileSystem
	OpenEditor    func(path string) error
	IsInteractive func() bool
	Now           func() time.Time
	Stdout        io.Writer
	LoadConfig    LoadConfigFunc
	Tasks         *tasks.Deps
	AttemptTimeout time.Duration
	PID           func() int
	ProcStartToken func(pid int) (string, bool)
	ProcessAlive  func(pid int, procStart string) bool
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	taskDeps := tasks.DefaultDeps()
	return &Deps{
		FS:            deps.NewRealFileSystem(),
		OpenEditor:    defaultOpenEditor,
		IsInteractive: defaultIsInteractive,
		Now:           time.Now,
		Stdout:        os.Stdout,
		LoadConfig:    DefaultLoadConfig,
		Tasks:         taskDeps,
		AttemptTimeout: tasks.DefaultAttemptTimeout,
		PID:           os.Getpid,
		ProcStartToken: defaultProcStartToken,
		ProcessAlive:  defaultProcessAlive(taskDeps),
	}
}

var defaultDeps = DefaultDeps()

func (d *Deps) taskDeps() *tasks.Deps {
	if d.Tasks != nil {
		return d.Tasks
	}
	return tasks.DefaultDeps()
}

func defaultProcessAlive(taskDeps *tasks.Deps) func(pid int, procStart string) bool {
	return func(pid int, procStart string) bool {
		if pid <= 0 {
			return false
		}
		if taskDeps.ProcessAlive != nil && !taskDeps.ProcessAlive(pid) {
			return false
		}
		if procStart == "" {
			return true
		}
		if taskDeps.ProcessStartToken != nil {
			current, ok := taskDeps.ProcessStartToken(pid)
			if ok {
				return current == procStart
			}
		}
		return true
	}
}

func defaultProcStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	// Best-effort start-time token; empty token falls back to PID-only liveness.
	return "", false
}

func processAlivePID(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}
