package tasks

// RefinePointer is a set's current Refine report as the HITL gate and Assist
// prompt carry it. Refine holds no pointer of its own: the shape, its commit
// phrase, its summary line and its staleness check are the shared ones every
// pass's report is carried by (ADR-0245).
type RefinePointer = ReportPointer

// latestRefinePointer resolves the pointer for a set, and false when the set has
// never been refined.
func latestRefinePointer(d *Deps, m *Manifest) (RefinePointer, bool) {
	return refineReport.latestPointer(d, m)
}

// refineBlockView is the pointer as the five attended prompts render it: the
// document to read, the commit it was written against, and whether the checkout
// has since moved past that commit. All five share one prompt fragment, so this
// is the one shape it renders against (ADR-0252).
type refineBlockView struct {
	HasRefine bool
	Path      string
	Commit    string
	OutOfDate bool
}

// refineBlock resolves that view for a set, and the empty view — which renders
// nothing at all — for a set that has never been refined.
func refineBlock(d *Deps, m *Manifest, runtimePath string) refineBlockView {
	if d == nil {
		d = defaultDeps
	}
	p, ok := latestRefinePointer(d, m)
	if !ok {
		return refineBlockView{}
	}
	view := refineBlockView{HasRefine: true, Path: p.Path, Commit: p.CommitPhrase()}
	if d != nil && d.Git != nil {
		view.OutOfDate = p.StaleAgainst(verifyWorkSHA(d, runtimePath))
	}
	return view
}
