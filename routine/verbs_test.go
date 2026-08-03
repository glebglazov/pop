package routine

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work"
)

// verbKind wires the kind over a tmux-recording data dir, the way a read surface
// wires it: the reader stands in home, which is where the fixtures are bound.
func verbKind(t *testing.T, d *Deps, home string) *Kind {
	t.Helper()
	return NewKind(&KindDeps{
		Routine:  d,
		Out:      io.Discard,
		Checkout: canonical(t, home),
		Project:  "pop",
		Checkouts: func() ([]project.ExpandedProject, error) {
			return []project.ExpandedProject{{Name: "pop", ProjectLabel: "pop", Path: canonical(t, home)}}, nil
		},
	})
}

func verbsOffered(actions []work.Action) []work.Verb {
	verbs := make([]work.Verb, 0, len(actions))
	for _, a := range actions {
		verbs = append(verbs, a.Verb)
	}
	return verbs
}

func keyFor(t *testing.T, actions []work.Action, verb work.Verb) string {
	t.Helper()
	for _, a := range actions {
		if a.Verb == verb {
			return a.Key
		}
	}
	t.Fatalf("verb %q not offered among %+v", verb, actions)
	return ""
}

func hasVerb(actions []work.Action, verb work.Verb) bool {
	for _, a := range actions {
		if a.Verb == verb {
			return true
		}
	}
	return false
}

// TestRoutineActionsAnswerTheRowInFrontOfThem drives the whole lazy-verb rule:
// what a Routine offers is read off the container each time a menu opens, so the
// consent pair flips direction with the pause bit, a Project routine never offers
// it at all, and a Routine whose definition will not load offers nothing that
// would need that definition.
func TestRoutineActionsAnswerTheRowInFrontOfThem(t *testing.T) {
	d, home := routineTmuxDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeWith(d, "alpha"); err != nil {
		t.Fatal(err)
	}
	k := verbKind(t, d, home)

	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "alpha")
	want := []work.Verb{
		VerbFire, VerbPause, VerbPreview, VerbEdit, VerbRefine, VerbRuns, VerbHandoff,
		work.VerbShell, work.VerbCopyName,
	}
	if got := verbsOffered(k.Actions(c)); !equalVerbs(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	// Exactly two of the offered verbs are shared ids; every other one is the
	// Routine's own, so nothing it does was promoted into the seam's vocabulary.
	shared := 0
	for _, verb := range verbsOffered(k.Actions(c)) {
		if verb == work.VerbShell || verb == work.VerbCopyName {
			shared++
		}
	}
	if shared != 2 {
		t.Fatalf("shared verbs among %v = %d, want shell and copy-name alone", verbsOffered(k.Actions(c)), shared)
	}

	// The pause the operator applies in one pane is what the next menu open reads.
	outcome, err := k.Perform(c, nil, VerbPause)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != work.OutcomeRefresh || outcome.Message != "paused alpha" {
		t.Fatalf("pause outcome = %+v, want a refresh naming the routine", outcome)
	}
	reloaded, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	paused := containerByID(t, reloaded, "alpha")
	if !paused.RoutinePaused {
		t.Fatal("pause did not persist")
	}
	actions := k.Actions(paused)
	if hasVerb(actions, VerbPause) || !hasVerb(actions, VerbResume) {
		t.Fatalf("a paused Routine must offer resume and not pause: %v", verbsOffered(actions))
	}
	if key := keyFor(t, actions, VerbResume); key != keyFor(t, k.Actions(c), VerbPause) {
		t.Fatalf("resume key %q differs from pause key; the consent pair shares one key", key)
	}
	if _, err := k.Perform(paused, nil, VerbResume); err != nil {
		t.Fatal(err)
	}
	if r, err := loadManifest(d, "alpha"); err != nil || r.Manifest.Paused {
		t.Fatalf("resume did not clear the pause bit (err %v)", err)
	}

	// A broken Routine offers only its name: every other verb needs the definition
	// that would not load.
	if _, err := AddWith(d, "broken", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// routineTmuxDeps roots the data dir one level above the bound home.
	breakManifest(t, filepath.Dir(home), "broken")
	brokenContainers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	broken := containerByID(t, brokenContainers, "broken")
	if !broken.Broken {
		t.Fatalf("fixture did not break: %+v", broken)
	}
	if got := verbsOffered(k.Actions(broken)); !equalVerbs(got, []work.Verb{work.VerbCopyName}) {
		t.Fatalf("broken actions = %v, want copy-name alone", got)
	}
}

func equalVerbs(got, want []work.Verb) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestRoutineFireEditRefineHandOffToPanes pins the three verbs that cannot finish
// in place: each spawns its pane and returns it as a handoff for the caller to
// focus, and the edit applies the run-affecting pause chokepoint before the editor
// it can no longer wait for opens.
func TestRoutineFireEditRefineHandOffToPanes(t *testing.T) {
	d, home := routineTmuxDeps(t)
	if _, err := AddWith(d, "beta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeWith(d, "beta"); err != nil {
		t.Fatal(err)
	}
	k := verbKind(t, d, home)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "beta")
	rt := tmuxRecorder(d)

	fire, err := k.Perform(c, nil, VerbFire)
	if err != nil {
		t.Fatal(err)
	}
	if fire.Kind != work.OutcomeHandoff || fire.Handoff.Kind != work.HandoffTmux || fire.Handoff.Target == "" {
		t.Fatalf("fire outcome = %+v, want a tmux handoff naming the pane", fire)
	}
	send, ok := rt.findCommand("send-keys")
	if !ok || !strings.Contains(strings.Join(send, " "), "pop routine fire beta") {
		t.Fatalf("fire did not spawn its pane: %v", rt.commands)
	}

	rt.commands = nil
	edit, err := k.Perform(c, nil, VerbEdit)
	if err != nil {
		t.Fatal(err)
	}
	if edit.Kind != work.OutcomeHandoff || edit.Handoff.Target == "" {
		t.Fatalf("edit outcome = %+v, want a tmux handoff naming the editor's pane", edit)
	}
	editWindow, ok := rt.findCommand("new-window")
	if !ok || !containsArg(editWindow, "-n", "beta-edit") {
		t.Fatalf("edit did not open its own window: %v", rt.commands)
	}
	send, ok = rt.findCommand("send-keys")
	if !ok || !strings.Contains(strings.Join(send, " "), filepath.Join(routineDir(d, "beta"), promptFileName)) {
		t.Fatalf("edit did not open the prompt in an editor: %v", rt.commands)
	}
	r, err := loadManifest(d, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Manifest.Paused {
		t.Fatal("editing the prompt must pause the Routine: the edit is run-affecting")
	}

	rt.commands = nil
	refine, err := k.Perform(c, nil, VerbRefine)
	if err != nil {
		t.Fatal(err)
	}
	if refine.Kind != work.OutcomeHandoff || refine.Handoff.Target == "" {
		t.Fatalf("refine outcome = %+v, want a tmux handoff naming the gate's pane", refine)
	}
	send, ok = rt.findCommand("send-keys")
	if !ok || !strings.Contains(strings.Join(send, " "), "routine edit beta") {
		t.Fatalf("refine did not spawn the gate: %v", rt.commands)
	}

	// The gate is not a Work status: nothing about spawning it marks the container,
	// which keeps reading as the paused Routine it is.
	after, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	refined := containerByID(t, after, "beta")
	if refined.Status != pausedStatusLabel(r.Manifest.PauseReason) {
		t.Fatalf("status = %q, want the Routine's own pause label — no being-refined state", refined.Status)
	}
	for _, section := range refined.DetailSections {
		if strings.Contains(strings.ToUpper(section.Body), "NEEDS-HUMAN") {
			t.Fatalf("a NEEDS-HUMAN facet appeared in %+v", section)
		}
	}
}

// TestRoutinePreviewTakesTheOperatorToTheRunPane pins the preview verb: it spawns
// nothing, hands the operator to the pane a fire is running in, and says so
// plainly when there is none.
func TestRoutinePreviewTakesTheOperatorToTheRunPane(t *testing.T) {
	d, home := routineTmuxDeps(t)
	if _, err := AddWith(d, "gamma", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	k := verbKind(t, d, home)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "gamma")

	none, err := k.Perform(c, nil, VerbPreview)
	if err != nil {
		t.Fatal(err)
	}
	if none.Kind != work.OutcomeMessage || !strings.Contains(none.Message, "no run pane") {
		t.Fatalf("preview with no pane = %+v, want a plain message", none)
	}

	rt := tmuxRecorder(d)
	rt.paneList = "gamma %77"
	live, err := k.Perform(c, nil, VerbPreview)
	if err != nil {
		t.Fatal(err)
	}
	if live.Kind != work.OutcomeHandoff || live.Handoff.Target != "%77" {
		t.Fatalf("preview outcome = %+v, want a handoff to the tagged pane", live)
	}
	if tmuxHasCommand(rt, "new-window") || tmuxHasCommand(rt, "send-keys") {
		t.Fatalf("preview must spawn nothing: %v", rt.commands)
	}
}

// TestRoutineCopyVerbs pins the two clipboard verbs: copy-report-path over the row
// copies the newest run's report and over a run copies that run's, and handoff
// copies the whole continuation prompt.
func TestRoutineCopyVerbs(t *testing.T) {
	d, home := routineTmuxDeps(t)
	if _, err := AddWith(d, "delta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	k := verbKind(t, d, home)

	// Before the first fire there is no report, so the verb is not offered at all
	// and performing it anyway says why rather than copying a path that is not one.
	fresh, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	dry := containerByID(t, fresh, "delta")
	if hasVerb(k.Actions(dry), VerbCopyReportPath) {
		t.Fatalf("a never-fired Routine offered copy-report-path: %v", verbsOffered(k.Actions(dry)))
	}
	if outcome, err := k.Perform(dry, nil, VerbCopyReportPath); err != nil || outcome.Clipboard != "" || outcome.Message != noReportMessage {
		t.Fatalf("copy with no report = %+v (err %v), want %q", outcome, err, noReportMessage)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	firedAt := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	report := filepath.Join(routineDir(d, "delta"), runsDirName, "2026-07-18T16-00-00Z.md")
	run, err := s.StartRoutineRun(store.RoutineRun{RoutineID: "delta", FiredAt: firedAt, PID: 100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run.ID, store.RoutineRunSucceeded, report, "", firedAt); err != nil {
		t.Fatal(err)
	}

	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "delta")
	if c.RoutineLastReport != report {
		t.Fatalf("container last report = %q, want %q", c.RoutineLastReport, report)
	}
	if !hasVerb(k.Actions(c), VerbCopyReportPath) {
		t.Fatalf("a fired Routine must offer copy-report-path: %v", verbsOffered(k.Actions(c)))
	}
	rowCopy, err := k.Perform(c, nil, VerbCopyReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if rowCopy.Clipboard != report {
		t.Fatalf("row copy = %q, want the newest run's report %q", rowCopy.Clipboard, report)
	}

	if len(c.Items) != 1 {
		t.Fatalf("items = %+v, want the one run", c.Items)
	}
	item := c.Items[0]
	if !hasVerb(k.ItemActions(c, item), VerbCopyReportPath) {
		t.Fatalf("a run must offer copy-report-path: %v", verbsOffered(k.ItemActions(c, item)))
	}
	itemCopy, err := k.Perform(c, &item, VerbCopyReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if itemCopy.Clipboard != report {
		t.Fatalf("item copy = %q, want that run's report %q", itemCopy.Clipboard, report)
	}

	handoff, err := k.Perform(c, nil, VerbHandoff)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(handoff.Clipboard, `routine "delta"`) || !strings.Contains(handoff.Clipboard, report) {
		t.Fatalf("handoff clipboard is not the continuation prompt:\n%s", handoff.Clipboard)
	}
	if handoff.Message != "copied handoff prompt" {
		t.Fatalf("handoff message = %q", handoff.Message)
	}
}

// TestRoutineRunsOpensTheGenericDetail pins the runs verb as the drill-down and
// nothing else: it asks the caller for the container's own detail view, so the run
// history is the generic item list rather than a frame of the Routine's own.
func TestRoutineRunsOpensTheGenericDetail(t *testing.T) {
	d, home := routineTmuxDeps(t)
	if _, err := AddWith(d, "epsilon", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	k := verbKind(t, d, home)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "epsilon")
	outcome, err := k.Perform(c, nil, VerbRuns)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != work.OutcomeDetail {
		t.Fatalf("runs outcome = %+v, want the detail view", outcome)
	}
	if key := keyFor(t, k.Actions(c), VerbRuns); key != "l" {
		t.Fatalf("runs key = %q, want the drill-down key l", key)
	}
}

// TestRoutineDetailSectionsCarryTheRetiredFrame pins what the run-history view's
// hand-rolled frame used to draw, now authored as sections above the generic item
// list: schedule, bound directory, pause state with its reason, last report.
func TestRoutineDetailSectionsCarryTheRetiredFrame(t *testing.T) {
	d, home := routineTmuxDeps(t)
	if _, err := AddWith(d, "zeta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// A drift pause is the one Routine state that genuinely needs a human, so the
	// section must name what to do about it.
	if err := PauseChangedWith(d, "zeta"); err != nil {
		t.Fatal(err)
	}
	k := verbKind(t, d, home)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "zeta")

	titles := make([]string, 0, len(c.DetailSections))
	bodies := map[string]string{}
	for _, s := range c.DetailSections {
		titles = append(titles, s.Title)
		bodies[s.Title] = s.Body
	}
	want := []string{"Schedule", "Directory", "Pause state", "Last report"}
	for i, title := range want {
		if i >= len(titles) || titles[i] != title {
			t.Fatalf("section titles = %v, want %v", titles, want)
		}
	}
	if bodies["Schedule"] != "every 6h" {
		t.Fatalf("schedule section = %q", bodies["Schedule"])
	}
	if bodies["Directory"] != c.RoutineDirectory {
		t.Fatalf("directory section = %q, want the bound directory %q", bodies["Directory"], c.RoutineDirectory)
	}
	if !strings.HasPrefix(bodies["Pause state"], "paused (changed)") || !strings.Contains(bodies["Pause state"], "re-prove") {
		t.Fatalf("pause section = %q, want the drift reason and what to do about it", bodies["Pause state"])
	}
	if bodies["Last report"] != "none yet" {
		t.Fatalf("last report section = %q, want the never-fired note", bodies["Last report"])
	}
}

// TestProjectRoutineOffersNoConsentPair pins the one verb a Project routine does
// not have: it carries no pause bit (ADR-0138), so neither direction of the
// consent pair is offered over it, while every other verb applies.
func TestProjectRoutineOffersNoConsentPair(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	mkdirs(t, checkout)
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	writeProjectRoutine(t, checkout, "audit", "Audit the logs.\n")

	k := verbKind(t, d, checkout)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "project:audit")
	actions := k.Actions(c)
	if hasVerb(actions, VerbPause) || hasVerb(actions, VerbResume) {
		t.Fatalf("a Project routine offered the consent pair: %v", verbsOffered(actions))
	}
	for _, verb := range []work.Verb{VerbFire, VerbEdit, VerbRefine, VerbRuns, VerbHandoff, work.VerbShell, work.VerbCopyName} {
		if !hasVerb(actions, verb) {
			t.Fatalf("a Project routine must still offer %q: %v", verb, verbsOffered(actions))
		}
	}
}
