package supervisor

import (
	"fmt"
	"github.com/glebglazov/pop/tasks/drain"
	"io"
	"os"
	"sync"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// Run starts the foreground supervisor loop: it acquires the single-instance
// lock, then every poll interval scans every registered project and spawns a
// drain into tmux for each idle project with a Ready set. It returns when a
// signal arrives on sigCh (graceful shutdown) — in-flight drains are
// tmux-owned panes and keep running. A second `pop work daemon` while one holds
// the lock is refused before the loop starts.
func Run(d *drain.Deps, interval time.Duration, out io.Writer, sigCh <-chan os.Signal) error {
	out, supervisorLog, err := supervisorOutput(d.Tasks, out)
	if err != nil {
		return err
	}
	defer supervisorLog.Close()

	lock, err := AcquireSupervisorLock(d.Tasks)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	if _, err := d.LoadConfig(config.DefaultConfigPath()); err != nil {
		return err
	}

	fmt.Fprintf(out, "pop work supervisor started (PID %d); poll every %s. Ctrl-C to stop.\n", os.Getpid(), interval)
	tasks.WarnProcStartUnsupported(out)

	output := newRunOutputState()
	for {
		tick(d, out, output)

		select {
		case <-sigCh:
			fmt.Fprintln(out, "\nShutting down supervisor; in-flight drains keep running in their panes.")
			return nil
		case <-time.After(interval):
		}
	}
}

// tick performs one reconcile-candidate-dispatch pass over every advanceable
// Work kind. Errors resolving or spawning a single project are reported and
// skipped; one bad project never halts the supervisor.
//
// The phases are the seam's: the kinds are reconciled and asked what they would
// advance (both pure enough to run concurrently across kinds), then driven one
// candidate at a time. Refusals ride the same dispatch call as advances, because
// a kind that must persist why it refused writes on exactly that path.
func tick(d *drain.Deps, out io.Writer, runOut *runOutputState) {
	cfg, err := d.LoadConfig(config.DefaultConfigPath())
	if err != nil {
		runOut.emitScanError(out, fmt.Sprintf("work: load config: %v", err))
		return
	}

	// One advancer list per tick: an adapter carries this pass's candidates, so a
	// fresh list is what keeps that state tick-scoped.
	passes, err := readPasses(advancers(d, cfg))
	if err != nil {
		runOut.emitScanError(out, err.Error())
		return
	}
	runOut.lastScan = ""

	if snap, err := drain.BuildStatus(d, cfg); err == nil {
		preSpawn := drain.BuildRunView(snap, time.Now())
		runOut.emitViewTransition(out, preSpawn, nil, func(w io.Writer) {
			renderBaseline(w, d, cfg, snap)
		})
	} else {
		fmt.Fprintf(out, "work: status: %v\n", err)
	}

	// Dispatch phase — serial, in kind precedence order, because it mutates the
	// shared checkout ledger and "first wins, rest defer" needs a defined order.
	occupancy := newCheckoutOccupancy(d)
	var spawned []drain.PickedUpSet
	for _, pass := range passes {
		for _, candidate := range pass.candidates {
			if line := dispatch(pass, candidate, occupancy).Line(); line != "" {
				fmt.Fprintln(out, line)
			}
		}
		if reporter, ok := pass.adv.(spawnedSetsReporter); ok {
			spawned = append(spawned, reporter.SpawnedSets()...)
		}
	}

	if snap, err := drain.BuildStatus(d, cfg); err == nil {
		// A just-spawned drain has not yet acquired its runtime lock, so the
		// post-spawn scan still lists its set as Ready, not Running. Seed the
		// spawned sets into the swallow snapshot so next tick's view diff does
		// not re-announce them as freshly "spawned drain" (they were already
		// reported imperatively above). This seeding is display-only — the
		// double-spawn window is closed against the store by the spawn-intent
		// guard, not by this view patch (see seedSpawnedRunning).
		runOut.emitPostSpawnView(out, drain.SeedSpawnedRunning(drain.BuildRunView(snap, time.Now()), spawned))
	}
}

// advancers is the supervisor's list: the advanceable kinds of the read-surface
// wiring list, then the Routine adapter, which wears both seams but is not on that
// list yet — the Routine dashboard page it belongs to arrives next. Appending is
// precedence-correct, routine being last in the closed enum, and the special case
// dies the moment the read list carries it.
//
// The daemon pins kind precedence here and takes no ordering from the active
// preset, which is why it names it: every Work read surface orders by creation
// date now and a preset may flip that (ADR-0210), but this list feeds the serial
// dispatch that writes the checkout occupancy ledger, where "first wins, rest
// defer" must not be re-ranked by a display setting.
func advancers(d *drain.Deps, cfg *config.Config) []work.Advancer {
	return append(work.AdvancersInKindPrecedence(d.AdvanceKinds(cfg)), d.RoutineAdvancer(cfg))
}

// kindPass is one kind's half of a supervisor tick: the advancer, the kind it
// answers for, and what it said it would advance.
type kindPass struct {
	kind       work.KindID
	adv        work.Advancer
	candidates []work.Candidate
}

// readPasses runs the reconcile and candidate phases, one goroutine per kind.
// They fan out because candidates are pure now: a kind's Candidates() performs
// no writes, so no ordering between kinds is observable and the slowest kind no
// longer delays the rest. Within a kind the two stay ordered — candidates are
// meant to see the healed state their own reconcile produced.
//
// A reconcile failure is already reported by the kind and never fails the tick:
// that kind's candidates then read the pre-reconcile snapshot, which is no worse
// than before the pass existed. A *candidate* failure does fail it, returning the
// first in kind precedence order — the pre-seam behaviour of reporting the scan
// error once and dispatching nothing.
func readPasses(advancers []work.Advancer) ([]kindPass, error) {
	passes := make([]kindPass, len(advancers))
	errs := make([]error, len(advancers))
	var wg sync.WaitGroup
	for i, adv := range advancers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = adv.Reconcile()
			candidates, err := adv.Candidates()
			passes[i] = kindPass{kind: advancerKindID(adv), adv: adv, candidates: candidates}
			errs[i] = err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return passes, nil
}

// dispatch drives one candidate and reports what happened as a structured event.
// The supervisor rules on checkout occupancy first — the one invariant it owns —
// and the kind is not asked at all about a candidate it may not start, so an
// adapter that omits its own check is still held to it.
func dispatch(pass kindPass, c work.Candidate, occupancy *checkoutOccupancy) work.AdvanceEvent {
	event := work.AdvanceEvent{Kind: pass.kind, Ref: c.Ref, Label: c.Label}
	if !c.Refused() {
		if reason := occupancy.refusal(c); reason != "" {
			event.Outcome = work.Outcome{Kind: work.OutcomeMessage, Message: fmt.Sprintf("work: %s: skip; %s", c.Label, reason)}
			return event
		}
	}
	event.Outcome, event.Err = pass.adv.Advance(c)
	if event.Err == nil && !c.Refused() {
		occupancy.occupy(c)
	}
	return event
}

// advancerKindID names the kind behind an advancer. It asks for the id alone
// rather than asserting work.Kind, because an adapter may wear the advance seam
// before the read one — the Routine adapter does — and a hand-built advancer
// that names no kind answers the zero kind rather than failing the tick.
func advancerKindID(adv work.Advancer) work.KindID {
	if k, ok := adv.(interface{ ID() work.KindID }); ok {
		return k.ID()
	}
	return work.KindID("")
}

// spawnedSetsReporter is how the supervisor asks for that seeding without naming
// the Task-set kind: an advancer that has nothing to seed implements nothing.
type spawnedSetsReporter interface {
	SpawnedSets() []drain.PickedUpSet
}
