package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/conventions"
	"github.com/spf13/cobra"
)

// repoCmd is the noun for what pop knows about the repository you are standing
// in, as against pop config repo, which holds that repository's scalar
// settings. The split is prose document versus TOML scalar (ADR-0211).
var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with what pop knows about this repository",
	Long: `Work with what pop knows about the repository of the current directory.

Everything here is keyed by the repository, not by the checkout: an answer is
the same in the trunk and in every worktree of it.`,
}

var repoConventionsCmd = &cobra.Command{
	Use:   "conventions",
	Short: "Read the conventions this repository is written under",
	Long: `Read the conventions this repository is written under.

A convention is prose — "how does this repository write its commits" — held for
one convention kind. It is never a single document: a kind resolves through a
stack of four layers, and what you get back is every layer that has something
to say.`,
}

var repoConventionsGetCmd = &cobra.Command{
	Use:   "get [kind]",
	Short: "Print every layer of a convention's stack, lowest rank first",
	Long: `Print the convention stack for this repository, for one kind or for all of them.

The consumer is an agent that has to follow the convention. It gets every layer
that exists — labelled with its origin and path, lowest rank first, under one
statement of the override rule — and reconciles them itself. Pop orders and
labels; it never merges prose.

The four layers, lowest rank first:

  user defaults  ~/.agents/docs/<kind>.md          yours, every repository
  pop memory     <task storage>/conventions/<kind>.md   pop-written, this repo
  repository     docs/agents/<kind>.md             the team's, in version control
  user overlay   ~/.agents/docs/<kind>.overlay.md  yours, over every repository

The memory layer is filed under the repository, not the directory, so every
worktree of a repository reads one file.

Output ends with a one-line provenance summary naming the layer on top, ready
to be surfaced verbatim as the "which source am I using" disclosure.

With no kind, every known kind's stack prints in turn. Exit is 1 when the kinds
asked about are all empty — a miss, not a failure — and the paths pop consulted
are printed so you know where an answer would go. An unknown kind is refused
with the list of the ones that exist, and no stack is printed.`,
	Args:              cobra.MaximumNArgs(1),
	RunE:              runRepoConventionsGet,
	ValidArgsFunction: completeConventionKind,
}

var repoConventionsRecipeCmd = &cobra.Command{
	Use:   "recipe <kind>",
	Short: "Print how to work out a convention pop does not hold",
	Long: `Print the built-in recipe for one convention kind.

A recipe is a method, not an answer: it is the steps that derive the convention,
and where to write the result once you have it. The output says so in its first
lines, so a recipe cannot be mistaken for a convention.

This is what a missed ` + "`get`" + ` prints, and it is reachable on its own because an
agent improving a convention that already exists needs the method too.

Nothing here is repository-specific and nothing is read from disk, so it answers
outside a repository as well as inside one.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runRepoConventionsRecipe,
	ValidArgsFunction: completeConventionKind,
}

var repoConventionsSetCmd = &cobra.Command{
	Use:   "set <kind>",
	Short: "Remember a convention for this repository",
	Long: `Write the pop memory layer of a convention stack for this repository.

The body is read from stdin, because the writer is an agent that has just worked
out the convention, not a human at a terminal. ` + "`--file`" + ` reads the same body from
a path instead. There is no editor mode.

  pop repo conventions recipe commits            # the method
  ... | pop repo conventions set commits --derived-from "the last 20 commits"

` + "`--derived-from`" + ` is required: it names the evidence the convention was derived
from, is stored in the file's frontmatter with the time of the write, and is
what the provenance line of a later ` + "`get`" + ` quotes. A remembered convention whose
origin nobody can state is worse than no memory at all.

The file is filed under the repository, not the checkout, so a convention
written in the trunk is the same convention in every worktree of it. Writing
again replaces what is there.

This is one rank of four. It is where a convention pop derived from evidence
belongs; a convention a human states in session belongs in the repository's
` + "`docs/agents/<kind>.md`" + `, which outranks this layer and which the team owns.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runRepoConventionsSet,
	ValidArgsFunction: completeConventionKind,
}

var repoConventionsUnsetCmd = &cobra.Command{
	Use:   "unset <kind>",
	Short: "Forget the convention pop remembered for this repository",
	Long: `Remove the pop memory layer of a convention stack for this repository.

Only pop's own layer goes: the user's documents and the repository's committed
document are untouched, so the kind usually keeps answering. The output says
what still answers it, printed exactly as ` + "`get`" + ` would print it, so the verb
cannot be read as silencing the kind.

A kind pop holds no memory for is reported as such and is not a failure.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runRepoConventionsUnset,
	ValidArgsFunction: completeConventionKind,
}

var (
	repoConventionsSetFile        string
	repoConventionsSetDerivedFrom string
)

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoConventionsCmd)
	repoConventionsCmd.AddCommand(repoConventionsGetCmd)
	repoConventionsCmd.AddCommand(repoConventionsRecipeCmd)
	repoConventionsCmd.AddCommand(repoConventionsSetCmd)
	repoConventionsCmd.AddCommand(repoConventionsUnsetCmd)

	repoConventionsSetCmd.Flags().StringVar(&repoConventionsSetFile, "file", "",
		"read the convention body from this file instead of stdin")
	repoConventionsSetCmd.Flags().StringVar(&repoConventionsSetDerivedFrom, "derived-from", "",
		"what the convention was derived from, recorded as its provenance")
	if err := repoConventionsSetCmd.MarkFlagRequired("derived-from"); err != nil {
		panic(err)
	}
}

func completeConventionKind(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return conventions.KindNames(), cobra.ShellCompDirectiveNoFileComp
}

func runRepoConventionsGet(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("repo conventions get: %w", err)
	}
	err = runRepoConventionsGetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir, args)
	// An empty stack has already printed everything it has to say. Exiting here
	// rather than returning the error keeps it out of the error reporter: the
	// caller wanted a status, not a failure report.
	if errors.Is(err, conventions.ErrNoConvention) {
		os.Exit(1)
	}
	return err
}

// runRepoConventionsGetWith is the seam tests drive: it takes the writer and
// the raw kind arguments, so both the refusal and the rendering are exercised
// without a process exit.
func runRepoConventionsGetWith(cd *conventions.Deps, w io.Writer, dir string, args []string) error {
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

func runRepoConventionsSet(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("repo conventions set: %w", err)
	}
	body, err := readConventionBody(cmd.InOrStdin(), repoConventionsSetFile)
	if err != nil {
		return fmt.Errorf("repo conventions set: %w", err)
	}
	return runRepoConventionsSetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir,
		args[0], body, repoConventionsSetDerivedFrom)
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

// runRepoConventionsSetWith is the seam tests drive: the refusal, the write and
// its report without a process or a terminal.
func runRepoConventionsSetWith(cd *conventions.Deps, w io.Writer, dir, name, body, derivedFrom string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.Set(cd, w, kind, dir, body, derivedFrom)
}

func runRepoConventionsUnset(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("repo conventions unset: %w", err)
	}
	return runRepoConventionsUnsetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir, args[0])
}

// runRepoConventionsUnsetWith is the seam tests drive, so the removal and the
// stack that survives it are exercised together.
func runRepoConventionsUnsetWith(cd *conventions.Deps, w io.Writer, dir, name string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.Unset(cd, w, kind, dir)
}

func runRepoConventionsRecipe(cmd *cobra.Command, args []string) error {
	return runRepoConventionsRecipeWith(cmd.OutOrStdout(), args[0])
}

// runRepoConventionsRecipeWith is the seam tests drive, so the refusal and the
// printed recipe are exercised without a process. It takes no Deps: a recipe is
// built in, and reads nothing.
func runRepoConventionsRecipeWith(w io.Writer, name string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.RenderRecipe(w, kind)
}
