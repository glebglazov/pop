package supervisor

import (
	"fmt"
	"io"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
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

// emitViewTransition prints the tick's output: the baseline on the first tick,
// the view diff on every later one. baseline is a closure because the baseline is
// `pop work status`'s own render (ADR-0121) — it needs the snapshot and the two
// tables, not the run view — and only the first tick may pay for building them.
func (s *runOutputState) emitViewTransition(out io.Writer, view drain.RunView, eventLines []string, baseline func(io.Writer)) {
	if s.firstTick {
		baseline(out)
		s.firstTick = false
	} else {
		for _, line := range append(drain.DiffRunView(s.prev, view), eventLines...) {
			fmt.Fprintln(out, line)
		}
	}
	copy := view
	s.prev = &copy
}

// renderBaseline prints the Daemon run baseline: the same Summary headline and
// two tables `pop work status` prints, off the tick's own snapshot. A table-build
// failure degrades to the headline alone rather than losing the whole baseline.
func renderBaseline(out io.Writer, d *drain.Deps, cfg *config.Config, snap drain.StatusSnapshot) {
	tables, err := dashboard.BuildStatusTables(d, cfg)
	if err != nil {
		fmt.Fprintf(out, "work: status tables: %v\n", err)
		drain.RenderRunSummary(out, drain.BuildRunView(snap, time.Now()))
		return
	}
	dashboard.RenderStatus(out, snap, tables)
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
