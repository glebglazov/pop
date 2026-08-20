package conventions

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Get resolves and prints the convention in force for each kind in turn. Every
// kind resolves to rules to follow — pop's own where nobody has written an
// answer — so there is no miss left to report and the only error it can return
// is a failure to resolve the repository at all (ADR-0226 decision 1).
func Get(d *Deps, w io.Writer, cwd string, kinds ...Kind) error {
	stacks, err := ResolveAll(d, cwd, kinds...)
	if err != nil {
		return err
	}
	return RenderStacks(w, stacks)
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
	answer := s.Answer()
	fmt.Fprintf(&b, "----- ANSWER: %s (%s) -----\n%s\n\n%s\n",
		strings.ToUpper(string(answer.Origin)), answer.Origin.Scope(),
		answer.Path, answer.Body)
	if overlay, ok := s.Overlay(); ok {
		fmt.Fprintln(&b)
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
// the overlay where there is one, and the provenance line. A kind nobody has
// written an answer to resolves to pop's own and prints that instead, so this
// never renders a miss.
func RenderStack(w io.Writer, s Stack) error {
	fmt.Fprintf(w, "CONVENTION %s\n\n", s.Kind)
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
	fmt.Fprintf(&b, "%s — what is in force here.\n\n%s", s.Kind, s.inForceProse())
	fmt.Fprintf(&b, "\n%s\n", s.overlayNote())
	return b.String()
}

// StackProse renders one kind as the prose an agent is handed inside a larger
// prompt: the answer, the overlay and the provenance line. It carries no
// heading of its own and names no editing surface — the prompt that embeds it
// owns both.
//
// It always speaks, and what it speaks is always rules to follow: a kind nobody
// has written an answer to resolves to pop's own.
func StackProse(s Stack) string { return s.inForceProse() }

// overlayNote says where the human's overlay is and whether it holds anything.
// An editing surface writes that one layer, so a reader deciding whether to
// edit needs to know which rank they would be changing — and the overlay is the
// one that never displaces an answer.
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

// RenderSet confirms a write by naming the rank it landed at. The rank is the
// fact worth reporting: a writer who took their document for a note to self
// would not expect it to stand down the team's committed one, and a writer who
// meant to state something everywhere would want to know they stated it here.
func RenderSet(w io.Writer, origin Origin, kind Kind, path string, replaced bool) error {
	verb := "WROTE"
	if replaced {
		verb = "REPLACED"
	}
	fmt.Fprintf(w, "%s your %s convention at the %s rank (%s)\n\n",
		verb, kind, origin, origin.Scope())
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "path\t%s\n", path)
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%s\nRun `pop conventions get %s` for what is in force.\n",
		rankReach(origin, kind), kind)
	return nil
}

// rankReach says what writing one rank did to the others, in the reader's own
// terms. Which ranks a write outranks is derivable from the stack, but the
// sentence a human needs is about the documents they know they have, so each
// rank names them.
func rankReach(origin Origin, kind Kind) string {
	switch origin {
	case OriginProject:
		return fmt.Sprintf("This is the first rank consulted, so it answers %[1]s here whatever\n"+
			"~/.agents/docs/%[1]s.md or a committed docs/agents/%[1]s.md says. Your overlay\n"+
			"is still appended to it.", kind)
	case OriginGlobal:
		return fmt.Sprintf("This answers %[1]s in every repository where you have written no\n"+
			"project document, and it stands down a committed docs/agents/%[1]s.md when it\n"+
			"does. Your overlay is still appended to it.", kind)
	case OriginOverlay:
		return fmt.Sprintf("This is appended to whichever rank answers %s, in every repository,\n"+
			"rather than displacing one — so it rides along with the team's document\n"+
			"instead of hiding it.", kind)
	}
	return ""
}

// RenderUnset reports a removal together with what answers the kind now.
// Removing the winning rank promotes the next one, which is the fact the verb
// exists to report; the stack beneath is printed through the same renderer
// `get` uses, so the two surfaces cannot disagree about what is in effect.
func RenderUnset(w io.Writer, origin Origin, kind Kind, path string, removed bool, remaining Stack) error {
	if removed {
		fmt.Fprintf(w, "REMOVED your %s convention at the %s rank (%s)\n%s\n\n",
			kind, origin, origin.Scope(), path)
	} else {
		fmt.Fprintf(w, "NO %s convention of yours at the %s rank (%s) — nothing to remove.\n%s\n\n",
			kind, origin, origin.Scope(), path)
	}
	fmt.Fprintf(w, "%s\n\n", nowInForce(remaining))
	return RenderStack(w, remaining)
}

// nowInForce names the rank that answers a kind after a removal, in one line,
// before the whole rendering repeats it at length. A reader told only "removed"
// would have to run `get` to learn which rank was promoted.
func nowInForce(s Stack) string {
	answer := s.Answer()
	line := fmt.Sprintf("Now in force: %s (%s).", answer.Origin, answer.Path)
	if answer.Origin == OriginShipped {
		line = fmt.Sprintf("Nobody answers %s now, so it falls through to pop's own answer for the kind.", s.Kind)
	}
	if overlay, ok := s.Overlay(); ok {
		line += fmt.Sprintf(" Your overlay is appended (%s).", overlay.Path)
	}
	return line
}

// Provenance is the ready-made one-line disclosure a reading agent surfaces:
// which rank answered, and whether the overlay rides on it. Pop emits it rather
// than leaving each skill to phrase it, so the "which source am I using" line
// cannot drift between the skills that ask (ADR-0211).
func (s Stack) Provenance() string {
	answer := s.Answer()
	line := fmt.Sprintf("Provenance: %s resolved to %s (%s).", s.Kind, answer.Origin, answer.Path)
	if overlay, ok := s.Overlay(); ok {
		line += fmt.Sprintf(" Your overlay is appended (%s).", overlay.Path)
	}
	return line
}
