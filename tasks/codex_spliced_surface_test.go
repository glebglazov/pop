package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// codexSplicedEvents is a codex Captured run as the Rollout splice stores it:
// codex's own exec stream plus one token_count event per model call.
var codexSplicedEvents = []streamEventRecord{
	{Type: "event", AtMS: 10, Raw: `{"type":"thread.started","thread_id":"019fbc9c-cd9f-7492-ac30-0d532096742c"}`},
	{Type: "event", AtMS: 20, Raw: `{"type":"turn.started"}`},
	{Type: "event", AtMS: 30, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":9000,"cached_input_tokens":5000,"cache_write_input_tokens":0,"output_tokens":10},"model_context_window":258400}}`},
	{Type: "event", AtMS: 40, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":41000,"cached_input_tokens":38000,"cache_write_input_tokens":0,"output_tokens":20},"model_context_window":258400}}`},
	{Type: "event", AtMS: 50, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":17000,"cached_input_tokens":16000,"cache_write_input_tokens":0,"output_tokens":5},"model_context_window":258400}}`},
	{Type: "event", AtMS: 60, Raw: `{"type":"turn.completed","usage":{"input_tokens":67000,"cached_input_tokens":59000,"output_tokens":35}}`},
}

// codexUnsplicedEvents is the same run stored without its rollout — a pruned
// session or another machine.
var codexUnsplicedEvents = []streamEventRecord{
	{Type: "event", AtMS: 10, Raw: `{"type":"thread.started","thread_id":"019fbc9c-cd9f-7492-ac30-0d532096742c"}`},
	{Type: "event", AtMS: 20, Raw: `{"type":"turn.started"}`},
	{Type: "event", AtMS: 60, Raw: `{"type":"turn.completed","usage":{"input_tokens":67000,"cached_input_tokens":59000,"output_tokens":35}}`},
}

func TestCodexSplicedRunReadsRealTurnsAndPeakOnSpendSurface(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-08-18-codex", []Task{
		{ID: "01-spliced", File: "01-spliced.md", Title: "Spliced", Type: "AFK", Status: "done"},
		{ID: "02-unspliced", File: "02-unspliced.md", Title: "Unspliced", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-spliced.md", "01-spliced", "codex", base, codexSplicedEvents)
	writeSpendRun(t, setDir, "02-unspliced.md", "02-unspliced", "codex", base.Add(time.Minute), codexUnsplicedEvents)

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-08-18-codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %#v", result.Rows)
	}
	spliced, unspliced := result.Rows[0], result.Rows[1]
	if spliced.Turns.Count != 3 || !spliced.Turns.HasTurn || spliced.TurnBlindRuns != 0 {
		t.Fatalf("spliced turns = %+v blind %d, want 3 reported", spliced.Turns, spliced.TurnBlindRuns)
	}
	if !spliced.PeakInput.HasPeak || spliced.PeakInput.Tokens != 41000 || spliced.PeakBlindRuns != 0 {
		t.Fatalf("spliced peak = %+v blind %d, want 41000 reported", spliced.PeakInput, spliced.PeakBlindRuns)
	}
	if unspliced.Turns.HasTurn || unspliced.TurnBlindRuns != 1 {
		t.Fatalf("unspliced turns = %+v blind %d, want turn-blind", unspliced.Turns, unspliced.TurnBlindRuns)
	}
	if unspliced.PeakInput.HasPeak || unspliced.PeakBlindRuns != 1 {
		t.Fatalf("unspliced peak = %+v blind %d, want peak-blind", unspliced.PeakInput, unspliced.PeakBlindRuns)
	}
	// Run spend still comes from the turn.completed rollup on both runs.
	for _, row := range result.Rows {
		if row.Tokens.Input != 67000 || row.Tokens.CacheRead != 59000 || row.Tokens.Output != 35 {
			t.Fatalf("%s tokens = %+v, want the turn.completed rollup", row.TaskID, row.Tokens)
		}
	}

	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	for _, want := range []string{"41000", "—"} {
		if !strings.Contains(out, want) {
			t.Fatalf("breakdown missing %q:\n%s", want, out)
		}
	}
}

func TestCodexSplicedRunReadsRealTurnsAndPeakOnTraceSurface(t *testing.T) {
	header := streamHeaderRecord{Agent: "codex", Model: "gpt-5.5", StartTime: time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)}
	footer := streamFooterRecord{Outcome: "completed", DurationMS: 60}

	spliced := deriveAttemptTiming(header, footer, codexSplicedEvents)
	if spliced.Turns.Count != 3 || !spliced.Turns.HasTurn {
		t.Fatalf("spliced trace turns = %+v, want 3 reported", spliced.Turns)
	}
	if !spliced.PeakInput.HasPeak || spliced.PeakInput.Tokens != 41000 {
		t.Fatalf("spliced trace peak = %+v, want 41000 reported", spliced.PeakInput)
	}

	unspliced := deriveAttemptTiming(header, footer, codexUnsplicedEvents)
	if unspliced.Turns.HasTurn || unspliced.PeakInput.HasPeak {
		t.Fatalf("unspliced trace timing = turns %+v peak %+v, want blind", unspliced.Turns, unspliced.PeakInput)
	}
}
