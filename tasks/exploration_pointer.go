package tasks

import (
	"path/filepath"
	"strings"
	"time"
)

// explorationBlockView is the set's Exploration report as the prompts that hand
// it over render it: the document to read, never a word of what it says. One
// pass writes one file (ADR-0262), so there is no timestamp to print and nothing
// to be superseded by — where it is, is the whole pointer.
type explorationBlockView struct {
	HasExploration bool
	Path           string
}

// explorationBlock resolves that view for a set, and the empty view — which
// renders nothing at all — for a set that has never been explored.
func explorationBlock(d *Deps, setDir string) explorationBlockView {
	doc, ok := explorationDocument(d, setDir)
	if !ok {
		return explorationBlockView{}
	}
	return explorationBlockView{HasExploration: true, Path: doc.Path}
}

// explorationDocument is the one reader of a set's Exploration report: where it
// is, what it says, and when the pass that wrote it ran. Everything that asks
// whether a set is explored — the builders' prompts, the Exploration mark, the park
// — comes through here, so "explored" means one thing across the feature.
//
// An unreadable or empty document is a set with no report, the way an empty
// spec is (readSpec): the builders after the pass are handed a path only when
// there is a map at the end of it.
//
// The instant comes from the document's own header rather than from its name:
// one pass rewrites one file, so the name carries no timestamp to read.
func explorationDocument(d *Deps, setDir string) (passDocument, bool) {
	if d == nil {
		d = defaultDeps
	}
	if d == nil || d.FS == nil || strings.TrimSpace(setDir) == "" {
		return passDocument{}, false
	}
	path := filepath.Join(setDir, ExplorationFileName)
	body, err := d.FS.ReadFile(path)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		return passDocument{}, false
	}
	doc := passDocument{Path: path, Body: strings.TrimSpace(string(body))}
	if at, err := time.Parse(time.RFC3339, reportHeaderField(doc.Body, explorationReport.WrittenLabel)); err == nil {
		doc.At = at
	}
	return doc, true
}

// ExplorationPointer is a set's Exploration report as the read surfaces carry
// it. Explore holds no pointer of its own: the shape, its commit phrase and its
// summary line are the shared ones every pass's report is carried by
// (ADR-0245).
type ExplorationPointer = ReportPointer

// explorationPointerLabel opens the pointer's one-line summary, where the
// report families' PointerLabel does for theirs.
const explorationPointerLabel = "Exploration"

// latestExplorationPointer resolves the pointer for a set, and false when the
// set has no report. A hand-edited or pre-header document still answers its
// path: the phrase the pointer prints degrades, the path does not.
func latestExplorationPointer(d *Deps, m *Manifest) (ExplorationPointer, bool) {
	if m == nil {
		return ExplorationPointer{}, false
	}
	doc, ok := explorationDocument(d, m.Dir)
	if !ok {
		return ExplorationPointer{}, false
	}
	p := ExplorationPointer{
		Label:   explorationPointerLabel,
		Path:    doc.Path,
		WorkSHA: reportHeaderField(doc.Body, "Work SHA"),
	}
	if !doc.At.IsZero() {
		p.Written = doc.At.UTC().Format("2006-01-02 15:04Z")
	}
	return p, true
}
