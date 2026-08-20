package conventions

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/conventions/shipped"
)

// Loading at init means a kind added to the enum without pop's own answer for
// it fails the first build's test run, rather than at the moment an agent
// reaches the bottom of the stack.
var shippedAnswers = mustLoadShipped()

func mustLoadShipped() map[Kind]string {
	loaded := make(map[Kind]string, len(Kinds()))
	for _, kind := range Kinds() {
		body, ok := shipped.Body(string(kind))
		if !ok {
			panic(fmt.Sprintf("conventions: no shipped answer for kind %q", kind))
		}
		if body == "" {
			panic(fmt.Sprintf("conventions: shipped answer for kind %q is empty", kind))
		}
		loaded[kind] = body
	}
	return loaded
}

// Shipped returns pop's own answer for kind: rules to follow, generic by
// construction, because pop is a work orchestrator and cannot know a project's
// taste. Every kind in the enum has one, which is the reason the enum is closed
// (ADR-0211, ADR-0226 decision 2).
func Shipped(kind Kind) string { return shippedAnswers[kind] }

// shippedLayer is pop's answer as the last rank of the stack: always present,
// with no path on disk. It carries no banner and needs none — a consumer handed
// this rank reads rules in the same place another rank puts its rules, which is
// what makes "always resolves" mean "always resolves to something followable"
// (ADR-0226 decision 1).
func shippedLayer(kind Kind) Layer {
	return Layer{
		Origin:  OriginShipped,
		Path:    fmt.Sprintf("embedded: shipped/%s.md", kind),
		Present: true,
		Body:    Shipped(kind),
	}
}

// RenderShipped prints one kind's shipped answer on its own, under the same
// read-whole notice `get` prints — both are command paths, and an agent reaching
// for pop's own answer truncates output the same way. It is what
// `default <kind>` prints, and it prints the same body the last rank of a
// resolved stack carries, so a human basing their own document on pop's cannot
// be shown a different one from the agent that fell through to it.
func RenderShipped(w io.Writer, kind Kind) error {
	_, err := io.WriteString(w, WithReadWholeNotice(
		fmt.Sprintf("SHIPPED CONVENTION %s\n\n%s\n", kind, Shipped(kind))))
	return err
}
