package binding

import (
	"fmt"
	"io"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// boundFoldSet is one live Worktree binding on the checkout a checkout-addressed
// fold is about to land: the set holding it, the status that decides what the fold
// owes that set, and the key its release needs. A binding is pop's own bookkeeping
// and never a reason git cannot rebase, so none of this refuses the fold — it
// decides what the fold says and what it settles afterwards (ADR-0233).
type boundFoldSet struct {
	key   string
	setID string
	// runtimePath is the binding's own record of the checkout, which the sign-off
	// writes through.
	runtimePath string
	status      tasks.TaskSetStatus
	openHITL    []tasks.Task
	// statusErr holds why the set's status could not be read at all. Such a set
	// counts as unfinished: pop cannot show it is finished, and refusing on a
	// binding pop cannot read is still refusing on a binding.
	statusErr error
}

// finished reports whether this fold is the set's ending — the only case where the
// fold owes it a sign-off and the release of its binding.
func (s boundFoldSet) finished() bool {
	return s.statusErr == nil && tasks.FoldEligibleStatus(s.status)
}

func (s boundFoldSet) statusLabel() string {
	if s.statusErr != nil {
		return "status unreadable"
	}
	return string(s.status)
}

// consequence says what this fold does to the set, in the same shape for every
// status: what it settles, or what it leaves standing.
func (s boundFoldSet) consequence() string {
	if !s.finished() {
		return "binding kept; the checkout stays where it carries on"
	}
	if len(s.openHITL) == 0 {
		return "signed off and its binding released"
	}
	labels := make([]string, len(s.openHITL))
	for i, task := range s.openHITL {
		labels[i] = tasks.FoldSignOffTaskLabel(task)
	}
	return "signed off (completing " + strings.Join(labels, ", ") + ") and its binding released"
}

// boundFoldSets are every set bound to one folding checkout, in the order the
// binding store reports them.
type boundFoldSets []boundFoldSet

// resolveBoundFoldSets reads the checkout's live bindings and the status of each
// set holding one. It is asked on every fold attempt, because a status the human
// is about to answer for must be the status on disk now.
func resolveBoundFoldSets(td *tasks.Deps, cfg *config.Config, path string) (boundFoldSets, error) {
	ids, err := LiveBoundSetIDs(td, path)
	if err != nil {
		return nil, fmt.Errorf("fold refused: inspect worktree binding: %w", err)
	}
	sets := make(boundFoldSets, 0, len(ids))
	for _, setID := range ids {
		key, b, ok, err := FindBySetID(td, setID)
		if err != nil {
			return nil, fmt.Errorf("fold refused: read binding for %s: %w", setID, err)
		}
		if !ok {
			continue
		}
		s := boundFoldSet{key: key, setID: setID, runtimePath: strings.TrimSpace(b.RuntimePath)}
		if s.runtimePath == "" {
			s.runtimePath = path
		}
		status, manifest, err := foldSetRowStatus(td, cfg, setID, s.runtimePath)
		switch {
		case err != nil:
			s.statusErr = err
		case status == tasks.StatusAwaitingApproval && (manifest == nil || !manifest.Valid):
			// A sign-off needs a manifest to write through, so a set that awaits one
			// without a readable manifest is malformed rather than foldable.
			s.status = tasks.StatusMalformed
		case status == tasks.StatusAwaitingApproval:
			s.status = status
			s.openHITL = tasks.OpenHITLTasks(manifest)
		default:
			s.status = status
		}
		sets = append(sets, s)
	}
	return sets, nil
}

// confirm is the Bound-checkout fold confirmation: one question for the whole
// checkout, naming every bound set with the status found and what the fold will do
// about it. With no bindings it is the plain checkout question, unchanged.
func (sets boundFoldSets) confirm(opts FoldOptions, out io.Writer, path string) (bool, error) {
	if len(sets) == 0 {
		return confirmCheckoutFold(opts.In, out, opts.Yes, path)
	}
	confirmed, err := confirmYesNo(opts.In, out, opts.Yes, sets.prompt(path),
		"non-interactive worktree fold requires --yes")
	if err != nil || !confirmed {
		return confirmed, err
	}
	if opts.Yes {
		sets.reportUnasked(out, path)
	}
	return true, nil
}

func (sets boundFoldSets) prompt(path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Worktree %s is bound to:\n", path)
	for _, s := range sets {
		fmt.Fprintf(&b, "  - Task set %s (%s) — %s\n", s.setID, s.statusLabel(), s.consequence())
	}
	fmt.Fprintf(&b, "Fold worktree %s into trunk? [y/N]: ", path)
	return b.String()
}

// reportUnasked leaves in the output the record the confirmation would have been.
// --yes is the entry ticket for every non-interactive channel rather than an
// extra-danger opt-in, so pop obeys it and states what it landed unasked instead of
// passing over it in silence (ADR-0233).
func (sets boundFoldSets) reportUnasked(out io.Writer, path string) {
	for _, s := range sets {
		fmt.Fprintf(out, "--yes: folding %s without asking; bound Task set %s is %s — %s\n",
			path, s.setID, s.statusLabel(), s.consequence())
	}
}

// completeTails settles what a finished set's fold settles: the Awaiting-approval
// sign-off and the release of the binding. It runs inside the post-landing sequence,
// so a failure here is marked by the Fold scratch branch and finished by a re-run.
// An unfinished set is left exactly as it was — the fold did not need its release,
// and taking it would move the set's home.
func (sets boundFoldSets) completeTails(td *tasks.Deps, out io.Writer) error {
	for _, s := range sets {
		if !s.finished() {
			continue
		}
		if err := completeFoldSetTail(td, s.setID, s.runtimePath, s.key, s.status); err != nil {
			return err
		}
		fmt.Fprintf(out, "Released worktree binding for %s\n", s.setID)
	}
	return nil
}
