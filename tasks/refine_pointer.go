package tasks

import (
	"fmt"
	"strings"
)

// RefinePointer is a set's current Refine report as the HITL gate and Assist
// prompt carry it: where the document is and which commit it was written
// against, never what it says (ADR-0240, narrowed by ADR-0217).
type RefinePointer struct {
	// Path is the absolute path of the latest Refine report.
	Path string
	// WorkSHA is the short work SHA the report was written against, empty when
	// the document records none.
	WorkSHA string
	// CommitRange is the range the Refiner read, empty when the document
	// records none.
	CommitRange string
	// Written is the instant in the report's name, RFC3339 in UTC.
	Written string
}

// latestRefinePointer resolves the pointer for a set, and false when the set has
// never been refined.
func latestRefinePointer(d *Deps, m *Manifest) (RefinePointer, bool) {
	if d == nil {
		d = defaultDeps
	}
	if d == nil || d.FS == nil || m == nil || strings.TrimSpace(m.Dir) == "" {
		return RefinePointer{}, false
	}
	doc, ok := latestRefineDocument(d, m.Dir)
	if !ok {
		return RefinePointer{}, false
	}
	p := RefinePointer{Path: doc.Path, Written: doc.At.UTC().Format("2006-01-02 15:04Z")}
	p.WorkSHA = refineHeaderField(doc.Body, "Work SHA")
	p.CommitRange = refineHeaderField(doc.Body, "Commit range")
	return p, true
}

// refineHeaderField reads one of the "- Field: value" facts renderRefineDocument
// stamps at the top of every report. The header is the document's own record
// of what it describes, so reading it back beats keeping a side-car that a
// document copied out of pop would leave behind.
func refineHeaderField(body, field string) string {
	prefix := "- " + field + ": "
	inHeader := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			// The header is the report's first bullet list; anything after it is
			// the Refiner's prose, which may say anything at all.
			if inHeader {
				return ""
			}
			continue
		}
		inHeader = true
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// CommitPhrase names the commit the document was written against, in the one
// form every surface prints it. Empty when the report recorded neither a work
// SHA nor a range — an old or hand-edited document still shows its path.
func (p RefinePointer) CommitPhrase() string {
	switch {
	case p.WorkSHA != "" && p.CommitRange != "":
		return fmt.Sprintf("%s (%s)", p.WorkSHA, p.CommitRange)
	case p.WorkSHA != "":
		return p.WorkSHA
	default:
		return p.CommitRange
	}
}

// Summary is the single line the gate preamble uses: the commit refined, when,
// and where to read it.
func (p RefinePointer) Summary() string {
	var b strings.Builder
	b.WriteString("Refine")
	if phrase := p.CommitPhrase(); phrase != "" {
		fmt.Fprintf(&b, " of %s", phrase)
	}
	if p.Written != "" {
		fmt.Fprintf(&b, ", written %s", p.Written)
	}
	fmt.Fprintf(&b, ": %s", p.Path)
	return b.String()
}

// StaleAgainst reports whether the checkout has moved past the commit this
// report was written against. Unknown on either side is not staleness: a
// document recording no work SHA, or a checkout whose HEAD pop cannot read, says
// nothing about whether the report still describes today's files.
func (p RefinePointer) StaleAgainst(workSHA string) bool {
	written, current := p.WorkSHA, ShortSHA(workSHA)
	if written == "" || current == "" {
		return false
	}
	// Either side may be the shorter abbreviation — a hand-edited header, or a
	// document written when the shortening differed — so a common prefix counts
	// as the same commit.
	return !strings.HasPrefix(current, written) && !strings.HasPrefix(written, current)
}

// refineBlockView is the pointer as the five attended prompts render it: the
// document to read, the commit it was written against, and whether the checkout
// has since moved past that commit. All five share one prompt fragment, so this
// is the one shape it renders against (ADR-0240).
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
