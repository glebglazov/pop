package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// claudeMaxTurnsResultEvent is the terminal record a capped claude run ends on,
// measured 2026-08-06 from a live `claude --max-turns` run: an error subtype, a
// terminal reason naming the cap, no result text, and a turn count one above the
// cap it enforced.
func claudeMaxTurnsResultEvent(reportedTurns int) string {
	event, err := json.Marshal(map[string]any{
		"type":            "result",
		"subtype":         "error_max_turns",
		"terminal_reason": "max_turns",
		"is_error":        true,
		"result":          nil,
		"num_turns":       reportedTurns,
	})
	if err != nil {
		panic(err)
	}
	return string(event)
}

// claudeCapExhaustionRunner answers claude-preset invocations in process. The
// first exhaustAttempts implementation attempts write a file into the checkout
// and then end the way a capped claude run ends; the ones after that finish the
// task. Anything without an implementation prompt is a Verifier and passes.
type claudeCapExhaustionRunner struct {
	mu              sync.Mutex
	exhaustAttempts int
	reportedTurns   int
	// scratchFile is written into the runtime checkout before the agent runs out
	// of turns, so a test can prove the work survives the recorded outcome.
	scratchFile string

	implementPrompts []string
}

func (r *claudeCapExhaustionRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *claudeCapExhaustionRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	prompt := ""
	if len(args) > 0 {
		prompt = readSpilledPrompt(args[len(args)-1])
	}
	taskPath := parseFakeAgentTaskPath(prompt)
	if taskPath == "" {
		fmt.Fprintf(stdout, "%s\n", `{"type":"result","subtype":"success","result":"VERDICT: PASS"}`)
		return finishedProcess(0), nil
	}

	r.mu.Lock()
	r.implementPrompts = append(r.implementPrompts, prompt)
	attempt := len(r.implementPrompts)
	r.mu.Unlock()

	fmt.Fprintf(stdout, "%s\n", `{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Starting on the task."}]}}`)
	if attempt <= r.exhaustAttempts {
		if r.scratchFile != "" {
			if err := os.WriteFile(filepath.Join(dir, r.scratchFile), []byte("half-finished work\n"), 0o644); err != nil {
				return nil, err
			}
		}
		fmt.Fprintf(stdout, "%s\n", `{"type":"assistant","message":{"id":"msg_2","content":[{"type":"text","text":"Wrote the scaffold, still to wire it up."}]}}`)
		fmt.Fprintf(stdout, "%s\n", claudeMaxTurnsResultEvent(r.reportedTurns))
		return finishedProcess(1), nil
	}

	tickTaskFile(taskPath)
	result, err := json.Marshal(map[string]string{
		"type":    "result",
		"subtype": "success",
		"result":  "SUMMARY_START\nfinished what the capped attempt started\nSUMMARY_END\nTASK_COMPLETE",
	})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(stdout, "%s\n", result)
	return finishedProcess(0), nil
}

func finishedProcess(exitCode int) *ManagedProcess {
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	proc.done <- waitResult{exitCode: exitCode}
	return proc
}

// cappedDrainConfig declares a Turn cap for the drain's runtime checkout, which
// is what makes the attempt a capped one and gives the failure a number to name.
func cappedDrainConfig(t *testing.T, d *Deps, root string, turnCap int) *config.Config {
	t.Helper()
	runtimePath, err := ResolveRuntimePathWith(d, root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	return &config.Config{Repo: map[string]config.RepoOverrideConfig{
		runtimePath: {TurnCap: intPtr(turnCap)},
	}}
}

// TestDrainRecordsTurnCapExhaustionAndRetriesFromIt drives a whole drain whose
// first attempt ends at the repository's Turn cap: the attempt is persisted with
// its own terminal outcome, it spends a try, the retry is told it was cut short,
// and the half-finished work it left in the checkout is still there afterwards
// (ADR-0190 decision 6).
func TestDrainRecordsTurnCapExhaustionAndRetriesFromIt(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	d := env.deps()
	runner := &claudeCapExhaustionRunner{exhaustAttempts: 1, reportedTurns: 5, scratchFile: "scaffold.txt"}
	d.Runner = runner
	d.LookPath = func(file string) (string, error) { return filepath.Join("/usr/bin", file), nil }
	cfg := cappedDrainConfig(t, d, env.root, 4)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.AgentPreset = "claude"
	opts.MaxTries = 2
	opts.MaxTriesExplicit = true

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) { return cfg, nil }, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v\n%s", err, buf.String())
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone\n%s", result, buf.String())
	}
	if len(runner.implementPrompts) != 2 {
		t.Fatalf("implementation attempts = %d, want 2 (the capped one spends a try)\n%s", len(runner.implementPrompts), buf.String())
	}

	runs, err := listTaskRuns(d, filepath.Join(env.tasksDir, "demo"), "01-a.md")
	if err != nil {
		t.Fatal(err)
	}
	sortRunsChronologically(runs)
	if len(runs) != 2 {
		t.Fatalf("persisted runs = %d, want 2\n%s", len(runs), buf.String())
	}
	if got := runs[0].meta.Outcome; got != streamOutcomeTurnCapExhausted {
		t.Fatalf("first run outcome = %q, want %s", got, streamOutcomeTurnCapExhausted)
	}
	if got := runs[0].meta.Reason; got != "stopped at its 4-turn cap" {
		t.Fatalf("first run reason = %q, want the cap named", got)
	}
	if got := runs[1].meta.Outcome; got != streamOutcomeCompleted {
		t.Fatalf("second run outcome = %q, want %s", got, streamOutcomeCompleted)
	}

	// The retry reads the carry-forward digest, and what it needs from it is that
	// the previous attempt ran out of turns rather than out of ideas.
	retryPrompt := runner.implementPrompts[1]
	if !strings.Contains(retryPrompt, lessonTurnCapExhausted) {
		t.Fatalf("retry prompt does not say the previous attempt was cut short at its turn cap:\n%s", retryPrompt)
	}

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimePath, "scaffold.txt")); err != nil {
		t.Fatalf("work written before the cap was hit is gone: %v", err)
	}
}

// TestDrainFailsTheTaskWhenTheLastTryEndsAtTheTurnCap is the other end of the
// same path: the tries run out with every attempt cut short, and the task fails
// with the cap named as the reason.
func TestDrainFailsTheTaskWhenTheLastTryEndsAtTheTurnCap(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	d := env.deps()
	runner := &claudeCapExhaustionRunner{exhaustAttempts: 2, reportedTurns: 5, scratchFile: "scaffold.txt"}
	d.Runner = runner
	d.LookPath = func(file string) (string, error) { return filepath.Join("/usr/bin", file), nil }
	cfg := cappedDrainConfig(t, d, env.root, 4)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.AgentPreset = "claude"
	opts.MaxTries = 2
	opts.MaxTriesExplicit = true

	_, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) { return cfg, nil }, opts)
	if err == nil {
		t.Fatalf("drain succeeded, want a failed task\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "stopped at its 4-turn cap on attempt 2") {
		t.Fatalf("failure = %v, want the cap named as the reason", err)
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 2)

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimePath, "scaffold.txt")); err != nil {
		t.Fatalf("work written before the cap was hit is gone: %v", err)
	}
}

// TestTurnCapExhaustionIsItsOwnTerminalOutcome pins the outcome apart from every
// other ending an attempt can have — it is neither a failure nor a timeout nor an
// unusable agent, and it is emphatically not the Effort model skip it inverts.
func TestTurnCapExhaustionIsItsOwnTerminalOutcome(t *testing.T) {
	t.Parallel()
	for _, other := range []string{
		streamOutcomeCompleted,
		streamOutcomeFailed,
		streamOutcomeTimedOut,
		streamOutcomeInterrupted,
		streamOutcomeQuotaPaused,
		streamOutcomeAgentUnusable,
		streamOutcomeModelSkipped,
	} {
		if streamOutcomeTurnCapExhausted == other {
			t.Fatalf("turn-cap exhaustion shares the outcome %q", other)
		}
	}
}

// TestEveryAdapterDeclaresTurnCapExhaustionSeparatelyFromEnforcement pins
// ADR-0190 decision 3: both capabilities are declared by every preset and by the
// custom-command adapter, and they are two answers rather than one — claude can
// be told a cap and can be seen hitting it, while the rest say, in their own
// words, why neither is true of them.
func TestEveryAdapterDeclaresTurnCapExhaustionSeparatelyFromEnforcement(t *testing.T) {
	t.Parallel()
	adapters := map[string]AgentAdapter{"custom": customAgentAdapter{}}
	for _, preset := range agentCatalogOrder {
		adapter, err := ResolveAgentAdapter(preset)
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		adapters[preset] = adapter
	}

	for name, adapter := range adapters {
		enforcement := adapter.TurnCapEnforcementCapability()
		exhaustion := adapter.TurnCapExhaustionCapability()
		if err := enforcement.validate(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := exhaustion.validate(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "claude" {
			if enforcement.Kind != CapabilitySupported || exhaustion.Kind != CapabilitySupported {
				t.Fatalf("claude enforcement = %v, exhaustion = %v; want both Supported", enforcement.Kind, exhaustion.Kind)
			}
			continue
		}
		if exhaustion.Kind != CapabilityBlind {
			t.Fatalf("%s turn-cap exhaustion = %v, want Blind", name, exhaustion.Kind)
		}
		// The two blindnesses have different causes, so a shared sentence would
		// mean one of them was never actually decided.
		if exhaustion.Reason == enforcement.Reason {
			t.Fatalf("%s answers both turn-cap questions with one sentence: %q", name, exhaustion.Reason)
		}
	}
}

// TestTurnCapExhaustionIsReadFromTheCapturedEndingNotACount drives the
// recognition rule over real captured streams. It is exhaustion only when the
// agent's own terminal record says the cap ended the run *and* the process
// agreed by exiting non-zero: an ordinary run that crashed is not exhaustion, and
// neither is a capped-looking run that exited clean.
func TestTurnCapExhaustionIsReadFromTheCapturedEndingNotACount(t *testing.T) {
	t.Parallel()
	capped, err := loadStreamShapeFixture("claude", streamShapeTurnCapExhaustion)
	if err != nil {
		t.Fatalf("load capped fixture: %v", err)
	}
	if !attemptExhaustedTurnCap("claude", capped, 1) {
		t.Fatal("the captured capped ending was not recognised")
	}
	if attemptExhaustedTurnCap("claude", capped, 0) {
		t.Fatal("a clean exit was read as turn-cap exhaustion")
	}

	ordinary, err := loadStreamFixture("claude")
	if err != nil {
		t.Fatalf("load ordinary fixture: %v", err)
	}
	if attemptExhaustedTurnCap("claude", ordinary, 1) {
		t.Fatal("an ordinary run that exited non-zero was read as turn-cap exhaustion")
	}

	// The count is never the evidence: strip the terminal record's cap markers and
	// leave its num_turns above every cap, and the run reads as an ordinary
	// non-zero exit again.
	countOnly := make([]streamEventRecord, len(capped))
	copy(countOnly, capped)
	last := &countOnly[len(countOnly)-1]
	var event map[string]any
	if err := json.Unmarshal([]byte(last.Raw), &event); err != nil {
		t.Fatal(err)
	}
	if event["subtype"] != claudeMaxTurnsSubtype || event["terminal_reason"] != claudeMaxTurnsTerminalReason {
		t.Fatalf("fixture's terminal record does not carry the measured cap markers: %v", event)
	}
	delete(event, "subtype")
	delete(event, "terminal_reason")
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	last.Raw = string(raw)
	if attemptExhaustedTurnCap("claude", countOnly, 1) {
		t.Fatal("exhaustion was inferred from a turn count")
	}

	// A Blind adapter recognises nothing, even handed the same ending.
	if attemptExhaustedTurnCap("kimi", capped, 1) {
		t.Fatal("a Blind adapter claimed to recognise a capped ending")
	}
}

// TestCapExhaustedRunKeepsPopsOwnTurnCount pins ADR-0190 decision 7. claude
// reports one turn more than the cap it enforced; pop's Turn is pop's own
// measurement of the Captured run, and the two numbers are deliberately left
// unreconciled. Nobody should "fix" this into agreement.
func TestCapExhaustedRunKeepsPopsOwnTurnCount(t *testing.T) {
	const (
		enforcedCap    = 3
		claudeReported = enforcedCap + 1 // the measured off-by-one
	)

	events, err := loadStreamShapeFixture("claude", streamShapeTurnCapExhaustion)
	if err != nil {
		t.Fatalf("load capped fixture: %v", err)
	}
	var terminal struct {
		NumTurns int `json:"num_turns"`
	}
	if err := json.Unmarshal([]byte(events[len(events)-1].Raw), &terminal); err != nil {
		t.Fatal(err)
	}
	if terminal.NumTurns != claudeReported {
		t.Fatalf("fixture reports num_turns %d, want the measured %d", terminal.NumTurns, claudeReported)
	}

	env := streamFixture(t)
	d := env.deps()
	sel := &Selection{
		TaskSetID: "demo",
		TaskID:    "01-a",
		TaskFile:  "01-a.md",
		Manifest:  &Manifest{Dir: env.demoDir()},
	}
	start := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	rec := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	for _, ev := range events {
		if _, err := rec.Write([]byte(ev.Raw + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	rec.finish()
	if p := persistAttemptStream(d, io.Discard, sel, rec, "claude", "claude", 1, streamOutcomeTurnCapExhausted, "stopped at its 3-turn cap", 1); p == "" {
		t.Fatal("the cap-exhausted attempt persisted nothing")
	}

	result, err := StreamWith(d, nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 || len(result.Tasks[0].Attempts) != 1 {
		t.Fatalf("stream result = %#v", result.Tasks)
	}
	timing := result.Tasks[0].Attempts[0].Timing
	if timing.Outcome != streamOutcomeTurnCapExhausted {
		t.Fatalf("outcome = %q, want %s", timing.Outcome, streamOutcomeTurnCapExhausted)
	}
	if !timing.Turns.HasTurn || timing.Turns.Count != enforcedCap {
		t.Fatalf("persisted turns = %+v, want pop's own count of %d", timing.Turns, enforcedCap)
	}
	if timing.Turns.Count == terminal.NumTurns {
		t.Fatalf("pop's turn count was reconciled with claude's reported %d", terminal.NumTurns)
	}
}
