package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/spf13/cobra"
)

// Checkout locality words. They are the verb's whole scalar vocabulary: a skill
// branching on registration routing compares against exactly these two strings.
const (
	localityTrunk    = "trunk"
	localityWorktree = "worktree"
)

// checkoutReport is what `pop tasks checkout --json` emits. Locality is derived
// from git alone (see resolveCheckoutReport); TrunkPath and Bare ride along as
// information for a caller that wants the config-aware answer too, never as
// inputs to Locality. TrunkPath is omitted rather than empty when no trunk
// resolves, so "unresolvable" and "resolved to nothing" cannot be confused.
type checkoutReport struct {
	Path      string `json:"path"`
	Locality  string `json:"locality"`
	Branch    string `json:"branch"`
	TrunkPath string `json:"trunk_path,omitempty"`
	Bare      bool   `json:"bare"`
	Managed   bool   `json:"managed"`
}

var (
	taskCheckoutLocality bool
	taskCheckoutJSON     bool
)

var taskCheckoutCmd = &cobra.Command{
	Use:   "checkout",
	Short: "Report this checkout's locality — trunk or worktree — and the rest of its identity",
	Long: `Report the current checkout: whether you are standing in the Trunk worktree or
in a linked worktree, and the facts that go with it.

Locality is pure git, derived from the same linked-worktree predicate a drain
routes on, so this verb can never contradict where "pop tasks implement" lands.
It consults no config: a checkout declared trunk in config that is nonetheless a
linked worktree reads "worktree", and a bare repository reads "worktree" in
every checkout, including the bare directory itself.

--locality prints exactly one word, "trunk" or "worktree", and nothing else, so
a skill body needs no JSON processor. It is also the default with no flags.

--json prints the whole checkout instead: path, locality, branch, trunk_path
(omitted when no trunk resolves), bare and managed. trunk_path is the
config-aware answer to a different question — where a managed worktree would
fork from — and never feeds locality.

Read-only, and unlike the "Checkout:" line in "pop tasks status" it needs no
registered task set, so it works in a virgin repository.`,
	Args: cobra.NoArgs,
	RunE: runTaskCheckout,
}

func runTaskCheckout(cmd *cobra.Command, _ []string) error {
	if taskCheckoutLocality && taskCheckoutJSON {
		return fmt.Errorf("tasks checkout: --locality and --json are alternative output modes; pass one")
	}
	dir, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("tasks checkout: %w", err)
	}
	cfg, _ := taskConfigLoad(taskConfigPath())
	return runTaskCheckoutWith(cmdLayerDeps().configDeps(), cmdLayerDeps().tasksDeps(), cfg, cmd.OutOrStdout(), dir, taskCheckoutJSON)
}

// runTaskCheckoutWith renders the checkout report for dir. Scalar mode is the
// default because the caller this verb exists for is a skill body picking a
// registration flag, and that caller cannot parse JSON.
func runTaskCheckoutWith(cd *config.Deps, td *tasks.Deps, cfg *config.Config, w io.Writer, dir string, asJSON bool) error {
	report, err := resolveCheckoutReport(cd, td, cfg, dir)
	if err != nil {
		return err
	}
	if !asJSON {
		fmt.Fprintln(w, report.Locality)
		return nil
	}
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("tasks checkout: %w", err)
	}
	fmt.Fprintln(w, string(out))
	return nil
}

// resolveCheckoutReport derives the whole report from dir.
//
// Locality reuses binding.IsLinkedWorktree — the single predicate deciding where
// a `pop tasks implement` run lands — so the verb and the drain agree by
// construction. Bareness is checked first because that predicate has a known
// false negative on a bare repository's own directory, where --git-dir and
// --git-common-dir are both "."; a bare repo has no trunk to stand in, so every
// checkout of one reads "worktree". Closing it here rather than in the skill
// body leaves the caller no ambiguous branch to handle.
func resolveCheckoutReport(cd *config.Deps, td *tasks.Deps, cfg *config.Config, dir string) (checkoutReport, error) {
	// Bare repositories have no top level, so this falls back to the canonical
	// directory rather than refusing; the git probe below is what rejects a
	// non-repository.
	path, err := tasks.NormalizeProjectPathWith(td, dir)
	if err != nil {
		return checkoutReport{}, fmt.Errorf("tasks checkout: %w", err)
	}

	// GitMainWorktree doubles as the in-repo probe (it errors outside a repo)
	// and as the bareness source, exactly as pop config show uses it.
	_, bare, err := binding.GitMainWorktree(td, path)
	if err != nil {
		return checkoutReport{}, fmt.Errorf("tasks checkout: not inside a git repository (run from a checkout of the target repo)")
	}

	locality := localityWorktree
	if !bare {
		linked, err := binding.IsLinkedWorktree(td, path)
		if err != nil {
			return checkoutReport{}, fmt.Errorf("tasks checkout: %w", err)
		}
		if !linked {
			locality = localityTrunk
		}
	}

	report := checkoutReport{
		Path:     path,
		Locality: locality,
		Branch:   binding.CurrentBranch(td, path),
		Bare:     bare,
	}
	if trunkPath, _, err := binding.ResolveTrunkPathWith(cd, td, cfg, path); err == nil {
		report.TrunkPath = trunkPath
	}
	if managed, err := binding.IsManagedCheckout(td, path); err == nil {
		report.Managed = managed
	}
	return report, nil
}
