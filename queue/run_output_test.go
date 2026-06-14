package queue

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

func TestBuildRunViewConfigErrorIsScanError(t *testing.T) {
	snap := statusFromDecisions([]Decision{
		{
			Project:            "broken",
			Reason:             "no ready set",
			ProjectConfigError: "/repo/broken/.pop.toml: expected value",
		},
	}, &DaemonState{Version: 1})

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
	snap := statusFromDecisions([]Decision{
		{Project: "running", Busy: true, lockStatus: &tasks.RuntimeLockStatus{
			Locked: true,
			Metadata: &tasks.RuntimeLockMetadata{SetID: "set-a", PID: 99},
		}},
		{Project: "queued", TaskSetID: "set-ready", Reason: "ready"},
		{Project: "idle-a", Reason: "no ready set"},
		{Project: "idle-b", Reason: "no ready set"},
		{Project: "idle-c", Reason: "no ready set"},
	}, &DaemonState{Version: 1})

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
