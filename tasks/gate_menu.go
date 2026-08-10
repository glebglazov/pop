package tasks

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// runGateMenu is the seam every gate prompt calls. Production points at
// ui.RunGateMenu; tests may swap it. Kept as a package var so the override
// picker (ADR-0196) wraps the same call site.
var runGateMenu = ui.RunGateMenu

// runAgentOverridePicker is the seam for the gate-side override picker.
var runAgentOverridePicker = ui.RunAgentOverridePicker

// promptGateMenu runs the shared inline gate menu and returns the chosen key.
// forceQuit is set when the interrupt gate's second SIGINT wins. reader is the
// shared per-run prompt reader used on the non-TTY line path so queued input
// across gates is not lost.
//
// alt+a opens the same two-level agent-override picker as the dashboard; a pick
// promotes into d.AgentOverrides for the rest of this process, then the menu
// re-opens with an updated AttendedLabel (ADR-0196 decisions 5 and 9).
func promptGateMenu(out io.Writer, in io.Reader, reader *promptReader, spec ui.GateMenuSpec, interrupt <-chan os.Signal, d *Deps, cfg *config.Config) (key string, forceQuit bool, err error) {
	if in == nil {
		in = os.Stdin
	}
	ensureAgentOverrides(d)
	reopened := false
	for {
		spec.AttendedLabel = FormatAgentEntry(EffectiveAttendedEntry(cfg, agentOverridesOf(d)))
		cfgRun := ui.GateMenuRunConfig{
			Interrupt:   interrupt,
			LineReader:  reader,
			Warn:        promptWarner(out),
			SkipContext: reopened,
		}
		res, err := runGateMenu(spec, in, out, cfgRun)
		if err != nil {
			return "", false, exitErr(ExitOperational, "read gate selection: %v", err)
		}
		if res.ForceQuit {
			return "", true, nil
		}
		if res.OpenOverride {
			if err := promptGateAgentOverride(out, in, d, cfg); err != nil {
				return "", false, err
			}
			reopened = true
			continue
		}
		return res.Key, false, nil
	}
}

func promptGateAgentOverride(out io.Writer, in io.Reader, d *Deps, cfg *config.Config) error {
	overrides := ensureAgentOverrides(d)
	choice, err := runAgentOverridePicker(AgentOverridePickerGroups(cfg), in, out, promptWarner(out))
	if err != nil {
		return exitErr(ExitOperational, "read agent override: %v", err)
	}
	if choice != nil {
		overrides.Promote(choice.Group, choice.Cmd)
	}
	return nil
}

func ensureAgentOverrides(d *Deps) *AgentOverrides {
	if d == nil {
		return NewAgentOverrides()
	}
	if d.AgentOverrides == nil {
		d.AgentOverrides = NewAgentOverrides()
	}
	return d.AgentOverrides
}

func agentOverridesOf(d *Deps) *AgentOverrides {
	if d == nil {
		return nil
	}
	return d.AgentOverrides
}

// AgentOverridePickerGroups builds the four Work groups for the shared
// override picker from config. Empty groups stay listed so the picker shape
// matches the dashboard (ADR-0196).
func AgentOverridePickerGroups(cfg *config.Config) []ui.AgentOverrideGroup {
	catalogs := AgentGroupCatalogs(cfg)
	out := make([]ui.AgentOverrideGroup, 0, len(catalogs))
	for _, catalog := range catalogs {
		g := ui.AgentOverrideGroup{ID: catalog.Group, Label: catalog.Group}
		for _, entry := range catalog.Entries {
			if entry.Problem != "" {
				continue
			}
			g.Entries = append(g.Entries, ui.AgentOverrideEntry{
				ID:    entry.Cmd,
				Label: FormatAgentEntry(entry),
			})
		}
		out = append(out, g)
	}
	return out
}

func gateInvocationDetails(invocation *AgentAssistanceInvocation) []string {
	if invocation == nil {
		return nil
	}
	var details []string
	if invocation.Display != "" {
		details = append(details, invocation.Display)
	}
	if invocation.Detail != "" {
		details = append(details, invocation.Detail)
	}
	return details
}

func gateWaiterPreamble(d *Deps, runtimePath string) []string {
	n := countRecoveryWaitersOnPath(d, runtimePath)
	if n <= 0 {
		return nil
	}
	noun := "waiters"
	if n == 1 {
		noun = "waiter"
	}
	return []string{fmt.Sprintf("⏳ %d quota %s blocked on this checkout", n, noun)}
}

func gateTaskBodyPreamble(taskFile, body string) []string {
	if body == "" {
		return nil
	}
	heading := fmt.Sprintf("--- %s ---", taskFile)
	lines := []string{heading}
	lines = append(lines, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
	lines = append(lines, strings.Repeat("-", len(heading)))
	return lines
}

func gateFindingsPreamble(findings string) []string {
	if strings.TrimSpace(findings) == "" {
		return nil
	}
	lines := []string{"  Findings:"}
	for _, line := range strings.Split(strings.TrimRight(findings, "\n"), "\n") {
		lines = append(lines, "    "+line)
	}
	return lines
}

func gateRemediationPreamble(d *Deps, taskSetID string, m *Manifest) []string {
	entries := CollectDoneRemediationHistory(d, m)
	if len(entries) == 0 {
		return nil
	}
	// Reuse the existing plain-text renderer so the block stays identical, then
	// split into lines for the shared menu preamble.
	text := FormatRemediationReviewBlock(taskSetID, entries)
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func joinPreamble(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
