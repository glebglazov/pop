package routine

import (
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Routine kind's answer to Pane work attribution (ADR-0201, ADR-0209): the
// tag rung, and deliberately nothing else.
//
// The tag rung means "this is the pane pop opened to fire this Routine".
// `@pop_routine` carries the routine id a fire pane was spawned for, a Project
// routine's `project:<name>` included, which is the same id its container is
// keyed by — so the match is an id lookup and never a path comparison.
//
// There is no neighbourhood half. A Routine is bound to a directory but scoped to
// the project that directory belongs to, so a "pane standing somewhere" rung
// would answer with a whole project's routine list for any shell in the
// repository — a block of rows lifted to the top of page B for a human who is not
// doing routines at all. A shell that merely sits in a project directory
// attributes to no Routine.
//
// There is no repository pass either, and that is a distinction rather than an
// omission (ADR-0241 decision 7). The repository pass gave the Task-set kind and
// the Map kind a locality they were missing because each of their containers names
// a definite piece of work that lives in a definite repository; a Routine names a
// schedule. It has no container-level locality to narrow the answer with, its pane
// is short-lived, and page B has no equivalent of the preset narrowing that keeps
// that pass to a row or two — so the objection above is not weakened by the pass,
// it is exactly what the pass would run into. Do not wire
// `AttributePaneRepository` here.

// recordPanes indexes the Routines one Load read, keyed by the id a fire pane is
// tagged with. It runs at Load rather than at attribution time so a pane belongs
// to its Routine whether or not the surface ends up rendering that row
// (ADR-0201 decision 6).
func (k *Kind) recordPanes(containers []work.Container) {
	panes := make(map[string]work.AttributedContainer, len(containers))
	for _, c := range containers {
		panes[c.ID] = work.AttributedContainer{
			Ref:       ref.WorkRef{Kind: ref.KindRoutine, ContainerID: c.ID},
			CursorKey: c.CursorKey,
			Label:     "routine " + c.ID,
		}
	}
	k.panes = panes
}

// AttributePane answers the tag rung. A tag naming a Routine this load did not
// find — one deleted since its pane was opened — attributes to nothing, the same
// silence an unrelated shell gets.
func (k *Kind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.Routine == "" {
		return work.Attribution{}, false
	}
	c, ok := k.panes[facts.Routine]
	if !ok {
		return work.Attribution{}, false
	}
	return work.AttributeOne(c), true
}
