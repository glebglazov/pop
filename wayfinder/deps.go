package wayfinder

import (
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
)

// Deps holds external dependencies for the wayfinder package.
type Deps struct {
	FS    deps.FileSystem
	Tasks *tasks.Deps
	// Clock and Owner are the two inputs a claim needs beyond the store: when it
	// was taken, and who took it. Both are injectable because a claim's whole
	// behaviour — TTL expiry, refusing another window, renewing your own — is a
	// function of them, and neither is reproducible from a test process.
	Clock func() time.Time
	Owner func() string
	// Tmux is the Map's session surface — `arrive` tears one down. Left nil it
	// resolves lazily to the real tmux, so a read verb never shells out.
	Tmux tmux.Tmux
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

func (d *Deps) now() time.Time {
	if d.Clock != nil {
		return d.Clock().UTC()
	}
	return time.Now().UTC()
}

func (d *Deps) tmux() tmux.Tmux {
	if d.Tmux != nil {
		return d.Tmux
	}
	d.Tmux = tmux.New()
	return d.Tmux
}

func (d *Deps) owner() string {
	if d.Owner != nil {
		return d.Owner()
	}
	return DefaultClaimOwner()
}
