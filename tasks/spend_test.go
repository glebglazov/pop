package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
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
}

func TestSpendRollupSortsRowsByTotalTokens(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	lowDir := registerSpendSet(t, env, "2026-06-10-low", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, lowDir, "01-a.md", "01-a", "claude", base, []streamEventRecord{
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
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens: TokenUsage{
			Input: 10, Output: 5, CacheRead: 2, CacheWrite: 1,
			HasInput: true, HasOutput: true, HasCacheRead: true, HasCacheWrite: true,
		},
		RunCount:       3,
		TokenBlindRuns: 1,
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
	if got.TaskSetID != "demo" || got.InputTokens != 10 || got.OutputTokens != 5 ||
		got.CacheReadTokens != 2 || got.CacheWriteTokens != 1 ||
		got.RunCount != 3 || got.TokenBlindRuns != 1 {
		t.Fatalf("row = %+v", got)
	}
}

func TestRenderSpendRollupHumanTable(t *testing.T) {
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
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
	for _, want := range []string{"task set", "cache-r", "cache-w", "runs", "blind", "demo", "100", "50", "10", "2", "1"} {
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

func formatSpendSetID(n int) string {
	return fmt.Sprintf("2026-06-10-set-%02d", n)
}
