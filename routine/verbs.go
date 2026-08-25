package routine

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/work"
)

// The Routine kind's verbs. Every one of them is kind-local: only copy-name and
// shell are shared across kinds, so nothing a Routine does is promoted into the
// Work seam's vocabulary. Ids are stable strings and keys follow ADR-0158's case
// rule — an uppercase key hands the operator to a pane and leaves the surface, a
// lowercase one acts in place and leaves the dashboard standing.
//
// The verbs are answered per container when a menu opens (never carried on the
// row), so what they offer is as fresh as the keypress: a Routine paused in
// another pane offers `resume`, and one whose newest run wrote no report offers no
// copy-report-path at all.
const (
	// VerbFire fires the Routine now, in its own tagged pane, and takes the
	// operator there: a spawn nobody is shown is a spawn that reads as a no-op
	// (ADR-0158), which is why the key is an uppercase one like every other verb
	// that starts work.
	VerbFire work.Verb = "fire"
	// VerbPause and VerbResume are the two directions of the Routine's one
	// consent bit. They are separate verbs rather than one toggle because a menu
	// that says "pause" must never resume: the label and the act are the same
	// fact, read off the container when the menu opens.
	VerbPause  work.Verb = "pause"
	VerbResume work.Verb = "resume"
	// VerbPreview takes the operator to the pane the Routine's fire is running
	// in.
	VerbPreview work.Verb = "preview"
	// VerbEdit opens the Routine's prompt in the operator's editor.
	VerbEdit work.Verb = "edit-prompt"
	// VerbRefine spawns the Routine refinement loop — the interactive gate that
	// lives outside the Work model (ADR-0125/0132).
	VerbRefine work.Verb = "refine"
	// VerbRuns opens the Routine's run history: the container's items, in the
	// generic detail view.
	VerbRuns work.Verb = "runs"
	// VerbHandoff copies the Routine's continuation prompt (ADR-0134).
	VerbHandoff work.Verb = "handoff"
	// VerbCopyReportPath copies a run report's absolute path — the newest run's
	// over a container, that run's over an item.
	VerbCopyReportPath work.Verb = "copy-report-path"
)

// noReportMessage is what a copy-report-path with nothing to copy says: a
// never-fired Routine, a skipped run, or a run still in flight.
const noReportMessage = "no report to copy"

// Actions returns the container-level verbs that apply to one Routine right now,
// spawning (handoff) verbs first and in-place verbs last: `I P E R O` then
// `a l h`. A Routine whose definition would not load offers none of them: every
// one reads or writes a definition that is not there, and offering one would
// promise an act that can only fail — such a row is still copyable, on the copy
// menu's own key (ADR-0236 decision 6). A Project routine carries no pause bit
// (ADR-0138), so the consent pair is absent from it — the same filtering the
// Routine action menu has always done, now answered by the kind that knows why.
//
// Neither copy verb is here. What a Routine can be copied as is its CopyActions,
// reached from the row list rather than from inside this list (decision 6).
//
// Capability audit (ADR-0215 decision 5). Nothing here is plural: fire, preview,
// edit and refine each hand the operator to a pane; the pause pair is one bit per
// Routine, so a mixed set has no single direction to drive; runs opens one
// history; and handoff names one Routine's text, which the surface has nowhere to
// put beside another's. The one plural verb a Routine has is copy-name, which
// lives on the copy menu.
func (k *Kind) Actions(c work.Container) []work.Action {
	if c.Broken {
		return nil
	}
	actions := []work.Action{
		{Verb: VerbFire, Key: "I", Label: "fire now"},
		{Verb: VerbPreview, Key: "P", Label: "preview pane"},
		{Verb: VerbEdit, Key: "E", Label: "edit prompt"},
		{Verb: VerbRefine, Key: "R", Label: "refine"},
		{Verb: work.VerbShell, Key: "O", Label: "shell"},
	}
	if !projectRoutineContainer(c) {
		if c.RoutinePaused {
			actions = append(actions, work.Action{Verb: VerbResume, Key: "a", Label: "resume"})
		} else {
			actions = append(actions, work.Action{Verb: VerbPause, Key: "a", Label: "pause"})
		}
	}
	actions = append(actions,
		work.Action{Verb: VerbRuns, Key: "l", Label: "runs"},
		work.Action{Verb: VerbHandoff, Key: "h", Label: "handoff prompt"},
	)
	return actions
}

// CopyActions returns what a Routine can be put on the clipboard as: its name and,
// when its newest run wrote one, that run's report path. A Routine has no folder
// of its own to offer in `y`'s place — it is a prompt in a directory it shares —
// so the report keeps the `p` the action menu gave it, and a never-fired Routine
// simply omits the line (ADR-0236 decision 6).
//
// Only copy-name is plural: two Routines' reports have nowhere to go together
// (ADR-0215 decision 5).
func (k *Kind) CopyActions(c work.Container) []work.Action {
	actions := []work.Action{{Verb: work.VerbCopyName, Key: "n", Label: "copy name", Modes: work.Plural}}
	if c.RoutineLastReport != "" {
		actions = append(actions, work.Action{Verb: VerbCopyReportPath, Key: "p", Label: "copy report path"})
	}
	return actions
}

// StatusActions returns nothing: a Routine has no status to write. Its one
// enable/disable state is the pause bit, which is a row verb on `a` because
// pausing is the act itself and not a submenu of variants (ADR-0138), and a
// Routine is never archived — an obsolete Routine is deleted. A kind that offers
// no status verb also offers no status opener, so `s` stays a dead key on a
// Routine row rather than opening an empty submenu.
func (k *Kind) StatusActions(c work.Container) []work.Action { return nil }

// ItemActions returns the verbs for one run. A run is a record, not a thing to
// act on: what a reader wants from it is where its report is — the same
// copy-report-path verb the row offers over the newest run — and its name. Both
// are singular, like every item verb: a Selection marks containers, and
// item-level bulk is out of scope by decision (ADR-0215).
func (k *Kind) ItemActions(c work.Container, item work.Item) []work.Action {
	var actions []work.Action
	if item.File != "" {
		actions = append(actions, work.Action{Verb: VerbCopyReportPath, Key: "p", Label: "copy report path"})
	}
	return append(actions, work.Action{Verb: work.VerbCopyName, Key: "y", Label: "copy name"})
}

// Perform runs one Routine verb. Every verb completes here except the three that
// cannot: firing, editing and refining each need a pane the caller hands the
// operator to (ADR-0158 keeps `tea.ExecProcess` out of the Work dashboard), so
// they spawn their pane and return it as a handoff for the caller to focus.
func (k *Kind) Perform(c work.Container, item *work.Item, verb work.Verb) (work.Outcome, error) {
	d := k.kd.Routine
	switch verb {
	case work.VerbCopyName:
		payload := c.ID
		if item != nil {
			payload = item.ID
		}
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: payload, Message: "copied " + payload}, nil
	case work.VerbShell:
		dir := strings.TrimSpace(c.Checkout)
		if dir == "" {
			return work.Outcome{}, fmt.Errorf("routine %q has no directory to open a shell in", c.ID)
		}
		return work.Outcome{
			Kind:    work.OutcomeHandoff,
			Handoff: work.Handoff{Kind: work.HandoffTmux, Dir: dir},
			Message: "shell in " + dir,
		}, nil
	case VerbFire:
		paneID, err := FirePaneWith(d, c.ID)
		if err != nil {
			return work.Outcome{}, err
		}
		return paneHandoff(paneID, fmt.Sprintf("fired %s", c.ID)), nil
	case VerbPause:
		if _, err := PauseWith(d, c.ID); err != nil {
			return work.Outcome{}, err
		}
		return work.Outcome{Kind: work.OutcomeRefresh, Message: fmt.Sprintf("paused %s", c.ID)}, nil
	case VerbResume:
		if _, err := ResumeWith(d, c.ID); err != nil {
			return work.Outcome{}, err
		}
		return work.Outcome{Kind: work.OutcomeRefresh, Message: fmt.Sprintf("resumed %s", c.ID)}, nil
	case VerbPreview:
		paneID, err := RunPaneWith(d, c.ID)
		if err != nil {
			return work.Outcome{}, err
		}
		if paneID == "" {
			return work.Outcome{Kind: work.OutcomeMessage, Message: fmt.Sprintf("no run pane for %s", c.ID)}, nil
		}
		return paneHandoff(paneID, fmt.Sprintf("run pane for %s", c.ID)), nil
	case VerbEdit:
		// The pause is the run-affecting edit chokepoint (ADR-0128): an edited
		// prompt must be re-proven by a manual fire. It is applied before the editor
		// opens rather than after, because the editor now outlives the surface that
		// spawned it — and an edit the operator abandons paused the Routine under the
		// old in-place editor too, so nothing about the outcome changes.
		if !projectRoutineContainer(c) {
			if err := pauseAfterEdit(d, c.ID); err != nil {
				return work.Outcome{}, err
			}
		}
		paneID, err := EditPromptPaneWith(d, c.ID)
		if err != nil {
			return work.Outcome{}, err
		}
		return paneHandoff(paneID, fmt.Sprintf("editing prompt for %s", c.ID)), nil
	case VerbRefine:
		paneID, err := RefinePaneWith(d, c.ID, "")
		if err != nil {
			return work.Outcome{}, err
		}
		return paneHandoff(paneID, fmt.Sprintf("refining %s", c.ID)), nil
	case VerbRuns:
		return work.Outcome{Kind: work.OutcomeDetail, Message: fmt.Sprintf("runs of %s", c.ID)}, nil
	case VerbHandoff:
		prompt, err := buildHandoff(d, c.ID)
		if err != nil {
			return work.Outcome{}, err
		}
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: prompt, Message: "copied handoff prompt"}, nil
	case VerbCopyReportPath:
		path := c.RoutineLastReport
		if item != nil {
			path = item.File
		}
		if path == "" {
			return work.Outcome{Kind: work.OutcomeMessage, Message: noReportMessage}, nil
		}
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: path, Message: "copied report path"}, nil
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
}

// paneHandoff is the outcome of a verb that spawned or found a pane: the caller
// focuses it and leaves, which is the whole of ADR-0158's handoff sequence.
func paneHandoff(paneID, message string) work.Outcome {
	return work.Outcome{
		Kind:    work.OutcomeHandoff,
		Handoff: work.Handoff{Kind: work.HandoffTmux, Target: paneID},
		Message: message,
	}
}

// projectRoutineContainer reports whether a container is a Project routine, read
// off the id form its discovery stamped (ADR-0138) rather than a second flag: the
// id is what every verb addresses it by.
func projectRoutineContainer(c work.Container) bool {
	_, ok := parseProjectRef(c.ID)
	return ok
}
