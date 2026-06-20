package queue

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// RunView is the scheduling-relevant snapshot used for queue run baseline and deltas.
type RunView struct {
	Running             []PickedUpSet
	Queued              []IdleProject
	Blocked             []BlockedItem
	AwaitingIntegration []AwaitingIntegrationSet
	WorktreeBindings    []WorktreeBindingView
	Skipped             []SkippedRepo
	IdleCount           int
	ScanErrors          map[string]string
}

// WorktreeBindingView is one provisioned checkout tracked in queue daemon state.
type WorktreeBindingView struct {
	Project     string
	SetID       string
	Branch      string
	RuntimePath string
	Phase       string
	MergeStatus string
	PID         int
}

// BlockedItem is one blocked scheduling bucket: parked set, backoff, or agent cooldown.
type BlockedItem struct {
	Project string
	SetID   string
	Kind    string
	Until   time.Time
	Reason  string
	Agent   string
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
		Running:             append([]PickedUpSet(nil), snap.PickedUp...),
		AwaitingIntegration: append([]AwaitingIntegrationSet(nil), snap.AwaitingIntegration...),
		Skipped:             append([]SkippedRepo(nil), snap.Skipped...),
		ScanErrors:          map[string]string{},
	}
	blockedProjects := map[string]bool{}

	for _, idle := range snap.Idle {
		switch {
		case idle.Waiting == "error":
			appendScanError(view.ScanErrors, idle.Project, idle.Reason)
		case idle.ProjectConfigError != "":
			appendScanError(view.ScanErrors, idle.Project, idle.ProjectConfigError)
		case idle.ReadySet != "":
			view.Queued = append(view.Queued, idle)
		case isBlockedIdleReason(idle.Reason):
			item := blockedItemFromIdle(idle, snap.DaemonState)
			view.Blocked = append(view.Blocked, item)
			blockedProjects[idle.Project] = true
		default:
			view.IdleCount++
		}
	}

	for _, picked := range view.Running {
		if picked.ProjectConfigError != "" {
			appendScanError(view.ScanErrors, picked.Project, picked.ProjectConfigError)
		}
	}

	if snap.DaemonState != nil {
		view.Blocked = append(view.Blocked, blockedItemsFromDaemonState(snap.DaemonState, now, blockedProjects)...)
		view.WorktreeBindings = buildWorktreeBindingViews(snap.DaemonState, view)
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

func blockedItemFromIdle(idle IdleProject, state *DaemonState) BlockedItem {
	item := BlockedItem{
		Project: idle.Project,
		Reason:  idle.Reason,
		Kind:    blockedKindFromReason(idle.Reason),
	}
	if state != nil {
		for key, parked := range state.ParkedSets {
			if idle.Project != "" && projectForScopedKey(state, key) != idle.Project {
				continue
			}
			item.SetID = parked.SetID
			item.Until = parked.ParkedAt
			return item
		}
		for key, until := range state.SetCrashBackoffs {
			if projectMatchesScopedKey(state, idle.Project, key) {
				item.SetID = setIDFromScopedKey(key)
				item.Until = until
				return item
			}
		}
		for key, until := range state.SetBackoffs {
			if projectMatchesScopedKey(state, idle.Project, key) {
				item.SetID = setIDFromScopedKey(key)
				item.Until = until
				return item
			}
		}
	}
	return item
}

func blockedItemsFromDaemonState(state *DaemonState, now time.Time, seenProjects map[string]bool) []BlockedItem {
	if state == nil {
		return nil
	}
	var out []BlockedItem
	for key, parked := range state.ParkedSets {
		project := projectForScopedKey(state, key)
		if seenProjects[project] {
			continue
		}
		out = append(out, BlockedItem{
			Project: project,
			SetID:   parked.SetID,
			Kind:    "parked",
			Until:   parked.ParkedAt,
			Reason:  parked.Reason,
		})
	}
	for key, until := range state.SetCrashBackoffs {
		if until.IsZero() || !until.After(now) {
			continue
		}
		project, setID := projectSetFromScopedKey(state, key)
		if seenProjects[project] {
			continue
		}
		out = append(out, BlockedItem{
			Project: project,
			SetID:   setID,
			Kind:    "crash_backoff",
			Until:   until,
			Reason:  "set backed off after abnormal drain exit",
		})
	}
	for key, until := range state.SetBackoffs {
		if until.IsZero() || !until.After(now) {
			continue
		}
		project, setID := projectSetFromScopedKey(state, key)
		if seenProjects[project] {
			continue
		}
		out = append(out, BlockedItem{
			Project: project,
			SetID:   setID,
			Kind:    "quota_backoff",
			Until:   until,
			Reason:  "set backed off for pinned agent cooldown",
		})
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

func projectMatchesScopedKey(state *DaemonState, project, key string) bool {
	p, _ := projectSetFromScopedKey(state, key)
	return p == project
}

func projectSetFromScopedKey(state *DaemonState, key string) (project, setID string) {
	setID = setIDFromScopedKey(key)
	if state != nil {
		if p := projectForScopedKey(state, key); p != "" {
			return p, setID
		}
		for _, rec := range state.Mergeability {
			if rec.SetID == setID {
				return rec.Project, setID
			}
		}
	}
	return "", setID
}

func projectForScopedKey(state *DaemonState, key string) string {
	if state == nil {
		return ""
	}
	setID := setIDFromScopedKey(key)
	for _, binding := range state.WorktreeBindings {
		if binding.Project != "" {
			for _, parked := range state.ParkedSets {
				if parked.SetID == setID && parked.RuntimePath == binding.RuntimePath {
					return binding.Project
				}
			}
		}
	}
	for _, rec := range state.Mergeability {
		if rec.SetID == setID {
			return rec.Project
		}
	}
	for _, parked := range state.ParkedSets {
		if parked.SetID == setID {
			for _, binding := range state.WorktreeBindings {
				if binding.RuntimePath == parked.RuntimePath {
					return binding.Project
				}
			}
		}
	}
	return ""
}

func setIDFromScopedKey(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
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
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func formatIntegrationSummary(view RunView) string {
	if len(view.AwaitingIntegration) == 0 {
		return "none awaiting"
	}
	clean, conflicts, unknown := 0, 0, 0
	for _, set := range view.AwaitingIntegration {
		switch set.Status {
		case MergeabilityClean:
			clean++
		case MergeabilityConflicts:
			conflicts++
		default:
			unknown++
		}
	}
	parts := []string{fmt.Sprintf("%d awaiting integration", len(view.AwaitingIntegration))}
	if clean > 0 {
		parts = append(parts, fmt.Sprintf("%d ready to merge", clean))
	}
	if conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", conflicts))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d mergeability unknown", unknown))
	}
	return strings.Join(parts, ", ")
}

// RenderRunSummary prints the aggregate queue and integration headline.
func RenderRunSummary(out io.Writer, view RunView) {
	fmt.Fprintln(out, "Summary:")
	fmt.Fprintf(out, "  Queue: %s\n", formatQueueWorkSummary(view))
	fmt.Fprintf(out, "  Integration: %s\n", formatIntegrationSummary(view))
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
			projectLabel := statusProjectLabel(q.Project, q.WorktreeReady, q.ProjectConfigError)
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

	if len(view.Skipped) > 0 {
		fmt.Fprintln(out, "Skipped repositories:")
		for _, s := range view.Skipped {
			project := s.Project
			if project == "" {
				project = "(unknown project)"
			}
			fmt.Fprintf(out, "  %s: %s\n", project, s.Reason)
		}
	}

	fmt.Fprintln(out, "Awaiting integration:")
	if len(view.AwaitingIntegration) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, set := range view.AwaitingIntegration {
			project := set.Project
			if project == "" {
				project = "(unknown project)"
			}
			setID := set.SetID
			if setID == "" {
				setID = "(unknown set)"
			}
			checked := ""
			if !set.CheckedAt.IsZero() {
				checked = " checked " + set.CheckedAt.UTC().Format(time.RFC3339)
			}
			hint := integrationHint(set.Status, setID)
			if hint != "" {
				fmt.Fprintf(out, "  %s: %s (%s%s)  %s\n", project, setID, mergeabilityLabel(set.Status), checked, hint)
				continue
			}
			fmt.Fprintf(out, "  %s: %s (%s%s)\n", project, setID, mergeabilityLabel(set.Status), checked)
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
	projectLabel := statusProjectLabel(p.Project, p.WorktreeReady, p.ProjectConfigError)
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

func buildWorktreeBindingViews(state *DaemonState, view RunView) []WorktreeBindingView {
	if state == nil || len(state.WorktreeBindings) == 0 {
		return nil
	}
	runningBySet := make(map[string]PickedUpSet, len(view.Running))
	for _, p := range view.Running {
		if p.SetID != "" {
			runningBySet[p.SetID] = p
		}
	}
	awaitingBySet := make(map[string]AwaitingIntegrationSet, len(view.AwaitingIntegration))
	for _, set := range view.AwaitingIntegration {
		if set.SetID != "" {
			awaitingBySet[set.SetID] = set
		}
	}

	keys := make([]string, 0, len(state.WorktreeBindings))
	for key := range state.WorktreeBindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]WorktreeBindingView, 0, len(keys))
	for _, key := range keys {
		binding := state.WorktreeBindings[key]
		setID := setIDFromScopedKey(key)
		project := binding.Project
		if project == "" {
			project = projectForScopedKey(state, key)
		}
		item := WorktreeBindingView{
			Project:     project,
			SetID:       setID,
			Branch:      binding.Branch,
			RuntimePath: binding.RuntimePath,
		}
		if picked, ok := runningBySet[setID]; ok {
			item.Phase = "draining"
			item.PID = picked.PID
		} else if awaiting, ok := awaitingBySet[setID]; ok {
			item.MergeStatus = awaiting.Status
			item.Phase = mergeabilityLabel(awaiting.Status)
		} else {
			item.Phase = "bound"
		}
		items = append(items, item)
	}
	return items
}

func formatWorktreeBindingLine(binding WorktreeBindingView) string {
	project := binding.Project
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
	if hint := integrationHint(binding.MergeStatus, setID); hint != "" {
		line += "  " + hint
	}
	return line
}

func integrationHint(mergeStatus, setID string) string {
	if setID == "" || setID == "(unknown set)" {
		return ""
	}
	switch mergeStatus {
	case MergeabilityClean, MergeabilityConflicts:
		return "integrate: pop tasks integrate " + setID
	default:
		return ""
	}
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
		project := b.Project
		if project == "" {
			project = "(unknown project)"
		}
		return fmt.Sprintf("%s: %s parked (%s)", project, setID, b.Reason)
	default:
		setID := b.SetID
		if setID == "" {
			setID = "(unknown set)"
		}
		project := b.Project
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
		lines = append(lines, fmt.Sprintf("queue: %s: spawned drain for %s", p.Project, setID))
	}

	prevQueued := queuedIndex(prev.Queued)
	currQueued := queuedIndex(curr.Queued)
	for key, q := range currQueued {
		if _, ok := prevQueued[key]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: ready set %s", q.Project, q.ReadySet))
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

	prevAwait := awaitingIndex(prev.AwaitingIntegration)
	currAwait := awaitingIndex(curr.AwaitingIntegration)
	for key, a := range currAwait {
		if _, ok := prevAwait[key]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: %s awaiting integration (%s)", a.Project, a.SetID, mergeabilityLabel(a.Status)))
	}
	for key, a := range prevAwait {
		if _, ok := currAwait[key]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: %s integrated", a.Project, a.SetID))
	}

	prevSkipped := skippedIndex(prev.Skipped)
	currSkipped := skippedIndex(curr.Skipped)
	for project, s := range currSkipped {
		if _, ok := prevSkipped[project]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("queue: %s: %s", project, s.Reason))
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
	if cleared {
		switch b.Kind {
		case "agent_cooldown":
			return fmt.Sprintf("queue: agent %s cooldown cleared", b.Agent)
		case "parked":
			return fmt.Sprintf("queue: %s: %s unparked", b.Project, b.SetID)
		default:
			return fmt.Sprintf("queue: %s: %s backoff cleared", b.Project, b.SetID)
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
		return fmt.Sprintf("queue: %s: %s parked reason=%s", b.Project, b.SetID, b.Reason)
	default:
		until := ""
		if !b.Until.IsZero() {
			until = b.Until.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("queue: %s: %s %s until=%s", b.Project, b.SetID, b.Reason, until)
	}
}

func runningIndex(items []PickedUpSet) map[string]PickedUpSet {
	out := make(map[string]PickedUpSet, len(items))
	for _, item := range items {
		key := item.Project + "\x00" + item.SetID
		out[key] = item
	}
	return out
}

func queuedIndex(items []IdleProject) map[string]IdleProject {
	out := make(map[string]IdleProject, len(items))
	for _, item := range items {
		key := item.Project + "\x00" + item.ReadySet
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
	return item.Project + "\x00" + item.SetID + "\x00" + item.Kind
}

func skippedIndex(items []SkippedRepo) map[string]SkippedRepo {
	out := make(map[string]SkippedRepo, len(items))
	for _, item := range items {
		out[item.Project] = item
	}
	return out
}

func awaitingIndex(items []AwaitingIntegrationSet) map[string]AwaitingIntegrationSet {
	out := make(map[string]AwaitingIntegrationSet, len(items))
	for _, item := range items {
		key := item.Project + "\x00" + item.SetID
		out[key] = item
	}
	return out
}

func formatOutcomeDelta(project, setID string, outcome tasks.DrainOutcome) string {
	return fmt.Sprintf("queue: %s: %s outcome=%s", project, setID, outcome)
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
