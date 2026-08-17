package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func spendFixture(t *testing.T) *execFixture {
	t.Helper()
	d := newTestDeps(t)
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	id, err := ResolveRepositoryIdentity(d, root)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	if err := EnsureStorage(d, id); err != nil {
		t.Fatalf("ensure storage: %v", err)
	}
	return &execFixture{root: root, tasksDir: id.TasksDir, d: d}
}

func registerSpendSet(t *testing.T, env *execFixture, setID string, tasks []Task) string {
	t.Helper()
	setupManifest(t, env.tasksDir, setID, tasks)
	if _, err := RegisterWith(env.deps(), env.tasksDir, DefaultStatePathWith(env.deps())); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(env.tasksDir, setID)
}

func TestSpendRollupSelectsTenMostRecentSets(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	for i := 1; i <= 11; i++ {
		setID := formatSpendSetID(i)
		setDir := registerSpendSet(t, env, setID, []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		})
		writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Duration(i)*time.Minute), []streamEventRecord{
			{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
		})
	}

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != spendRollupSetLimit {
		t.Fatalf("sets = %d, want %d", len(result.Sets), spendRollupSetLimit)
	}
	seen := map[string]bool{}
	for _, row := range result.Sets {
		seen[row.TaskSetID] = true
	}
	if seen["2026-06-10-set-01"] {
		t.Fatal("oldest set should be excluded")
	}
	for i := 2; i <= 11; i++ {
		if !seen[formatSpendSetID(i)] {
			t.Fatalf("missing expected set %q", formatSpendSetID(i))
		}
	}
	if result.Sets[0].TaskSetID != formatSpendSetID(11) {
		t.Fatalf("default order starts with %q, want newest-by-run-start %q", result.Sets[0].TaskSetID, formatSpendSetID(11))
	}
}

func TestSpendRollupSortsRowsByTotalTokens(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	lowDir := registerSpendSet(t, env, "2026-06-10-low", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, lowDir, "01-a.md", "01-a", "claude", base.Add(time.Hour), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":10,"output_tokens":5}}`},
	})

	highDir := registerSpendSet(t, env, "2026-06-11-high", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, highDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":25,"cache_creation_input_tokens":5}}`},
	})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Sort:         SpendSortTokens,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 2 {
		t.Fatalf("sets = %d, want 2", len(result.Sets))
	}
	if result.Sets[0].TaskSetID != "2026-06-11-high" || result.Sets[1].TaskSetID != "2026-06-10-low" {
		t.Fatalf("order = %#v, want high before low by total tokens", result.Sets)
	}
	high := result.Sets[0]
	if high.Tokens.Input != 100 || high.Tokens.Output != 50 || high.Tokens.CacheRead != 25 || high.Tokens.CacheWrite != 5 {
		t.Fatalf("high tokens = %+v", high.Tokens)
	}
	if high.RunCount != 1 || high.TokenBlindRuns != 0 {
		t.Fatalf("high counts = runs %d blind %d", high.RunCount, high.TokenBlindRuns)
	}
}

func TestSpendRollupDefaultOrdersByLastRunAt(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	olderID := registerSpendSet(t, env, "2026-06-09-older-id", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, olderID, "01-a.md", "01-a", "claude", base.Add(2*time.Hour), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})

	newerID := registerSpendSet(t, env, "2026-06-12-newer-id", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, newerID, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 2 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	if result.Sets[0].TaskSetID != "2026-06-09-older-id" || result.Sets[1].TaskSetID != "2026-06-12-newer-id" {
		t.Fatalf("order = %#v, want run-start recency over identifier order", []string{result.Sets[0].TaskSetID, result.Sets[1].TaskSetID})
	}
}

func TestSpendRollupTokensTieBreaksByRecency(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	usage := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":10,"output_tokens":5}}`},
	}

	older := registerSpendSet(t, env, "2026-06-10-a", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, older, "01-a.md", "01-a", "claude", base, usage)

	newer := registerSpendSet(t, env, "2026-06-10-b", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, newer, "01-a.md", "01-a", "claude", base.Add(time.Hour), usage)

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Sort:         SpendSortTokens,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 2 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	if result.Sets[0].TaskSetID != "2026-06-10-b" || result.Sets[1].TaskSetID != "2026-06-10-a" {
		t.Fatalf("order = %#v, want equal tokens broken newest-first", []string{result.Sets[0].TaskSetID, result.Sets[1].TaskSetID})
	}
}

func TestSpendRollupMissingLastRunAtSortsLast(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	registerSpendSet(t, env, "2026-06-12-no-runs", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	withRuns := registerSpendSet(t, env, "2026-06-09-with-runs", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, withRuns, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 2 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	if result.Sets[0].TaskSetID != "2026-06-09-with-runs" || result.Sets[1].TaskSetID != "2026-06-12-no-runs" {
		t.Fatalf("order = %#v, want readable last_run_at before missing", []string{result.Sets[0].TaskSetID, result.Sets[1].TaskSetID})
	}
	if !result.Sets[1].LastRunAt.IsZero() {
		t.Fatalf("no-runs LastRunAt = %v, want zero", result.Sets[1].LastRunAt)
	}
}

func TestSpendRollupRejectsUnknownSort(t *testing.T) {
	env := spendFixture(t)
	_, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Sort:         "bogus",
	})
	if err == nil {
		t.Fatal("expected unknown sort to be refused")
	}
	msg := err.Error()
	for _, want := range []string{"bogus", SpendSortRecency, SpendSortTokens, SpendSortCost} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
	assertExitCode(t, err, ExitSetup)
}

func TestSpendRollupCountsTokenBlindRuns(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":10,"output_tokens":5}}`},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "codex", base.Add(time.Minute), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","result":"ok"}`},
	})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 1 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	row := result.Sets[0]
	if row.RunCount != 2 || row.TokenBlindRuns != 1 {
		t.Fatalf("counts = runs %d blind %d, want 2 and 1", row.RunCount, row.TokenBlindRuns)
	}
	if row.Tokens.Input != 10 || row.Tokens.Output != 5 {
		t.Fatalf("tokens = %+v", row.Tokens)
	}
}

func TestRenderSpendRollupJSON(t *testing.T) {
	turns := 4
	peak := int64(900)
	lastRun := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Project:   "pop",
		Tokens: TokenUsage{
			Input: 10, Output: 5, CacheRead: 2, CacheWrite: 1,
			HasInput: true, HasOutput: true, HasCacheRead: true, HasCacheWrite: true,
		},
		Turns:          TurnCount{Count: turns, HasTurn: true},
		PeakInput:      PeakInput{Tokens: peak, HasPeak: true},
		RunCount:       3,
		TokenBlindRuns: 1,
		TurnBlindRuns:  1,
		PeakBlindRuns:  1,
		LastRunAt:      lastRun,
	}}}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Sets) != 1 {
		t.Fatalf("sets = %#v", decoded.Sets)
	}
	got := decoded.Sets[0]
	if got.TaskSetID != "demo" || got.Project != "pop" || got.InputTokens != 10 || got.OutputTokens != 5 ||
		got.CacheReadTokens != 2 || got.CacheWriteTokens != 1 ||
		got.RunCount != 3 || got.TokenBlindRuns != 1 {
		t.Fatalf("row = %+v", got)
	}
	if got.Turns == nil || *got.Turns != turns || got.TurnBlindRuns != 1 {
		t.Fatalf("turns = %v blind = %d, want %d and 1 blind", got.Turns, got.TurnBlindRuns, turns)
	}
	if got.PeakInputTokens == nil || *got.PeakInputTokens != peak || got.PeakBlindRuns != 1 {
		t.Fatalf("peak = %v blind = %d, want %d and 1 blind", got.PeakInputTokens, got.PeakBlindRuns, peak)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(lastRun) {
		t.Fatalf("last_run_at = %v, want %v", got.LastRunAt, lastRun)
	}

	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	sets, _ := raw["sets"].([]any)
	row, _ := sets[0].(map[string]any)
	if _, ok := row["last_run_at"]; !ok {
		t.Fatalf("last_run_at missing from JSON row: %s", buf.String())
	}
	if _, ok := row["project"]; !ok {
		t.Fatalf("project missing from JSON row: %s", buf.String())
	}

	empty := &SpendRollupResult{Sets: []SpendRollupRow{{TaskSetID: "empty"}}}
	buf.Reset()
	if err := RenderSpendRollupJSON(&buf, empty); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	sets, _ = raw["sets"].([]any)
	row, _ = sets[0].(map[string]any)
	if v, ok := row["last_run_at"]; !ok || v != nil {
		t.Fatalf("last_run_at = %#v, want present null", row["last_run_at"])
	}
	if _, ok := row["project"]; !ok {
		t.Fatalf("project missing from empty JSON row: %s", buf.String())
	}
}

func TestRenderSpendRollupHumanTable(t *testing.T) {
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Project:   "pop",
		Tokens: TokenUsage{
			Input: 100, Output: 50, CacheRead: 10,
			HasInput: true, HasOutput: true, HasCacheRead: true,
		},
		RunCount:       2,
		TokenBlindRuns: 1,
	}}}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	for _, want := range []string{"project", "task set", "turns", "peak-in", "cache-r", "cache-w", "runs", "blind", "pop", "demo", "100", "50", "10", "2", "1", "token-blind runs"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "—") {
		t.Fatalf("expected em dash for absent cache-write, got:\n%s", out)
	}
}

func TestSpendRollupIsReadOnly(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})

	before, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	runsBefore, err := os.ReadDir(capturedRunsDir(setDir))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("spend mutated task manifest")
	}
	runsAfter, err := os.ReadDir(capturedRunsDir(setDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsBefore) != len(runsAfter) {
		t.Fatalf("runs dir changed: %d -> %d", len(runsBefore), len(runsAfter))
	}
}

func TestSpendRollupExcludesArchivedSets(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	activeDir := registerSpendSet(t, env, "2026-06-11-active", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, activeDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})

	archivedDir := registerSpendSet(t, env, "2026-06-12-archived", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, archivedDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":999,"output_tokens":999}}`},
	})
	if _, err := ArchiveTaskSetWith(env.deps(), nil, nil, ResolveInput{CWD: env.root}, "2026-06-12-archived"); err != nil {
		t.Fatal(err)
	}

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 1 || result.Sets[0].TaskSetID != "2026-06-11-active" {
		t.Fatalf("sets = %#v, want only active set", result.Sets)
	}
}

func TestSpendRollupAllCrossesProjects(t *testing.T) {
	d := newTestDeps(t)
	base := t.TempDir()
	envA := spendFixtureAt(t, d, filepath.Join(base, "personal", "pop"))
	envB := spendFixtureAt(t, d, filepath.Join(base, "work", "pop"))
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	dirA := registerSpendSet(t, envA, "2026-06-10-alpha", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, dirA, "01-a.md", "01-a", "claude", start, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})
	dirB := registerSpendSet(t, envB, "2026-06-11-beta", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, dirB, "01-a.md", "01-a", "claude", start.Add(time.Minute), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":2,"output_tokens":2}}`},
	})

	scoped, err := SpendRollupWith(d, nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: envA.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Sets) != 1 || scoped.Sets[0].TaskSetID != "2026-06-10-alpha" {
		t.Fatalf("repo-scoped sets = %#v", scoped.Sets)
	}
	if scoped.Sets[0].Project == "" {
		t.Fatal("repo-scoped row missing project")
	}

	all, err := SpendRollupWith(d, nil, nil, SpendOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Sets) != 2 {
		t.Fatalf("all sets = %#v, want 2", all.Sets)
	}
	byID := map[string]SpendRollupRow{}
	for _, row := range all.Sets {
		byID[row.TaskSetID] = row
	}
	if byID["2026-06-10-alpha"].Project == byID["2026-06-11-beta"].Project {
		t.Fatalf("colliding basenames not disambiguated: %#v", all.Sets)
	}
	if !strings.Contains(byID["2026-06-10-alpha"].Project, "personal") {
		t.Fatalf("alpha project = %q, want personal disambiguator", byID["2026-06-10-alpha"].Project)
	}
	if !strings.Contains(byID["2026-06-11-beta"].Project, "work") {
		t.Fatalf("beta project = %q, want work disambiguator", byID["2026-06-11-beta"].Project)
	}
	if all.Sets[0].TaskSetID != "2026-06-11-beta" {
		t.Fatalf("flat recency order starts with %q, want beta", all.Sets[0].TaskSetID)
	}
}

func TestSpendRollupAllRespectsLimit(t *testing.T) {
	d := newTestDeps(t)
	env := spendFixtureAt(t, d, t.TempDir())
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		setDir := registerSpendSet(t, env, formatSpendSetID(i), []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		})
		writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Duration(i)*time.Minute), []streamEventRecord{
			{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
		})
	}
	result, err := SpendRollupWith(d, nil, nil, SpendOptions{All: true, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 3 {
		t.Fatalf("sets = %d, want 3", len(result.Sets))
	}
	if result.Sets[0].TaskSetID != formatSpendSetID(5) {
		t.Fatalf("newest first = %q", result.Sets[0].TaskSetID)
	}
}

func TestSpendRollupAllSkipsMissingStorage(t *testing.T) {
	d := newTestDeps(t)
	base := t.TempDir()
	envKeep := spendFixtureAt(t, d, filepath.Join(base, "keep"))
	envGone := spendFixtureAt(t, d, filepath.Join(base, "gone"))
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	keepDir := registerSpendSet(t, envKeep, "2026-06-10-keep", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, keepDir, "01-a.md", "01-a", "claude", start, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})
	goneDir := registerSpendSet(t, envGone, "2026-06-11-gone", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, goneDir, "01-a.md", "01-a", "claude", start.Add(time.Minute), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":9,"output_tokens":9}}`},
	})
	if err := os.RemoveAll(filepath.Dir(envGone.tasksDir)); err != nil {
		t.Fatal(err)
	}

	result, err := SpendRollupWith(d, nil, nil, SpendOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedStorages != 1 {
		t.Fatalf("SkippedStorages = %d, want 1", result.SkippedStorages)
	}
	if len(result.Sets) != 1 || result.Sets[0].TaskSetID != "2026-06-10-keep" {
		t.Fatalf("sets = %#v, want only keep", result.Sets)
	}

	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	if !strings.Contains(buf.String(), "1 missing storages skipped") {
		t.Fatalf("footer missing skipped count:\n%s", buf.String())
	}
}

func TestSpendRollupAllExcludesArchived(t *testing.T) {
	d := newTestDeps(t)
	env := spendFixtureAt(t, d, t.TempDir())
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	activeDir := registerSpendSet(t, env, "2026-06-11-active", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, activeDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	})
	archivedDir := registerSpendSet(t, env, "2026-06-12-archived", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, archivedDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":999,"output_tokens":999}}`},
	})
	if _, err := ArchiveTaskSetWith(d, nil, nil, ResolveInput{CWD: env.root}, "2026-06-12-archived"); err != nil {
		t.Fatal(err)
	}

	result, err := SpendRollupWith(d, nil, nil, SpendOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 1 || result.Sets[0].TaskSetID != "2026-06-11-active" {
		t.Fatalf("sets = %#v", result.Sets)
	}
}

func spendFixtureAt(t *testing.T, d *Deps, root string) *execFixture {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	initExecutorGitRepo(t, root)
	id, err := ResolveRepositoryIdentity(d, root)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	if err := EnsureStorage(d, id); err != nil {
		t.Fatalf("ensure storage: %v", err)
	}
	return &execFixture{root: root, tasksDir: id.TasksDir, d: d}
}

func formatSpendSetID(n int) string {
	return fmt.Sprintf("2026-06-10-set-%02d", n)
}

type spendRunOpts struct {
	phase          string
	attempt        int
	outcome        string
	requestedAgent string
	model          string
}

func writeSpendRunEx(t *testing.T, taskSetDir, taskFile, taskID, agent string, start time.Time, events []streamEventRecord, opts spendRunOpts) {
	t.Helper()
	if opts.phase == "" {
		opts.phase = "implement"
	}
	if opts.attempt == 0 {
		opts.attempt = 1
	}
	if opts.outcome == "" {
		opts.outcome = "completed"
	}
	dir := capturedRunsDir(taskSetDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runID := uuid.New().String()
	meta := capturedRunMeta{
		RunID:          runID,
		Phase:          opts.phase,
		TaskSetID:      filepath.Base(taskSetDir),
		TaskID:         taskID,
		TaskFile:       taskFile,
		StartTime:      start.UTC(),
		EndTime:        start.Add(time.Minute).UTC(),
		Outcome:        opts.outcome,
		Agent:          agent,
		RequestedAgent: opts.requestedAgent,
		Model:          opts.model,
		Attempt:        opts.attempt,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".meta.json"), metaData, 0o644); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, line...)
		raw = append(raw, '\n')
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".events.jsonl.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func claudeUsageEvents(input, output int64) []streamEventRecord {
	return []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: fmt.Sprintf(`{"type":"result","usage":{"input_tokens":%d,"output_tokens":%d}}`, input, output)},
	}
}

func claudeTurnAndUsageEvents(turns int, input, output int64) []streamEventRecord {
	events := make([]streamEventRecord, 0, turns+1)
	for i := 0; i < turns; i++ {
		events = append(events, streamEventRecord{
			Type: "event", AtMS: int64(i + 1),
			Raw: fmt.Sprintf(`{"type":"assistant","message":{"id":"msg_%d","role":"assistant","usage":{"input_tokens":%d,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, i, input/int64(turns)),
		})
	}
	events = append(events, streamEventRecord{
		Type: "event", AtMS: 100,
		Raw: fmt.Sprintf(`{"type":"result","usage":{"input_tokens":%d,"output_tokens":%d}}`, input, output),
	})
	return events
}

func TestSpendSetBreakdownPerTask(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "Second", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudeUsageEvents(100, 50))
	writeSpendRun(t, setDir, "02-b.md", "02-b", "claude", base.Add(time.Minute), claudeUsageEvents(200, 100))

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if result.Rows[0].TaskID != "01-a" || result.Rows[1].TaskID != "02-b" {
		t.Fatalf("row order = %#v", result.Rows)
	}
	if result.Rows[0].Tokens.Input != 100 || result.Rows[1].Tokens.Input != 200 {
		t.Fatalf("task tokens = %#v", result.Rows)
	}
	if result.TokensPerCompletedTask == nil || *result.TokensPerCompletedTask != 225 {
		t.Fatalf("tokens/completed = %v, want 225", result.TokensPerCompletedTask)
	}
	if result.CompletedTasks != 2 {
		t.Fatalf("completed = %d", result.CompletedTasks)
	}
}

func TestSpendSetBreakdownChargesFailedAndRetriedAttemptsToTask(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeSpendRunEx(t, setDir, "01-a.md", "01-a", "claude", base, claudeUsageEvents(10, 5), spendRunOpts{attempt: 1, outcome: "failed"})
	writeSpendRunEx(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), claudeUsageEvents(20, 10), spendRunOpts{attempt: 2, outcome: "completed"})

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %#v", result.Rows)
	}
	row := result.Rows[0]
	if row.RunCount != 2 || row.Tokens.Input != 30 || row.Tokens.Output != 15 {
		t.Fatalf("row = %+v", row)
	}
	if result.TokensPerCompletedTask != nil {
		t.Fatalf("expected no tokens/completed with zero done tasks, got %d", *result.TokensPerCompletedTask)
	}
}

func TestSpendSetBreakdownListsVerifyRunsSeparately(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudeUsageEvents(100, 50))
	writeSpendRunEx(t, setDir, "", "", "claude", base.Add(5*time.Minute), claudeUsageEvents(500, 250), spendRunOpts{phase: "verify"})

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Rows[1].TaskID != "verify" {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if result.Rows[0].Tokens.Input != 100 || result.Rows[1].Tokens.Input != 500 {
		t.Fatalf("task vs verify tokens = %#v", result.Rows)
	}
	if result.VerificationRunCount != 1 || result.VerificationTokens.Input != 500 {
		t.Fatalf("verification = runs %d tokens %+v", result.VerificationRunCount, result.VerificationTokens)
	}
	if result.ImplementRunCount != 1 || result.ImplementTokens.Input != 100 {
		t.Fatalf("implement = runs %d tokens %+v", result.ImplementRunCount, result.ImplementTokens)
	}
	if result.TokensPerCompletedTask == nil || *result.TokensPerCompletedTask != 150 {
		t.Fatalf("tokens/completed = %v, want 150 (implement only)", result.TokensPerCompletedTask)
	}
}

func TestSpendSetBreakdownCountsTokenBlindRuns(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudeUsageEvents(10, 5))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "codex", base.Add(time.Minute), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","result":"ok"}`},
	})

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Rows[0]
	if row.RunCount != 2 || row.TokenBlindRuns != 1 {
		t.Fatalf("row counts = runs %d blind %d", row.RunCount, row.TokenBlindRuns)
	}
	if result.ImplementTokenBlindRuns != 1 {
		t.Fatalf("implement blind = %d", result.ImplementTokenBlindRuns)
	}
}

func TestRenderSpendSetBreakdownJSON(t *testing.T) {
	perTask := int64(150)
	result := &SpendSetBreakdownResult{
		TaskSetID:              "demo",
		CompletedTasks:         1,
		TokensPerCompletedTask: &perTask,
		ImplementTokens:        TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
		ImplementRunCount:      1,
		VerificationTokens:     TokenUsage{Input: 500, Output: 250, HasInput: true, HasOutput: true},
		VerificationRunCount:   1,
		Rows: []SpendBreakdownRow{{
			TaskID: "01-a", Title: "A",
			Tokens:   TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
			RunCount: 1,
		}, {
			TaskID: "verify", Title: "Verify",
			Tokens:   TokenUsage{Input: 500, Output: 250, HasInput: true, HasOutput: true},
			RunCount: 1,
		}},
	}
	var buf bytes.Buffer
	if err := RenderSpendSetBreakdownJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendSetBreakdownJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TaskSetID != "demo" || decoded.CompletedTasks != 1 || decoded.TokensPerCompletedTask == nil || *decoded.TokensPerCompletedTask != 150 {
		t.Fatalf("headline = %+v", decoded)
	}
	if decoded.ImplementInputTokens != 100 || decoded.VerificationInputTokens != 500 {
		t.Fatalf("scoped totals = implement %d verify %d", decoded.ImplementInputTokens, decoded.VerificationInputTokens)
	}
	if len(decoded.Rows) != 2 || decoded.Rows[1].TaskID != "verify" {
		t.Fatalf("rows = %#v", decoded.Rows)
	}
}

func TestRenderSpendSetBreakdownHuman(t *testing.T) {
	perTask := int64(150)
	result := &SpendSetBreakdownResult{
		TaskSetID:              "demo",
		CompletedTasks:         1,
		TokensPerCompletedTask: &perTask,
		ImplementTokens:        TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
		ImplementRunCount:      1,
		VerificationTokens:     TokenUsage{Input: 500, HasInput: true},
		VerificationRunCount:   1,
		Rows: []SpendBreakdownRow{{
			TaskID:   "01-a",
			Tokens:   TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
			RunCount: 1,
		}, {
			TaskID:   "verify",
			Tokens:   TokenUsage{Input: 500, HasInput: true},
			RunCount: 1,
		}},
	}
	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	for _, want := range []string{"tokens per completed task: 150 (—)", "verification spend: 500 (—)", "turns", "peak-in", "01-a", "verify", "blind"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSpendSetBreakdownRejectsTaskFileTarget(t *testing.T) {
	env := spendFixture(t)
	registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	_, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo/01-a.md",
	})
	if err == nil {
		t.Fatal("expected error for task file target")
	}
}

func piCostEvents(total float64) []streamEventRecord {
	return []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","usage":{"input":100,"output":50},"cost":{"total":%g,"input":0.03,"output":0.02}}}`, total)},
	}
}

func TestSpendRollupAggregatesPiPartialCost(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "pi", base, piCostEvents(0.05))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "pi", base.Add(time.Minute), piCostEvents(0.10))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(2*time.Minute), claudeUsageEvents(10, 5))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 1 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	row := result.Sets[0]
	if !row.Cost.HasCost {
		t.Fatalf("expected partial cost, got %+v", row.Cost)
	}
	if diff := row.Cost.Dollars - 0.15; diff > 0.0001 || diff < -0.0001 {
		t.Fatalf("partial cost = %+v, want 0.15 from pi runs only", row.Cost)
	}
	if !row.Notional.HasCost || row.Notional.Dollars < 0.149 || row.Notional.Dollars > 0.151 {
		t.Fatalf("notional should prefer pi measured 0.15, got %+v", row.Notional)
	}
	if row.RunCount != 3 {
		t.Fatalf("runs = %d", row.RunCount)
	}
}

func TestSpendRollupOmitsDollarWhenRateBlind(t *testing.T) {
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens: TokenUsage{
			Input: 100, Output: 50, HasInput: true, HasOutput: true,
		},
		RunCount: 1,
	}}}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "150 (—)") {
		t.Fatalf("expected rate-blind spend cell, got:\n%s", out)
	}
	if strings.Contains(out, "$") {
		t.Fatalf("expected no dollar figure when rate-blind, got:\n%s", out)
	}
}

func TestRenderSpendRollupShowsNotionalSpendCell(t *testing.T) {
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens: TokenUsage{
			Input: 100, Output: 50, HasInput: true, HasOutput: true,
		},
		Notional: PartialCost{Dollars: 0.1234, HasCost: true},
		RunCount: 1,
	}}}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	for _, want := range []string{"tokens", "150 (~$0.12)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderSpendRollupJSONPartialCostOmitempty(t *testing.T) {
	noCost := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens:    TokenUsage{Input: 1, HasInput: true},
		RunCount:  1,
	}}}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, noCost); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "partial_cost") {
		t.Fatalf("expected no partial_cost_usd when absent, got %s", buf.String())
	}

	withCost := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens:    TokenUsage{Input: 1, HasInput: true},
		Cost:      PartialCost{Dollars: 0.42, HasCost: true},
		RunCount:  1,
	}}}
	buf.Reset()
	if err := RenderSpendRollupJSON(&buf, withCost); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Sets[0].PartialCostUSD == nil || *decoded.Sets[0].PartialCostUSD != 0.42 {
		t.Fatalf("partial_cost_usd = %v", decoded.Sets[0].PartialCostUSD)
	}
}

func TestSpendSetBreakdownAggregatesPiPartialCost(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "pi", base, piCostEvents(0.08))
	writeSpendRunEx(t, setDir, "", "", "pi", base.Add(5*time.Minute), piCostEvents(0.02), spendRunOpts{phase: "verify"})

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ImplementCost.HasCost || result.ImplementCost.Dollars != 0.08 {
		t.Fatalf("implement cost = %+v", result.ImplementCost)
	}
	if !result.VerificationCost.HasCost || result.VerificationCost.Dollars != 0.02 {
		t.Fatalf("verification cost = %+v", result.VerificationCost)
	}
}

func TestRenderSpendSetBreakdownShowsNotionalSpendCell(t *testing.T) {
	perTask := int64(150)
	result := &SpendSetBreakdownResult{
		TaskSetID:              "demo",
		CompletedTasks:         1,
		TokensPerCompletedTask: &perTask,
		ImplementTokens:        TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
		ImplementNotional:      PartialCost{Dollars: 0.50, HasCost: true},
		ImplementRunCount:      1,
		VerificationTokens:     TokenUsage{Input: 500, HasInput: true},
		VerificationNotional:   PartialCost{Dollars: 0.25, HasCost: true},
		VerificationRunCount:   1,
		Rows: []SpendBreakdownRow{{
			TaskID:   "01-a",
			Tokens:   TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
			Notional: PartialCost{Dollars: 0.50, HasCost: true},
			RunCount: 1,
		}, {
			TaskID:   "verify",
			Tokens:   TokenUsage{Input: 500, HasInput: true},
			Notional: PartialCost{Dollars: 0.25, HasCost: true},
			RunCount: 1,
		}},
	}
	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	for _, want := range []string{"150 (~$0.50)", "500 (~$0.25)", "tokens"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSpendRollupAggregatesTurnsAndPeakInput(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudeTurnAndUsageEvents(3, 100, 50))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), claudeTurnAndUsageEvents(2, 50, 25))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if !row.Turns.HasTurn || row.Turns.Count != 5 {
		t.Fatalf("turns = %+v, want 5 reported", row.Turns)
	}
	if !row.PeakInput.HasPeak || row.PeakInput.Tokens != 33 {
		t.Fatalf("peak = %+v, want 33 reported", row.PeakInput)
	}
}

func TestSpendRollupTurnAndPeakBlindShowMarkerNotZero(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "codex", base, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","result":"ok"}`},
	})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if row.Turns.HasTurn {
		t.Fatalf("turns should be blind, got %+v", row.Turns)
	}
	if row.PeakInput.HasPeak {
		t.Fatalf("peak should be blind, got %+v", row.PeakInput)
	}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	if !strings.Contains(buf.String(), "—") {
		t.Fatalf("expected blind marker, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), " 0 ") {
		t.Fatalf("blind run must not show zero, got:\n%s", buf.String())
	}
}

func TestSpendRollupShowsAgentColumnOnlyWhenSetMixesAgents(t *testing.T) {
	single := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens:    TokenUsage{Input: 1, HasInput: true},
		Agents:    "claude",
		RunCount:  1,
	}}}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, single)
	if strings.Contains(buf.String(), "agent") {
		t.Fatalf("single-agent set should hide agent column:\n%s", buf.String())
	}

	mixed := &SpendRollupResult{
		ShowAgents: true,
		Sets: []SpendRollupRow{{
			TaskSetID: "mixed",
			Tokens:    TokenUsage{Input: 1, HasInput: true},
			Agents:    "claude,codex",
			RunCount:  2,
		}},
	}
	buf.Reset()
	RenderSpendRollup(&buf, mixed)
	if !strings.Contains(buf.String(), "agent") || !strings.Contains(buf.String(), "claude,codex") {
		t.Fatalf("mixed set should show agent column:\n%s", buf.String())
	}
}

func TestSpendSetBreakdownShowsAgentColumnOnlyWhenSetMixesAgents(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudeUsageEvents(10, 5))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "codex", base.Add(time.Minute), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","result":"ok"}`},
	})

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShowAgents {
		t.Fatal("expected ShowAgents for mixed-agent set")
	}
	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "agent") {
		t.Fatalf("mixed set should show agent column:\n%s", out)
	}
	if !strings.Contains(out, "claude,codex") {
		t.Fatalf("row should list both agents:\n%s", out)
	}

	singleDir := registerSpendSet(t, env, "2026-06-11-single", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, singleDir, "01-a.md", "01-a", "claude", base, claudeUsageEvents(10, 5))
	single, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-11-single",
	})
	if err != nil {
		t.Fatal(err)
	}
	if single.ShowAgents {
		t.Fatal("single-agent set should not show agent column")
	}
	buf.Reset()
	RenderSpendSetBreakdown(&buf, single)
	if strings.Contains(buf.String(), "agent") {
		t.Fatalf("single-agent set should hide agent column:\n%s", buf.String())
	}
}

func TestRenderSpendRollupJSONTurnAndPeakBlindExplicit(t *testing.T) {
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID:     "demo",
		Tokens:        TokenUsage{Input: 1, HasInput: true},
		TurnBlindRuns: 1,
		PeakBlindRuns: 1,
		RunCount:      1,
	}}}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	sets := raw["sets"].([]any)
	row := sets[0].(map[string]any)
	if row["turns"] != nil {
		t.Fatalf("turns = %v, want null for blind", row["turns"])
	}
	if row["peak_input_tokens"] != nil {
		t.Fatalf("peak_input_tokens = %v, want null for blind", row["peak_input_tokens"])
	}
	if row["turn_blind_runs"].(float64) != 1 || row["peak_blind_runs"].(float64) != 1 {
		t.Fatalf("blind counts = %+v", row)
	}
}

func TestRenderSpendRollupJSONReportsTurnsAndPeakWhenPresent(t *testing.T) {
	turns := 7
	peak := int64(39552)
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens:    TokenUsage{Input: 1, HasInput: true},
		Turns:     TurnCount{Count: turns, HasTurn: true},
		PeakInput: PeakInput{Tokens: peak, HasPeak: true},
		RunCount:  1,
	}}}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Sets[0]
	if got.Turns == nil || *got.Turns != turns {
		t.Fatalf("turns = %v, want %d", got.Turns, turns)
	}
	if got.PeakInputTokens == nil || *got.PeakInputTokens != peak {
		t.Fatalf("peak = %v, want %d", got.PeakInputTokens, peak)
	}
}

func TestSpendRollupSortsByNotionalCostDearestFirst(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	cheapDir := registerSpendSet(t, env, "2026-06-10-cheap", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, cheapDir, "01-a.md", "01-a", "claude", base.Add(time.Hour), claudePricedEvents(100, 0, 0, 0, "claude-opus-5"))

	dearDir := registerSpendSet(t, env, "2026-06-10-dear", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, dearDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(10000, 0, 0, 0, "claude-opus-5"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Sort:         SpendSortCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 2 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	if result.Sets[0].TaskSetID != "2026-06-10-dear" || result.Sets[1].TaskSetID != "2026-06-10-cheap" {
		t.Fatalf("order = %#v, want dearest first", []string{result.Sets[0].TaskSetID, result.Sets[1].TaskSetID})
	}
	if result.Sort != SpendSortCost {
		t.Fatalf("Sort = %q, want %q", result.Sort, SpendSortCost)
	}
}

func TestSpendRollupCostSortPutsRateBlindInTrailingBlock(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	blindDir := registerSpendSet(t, env, "2026-06-10-blind", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, blindDir, "01-a.md", "01-a", "claude", base.Add(2*time.Hour), claudePricedEvents(1, 0, 0, 0, "claude-does-not-exist"))

	midDir := registerSpendSet(t, env, "2026-06-10-mid", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, midDir, "01-a.md", "01-a", "claude", base.Add(time.Hour), claudePricedEvents(100, 0, 0, 0, "claude-opus-5"))

	dearDir := registerSpendSet(t, env, "2026-06-10-dear", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, dearDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(10000, 0, 0, 0, "claude-opus-5"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Sort:         SpendSortCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 3 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	got := []string{result.Sets[0].TaskSetID, result.Sets[1].TaskSetID, result.Sets[2].TaskSetID}
	want := []string{"2026-06-10-dear", "2026-06-10-mid", "2026-06-10-blind"}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
	if result.Sets[2].Notional.HasCost {
		t.Fatalf("trailing row should be rate-blind, got %+v", result.Sets[2].Notional)
	}

	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "(rate-blind)") {
		t.Fatalf("expected labelled rate-blind block:\n%s", out)
	}
	blindIdx := strings.Index(out, "(rate-blind)")
	rowIdx := strings.Index(out, "2026-06-10-blind")
	if blindIdx < 0 || rowIdx < 0 || blindIdx > rowIdx {
		t.Fatalf("label must precede the blind row:\n%s", out)
	}
	midIdx := strings.Index(out, "2026-06-10-mid")
	if midIdx > blindIdx {
		t.Fatalf("priceable rows must precede the rate-blind label:\n%s", out)
	}
}

func TestRenderSpendRollupJSONNotionalCostFields(t *testing.T) {
	priced := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID:      "priced",
		Tokens:         TokenUsage{Input: 10, HasInput: true},
		Notional:       PartialCost{Dollars: 1.25, HasCost: true},
		RateSource:     RateSourceTable,
		ModelKey:       "anthropic/claude-opus-5",
		ModelKeySource: RateKeyFromActual,
		RunCount:       1,
	}}}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, priced); err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if strings.Contains(raw, "~$") || strings.Contains(raw, "tokens (") {
		t.Fatalf("JSON must not carry a formatted spend cell: %s", raw)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Sets[0]
	if got.NotionalCostUSD == nil || *got.NotionalCostUSD != 1.25 {
		t.Fatalf("notional_cost_usd = %v", got.NotionalCostUSD)
	}
	if got.RateSource == nil || *got.RateSource != RateSourceTable {
		t.Fatalf("rate_source = %v", got.RateSource)
	}
	if got.ModelKey != "anthropic/claude-opus-5" {
		t.Fatalf("model_key = %q", got.ModelKey)
	}
	if got.ModelKeySource == nil || *got.ModelKeySource != string(RateKeyFromActual) {
		t.Fatalf("model_key_source = %v, want actual", got.ModelKeySource)
	}

	blind := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "blind",
		Tokens:    TokenUsage{Input: 10, HasInput: true},
		RunCount:  1,
	}}}
	buf.Reset()
	if err := RenderSpendRollupJSON(&buf, blind); err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(buf.Bytes(), &asMap); err != nil {
		t.Fatal(err)
	}
	sets, _ := asMap["sets"].([]any)
	row, _ := sets[0].(map[string]any)
	if _, ok := row["notional_cost_usd"]; ok {
		t.Fatalf("notional_cost_usd must be absent for rate-blind, got %s", buf.String())
	}
	if row["rate_source"] != nil {
		t.Fatalf("rate_source = %v, want null", row["rate_source"])
	}
	if row["model_key"] != "" {
		t.Fatalf("model_key = %v, want empty", row["model_key"])
	}
	if row["model_key_source"] != nil {
		t.Fatalf("model_key_source = %v, want null", row["model_key_source"])
	}
}

func TestSpendRollupJSONCarriesNotionalFromLivePricing(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-priced", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(1000, 0, 0, 0, "claude-opus-5"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Sets[0]
	if got.NotionalCostUSD == nil || *got.NotionalCostUSD <= 0 {
		t.Fatalf("notional_cost_usd = %v", got.NotionalCostUSD)
	}
	if got.RateSource == nil || *got.RateSource != RateSourceTable {
		t.Fatalf("rate_source = %v", got.RateSource)
	}
	if got.ModelKey != "anthropic/claude-opus-5" {
		t.Fatalf("model_key = %q", got.ModelKey)
	}
	if got.ModelKeySource == nil || *got.ModelKeySource != string(RateKeyFromActual) {
		t.Fatalf("model_key_source = %v, want actual", got.ModelKeySource)
	}
}

func TestSpendRollupShowsModelColumnOnlyWhenSetMixesModels(t *testing.T) {
	single := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens:    TokenUsage{Input: 1, HasInput: true},
		Models:    "anthropic/claude-opus-5",
		RunCount:  1,
	}}}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, single)
	if strings.Contains(buf.String(), "model") {
		t.Fatalf("single-model set should hide model column:\n%s", buf.String())
	}

	mixed := &SpendRollupResult{
		ShowModels: true,
		Sets: []SpendRollupRow{{
			TaskSetID: "mixed",
			Tokens:    TokenUsage{Input: 1, HasInput: true},
			Models:    "anthropic/claude-opus-5+",
			RunCount:  2,
		}},
	}
	buf.Reset()
	RenderSpendRollup(&buf, mixed)
	out := buf.String()
	if !strings.Contains(out, "model") || !strings.Contains(out, "anthropic/claude-opus-5+") {
		t.Fatalf("mixed set should show model column:\n%s", out)
	}
}

func TestSpendRollupSingleModelNamesModelWhenColumnVisible(t *testing.T) {
	result := &SpendRollupResult{
		ShowModels: true,
		Sets: []SpendRollupRow{
			{
				TaskSetID: "mixed",
				Project:   "pop",
				Tokens:    TokenUsage{Input: 1, HasInput: true},
				Models:    "anthropic/claude-opus-5+",
				RunCount:  2,
			},
			{
				TaskSetID: "single",
				Project:   "pop",
				Tokens:    TokenUsage{Input: 1, HasInput: true},
				Models:    "openai/gpt-5.6-sol",
				RunCount:  1,
			},
		},
	}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "openai/gpt-5.6-sol") {
		t.Fatalf("single-model row should name its model:\n%s", out)
	}
}

func TestSpendRollupMixedModelsShowsDominantWithMixMarker(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-mixed-models", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(10000, 0, 0, 0, "claude-opus-5"))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), claudePricedEvents(100, 0, 0, 0, "claude-sonnet-5"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShowModels {
		t.Fatal("expected ShowModels for mixed-model set")
	}
	row := result.Sets[0]
	if row.Models != "anthropic/claude-opus-5+" {
		t.Fatalf("models = %q, want dominant with mix marker", row.Models)
	}
}

func TestSpendRollupAllModelBlindShowsBlindMarker(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-blind-models", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRunEx(t, setDir, "01-a.md", "01-a", "codex", base, codexPricedEvents(100, 50), spendRunOpts{})
	writeSpendRunEx(t, setDir, "01-a.md", "01-a", "codex", base.Add(time.Minute), codexPricedEvents(10, 5), spendRunOpts{})

	blindOnly, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if blindOnly.Sets[0].Models != spendModelBlindKey {
		t.Fatalf("models = %q, want blind marker", blindOnly.Sets[0].Models)
	}

	mixedDir := registerSpendSet(t, env, "2026-06-10-mixed-with-blind", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, mixedDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(1000, 0, 0, 0, "claude-opus-5"))
	writeSpendRun(t, mixedDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), claudePricedEvents(100, 0, 0, 0, "claude-sonnet-5"))

	withBlind := &SpendRollupResult{
		ShowModels: true,
		Sets: []SpendRollupRow{
			{TaskSetID: "mixed", Project: "pop", Tokens: TokenUsage{Input: 1, HasInput: true}, Models: "anthropic/claude-opus-5+", RunCount: 2},
			blindOnly.Sets[0],
		},
	}
	withBlind.Sets[1].Project = "pop"
	var buf bytes.Buffer
	RenderSpendRollup(&buf, withBlind)
	if !strings.Contains(buf.String(), spendModelBlindKey) {
		t.Fatalf("all-blind row should render blind marker when column visible:\n%s", buf.String())
	}
}

func TestRenderSpendRollupJSONCarriesModelSplit(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-json-models", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(1000, 50, 0, 0, "claude-opus-5"))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), claudePricedEvents(100, 10, 0, 0, "claude-sonnet-5"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	sets := raw["sets"].([]any)
	row := sets[0].(map[string]any)
	models, ok := row["models"].(map[string]any)
	if !ok || len(models) != 2 {
		t.Fatalf("models = %#v, want two entries", row["models"])
	}
	opus := models["anthropic/claude-opus-5"].(map[string]any)
	if opus["input_tokens"].(float64) != 1000 || opus["output_tokens"].(float64) != 50 {
		t.Fatalf("opus split = %#v", opus)
	}
	sonnet := models["anthropic/claude-sonnet-5"].(map[string]any)
	if sonnet["input_tokens"].(float64) != 100 || sonnet["output_tokens"].(float64) != 10 {
		t.Fatalf("sonnet split = %#v", sonnet)
	}
}

func TestRenderSpendRollupJSONModelSplitForBlindRuns(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-json-blind", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRunEx(t, setDir, "01-a.md", "01-a", "codex", base, codexPricedEvents(100, 50), spendRunOpts{})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Sets[0].Models) != 1 {
		t.Fatalf("models = %#v", decoded.Sets[0].Models)
	}
	if _, ok := decoded.Sets[0].Models[spendModelBlindKey]; !ok {
		t.Fatalf("blind runs should land under blind key, got %#v", decoded.Sets[0].Models)
	}
}
