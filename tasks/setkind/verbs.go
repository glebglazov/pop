package setkind

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// The Task-set kind's verbs. Ids are stable strings, keys follow ADR-0158's case
// rule (uppercase hands off, lowercase acts in place), and Actions orders
// spawning verbs before in-place ones.
const (
	VerbDrain     work.Verb = "drain"
	VerbVerify    work.Verb = "verify"
	VerbBind      work.Verb = "bind"
	VerbUnbind    work.Verb = "unbind"
	VerbAutoDrain work.Verb = "auto-drain"
	VerbAssist    work.Verb = "assist"
	VerbFold      work.Verb = "fold"
	VerbUnpark    work.Verb = "unpark"
	VerbArchive   work.Verb = "archive"
	// VerbUnarchive restores an archived set to the default view. It is offered in
	// the status submenu only: the row-level `x` is the archive half, and a row
	// reachable to unarchive is one the show-archived filter is already on for.
	VerbUnarchive work.Verb = "unarchive"
	// VerbCopyPath copies the bound worktree's path to the clipboard. In-place
	// (lowercase) like copy-name, and hidden rather than shown-and-erroring on an
	// unbound set — the same gate unbind uses.
	VerbCopyPath work.Verb = "copy-path"

	// The three task-status writes, offered twice over: as item verbs filtered to
	// one task's status the way the task action menu has always filtered them, and
	// as status-submenu verbs over the whole set. Which one Perform runs is which
	// one it was handed — an item, or the container alone.
	VerbComplete work.Verb = "complete"
	VerbOpen     work.Verb = "open"
	VerbSkip     work.Verb = "skip"
)

// Actions returns the container-level verbs that apply to one task set right now,
// spawning (handoff) verbs first and in-place verbs last: `I V F S O` then
// `b u a s r x y p`, mirroring the order handoffAfterLaunch already names (drain,
// verify, fold, assist, shell) so the two lists never drift apart. Conditional
// verbs are filtered to the set's context: verify only for NEEDS-VERIFY /
// VERIFY-FAILED sets with no live drain, fold only for a bound terminal set,
// unbind only for bound sets, auto-drain only for non-orphaned sets, unpark only
// for parked sets, and copy-path only for bound sets. Drain, assist, the runtime
// shell, bind, status, archive and copy-name apply to every set regardless of
// status.
//
// It is called when a menu opens over one container, not per container at load
// time, so the eligibility it reports is as fresh as the keypress.
func (k *Kind) Actions(c work.Container) []work.Action {
	actions := []work.Action{{Verb: VerbDrain, Key: "I", Label: "drain"}}
	// Verify is the lighter, explicit Verifier force (ADR-0123): offered only on
	// sets a verdict can move (NEEDS-VERIFY / VERIFY-FAILED) and hidden while a
	// live drain holds the set — a plain verify is not quiescence-gated, so the
	// running drain verifies itself instead.
	if verifyEligible(c) {
		actions = append(actions, work.Action{Verb: VerbVerify, Key: "V", Label: "verify"})
	}
	if foldEligible(c) {
		actions = append(actions, work.Action{Verb: VerbFold, Key: "F", Label: "fold"})
	}
	actions = append(actions,
		work.Action{Verb: VerbAssist, Key: "S", Label: "assist"},
		work.Action{Verb: work.VerbShell, Key: "O", Label: "shell"},
		work.Action{Verb: VerbBind, Key: "b", Label: "bind worktree"},
	)
	if c.Bound {
		actions = append(actions, work.Action{Verb: VerbUnbind, Key: "u", Label: "unbind worktree"})
	}
	if !c.Orphaned {
		actions = append(actions, work.Action{Verb: VerbAutoDrain, Key: "a", Label: "auto-drain"})
	}
	actions = append(actions, work.Action{Verb: work.VerbStatus, Key: "s", Label: "status ▸"})
	if c.Parked {
		actions = append(actions, work.Action{Verb: VerbUnpark, Key: "r", Label: "unpark"})
	}
	actions = append(actions,
		work.Action{Verb: VerbArchive, Key: "x", Label: "archive"},
		work.Action{Verb: work.VerbCopyName, Key: "y", Label: "copy name"},
	)
	if c.Bound {
		actions = append(actions, work.Action{Verb: VerbCopyPath, Key: "p", Label: "copy path"})
	}
	return actions
}

// StatusActions returns the set's status submenu — the five writes it has always
// carried, on the same keys, in the same order: complete, open and skip over every
// unlocked task in the set, then the archive pair. They live here rather than on
// the dashboard because they are this kind's status vocabulary and no other's
// (ADR-0186); the keys and labels are unchanged from the hardcoded list they
// replace, because an operator's fingers are part of the interface.
func (k *Kind) StatusActions(c work.Container) []work.Action {
	return []work.Action{
		{Verb: VerbComplete, Key: "c", Label: "complete"},
		{Verb: VerbOpen, Key: "o", Label: "open"},
		{Verb: VerbSkip, Key: "s", Label: "skip"},
		{Verb: VerbArchive, Key: "x", Label: "archive"},
		{Verb: VerbUnarchive, Key: "u", Label: "unarchive"},
	}
}

// ItemActions returns the verbs applicable to one task, filtered to its status:
// complete for anything not already done, open for any reopenable task (mirroring
// CanReopen), skip for an open task, and copy-name always.
func (k *Kind) ItemActions(c work.Container, item work.Item) []work.Action {
	var actions []work.Action
	status := tasks.TaskStatus(item.Status)
	if status != tasks.TaskDone {
		actions = append(actions, work.Action{Verb: VerbComplete, Key: "c", Label: "complete"})
	}
	if tasks.CanReopen(status) {
		actions = append(actions, work.Action{Verb: VerbOpen, Key: "o", Label: "open"})
	}
	if status == tasks.TaskOpen {
		actions = append(actions, work.Action{Verb: VerbSkip, Key: "s", Label: "skip"})
	}
	return append(actions, work.Action{Verb: work.VerbCopyName, Key: "y", Label: "copy name"})
}

// verifyEligible reports whether the verify verb applies to a set: one a verdict
// can still move that no live drain holds (ADR-0123). The mark, not the status, is
// the fact — a human-completed set reads DONE and still needs verifying, so
// keying on NEEDS-VERIFY / VERIFY-FAILED would hide the verb from exactly the set
// whose verification was deferred.
func verifyEligible(row work.Container) bool {
	if row.LiveDrain {
		return false
	}
	return row.VerifyMark == tasks.VerifyMarkUnverified || row.VerifyMark == tasks.VerifyMarkFailed
}

// foldEligible reports whether the fold verb applies to a set: the shared
// Unfolded condition (provisioned binding, DONE or AWAITING-APPROVAL —
// ADR-0148, ADR-0156, ADR-0197).
func foldEligible(row work.Container) bool {
	return tasks.Unfolded(row.Bound, row.Provisioned, row.RawStatus)
}

// Perform runs one verb. The shared verbs, every status write (per task, per set,
// and the archive pair) complete here; the set's remaining menu verbs hand back to
// the caller, which still owns their dispatch — the drain picker, the bind picker
// and the abandon confirm are modal, and moving them behind Perform needs a
// modal-capable Outcome (deferred by decision, not by accident).
func (k *Kind) Perform(c work.Container, item *work.Item, verb work.Verb) (work.Outcome, error) {
	switch verb {
	case work.VerbCopyName:
		payload := c.ID
		if item != nil {
			// The paste-ready Task target reference: the <task-set>/<file>.md form
			// `pop tasks implement/complete/open` accept. The item carries the file's
			// absolute path, so the reference is its base name under the set.
			payload = TaskRef(c.ID, *item)
		}
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: payload, Message: "copied " + payload}, nil
	case VerbCopyPath:
		path := strings.TrimSpace(c.RuntimePath)
		if path == "" {
			return work.Outcome{}, fmt.Errorf("setkind: %s is not bound to a worktree", c.ID)
		}
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: path, Message: "copied " + path}, nil
	case work.VerbShell:
		dir := strings.TrimSpace(c.Checkout)
		if dir == "" {
			return work.Outcome{}, fmt.Errorf("setkind: %s has no checkout to open a shell in", c.ID)
		}
		return work.Outcome{
			Kind:    work.OutcomeHandoff,
			Handoff: work.Handoff{Kind: work.HandoffTmux, Dir: dir},
			Message: "shell in " + dir,
		}, nil
	case VerbComplete, VerbOpen, VerbSkip:
		// One task when the caller named one, the whole set when it did not: the
		// status submenu's complete/open/skip have always written every unlocked task
		// in the set, and that is what "no item" means here.
		if item == nil {
			if err := k.applySetVerb(c, verb); err != nil {
				return work.Outcome{}, err
			}
			return work.Outcome{Kind: work.OutcomeRefresh, Message: fmt.Sprintf("%s %s", verb, c.ID)}, nil
		}
		if err := k.applyTaskVerb(c, *item, verb); err != nil {
			return work.Outcome{}, err
		}
		return work.Outcome{Kind: work.OutcomeRefresh, Message: fmt.Sprintf("%s %s/%s", verb, c.ID, item.ID)}, nil
	case VerbArchive, VerbUnarchive:
		archived := verb == VerbArchive
		if err := k.setArchived(c, archived); err != nil {
			return work.Outcome{}, err
		}
		word := "archived"
		if !archived {
			word = "unarchived"
		}
		return work.Outcome{Kind: work.OutcomeRefresh, Message: fmt.Sprintf("%s %s", word, c.ID)}, nil
	case VerbDrain, VerbVerify, VerbBind, VerbUnbind, VerbAutoDrain, work.VerbStatus, VerbAssist, VerbFold, VerbUnpark:
		return work.Outcome{Kind: work.OutcomeCallerModal, Message: string(verb)}, nil
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
}

// TaskRef is the paste-ready `<task-set>/<file>.md` target reference for one
// task item — the form every `pop tasks` verb accepts. It is exported because a
// caller that dispatches a task write of its own (the dashboard's detail
// overrides) must name the task the same way this kind does.
func TaskRef(setID string, item work.Item) string {
	return setID + "/" + filepath.Base(item.File)
}

// applyTaskVerb writes one task's status in-process (ADR-0158): no subprocess, no
// TUI suspend. The set's definition path and checkout come off the container, so
// the write lands in the same repository the container was read from.
func (k *Kind) applyTaskVerb(c work.Container, item work.Item, verb work.Verb) error {
	d := k.d
	loadConfig := k.loadConfig()
	in := resolveInput(c)
	ids := []string{item.ID}
	var err error
	switch verb {
	case VerbComplete:
		_, err = tasks.CompleteTasksWith(d.Tasks, d.Project, loadConfig, tasks.CompleteTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   c.ID,
			SelectedTaskIDs: ids,
		})
	case VerbOpen:
		_, err = tasks.OpenTasksWith(d.Tasks, d.Project, loadConfig, tasks.OpenTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   c.ID,
			SelectedTaskIDs: ids,
		})
	case VerbSkip:
		_, err = tasks.SkipTasksWith(d.Tasks, d.Project, loadConfig, tasks.SkipTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   c.ID,
			SelectedTaskIDs: ids,
		})
	default:
		return work.UnknownVerb(k.ID(), verb)
	}
	return err
}

// resolveInput points a task-set write at the definition the container was read
// from, with the best available checkout as the working directory.
func resolveInput(row work.Container) tasks.ResolveInput {
	cwd := strings.TrimSpace(row.ProjectPath)
	if cwd == "" {
		cwd = strings.TrimSpace(row.RuntimePath)
	}
	if cwd == "" {
		cwd = strings.TrimSpace(row.DefPath)
	}
	return tasks.ResolveInput{DefinitionOverride: row.DefPath, CWD: cwd}
}
