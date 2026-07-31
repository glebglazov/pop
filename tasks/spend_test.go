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

type spendRunOpts struct {
	phase   string
	attempt int
	outcome string
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
		RunID:     runID,
		Phase:     opts.phase,
		TaskSetID: filepath.Base(taskSetDir),
		TaskID:    taskID,
		TaskFile:  taskFile,
		StartTime: start.UTC(),
		EndTime:   start.Add(time.Minute).UTC(),
		Outcome:   opts.outcome,
		Agent:     agent,
		Attempt:   opts.attempt,
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
		TaskSetID:      "demo",
		CompletedTasks: 1,
		TokensPerCompletedTask: &perTask,
		ImplementTokens: TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
		ImplementRunCount: 1,
		VerificationTokens: TokenUsage{Input: 500, Output: 250, HasInput: true, HasOutput: true},
		VerificationRunCount: 1,
		Rows: []SpendBreakdownRow{{
			TaskID: "01-a", Title: "A",
			Tokens: TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
			RunCount: 1,
		}, {
			TaskID: "verify", Title: "Verify",
			Tokens: TokenUsage{Input: 500, Output: 250, HasInput: true, HasOutput: true},
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
			TaskID: "01-a",
			Tokens: TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
			RunCount: 1,
		}, {
			TaskID: "verify",
			Tokens: TokenUsage{Input: 500, HasInput: true},
			RunCount: 1,
		}},
	}
	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	for _, want := range []string{"tokens per completed task: 150", "verification spend: 500", "01-a", "verify", "blind"} {
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
	if row.RunCount != 3 {
		t.Fatalf("runs = %d", row.RunCount)
	}
}

func TestSpendRollupOmitsCostColumnWhenNoAdapterReportsIt(t *testing.T) {
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
	if strings.Contains(out, "cost") || strings.Contains(out, "$") {
		t.Fatalf("expected no cost column when absent, got:\n%s", out)
	}
}

func TestRenderSpendRollupShowsPartialCostLabel(t *testing.T) {
	result := &SpendRollupResult{Sets: []SpendRollupRow{{
		TaskSetID: "demo",
		Tokens: TokenUsage{
			Input: 100, Output: 50, HasInput: true, HasOutput: true,
		},
		Cost:     PartialCost{Dollars: 0.1234, HasCost: true},
		RunCount: 1,
	}}}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	for _, want := range []string{"cost (partial)", "$0.1234"} {
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

func TestRenderSpendSetBreakdownShowsPartialCostLabel(t *testing.T) {
	perTask := int64(150)
	result := &SpendSetBreakdownResult{
		TaskSetID:              "demo",
		CompletedTasks:         1,
		TokensPerCompletedTask: &perTask,
		ImplementTokens:        TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
		ImplementCost:          PartialCost{Dollars: 0.50, HasCost: true},
		ImplementRunCount:      1,
		VerificationTokens:     TokenUsage{Input: 500, HasInput: true},
		VerificationCost:         PartialCost{Dollars: 0.25, HasCost: true},
		VerificationRunCount:   1,
		Rows: []SpendBreakdownRow{{
			TaskID: "01-a",
			Tokens: TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true},
			Cost:   PartialCost{Dollars: 0.50, HasCost: true},
			RunCount: 1,
		}, {
			TaskID: "verify",
			Tokens: TokenUsage{Input: 500, HasInput: true},
			Cost:   PartialCost{Dollars: 0.25, HasCost: true},
			RunCount: 1,
		}},
	}
	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	for _, want := range []string{"partial cost", "cost (partial)", "$0.5000", "$0.2500"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
