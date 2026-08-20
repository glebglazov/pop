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

  user project   ~/.agents/docs/projects/<slug>/<kind>.md   yours, this project
  user global    ~/.agents/docs/<kind>.md                   yours, every repository
  repository     docs/agents/<kind>.md                      the team's, in version control
  shipped        built into pop                             pop's own, displaced by any above

Your own documents outrank the team's — pop resolves conventions on your
machine, on your behalf — and the more specific of the two outranks the general.
The shipped rank is last and is always there, so a kind nobody has answered
still hands you rules to follow: generic ones, because pop cannot know your
project's taste, and displaced whole the moment anybody writes their own.

  user overlay   ~/.agents/docs/<kind>.overlay.md  appended to whichever answered

Your project document is keyed by the repository's git remote, so every clone of
one project reads one file; a repository with no remote is keyed by pop's own
identity for it instead.

Output ends with a one-line provenance summary naming what answered, ready to be
surfaced verbatim as the "which source am I using" disclosure.

With no kind, every known kind prints in turn. Exit is 0 in every case, because
every kind resolves to something followable. An unknown kind is refused with the list of
the ones that exist, and nothing is printed.`,
	Args:              cobra.MaximumNArgs(1),
	RunE:              runConventionsGet,
	ValidArgsFunction: completeConventionKind,
}

var conventionsDefaultCmd = &cobra.Command{
	Use:   "default <kind>",
	Short: "Print pop's own answer for one convention kind",
	Long: `Print the shipped convention for one kind: pop's own answer, built into the
binary and the last rank of the stack.

It is rules to follow, not a method for deriving them — generic by construction,
because pop cannot know your project's taste. ` + "`get`" + ` prints this same body when
nothing else answers the kind.

This verb is reachable on its own so a human writing their own document can
start from pop's: read it, keep what fits, and write the result at whichever
rank you mean it to apply to. Customising is a human asking, never a machine
telling a machine to derive.

Nothing here is repository-specific and nothing is read from disk, so it answers
outside a repository as well as inside one.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsDefault,
	ValidArgsFunction: completeConventionKind,
}

var conventionsSetCmd = &cobra.Command{
	Use:   "set <kind>",
	Short: "State your own convention for this project",
	Long: `Write your own document for one convention kind in this project.

The body is read from stdin. ` + "`--file`" + ` reads the same body from a path instead.
There is no editor mode.

  pop conventions default commits      # what pop answers with today
  ... | pop conventions set commits

This is the first rank consulted, so it answers the kind here whatever your
global ` + "`~/.agents/docs/<kind>.md`" + ` or the team's committed
` + "`docs/agents/<kind>.md`" + ` says. It is what to reach for when you mean to override
everything for one project — trying a convention out, or contradicting a team's
grammar locally.

It lives outside the repository, keyed by the git remote, so it needs no commit
and every clone of the project reads it. Writing again replaces what is there.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsSet,
	ValidArgsFunction: completeConventionKind,
}

var conventionsUnsetCmd = &cobra.Command{
	Use:   "unset <kind>",
	Short: "Remove your own convention for this project",
	Long: `Remove your own document for one convention kind in this project.

Only that one file goes: your global document and the repository's committed one
are untouched, so the kind usually keeps answering — and since this was the top
rank, removing it promotes whichever rank sat under it. The output names what is
in force afterwards and prints it exactly as ` + "`get`" + ` would, so the verb cannot be
read as silencing the kind.

A kind you have written nothing for in this project is reported as such and is
not a failure.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsUnset,
	ValidArgsFunction: completeConventionKind,
}

var conventionsSetFile string

func init() {
	rootCmd.AddCommand(conventionsCmd)
	conventionsCmd.AddCommand(conventionsGetCmd)
	conventionsCmd.AddCommand(conventionsDefaultCmd)
	conventionsCmd.AddCommand(conventionsSetCmd)
	conventionsCmd.AddCommand(conventionsUnsetCmd)

	conventionsSetCmd.Flags().StringVar(&conventionsSetFile, "file", "",
		"read the convention body from this file instead of stdin")
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
	// Every kind resolves to rules to follow, so there is no miss status to
	// translate: the command succeeds whenever it printed (ADR-0226 decision 1).
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
	return runConventionsSetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir, args[0], body)
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
func runConventionsSetWith(cd *conventions.Deps, w io.Writer, dir, name, body string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.Set(cd, w, kind, dir, body)
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

func runConventionsDefault(cmd *cobra.Command, args []string) error {
	return runConventionsDefaultWith(cmd.OutOrStdout(), args[0])
}

// runConventionsDefaultWith is the seam tests drive, so the refusal and the
// printed answer are exercised without a process. It takes no Deps: the shipped
// rank is built in, and reads nothing.
func runConventionsDefaultWith(w io.Writer, name string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	return conventions.RenderShipped(w, kind)
}
