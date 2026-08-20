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
	Use:   "set <kind> (--project|--global|--overlay)",
	Short: "State your own convention at one rank",
	Long: `Write your own document for one convention kind, at the rank you name.

The body is read from stdin. ` + "`--file`" + ` reads the same body from a path instead.
There is no editor mode.

  pop conventions default commits      # what pop answers with today
  ... | pop conventions set commits --project

Exactly one rank must be named. Pop never guesses: a convention is authoritative
prose, and writing it a rank away from where you meant is the mistake worth a
refusal.

  --project    ~/.agents/docs/projects/<slug>/<kind>.md   yours, this project
  --global     ~/.agents/docs/<kind>.md                   yours, every repository
  --overlay    ~/.agents/docs/<kind>.overlay.md           appended to whichever answered

` + "`--project`" + ` is the first rank consulted, so it answers the kind here whatever your
global document or the team's committed one says: reach for it to override
everything for one project — trying a convention out, or contradicting a team's
grammar locally. It lives outside the repository, keyed by the git remote, so it
needs no commit and every clone of the project reads it.

` + "`--global`" + ` is the same statement made everywhere you work. ` + "`--overlay`" + ` does not
displace anything: it rides along with whichever rank answered.

The team's committed ` + "`docs/agents/<kind>.md`" + ` is not writable here. Naming
` + "`--repository`" + ` is refused with its path and the reason: a document a team follows
should land through a diff somebody reviews. Writing again at the same rank
replaces what is there.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsSet,
	ValidArgsFunction: completeConventionKind,
}

var conventionsUnsetCmd = &cobra.Command{
	Use:   "unset <kind> (--project|--global|--overlay)",
	Short: "Remove your own convention at one rank",
	Long: `Remove your own document for one convention kind, at the rank you name.

It mirrors ` + "`set`" + ` exactly: the same three ranks, named the same way, and the same
refusal for naming none or for naming the team's committed document.

Only that one file goes: the other ranks are untouched, so the kind usually keeps
answering — and where the removed rank was the one in force, whichever rank sat
under it is promoted. The output names what is in force afterwards and prints it
exactly as ` + "`get`" + ` would, so the verb cannot be read as silencing the kind.

A rank you have written nothing at is reported as such and is not a failure.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runConventionsUnset,
	ValidArgsFunction: completeConventionKind,
}

var conventionsSetFile string

// rankFlags is one command's rank switches: the flag a human sets to say which
// layer a write lands in. They are held as a set rather than one bool each so
// the "exactly one" rule is stated once, and so the flag names come from the
// ranks themselves rather than from a second list that could drift.
type rankFlags map[string]*bool

// named returns the rank the caller asked for, or the empty string when they
// asked for none. Cobra refuses two at once before this is reached, so the first
// set flag is the only one.
func (r rankFlags) named() string {
	for name, set := range r {
		if set != nil && *set {
			return name
		}
	}
	return ""
}

var (
	conventionsSetRanks   = rankFlags{}
	conventionsUnsetRanks = rankFlags{}
)

// addRankFlags gives a write verb its rank switches. The repository rank is
// registered alongside the writable three even though pop refuses it: a reader
// who reaches for the team's document asked a fair question, and the answer is
// the reason it is not written here rather than "unknown flag".
func addRankFlags(cmd *cobra.Command, flags rankFlags) {
	ranks := append([]conventions.Origin{}, conventions.WritableRanks...)
	ranks = append(ranks, conventions.OriginRepository)
	names := make([]string, 0, len(ranks))
	for _, origin := range ranks {
		name := origin.RankName()
		usage := fmt.Sprintf("the %s rank — %s", origin, origin.Scope())
		if origin == conventions.OriginRepository {
			usage = "the team's committed document — refused, with the reason"
		}
		flags[name] = cmd.Flags().Bool(name, false, usage)
		names = append(names, name)
	}
	cmd.MarkFlagsMutuallyExclusive(names...)
}

func init() {
	rootCmd.AddCommand(conventionsCmd)
	conventionsCmd.AddCommand(conventionsGetCmd)
	conventionsCmd.AddCommand(conventionsDefaultCmd)
	conventionsCmd.AddCommand(conventionsSetCmd)
	conventionsCmd.AddCommand(conventionsUnsetCmd)

	conventionsSetCmd.Flags().StringVar(&conventionsSetFile, "file", "",
		"read the convention body from this file instead of stdin")
	addRankFlags(conventionsSetCmd, conventionsSetRanks)
	addRankFlags(conventionsUnsetCmd, conventionsUnsetRanks)
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
	return runConventionsSetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir,
		args[0], conventionsSetRanks.named(), body)
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

// runConventionsSetWith is the seam tests drive: the refusals, the write and
// its report without a process or a terminal. The rank arrives as the word the
// caller named, so the seam exercises the same refusal a bare `set` gets.
func runConventionsSetWith(cd *conventions.Deps, w io.Writer, dir, name, rank, body string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	origin, err := conventions.ParseRank(rank, kind)
	if err != nil {
		return err
	}
	return conventions.Set(cd, w, origin, kind, dir, body)
}

func runConventionsUnset(cmd *cobra.Command, args []string) error {
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("conventions unset: %w", err)
	}
	return runConventionsUnsetWith(cmdLayerDeps().conventionsDeps(), cmd.OutOrStdout(), dir,
		args[0], conventionsUnsetRanks.named())
}

// runConventionsUnsetWith is the seam tests drive, so the removal and the
// stack that survives it are exercised together.
func runConventionsUnsetWith(cd *conventions.Deps, w io.Writer, dir, name, rank string) error {
	kind, err := conventions.ParseKind(name)
	if err != nil {
		return err
	}
	origin, err := conventions.ParseRank(rank, kind)
	if err != nil {
		return err
	}
	return conventions.Unset(cd, w, origin, kind, dir)
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
