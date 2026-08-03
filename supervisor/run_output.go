package supervisor

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/tasks/drain"
)

// The daemon's run output is an emit-on-change diff over the Task-set run view:
// the first tick prints a baseline, every later tick prints only what changed.
// The view itself is the Task-set kind's (drain.RunView) — this is just the
// swallowing the loop wraps it in.

type runOutputState struct {
	firstTick bool
	prev      *drain.RunView
	lastScan  string
}

func newRunOutputState() *runOutputState {
	return &runOutputState{firstTick: true}
}

func (s *runOutputState) emitViewTransition(out io.Writer, view drain.RunView, eventLines []string) {
	if s.firstTick {
		drain.RenderRunBaseline(out, view)
		s.firstTick = false
	} else {
		for _, line := range append(drain.DiffRunView(s.prev, view), eventLines...) {
			fmt.Fprintln(out, line)
		}
	}
	copy := view
	s.prev = &copy
}

func (s *runOutputState) emitPostSpawnView(out io.Writer, view drain.RunView) {
	copy := view
	s.prev = &copy
}

func (s *runOutputState) emitScanError(out io.Writer, msg string) {
	if s.lastScan == msg {
		return
	}
	fmt.Fprintln(out, msg)
	s.lastScan = msg
}
