package wayfinder

import (
	"strings"
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
	// behaviour — refusing another window, re-claiming your own — is a function
	// of them, and neither is reproducible from a test process.
	Clock func() time.Time
	Owner func() string
	// PIDAlive is the liveness probe behind a `pid:` claim owner. Left nil it is
	// a zero-signal probe; injectable because a test cannot conjure a pid it
	// knows to be dead without racing whoever the kernel hands it to next.
	PIDAlive func(pid int) bool
	// Tmux is the Map's session surface — `arrive` tears one down. Left nil it
	// resolves lazily to the real tmux, so a read verb never shells out.
	Tmux tmux.Tmux
	// Trunk resolves the Trunk worktree a Map's session is rooted at. It is a
	// dependency rather than a wayfinder computation because the answer comes
	// from the repository config and the caller's --trunk override, both of which
	// live at the CLI edge. Left nil, opening a session refuses with ErrNoTrunk.
	Trunk func() (string, error)
	// SetStatuses returns the live status of every Task set registered under a
	// definition path, keyed by set id — what a Map's spawned ids resolve to. Left
	// nil it reads through the Task-set refresh; injectable because a test wants to
	// name a set's status rather than lay out and register one.
	SetStatuses func(defPath string) (map[string]SpawnedSet, error)
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

// trunk resolves the Trunk worktree, refusing rather than guessing a directory:
// a Map session rooted at the wrong checkout would put every grilling pane in
// the wrong tree.
func (d *Deps) trunk() (string, error) {
	if d.Trunk == nil {
		return "", ErrNoTrunk
	}
	dir, err := d.Trunk()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		return "", ErrNoTrunk
	}
	return dir, nil
}

func (d *Deps) owner() string {
	return d.ownerWith(d.ownerLiveness())
}

// ownerWith names this process against a liveness object already in hand, so a
// claim verb's owner and its liveness probe share one pane listing.
func (d *Deps) ownerWith(l *ownerLiveness) string {
	if d.Owner != nil {
		return d.Owner()
	}
	return l.selfOwner()
}
