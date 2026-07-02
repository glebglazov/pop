package cmd

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/spf13/cobra"
)

// workbenchPreferDeps carries the seams for the `pop workbench prefer` command
// so the set/clear/none/bad-name branches and the completion source are
// unit-testable without a real terminal, git repo, or runtime file. Picker holds
// the slice-02 write path (SetPreferred/ClearPreferred) and picker, which this
// command reuses verbatim — no duplicated write logic (ADR-0078).
type workbenchPreferDeps struct {
	Picker          *preferredPickerDeps
	CurrentCheckout func() (string, error)
}

func defaultWorkbenchPreferDeps() *workbenchPreferDeps {
	return &workbenchPreferDeps{
		Picker:          defaultPreferredPickerDeps(),
		CurrentCheckout: project.CurrentCheckoutPath,
	}
}

var (
	workbenchPreferClear bool
	workbenchPreferNone  bool
)

// workbenchPreferCmd is the standalone door into the Preferred workbench picker
// and write path (ADR-0078), usable from inside any session (bindable to a tmux
// key). It writes the per-checkout preference only — it never touches a running
// session.
var workbenchPreferCmd = &cobra.Command{
	Use:   "prefer [name]",
	Short: "Set the preferred workbench for the current checkout",
	Long: `Set the per-checkout Preferred workbench (ADR-0078).

With no arguments, opens a picker of the Workbenches resolved for the current
checkout and writes the chosen preference. With a name, sets it
non-interactively (the name must resolve for this checkout). Use --clear to
reset to inheritance/default, or --none to pin "no workbench (here)".

This sets the preference only; it never touches a running session. The stored
preference auto-applies the next time a session is born for this checkout.`,
	Args:              cobra.MaximumNArgs(1),
	RunE:              runWorkbenchPrefer,
	ValidArgsFunction: completeWorkbenchPreferArgs,
}

func init() {
	workbenchPreferCmd.Flags().BoolVar(&workbenchPreferClear, "clear", false, "remove the current checkout's preference (reset to inheritance/default)")
	workbenchPreferCmd.Flags().BoolVar(&workbenchPreferNone, "none", false, `pin "no workbench (here)" (explicit none) for the current checkout`)
	workbenchCmd.AddCommand(workbenchPreferCmd)
}

func runWorkbenchPrefer(cmd *cobra.Command, args []string) error {
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	return runWorkbenchPreferWith(defaultWorkbenchPreferDeps(), name, workbenchPreferClear, workbenchPreferNone)
}

// runWorkbenchPreferWith is the injectable core. It resolves the current
// checkout, then dispatches to the slice-02 write path (clear / explicit-none /
// named) or opens the slice-02 picker when no selection is given.
func runWorkbenchPreferWith(d *workbenchPreferDeps, name string, clear, none bool) error {
	if clear && none {
		return fmt.Errorf("--clear and --none are mutually exclusive")
	}
	if (clear || none) && name != "" {
		return fmt.Errorf("cannot combine a workbench name with --clear/--none")
	}

	checkout, err := d.CurrentCheckout()
	if err != nil {
		return err
	}

	switch {
	case clear:
		return d.Picker.ClearPreferred(checkout)
	case none:
		return d.Picker.SetPreferred(checkout, "")
	case name != "":
		// Validate the name resolves for this checkout; error without writing.
		if !workbenchResolves(d.Picker.ResolveWorkbenches(checkout), name) {
			return fmt.Errorf("workbench %q does not resolve for %s", name, checkout)
		}
		return d.Picker.SetPreferred(checkout, name)
	default:
		// No selection: open the slice-02 picker for this checkout.
		return setPreferredWorkbench(d.Picker, checkout)
	}
}

// workbenchResolves reports whether name is a real Workbench in the resolved set.
func workbenchResolves(workbenches []config.Workbench, name string) bool {
	for _, wb := range workbenches {
		if wb.Name == name {
			return true
		}
	}
	return false
}

// workbenchPreferNames is the completion source for `pop workbench prefer <name>`:
// the names of the Workbenches resolved for the current checkout.
func workbenchPreferNames(d *workbenchPreferDeps) ([]string, error) {
	checkout, err := d.CurrentCheckout()
	if err != nil {
		return nil, err
	}
	workbenches := d.Picker.ResolveWorkbenches(checkout)
	names := make([]string, 0, len(workbenches))
	for _, wb := range workbenches {
		names = append(names, wb.Name)
	}
	return names, nil
}

func completeWorkbenchPreferArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names, err := workbenchPreferNames(defaultWorkbenchPreferDeps())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}
