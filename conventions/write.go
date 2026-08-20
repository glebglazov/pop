package conventions

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// This file is the whole write side of the Convention stack: one writer, taking
// the rank it lands in as an argument (ADR-0226 decision 6). Pop grew a writer
// per layer, and with four of them the rank a write landed at was implicit in
// which function the caller happened to reach for — the failure mode being an
// authoritative layer written at the wrong rank, which is the class of bug the
// rank work exists to close. Here the rank is a value, so it is stated by the
// command that was run and nothing defaults.

// ErrEmptyConvention refuses a write with nothing in it. A file holding only
// whitespace reads as an absent layer to Resolve, so accepting one would leave
// the human believing they had stated something pop will never print. It is one
// refusal for every rank, because every rank is read by the same reader.
var ErrEmptyConvention = errors.New("refusing to write an empty convention body")

// WritableRanks are the ranks a human may write, best first. The repository's
// document is not among them — it is version-controlled, so it lands through a
// diff somebody reviews — and neither is the shipped rank, which is pop's own
// and lives in the binary.
var WritableRanks = []Origin{OriginProject, OriginGlobal, OriginOverlay}

// RankName is the one word that names a rank where a human has to choose one.
// It is the rank's own name rather than a spelling belonging to any surface, so
// a flag, a refusal and a report cannot call one rank three things.
func (o Origin) RankName() string {
	switch o {
	case OriginProject:
		return "project"
	case OriginGlobal:
		return "global"
	case OriginRepository:
		return "repository"
	case OriginShipped:
		return "shipped"
	case OriginOverlay:
		return "overlay"
	}
	return ""
}

// ParseRank turns the rank a caller named into the rank a write lands in, and
// refuses everything else with what the reader needs to get it right.
//
// Naming nothing is a refusal rather than a default: guessing would write an
// authoritative layer at a rank the human did not choose, and no rank is safe
// enough to guess. Naming the repository's document is refused with its path and
// the reason, because a reader who reached for it asked a reasonable question and
// deserves an answer rather than an unknown-flag error.
func ParseRank(name string, kind Kind) (Origin, error) {
	switch strings.TrimSpace(name) {
	case OriginProject.RankName():
		return OriginProject, nil
	case OriginGlobal.RankName():
		return OriginGlobal, nil
	case OriginOverlay.RankName():
		return OriginOverlay, nil
	case OriginRepository.RankName():
		return "", fmt.Errorf("the repository rank is %s — the team's document, in version control. "+
			"Pop does not write it: a document a team follows should land through a diff somebody "+
			"reviews, not a CLI write. Edit it and commit it. To state your own answer instead, "+
			"write one of %s", filepath.Join("docs", "agents", string(kind)+".md"), rankList())
	case OriginShipped.RankName():
		return "", fmt.Errorf("the shipped rank is pop's own answer, built into the binary and not writable. "+
			"Run `pop conventions default %s` to read it, then write what you keep at one of %s", kind, rankList())
	case "":
		return "", fmt.Errorf("name the rank to write — pop does not guess which layer you meant. The ranks are %s", rankList())
	}
	return "", fmt.Errorf("%q is no rank of the convention stack. The ranks you can write are %s", name, rankList())
}

// rankList spells the writable ranks with what each one reaches, so a refusal
// hands the reader the choice rather than a list of words to go and look up.
func rankList() string {
	parts := make([]string, 0, len(WritableRanks))
	for _, origin := range WritableRanks {
		parts = append(parts, fmt.Sprintf("--%s (%s)", origin.RankName(), origin.Scope()))
	}
	return strings.Join(parts, ", ")
}

// WritablePath is where a write at origin lands. Each rank derives its own path
// — the project's from the git remote, the human's two from their home directory
// — and this is the one place that says which derivation belongs to which rank.
func WritablePath(d *Deps, origin Origin, kind Kind, cwd string) (string, error) {
	switch origin {
	case OriginProject:
		return ProjectPath(d, kind, cwd)
	case OriginGlobal:
		return GlobalPath(d, kind)
	case OriginOverlay:
		return OverlayPath(d, kind)
	}
	return "", fmt.Errorf("the %s rank is not written by pop; the ranks you can write are %s", origin, rankList())
}

// Write puts body at one rank, replacing whatever was there, and reports where
// it landed and whether it displaced a document. It carries no frontmatter:
// every writable rank is the human's own statement, whose origin is not in
// question (ADR-0226 decision 5).
//
// Whether it replaced something matters to the writer: somebody who meant to
// state a convention and instead overwrote what they wrote last month should be
// told so by the same line that confirms the write.
func Write(d *Deps, origin Origin, kind Kind, cwd, body string) (string, bool, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false, ErrEmptyConvention
	}
	path, err := WritablePath(d, origin, kind, cwd)
	if err != nil {
		return "", false, err
	}
	if err := d.fs().MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	_, statErr := d.fs().Stat(path)
	replaced := statErr == nil
	if err := d.fs().WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
		return "", false, err
	}
	return path, replaced, nil
}

// Erase removes one rank's document. Nothing to remove is a fact about the stack
// rather than a failure: the caller asked for a state, and that state already
// holds.
func Erase(d *Deps, origin Origin, kind Kind, cwd string) (string, bool, error) {
	path, err := WritablePath(d, origin, kind, cwd)
	if err != nil {
		return "", false, err
	}
	if _, err := d.fs().Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, false, nil
		}
		return path, false, err
	}
	if err := d.fs().RemoveAll(path); err != nil {
		return path, false, err
	}
	return path, true, nil
}

// Set writes one rank and confirms it, which is the whole of `pop conventions
// set`. The rank comes in as a value, so the report can name it.
func Set(d *Deps, w io.Writer, origin Origin, kind Kind, cwd, body string) error {
	path, replaced, err := Write(d, origin, kind, cwd, body)
	if err != nil {
		return err
	}
	return RenderSet(w, origin, kind, path, replaced)
}

// Unset removes one rank's document and prints the stack that remains. The
// report is not a courtesy: removing a rank can hand the kind to whatever sat
// under it, and printing that on the spot is what stops `unset` being read as
// silencing the kind.
func Unset(d *Deps, w io.Writer, origin Origin, kind Kind, cwd string) error {
	path, removed, err := Erase(d, origin, kind, cwd)
	if err != nil {
		return err
	}
	remaining, err := Resolve(d, kind, cwd)
	if err != nil {
		return err
	}
	return RenderUnset(w, origin, kind, path, removed, remaining)
}
