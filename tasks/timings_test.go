package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTimingStream writes one gzipped Captured attempt stream file with the
// given header and footer, mirroring what persistAttemptStream stores.
func writeTimingStream(t *testing.T, dir, name, agent string, attempt int, start time.Time, outcome string, durationMS int64) {
	t.Helper()
	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	if err := enc.Encode(streamHeaderRecord{Type: "header", Agent: agent, Attempt: attempt, StartTime: start.UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(streamEventRecord{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(streamFooterRecord{Type: "footer", Outcome: outcome, DurationMS: durationMS}); err != nil {
		t.Fatal(err)
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func timingsFixture(t *testing.T) *execFixture {
	t.Helper()
	return setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
}

func TestTimingsWholeSetSpansAttemptsOrderedByStartTime(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	// Attempts from separate invocations and reopens: numbering has gaps and
	// the on-disk order (by NNN) disagrees with start-time order.
	writeTimingStream(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base.Add(10*time.Minute), "failed", 45_000)
	writeTimingStream(t, streamDir, "attempt-002.jsonl.gz", "claude", 2, base, "timed_out", 83_000)
	writeTimingStream(t, streamDir, "attempt-007.jsonl.gz", "codex", 7, base.Add(20*time.Minute), "completed", 500)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" {
		t.Fatalf("task set = %q", result.TaskSetID)
	}
	if len(result.Tasks) != 2 || result.Tasks[0].TaskID != "01-a" || result.Tasks[1].TaskID != "02-b" {
		t.Fatalf("tasks = %#v", result.Tasks)
	}

	attempts := result.Tasks[0].Attempts
	if len(attempts) != 3 {
		t.Fatalf("attempts = %d, want all stored attempts", len(attempts))
	}
	wantOrder := []AttemptTiming{
		{Agent: "claude", Start: base, Outcome: "timed_out", Duration: 83 * time.Second},
		{Agent: "claude", Start: base.Add(10 * time.Minute), Outcome: "failed", Duration: 45 * time.Second},
		{Agent: "codex", Start: base.Add(20 * time.Minute), Outcome: "completed", Duration: 500 * time.Millisecond},
	}
	for i, want := range wantOrder {
		got := attempts[i]
		if got.Agent != want.Agent || !got.Start.Equal(want.Start) || got.Outcome != want.Outcome || got.Duration != want.Duration {
			t.Fatalf("attempt[%d] = %+v, want %+v", i, got, want)
		}
	}

	if len(result.Tasks[1].Attempts) != 0 {
		t.Fatalf("02-b attempts = %#v, want none", result.Tasks[1].Attempts)
	}
}

func TestTimingsSingleTaskTarget(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/02-b.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].TaskID != "02-b" {
		t.Fatalf("tasks = %#v, want only 02-b", result.Tasks)
	}
	if len(result.Tasks[0].Attempts) != 1 || result.Tasks[0].Attempts[0].Outcome != "failed" {
		t.Fatalf("attempts = %#v", result.Tasks[0].Attempts)
	}
}

func TestTimingsRejectsInvalidTargets(t *testing.T) {
	env := timingsFixture(t)

	cases := map[string]struct {
		target string
		code   int
	}{
		"unknown set":       {"missing", ExitNoRunnable},
		"unknown task file": {"demo/99-z.md", ExitNoRunnable},
		"bare filename":     {"01-a.md", ExitSetup},
		"absolute path":     {filepath.Join(env.demoDir(), "01-a.md"), ExitSetup},
		"relative path":     {"./demo", ExitSetup},
		"empty after trim":  {"   ", ExitSetup},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
				ResolveInput: ResolveInput{CWD: env.root},
				Target:       tc.target,
			})
			assertExitCode(t, err, tc.code)
		})
	}
}

func TestRenderTimingsShowsRowsWithoutOrdinals(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &TimingsResult{
		TaskSetID: "demo",
		Tasks: []TaskTimings{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptTiming{
					{Agent: "claude", Start: base, Outcome: "timed_out", Duration: 83 * time.Second},
					{Agent: "codex", Start: base.Add(10 * time.Minute), Outcome: "completed", Duration: 500 * time.Millisecond},
				},
			},
			{TaskID: "02-b", File: "02-b.md", Title: "B"},
		},
	}

	var buf bytes.Buffer
	RenderTimings(&buf, result)
	out := buf.String()

	for _, want := range []string{
		"demo/01-a.md  A",
		"2026-06-10T12:00:00Z  claude  timed_out  1m23s",
		"2026-06-10T12:10:00Z  codex   completed  500ms",
		"demo/02-b.md  B",
		"no recorded attempts",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	// Rows are keyed by start time only: the earlier attempt renders first and
	// no ordinal appears that could contradict the executor's "Attempt N/max".
	if strings.Index(out, "12:00:00Z") > strings.Index(out, "12:10:00Z") {
		t.Fatalf("attempts not ordered by start time:\n%s", out)
	}
	for _, forbidden := range []string{"attempt-", "Attempt 1", "#1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("output shows ordinal %q:\n%s", forbidden, out)
		}
	}
}

func TestReadAttemptTimingRejectsTruncatedStream(t *testing.T) {
	env := timingsFixture(t)
	dir := taskStreamDir(env.demoDir(), "01-a.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid gzip whose JSONL lacks a footer.
	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	if err := enc.Encode(streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "attempt-001.jsonl.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "missing footer") {
		t.Fatalf("err = %v", err)
	}
}
