package conventions

import (
	"embed"
	"fmt"
	"io"
	"strings"
)

// The recipes are authored as markdown a human can read and edit without
// touching Go (ADR-0208), owned by this package rather than by integrate/: they
// are printed on demand, never installed, so they are not a shipped asset.
//
//go:embed recipes/*.md
var recipeFS embed.FS

// Loading at init means a kind added to the enum without its recipe fails the
// first build's test run, rather than at the moment an agent misses the stack.
var recipes = mustLoadRecipes()

func mustLoadRecipes() map[Kind]string {
	loaded := make(map[Kind]string, len(Kinds()))
	for _, kind := range Kinds() {
		raw, err := recipeFS.ReadFile("recipes/" + string(kind) + ".md")
		if err != nil {
			panic(fmt.Sprintf("conventions: no embedded recipe for kind %q: %v", kind, err))
		}
		body := strings.TrimSpace(string(raw))
		if body == "" {
			panic(fmt.Sprintf("conventions: embedded recipe for kind %q is empty", kind))
		}
		loaded[kind] = body
	}
	return loaded
}

// recipeBanner is what keeps a recipe from being read as an answer. A caller
// that asked for the convention and got the method has to be told so in the
// first line it reads, because the two are printed on the same stream and one
// of them is a set of instructions to carry out.
const recipeBanner = `This is a METHOD, not a convention. What follows is how to work out a %[1]s
convention — steps to carry out, not rules to follow. Nothing here is this
repository's answer; the answer is what you produce by working the steps. Write
it down where the last step says, so the next agent reads an answer instead of
this.`

// Recipe returns the built-in derivation for kind: the method an agent works
// when the Convention stack has nothing to say. Every kind in the enum has one,
// which is the reason the enum is closed (ADR-0211).
func Recipe(kind Kind) string { return recipes[kind] }

// RenderRecipe prints one kind's recipe under the banner that marks it as a
// method. It is what `recipe <kind>` prints and what a missed `get` prints
// beneath the paths it consulted — one rendering, so the two surfaces cannot
// describe the same recipe differently.
func RenderRecipe(w io.Writer, kind Kind) error {
	fmt.Fprintf(w, "RECIPE %s\n\n", kind)
	fmt.Fprintf(w, recipeBanner+"\n\n", kind)
	_, err := fmt.Fprintf(w, "%s\n", Recipe(kind))
	return err
}
