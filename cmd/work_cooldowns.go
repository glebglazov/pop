package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

// The agent-quota cooldown verbs. They sit under `pop work` beside `status`
// because a cooldown is machine-global scheduling state — it belongs to no task
// set and no project — and because the two questions a human has when a drain
// will not start are what is holding it and how to let it go (ADR-0235).

var workCooldownsCmd = &cobra.Command{
	Use:   "cooldowns",
	Short: "List live agent quota cooldowns, and whether each expiry was stated or guessed",
	Long: `List the machine-global agent-preset quota cooldowns still in force.

Each row says where its expiry came from. A stated expiry is the reset instant
the provider itself named. A guessed one is pop's own backstop, dated from the
window class the refusal named — the exhausted preset is asked whether it will
run yet, and the cooldown ends on that answer rather than on the backstop.

clear drops one preset's cooldown and releases every drain parked on it, which
is what to reach for when pop guessed and the guess is plainly wrong.`,
	Args: cobra.NoArgs,
	RunE: runWorkCooldowns,
}

var workCooldownsClearCmd = &cobra.Command{
	Use:               "clear <preset>",
	Short:             "Drop one preset's quota cooldown and release the drains parked on it",
	Args:              cobra.ExactArgs(1),
	RunE:              runWorkCooldownsClear,
	ValidArgsFunction: completeCooledPreset,
}

// completeCooledPreset offers only the presets that actually have a cooldown:
// the argument names a live row, so anything else is a typo.
func completeCooledPreset(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	rows, err := tasks.LiveAgentQuotaCooldownsWith(cmdLayerDeps().tasksDeps(), time.Now())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	presets := make([]string, 0, len(rows))
	for _, row := range rows {
		presets = append(presets, row.Preset)
	}
	return filterShellCompletions(presets, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func init() {
	workCmd.AddCommand(workCooldownsCmd)
	workCooldownsCmd.AddCommand(workCooldownsClearCmd)
}

func runWorkCooldowns(cmd *cobra.Command, args []string) error {
	return runWorkCooldownsWith(cmdLayerDeps().tasksDeps(), cmdOut(cmd), time.Now())
}

func runWorkCooldownsWith(d *tasks.Deps, w io.Writer, now time.Time) error {
	rows, err := tasks.LiveAgentQuotaCooldownsWith(d, now)
	if err != nil {
		return fmt.Errorf("work cooldowns: %w", err)
	}
	renderWorkCooldowns(w, rows)
	return nil
}

func runWorkCooldownsClear(cmd *cobra.Command, args []string) error {
	return runWorkCooldownsClearWith(cmdLayerDeps().tasksDeps(), cmdOut(cmd), args[0], time.Now())
}

func runWorkCooldownsClearWith(d *tasks.Deps, w io.Writer, preset string, now time.Time) error {
	cleared, err := tasks.ClearAgentQuotaCooldownWith(d, preset, now)
	if err != nil {
		return fmt.Errorf("work cooldowns clear: %w", err)
	}
	if !cleared {
		fmt.Fprintf(w, "%s has no quota cooldown\n", preset)
		return nil
	}
	fmt.Fprintf(w, "cleared %s quota cooldown\n", preset)
	return nil
}

// renderWorkCooldowns prints one row per live cooldown. The origin column is the
// point of the surface: without it a provider's reset and a ceiling pop invented
// are the same number, and a human with no way to tell them apart edits the
// database by hand (ADR-0235). A guessed row also carries when the preset is
// next asked, since that — not the expiry — is when the cooldown is likely to
// end.
func renderWorkCooldowns(w io.Writer, rows []tasks.AgentQuotaCooldownView) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "No agent quota cooldowns are in force.")
		return
	}
	fmt.Fprintf(w, "%-9s %-8s %-25s %-21s %s\n", "preset", "origin", "expires", "window", "next probe")
	for _, row := range rows {
		fmt.Fprintf(w, "%-9s %-8s %-25s %-21s %s\n",
			row.Preset,
			row.Origin(),
			row.Until.Local().Format(time.RFC3339),
			workCooldownWindow(row),
			workCooldownNextProbe(row))
	}
}

// workCooldownWindow names the Quota window class a guess was dated from. A
// stated expiry needs no window: the provider gave the instant outright, so the
// class it came from would explain nothing about the number.
func workCooldownWindow(row tasks.AgentQuotaCooldownView) string {
	if !row.Guessed {
		return "-"
	}
	return row.Class.Label() + " backstop"
}

func workCooldownNextProbe(row tasks.AgentQuotaCooldownView) string {
	if row.NextProbeAt.IsZero() {
		return "-"
	}
	return row.NextProbeAt.Local().Format(time.RFC3339)
}
