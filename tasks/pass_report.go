package tasks

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// passReport is one pass's report family: where its documents accumulate, how
// their names are spelled, and how a rendered document names the pass that
// wrote it. Refine and verification publish reports with the same mechanics and
// different roles (ADR-0245), so the mechanics live here once and each pass is
// a value of this type rather than a second copy of them.
//
// Every report directory sits under the Task-set directory, which lives in
// pop's Work store rather than in the repository, so no report can ever be
// staged into a commit (ADR-0214).
type passReport struct {
	// ArtifactType is the report's type in the Artifact list, and its position
	// in artifactTierOrder.
	ArtifactType string
	// DirName is the set sub-directory the documents accumulate in.
	DirName string
	// FilePrefix opens every document's name; the instant follows it. The
	// instant is in the file name rather than only in the document, because
	// "the latest" is resolved by timestamp and a directory listing must be
	// able to answer that without opening every file.
	FilePrefix string
	// Title heads a rendered document, before the set id.
	Title string
	// WrittenLabel and AgentLabel name the header facts that differ by pass:
	// when it ran, and who wrote it.
	WrittenLabel string
	AgentLabel   string
	// Noun names the pass in the operator-facing errors of filing a document.
	Noun string
	// PointerLabel opens the one-line summary every surface prints the pointer
	// as.
	PointerLabel string
}

// reportFileTimeLayout spells the instant in every report's name, whichever
// pass wrote it: one layout, so a directory of mixed reports still sorts by the
// same key.
const reportFileTimeLayout = "20060102T150405Z"

// passDocument is one report on disk: where it is, when it was written, and
// what it says.
type passDocument struct {
	Path string
	At   time.Time
	Body string
}

// dir is the set sub-directory this pass's reports live in.
func (r passReport) dir(setDir string) string {
	return filepath.Join(setDir, r.DirName)
}

// fileInstant reads one of this pass's report names back as the instant it was
// written, and false for anything else in the directory.
func (r passReport) fileInstant(name string) (time.Time, bool) {
	stamp, ok := strings.CutPrefix(name, r.FilePrefix)
	if !ok {
		return time.Time{}, false
	}
	stamp, ok = strings.CutSuffix(stamp, ".md")
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse(reportFileTimeLayout, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

// latestDocument returns the set's current report of this pass — the one with
// the newest timestamp in its name — and false when the pass has never run.
// Every earlier document stays where it is; superseding is a matter of which
// one the readers take, not of deleting the ones before it (ADR-0214).
func (r passReport) latestDocument(d *Deps, setDir string) (passDocument, bool) {
	dir := r.dir(setDir)
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		return passDocument{}, false
	}
	var found passDocument
	ok := false
	for _, entry := range entries {
		at, isReport := r.fileInstant(entry.Name())
		if entry.IsDir() || !isReport {
			continue
		}
		if ok && !at.After(found.At) {
			continue
		}
		found, ok = passDocument{Path: filepath.Join(dir, entry.Name()), At: at}, true
	}
	if !ok {
		return passDocument{}, false
	}
	body, err := d.FS.ReadFile(found.Path)
	if err != nil {
		return passDocument{}, false
	}
	found.Body = strings.TrimSpace(string(body))
	return found, true
}

// writeDocument files a new report under the pass's own directory and returns
// its path. A name already taken (two reports inside one second) advances the
// instant rather than overwriting: retention is the whole point of the
// directory, and the names are also the ordering.
func (r passReport) writeDocument(d *Deps, setDir string, at time.Time, body string) (string, error) {
	dir := r.dir(setDir)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return "", exitErr(ExitOperational, "create %s directory: %v", r.Noun, err)
	}
	at = at.UTC().Truncate(time.Second)
	taken := map[string]bool{}
	if entries, err := d.FS.ReadDir(dir); err == nil {
		for _, entry := range entries {
			taken[entry.Name()] = true
		}
	}
	name := r.fileName(at)
	for taken[name] {
		at = at.Add(time.Second)
		name = r.fileName(at)
	}
	path := filepath.Join(dir, name)
	if err := d.FS.WriteFile(path, []byte(ensureTrailingNewline(body)), 0o644); err != nil {
		return "", exitErr(ExitOperational, "write %s report: %v", r.Noun, err)
	}
	return path, nil
}

func (r passReport) fileName(at time.Time) string {
	return r.FilePrefix + at.Format(reportFileTimeLayout) + ".md"
}

// renderDocument wraps the pass's prose in the four facts a reader needs to
// know what it describes: which set, at which commits, by whom and when. They
// ride the document rather than a side-car because the document leaves pop the
// moment a human pipes it somewhere.
//
// The Work SHA line is the report's own answer to which tree it describes, so a
// reader who wants the state the report was written against checks out the SHA.
// It is stamped here rather than asked of the agent, because pop read it before
// the pass began and the agent would only be copying it back.
func (r passReport) renderDocument(at time.Time, setID, workSHA, commitRange, agent, body string) string {
	return r.renderDocumentBy(at, setID, workSHA, commitRange, r.AgentLabel, agent, body)
}

// renderDocumentBy is renderDocument with the authorship fact named by the
// caller. A pass whose documents are not all written by its agent — a human
// recording an Accepted verdict writes one of verification's (ADR-0103) — keeps
// the same header of facts in the same order, and says in the last of them who
// wrote it. That one label is how a reader scanning a directory of reports tells
// the two apart.
func (r passReport) renderDocumentBy(at time.Time, setID, workSHA, commitRange, authorLabel, author, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", r.Title, setID)
	fmt.Fprintf(&b, "- %s: %s\n", r.WrittenLabel, at.UTC().Format(time.RFC3339))
	if commitRange != "" {
		fmt.Fprintf(&b, "- Commit range: %s\n", commitRange)
	}
	if workSHA != "" {
		fmt.Fprintf(&b, "- Work SHA: %s\n", ShortSHA(workSHA))
	}
	if strings.TrimSpace(author) != "" {
		fmt.Fprintf(&b, "- %s: %s\n", authorLabel, strings.TrimSpace(author))
	}
	fmt.Fprintf(&b, "\n%s", ensureTrailingNewline(strings.TrimSpace(body)))
	return b.String()
}

// ReportPointer is a set's current report of one pass as the surfaces carry it:
// where the document is and which commit it was written against, never what it
// says (ADR-0240, narrowed by ADR-0217; shared across passes by ADR-0245).
type ReportPointer struct {
	// Label names the pass in the pointer's one-line summary.
	Label string
	// Path is the absolute path of the latest report.
	Path string
	// WorkSHA is the short work SHA the report was written against, empty when
	// the document records none.
	WorkSHA string
	// CommitRange is the range the agent read, empty when the document records
	// none.
	CommitRange string
	// Written is the instant in the report's name, RFC3339 in UTC.
	Written string
}

// latestPointer resolves the pointer to this pass's current report for a set,
// and false when the pass has never run on it.
func (r passReport) latestPointer(d *Deps, m *Manifest) (ReportPointer, bool) {
	if d == nil {
		d = defaultDeps
	}
	if d == nil || d.FS == nil || m == nil || strings.TrimSpace(m.Dir) == "" {
		return ReportPointer{}, false
	}
	doc, ok := r.latestDocument(d, m.Dir)
	if !ok {
		return ReportPointer{}, false
	}
	p := ReportPointer{Label: r.PointerLabel, Path: doc.Path, Written: doc.At.UTC().Format("2006-01-02 15:04Z")}
	p.WorkSHA = reportHeaderField(doc.Body, "Work SHA")
	p.CommitRange = reportHeaderField(doc.Body, "Commit range")
	return p, true
}

// reportHeaderField reads one of the "- Field: value" facts renderDocument
// stamps at the top of every report. The header is the document's own record
// of what it describes, so reading it back beats keeping a side-car that a
// document copied out of pop would leave behind.
func reportHeaderField(body, field string) string {
	prefix := "- " + field + ": "
	inHeader := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			// The header is the report's first bullet list; anything after it is
			// the agent's prose, which may say anything at all.
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
func (p ReportPointer) CommitPhrase() string {
	switch {
	case p.WorkSHA != "" && p.CommitRange != "":
		return fmt.Sprintf("%s (%s)", p.WorkSHA, p.CommitRange)
	case p.WorkSHA != "":
		return p.WorkSHA
	default:
		return p.CommitRange
	}
}

// Summary is the single line a gate preamble uses: the commit the pass read,
// when, and where to read the report.
func (p ReportPointer) Summary() string {
	var b strings.Builder
	b.WriteString(p.Label)
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
func (p ReportPointer) StaleAgainst(workSHA string) bool {
	written, current := p.WorkSHA, ShortSHA(workSHA)
	if written == "" || current == "" {
		return false
	}
	// Either side may be the shorter abbreviation — a hand-edited header, or a
	// document written when the shortening differed — so a common prefix counts
	// as the same commit.
	return !strings.HasPrefix(current, written) && !strings.HasPrefix(written, current)
}
