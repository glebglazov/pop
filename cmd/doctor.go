package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/integrate"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/release"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

// `pop doctor` is the read-only readiness report (PRD: Doctor). It prints the
// canonical command families and their nested checks. Doctor adds no state
// logic of its own: where a family needs agent or integration evidence, it
// computes that evidence through the same read paths the wizard and removal
// paths use. It never installs or repairs — actionable checks simply carry the
// copy-paste command that fixes them.
//
// Exit status mirrors `task status`: it reflects rendering success, not the
// health findings. A machine with everything broken still exits 0; only a
// failure to produce the report (e.g. no home directory) is a non-zero exit.

// doctorDeps holds the seams Doctor reads through. integrate carries the
// read-only filesystem/git seams shared with the integrate command (Doctor
// calls only their read paths). The three core-check closures are injected so
// tests can compose healthy and unhealthy machines without a real tmux,
// config file, or daemon.
type doctorDeps struct {
	integrate                 *integrate.Deps
	tmuxAvailable             func() bool
	loadProjectConfig         func() (*config.Config, error)
	projectConfigureAvailable func() bool
	expandProjectConfig       func(*config.Config) ([]config.ExpandedPath, error)
	expandProjects            func([]config.ExpandedPath) ([]project.ExpandedProject, []string)
	projectSessionActivity    func() map[string]int64
	detectRepoContext         func() (*project.RepoContext, error)
	listWorktrees             func(*project.RepoContext) ([]project.Worktree, error)
	daemonRunning             func() bool
	monitorDaemonStartable    func() bool
	loadMonitorState          func() (*monitor.State, error)
	paneSessionAddressable    func() (string, error)
	agentIntent               func() (*integrate.AgentIntentReport, error)
	explicitAgentContext      func() []string
	agentExecutableAvailable  func(string) bool
	taskStorageWritable       func() (string, error)
	scanWayfinderMaps         func() (int, error)
	legacyTaskSets            func() ([]string, error)
	orphanedTaskStorage       func() ([]tasks.OrphanedStorage, error)
	legacyLayoutStorage       func() ([]string, error)
	updateCheck               func() release.Result
	agentCatalog              func() []tasks.AgentCatalogRow
	probeAgentAuthentication  func(preset string) tasks.AgentAuthenticationProbe
}

func defaultDoctorDeps() *doctorDeps {
	d := &doctorDeps{
		integrate: integrate.DefaultDeps(),
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
			return expandProjectsWith(cmdLayerDeps().projectDeps(), paths)
		},
		projectSessionActivity: historyTmuxSessionActivity,
		detectRepoContext:      project.DetectRepoContext,
		listWorktrees:          project.ListWorktrees,
		daemonRunning: func() bool {
			cfg := loadConfigQuietly()
			if cfg.PaneMonitoringTCPServer() {
				_, err := monitor.Handshake(monitorAddr(cfg))
				return err == nil
			}
			return monitor.IsDaemonRunning(monitor.DefaultPIDPath())
		},
		monitorDaemonStartable: func() bool {
			exe, err := os.Executable()
			if err != nil {
				return false
			}
			info, err := os.Stat(exe)
			return err == nil && !info.IsDir()
		},
		loadMonitorState: func() (*monitor.State, error) {
			return monitor.Load(monitor.DefaultStatePath())
		},
		paneSessionAddressable: defaultPaneSessionAddressable,
		explicitAgentContext:     func() []string { return nil },
		agentExecutableAvailable: tasks.AgentExecutableAvailable,
		taskStorageWritable: func() (string, error) { return tasks.ProbeStorageWritable(cmdLayerDeps().tasksDeps()) },
		scanWayfinderMaps: func() (int, error) {
			maps, err := wayfinder.ScanMaps(cmdLayerDeps().wayfinderDeps(), cmdLayerDeps().WorkDir())
			if err != nil {
				return 0, err
			}
			return len(maps), nil
		},
		legacyTaskSets: func() ([]string, error) {
			return tasks.LegacyTaskSetIDs(cmdLayerDeps().tasksDeps(), cmdLayerDeps().WorkDir())
		},
		orphanedTaskStorage: func() ([]tasks.OrphanedStorage, error) {
			return tasks.FindOrphanedStorage(cmdLayerDeps().tasksDeps())
		},
		legacyLayoutStorage: func() ([]string, error) {
			return tasks.LegacyLayoutStorageDirs(cmdLayerDeps().tasksDeps())
		},
		updateCheck: func() release.Result { return release.Check(buildVersion()) },
	}
	d.agentCatalog = func() []tasks.AgentCatalogRow {
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil && !os.IsNotExist(err) {
			cfg = nil
		}
		if os.IsNotExist(err) {
			cfg = nil
		}
		return tasks.AgentCatalogWithConfig(cmdLayerDeps().tasksDeps(), cfg)
	}
	d.probeAgentAuthentication = func(preset string) tasks.AgentAuthenticationProbe {
		return tasks.ProbeAgentAuthentication(cmdLayerDeps().tasksDeps(), cmdLayerDeps().WorkDir(), preset)
	}
	d.agentIntent = func() (*integrate.AgentIntentReport, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return integrate.DetectAgentIntent(d.integrate, home, config.Load, nil, tasks.AgentExecutableAvailable)
	}
	return d
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
// to the command family, not to an agent/component matrix; checks include agent
// names only when a family needs that evidence to explain readiness.
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
	update   release.Result
}

type doctorAgentStatusWiring = integrate.AgentComponentState

type doctorAgentIntentReport = integrate.AgentIntentReport
type doctorAgentIntent = integrate.AgentIntent
type doctorAgentSuggestion = integrate.AgentSuggestion

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report pop command-family readiness",
	Long: `Report pop's readiness on this machine — strictly read-only.

Doctor prints the canonical command families (project, worktree, monitor,
pane, tasks, wayfinder, integrate) and nested checks for each family. When a family
depends on agent setup, Doctor reads Pop's existing integration evidence to
explain that family's readiness; it does not present a support matrix or
per-agent component inventory as the report.

Each actionable check carries a copy-paste command that fixes it. Doctor
always exits 0 when it succeeds in rendering the report; the exit status
reflects rendering, not the findings.`,
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
	"pop tasks",
	"pop wayfinder",
	"pop integrate",
}

// buildDoctorReport assembles command-family readiness. It performs only
// reads; a failure here means the report itself could not be produced (a
// non-zero exit), not an unhealthy finding.
func buildDoctorReport(d *doctorDeps) (*doctorReport, error) {
	intent, err := d.agentIntent()
	if err != nil {
		return nil, err
	}
	integrate.MergeExplicitAgentContext(intent, d.explicitAgentContext())
	report := &doctorReport{
		families: []doctorFamilyReport{
			familyReport("pop project", doctorProjectChecks(d)),
			familyReport("pop worktree", doctorWorktreeChecks(d)),
			familyReport("pop monitor", doctorMonitorChecks(d, intent)),
			familyReport("pop pane", doctorPaneChecks(d)),
			familyReport("pop tasks", doctorTaskChecks(d)),
			familyReport("pop wayfinder", doctorWayfinderChecks(d, intent)),
		},
	}

	integrationChecks := doctorIntegrateIntentChecks(intent)
	integrationChecks = append(integrationChecks, doctorIntegrateSuggestionChecks(intent)...)
	report.families = append(report.families, familyReport("pop integrate", integrationChecks))

	// The Update check is a header line only; it never affects any family's
	// status (CONTEXT.md "Update notice"), so it is computed independently and
	// not folded into any familyReport.
	if d.updateCheck != nil {
		report.update = d.updateCheck()
	}
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

func doctorPaneChecks(d *doctorDeps) []doctorCheck {
	tmuxAvailable := d.tmuxAvailable()
	checks := []doctorCheck{
		doctorBoolCheck("tmux available", tmuxAvailable, "tmux executable was not found", "", ""),
	}
	if !tmuxAvailable {
		return checks
	}

	detail, err := d.paneSessionAddressable()
	if err != nil {
		checks = append(checks, doctorCheck{
			label:  "pane target session addressable",
			status: doctorStatusBlocked,
			detail: err.Error(),
		})
	} else {
		checks = append(checks, doctorCheck{
			label:  "pane target session addressable",
			status: doctorStatusOK,
			detail: detail,
		})
	}

	if _, err := d.loadMonitorState(); err != nil {
		checks = append(checks, doctorCheck{
			label:  "monitor fallback writes",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("monitor state is not readable for direct fallback writes: %v", err),
		})
	} else {
		checks = append(checks, doctorCheck{
			label:  "monitor fallback writes",
			status: doctorStatusOK,
			detail: "direct status writes can use monitor state",
		})
	}
	return checks
}

// doctorTaskChecks reports Tasks readiness for storage that lives in pop's
// data dir rather than the repository tree (ADR 0039): the Task storage data dir
// is writable, no legacy in-tree task sets remain in this worktree, no pre-rename
// storage layout is left un-migrated, and no Task storage has been orphaned by a
// vanished repository. Legacy-layout, orphan, and migration reporting are
// informational only — Doctor never deletes, moves, or modifies storage.
// doctorWayfinderChecks reports Wayfinder readiness with the same git and Task
// storage gates as Tasks: a runtime checkout must resolve and pop must be able
// to write beneath Task storage. Map count is informational only — zero maps is
// OK, matching Worktree readiness (absence is content, never Degraded). When
// maps exist, a second gate checks that at least one intended agent has the
// task-skills component (which ships the wayfinder planning skill).
func doctorWayfinderChecks(d *doctorDeps, intent *doctorAgentIntentReport) []doctorCheck {
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
	checks = append(checks, doctorTaskStorageWritableCheck(d))

	if d.scanWayfinderMaps == nil {
		return checks
	}
	count, err := d.scanWayfinderMaps()
	if err != nil {
		checks = append(checks, doctorCheck{
			label:  "maps listed",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("failed to list maps: %v", err),
		})
		return checks
	}
	checks = append(checks, doctorCheck{
		label:  "maps listed",
		status: doctorStatusOK,
		detail: fmt.Sprintf("%d map(s) listed", count),
	})

	if count > 0 {
		taskSkills, err := doctorIntendedAgentTaskSkills(d.integrate, intent)
		if err != nil {
			checks = append(checks, doctorCheck{
				label:  "wayfinder planning skill installed",
				status: doctorStatusDegraded,
				detail: fmt.Sprintf("failed to inspect intended agent task-skills: %v", err),
			})
		} else if check, ok := doctorIntendedAgentTaskSkillsCheck(taskSkills); ok {
			checks = append(checks, check)
		}
	}
	return checks
}

func doctorTaskChecks(d *doctorDeps) []doctorCheck {
	checks := []doctorCheck{doctorTaskStorageWritableCheck(d)}
	checks = append(checks, doctorTaskLegacyCheck(d))
	checks = append(checks, doctorTaskLegacyLayoutCheck(d))
	checks = append(checks, doctorTaskOrphanCheck(d))
	checks = append(checks, doctorTaskAgentAuthenticationChecks(d)...)
	return checks
}

func doctorTaskAgentAuthenticationChecks(d *doctorDeps) []doctorCheck {
	if d.agentCatalog == nil || d.probeAgentAuthentication == nil {
		return nil
	}
	rows := d.agentCatalog()
	checks := make([]doctorCheck, 0, len(rows))
	for _, row := range rows {
		checks = append(checks, doctorAgentAuthenticationCheck(row.Agent, d.probeAgentAuthentication(row.Agent)))
	}
	return checks
}

func doctorAgentAuthenticationCheck(preset string, probe tasks.AgentAuthenticationProbe) doctorCheck {
	label := preset + " authentication"
	switch probe.Status {
	case tasks.AgentAuthAuthenticated:
		return doctorCheck{label: label, status: doctorStatusOK, detail: probe.Detail}
	case tasks.AgentAuthUnauthenticated:
		return doctorCheck{label: label, status: doctorStatusPartial, detail: probe.Detail}
	case tasks.AgentAuthUnknown:
		return doctorCheck{label: label, status: doctorStatusNA, detail: probe.Detail}
	case tasks.AgentAuthCannotDetermine:
		return doctorCheck{label: label, status: doctorStatusNA, detail: probe.Detail}
	default:
		return doctorCheck{label: label, status: doctorStatusNA, detail: "authentication status unknown"}
	}
}

// doctorTaskLegacyLayoutCheck surfaces a pre-rename storage layout still
// present under the retired workloads/ data dir. It is informational (N/A): an
// un-migrated layout is migrated automatically on the next tasks command, so it
// never drives readiness below OK and Doctor never moves storage itself.
func doctorTaskLegacyLayoutCheck(d *doctorDeps) doctorCheck {
	if d.legacyLayoutStorage == nil {
		return doctorCheck{
			label:  "legacy storage layout",
			status: doctorStatusOK,
			detail: "no pre-rename task storage layout present",
		}
	}
	dirs, err := d.legacyLayoutStorage()
	switch {
	case err != nil:
		return doctorCheck{
			label:  "legacy storage layout",
			status: doctorStatusNA,
			detail: fmt.Sprintf("not assessed: %v", err),
		}
	case len(dirs) > 0:
		return doctorCheck{
			label:      "legacy storage layout",
			status:     doctorStatusNA,
			detail:     fmt.Sprintf("%d pre-rename storage director(ies) under workloads/ (auto-migrated on next tasks command): %s", len(dirs), strings.Join(dirs, ", ")),
			nextAction: "pop tasks status",
		}
	default:
		return doctorCheck{
			label:  "legacy storage layout",
			status: doctorStatusOK,
			detail: "no pre-rename task storage layout present",
		}
	}
}

func doctorTaskStorageWritableCheck(d *doctorDeps) doctorCheck {
	dir, err := d.taskStorageWritable()
	if err != nil {
		return doctorCheck{
			label:  "task storage writable",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("cannot create or write beneath task storage data dir: %v", err),
		}
	}
	return doctorCheck{
		label:  "task storage writable",
		status: doctorStatusOK,
		detail: fmt.Sprintf("pop can create and write beneath %s", dir),
	}
}

func doctorTaskLegacyCheck(d *doctorDeps) doctorCheck {
	legacy, err := d.legacyTaskSets()
	switch {
	case err != nil:
		return doctorCheck{
			label:  "legacy in-tree task sets",
			status: doctorStatusNA,
			detail: fmt.Sprintf("not assessed: %v", err),
		}
	case len(legacy) > 0:
		return doctorCheck{
			label:      "legacy in-tree task sets",
			status:     doctorStatusPartial,
			detail:     fmt.Sprintf("%d legacy task set(s) under thoughts/issues/ in this worktree: %s", len(legacy), strings.Join(legacy, ", ")),
			nextAction: "pop tasks migrate",
		}
	default:
		return doctorCheck{
			label:  "legacy in-tree task sets",
			status: doctorStatusOK,
			detail: "no legacy thoughts/issues task sets in this worktree",
		}
	}
}

func doctorTaskOrphanCheck(d *doctorDeps) doctorCheck {
	orphans, err := d.orphanedTaskStorage()
	switch {
	case err != nil:
		return doctorCheck{
			label:  "orphaned task storage",
			status: doctorStatusNA,
			detail: fmt.Sprintf("not assessed: %v", err),
		}
	case len(orphans) > 0:
		var lines []string
		for _, o := range orphans {
			lines = append(lines, fmt.Sprintf("%s (repository %s no longer exists)", o.StorageDir, o.RepositoryPath))
		}
		// N/A keeps orphan reporting informational: it never drives a healthy
		// repository's Task family below OK. Doctor only reports orphans —
		// it never deletes storage, and no GC exists.
		return doctorCheck{
			label:  "orphaned task storage",
			status: doctorStatusNA,
			detail: fmt.Sprintf("%d orphaned storage director(ies) (report-only, never deleted): %s", len(orphans), strings.Join(lines, "; ")),
		}
	default:
		return doctorCheck{
			label:  "orphaned task storage",
			status: doctorStatusOK,
			detail: "no orphaned task storage detected",
		}
	}
}

func defaultPaneSessionAddressable() (string, error) {
	if current := currentTmuxSession(); current != "" {
		return fmt.Sprintf("current tmux session %q is addressable", current), nil
	}
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return "", fmt.Errorf("not inside a tmux session and no target project config is available")
	}
	if cfg == nil {
		return "", fmt.Errorf("not inside a tmux session and no target project config is available")
	}
	paths, err := cfg.ExpandProjects()
	if err != nil {
		return "", fmt.Errorf("not inside a tmux session and configured projects could not be expanded: %v", err)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("not inside a tmux session and no target project is configured")
	}
	return "target project sessions can be addressed with --project", nil
}

func doctorMonitorChecks(d *doctorDeps, intent *doctorAgentIntentReport) []doctorCheck {
	checks := []doctorCheck{}

	state, err := d.loadMonitorState()
	if err != nil {
		checks = append(checks, doctorCheck{
			label:  "monitor state readable",
			status: doctorStatusBlocked,
			detail: fmt.Sprintf("failed to read monitor state: %v", err),
		})
	} else {
		checks = append(checks, doctorCheck{
			label:  "monitor state readable",
			status: doctorStatusOK,
			detail: fmt.Sprintf("%d tracked pane(s)", len(state.Panes)),
		})
	}

	switch {
	case d.daemonRunning():
		checks = append(checks, doctorCheck{
			label:  "monitor daemon usable",
			status: doctorStatusOK,
			detail: "daemon is running",
		})
	case d.monitorDaemonStartable():
		checks = append(checks, doctorCheck{
			label:  "monitor daemon usable",
			status: doctorStatusOK,
			detail: "daemon is stopped; normal pop startup can start it",
		})
	default:
		checks = append(checks, doctorCheck{
			label:      "monitor daemon usable",
			status:     doctorStatusBlocked,
			detail:     "daemon is stopped and normal pop startup cannot start it",
			nextAction: "pop project",
		})
	}

	if d.tmuxAvailable() {
		checks = append(checks, doctorCheck{
			label:  "automatic visit/status quality",
			status: doctorStatusOK,
			detail: "tmux is available for pane discovery, cleanup, and visit hooks",
		})
	} else {
		checks = append(checks, doctorCheck{
			label:  "automatic visit/status quality",
			status: doctorStatusDegraded,
			detail: "tmux unavailable; monitor state can be used but automatic pane tracking quality is reduced",
		})
	}

	wiring, err := doctorIntendedAgentStatusWiring(d.integrate, intent)
	if err != nil {
		checks = append(checks, doctorCheck{
			label:  "intended agent status wiring",
			status: doctorStatusDegraded,
			detail: fmt.Sprintf("failed to inspect intended agent wiring: %v", err),
		})
		return checks
	}
	if check, ok := doctorIntendedAgentStatusWiringCheck(wiring); ok {
		checks = append(checks, check)
	}

	return checks
}

func doctorIntendedAgentStatusWiringCheck(wiring []doctorAgentStatusWiring) (doctorCheck, bool) {
	if len(wiring) == 0 {
		return doctorCheck{
			label:  "intended agent status wiring",
			status: doctorStatusOK,
			detail: "no intended agents detected",
		}, true
	}

	var ok, needsAttention []string
	for _, w := range wiring {
		switch w.State.Kind {
		case integrate.StateInstalledCurrent:
			ok = append(ok, w.Agent)
		case integrate.StateConflict:
			needsAttention = append(needsAttention, w.Agent+" (conflicting)")
		case integrate.StateStale:
			needsAttention = append(needsAttention, w.Agent+" (stale)")
		default:
			needsAttention = append(needsAttention, w.Agent+" (missing)")
		}
	}

	switch {
	case len(ok) > 0 && len(needsAttention) > 0:
		return doctorCheck{
			label:  "intended agent status wiring",
			status: doctorStatusPartial,
			detail: fmt.Sprintf("wired: %s; missing, stale, or conflicting: %s", strings.Join(ok, ", "), strings.Join(needsAttention, ", ")),
		}, true
	case len(ok) == 0 && len(needsAttention) > 0:
		return doctorCheck{
			label:  "intended agent status wiring",
			status: doctorStatusDegraded,
			detail: fmt.Sprintf("no intended agent status wiring is currently usable; missing, stale, or conflicting: %s", strings.Join(needsAttention, ", ")),
		}, true
	default:
		return doctorCheck{
			label:  "intended agent status wiring",
			status: doctorStatusOK,
			detail: fmt.Sprintf("wired for intended agent(s): %s", strings.Join(ok, ", ")),
		}, true
	}
}

func doctorIntendedAgentTaskSkillsCheck(wiring []doctorAgentStatusWiring) (doctorCheck, bool) {
	if len(wiring) == 0 {
		return doctorCheck{
			label:  "wayfinder planning skill installed",
			status: doctorStatusOK,
			detail: "no intended agents detected",
		}, true
	}

	var installed, needsAttention []string
	var firstActionAgent string
	for _, w := range wiring {
		if w.State.Kind == integrate.StateNotSupported {
			continue
		}
		if firstActionAgent == "" {
			firstActionAgent = w.Agent
		}
		switch w.State.Kind {
		case integrate.StateInstalledCurrent, integrate.StateStale:
			installed = append(installed, w.Agent)
		case integrate.StateConflict:
			needsAttention = append(needsAttention, w.Agent+" (conflicting)")
		default:
			needsAttention = append(needsAttention, w.Agent+" (missing)")
		}
	}

	if len(installed) > 0 {
		return doctorCheck{
			label:  "wayfinder planning skill installed",
			status: doctorStatusOK,
			detail: fmt.Sprintf("installed for intended agent(s): %s", strings.Join(installed, ", ")),
		}, true
	}

	detail := "maps exist but no intended agent has task planning skills installed"
	if len(needsAttention) > 0 {
		detail = fmt.Sprintf("maps exist but task planning skills are not installed for intended agent(s): %s", strings.Join(needsAttention, ", "))
	}
	check := doctorCheck{
		label:  "wayfinder planning skill installed",
		status: doctorStatusDegraded,
		detail: detail,
	}
	if firstActionAgent != "" {
		check.nextAction = integrateInvocation(firstActionAgent, integrate.ComponentTaskSkills)
	}
	return check, true
}

func doctorIntendedAgentTaskSkills(d *integrate.Deps, intent *doctorAgentIntentReport) ([]doctorAgentStatusWiring, error) {
	home, err := d.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return integrate.IntendedAgentComponentStates(d, home, intent, integrate.ComponentTaskSkills)
}

func doctorIntendedAgentStatusWiring(d *integrate.Deps, intent *doctorAgentIntentReport) ([]doctorAgentStatusWiring, error) {
	home, err := d.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return integrate.IntendedAgentComponentStates(d, home, intent, integrate.ComponentStatusWiring)
}

func doctorIntegrateIntentChecks(intent *doctorAgentIntentReport) []doctorCheck {
	if intent == nil || len(intent.Intended) == 0 {
		return []doctorCheck{{
			label:  "intended agent setup repair path",
			status: doctorStatusOK,
			detail: "no intended agents detected; available agents are suggestions only",
		}}
	}
	var agents []string
	for _, intended := range intent.Intended {
		source := strings.Join(intended.Sources, ", ")
		if source == "" {
			source = "detected intent"
		}
		agents = append(agents, fmt.Sprintf("%s (%s)", intended.Agent, source))
	}
	return []doctorCheck{{
		label:  "intended agent setup repair path",
		status: doctorStatusOK,
		detail: fmt.Sprintf("can inspect and repair intended agent setup through pop integrate for: %s", strings.Join(agents, "; ")),
	}}
}

func doctorIntegrateSuggestionChecks(intent *doctorAgentIntentReport) []doctorCheck {
	if intent == nil {
		return nil
	}
	var checks []doctorCheck
	for _, suggestion := range intent.Suggestions {
		checks = append(checks, doctorCheck{
			label:      fmt.Sprintf("%s available agent suggestion", suggestion.Agent),
			status:     doctorStatusNA,
			detail:     suggestion.Reason,
			nextAction: integrateInvocation(suggestion.Agent, integrate.ComponentStatusWiring),
		})
	}
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

// doctorComponentFlag maps a component to the `pop integrate` flag that selects
// it non-interactively. The status wiring has no flag — it is the core
// component a bare `pop integrate <agent>` installs.
var doctorComponentFlag = map[integrate.ComponentID]string{
	integrate.ComponentStatusWiring: "",
	integrate.ComponentPaneSkill:    "--no-pane-skills",
	integrate.ComponentTaskSkills:   "--task-skills",
}

// integrateInvocation builds the copy-paste integrate command that installs the
// given component for the agent.
func integrateInvocation(agent string, id integrate.ComponentID) string {
	if flag := doctorComponentFlag[id]; flag != "" {
		return fmt.Sprintf("pop integrate %s %s", agent, flag)
	}
	return fmt.Sprintf("pop integrate %s", agent)
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
	fmt.Fprintln(w, doctorUpdateHeader(color, report.update))
	fmt.Fprintln(w)
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

// doctorUpdateHeader renders the version-freshness header line above the
// family rows. It reflects the Update check per CONTEXT.md "Releases": an
// outdated binary notes the available update, a current one is marked
// "(latest)", a Dev build shows its version with "(dev build)" and no
// comparison, and a failed check appends a dim note — never a failure.
func doctorUpdateHeader(color bool, res release.Result) string {
	base := "pop " + res.Current
	switch res.State {
	case release.StateOutdated:
		return fmt.Sprintf("%s (latest: %s — update available)", base, res.Latest)
	case release.StateCurrent:
		return base + " (latest)"
	case release.StateDev:
		return base + doctorStyled(color, doctorANSIDim, " (dev build)")
	case release.StateFailed:
		return base + doctorStyled(color, doctorANSIDim, " (update check failed)")
	default:
		return base
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
