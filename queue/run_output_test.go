package queue

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

func TestBuildRunViewConfigErrorIsScanError(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{
		{
			Project:            "broken",
			Reason:             "no ready set",
			ProjectConfigError: "/repo/broken/.pop.toml: expected value",
		},
	}, &DaemonState{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now())
	if view.IdleCount != 0 {
		t.Fatalf("IdleCount = %d, want 0 for config error project", view.IdleCount)
	}
	if got := view.ScanErrors["broken"]; !strings.Contains(got, ".pop.toml") {
		t.Fatalf("ScanErrors[broken] = %q, want .pop.toml parse error", got)
	}
}

func TestFormatRunSummary(t *testing.T) {
	view := RunView{
		Running: []PickedUpSet{{Project: "a", SetID: "set-a"}},
		Queued: []IdleProject{
			{Project: "b", ReadySet: "set-b"},
			{Project: "c", ReadySet: "set-c"},
		},
		Blocked: []BlockedItem{{Project: "d", SetID: "set-d", Kind: "parked"}},
		AwaitingIntegration: []AwaitingIntegrationSet{
			{Project: "e", SetID: "set-e", Status: MergeabilityClean},
			{Project: "f", SetID: "set-f", Status: MergeabilityConflicts},
		},
	}

	var out bytes.Buffer
	RenderRunSummary(&out, view)
	text := out.String()
	for _, want := range []string{
		"Summary:",
		"Queue: 1 running, 2 queued, 1 blocked",
		"Integration: 2 awaiting integration, 1 ready to merge, 1 conflicts",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}

func TestRenderRunBaselineCollapsesIdleProjects(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{
		{Project: "running", Busy: true, lockStatus: &tasks.RuntimeLockStatus{
			Locked:   true,
			Metadata: &tasks.RuntimeLockMetadata{SetID: "set-a", PID: 99},
		}},
		{Project: "queued", TaskSetID: "set-ready", Reason: "ready"},
		{Project: "idle-a", Reason: "no ready set"},
		{Project: "idle-b", Reason: "no ready set"},
		{Project: "idle-c", Reason: "no ready set"},
	}, &DaemonState{Version: 1})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = queueDataDeps(t)

	view := BuildRunView(snap, time.Now())
	if view.IdleCount != 3 {
		t.Fatalf("IdleCount = %d, want 3", view.IdleCount)
	}

	var out bytes.Buffer
	RenderRunBaseline(&out, view)
	text := out.String()
	for _, want := range []string{
		"Summary:",
		"Picked-up sets:",
		"Active worktrees:",
		"running: set-a pid=99",
		"Queued ready sets:",
		"queued: waiting ready set set-ready",
		"3 other projects: no ready work",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("baseline missing %q:\n%s", want, text)
		}
	}
	for _, omit := range []string{"idle-a", "idle-b", "idle-c", "Daemon state", `"version"`} {
		if strings.Contains(text, omit) {
			t.Fatalf("baseline should not contain %q:\n%s", omit, text)
		}
	}
}

func TestBuildStatusReportsTaskOwnedAgentCooldowns(t *testing.T) {
	td := queueDataDeps(t)
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	data, err := json.Marshal(map[string]tasks.AgentCooldownEntry{
		"codex": {ExhaustedUntil: until},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := tasks.AgentCooldownPathWith(td)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{Tasks: td, Project: project.DefaultDeps()}

	snap, err := BuildStatus(d, &config.Config{})
	if err != nil {
		t.Fatalf("build status: %v", err)
	}
	view := BuildRunView(snap, time.Now())

	if len(view.Blocked) != 1 || view.Blocked[0].Kind != "agent_cooldown" || view.Blocked[0].Agent != "codex" || !view.Blocked[0].Until.Equal(until) {
		t.Fatalf("blocked = %+v, want codex cooldown from task store until %s", view.Blocked, until)
	}
}

func TestRunOutputBaselineOnceAndQuietTick(t *testing.T) {
	repo := t.TempDir()
	spawnInitGitRepo(t, repo)
	xdg := filepath.Join(repo, ".xdg")
	t.Setenv("XDG_DATA_HOME", xdg)

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	td := queueTestTasksDeps(true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       newRecordingTmux(false, "0"),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh:    func(defPath string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
	}

	var out bytes.Buffer
	runOut := newRunOutputState()

	tick(d, &out, runOut)
	first := out.String()
	if !strings.Contains(first, "Picked-up sets:") {
		t.Fatalf("first tick must print baseline:\n%s", first)
	}
	if !strings.Contains(first, "1 other project: no ready work") {
		t.Fatalf("first tick baseline missing collapsed idle count:\n%s", first)
	}
	if strings.Contains(first, "Daemon state:") {
		t.Fatalf("baseline must not dump daemon JSON:\n%s", first)
	}

	tick(d, &out, runOut)
	second := out.String()[len(first):]
	if strings.TrimSpace(second) != "" {
		t.Fatalf("quiet second tick must print nothing, got:\n%q", second)
	}
}

func TestRunOutputSpawnDelta(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "spawn-set", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	td := queueTestTasksDeps(true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	want := "spawned drain for " + setID
	if !strings.Contains(out.String(), want) {
		t.Fatalf("spawn tick must emit delta %q:\n%s", want, out.String())
	}
}

func TestDiffRunViewOutcomeTransition(t *testing.T) {
	prev := RunView{
		Running: []PickedUpSet{{Project: "pop", SetID: "set-1", PID: 42}},
	}
	curr := RunView{
		Queued: []IdleProject{{Project: "pop", ReadySet: "set-1", Waiting: "ready"}},
	}

	lines := DiffRunView(&prev, curr)
	if len(lines) != 1 {
		t.Fatalf("diff lines = %v, want one queued transition", lines)
	}
	if !strings.Contains(lines[0], "ready set set-1") {
		t.Fatalf("diff line = %q, want ready set transition", lines[0])
	}
}

func TestRecordTerminalOutcomesEmitsOutcomeDelta(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	writtenAt := time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:       "set-1",
				RuntimePath: repo,
				Outcome:     tasks.DrainOutcomeDone,
				WrittenAt:   writtenAt,
			}, nil
		},
	}

	var events []string
	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, &events); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}
	if len(events) != 1 || !strings.Contains(events[0], "outcome=done") {
		t.Fatalf("events = %v, want outcome delta", events)
	}
}

// TestRecordTerminalOutcomesEmitsUnverifiedDelta checks that an unverified drain
// outcome produces a distinct delta line separate from a blocked outcome.
func TestRecordTerminalOutcomesEmitsUnverifiedDelta(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	writtenAt := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:       "set-hitl",
				RuntimePath: repo,
				Outcome:     tasks.DrainOutcomeUnverified,
				WrittenAt:   writtenAt,
			}, nil
		},
	}

	var events []string
	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, &events); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %v, want exactly one delta line", events)
	}
	line := events[0]
	if !strings.Contains(line, "outcome=unverified") {
		t.Fatalf("delta line = %q, want outcome=unverified", line)
	}
	if strings.Contains(line, "outcome=blocked") {
		t.Fatalf("delta line = %q, must not say outcome=blocked for unverified outcome", line)
	}
}

// TestBuildRunViewUnverifiedBucket checks that a project with an UNVERIFIED set is
// placed in view.Unverified rather than view.Blocked or view.IdleCount.
func TestBuildRunViewUnverifiedBucket(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{
		{
			Project:         "pop",
			Reason:          "awaiting verification",
			UnverifiedSetID: "set-hitl",
		},
	}, &DaemonState{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now())

	if view.IdleCount != 0 {
		t.Fatalf("IdleCount = %d, want 0 for unverified project", view.IdleCount)
	}
	if len(view.Blocked) != 0 {
		t.Fatalf("Blocked = %v, want empty — unverified must not be in blocked bucket", view.Blocked)
	}
	if len(view.Unverified) != 1 {
		t.Fatalf("Unverified = %v, want one item", view.Unverified)
	}
	if view.Unverified[0].Project != "pop" || view.Unverified[0].SetID != "set-hitl" {
		t.Fatalf("Unverified[0] = %+v, want pop/set-hitl", view.Unverified[0])
	}
}

// TestFormatRunSummaryUnverified checks the queue summary counts unverified in its own bucket.
func TestFormatRunSummaryUnverified(t *testing.T) {
	view := RunView{
		Running:    []PickedUpSet{{Project: "a", SetID: "set-a"}},
		Blocked:    []BlockedItem{{Project: "b", SetID: "set-b", Kind: "parked"}},
		Unverified: []UnverifiedItem{{Project: "c", SetID: "set-c"}},
	}

	var out bytes.Buffer
	RenderRunSummary(&out, view)
	text := out.String()

	for _, want := range []string{
		"1 running",
		"1 blocked",
		"1 awaiting verification",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}

// TestRenderRunBaselineUnverifiedSection checks the baseline renders a distinct
// "Awaiting verification:" section for UNVERIFIED sets.
func TestRenderRunBaselineUnverifiedSection(t *testing.T) {
	view := RunView{
		Unverified: []UnverifiedItem{{Project: "pop", SetID: "hitl-set"}},
		ScanErrors: map[string]string{},
	}

	var out bytes.Buffer
	RenderRunBaseline(&out, view)
	text := out.String()

	for _, want := range []string{
		"Awaiting verification:",
		"pop: hitl-set — awaiting your check",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("baseline missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Blocked:\n  pop") {
		t.Fatalf("unverified set must not appear under Blocked:\n%s", text)
	}
}

// TestRenderLogUnverifiedOutcome checks the journal log renders unverified outcomes by name.
func TestRenderLogUnverifiedOutcome(t *testing.T) {
	ts := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	entries := []JournalEntry{
		{
			Timestamp: ts,
			Event:     JournalEventOutcome,
			Project:   "pop",
			SetID:     "set-hitl",
			Outcome:   tasks.DrainOutcomeUnverified,
		},
	}

	var out bytes.Buffer
	RenderLog(&out, entries, 10)
	text := out.String()

	if !strings.Contains(text, "outcome=unverified") {
		t.Fatalf("log missing outcome=unverified:\n%s", text)
	}
	if strings.Contains(text, "outcome=blocked") {
		t.Fatalf("log must not say outcome=blocked for unverified:\n%s", text)
	}
}
