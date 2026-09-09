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

// AttendedSession owns the attended choice for one interactive run. The menu
// and every later launch in that run share this value; no state is saved.
type AttendedSession struct {
	cfg          *config.Config
	override     string
	choice       *AgentGroupEntry
	verifyChoice *AgentGroupEntry
	refineChoice *AgentGroupEntry
	refine       *refineGateSession
	verify       *verifyGateSession
}

// NewAttendedSession starts one interactive run at the normal attended
// precedence: an explicit attended flag, then the resolved config and default.
func NewAttendedSession(cfg *config.Config, override string) *AttendedSession {
	return &AttendedSession{cfg: cfg, override: strings.TrimSpace(override)}
}

// Value is the config as last read.
func (g *AttendedSession) Value() *config.Config {
	if g == nil {
		return nil
	}
	return g.cfg
}

// EffectiveEntry is the same whole entry the next launch will use.
func (g *AttendedSession) EffectiveEntry() AgentGroupEntry {
	if g != nil && g.choice != nil {
		return *g.choice
	}
	if g == nil {
		return EffectiveAttendedEntry(nil)
	}
	return LaunchedAttendedEntry(g.cfg, g.override)
}

// ResolveAssistance applies the session choice before the attended flag. The
// agent command belongs to the selected entry and is never mixed with another.
func (g *AttendedSession) ResolveAssistance(d *Deps, agentCmd, prompt, runtimePath string) (*AgentAssistanceInvocation, error) {
	spec := ""
	if g != nil {
		spec = g.override
		if g.choice != nil {
			spec = g.choice.Cmd
		}
	}
	return ResolveAgentAssistanceInvocation(d, g.Value(), spec, agentCmd, prompt, runtimePath)
}

// Pick changes this run only. A cancel or picker error leaves the current
// choice intact and returns a notice for the gate.
func (g *AttendedSession) Pick(in io.Reader, out io.Writer, warn func(string, ...any)) string {
	choice, notice := PickAttendedAgent(g.Value(), in, out, warn)
	if g != nil && choice != nil {
		g.choice = choice
	}
	return notice
}

// promptGateMenu runs the shared inline gate menu and returns the chosen key.
// forceQuit is set when the interrupt gate's second SIGINT wins. reader is the
// shared per-run prompt reader used on the non-TTY line path so queued input
// across gates is not lost.
//
// The Assists item and its launch read the same Attended session choice. Tab
// changes that choice for this interactive run only (ADR-0266).
func promptGateMenu(out io.Writer, in io.Reader, reader *promptReader, spec ui.GateMenuSpec, interrupt <-chan os.Signal, cfg *AttendedSession) (key string, forceQuit bool, err error) {
	if in == nil {
		in = os.Stdin
	}
	if cfg != nil && cfg.refine != nil {
		cfg.refine.addItems(&spec)
	}
	for {
		spec.AttendedLabel = FormatAgentEntry(cfg.EffectiveEntry())
		spec.AttendedPickable = AttendedPickOffered(cfg.Value())
		if cfg != nil && cfg.verify != nil {
			cfg.verify.decorate(&spec, cfg.verifyChoice)
		}
		if cfg != nil && cfg.refine != nil {
			cfg.refine.decorate(&spec, cfg.refineChoice, cfg.Value())
		}
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
		if res.PickAction != "" {
			spec.FocusKey = res.PickAction
		}
		if res.PickAction != "" && cfg != nil {
			for _, item := range spec.Items {
				if item.Key != res.PickAction {
					continue
				}
				if item.Role == "verify" && cfg.verify != nil {
					choice, notice := pickAgentEntry(cfg.verify.entries(), in, out, promptWarner(out))
					spec.Notice = notice
					if choice != nil {
						cfg.verifyChoice = choice
					}
				}
				if item.Role == "refine" && cfg.refine != nil {
					choice, notice := pickAgentEntry(cfg.refine.entries(cfg.Value()), in, out, promptWarner(out))
					spec.Notice = notice
					if choice != nil {
						cfg.refineChoice = choice
					}
				}
			}
			if !res.PickAttended {
				continue
			}
		}
		if !res.PickAttended {
			return res.Key, false, nil
		}
		spec.Notice = cfg.Pick(in, out, promptWarner(out))
	}
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

// gateExplorePreamble names the map this set's builders were handed, on the same
// terms as the refine pointer above it: the mark with its reason, then the
// report as a pointer and never the document, which is a page long and would
// bury the menu it was printed above (ADR-0252).
//
// It is here because the human deciding on a set is the one who can act on a set
// that asked for a map and never got one — every attempt in it built blind — and
// because the report is what says which seam the work was supposed to sit in.
func gateExplorePreamble(res ExploreResolution) []string {
	line := explorationLine(res)
	if line == "" {
		return nil
	}
	return []string{"🧭 " + line}
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
