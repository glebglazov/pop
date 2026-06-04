package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
	"github.com/spf13/cobra"
)

// `pop doctor` is the read-only readiness report (PRD: Doctor). It prints the
// canonical command families and their nested checks. Doctor adds no state
// logic of its own: every Integration component state is computed through the
// same read paths the wizard and removal paths use (the catalog support matrix,
// the render engine, the link installer's ownership/staleness checks, and the
// gitignore presence check). It never installs or repairs — actionable checks
// simply carry the copy-paste `pop integrate` command that fixes them.
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
	integrate                 *integrateDeps
	tmuxAvailable             func() bool
	loadProjectConfig         func() (*config.Config, error)
	projectConfigureAvailable func() bool
	expandProjectConfig       func(*config.Config) ([]config.ExpandedPath, error)
	expandProjects            func([]config.ExpandedPath) ([]project.ExpandedProject, []string)
	projectSessionActivity    func() map[string]int64
	detectRepoContext         func() (*project.RepoContext, error)
	listWorktrees             func(*project.RepoContext) ([]project.Worktree, error)
	daemonRunning             func() bool
}

func defaultDoctorDeps() *doctorDeps {
	return &doctorDeps{
		integrate: defaultIntegrateDeps(),
		tmuxAvailable: func() bool {
			_, err := exec.LookPath("tmux")
			return err == nil
		},
		loadProjectConfig: func() (*config.Config, error) {
			path := config.DefaultConfigPath()
			return config.Load(path)
		},
		projectConfigureAvailable: func() bool { return true },
		expandProjectConfig: func(cfg *config.Config) ([]config.ExpandedPath, error) {
			return cfg.ExpandProjects()
		},
		expandProjects: func(paths []config.ExpandedPath) ([]project.ExpandedProject, []string) {
			return expandProjectsWith(project.DefaultDeps(), paths)
		},
		projectSessionActivity: historyTmuxSessionActivity,
		detectRepoContext:      project.DetectRepoContext,
		listWorktrees:          project.ListWorktrees,
		daemonRunning: func() bool {
			return monitor.IsDaemonRunning(monitor.DefaultPIDPath())
		},
	}
}

// doctorStatus is the readiness state for a command family or nested check.
type doctorStatus string

const (
	doctorStatusOK       doctorStatus = "OK"
	doctorStatusPartial  doctorStatus = "Partial"
	doctorStatusDegraded doctorStatus = "Degraded"
	doctorStatusBlocked  doctorStatus = "Blocked"
	doctorStatusNA       doctorStatus = "N/A"
)

// doctorCheck is one nested assessment under a command family. It is generic
// to the command family, not to agents; Integration checks happen to include
// agent names in their labels because the underlying artifacts are per-agent.
type doctorCheck struct {
	label      string
	status     doctorStatus
	detail     string
	nextAction string
}

// doctorFamilyReport is the readiness report for one canonical command family.
type doctorFamilyReport struct {
	command string
	status  doctorStatus
	reason  string
	checks  []doctorCheck
}

type doctorReport struct {
	families []doctorFamilyReport
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report pop command-family readiness",
	Long: `Report pop's readiness on this machine — strictly read-only.

Doctor prints the canonical command families (project, worktree, monitor,
pane, workload, integrate) and nested checks for each family. Integration
state is computed from pop's existing read paths — the component catalog, the
render engine, the link installer's ownership checks, and the gitignore
presence check — so doctor never installs, repairs, or writes anything.

Each actionable check carries the copy-paste command that fixes it (an
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

// canonicalDoctorCommands is the ordered family model for Doctor. The strings
// are the command-family names Doctor reports and future slices extend.
var canonicalDoctorCommands = []string{
	"pop project",
	"pop worktree",
	"pop monitor",
	"pop pane",
	"pop workload",
	"pop integrate",
}

// buildDoctorReport assembles command-family readiness. It performs only
// reads; a failure here means the report itself could not be produced (a
// non-zero exit), not an unhealthy finding.
func buildDoctorReport(d *doctorDeps) (*doctorReport, error) {
	report := &doctorReport{
		families: []doctorFamilyReport{
			familyReport("pop project", doctorProjectChecks(d)),
			familyReport("pop worktree", doctorWorktreeChecks(d)),
			familyReport("pop monitor", []doctorCheck{
				doctorBoolCheck("monitor daemon running", d.daemonRunning(), "monitor daemon is not running", "", "pop monitor"),
			}),
			familyReport("pop pane", []doctorCheck{
				doctorBoolCheck("tmux available", d.tmuxAvailable(), "tmux executable was not found", "", ""),
			}),
			familyReport("pop workload", []doctorCheck{
				{label: "workload-specific checks", status: doctorStatusNA, detail: "no workload-specific readiness checks yet"},
			}),
		},
	}

	home, err := d.integrate.userHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	var integrationChecks []doctorCheck
	for _, agent := range integrationAgents {
		for _, comp := range integrationCatalog {
			state, err := doctorComponentState(d.integrate, home, comp.id, agent)
			if err != nil {
				return nil, err
			}
			integrationChecks = append(integrationChecks, doctorIntegrationCheck(agent, comp.id, state))
		}
	}
	report.families = append(report.families, familyReport("pop integrate", integrationChecks))
	return report, nil
}

func historyTmuxSessionActivity() map[string]int64 {
	return history.TmuxSessionActivity()
}

func doctorProjectChecks(d *doctorDeps) []doctorCheck {
	checks := []doctorCheck{
		doctorBoolCheck("tmux available", d.tmuxAvailable(), "tmux executable was not found", "", ""),
	}

	cfg, err := d.loadProjectConfig()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && d.projectConfigureAvailable() {
			checks = append(checks, doctorCheck{
				label:      "project config",
				status:     doctorStatusPartial,
				detail:     "config missing; first-run configure is available",
				nextAction: "pop configure",
			})
			return checks
		}
		checks = append(checks, doctorCheck{
			label:  "project config",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("failed to load config: %v", err),
		})
		return checks
	}
	checks = append(checks, doctorCheck{label: "project config", status: doctorStatusOK, detail: "config loads"})

	paths, err := d.expandProjectConfig(cfg)
	if err != nil {
		checks = append(checks, doctorCheck{
			label:  "selectable projects and sessions",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("failed to expand configured projects: %v", err),
		})
		return checks
	}

	expanded, failed := d.expandProjects(paths)
	standaloneSessions := len(d.projectSessionActivity())
	selectable := len(expanded) + standaloneSessions
	if selectable == 0 {
		detail := "no configured projects or standalone sessions discovered"
		if len(paths) > 0 && len(failed) > 0 {
			detail = fmt.Sprintf("failed to discover any selectable configured projects: %d errors", len(failed))
		}
		checks = append(checks, doctorCheck{
			label:  "selectable projects and sessions",
			status: doctorStatusBlocked,
			detail: detail,
		})
		return checks
	}

	detail := fmt.Sprintf("%d selectable project/session path(s) discovered", selectable)
	if len(failed) > 0 {
		detail = fmt.Sprintf("%s; %d configured project(s) failed to expand", detail, len(failed))
	}
	checks = append(checks, doctorCheck{
		label:  "selectable projects and sessions",
		status: doctorStatusOK,
		detail: detail,
	})
	return checks
}

func doctorWorktreeChecks(d *doctorDeps) []doctorCheck {
	ctx, err := d.detectRepoContext()
	if err != nil {
		return []doctorCheck{{
			label:  "git repository detected",
			status: doctorStatusBlocked,
			detail: "not in a git repository",
		}}
	}

	checks := []doctorCheck{{
		label:  "git repository detected",
		status: doctorStatusOK,
		detail: ctx.GitRoot,
	}}
	worktrees, err := d.listWorktrees(ctx)
	if err != nil {
		checks = append(checks, doctorCheck{
			label:  "worktrees listed",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("failed to list worktrees: %v", err),
		})
		return checks
	}
	checks = append(checks, doctorCheck{
		label:  "worktrees listed",
		status: doctorStatusOK,
		detail: fmt.Sprintf("%d linked worktree(s) listed", len(worktrees)),
	})
	return checks
}

func doctorBoolCheck(label string, ok bool, failDetail, okDetail, nextAction string) doctorCheck {
	if ok {
		return doctorCheck{label: label, status: doctorStatusOK, detail: okDetail}
	}
	return doctorCheck{label: label, status: doctorStatusBlocked, detail: failDetail, nextAction: nextAction}
}

func familyReport(command string, checks []doctorCheck) doctorFamilyReport {
	status, reason := aggregateDoctorStatus(checks)
	return doctorFamilyReport{
		command: command,
		status:  status,
		reason:  reason,
		checks:  checks,
	}
}

func aggregateDoctorStatus(checks []doctorCheck) (doctorStatus, string) {
	if len(checks) == 0 {
		return doctorStatusNA, "no checks defined"
	}
	priority := map[doctorStatus]int{
		doctorStatusOK:       0,
		doctorStatusNA:       1,
		doctorStatusPartial:  2,
		doctorStatusDegraded: 3,
		doctorStatusBlocked:  4,
	}
	worst := doctorStatusOK
	reason := ""
	allNA := true
	for _, check := range checks {
		if check.status != doctorStatusNA {
			allNA = false
		}
		if priority[check.status] > priority[worst] {
			worst = check.status
			reason = doctorStatusReason(check)
		}
	}
	if allNA {
		return doctorStatusNA, doctorStatusReason(checks[0])
	}
	if worst == doctorStatusNA {
		return doctorStatusOK, ""
	}
	if worst != doctorStatusOK && reason == "" {
		reason = "non-OK check has no detail"
	}
	return worst, reason
}

func doctorStatusReason(check doctorCheck) string {
	if check.status == doctorStatusOK {
		return ""
	}
	if check.detail != "" {
		return check.detail
	}
	if check.nextAction != "" {
		return check.nextAction
	}
	return fmt.Sprintf("%s is %s", check.label, check.status)
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

// doctorFix returns the copy-paste fix command for an actionable check, or ""
// for a healthy (installed-current) or not-supported check. A conflict is
// resolved by removing the unowned entry and re-running integrate, so its
// command leads with that removal (ADR 0011 conflict resolution: remove, then
// re-integrate).
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

func doctorIntegrationCheck(agent string, id ComponentID, state componentStateInfo) doctorCheck {
	check := doctorCheck{
		label:      fmt.Sprintf("%s %s", agent, id),
		status:     doctorStatusFromComponent(state.kind),
		detail:     doctorComponentDetail(agent, id, state),
		nextAction: doctorFix(agent, id, state),
	}
	return check
}

func doctorStatusFromComponent(kind componentStateKind) doctorStatus {
	switch kind {
	case stateInstalledCurrent:
		return doctorStatusOK
	case stateStale, stateNotInstalled:
		return doctorStatusPartial
	case stateConflict:
		return doctorStatusBlocked
	case stateNotSupported:
		return doctorStatusNA
	default:
		return doctorStatusDegraded
	}
}

func doctorComponentDetail(agent string, id ComponentID, state componentStateInfo) string {
	switch state.kind {
	case stateInstalledCurrent:
		return doctorStateLabel(state.kind)
	case stateStale:
		return fmt.Sprintf("%s %s is stale", agent, id)
	case stateNotInstalled:
		return fmt.Sprintf("%s %s is not installed", agent, id)
	case stateConflict:
		return fmt.Sprintf("%s %s conflicts at %s", agent, id, state.conflictPath)
	case stateNotSupported:
		return fmt.Sprintf("%s does not support %s", agent, id)
	default:
		return fmt.Sprintf("%s %s state is unknown", agent, id)
	}
}

// doctorStateLabel renders a component state for check details.
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

const (
	doctorANSIReset  = "\033[0m"
	doctorANSIBold   = "\033[1m"
	doctorANSIDim    = "\033[2m"
	doctorANSIRed    = "\033[31m"
	doctorANSIGreen  = "\033[32m"
	doctorANSIYellow = "\033[33m"
	doctorANSICyan   = "\033[36m"
)

func renderDoctorReport(w io.Writer, report *doctorReport) {
	color := doctorColorEnabled(w)
	fmt.Fprintln(w, doctorStyled(color, doctorANSIBold, "Command-family readiness"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "STATUS    COMMAND        SUMMARY")
	for _, family := range report.families {
		fmt.Fprintf(
			w,
			"%s  %-13s  %s\n",
			doctorStatusCell(color, family.status),
			family.command,
			doctorFamilySummary(family),
		)
		for _, check := range family.checks {
			line := fmt.Sprintf("  %s  %s", doctorStatusCell(color, check.status), check.label)
			if check.detail != "" {
				line += fmt.Sprintf(" - %s", check.detail)
			}
			if check.nextAction != "" {
				line += fmt.Sprintf(" (next: %s)", check.nextAction)
			}
			fmt.Fprintln(w, line)
		}
	}
}

func doctorFamilySummary(family doctorFamilyReport) string {
	if family.reason != "" {
		return family.reason
	}
	return "ready"
}

func doctorStatusStyle(status doctorStatus) string {
	switch status {
	case doctorStatusOK:
		return doctorANSIGreen
	case doctorStatusPartial, doctorStatusNA:
		return doctorANSIYellow
	case doctorStatusBlocked:
		return doctorANSIRed
	case doctorStatusDegraded:
		return doctorANSICyan
	default:
		return doctorANSIDim
	}
}

func doctorStatusCell(color bool, status doctorStatus) string {
	return doctorStyled(color, doctorStatusStyle(status), fmt.Sprintf("%-8s", status))
}

func doctorColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func doctorStyled(color bool, style, text string) string {
	if !color {
		return text
	}
	return style + text + doctorANSIReset
}
