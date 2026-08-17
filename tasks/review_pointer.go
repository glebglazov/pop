package tasks

import (
	"fmt"
	"strings"
)

// ReviewPointer is a set's current Review artifact as the HITL gate and Assist
// prompt carry it: where the document is and which commit it was written
// against, never what it says (ADR-0214, narrowed by ADR-0217).
type ReviewPointer struct {
	// Path is the absolute path of the latest Review artifact.
	Path string
	// WorkSHA is the short work SHA the review was written against, empty when
	// the document records none.
	WorkSHA string
	// CommitRange is the range the Reviewer read, empty when the document
	// records none.
	CommitRange string
	// Written is the instant in the artifact's name, RFC3339 in UTC.
	Written string
}

// latestReviewPointer resolves the pointer for a set, and false when the set has
// never been reviewed.
func latestReviewPointer(d *Deps, m *Manifest) (ReviewPointer, bool) {
	if d == nil {
		d = defaultDeps
	}
	if d == nil || d.FS == nil || m == nil || strings.TrimSpace(m.Dir) == "" {
		return ReviewPointer{}, false
	}
	doc, ok := latestReviewDocument(d, m.Dir)
	if !ok {
		return ReviewPointer{}, false
	}
	p := ReviewPointer{Path: doc.Path, Written: doc.At.UTC().Format("2006-01-02 15:04Z")}
	p.WorkSHA = reviewHeaderField(doc.Body, "Work SHA")
	p.CommitRange = reviewHeaderField(doc.Body, "Commit range")
	return p, true
}

// reviewHeaderField reads one of the "- Field: value" facts renderReviewDocument
// stamps at the top of every artifact. The header is the document's own record
// of what it describes, so reading it back beats keeping a side-car that a
// document copied out of pop would leave behind.
func reviewHeaderField(body, field string) string {
	prefix := "- " + field + ": "
	inHeader := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			// The header is the artifact's first bullet list; anything after it is
			// the Reviewer's prose, which may say anything at all.
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
// form every surface prints it. Empty when the artifact recorded neither a work
// SHA nor a range — an old or hand-edited document still shows its path.
func (p ReviewPointer) CommitPhrase() string {
	switch {
	case p.WorkSHA != "" && p.CommitRange != "":
		return fmt.Sprintf("%s (%s)", p.WorkSHA, p.CommitRange)
	case p.WorkSHA != "":
		return p.WorkSHA
	default:
		return p.CommitRange
	}
}

// Summary is the single line the gate preamble uses: the commit reviewed, when,
// and where to read it.
func (p ReviewPointer) Summary() string {
	var b strings.Builder
	b.WriteString("Code review")
	if phrase := p.CommitPhrase(); phrase != "" {
		fmt.Fprintf(&b, " of %s", phrase)
	}
	if p.Written != "" {
		fmt.Fprintf(&b, ", written %s", p.Written)
	}
	fmt.Fprintf(&b, ": %s", p.Path)
	return b.String()
}
