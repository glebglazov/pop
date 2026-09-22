package implement

import (
	"io"
	"os"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// Deps holds implement orchestration dependencies and test seams.
type Deps struct {
	Tasks   *tasks.Deps
	Project *project.Deps
	// LoadConfig loads a config file. When nil, every config layer is read
	// through Tasks' FileSystem.
	LoadConfig func(string) (*config.Config, error)

	// StdinInteractive reports whether stdin is an interactive terminal. When nil,
	// the default checks os.Stdin when ConfirmIn is an *os.File.
	StdinInteractive func(io.Reader) bool

	// Now stamps provisioned worktree branch names (`--in-worktree`). When nil it
	// defaults to time.Now so tests can pin a deterministic branch.
	Now func() time.Time
}

// DefaultDeps returns production implement dependencies.
func DefaultDeps() *Deps {
	return &Deps{
		Tasks:   tasks.DefaultDeps(),
		Project: project.DefaultDeps(),
	}
}

func (d *Deps) tasksDeps() *tasks.Deps {
	if d != nil && d.Tasks != nil {
		return d.Tasks
	}
	return tasks.DefaultDeps()
}

func (d *Deps) projectDeps() *project.Deps {
	if d != nil && d.Project != nil {
		return d.Project
	}
	return project.DefaultDeps()
}

// loadDefaultConfig loads the default config, resolving its path and every
// config layer through the task Deps' FileSystem so a run reads the config of
// the environment its Deps describe.
func (d *Deps) loadDefaultConfig() (*config.Config, error) {
	return d.loadConfig(config.DefaultConfigPathWith(d.tasksDeps().ConfigDeps()))
}

// loadConfig is the config loader implement hands the tasks package. Without an
// injected LoadConfig it reads every config layer through the task Deps'
// FileSystem rather than the process environment.
func (d *Deps) loadConfig(path string) (*config.Config, error) {
	if d != nil && d.LoadConfig != nil {
		return d.LoadConfig(path)
	}
	return config.LoadWith(d.tasksDeps().ConfigDeps(), path)
}

func (d *Deps) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Deps) stdinInteractive(in io.Reader) bool {
	if d != nil && d.StdinInteractive != nil {
		return d.StdinInteractive(in)
	}
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
