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
	"github.com/glebglazov/pop/tasks/binding"
)

func TestBuildRunViewConfigErrorIsScanError(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{
		{
			Project:            "broken",
			Reason:             "no ready set",
			ProjectConfigError: "/repo/broken/.pop.toml: expected value",
		},
	})
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
	}

	var out bytes.Buffer
	RenderRunSummary(&out, view)
	text := out.String()
	for _, want := range []string{
		"Summary:",
		"Queue: 1 running, 2 queued, 1 blocked",
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
	})
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
	td := queueTestTasksDeps(t, true)
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
	td := queueTestTasksDeps(t, true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

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

// TestSeedSpawnedRunningSuppressesDuplicateSpawnLine reproduces the double
// "spawned drain" artifact: a just-spawned drain has not yet acquired its
// runtime lock, so the post-spawn scan still lists its set as Ready (Queued),
// not Running. Without seeding, the next tick's diff — where the drain finally
// holds the lock and appears in Running — re-announces it as freshly spawned.
// seedSpawnedRunning must fold the spawned set into the swallow snapshot so the
// diff sees it already running and stays silent.
func TestSeedSpawnedRunningSuppressesDuplicateSpawnLine(t *testing.T) {
	spawned := []PickedUpSet{{Project: "pop", SetID: "set-1"}}
	// Post-spawn scan: lock not yet held, set still shows as Ready.
	postSpawn := RunView{Queued: []IdleProject{{Project: "pop", ReadySet: "set-1", Waiting: "ready"}}}
	// Next tick: drain now holds the lock and shows as Running.
	nextTick := RunView{Running: []PickedUpSet{{Project: "pop", SetID: "set-1", PID: 42}}}

	// Control: without seeding, the diff re-announces the spawn.
	if lines := DiffRunView(&postSpawn, nextTick); !containsSpawnLine(lines, "set-1") {
		t.Fatalf("unseeded diff should re-announce spawn (the artifact), got %v", lines)
	}

	// Fixed: seed the spawned set into the swallow snapshot.
	seeded := seedSpawnedRunning(postSpawn, spawned)
	if lines := DiffRunView(&seeded, nextTick); containsSpawnLine(lines, "set-1") {
		t.Fatalf("seeded diff must not re-announce spawn, got %v", lines)
	}
}

func containsSpawnLine(lines []string, setID string) bool {
	for _, l := range lines {
		if strings.Contains(l, "spawned drain for "+setID) {
			return true
		}
	}
	return false
}

// TestBuildRunViewAwaitingApprovalBucket checks that a project with an AWAITING-APPROVAL set is
// placed in view.AwaitingApproval rather than view.Blocked or view.IdleCount.
func TestBuildRunViewAwaitingApprovalBucket(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{
		{
			Project:               "pop",
			Reason:                "awaiting approval",
			AwaitingApprovalSetID: "set-hitl",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now())

	if view.IdleCount != 0 {
		t.Fatalf("IdleCount = %d, want 0 for awaiting-approval project", view.IdleCount)
	}
	if len(view.Blocked) != 0 {
		t.Fatalf("Blocked = %v, want empty — awaiting-approval must not be in blocked bucket", view.Blocked)
	}
	if len(view.AwaitingApproval) != 1 {
		t.Fatalf("AwaitingApproval = %v, want one item", view.AwaitingApproval)
	}
	if view.AwaitingApproval[0].Project != "pop" || view.AwaitingApproval[0].SetID != "set-hitl" {
		t.Fatalf("AwaitingApproval[0] = %+v, want pop/set-hitl", view.AwaitingApproval[0])
	}
}

// TestFormatRunSummaryAwaitingApproval checks the queue summary counts awaiting-approval in its own bucket.
func TestFormatRunSummaryAwaitingApproval(t *testing.T) {
	view := RunView{
		Running:          []PickedUpSet{{Project: "a", SetID: "set-a"}},
		Blocked:          []BlockedItem{{Project: "b", SetID: "set-b", Kind: "parked"}},
		AwaitingApproval: []AwaitingApprovalItem{{Project: "c", SetID: "set-c"}},
	}

	var out bytes.Buffer
	RenderRunSummary(&out, view)
	text := out.String()

	for _, want := range []string{
		"1 running",
		"1 blocked",
		"1 awaiting approval",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}

// TestRenderRunBaselineAwaitingApprovalSection checks the baseline renders a distinct
// "Awaiting approval:" section for AWAITING-APPROVAL sets.
func TestRenderRunBaselineAwaitingApprovalSection(t *testing.T) {
	view := RunView{
		AwaitingApproval: []AwaitingApprovalItem{{Project: "pop", SetID: "hitl-set"}},
		ScanErrors:       map[string]string{},
	}

	var out bytes.Buffer
	RenderRunBaseline(&out, view)
	text := out.String()

	for _, want := range []string{
		"Awaiting approval:",
		"pop: hitl-set — awaiting your sign-off",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("baseline missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Blocked:\n  pop") {
		t.Fatalf("awaiting-approval set must not appear under Blocked:\n%s", text)
	}
}

// TestRepoIdentityLabelAcrossSpawnAndBackoff verifies that when several picker
// Projects map to a single Repository identity, both the spawn delta and the
// backoff-cleared delta print the repo-identity basename, not a picker-Project
// name and never a blank prefix.
func TestRepoIdentityLabelAcrossSpawnAndBackoff(t *testing.T) {
	td := queueDataDeps(t)
	_, wts := initBareRepoWithWorktrees(t, 2)
	mainWT := wts[0]
	featureWT := wts[1]

	id, err := tasks.ResolveRepositoryIdentity(td, mainWT)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	if err := tasks.EnsureStorage(td, id); err != nil {
		t.Fatalf("EnsureStorage: %v", err)
	}

	setID := "wt0"
	repoKey := repoIdentityKey(id)
	commonDir := id.CommonDir
	basename := id.Basename

	// Bind the set to the main worktree under one picker project name. The
	// worktree basename matches the setID, so this exercises the managed case
	// where no adopted-worktree suffix is rendered.
	if err := binding.Put(td, setScopedKey(repoKey, setID), WorktreeBinding{RuntimePath: mainWT, Project: "game_server/main"}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}

	// Spawn decision originates from a different picker project (feature worktree)
	// but resolves to the same Repository identity.
	spawnDec := Decision{
		Project:   "game_server/feature",
		TaskSetID: setID,
		Reason:    "ready",
		Busy:      true,
		scan: projectScan{
			Name:           "game_server/feature",
			ProjectPath:    featureWT,
			RuntimePath:    mainWT,
			DefinitionPath: id.TasksDir,
			RepoKey:        repoKey,
			RepoCommonDir:  commonDir,
		},
		lockStatus: &tasks.RuntimeLockStatus{
			Locked:      true,
			RuntimePath: mainWT,
			Metadata: &tasks.RuntimeLockMetadata{
				SetID:       setID,
				RuntimePath: mainWT,
			},
		},
	}

	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{spawnDec})
	if err != nil {
		t.Fatalf("statusFromDecisions: %v", err)
	}
	snap.Tasks = td
	snap.CrashRetryDelays = []time.Duration{time.Minute}

	view := BuildRunView(snap, time.Now().UTC())
	if len(view.Running) != 1 {
		t.Fatalf("expected one running set, got %+v", view.Running)
	}
	if got := view.Running[0].RepoLabel; got != basename {
		t.Fatalf("running RepoLabel = %q, want %q", got, basename)
	}

	lines := DiffRunView(&RunView{}, view)
	if len(lines) != 1 || !strings.Contains(lines[0], "queue: "+basename+": spawned drain for "+setID) {
		t.Fatalf("spawn delta = %v, want repo basename label", lines)
	}

	// Seed an abnormal terminal so the same set enters crash backoff.
	seedAbnormalDrain(t, td, mainWT, setID)
	info, err := tasks.ReadSetBackoff(td, commonDir, setID)
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}

	blockedView := BuildRunView(snap, info.LastAbnormalAt.Add(30*time.Second))
	if len(blockedView.Blocked) != 1 {
		t.Fatalf("expected one blocked item, got %+v", blockedView.Blocked)
	}
	if got := blockedView.Blocked[0].RepoLabel; got != basename {
		t.Fatalf("blocked RepoLabel = %q, want %q", got, basename)
	}
	if got := blockedView.Blocked[0].Project; got != "game_server/main" {
		t.Fatalf("blocked Project = %q, want original picker project preserved", got)
	}

	clearedView := BuildRunView(snap, info.LastAbnormalAt.Add(2*time.Minute))
	lines = DiffRunView(&blockedView, clearedView)
	if len(lines) != 1 || !strings.Contains(lines[0], "queue: "+basename+": "+setID+" backoff cleared") {
		t.Fatalf("backoff cleared delta = %v, want repo basename label", lines)
	}
}

// TestRunViewHidesDoneManagedWorktreeBinding pins the status-surface half of the
// ADR-0121 uniform DONE hide: a DONE set that still holds a managed Worktree
// binding is omitted from Active worktrees by default (the old teardown reminder
// is retired) and revealed by Done inclusion (`--include-done`).
func TestRunViewHidesDoneManagedWorktreeBinding(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo, setID, _ := setupSupervisorSpawnRepo(t, "done-set", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	td := tasks.DefaultDeps()
	t.Cleanup(func() { _ = td.CloseStore() })

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	repoKey := repoIdentityKey(id)
	if err := binding.Put(td, setScopedKey(repoKey, setID), WorktreeBinding{
		RuntimePath: repo, Project: "pop", Branch: "done-branch", Provisioned: true,
	}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}

	// Default: the DONE set's managed binding is hidden.
	hidden := BuildRunView(StatusSnapshot{Tasks: td}, time.Now().UTC())
	if len(hidden.WorktreeBindings) != 0 {
		t.Fatalf("Active worktrees = %+v, want empty (DONE binding hidden by default)", hidden.WorktreeBindings)
	}

	// Done inclusion reveals it.
	shown := BuildRunView(StatusSnapshot{Tasks: td, IncludeDone: true}, time.Now().UTC())
	if len(shown.WorktreeBindings) != 1 || shown.WorktreeBindings[0].SetID != setID {
		t.Fatalf("Active worktrees = %+v, want the DONE binding revealed with include-done", shown.WorktreeBindings)
	}
}

// TestRepoIdentityLabelBaselineDeltaParity verifies that for a bare repo with
// multiple picker Projects, every baseline section and every delta line uses the
// same repo-identity label (the basename), not a picker-Project name.
func TestRepoIdentityLabelBaselineDeltaParity(t *testing.T) {
	td := queueDataDeps(t)
	_, wts := initBareRepoWithWorktrees(t, 2)
	mainWT := wts[0]
	featureWT := wts[1]

	id, err := tasks.ResolveRepositoryIdentity(td, mainWT)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	if err := tasks.EnsureStorage(td, id); err != nil {
		t.Fatalf("EnsureStorage: %v", err)
	}

	basename := id.Basename
	repoKey := repoIdentityKey(id)
	setIDRunning := "wt0"
	setIDQueued := "wt1"

	// Bind the running set under one picker project name.
	if err := binding.Put(td, setScopedKey(repoKey, setIDRunning), WorktreeBinding{RuntimePath: mainWT, Project: "game_server/main"}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}

	// Create running decision from one picker project.
	runningDec := Decision{
		Project:   "game_server/main",
		TaskSetID: setIDRunning,
		Reason:    "ready",
		Busy:      true,
		scan: projectScan{
			Name:           "game_server/main",
			ProjectPath:    mainWT,
			RuntimePath:    mainWT,
			DefinitionPath: id.TasksDir,
			RepoKey:        repoKey,
			RepoCommonDir:  id.CommonDir,
		},
		lockStatus: &tasks.RuntimeLockStatus{
			Locked:      true,
			RuntimePath: mainWT,
			Metadata: &tasks.RuntimeLockMetadata{
				SetID:       setIDRunning,
				RuntimePath: mainWT,
				PID:         100,
			},
		},
	}

	// Create queued decision from a different picker project.
	queuedDec := Decision{
		Project:   "game_server/feature",
		TaskSetID: setIDQueued,
		Reason:    "ready",
		scan: projectScan{
			Name:           "game_server/feature",
			ProjectPath:    featureWT,
			RuntimePath:    featureWT,
			DefinitionPath: id.TasksDir,
			RepoKey:        repoKey,
			RepoCommonDir:  id.CommonDir,
		},
	}

	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{runningDec, queuedDec})
	if err != nil {
		t.Fatalf("statusFromDecisions: %v", err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now().UTC())

	// -- Baseline assertions --
	var buf bytes.Buffer
	RenderRunBaseline(&buf, view)
	baselineText := buf.String()

	// Picked-up sets uses repo-identity label.
	if !strings.Contains(baselineText, basename+": "+setIDRunning) {
		t.Fatalf("baseline Picked-up sets must use repo-identity label %q:\n%s", basename, baselineText)
	}
	// Must NOT contain any picker-Project name as a row prefix.
	if strings.Contains(baselineText, "game_server/main: "+setIDRunning) {
		t.Fatalf("baseline must not use picker-Project name:\n%s", baselineText)
	}

	// Active worktrees uses repo-identity label.
	if !strings.Contains(baselineText, basename+": "+setIDRunning) {
		t.Fatalf("baseline Active worktrees must use repo-identity label %q:\n%s", basename, baselineText)
	}

	// Queued ready sets uses repo-identity label.
	if !strings.Contains(baselineText, basename+": waiting ready set "+setIDQueued) {
		t.Fatalf("baseline Queued ready sets must use repo-identity label %q:\n%s", basename, baselineText)
	}

	// -- Delta assertions (same label as baseline) --
	lines := DiffRunView(&RunView{}, view)
	spawnFound := false
	readyFound := false
	for _, line := range lines {
		if strings.Contains(line, "queue: "+basename+": spawned drain for "+setIDRunning) {
			spawnFound = true
		}
		if strings.Contains(line, "queue: "+basename+": ready set "+setIDQueued) {
			readyFound = true
		}
	}
	if !spawnFound {
		t.Fatalf("delta lines must contain spawn with repo-identity label %q, got:\n%s", basename, strings.Join(lines, "\n"))
	}
	if !readyFound {
		t.Fatalf("delta lines must contain ready-set with repo-identity label %q, got:\n%s", basename, strings.Join(lines, "\n"))
	}

	// Verify spawn delta label matches baseline label exactly (same basename).
	for _, line := range lines {
		if strings.Contains(line, "game_server") && !strings.Contains(line, basename) {
			t.Fatalf("delta line must not reference picker-Project name: %q", line)
		}
	}
}

// TestRepoIdentityLabelFallsBackToProject preserves the familiar label for
// single-worktree repos whose scan did not resolve a separate Repository identity.
func TestRepoIdentityLabelFallsBackToProject(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{{
		Project:   "pop",
		TaskSetID: "set-1",
		Reason:    "ready",
		Busy:      true,
		scan:      projectScan{Name: "pop"},
		lockStatus: &tasks.RuntimeLockStatus{
			Locked: true,
			Metadata: &tasks.RuntimeLockMetadata{
				SetID: "set-1",
			},
		},
	}})
	if err != nil {
		t.Fatalf("statusFromDecisions: %v", err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now().UTC())
	if got := view.Running[0].RepoLabel; got != "pop" {
		t.Fatalf("running RepoLabel = %q, want project fallback", got)
	}

	lines := DiffRunView(&RunView{}, view)
	if len(lines) != 1 || !strings.Contains(lines[0], "queue: pop: spawned drain for set-1") {
		t.Fatalf("spawn delta = %v, want project fallback label", lines)
	}
}

// TestAdoptedWorktreeSuffixOnBaselineAndDelta verifies that when a set's bound
// checkout basename differs from the set identifier, the worktree name is
// surfaced on both baseline rows and delta lines.
func TestAdoptedWorktreeSuffixOnBaselineAndDelta(t *testing.T) {
	td := queueDataDeps(t)
	_, wts := initBareRepoWithWorktrees(t, 2)
	mainWT := wts[0] // basename "wt0"

	id, err := tasks.ResolveRepositoryIdentity(td, mainWT)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	if err := tasks.EnsureStorage(td, id); err != nil {
		t.Fatalf("EnsureStorage: %v", err)
	}

	basename := id.Basename
	repoKey := repoIdentityKey(id)
	setID := "set-adopted" // differs from worktree basename "wt0"

	if err := binding.Put(td, setScopedKey(repoKey, setID), WorktreeBinding{RuntimePath: mainWT, Project: "game_server/main"}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}

	dec := Decision{
		Project:   "game_server/main",
		TaskSetID: setID,
		Reason:    "ready",
		Busy:      true,
		scan: projectScan{
			Name:           "game_server/main",
			ProjectPath:    mainWT,
			RuntimePath:    mainWT,
			DefinitionPath: id.TasksDir,
			RepoKey:        repoKey,
			RepoCommonDir:  id.CommonDir,
		},
		lockStatus: &tasks.RuntimeLockStatus{
			Locked:      true,
			RuntimePath: mainWT,
			Metadata: &tasks.RuntimeLockMetadata{
				SetID:       setID,
				RuntimePath: mainWT,
				PID:         100,
			},
		},
	}

	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{dec})
	if err != nil {
		t.Fatalf("statusFromDecisions: %v", err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now().UTC())

	var buf bytes.Buffer
	RenderRunBaseline(&buf, view)
	baselineText := buf.String()

	wantLabel := basename + " (in wt0): " + setID
	if !strings.Contains(baselineText, wantLabel) {
		t.Fatalf("baseline missing adopted-worktree suffix %q:\n%s", wantLabel, baselineText)
	}

	lines := DiffRunView(&RunView{}, view)
	wantDelta := "queue: " + basename + " (in wt0): spawned drain for " + setID
	found := false
	for _, line := range lines {
		if strings.Contains(line, wantDelta) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("spawn delta missing adopted-worktree suffix %q, got:\n%s", wantDelta, strings.Join(lines, "\n"))
	}
}

// TestManagedWorktreeSuffixSuppressed verifies that when a pop-provisioned
// (managed) worktree's checkout basename equals the set identifier, no
// redundant worktree suffix is appended.
func TestManagedWorktreeSuffixSuppressed(t *testing.T) {
	td := queueDataDeps(t)
	_, wts := initBareRepoWithWorktrees(t, 2)
	mainWT := wts[0] // basename "wt0"

	id, err := tasks.ResolveRepositoryIdentity(td, mainWT)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	if err := tasks.EnsureStorage(td, id); err != nil {
		t.Fatalf("EnsureStorage: %v", err)
	}

	basename := id.Basename
	repoKey := repoIdentityKey(id)
	setID := "wt0" // matches worktree basename, so managed/no suffix

	if err := binding.Put(td, setScopedKey(repoKey, setID), WorktreeBinding{RuntimePath: mainWT, Project: "game_server/main"}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}

	dec := Decision{
		Project:   "game_server/main",
		TaskSetID: setID,
		Reason:    "ready",
		Busy:      true,
		scan: projectScan{
			Name:           "game_server/main",
			ProjectPath:    mainWT,
			RuntimePath:    mainWT,
			DefinitionPath: id.TasksDir,
			RepoKey:        repoKey,
			RepoCommonDir:  id.CommonDir,
		},
		lockStatus: &tasks.RuntimeLockStatus{
			Locked:      true,
			RuntimePath: mainWT,
			Metadata: &tasks.RuntimeLockMetadata{
				SetID:       setID,
				RuntimePath: mainWT,
				PID:         100,
			},
		},
	}

	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{dec})
	if err != nil {
		t.Fatalf("statusFromDecisions: %v", err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now().UTC())

	var buf bytes.Buffer
	RenderRunBaseline(&buf, view)
	baselineText := buf.String()

	if strings.Contains(baselineText, "(in wt0)") {
		t.Fatalf("managed worktree must not render adopted suffix:\n%s", baselineText)
	}
	wantLabel := basename + ": " + setID
	if !strings.Contains(baselineText, wantLabel) {
		t.Fatalf("baseline missing managed label %q:\n%s", wantLabel, baselineText)
	}

	lines := DiffRunView(&RunView{}, view)
	for _, line := range lines {
		if strings.Contains(line, "(in wt0)") {
			t.Fatalf("managed spawn delta must not render adopted suffix: %q", line)
		}
	}
	wantDelta := "queue: " + basename + ": spawned drain for " + setID
	found := false
	for _, line := range lines {
		if strings.Contains(line, wantDelta) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("spawn delta missing managed label %q, got:\n%s", wantDelta, strings.Join(lines, "\n"))
	}
}

// TestInPlaceDrainNoWorktreeSuffix verifies that a trunk/in-place drain with
// no worktree binding shows no worktree suffix.
func TestInPlaceDrainNoWorktreeSuffix(t *testing.T) {
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{{
		Project:   "pop",
		TaskSetID: "set-1",
		Reason:    "ready",
		Busy:      true,
		scan:      projectScan{Name: "pop", RepoKey: "pop"},
		lockStatus: &tasks.RuntimeLockStatus{
			Locked: true,
			Metadata: &tasks.RuntimeLockMetadata{
				SetID: "set-1",
			},
		},
	}})
	if err != nil {
		t.Fatalf("statusFromDecisions: %v", err)
	}
	snap.Tasks = td

	view := BuildRunView(snap, time.Now().UTC())

	var buf bytes.Buffer
	RenderRunBaseline(&buf, view)
	baselineText := buf.String()
	if strings.Contains(baselineText, "(in ") {
		t.Fatalf("in-place drain must not render worktree suffix:\n%s", baselineText)
	}

	lines := DiffRunView(&RunView{}, view)
	for _, line := range lines {
		if strings.Contains(line, "(in ") {
			t.Fatalf("in-place spawn delta must not render worktree suffix: %q", line)
		}
	}
}
