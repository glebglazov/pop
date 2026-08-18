package conventions

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Get resolves and prints the convention in force for each kind in turn. It
// returns ErrNoConvention when every kind asked about was empty — the miss the
// CLI turns into exit 1 — after the output is already written, so the caller
// has been told where pop looked either way.
func Get(d *Deps, w io.Writer, cwd string, kinds ...Kind) error {
	stacks, err := ResolveAll(d, cwd, kinds...)
	if err != nil {
		return err
	}
	if err := RenderStacks(w, stacks); err != nil {
		return err
	}
	for _, stack := range stacks {
		if !stack.Empty() {
			return nil
		}
	}
	return ErrNoConvention
}

// RenderStacks prints several kinds in turn, separated so a reader can tell
// where one kind's answer ends.
func RenderStacks(w io.Writer, stacks []Stack) error {
	for i, stack := range stacks {
		if i > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, strings.Repeat("=", 72))
			fmt.Fprintln(w)
		}
		if err := RenderStack(w, stack); err != nil {
			return err
		}
	}
	return nil
}

// inForce renders what a reader must follow, as labelled blocks: the one rank
// that answered, then the overlay where there is one. A rank the answer
// displaced is not printed — the question every surface here exists to answer
// is what is in force, and a suppressed layer is not (ADR-0223).
func (s Stack) inForce() string {
	var b strings.Builder
	if answer, ok := s.Answer(); ok {
		fmt.Fprintf(&b, "----- ANSWER: %s (%s) -----\n%s\n\n%s\n",
			strings.ToUpper(string(answer.Origin)), answer.Origin.Scope(), answer.Path, answer.Body)
	}
	if overlay, ok := s.Overlay(); ok {
		if b.Len() > 0 {
			fmt.Fprintln(&b)
		}
		fmt.Fprintf(&b, "----- APPENDED: %s (%s) -----\n%s\n\n%s\n",
			strings.ToUpper(string(overlay.Origin)), overlay.Origin.Scope(), overlay.Path, overlay.Body)
	}
	return b.String()
}

// inForceProse is the whole of what a kind that answers renders to: the blocks
// above, closed by the provenance line. Every surface that shows a convention
// prints this and nothing else about the layers, which is what stops the
// human-facing and agent-facing renderings describing one differently.
func (s Stack) inForceProse() string { return s.inForce() + "\n" + s.Provenance() + "\n" }

// RenderStack prints one kind's convention as plain text: the answer in force,
// the overlay where there is one, and the provenance line. A kind nothing
// answers prints the paths pop consulted instead, because "nothing here" is
// only actionable with the four places named.
func RenderStack(w io.Writer, s Stack) error {
	fmt.Fprintf(w, "CONVENTION %s\n\n", s.Kind)

	if s.Empty() {
		fmt.Fprintf(w, "EMPTY — nothing answers the %s convention. Pop consulted, in resolution order:\n\n", s.Kind)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ORIGIN\tPATH")
		for _, l := range s.Layers {
			fmt.Fprintf(tw, "%s\t%s\n", l.Origin, l.Path)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		// A miss answers with the method rather than stopping at "nothing here":
		// the caller wanted the convention, and the recipe is how it gets one
		// (ADR-0211).
		fmt.Fprintln(w)
		return RenderRecipe(w, s.Kind)
	}

	_, err := io.WriteString(w, s.inForceProse())
	return err
}

// StackPreview renders one kind for a surface that shows it in a pane beside
// values of another sort: the same answer, overlay and provenance `get` prints,
// plus where the layer an editor writes lives.
//
// It returns a string rather than writing to an io.Writer because its consumer
// is a pane and not a stream, and it lives here rather than in that consumer
// because resolution and every rendering are this package's: a preview that
// labelled the answer itself could disagree with what `pop conventions get`
// prints for the same repository (ADR-0212 decision 8).
func StackPreview(s Stack) string {
	var b strings.Builder
	if s.Empty() {
		fmt.Fprintf(&b, "%s — nothing answers it.\n\nPop consulted, in resolution order:\n", s.Kind)
		for _, l := range s.Layers {
			fmt.Fprintf(&b, "  %-14s %s\n", l.Origin, l.Path)
		}
	} else {
		fmt.Fprintf(&b, "%s — what is in force here.\n\n%s", s.Kind, s.inForceProse())
	}
	fmt.Fprintf(&b, "\n%s\n", s.overlayNote())
	return b.String()
}

// StackProse renders one kind as the prose an agent is handed inside a larger
// prompt: the answer, the overlay and the provenance line. It carries no
// heading of its own and names no editing surface — the prompt that embeds it
// owns both.
//
// A kind nothing answers returns false rather than the four paths `get` prints:
// an agent cannot act on where pop looked, and a prompt that recited them would
// read as an instruction to go and write one.
func StackProse(s Stack) (string, bool) {
	if s.Empty() {
		return "", false
	}
	return s.inForceProse(), true
}

// overlayNote says where the human's overlay is and whether it holds anything.
// An editing surface writes that one layer, so a reader deciding whether to
// edit needs to know which of the four they would be changing — and the overlay
// is the one that never displaces an answer.
func (s Stack) overlayNote() string {
	for _, l := range s.Layers {
		if l.Origin != OriginOverlay {
			continue
		}
		if l.Present {
			return "your overlay, edited here:\n" + l.Path
		}
		return "your overlay — not written yet — would be:\n" + l.Path
	}
	return ""
}

// RenderSet confirms a write of the Convention memory layer: which file holds
// it now, the provenance stored beside it, and the reminder that memory is the
// last rank consulted — a writer who thinks it is the answer would never look
// at the document either the human or the team may have written above it.
func RenderSet(w io.Writer, kind Kind, path, derivedFrom, derivedAt string, replaced bool) error {
	verb := "WROTE"
	if replaced {
		verb = "REPLACED"
	}
	fmt.Fprintf(w, "%s pop memory for %s\n\n", verb, kind)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "path\t%s\n", path)
	fmt.Fprintf(tw, "derived from\t%s\n", derivedFrom)
	fmt.Fprintf(tw, "derived at\t%s\n", derivedAt)
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nThis is the pop memory rank — pop's stand-in for a written answer, and the\n"+
		"last one consulted. A ~/.agents/docs/%[1]s.md or a committed docs/agents/%[1]s.md\n"+
		"answers instead of it. Run `pop conventions get %[1]s` for what is in force.\n", kind)
	return nil
}

// RenderUnset reports a removal together with what answers the kind now.
// Removing the winning rank promotes the next one, which is the fact the verb
// exists to report; the stack beneath is printed through the same renderer
// `get` uses, so the two surfaces cannot disagree about what is in effect.
func RenderUnset(w io.Writer, kind Kind, path string, removed bool, remaining Stack) error {
	if removed {
		fmt.Fprintf(w, "REMOVED pop memory for %s\n%s\n\n", kind, path)
	} else {
		fmt.Fprintf(w, "NO pop memory for %s — nothing to remove.\n%s\n\n", kind, path)
	}
	fmt.Fprintf(w, "%s\n\n", nowInForce(remaining))
	return RenderStack(w, remaining)
}

// nowInForce names the rank that answers a kind after a removal, in one line,
// before the whole rendering repeats it at length. A reader told only "removed"
// would have to run `get` to learn which rank was promoted.
func nowInForce(s Stack) string {
	answer, hasAnswer := s.Answer()
	overlay, hasOverlay := s.Overlay()
	switch {
	case !hasAnswer && !hasOverlay:
		return fmt.Sprintf("Nothing answers %s now.", s.Kind)
	case !hasAnswer:
		return fmt.Sprintf("Now in force: your overlay alone (%s).", overlay.Path)
	}
	line := fmt.Sprintf("Now in force: %s (%s).", answer.Origin, answer.Path)
	if hasOverlay {
		line += fmt.Sprintf(" Your overlay is appended (%s).", overlay.Path)
	}
	return line
}

// Provenance is the ready-made one-line disclosure a reading agent surfaces:
// which rank answered, whether the overlay rides on it, and — when pop's own
// layer is the answer — what pop derived it from. Pop emits it rather than
// leaving each skill to phrase it, so the "which source am I using" line cannot
// drift between skills the way the recipe itself did (ADR-0211).
func (s Stack) Provenance() string {
	answer, hasAnswer := s.Answer()
	overlay, hasOverlay := s.Overlay()
	switch {
	case !hasAnswer && !hasOverlay:
		return fmt.Sprintf("Provenance: no %s convention in any of the %d layers pop consulted.", s.Kind, len(s.Layers))
	case !hasAnswer:
		return fmt.Sprintf("Provenance: %s is your overlay alone (%s); no layer holds an answer for it to ride on.",
			s.Kind, overlay.Path)
	}
	line := fmt.Sprintf("Provenance: %s resolved to %s (%s).", s.Kind, answer.Origin, answer.Path)
	if hasOverlay {
		line += fmt.Sprintf(" Your overlay is appended (%s).", overlay.Path)
	}
	// Only an answering memory is disclosed: a memory the documents stood down
	// is not in force, and quoting its derivation would describe prose nobody is
	// being handed.
	if answer.Origin == OriginMemory {
		line += " " + memoryDerivation(answer)
	}
	return line
}

// memoryDerivation phrases Convention memory's frontmatter as the clause the
// provenance line carries. Memory written without frontmatter still gets a
// clause: a pop-written layer whose origin is unrecorded is itself worth
// disclosing.
func memoryDerivation(mem Layer) string {
	switch {
	case mem.DerivedFrom != "" && mem.DerivedAt != "":
		return fmt.Sprintf("Pop memory was derived from %s on %s.", mem.DerivedFrom, mem.DerivedAt)
	case mem.DerivedFrom != "":
		return fmt.Sprintf("Pop memory was derived from %s.", mem.DerivedFrom)
	}
	return "Pop memory records no derivation."
}
