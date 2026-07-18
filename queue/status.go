package queue

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// PickedUpSet is a live in-flight drain derived from a runtime lock.
type PickedUpSet struct {
	Project            string
	RepoLabel          string
	SetID              string
	RuntimePath        string
	PID                int
	StartedAt          time.Time
	WorktreeReady      bool
	ProjectConfigError string
}

// IdleProject is a configured project with no live runtime lock.
type IdleProject struct {
	Project   string
	RepoLabel string
	Waiting   string
	ReadySet  string
	// AwaitingApprovalSetID is the first Task-set in AWAITING-APPROVAL state
	// (awaiting human sign-off). Non-empty only when Reason is "awaiting approval".
	AwaitingApprovalSetID string
	Reason                string
	// BlockedSetID names the set whose abnormal backoff or parking produced
	// Reason; WaitUntil is when a backed-off set next becomes spawnable (zero for
	// a parked set). Both are derived from Drain history (ADR-0055).
	BlockedSetID string
	WaitUntil    time.Time
	// Deferral is the readiness selector's typed "Ready but not spawning" verdict
	// (ADR-0106) carried through from the Decision. The run view classifies and
	// renders blocked rows from it rather than re-matching Reason strings.
	Deferral           SpawnDeferral
	WorktreeReady      bool
	ProjectConfigError string
	// RuntimePath is the bound checkout for the set represented by this idle
	// project, when one exists. It is used to surface an adopted-worktree suffix
	// only when the checkout basename differs from the set identifier.
	RuntimePath string
}

// SkippedRepo is a repository the Queue refused to schedule because it could
// resolve no representative checkout (a bare repo with no Trunk worktree and no
// per-set Worktree binding). It is reported, never scheduled (ADR-0035).
type SkippedRepo struct {
	Project   string
	RepoLabel string
	Reason    string
}

// StatusSnapshot is the pure data model for `pop queue status`.
type StatusSnapshot struct {
	PickedUp             []PickedUpSet
	Idle                 []IdleProject
	Skipped              []SkippedRepo
	ActiveAgentCooldowns map[string]time.Time
	RecoveryWaiters      map[string]tasks.RecoveryWaiter
	Tasks                *tasks.Deps
	// CrashRetryDelays is the resolved abnormal-backoff escalation schedule (its
	// length is the park threshold). The run view derives each set's parked /
	// backed-off status from Drain history against it (ADR-0055).
	CrashRetryDelays []time.Duration
	// IncludeDone is the Done-inclusion view flag (ADR-0121) carried from the
	// Deps so the run view applies the same uniform DONE hide the dashboard row
	// layer does: a DONE set's managed Worktree binding is omitted from the
	// Active-worktrees view by default, revealed by `--include-done`.
	IncludeDone bool
}

// BuildStatus derives queue status from on-disk lock/state truth.
func BuildStatus(d *Deps, cfg *config.Config) (StatusSnapshot, error) {
	decisions, err := Scan(d, cfg)
	if err != nil {
		return StatusSnapshot{}, err
	}
	snap, err := statusFromDecisions(d, decisions)
	if err != nil {
		return StatusSnapshot{}, err
	}
	cooldowns, err := tasks.ActiveAgentCooldownsWith(d.Tasks, d.now().UTC())
	if err != nil {
		return StatusSnapshot{}, err
	}
	snap.ActiveAgentCooldowns = cooldowns
	snap.RecoveryWaiters = loadRecoveryWaiters(d)
	snap.Tasks = d.Tasks
	snap.IncludeDone = d.IncludeDone
	if qcfg, qerr := resolvedQueueConfig(cfg); qerr == nil {
		snap.CrashRetryDelays = qcfg.CrashRetryDelays
	}
	return snap, nil
}

func statusFromDecisions(d *Deps, decisions []Decision) (StatusSnapshot, error) {
	var snap StatusSnapshot
	for _, dec := range decisions {
		repoLabel := repoLabelFromScan(dec.scan)
		if dec.Busy {
			lock := dec.lockStatus
			picked := PickedUpSet{Project: dec.Project, RepoLabel: repoLabel, WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError}
			if lock != nil {
				picked.RuntimePath = lock.RuntimePath
				if lock.Metadata != nil {
					picked.SetID = lock.Metadata.SetID
					picked.PID = lock.Metadata.PID
					picked.StartedAt = lock.Metadata.StartedAt
					picked.RuntimePath = lock.Metadata.RuntimePath
				}
			}
			snap.PickedUp = append(snap.PickedUp, picked)
			continue
		}
		if dec.Err != nil {
			snap.Idle = append(snap.Idle, IdleProject{Project: dec.Project, RepoLabel: repoLabel, Waiting: "error", Reason: dec.Err.Error(), WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError})
			continue
		}
		if dec.TaskSetID == "" && dec.Reason == repoScanReason {
			snap.Skipped = append(snap.Skipped, SkippedRepo{Project: dec.Project, RepoLabel: repoLabel, Reason: dec.Reason})
			continue
		}
		idle := IdleProject{Project: dec.Project, RepoLabel: repoLabel, Reason: dec.Reason, WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError, AwaitingApprovalSetID: dec.AwaitingApprovalSetID, BlockedSetID: dec.BlockedSetID, WaitUntil: dec.WaitUntil, Deferral: dec.Deferral}
		if dec.TaskSetID != "" {
			idle.Waiting = "ready"
			idle.ReadySet = dec.TaskSetID
		} else {
			idle.Waiting = "idle"
		}
		snap.Idle = append(snap.Idle, idle)
	}
	sort.SliceStable(snap.PickedUp, func(i, j int) bool { return snap.PickedUp[i].Project < snap.PickedUp[j].Project })
	sort.SliceStable(snap.Idle, func(i, j int) bool { return snap.Idle[i].Project < snap.Idle[j].Project })
	sort.SliceStable(snap.Skipped, func(i, j int) bool { return snap.Skipped[i].Project < snap.Skipped[j].Project })
	return snap, nil
}

// RenderStatus prints the static Queue status surface (ADR-0121): a one-line
// Summary headline, then the Queue dashboard's task-set table (the same rows,
// columns, row filter, and sort — status and the dashboard key on one row
// builder and one comparator), then a trailing Scan errors section when there
// are scan errors. Every former per-bucket inventory section is retired; the
// STATUS column, live-drain indicator, and status suffixes now encode the
// picked-up / parked / awaiting / config-error state those sections carried.
//
// The Summary headline and Scan errors are derived from the RunView (the
// existing aggregate) so the summary stays a scheduling roll-up; the table rows
// are the dashboard rows the command builds via queue.BuildDashboard. Output is
// plain text (no ANSI, non-interactive) so it stays greppable/pipeable and
// serves as the Queue run baseline.
func RenderStatus(out io.Writer, snap StatusSnapshot, rows []DashboardRow) {
	view := BuildRunView(snap, time.Now())
	RenderRunSummary(out, view)
	renderStatusTable(out, rows)
	renderStatusScanErrors(out, view.ScanErrors)
}

// renderStatusTable renders the dashboard's task-set table as static plain
// text: the PROJECT / TASK SET / STATUS / WORKTREE columns plus the trailing
// live-drain indicator. It reuses the dashboard's headers and column-width math
// (dashboardColumnWidths measures with lipgloss.Width, which strips ANSI, so the
// widths match the dashboard's) but renders fully plain cells via
// statusRowValues so no styling leaks into the pipeable surface. Widths are the
// natural widths (no terminal-fit shrink) so nothing is truncated, and each line
// is right-trimmed so the empty trailing indicator leaves no dangling
// whitespace.
func renderStatusTable(out io.Writer, rows []DashboardRow) {
	fmt.Fprintln(out)
	if len(rows) == 0 {
		fmt.Fprintln(out, "No queue-actionable task sets.")
		return
	}
	widths := dashboardColumnWidths(rows)
	fmt.Fprintln(out, strings.TrimRight(dashboardTableLine(dashboardTableHeaders(), widths), " "))
	fmt.Fprintln(out, strings.TrimRight(dashboardTableSeparator(widths), " "))
	for _, row := range rows {
		fmt.Fprintln(out, strings.TrimRight(dashboardTableLine(statusRowValues(row), widths), " "))
	}
}

// statusRowValues returns a dashboard row's fully plain column cells for the
// static status table. Unlike dashboardRowValues / dashboardRowNaturalValues —
// which style the WORKTREE destination badge for the TUI — every cell here is
// plain text: the composed STATUS cell (already un-styled) and the plain
// destination label keep the status surface ANSI-free and greppable.
func statusRowValues(row DashboardRow) []string {
	return []string{
		row.Project,
		row.SetID,
		dashboardStatusCell(row),
		statusDestLabel(row.destKind, row.Worktree),
		dashboardLiveIndicator(row, false),
	}
}

// statusDestLabel returns the plain WORKTREE cell for the status table, the
// un-styled counterpart of renderDashboardDest.
func statusDestLabel(kind dashboardDestKind, label string) string {
	switch kind {
	case dashboardDestManagedDirective:
		return dashboardDestLabelManagedWt
	case dashboardDestNeedsBind:
		return dashboardDestLabelNeedsBind
	case dashboardDestDoneManagedBound:
		return "[managed wt " + label + "]"
	default:
		return label
	}
}

// renderStatusScanErrors prints the trailing Scan errors section, only when
// there are scan errors, projects sorted for stable output.
func renderStatusScanErrors(out io.Writer, scanErrors map[string]string) {
	if len(scanErrors) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Scan errors:")
	projects := make([]string, 0, len(scanErrors))
	for project := range scanErrors {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	for _, project := range projects {
		fmt.Fprintf(out, "  %s: %s\n", project, scanErrors[project])
	}
}

// repoLabelFromScan returns the repository-identity basename for a scan when the
// common directory was resolved, falling back to the picker project name.
func repoLabelFromScan(scan projectScan) string {
	if scan.RepoCommonDir != "" {
		return tasks.RepoBasename(scan.RepoCommonDir)
	}
	return scan.Name
}

func statusProjectLabel(project string, worktreeReady bool, configError string) string {
	label := project
	if worktreeReady {
		label += " [worktree-ready]"
	}
	if configError != "" {
		label += " [.pop.toml error: " + configError + "]"
	}
	return label
}
