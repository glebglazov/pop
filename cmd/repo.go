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

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoConventionsCmd)
	repoConventionsCmd.AddCommand(repoConventionsGetCmd)
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
