package drain

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// RoutineKind is the Routine Work kind, wired with the supervisor's own
// dependencies: one adapter wearing both seams, so the daemon drives the same
// object a read surface renders from. There is no routine pipeline left in queue —
// the schedule, the drift safety net, the overlap check and the fire all live
// beside the Routine's other verbs — and this is only the wiring that hands them
// the daemon's tmux, project and clock seams.
//
// The reader's location is left unwired here on purpose: relevance tiers matter to
// a page of Routines and not to a tick, so the kind resolves the cwd's checkout
// lazily inside Load and the daemon pays nothing for a fact it never reads.
func (d *Deps) RoutineKind(cfg *config.Config) work.Kind {
	return routine.NewKind(d.RoutineKindDeps(cfg))
}

// RoutineKindDeps projects queue's dependencies onto the Routine kind's. The
// checkout list is a seam rather than a resolved slice so nothing is read until a
// Load asks: a supervisor tick builds this list and never touches it.
func (d *Deps) RoutineKindDeps(cfg *config.Config) *routine.KindDeps {
	return &routine.KindDeps{
		Routine: d.routineDeps(),
		Out:     d.reconcileOut(),
		Checkouts: func() ([]project.ExpandedProject, error) {
			return tasks.ListPickerProjectsWith(d.Project, cfg)
		},
	}
}

// routineAdvancer is the advance half of the Routine kind, which is the same
// object: the supervisor asks for a list of advancers, and this is the entry it
// appends.
func (d *Deps) RoutineAdvancer(cfg *config.Config) work.Advancer {
	return routine.NewKind(d.RoutineKindDeps(cfg))
}

// routineDeps hands the drain pipeline's seams to the Routine kind's own
// projection. The mapping itself lives in routine, beside what reads it.
func (d *Deps) routineDeps() *routine.Deps {
	return routine.DepsFrom(routine.SupervisorSeams{
		Tasks:   d.Tasks,
		Project: d.Project,
		Tmux:    d.Tmux,
		Now:     d.now,
	})
}
