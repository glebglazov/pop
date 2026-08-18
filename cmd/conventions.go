package cmd

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/conventions"
	"github.com/spf13/cobra"
)

// conventionsCmd is the noun for what pop knows about the repository you are
// standing in, as against pop config repo, which holds that repository's
// scalar settings. The split is prose document versus TOML scalar (ADR-0211).
// It sits at the top level, a sibling of pop config: every convention is a
// repository convention, so a repo parent would have selected nothing
// (ADR-0212).
var conventionsCmd = &cobra.Command{
	Use:   "conventions",
	Short: "Read the conventions this repository is written under",
	Long: `Read the conventions this repository is written under.

A convention is prose — "how does this repository write its commits" — held for
one convention kind. A kind resolves to exactly one answer, plus your own
overlay where you have written one.`,
}

var conventionsGetCmd = &cobra.Command{
	Use:   "get [kind]",
	Short: "Print the convention in force here, and your overlay",
	Long: `Print the convention in force for this repository, for one kind or for all of them.

The consumer is an agent that has to follow the convention. It gets one answer,
labelled with its origin and path, and your overlay appended where you have
written one. Nothing else composes, and there is no contradiction left for the
reader to reconcile.

The answer is the first of these that holds something:

  user defaults  ~/.agents/docs/<kind>.md               yours, every repository
  repository     docs/agents/<kind>.md                  the team's, in version control
  pop memory     <task storage>/conventions/<kind>.md   pop-written, this repo
  recipe         built into pop                         the method for deriving one

Your own document outranks the team's: pop resolves conventions on your machine,
on your behalf. Pop memory is pop's stand-in for a written answer, so it stands
down as soon as either document exists. The recipe is last and is always there,
so a kind nobody has answered hands you the method for working one out, under a
banner saying so — steps to carry out, not rules to follow.

  user overlay   ~/.agents/docs/<kind>.overlay.md  appended to whichever answered

The memory layer is filed under the repository, not the directory, so every
worktree of a repository reads one file.

Output ends with a one-line provenance summary naming what answered, ready to be
surfaced verbatim as the "which source am I using" disclosure.

With no kind, every known kind prints in turn. Exit is 0 in every case, because
every kind resolves to something. An unknown kind is refused with the list of
the ones that exist, and nothing is printed.`,
	Args:              cobra.MaximumNArgs(1),
	RunE:              runConventionsGet,
	ValidArgsFunction: completeConventionKind,
}

var conventionsRecipeCmd = &cobra.Command{
	Use:   "recipe <kind>",
	Short: "Print how to work out a convention pop does not hold",
	Long: `Print the built-in recipe for one convention kind.

A recipe is a method, not an answer: it is the steps that derive the convention,
and where to write the result once you have it. The output says so in its first
lines, so a recipe cannot be mistaken for a convention.

` + "`get`" + ` prints this same body when nothing else answers the kind, and this verb
is reachable on its own because an agent improving a convention that already
answers needs the method too.

Nothing here is repository-specific and nothing is read from disk, so it answers
outside a repository as well as inside one.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsRecipe,
	ValidArgsFunction: completeConventionKind,
}

var conventionsSetCmd = &cobra.Command{
	Use:   "set <kind>",
	Short: "Remember a convention for this repository",
	Long: `Write the pop memory layer of a convention stack for this repository.

The body is read from stdin, because the writer is an agent that has just worked
out the convention, not a human at a terminal. ` + "`--file`" + ` reads the same body from
a path instead. There is no editor mode.

  pop conventions recipe commits            # the method
  ... | pop conventions set commits --derived-from "the last 20 commits"

` + "`--derived-from`" + ` is required: it names the evidence the convention was derived
from, is stored in the file's frontmatter with the time of the write, and is
what the provenance line of a later ` + "`get`" + ` quotes. A remembered convention whose
origin nobody can state is worse than no memory at all.

The file is filed under the repository, not the checkout, so a convention
written in the trunk is the same convention in every worktree of it. Writing
again replaces what is there.

This is the last rank consulted: pop's stand-in for a written answer, which
stands down the moment either document exists. It is where a convention pop
derived from evidence belongs; a convention a human states in session belongs in
the repository's ` + "`docs/agents/<kind>.md`" + `, which the team owns, or in
` + "`~/.agents/docs/<kind>.md`" + `, which is yours and outranks both.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsSet,
	ValidArgsFunction: completeConventionKind,
}

var conventionsUnsetCmd = &cobra.Command{
	Use:   "unset <kind>",
	Short: "Forget the convention pop remembered for this repository",
	Long: `Remove the pop memory layer of a convention stack for this repository.

Only pop's own layer goes: the user's documents and the repository's committed
document are untouched, so the kind usually keeps answering — and where memory
was the answer, removing it promotes the next rank. The output names what is in
force afterwards and prints it exactly as ` + "`get`" + ` would, so the verb cannot be
read as silencing the kind.

A kind pop holds no memory for is reported as such and is not a failure.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsUnset,
	ValidArgsFunction: completeConventionKind,
}

var (
	conventionsSetFile        string
	conventionsSetDerivedFrom string
)

func init() {
	rootCmd.AddCommand(conventionsCmd)
	conventionsCmd.AddCommand(conventionsGetCmd)
	conventionsCmd.AddCommand(conventionsRecipeCmd)
	conventionsCmd.AddCommand(conventionsSetCmd)
	conventionsCmd.AddCommand(conventionsUnsetCmd)

	conventionsSetCmd.Flags().StringVar(&conventionsSetFile, "file", "",
		"read the convention body from this file instead of stdin")
	conventionsSetCmd.Flags().StringVar(&conventionsSetDerivedFrom, "derived-from", "",
		"what the convention was derived from, recorded as its provenance")
	if err := conventionsSetCmd.MarkFlagRequired("derived-from"); err != nil {
		panic(err)
	}
}

func completeConventionKind(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return conventions.KindNames(), cobra.ShellCompDirectiveNoFileComp
}

func runConventionsGet(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("conventions get: %w", err)
	}
	// Every kind resolves to something, so there is no miss status to translate:
	// the command succeeds whenever it printed, and the reader tells a method
	// from an answer by what it printed (ADR-0223 decision 5).
	return runConventionsGetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir, args)
}

// runConventionsGetWith is the seam tests drive: it takes the writer and the
// raw kind arguments, so both the refusal and the rendering are exercised
// without a process exit.
func runConventionsGetWith(cd *conventions.Deps, w io.Writer, dir string, args []string) error {
	kinds := conventions.Kinds()
	if len(args) == 1 {
		kind, err := conventions.ParseKind(args[0])
		if err != nil {
			return err
		}
		kinds = []conventions.Kind{kind}
	}
	return conventions.Get(cd, w, dir, kinds...)
}

func runConventionsSet(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("conventions set: %w", err)
	}
	body, err := readConventionBody(cmd.InOrStdin(), conventionsSetFile)
	if err != nil {
		return fmt.Errorf("conventions set: %w", err)
	}
	return runConventionsSetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir,
		args[0], body, conventionsSetDerivedFrom)
}

// readConventionBody resolves the two ways in, which are one path: --file is an
// alias for stdin, not a second mode, so nothing downstream branches on which
// one the caller used.
func readConventionBody(stdin io.Reader, file string) (string, error) {
	if file != "" {
		raw, err := cmdLayerDeps().FS.ReadFile(file)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// runConventionsSetWith is the seam tests drive: the refusal, the write and
// its report without a process or a terminal.
func runConventionsSetWith(cd *conventions.Deps, w io.Writer, dir, name, body, derivedFrom string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.Set(cd, w, kind, dir, body, derivedFrom)
}

func runConventionsUnset(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("conventions unset: %w", err)
	}
	return runConventionsUnsetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir, args[0])
}

// runConventionsUnsetWith is the seam tests drive, so the removal and the
// stack that survives it are exercised together.
func runConventionsUnsetWith(cd *conventions.Deps, w io.Writer, dir, name string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.Unset(cd, w, kind, dir)
}

func runConventionsRecipe(cmd *cobra.Command, args []string) error {
	return runConventionsRecipeWith(cmd.OutOrStdout(), args[0])
}

// runConventionsRecipeWith is the seam tests drive, so the refusal and the
// printed recipe are exercised without a process. It takes no Deps: a recipe is
// built in, and reads nothing.
func runConventionsRecipeWith(w io.Writer, name string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.RenderRecipe(w, kind)
}
