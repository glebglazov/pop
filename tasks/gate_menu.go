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
// ui.RunGateMenu; tests may swap it.
var runGateMenu = ui.RunGateMenu

// promptGateMenu runs the shared inline gate menu and returns the chosen key.
// forceQuit is set when the interrupt gate's second SIGINT wins. reader is the
// shared per-run prompt reader used on the non-TTY line path so queued input
// across gates is not lost.
//
// The Assists item names the attended entry cfg resolves to, so a gate says
// which agent the default choice will launch (ADR-0196 decision 9). cfg is the
// merged config, override layer included: whatever the Config dashboard wrote is
// what the menu reports (ADR-0202 decision 5).
func promptGateMenu(out io.Writer, in io.Reader, reader *promptReader, spec ui.GateMenuSpec, interrupt <-chan os.Signal, cfg *config.Config) (key string, forceQuit bool, err error) {
	if in == nil {
		in = os.Stdin
	}
	spec.AttendedLabel = FormatAgentEntry(EffectiveAttendedEntry(cfg))
	res, err := runGateMenu(spec, in, out, ui.GateMenuRunConfig{
		Interrupt:  interrupt,
		LineReader: reader,
		Warn:       promptWarner(out),
	})
	if err != nil {
		return "", false, exitErr(ExitOperational, "read gate selection: %v", err)
	}
	if res.ForceQuit {
		return "", true, nil
	}
	return res.Key, false, nil
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

// gateRefineState is what the sign-off gate knows about Refine for one set:
// whether anything held its changeset to the standard, and the report of the
// last pass when there is one. The three travel together — the menu entry, the
// document the entry pages and the preamble line above them all say parts of the
// same answer — so they are resolved once, here, and handed round as one value.
type gateRefineState struct {
	Resolution RefineResolution
	Pointer    RefinePointer
	// HasReport is false for a set no pass has ever published a document for,
	// which is not the same as a set nothing has refined: an interrupted pass
	// leaves the previous report in place.
	HasReport bool
}

// resolveGateRefineState reads both halves through their own single
// resolutions. It runs each time round the gate menu: a report written while the
// gate was open is still the one to point at.
func resolveGateRefineState(d *Deps, cfg *config.Config, m *Manifest) gateRefineState {
	pointer, ok := latestRefinePointer(d, m)
	return gateRefineState{Resolution: ResolveRefineMark(d, cfg, m), Pointer: pointer, HasReport: ok}
}

// gateRefinePreamble tells the human deciding on a set whether its changeset was
// refined, and where the last pass's report is — a pointer, never the document,
// which is long enough to bury the menu it would be printed above (ADR-0252).
//
// The mark refuses nothing and holds nothing back (ADR-0260 decision 6). It is
// here because the human at this gate is the only one who can act on a set the
// standard was never applied to, and because a set whose pass died looked, until
// now, exactly like one that was refined and found clean.
func gateRefinePreamble(refine gateRefineState) []string {
	var parts []string
	if phrase := refineMarkPhrase(refine.Resolution); phrase != "" {
		parts = append(parts, phrase)
	}
	if refine.HasReport {
		parts = append(parts, refine.Pointer.Summary())
	}
	if len(parts) == 0 {
		return nil
	}
	return []string{"📝 " + strings.Join(parts, " · ")}
}

// gateRefineEntryDetails is what the paging entry says under its label: the
// document it will open, and the mark from the same resolution the preamble
// reads (ADR-0260 decision 5). The mark belongs on the entry as well as above
// it, because a report is left in place by a pass that did not refine — a
// reader choosing to read one has to know whether it describes the changeset
// they are signing off.
func gateRefineEntryDetails(refine gateRefineState) []string {
	details := []string{refine.Pointer.Path}
	if phrase := refineMarkPhrase(refine.Resolution); phrase != "" {
		details = append(details, phrase)
	}
	return details
}

// gateVerifyPreamble tells the same human that a Verify report of the set
// exists, and against which commit — the pointer only, on the same terms as the
// refine one above it (ADR-0245). It answers why verification judged as it did,
// never whether the judgment still stands: that stays the Verified-at badge's
// question. Returns no lines for a set that has never been verified.
func gateVerifyPreamble(p ReportPointer, ok bool) []string {
	if !ok {
		return nil
	}
	return []string{"🔍 " + p.Summary()}
}

func joinPreamble(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
