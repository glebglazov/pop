package conventions

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrEmptyConvention refuses a write with nothing in it. A memory file holding
// only frontmatter reads as an absent layer to Resolve, so accepting one would
// leave the caller believing pop remembers something it will never print.
var ErrEmptyConvention = errors.New("refusing to write an empty convention body")

// ErrNoDerivation refuses a write that does not say where the convention came
// from. Provenance is the whole reason Convention memory carries frontmatter:
// the disclosure line quotes it, so a layer pop wrote without one would make
// the summary lie by omission (ADR-0211).
var ErrNoDerivation = errors.New("a convention written to pop memory must record what it was derived from")

// timestampLayout is how a write dates itself: to the minute, in the machine's
// own zone, because the reader of the provenance line is deciding whether a
// derivation is stale, not diffing instants.
const timestampLayout = "2006-01-02 15:04 MST"

// Set writes the Convention memory layer for kind in the repository owning cwd
// and reports where it landed. It is the only writer of that layer: body is
// what an agent derived, derivedFrom is the evidence it derived it from, and
// the two are stored together so the provenance a reader is shown cannot drift
// from the prose it describes.
func Set(d *Deps, w io.Writer, kind Kind, cwd, body, derivedFrom string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return ErrEmptyConvention
	}
	derivedFrom = oneLine(derivedFrom)
	if derivedFrom == "" {
		return ErrNoDerivation
	}

	path, err := MemoryPath(d, kind, cwd)
	if err != nil {
		return err
	}
	if err := d.fs().MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// Whether this replaces something matters to the writer: an agent that meant
	// to add a convention and instead overwrote yesterday's derivation should be
	// told so by the same line that confirms the write.
	_, statErr := d.fs().Stat(path)
	replaced := statErr == nil

	derivedAt := d.now().Format(timestampLayout)
	doc := fmt.Sprintf("---\nderived_from: %s\nderived_at: %s\n---\n\n%s\n", derivedFrom, derivedAt, body)
	if err := d.fs().WriteFile(path, []byte(doc), 0o644); err != nil {
		return err
	}
	return RenderSet(w, kind, path, derivedFrom, derivedAt, replaced)
}

// Unset removes the Convention memory layer for kind and prints the stack that
// remains. The report is not a courtesy: under composition a removed layer
// usually leaves the kind answering anyway, and printing the survivors on the
// spot is what stops `unset` being read as silencing the kind (ADR-0211).
func Unset(d *Deps, w io.Writer, kind Kind, cwd string) error {
	path, err := MemoryPath(d, kind, cwd)
	if err != nil {
		return err
	}

	// Nothing to remove is a fact about the stack, not a failure: the caller
	// asked for a state, and that state already holds.
	removed := true
	if _, err := d.fs().Stat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = false
	} else if err := d.fs().RemoveAll(path); err != nil {
		return err
	}

	remaining, err := Resolve(d, kind, cwd)
	if err != nil {
		return err
	}
	return RenderUnset(w, kind, path, removed, remaining)
}

// oneLine flattens a derivation to something a `key: value` frontmatter line
// can hold and a one-line summary can quote.
func oneLine(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
