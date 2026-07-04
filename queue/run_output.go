package queue

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// AwaitingApprovalItem is a Task-set whose AFK work is finished but a terminal HITL
// approval gate remains before it can be marked Done.
type AwaitingApprovalItem struct {
	Project   string
	RepoLabel string
	SetID     string
}

// RunView is the scheduling-relevant snapshot used for queue run baseline and deltas.
type RunView struct {
	Running          []PickedUpSet
	Queued           []IdleProject
	Blocked          []BlockedItem
	AwaitingApproval []AwaitingApprovalItem
	WorktreeBindings []WorktreeBindingView
	Skipped          []SkippedRepo
	IdleCount        int
	ScanErrors       map[string]string
}

// WorktreeBindingView is one provisioned checkout tracked in queue daemon state.
type WorktreeBindingView struct {
	Project     string
	RepoLabel   string
	SetID       string
	Branch      string
	RuntimePath string
	Phase       string
	PID         int
}

// BlockedItem is one blocked scheduling bucket: parked set, backoff, or agent cooldown.
type BlockedItem struct {
	Project   string
	RepoLabel string
	SetID     string
	Kind      string
	Until     time.Time
	Reason    string
	Agent     string
}

type runOutputState struct {
	firstTick bool
	prev      *RunView
	lastScan  string
}

func newRunOutputState() *runOutputState {
	return &runOutputState{firstTick: true}
}

// BuildRunView derives the queue run view from a status snapshot.
func BuildRunView(snap StatusSnapshot, now time.Time) RunView {
	view := RunView{
		Running:    append([]PickedUpSet(nil), snap.PickedUp...),
		Skipped:    append([]SkippedRepo(nil), snap.Skipped...),
		ScanErrors: map[string]string{},
	}
	blockedProjects := map[string]bool{}

	for _, idle := range snap.Idle {
		repoLabel := idle.RepoLabel
		if repoLabel == "" {
			repoLabel = idle.Project
		}
		switch {
		case idle.Waiting == "error":
			appendScanError(view.ScanErrors, repoLabel, idle.Reason)
		case idle.ProjectConfigError != "":
			appendScanError(view.ScanErrors, repoLabel, idle.ProjectConfigError)
		case idle.ReadySet != "":
			view.Queued = append(view.Queued, idle)
		case isBlockedIdleReason(idle.Reason):
			view.Blocked = append(view.Blocked, blockedItemFromIdle(idle))
			blockedProjects[repoLabel] = true
		case idle.AwaitingApprovalSetID != "":
			view.AwaitingApproval = append(view.AwaitingApproval, AwaitingApprovalItem{
				Project:   idle.Project,
				RepoLabel: repoLabel,
				SetID:     idle.AwaitingApprovalSetID,
			})
		default:
			view.IdleCount++
		}
	}

	for _, picked := range view.Running {
		if picked.ProjectConfigError != "" {
			repoLabel := picked.RepoLabel
			if repoLabel == "" {
				repoLabel = picked.Project
			}
			appendScanError(view.ScanErrors, repoLabel, picked.ProjectConfigError)
		}
	}

	view.Blocked = append(view.Blocked, blockedItemsFromState(snap.Tasks, snap.DaemonState, snap.CrashRetryDelays, now, blockedProjects)...)
	if snap.DaemonState != nil {
		view.WorktreeBindings = buildWorktreeBindingViews(snap.Tasks, snap.DaemonState, view)
	}
	view.Blocked = append(view.Blocked, blockedItemsFromAgentCooldowns(snap.ActiveAgentCooldowns, now)...)

	sort.SliceStable(view.Queued, func(i, j int) bool { return view.Queued[i].Project < view.Queued[j].Project })
	sort.SliceStable(view.Blocked, func(i, j int) bool {
		if view.Blocked[i].Project != view.Blocked[j].Project {
			return view.Blocked[i].Project < view.Blocked[j].Project
		}
		if view.Blocked[i].SetID != view.Blocked[j].SetID {
			return view.Blocked[i].SetID < view.Blocked[j].SetID
		}
		return view.Blocked[i].Kind < view.Blocked[j].Kind
	})
	sort.SliceStable(view.AwaitingApproval, func(i, j int) bool {
		if view.AwaitingApproval[i].Project != view.AwaitingApproval[j].Project {
			return view.AwaitingApproval[i].Project < view.AwaitingApproval[j].Project
		}
		return view.AwaitingApproval[i].SetID < view.AwaitingApproval[j].SetID
	})
	return view
}

func appendScanError(scanErrors map[string]string, project, msg string) {
	if msg == "" {
		return
	}
	if existing, ok := scanErrors[project]; ok {
		scanErrors[project] = existing + "; " + msg
		return
	}
	scanErrors[project] = msg
}

func isBlockedIdleReason(reason string) bool {
	switch reason {
	case "set parked after repeated abnormal drain exits",
		"set backed off after abnormal drain exit",
		"set backed off for pinned agent cooldown",
		"all agents cooling":
		return true
	default:
		return false
	}
}

func blockedItemFromIdle(idle IdleProject) BlockedItem {
	repoLabel := idle.RepoLabel
	if repoLabel == "" {
		repoLabel = idle.Project
	}
	return BlockedItem{
		Project:   idle.Project,
		RepoLabel: repoLabel,
		SetID:     idle.BlockedSetID,
		Reason:    idle.Reason,
		Kind:      blockedKindFromReason(idle.Reason),
		Until:     idle.WaitUntil,
	}
}

// blockedItemsFromState surfaces parked / backed-off sets whose project is not
// already shown as idle (e.g. a repo busy running another set). Abnormal-driven
// parking and backoff are derived from each bound set's Drain history (ADR-0055);
// only the pinned-agent quota cooldown still reads a persisted timer.
func blockedItemsFromState(td *tasks.Deps, state *DaemonState, delays []time.Duration, now time.Time, seenProjects map[string]bool) []BlockedItem {
	bindings, _ := binding.AllBindings(td)
	out := blockedItemsFromDrainHistory(td, bindings, delays, now, seenProjects)
	if state != nil {
		for key, until := range state.SetBackoffs {
			if until.IsZero() || !until.After(now) {
				continue
			}
			setID := binding.SetIDFromKey(key)
			repoLabel := repoLabelFromScopedKey(key)
			if repoLabel == "" {
				repoLabel = projectForScopedKey(td, key)
			}
			if seenProjects[repoLabel] {
				continue
			}
			project := ""
			if b, ok := bindings[key]; ok {
				project = b.Project
			}
			if project == "" {
				project = repoLabel
			}
			out = append(out, BlockedItem{
				Project:   project,
				RepoLabel: repoLabel,
				SetID:     setID,
				Kind:      "quota_backoff",
				Until:     until,
				Reason:    "set backed off for pinned agent cooldown",
			})
		}
	}
	return out
}

// blockedItemsFromDrainHistory derives parked and crash-backed-off sets from the
// Drain store, one per provisioned Worktree binding (a parked set ran in a
// checkout, so it has a binding). A nil tasks dep or store-read error yields
// nothing rather than blocking the view.
func blockedItemsFromDrainHistory(td *tasks.Deps, bindings map[string]WorktreeBinding, delays []time.Duration, now time.Time, seenProjects map[string]bool) []BlockedItem {
	if td == nil {
		return nil
	}
	if bindings == nil {
		var err error
		bindings, err = binding.AllBindings(td)
		if err != nil {
			return nil
		}
	}
	var out []BlockedItem
	for key, b := range bindings {
		repoLabel := repoLabelFromScopedKey(key)
		if repoLabel == "" {
			repoLabel = b.Project
		}
		if seenProjects[repoLabel] || strings.TrimSpace(b.RuntimePath) == "" {
			continue
		}
		setID := binding.SetIDFromKey(key)
		id, err := tasks.ResolveRepositoryIdentity(td, b.RuntimePath)
		if err != nil {
			continue
		}
		info, err := tasks.ReadSetBackoff(td, id.CommonDir, setID)
		if err != nil {
			continue
		}
		parked, until := setBackoffStatus(info, delays, now)
		switch {
		case parked:
			out = append(out, BlockedItem{
				Project:   b.Project,
				RepoLabel: id.Basename,
				SetID:     setID,
				Kind:      "parked",
				Until:     info.LastAbnormalAt,
				Reason:    "repeated abnormal drain exits",
			})
		case !until.IsZero():
			out = append(out, BlockedItem{
				Project:   b.Project,
				RepoLabel: id.Basename,
				SetID:     setID,
				Kind:      "crash_backoff",
				Until:     until,
				Reason:    "set backed off after abnormal drain exit",
			})
		}
	}
	return out
}

func blockedItemsFromAgentCooldowns(cooldowns map[string]time.Time, now time.Time) []BlockedItem {
	var out []BlockedItem
	for agent, until := range cooldowns {
		if until.IsZero() || !until.After(now) {
			continue
		}
		out = append(out, BlockedItem{
			Kind:   "agent_cooldown",
			Agent:  agent,
			Until:  until,
			Reason: "agent quota cooldown",
		})
	}
	return out
}

func blockedKindFromReason(reason string) string {
	switch reason {
	case "set parked after repeated abnormal drain exits":
		return "parked"
	case "set backed off after abnormal drain exit":
		return "crash_backoff"
	case "set backed off for pinned agent cooldown":
		return "quota_backoff"
	case "all agents cooling":
		return "agent_cooldown"
	default:
		return "blocked"
	}
}

func projectForScopedKey(td *tasks.Deps, key string) string {
	setID := binding.SetIDFromKey(key)
	bindings, _ := binding.AllBindings(td)
	for k, b := range bindings {
		if binding.SetIDFromKey(k) == setID && b.Project != "" {
			return b.Project
		}
	}
	return ""
}

// repoLabelFromScopedKey extracts the repository-identity basename from a
// repoKey\x00setID state key. The repo key is basename-shorthash, so the label
// is the basename portion.
func repoLabelFromScopedKey(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) == 0 {
		return ""
	}
	return repoLabelFromRepoKey(parts[0])
}

// repoLabelFromRepoKey returns the basename portion of a repo-identity key
// (basename-shorthash). The short hash is always tasks.ShortHashLen hex
// characters; if the key does not match that shape, the whole key is returned.
func repoLabelFromRepoKey(repoKey string) string {
	if len(repoKey) <= tasks.ShortHashLen+1 {
		return repoKey
	}
	sepIdx := len(repoKey) - tasks.ShortHashLen - 1
	if repoKey[sepIdx] != '-' {
		return repoKey
	}
	for i := sepIdx + 1; i < len(repoKey); i++ {
		c := repoKey[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return repoKey
		}
	}
	return repoKey[:sepIdx]
}

func formatQueueWorkSummary(view RunView) string {
	var parts []string
	if n := len(view.Running); n > 0 {
		parts = append(parts, fmt.Sprintf("%d running", n))
	}
	if n := len(view.Queued); n > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", n))
	}
	if n := len(view.Blocked); n > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", n))
	}
	if n := len(view.AwaitingApproval); n > 0 {
		parts = append(parts, fmt.Sprintf("%d awaiting approval", n))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// RenderRunSummary prints the aggregate queue headline.
func RenderRunSummary(out io.Writer, view RunView) {
	fmt.Fprintln(out, "Summary:")
	fmt.Fprintf(out, "  Queue: %s\n", formatQueueWorkSummary(view))
}

// RenderRunBaseline prints the one-time queue run inventory.
func RenderRunBaseline(out io.Writer, view RunView) {
	RenderRunSummary(out, view)

	fmt.Fprintln(out, "Picked-up sets:")
	if len(view.Running) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, p := range view.Running {
			fmt.Fprintf(out, "  %s\n", formatRunningLine(p))
		}
	}

	fmt.Fprintln(out, "Active worktrees:")
	if len(view.WorktreeBindings) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, binding := range view.WorktreeBindings {
			fmt.Fprintf(out, "  %s\n", formatWorktreeBindingLine(binding))
		}
	}

	fmt.Fprintln(out, "Queued ready sets:")
	if len(view.Queued) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, q := range view.Queued {
			projectLabel := statusProjectLabel(repoLabelOrProject(q.RepoLabel, q.Project), q.WorktreeReady, q.ProjectConfigError)
			fmt.Fprintf(out, "  %s: waiting ready set %s\n", projectLabel, q.ReadySet)
		}
	}

	fmt.Fprintln(out, "Blocked:")
	if len(view.Blocked) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, b := range view.Blocked {
			fmt.Fprintf(out, "  %s\n", formatBlockedLine(b))
		}
	}

	fmt.Fprintln(out, "Awaiting approval:")
	if len(view.AwaitingApproval) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, u := range view.AwaitingApproval {
			project := repoLabelOrProject(u.RepoLabel, u.Project)
			if project == "" {
				project = "(unknown project)"
			}
			setID := u.SetID
			if setID == "" {
				setID = "(unknown set)"
			}
			fmt.Fprintf(out, "  %s: %s — awaiting your sign-off\n", project, setID)
		}
	}

	if len(view.Skipped) > 0 {
		fmt.Fprintln(out, "Skipped repositories:")
		for _, s := range view.Skipped {
			project := repoLabelOrProject(s.RepoLabel, s.Project)
			if project == "" {
				project = "(unknown project)"
			}
			fmt.Fprintf(out, "  %s: %s\n", project, s.Reason)
		}
	}

	if len(view.ScanErrors) > 0 {
		fmt.Fprintln(out, "Scan errors:")
		projects := make([]string, 0, len(view.ScanErrors))
		for project := range view.ScanErrors {
			projects = append(projects, project)
		}
		sort.Strings(projects)
		for _, project := range projects {
			fmt.Fprintf(out, "  %s: %s\n", project, view.ScanErrors[project])
		}
	}

	switch view.IdleCount {
	case 0:
	case 1:
		fmt.Fprintln(out, "1 other project: no ready work")
	default:
		fmt.Fprintf(out, "%d other projects: no ready work\n", view.IdleCount)
	}
}

func formatRunningLine(p PickedUpSet) string {
	projectLabel := statusProjectLabel(repoLabelOrProject(p.RepoLabel, p.Project), p.WorktreeReady, p.ProjectConfigError)
	setID := p.SetID
	if setID == "" {
		setID = "(unknown set)"
	}
	started := ""
	if !p.StartedAt.IsZero() {
		started = " since " + p.StartedAt.UTC().Format(time.RFC3339)
	}
	pid := ""
	if p.PID > 0 {
		pid = fmt.Sprintf(" pid=%d", p.PID)
	}
	return fmt.Sprintf("%s: %s%s%s", projectLabel, setID, pid, started)
}

func buildWorktreeBindingViews(d *tasks.Deps, state *DaemonState, view RunView) []WorktreeBindingView {
	bindings, err := binding.AllBindings(d)
	if err != nil || len(bindings) == 0 {
		return nil
	}
	runningBySet := make(map[string]PickedUpSet, len(view.Running))
	for _, p := range view.Running {
		if p.SetID != "" {
			runningBySet[p.SetID] = p
		}
	}

	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]WorktreeBindingView, 0, len(keys))
	for _, key := range keys {
		b := bindings[key]
		setID := binding.SetIDFromKey(key)
		project := b.Project
		if project == "" {
			project = projectForScopedKey(d, key)
		}
		repoLabel := repoLabelFromScopedKey(key)
		if repoLabel == "" {
			repoLabel = project
		}
		item := WorktreeBindingView{
			Project:     project,
			RepoLabel:   repoLabel,
			SetID:       setID,
			Branch:      b.Branch,
			RuntimePath: b.RuntimePath,
		}
		if picked, ok := runningBySet[setID]; ok {
			item.Phase = "draining"
			item.PID = picked.PID
		} else {
			item.Phase = "bound"
		}
		items = append(items, item)
	}
	return items
}

func formatWorktreeBindingLine(binding WorktreeBindingView) string {
	project := repoLabelOrProject(binding.RepoLabel, binding.Project)
	if project == "" {
		project = "(unknown project)"
	}
	setID := binding.SetID
	if setID == "" {
		setID = "(unknown set)"
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s: %s", project, setID))
	if binding.Branch != "" {
		parts = append(parts, "branch="+binding.Branch)
	}
	if binding.RuntimePath != "" {
		parts = append(parts, "at "+binding.RuntimePath)
	}
	line := strings.Join(parts, " ")
	line += " — " + binding.Phase
	if binding.PID > 0 {
		line += fmt.Sprintf(" pid=%d", binding.PID)
	}
	return line
}

func formatBlockedLine(b BlockedItem) string {
	switch b.Kind {
	case "agent_cooldown":
		until := ""
		if !b.Until.IsZero() {
			until = " until " + b.Until.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("agent %s cooling%s", b.Agent, until)
	case "parked":
		setID := b.SetID
		if setID == "" {
			setID = "(unknown set)"
		}
		project := repoLabelOrProject(b.RepoLabel, b.Project)
		if project == "" {
			project = "(unknown project)"
		}
		return fmt.Sprintf("%s: %s parked (%s)", project, setID, b.Reason)
	default:
		setID := b.SetID
		if setID == "" {
			setID = "(unknown set)"
		}
		project := repoLabelOrProject(b.RepoLabel, b.Project)
		if project == "" {
			project = "(unknown project)"
		}
		until := ""
		if !b.Until.IsZero() {
			until = " until " + b.Until.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("%s: %s %s%s", project, setID, b.Reason, until)
	}
}

// seedSpawnedRunning augments view.Running with any spawned set not already
// present, keyed by RepoLabel+SetID. It bridges the gap between issuing a spawn
// and the drain acquiring its runtime lock: the post-spawn scan cannot yet see
// the set as Running, so without this the next tick's diff would re-report the
// spawn a second time.
func seedSpawnedRunning(view RunView, spawned []PickedUpSet) RunView {
	if len(spawned) == 0 {
		return view
	}
	present := runningIndex(view.Running)
	for _, s := range spawned {
		if _, ok := present[repoLabelOrProject(s.RepoLabel, s.Project)+"\x00"+s.SetID]; ok {
			continue
		}
		view.Running = append(view.Running, s)
	}
	return view
}

// DiffRunView returns delta lines for scheduling-relevant changes between views.
func DiffRunView(prev *RunView, curr RunView) []string {
	if prev == nil {
		return nil
	}
	var lines []string

	prevRunning := runningIndex(prev.Running)
	currRunning := runningIndex(curr.Running)
	for key, p := range currRunning {
		if _, ok := prevRunning[key]; ok {
			continue
		}
		setID := p.SetID
		if setID == "" {
			setID = "(unknown set)"
		}
		label := statusProjectLabel(repoLabelOrProject(p.RepoLabel, p.Project), p.WorktreeReady, p.ProjectConfigError)
		lines = append(lines, fmt.Sprintf("queue: %s: spawned drain for %s", label, setID))
	}

	prevQueued := queuedIndex(prev.Queued)
	currQueued := queuedIndex(curr.Queued)
	for key, q := range currQueued {
		if _, ok := prevQueued[key]; ok {
			continue
		}
		label := statusProjectLabel(repoLabelOrProject(q.RepoLabel, q.Project), q.WorktreeReady, q.ProjectConfigError)
		lines = append(lines, fmt.Sprintf("queue: %s: ready set %s", label, q.ReadySet))
	}

	prevBlocked := blockedIndex(prev.Blocked)
	currBlocked := blockedIndex(curr.Blocked)
	for key, b := range currBlocked {
		if _, ok := prevBlocked[key]; ok {
			continue
		}
		lines = append(lines, formatBlockedDelta(b, false))
	}
	for key, b := range prevBlocked {
		if _, ok := currBlocked[key]; ok {
			continue
		}
		lines = append(lines, formatBlockedDelta(b, true))
	}

	prevSkipped := skippedIndex(prev.Skipped)
	currSkipped := skippedIndex(curr.Skipped)
	for repoLabel, s := range currSkipped {
		if _, ok := prevSkipped[repoLabel]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: %s", repoLabel, s.Reason))
	}

	for project, msg := range curr.ScanErrors {
		if prev.ScanErrors[project] == msg {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: %s", project, msg))
	}
	for project := range prev.ScanErrors {
		if _, ok := curr.ScanErrors[project]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: scan error cleared", project))
	}

	return lines
}

func formatBlockedDelta(b BlockedItem, cleared bool) string {
	label := repoLabelOrProject(b.RepoLabel, b.Project)
	if cleared {
		switch b.Kind {
		case "agent_cooldown":
			return fmt.Sprintf("queue: agent %s cooldown cleared", b.Agent)
		case "parked":
			return fmt.Sprintf("queue: %s: %s unparked", label, b.SetID)
		default:
			return fmt.Sprintf("queue: %s: %s backoff cleared", label, b.SetID)
		}
	}
	switch b.Kind {
	case "agent_cooldown":
		until := ""
		if !b.Until.IsZero() {
			until = b.Until.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("queue: agent %s cooldown until=%s", b.Agent, until)
	case "parked":
		return fmt.Sprintf("queue: %s: %s parked reason=%s", label, b.SetID, b.Reason)
	default:
		until := ""
		if !b.Until.IsZero() {
			until = b.Until.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("queue: %s: %s %s until=%s", label, b.SetID, b.Reason, until)
	}
}

func runningIndex(items []PickedUpSet) map[string]PickedUpSet {
	out := make(map[string]PickedUpSet, len(items))
	for _, item := range items {
		key := repoLabelOrProject(item.RepoLabel, item.Project) + "\x00" + item.SetID
		out[key] = item
	}
	return out
}

func queuedIndex(items []IdleProject) map[string]IdleProject {
	out := make(map[string]IdleProject, len(items))
	for _, item := range items {
		key := repoLabelOrProject(item.RepoLabel, item.Project) + "\x00" + item.ReadySet
		out[key] = item
	}
	return out
}

func blockedIndex(items []BlockedItem) map[string]BlockedItem {
	out := make(map[string]BlockedItem, len(items))
	for _, item := range items {
		out[blockedKey(item)] = item
	}
	return out
}

func blockedKey(item BlockedItem) string {
	if item.Kind == "agent_cooldown" {
		return "agent\x00" + item.Agent
	}
	return repoLabelOrProject(item.RepoLabel, item.Project) + "\x00" + item.SetID + "\x00" + item.Kind
}

func skippedIndex(items []SkippedRepo) map[string]SkippedRepo {
	out := make(map[string]SkippedRepo, len(items))
	for _, item := range items {
		out[repoLabelOrProject(item.RepoLabel, item.Project)] = item
	}
	return out
}

func formatOutcomeDelta(repoLabel, setID string, outcome tasks.DrainOutcome) string {
	return fmt.Sprintf("queue: %s: %s outcome=%s", repoLabel, setID, outcome)
}

// repoLabelOrProject returns the repository-identity label when present,
// otherwise the picker project name. It is a convenience for callers that have
// already derived the repo label; an empty label means "use project fallback".
func repoLabelOrProject(repoLabel, project string) string {
	if repoLabel != "" {
		return repoLabel
	}
	if project != "" {
		return project
	}
	return "(unknown project)"
}

func (s *runOutputState) emitViewTransition(out io.Writer, view RunView, eventLines []string) {
	if s.firstTick {
		RenderRunBaseline(out, view)
		s.firstTick = false
	} else {
		for _, line := range append(DiffRunView(s.prev, view), eventLines...) {
			fmt.Fprintln(out, line)
		}
	}
	copy := view
	s.prev = &copy
}

func (s *runOutputState) emitPostSpawnView(out io.Writer, view RunView) {
	copy := view
	s.prev = &copy
}

func (s *runOutputState) emitScanError(out io.Writer, msg string) {
	if s.lastScan == msg {
		return
	}
	fmt.Fprintln(out, msg)
	s.lastScan = msg
}
