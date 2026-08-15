package conventions

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// overrideRule is the one statement of precedence the whole output hangs under.
// It is printed once per stack rather than per layer because rank decides only
// direct contradictions — repeating it beside each layer would suggest the
// layers are alternatives, which is exactly the reading composition rejects.
const overrideRule = `These layers compose: read every one of them. They are printed lowest rank
first, and where two of them directly contradict, the later one wins.`

// Get resolves and prints the Convention stack for each kind in turn. It
// returns ErrNoConvention when every kind asked about was empty — the miss the
// CLI turns into exit 1 — after the output is already written, so the caller
// has been told where pop looked either way.
func Get(d *Deps, w io.Writer, cwd string, kinds ...Kind) error {
	stacks := make([]Stack, 0, len(kinds))
	for _, kind := range kinds {
		stack, err := Resolve(d, kind, cwd)
		if err != nil {
			return err
		}
		stacks = append(stacks, stack)
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

// RenderStacks prints several kinds' stacks in turn, separated so a reader can
// tell where one kind's answer ends.
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

// RenderStack prints one kind's Convention stack as plain text: the override
// rule once, then every layer that holds something labelled with its origin and
// path, then the provenance line. An empty stack prints the paths pop consulted
// instead, because "nothing here" is only actionable with the four places named.
func RenderStack(w io.Writer, s Stack) error {
	fmt.Fprintf(w, "CONVENTION %s\n\n", s.Kind)

	present := s.Present()
	if len(present) == 0 {
		fmt.Fprintf(w, "EMPTY — no layer holds the %s convention. Pop consulted, lowest rank first:\n\n", s.Kind)
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

	fmt.Fprintf(w, "%s\n", overrideRule)
	for i, l := range present {
		fmt.Fprintf(w, "\n----- LAYER %d OF %d: %s (%s) -----\n",
			i+1, len(present), strings.ToUpper(string(l.Origin)), l.Origin.Scope())
		fmt.Fprintf(w, "%s\n\n%s\n", l.Path, l.Body)
	}
	fmt.Fprintf(w, "\n%s\n", s.Provenance())
	return nil
}

// Provenance is the ready-made one-line disclosure a reading agent surfaces:
// which layer is on top, and — whenever pop's own layer is in play — what pop
// derived it from. Pop emits it rather than leaving each skill to phrase it, so
// the "which source am I using" line cannot drift between skills the way the
// recipe itself did (ADR-0211).
func (s Stack) Provenance() string {
	top, ok := s.Top()
	if !ok {
		return fmt.Sprintf("Provenance: no %s convention in any of the %d layers pop consulted.", s.Kind, len(s.Layers))
	}
	n := len(s.Present())
	plural := "layers"
	if n == 1 {
		plural = "layer"
	}
	line := fmt.Sprintf("Provenance: %s resolved from %d %s; on top is %s (%s).",
		s.Kind, n, plural, top.Origin, top.Path)
	if mem, ok := s.memory(); ok {
		line += " " + memoryDerivation(mem)
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
