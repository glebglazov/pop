package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/monitor"
	"github.com/spf13/cobra"
)

// `pop doctor` is the read-only readiness report (PRD: Doctor). It prints the
// three core checks (tmux present, config loads, monitor daemon running) and a
// per-agent table of Integration component states. Doctor adds no state logic
// of its own: every component state is computed through the same check seams
// the wizard and removal paths use (the catalog support matrix, the render
// engine, the link installer's ownership/staleness checks, and the gitignore
// presence check). It never installs or repairs — actionable rows simply carry
// the copy-paste `pop integrate` command that fixes them.
//
// Exit status mirrors `workload status`: it reflects rendering success, not the
// health findings. A machine with everything broken still exits 0; only a
// failure to produce the report (e.g. no home directory) is a non-zero exit.

// doctorDeps holds the seams Doctor reads through. integrate carries the
// read-only filesystem/git seams shared with the integrate command (Doctor
// calls only their read paths). The three core-check closures are injected so
// tests can compose healthy and unhealthy machines without a real tmux,
// config file, or daemon.
type doctorDeps struct {
	integrate     *integrateDeps
	tmuxAvailable func() bool
	configCheck   func() (ok bool, detail string)
	daemonRunning func() bool
}

func defaultDoctorDeps() *doctorDeps {
	return &doctorDeps{
		integrate: defaultIntegrateDeps(),
		tmuxAvailable: func() bool {
			_, err := exec.LookPath("tmux")
			return err == nil
		},
		configCheck: func() (bool, string) {
			path := config.DefaultConfigPath()
			if _, err := config.Load(path); err != nil {
				return false, fmt.Sprintf("%s: %v", path, err)
			}
			return true, path
		},
		daemonRunning: func() bool {
			return monitor.IsDaemonRunning(monitor.DefaultPIDPath())
		},
	}
}

// doctorCoreCheck is one core-readiness line.
type doctorCoreCheck struct {
	label  string
	ok     bool
	detail string
}

// doctorComponentRow is one (agent, component) cell of the state table, with
// the copy-paste fix command for actionable states (empty otherwise).
type doctorComponentRow struct {
	agent     string
	component ComponentID
	state     componentStateInfo
	fix       string
}

type doctorReport struct {
	core []doctorCoreCheck
	rows []doctorComponentRow
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report pop readiness and per-agent integration component states",
	Long: `Report pop's readiness on this machine — strictly read-only.

Doctor prints three core checks (tmux available, config loads, monitor daemon
running) followed by a per-agent table of Integration component states:
installed-current, stale, not installed, conflict, or not supported. Every
state is computed from pop's existing check seams — the component catalog, the
render engine, the link installer's ownership checks, and the gitignore
presence check — so doctor never installs, repairs, or writes anything.

Each actionable row carries the copy-paste command that fixes it (an
` + "`pop integrate`" + ` invocation). Doctor always exits 0 when it succeeds in
rendering the report; the exit status reflects rendering, not the findings.`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	return runDoctorWith(defaultDoctorDeps(), os.Stdout)
}

func runDoctorWith(d *doctorDeps, w io.Writer) error {
	report, err := buildDoctorReport(d)
	if err != nil {
		return err
	}
	renderDoctorReport(w, report)
	return nil
}

// buildDoctorReport assembles the core checks and the per-agent component-state
// table. It performs only reads; a failure here means the report itself could
// not be produced (a non-zero exit), not an unhealthy finding.
func buildDoctorReport(d *doctorDeps) (*doctorReport, error) {
	report := &doctorReport{}

	report.core = append(report.core, doctorCoreCheck{label: "tmux available", ok: d.tmuxAvailable()})
	cfgOK, cfgDetail := d.configCheck()
	report.core = append(report.core, doctorCoreCheck{label: "config loads", ok: cfgOK, detail: cfgDetail})
	report.core = append(report.core, doctorCoreCheck{label: "monitor daemon running", ok: d.daemonRunning()})

	home, err := d.integrate.userHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	for _, agent := range integrationAgents {
		for _, comp := range integrationCatalog {
			state, err := doctorComponentState(d.integrate, home, comp.id, agent)
			if err != nil {
				return nil, err
			}
			report.rows = append(report.rows, doctorComponentRow{
				agent:     agent,
				component: comp.id,
				state:     state,
				fix:       doctorFix(agent, comp.id, state),
			})
		}
	}
	return report, nil
}

// doctorComponentState computes a component's state for an agent by composing
// the existing check seams — it owns no state logic. File-based components
// (the pane skill, the workload planning skills) defer entirely to
// wizardFileComponentState (catalog support + conflict + installed + stale).
// The status wiring and the global-gitignore step have no render tree, so their
// state is the binary installed/not-installed signal from their own seams
// (statusWiringInstalled, gitignoreConfigured).
func doctorComponentState(d *integrateDeps, home string, id ComponentID, agent string) (componentStateInfo, error) {
	comp, ok := lookupComponent(id)
	if !ok {
		return componentStateInfo{}, fmt.Errorf("unknown component %q", id)
	}
	if !comp.supported(agent) {
		return componentStateInfo{kind: stateNotSupported}, nil
	}
	switch id {
	case ComponentStatusWiring:
		installed, err := statusWiringInstalled(d, home, agent)
		if err != nil {
			return componentStateInfo{}, err
		}
		return installedState(installed), nil
	case ComponentWorkloadGitignore:
		configured, _, err := gitignoreConfigured(d)
		if err != nil {
			return componentStateInfo{}, err
		}
		return installedState(configured), nil
	default:
		return wizardFileComponentState(d, home, id, agent)
	}
}

// installedState maps a plain installed bool onto the shared state enum. These
// seams report only presence, not staleness, so present means installed-current.
func installedState(installed bool) componentStateInfo {
	if installed {
		return componentStateInfo{kind: stateInstalledCurrent}
	}
	return componentStateInfo{kind: stateNotInstalled}
}

// doctorComponentFlag maps a component to the `pop integrate` flag that selects
// it non-interactively. The status wiring has no flag — it is the core
// component a bare `pop integrate <agent>` installs.
var doctorComponentFlag = map[ComponentID]string{
	ComponentStatusWiring:      "",
	ComponentPaneSkill:         "--pane-skill",
	ComponentWorkloadSkills:    "--workload-skills",
	ComponentWorkloadGitignore: "--workload-gitignore",
}

// integrateInvocation builds the copy-paste integrate command that installs the
// given component for the agent.
func integrateInvocation(agent string, id ComponentID) string {
	if flag := doctorComponentFlag[id]; flag != "" {
		return fmt.Sprintf("pop integrate %s %s", agent, flag)
	}
	return fmt.Sprintf("pop integrate %s", agent)
}

// doctorFix returns the copy-paste fix command for an actionable row, or "" for
// a healthy (installed-current) or not-supported row. A conflict is resolved by
// removing the unowned entry and re-running integrate, so its command leads with
// that removal (ADR 0011 conflict resolution: remove, then re-integrate).
func doctorFix(agent string, id ComponentID, state componentStateInfo) string {
	switch state.kind {
	case stateNotInstalled, stateStale:
		return integrateInvocation(agent, id)
	case stateConflict:
		return fmt.Sprintf("rm %s && %s", state.conflictPath, integrateInvocation(agent, id))
	default:
		return ""
	}
}

// doctorStateLabel renders a component state for the table.
func doctorStateLabel(kind componentStateKind) string {
	switch kind {
	case stateInstalledCurrent:
		return "installed-current"
	case stateStale:
		return "stale"
	case stateNotInstalled:
		return "not installed"
	case stateConflict:
		return "conflict"
	case stateNotSupported:
		return "not supported"
	default:
		return "unknown"
	}
}

func renderDoctorReport(w io.Writer, report *doctorReport) {
	fmt.Fprintln(w, "Core readiness:")
	for _, c := range report.core {
		mark := "ok"
		if !c.ok {
			mark = "FAIL"
		}
		if c.detail != "" {
			fmt.Fprintf(w, "  [%-4s] %s (%s)\n", mark, c.label, c.detail)
		} else {
			fmt.Fprintf(w, "  [%-4s] %s\n", mark, c.label)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Agent integrations:")

	const (
		agentW = 10
		compW  = 22
		stateW = 19
	)
	fmt.Fprintf(w, "  %-*s%-*s%-*s%s\n", agentW, "AGENT", compW, "COMPONENT", stateW, "STATE", "FIX")
	for _, r := range report.rows {
		fmt.Fprintf(w, "  %-*s%-*s%-*s%s\n",
			agentW, r.agent,
			compW, string(r.component),
			stateW, doctorStateLabel(r.state.kind),
			r.fix,
		)
	}
}
