package drain

import (
	"sync"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// WorkKinds is the wiring list the Work snapshot builder consumes: one adapter
// per Work kind, each constructed with queue's own dependencies captured — the
// store handle, the config loader, the Done-inclusion flag, and every build seam
// the dashboard tests inject — so the TUI's refresh rebuilds through exactly the
// seams its tests drive. The borrow is by pointer: no adapter closes the
// process-cached store handle it reaches through Tasks (ADR-0140).
//
// The list itself — which kinds exist and in what order — is `cmd`'s to wire
// through Kinds (ADR-0173); this is the default for callers that build queue deps
// directly, and the hook a test uses to hand the builder a synthetic kind.
func (d *Deps) WorkKinds(cfg *config.Config) []work.Kind {
	if d == nil {
		d = DefaultDeps()
	}
	// One wiring list is one load: every kind on it reads through a git seam that
	// memoizes the load's repeated questions (see AdvanceKinds for the wiring that
	// deliberately does not). The memoized deps are handed to the wiring list rather
	// than captured by it, so an override list builds its kinds over this load's
	// seam too.
	d = d.WithGitMemo()
	if d.Kinds != nil {
		return d.Kinds(d, cfg)
	}
	return d.kinds(cfg)
}

// AdvanceKinds is the same wiring list without the per-load git memo: the
// supervisor's dispatch phase creates worktrees and moves branches between
// asking a kind for candidates and telling it to advance, so a memo spanning the
// tick would answer a question about the repository as it was before its own
// writes. The read it does pay for — each kind's scan — memoizes inside itself.
func (d *Deps) AdvanceKinds(cfg *config.Config) []work.Kind {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Kinds != nil {
		return d.Kinds(d, cfg)
	}
	return d.kinds(cfg)
}

// kinds builds the adapters over whatever git seam the receiver carries.
func (d *Deps) kinds(cfg *config.Config) []work.Kind {
	groups := d.RepoGroups(cfg)
	return []work.Kind{
		d.TaskSetKind(cfg, groups),
		wayfinder.NewMapKind(d.MapKindDeps(cfg, groups)),
	}
}

// WithGitMemo returns a shallow copy of the deps whose git seams memoize the
// idempotent reads of one load (deps.MemoGit), leaving the caller's own deps
// untouched. Task and Project deps share one memo when they share one seam, so
// the common dir a project resolution and a task resolution both ask for is
// forked once rather than twice.
func (d *Deps) WithGitMemo() *Deps {
	if d == nil {
		return nil
	}
	td, pd := d.Tasks, d.Project
	tasksGit := td != nil && td.Git != nil
	projectGit := pd != nil && pd.Git != nil
	if !tasksGit && !projectGit {
		return d
	}
	out := *d
	var shared *deps.MemoGit
	if tasksGit {
		shared = deps.NewMemoGit(td.Git)
		cp := *td
		cp.Git = shared
		out.Tasks = &cp
	}
	if projectGit {
		memo := shared
		if memo == nil || memo.Inner() != pd.Git {
			memo = deps.NewMemoGit(pd.Git)
		}
		cp := *pd
		cp.Git = memo
		out.Project = &cp
	}
	return &out
}

// SetKindDeps projects queue's dependencies onto the Task-set kind's, forwarding
// every build seam the dashboard tests inject. The borrow is by pointer: the
// adapter never closes the process-cached store handle it reaches through Tasks
// (ADR-0140).
func (d *Deps) SetKindDeps(cfg *config.Config, groups func() ([]repogroup.Group, error)) *setkind.Deps {
	return &setkind.Deps{
		Tasks:           d.Tasks,
		Project:         d.Project,
		LoadConfig:      d.LoadConfig,
		Config:          cfg,
		IncludeDone:     d.IncludeDone,
		IncludeArchived: d.IncludeArchived,
		Groups:          groups,
		SetArchived:     d.SetArchived,
		Refresh:         d.Refresh,
		LiveDrains:      d.LiveDrains,
		Now:             d.Now,
		ProbeDirective:  d.ProbeDirective,
	}
}

// MapKindDeps projects queue's dependencies onto the Map kind's. The Map kind
// reads through queue's own filesystem and tmux, so a test that injects either
// sees Maps through it too.
func (d *Deps) MapKindDeps(cfg *config.Config, groups func() ([]repogroup.Group, error)) *wayfinder.MapKindDeps {
	// A caller that wired no Task deps still gets a usable kind: constructing one
	// must never touch the filesystem, and every read it does later goes through
	// these.
	td := d.Tasks
	if td == nil {
		td = tasks.DefaultDeps()
	}
	return &wayfinder.MapKindDeps{
		Wayfinder:       &wayfinder.Deps{FS: td.FS, Tasks: td, Tmux: d.Tmux},
		Project:         d.Project,
		Config:          cfg,
		Groups:          groups,
		IncludeArchived: d.IncludeArchived,
	}
}

// RepoGroups resolves the repository groups once per build and replays the
// answer: every kind scans per repository group, and the kinds of one build must
// read the same repository picture. A wiring list calls this once and hands the
// result to every kind it constructs.
func (d *Deps) RepoGroups(cfg *config.Config) func() ([]repogroup.Group, error) {
	return memoRepoGroups(&repogroup.Deps{Tasks: d.Tasks, Project: d.Project}, cfg)
}

// memoRepoGroups resolves the repository groups once and replays the answer, so
// every kind of one build reads the same repository picture.
func memoRepoGroups(rd *repogroup.Deps, cfg *config.Config) func() ([]repogroup.Group, error) {
	var (
		once   sync.Once
		groups []repogroup.Group
		err    error
	)
	return func() ([]repogroup.Group, error) {
		once.Do(func() { groups, err = repogroup.Resolve(rd, cfg) })
		return groups, err
	}
}

// RoutinePageKinds is the Routine page's wiring list: the one Routine kind,
// wired with the reader's own checkout so the relevance tiers its comparator
// sorts on are stamped against where the operator is standing. The location is
// resolved here rather than inside the kind because a supervisor tick wires the
// same adapter and must pay nothing for a fact it never reads.
func (d *Deps) RoutinePageKinds(cfg *config.Config) []work.Kind {
	if d == nil {
		d = DefaultDeps()
	}
	// A read surface's wiring list is one load, as on page A: the reader's own
	// checkout and every routine's bound directory ask the same few questions.
	d = d.WithGitMemo()
	if d.RoutineKinds != nil {
		return d.RoutineKinds(d, cfg)
	}
	kd := d.RoutineKindDeps(cfg)
	// A read surface narrates nothing: the advance half's writer would print into
	// the TUI's own screen.
	kd.Out = nil
	kd.Checkout = readerCheckout(d)
	return []work.Kind{routine.NewKind(kd)}
}

// readerCheckout is the canonical checkout the operator is standing in, empty
// when the cwd is in no checkout at all — in which case nothing but a Project
// routine reads as "here", which is the truth.
func readerCheckout(d *Deps) string {
	path, err := project.CurrentCheckoutPathWith(d.ProjectDeps())
	if err != nil {
		return ""
	}
	return path
}
