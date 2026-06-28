package cmd

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/monitor"
)

// writeRecord captures one daemon-side Topic write for assertions.
type writeRecord struct {
	PaneID string
	Topic  string
	Kind   string
}

// recordingWriter returns a topicDerivationWriter that appends each write to
// *recs under a mutex. Tests read recs to assert what the daemon persisted.
func recordingWriter(recs *[]writeRecord, mu *sync.Mutex) topicDerivationWriter {
	return func(paneID, topic, kind string) error {
		mu.Lock()
		*recs = append(*recs, writeRecord{paneID, topic, kind})
		mu.Unlock()
		return nil
	}
}

// newTestDispatcher builds a topicDerivationDispatcher with injected stubs so
// tests can observe recipe execution, cancellation, and writes without tmux or
// real CLIs.
func newTestDispatcher(loadCfg func() *config.Config, lookup topicStateLookup, hasNote paneNoteLookup, run topicRecipeRunner, write topicDerivationWriter) *topicDerivationDispatcher {
	return &topicDerivationDispatcher{
		latest:  map[string]uint64{},
		cancels: map[string]context.CancelFunc{},
		loadCfg: loadCfg,
		lookup:  lookup,
		hasNote: hasNote,
		run:     run,
		write:   write,
	}
}

// alwaysAgentStep is a single agent step with set_if = "always" — the
// regeneration guard that re-derives on every prompt regardless of kind.
func alwaysAgentStep(command string) config.TopicStep {
	return config.TopicStep{Type: config.TopicStepAgent, Command: command, SetIf: config.TopicSetIfAlways}
}

// TestDispatcherDaemon_WritesFinalFirstNonEmptyWins locks the daemon-side agent
// phase (ADR 0068): it re-reads the pane state, runs the configured agent steps
// against the current kind, and writes the first non-empty result as final. A
// failing/empty first step falls through to the next; a first success stops the
// chain (later steps do not run).
func TestDispatcherDaemon_WritesFinalFirstNonEmptyWins(t *testing.T) {
	t.Run("first non-empty result is written as final and stops the chain", func(t *testing.T) {
		var mu sync.Mutex
		var recs []writeRecord
		var ran []string
		var ranMu sync.Mutex
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			ranMu.Lock()
			ran = append(ran, argv[0])
			ranMu.Unlock()
			if argv[0] == "ollama" {
				return "auth-refactor", nil
			}
			return "", nil // claude produces nothing usable
		}
		cfg := withTopicSteps(
			config.TopicStep{Type: config.TopicStepAgent, Command: "claude", SetIf: config.TopicSetIfEmpty},
			config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfEmpty},
		)
		disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))
		disp.Enqueue(topicDeriveJob{PaneID: "%5", Prompt: "refactor auth"})

		waitForWrites(t, &mu, &recs, 1, 2*time.Second)
		ranMu.Lock()
		got := ran
		ranMu.Unlock()
		// claude ran and produced nothing; ollama ran and won; no further step.
		if len(got) != 2 || got[0] != "claude" || got[1] != "ollama" {
			t.Errorf("ran = %v, want [claude ollama]", got)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 1 || recs[0] != (writeRecord{"%5", "auth-refactor", "final"}) {
			t.Errorf("writes = %v, want one final auth-refactor write on pane 5", recs)
		}
	})

	t.Run("first success stops the chain — later steps do not run", func(t *testing.T) {
		var mu sync.Mutex
		var recs []writeRecord
		ran := map[string]bool{}
		var ranMu sync.Mutex
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			ranMu.Lock()
			ran[argv[0]] = true
			ranMu.Unlock()
			if argv[0] == "ollama" {
				return "first-wins", nil // ollama's parse is plain text, so this lands
			}
			return "should-not-win", nil // claude must never be reached
		}
		cfg := withTopicSteps(
			config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfAlways},
			config.TopicStep{Type: config.TopicStepAgent, Command: "claude", SetIf: config.TopicSetIfAlways},
		)
		disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))
		disp.Enqueue(topicDeriveJob{PaneID: "%5", Prompt: "refactor auth"})

		waitForWrites(t, &mu, &recs, 1, 2*time.Second)
		ranMu.Lock()
		if ran["claude"] {
			t.Error("claude must not run after ollama succeeds (first non-empty stops the chain)")
		}
		ranMu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		if recs[0] != (writeRecord{"%5", "first-wins", "final"}) {
			t.Errorf("writes = %v, want first-wins", recs)
		}
	})

	t.Run("a failing first step falls through to the next non-empty", func(t *testing.T) {
		var mu sync.Mutex
		var recs []writeRecord
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			switch argv[0] {
			case "claude":
				return "", context.DeadlineExceeded // reason-blind fallthrough
			case "ollama":
				return "local-topic", nil
			}
			return "", nil
		}
		cfg := withTopicSteps(
			config.TopicStep{Type: config.TopicStepAgent, Command: "claude", SetIf: config.TopicSetIfAlways},
			config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfAlways},
		)
		disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))
		disp.Enqueue(topicDeriveJob{PaneID: "%9", Prompt: "fix the build"})

		waitForWrites(t, &mu, &recs, 1, 2*time.Second)
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 1 || recs[0].Topic != "local-topic" || recs[0].Kind != "final" {
			t.Errorf("writes = %v, want one final local-topic", recs)
		}
	})

	t.Run("all steps produce nothing → no write", func(t *testing.T) {
		var mu sync.Mutex
		var recs []writeRecord
		run := func(context.Context, []string, []byte) (string, error) { return "   \n", nil }
		cfg := withTopicSteps(alwaysAgentStep("ollama"))
		disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))
		disp.Enqueue(topicDeriveJob{PaneID: "%1", Prompt: "anything"})

		// Give the background goroutine time to run and conclude nothing.
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 0 {
			t.Errorf("writes = %v, want none for all-empty recipe output", recs)
		}
	})
}

// TestDispatcherDaemon_NoteSkipsAgentDerivation locks "a pane with a Note
// triggers no agent derivation" at the daemon: even when a derive request is
// enqueued, no recipe runs and nothing is written.
func TestDispatcherDaemon_NoteSkipsAgentDerivation(t *testing.T) {
	var mu sync.Mutex
	var recs []writeRecord
	run := func(context.Context, []string, []byte) (string, error) {
		t.Error("recipe must not run when the pane has a Note")
		return "", nil
	}
	cfg := withTopicSteps(alwaysAgentStep("ollama"))
	disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, paneHasNote, run, recordingWriter(&recs, &mu))
	disp.Enqueue(topicDeriveJob{PaneID: "%7", Prompt: "anything"})

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 0 {
		t.Errorf("writes = %v, want none when the pane has a Note", recs)
	}
}

// TestDispatcherDaemon_GatingHonorsKind confirms the daemon re-reads the pane's
// current @pop_topic_kind and gates agent steps against it: an empty/empty_or_seed
// step does not run against a final Topic (the seed-then-final guard from ADR 0068),
// while set_if="always" runs regardless.
func TestDispatcherDaemon_GatingHonorsKind(t *testing.T) {
	t.Run("empty_or_seed does not run against a final Topic", func(t *testing.T) {
		var mu sync.Mutex
		var recs []writeRecord
		run := func(context.Context, []string, []byte) (string, error) {
			t.Error("agent step must not run against a final Topic with empty_or_seed")
			return "", nil
		}
		lookup := func(string) (string, string, string) { return "existing", config.TopicKindFinal, "sess" }
		cfg := withTopicSteps(config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfEmptyOrSeed})
		disp := newTestDispatcher(func() *config.Config { return cfg }, lookup, noPaneNote, run, recordingWriter(&recs, &mu))
		disp.Enqueue(topicDeriveJob{PaneID: "%3", Prompt: "new prompt"})

		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 0 {
			t.Errorf("writes = %v, want none: final Topic blocks empty_or_seed", recs)
		}
	})

	t.Run("always runs and overwrites a final Topic", func(t *testing.T) {
		var mu sync.Mutex
		var recs []writeRecord
		run := func(context.Context, []string, []byte) (string, error) { return "regenerated", nil }
		lookup := func(string) (string, string, string) { return "old-final", config.TopicKindFinal, "sess" }
		cfg := withTopicSteps(alwaysAgentStep("ollama"))
		disp := newTestDispatcher(func() *config.Config { return cfg }, lookup, noPaneNote, run, recordingWriter(&recs, &mu))
		disp.Enqueue(topicDeriveJob{PaneID: "%3", Prompt: "new prompt"})

		waitForWrites(t, &mu, &recs, 1, 2*time.Second)
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 1 || recs[0] != (writeRecord{"%3", "regenerated", "final"}) {
			t.Errorf("writes = %v, want regenerated final overwriting the old final", recs)
		}
	})
}

// TestDispatcherDaemon_EnqueueReturnsImmediately locks "enqueue/return": the
// dispatcher's Enqueue returns before the (potentially slow) recipe finishes —
// the prompt submit is never blocked by a model call. The recipe runs on a
// background goroutine and writes the final Topic when it lands.
func TestDispatcherDaemon_EnqueueReturnsImmediately(t *testing.T) {
	var mu sync.Mutex
	var recs []writeRecord
	gate := make(chan struct{})
	started := make(chan struct{})
	run := func(_ context.Context, _ []string, _ []byte) (string, error) {
		started <- struct{}{}
		<-gate // simulate a slow model call
		return "async-topic", nil
	}
	cfg := withTopicSteps(alwaysAgentStep("ollama"))
	disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))

	disp.Enqueue(topicDeriveJob{PaneID: "%5", Prompt: "alpha"})

	// The recipe started running in the background…
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("recipe did not start")
	}
	// …but no write has happened yet (the recipe is still blocked), and
	// Enqueue has already returned — the hook is unblocked.
	mu.Lock()
	if len(recs) != 0 {
		t.Errorf("writes = %v, want none before the recipe finishes", recs)
	}
	mu.Unlock()

	close(gate)
	waitForWrites(t, &mu, &recs, 1, 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if recs[0] != (writeRecord{"%5", "async-topic", "final"}) {
		t.Errorf("writes = %v, want one final async-topic after the recipe lands", recs)
	}
}

// TestDispatcherDaemon_SingleFlightSupersede locks per-pane single-flight
// (ADR 0068): a newer prompt for the same pane supersedes an in-flight
// derivation — the older one's context is cancelled (so a real exec would be
// killed) and its eventual write is dropped, so only the newest result lands.
func TestDispatcherDaemon_SingleFlightSupersede(t *testing.T) {
	var mu sync.Mutex
	var recs []writeRecord
	var ranOrderMu sync.Mutex
	ranOrder := []string{}

	gate1 := make(chan struct{})
	started1 := make(chan struct{})
	var firstCtxCancelled atomic.Bool

	run := func(ctx context.Context, argv []string, stdin []byte) (string, error) {
		s := string(stdin)
		switch {
		case strings.Contains(s, "alpha prompt"):
			ranOrderMu.Lock()
			ranOrder = append(ranOrder, "alpha-start")
			ranOrderMu.Unlock()
			started1 <- struct{}{}
			go func() { <-ctx.Done(); firstCtxCancelled.Store(true) }()
			<-gate1 // first derivation stays in flight until released
			ranOrderMu.Lock()
			ranOrder = append(ranOrder, "alpha-finish")
			ranOrderMu.Unlock()
			return "topic-alpha", nil
		case strings.Contains(s, "beta prompt"):
			ranOrderMu.Lock()
			ranOrder = append(ranOrder, "beta")
			ranOrderMu.Unlock()
			return "topic-beta", nil
		}
		return "", nil
	}
	cfg := withTopicSteps(alwaysAgentStep("ollama"))
	disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))

	// 1. Enqueue the first derivation; it enters the recipe and blocks.
	disp.Enqueue(topicDeriveJob{PaneID: "%5", Prompt: "alpha prompt"})
	select {
	case <-started1:
	case <-time.After(2 * time.Second):
		t.Fatal("first recipe did not start")
	}

	// 2. A newer prompt supersedes it. Its context is cancelled (verified
	//    below) and its result will be dropped by the generation guard.
	disp.Enqueue(topicDeriveJob{PaneID: "%5", Prompt: "beta prompt"})

	// The newer (beta) derivation should write its result; the older (alpha)
	// derivation is in flight and will be dropped when it finishes.
	waitForWrites(t, &mu, &recs, 1, 2*time.Second)

	// The newer derivation cancelled the older's context (killing a real
	// exec promptly). Wait briefly for the cancels propagation to register.
	if !waitUntil(2*time.Second, func() bool { return firstCtxCancelled.Load() }) {
		t.Fatal("the superseded derivation's context was not cancelled")
	}

	// 3. Release the older derivation. It computes topic-alpha, but the
	//    generation guard must drop the write — only the newest may persist.
	close(gate1)
	if !waitUntil(time.Second, func() bool {
		ranOrderMu.Lock()
		defer ranOrderMu.Unlock()
		return len(ranOrder) == 3 && ranOrder[2] == "alpha-finish"
	}) {
		ranOrderMu.Lock()
		t.Fatalf("older derivation never finished: %v", ranOrder)
	}

	// Give the dropped write a moment to (not) happen.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 1 || recs[0] != (writeRecord{"%5", "topic-beta", "final"}) {
		t.Errorf("writes = %v, want only the newest (topic-beta) to persist", recs)
	}
}

// TestDispatcherDaemon_SingleFlightAcrossPanes confirms single-flight is
// per-pane: derivations for different panes run concurrently; one does not
// cancel the other.
func TestDispatcherDaemon_SingleFlightAcrossPanes(t *testing.T) {
	var mu sync.Mutex
	var recs []writeRecord
	gate := make(chan struct{})
	startA := make(chan struct{})
	startB := make(chan struct{})
	run := func(_ context.Context, _ []string, stdin []byte) (string, error) {
		// The model prompt on stdin carries the job's prompt text; branch on it
		// to give each pane a distinct topic and a distinct start signal.
		s := string(stdin)
		switch {
		case strings.Contains(s, "pane a"):
			startA <- struct{}{}
			<-gate
			return "topic-a", nil
		case strings.Contains(s, "pane b"):
			startB <- struct{}{}
			<-gate
			return "topic-b", nil
		}
		return "", nil
	}
	cfg := withTopicSteps(alwaysAgentStep("ollama"))
	disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))

	disp.Enqueue(topicDeriveJob{PaneID: "%A", Prompt: "pane a"})
	disp.Enqueue(topicDeriveJob{PaneID: "%B", Prompt: "pane b"})

	// Both derivations started (concurrent across panes) — neither cancelled.
	for _, ch := range []chan struct{}{startA, startB} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("a cross-pane derivation did not start")
		}
	}
	close(gate)
	waitForWrites(t, &mu, &recs, 2, 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	got := map[string]string{}
	for _, w := range recs {
		got[w.PaneID] = w.Topic
	}
	if got["%A"] != "topic-a" || got["%B"] != "topic-b" {
		t.Errorf("writes = %v, want topic-a and topic-b for the two panes", recs)
	}
}

// TestHandleDeriveTopic_ReturnsImmediately confirms the daemon handler returns
// OK right after enqueuing — it does not wait for the agent recipe to finish.
// This is the daemon-side half of "agent steps run on the daemon, not in the
// hook": the hook's socket send returns instantly, and the work proceeds in
// the background.
func TestHandleDeriveTopic_ReturnsImmediately(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{})
	run := func(_ context.Context, _ []string, _ []byte) (string, error) {
		started <- struct{}{}
		<-gate
		return "topic", nil
	}
	var mu sync.Mutex
	var recs []writeRecord
	cfg := withTopicSteps(alwaysAgentStep("ollama"))
	disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))

	done := make(chan struct{})
	go func() {
		resp := handleDeriveTopic(disp, monitor.Request{Cmd: "derive-topic", PaneID: "%5", Prompt: "alpha"})
		if !resp.OK {
			t.Errorf("handleDeriveTopic returned !OK: %s", resp.Error)
		}
		close(done)
	}()

	select {
	case <-done:
		// Handler returned before the recipe finished — instant enqueue.
	case <-time.After(2 * time.Second):
		t.Fatal("handleDeriveTopic blocked on the recipe")
	}

	// The recipe is running in the background now.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("recipe did not start after handleDeriveTopic returned")
	}
	close(gate)
	waitForWrites(t, &mu, &recs, 1, 2*time.Second)
}

// TestHandleDeriveTopic_NoopOnEmpty confirms a derive request with no pane id
// or empty prompt is a silent no-op (nothing enqueued, no recipe run).
func TestHandleDeriveTopic_NoopOnEmpty(t *testing.T) {
	run := func(context.Context, []string, []byte) (string, error) {
		t.Error("recipe must not run for an empty derive request")
		return "", nil
	}
	var mu sync.Mutex
	var recs []writeRecord
	cfg := withTopicSteps(alwaysAgentStep("ollama"))
	disp := newTestDispatcher(func() *config.Config { return cfg }, noTopicState, noPaneNote, run, recordingWriter(&recs, &mu))

	for _, req := range []monitor.Request{
		{Cmd: "derive-topic", PaneID: "", Prompt: "prompt"},
		{Cmd: "derive-topic", PaneID: "%5", Prompt: "  "},
	} {
		resp := handleDeriveTopic(disp, req)
		if !resp.OK {
			t.Errorf("expected OK for empty derive request, got %q", resp.Error)
		}
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 0 {
		t.Errorf("writes = %v, want none for empty derive requests", recs)
	}
}

// TestDeriveTopicSeedWith_TruncateOnlyWritesSeedReturnsImmediately locks the
// hook phase for the default config: the truncate step runs synchronously and
// writes the seed, the hook does not enqueue agent work (none configured), and
// no agent recipe runs in the hook.
func TestDeriveTopicSeedWith_TruncateOnlyWritesSeedReturnsImmediately(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	var mu sync.Mutex
	var recs []writeRecord
	cfg := &config.Config{} // default = single truncate / set_if="empty"
	_, enqueue := deriveTopicSeedWith(
		strings.NewReader(`{"prompt":"refactor the auth layer"}`),
		nil, cfg, "claude",
		noTopicState, noPaneNote,
		func(paneID, topic, kind string) error {
			mu.Lock()
			recs = append(recs, writeRecord{paneID, topic, kind})
			mu.Unlock()
			return nil
		},
	)
	if enqueue {
		t.Error("expected enqueue=false with no agent step configured")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 1 || recs[0] != (writeRecord{"%env", "refactor-the-auth-layer", "seed"}) {
		t.Errorf("seed writes = %v, want one seed refactor-the-auth-layer on env pane", recs)
	}
}

// TestDeriveTopicSeedWith_SeedsThenEnqueues locks the hook phase for the
// truncate→agent pipeline: the truncate step writes the seed synchronously and
// the hook returns a job to enqueue on the daemon (enqueue=true). No agent
// recipe runs in the hook.
func TestDeriveTopicSeedWith_SeedsThenEnqueues(t *testing.T) {
	t.Setenv("TMUX_PANE", "%7")
	var mu sync.Mutex
	var recs []writeRecord
	cfg := withTopicSteps(
		config.TopicStep{Type: config.TopicStepTruncate, SetIf: config.TopicSetIfEmpty},
		config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfEmptyOrSeed},
	)
	job, enqueue := deriveTopicSeedWith(
		strings.NewReader(`{"prompt":"refactor the auth layer"}`),
		nil, cfg, "claude",
		noTopicState, noPaneNote,
		func(paneID, topic, kind string) error {
			mu.Lock()
			recs = append(recs, writeRecord{paneID, topic, kind})
			mu.Unlock()
			return nil
		},
	)
	if !enqueue {
		t.Fatal("expected enqueue=true with an eligible agent step")
	}
	if job.PaneID != "%7" || job.Prompt != "refactor the auth layer" {
		t.Errorf("job = %+v", job)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 1 || recs[0] != (writeRecord{"%7", "refactor-the-auth-layer", "seed"}) {
		t.Errorf("seed writes = %v, want one seed written synchronously in the hook", recs)
	}
}

// TestDeriveTopicSeedWith_RegenerationOverwrite locks "set_if = 'always'
// re-derives every prompt and overwrites even a final Topic without blocking
// the submit": the truncate (always) step writes a seed over the existing
// final synchronously, the hook enqueues the agent (always) phase without
// running any recipe, and the daemon then overwrites the seed with a new final.
func TestDeriveTopicSeedWith_RegenerationOverwrite(t *testing.T) {
	t.Setenv("TMUX_PANE", "%5")
	// Pane already carries a final Topic.
	lookup := func(string) (string, string, string) { return "old-final", config.TopicKindFinal, "sess" }
	cfg := withTopicSteps(
		config.TopicStep{Type: config.TopicStepTruncate, SetIf: config.TopicSetIfAlways},
		config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfAlways},
	)

	// Hook phase: writes the seed over the final synchronously, returns a job
	// to enqueue. No recipe runs in the hook (it never blocks the submit).
	hookWrites := []writeRecord{}
	var hmu sync.Mutex
	job, enqueue := deriveTopicSeedWith(
		strings.NewReader(`{"prompt":"new prompt direction"}`),
		nil, cfg, "claude",
		lookup, noPaneNote,
		func(paneID, topic, kind string) error {
			hmu.Lock()
			hookWrites = append(hookWrites, writeRecord{paneID, topic, kind})
			hmu.Unlock()
			return nil
		},
	)
	if !enqueue {
		t.Fatal("expected enqueue=true for set_if=always regeneration over a final Topic")
	}
	hmu.Lock()
	if len(hookWrites) != 1 || hookWrites[0].Kind != "seed" || hookWrites[0].Topic != "new-prompt-direction" {
		t.Errorf("hook seed writes = %v, want a seed written over the final (synchronously)", hookWrites)
	}
	hmu.Unlock()

	// Daemon phase: re-reads the now-seed pane state, runs the always agent step,
	// and overwrites the seed with a new final — without the hook having to wait
	// for it.
	daemonLookup := func(string) (string, string, string) { return "new-prompt-direction", config.TopicKindSeed, "sess" }
	var dmu sync.Mutex
	var daemonWrites []writeRecord
	run := func(context.Context, []string, []byte) (string, error) { return "regenerated-final", nil }
	disp := newTestDispatcher(func() *config.Config { return cfg }, daemonLookup, noPaneNote, run, recordingWriter(&daemonWrites, &dmu))
	disp.Enqueue(job)
	waitForWrites(t, &dmu, &daemonWrites, 1, 2*time.Second)
	dmu.Lock()
	defer dmu.Unlock()
	if daemonWrites[0] != (writeRecord{"%5", "regenerated-final", "final"}) {
		t.Errorf("daemon writes = %v, want a new final overwriting the seed", daemonWrites)
	}
}

// TestDeriveTopicSeedWith_FinalBlocksEmptyOrSeedGating locks the gating from
// the hook's perspective: when the pane already has a final Topic and the
// truncate/agent steps are empty/empty_or_seed, neither runs and the hook
// enqueues nothing (the final Topic is preserved, no agent work).
func TestDeriveTopicSeedWith_FinalBlocksEmptyOrSeedGating(t *testing.T) {
	t.Setenv("TMUX_PANE", "%5")
	lookup := func(string) (string, string, string) { return "existing", config.TopicKindFinal, "sess" }
	cfg := withTopicSteps(
		config.TopicStep{Type: config.TopicStepTruncate, SetIf: config.TopicSetIfEmpty},
		config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfEmptyOrSeed},
	)
	var mu sync.Mutex
	var recs []writeRecord
	_, enqueue := deriveTopicSeedWith(
		strings.NewReader(`{"prompt":"another prompt"}`),
		nil, cfg, "claude",
		lookup, noPaneNote,
		func(paneID, topic, kind string) error {
			mu.Lock()
			recs = append(recs, writeRecord{paneID, topic, kind})
			mu.Unlock()
			return nil
		},
	)
	if enqueue {
		t.Error("expected enqueue=false: final Topic blocks empty/empty_or_seed")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 0 {
		t.Errorf("writes = %v, want none — final Topic preserved", recs)
	}
}

// TestDeriveTopicSeedWith_NoteSkipsEnqueue locks the hook-side Note guard: a
// pane with a Note does not enqueue the agent phase (the Note outranks the
// Topic, so deriving it would be invisible work). The truncate seed still
// writes if its gate allows (matching ADR 0068's truncate-still-runs rule).
func TestDeriveTopicSeedWith_NoteSkipsEnqueue(t *testing.T) {
	t.Setenv("TMUX_PANE", "%5")
	cfg := withTopicSteps(
		config.TopicStep{Type: config.TopicStepTruncate, SetIf: config.TopicSetIfEmpty},
		config.TopicStep{Type: config.TopicStepAgent, Command: "ollama", SetIf: config.TopicSetIfEmptyOrSeed},
	)
	var mu sync.Mutex
	var recs []writeRecord
	_, enqueue := deriveTopicSeedWith(
		strings.NewReader(`{"prompt":"refactor the auth layer"}`),
		nil, cfg, "claude",
		noTopicState, paneHasNote,
		func(paneID, topic, kind string) error {
			mu.Lock()
			recs = append(recs, writeRecord{paneID, topic, kind})
			mu.Unlock()
			return nil
		},
	)
	if enqueue {
		t.Error("expected enqueue=false: a pane with a Note skips the agent phase")
	}
	mu.Lock()
	defer mu.Unlock()
	// Truncate still runs (the seed is written) — only the agent phase is
	// suppressed for a pane with a Note.
	if len(recs) != 1 || recs[0].Kind != "seed" {
		t.Errorf("writes = %v, want only the seed (truncate still runs)", recs)
	}
}

// TestDeriveTopicSeedWith_NoAgentStepNoEnqueue locks that an agent-only-eligible
// gate with no agent step never enqueues; and an unparseable payload is a silent
// no-op.
func TestDeriveTopicSeedWith_NoOpCases(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")

	t.Run("no agent step configured → no enqueue", func(t *testing.T) {
		cfg := &config.Config{} // single truncate step
		var recs []writeRecord
		var mu sync.Mutex
		_, enqueue := deriveTopicSeedWith(
			strings.NewReader(`{"prompt":"hi"}`), nil, cfg, "claude",
			noTopicState, noPaneNote,
			func(paneID, topic, kind string) error {
				mu.Lock()
				recs = append(recs, writeRecord{paneID, topic, kind})
				mu.Unlock()
				return nil
			},
		)
		if enqueue {
			t.Error("expected enqueue=false with no agent step")
		}
	})

	t.Run("empty prompt → no enqueue, no seed", func(t *testing.T) {
		cfg := withTopicSteps(alwaysAgentStep("ollama"))
		var recs []writeRecord
		var mu sync.Mutex
		_, enqueue := deriveTopicSeedWith(
			strings.NewReader(`{"prompt":"   "}`), nil, cfg, "claude",
			noTopicState, noPaneNote,
			func(paneID, topic, kind string) error {
				mu.Lock()
				recs = append(recs, writeRecord{paneID, topic, kind})
				mu.Unlock()
				return nil
			},
		)
		if enqueue {
			t.Error("expected enqueue=false for an empty prompt")
		}
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 0 {
			t.Errorf("writes = %v, want none for an empty prompt", recs)
		}
	})

	t.Run("unparseable payload → no enqueue, no seed", func(t *testing.T) {
		cfg := withTopicSteps(alwaysAgentStep("ollama"))
		var recs []writeRecord
		var mu sync.Mutex
		_, enqueue := deriveTopicSeedWith(
			strings.NewReader("not json"), nil, cfg, "claude",
			noTopicState, noPaneNote,
			func(paneID, topic, kind string) error {
				mu.Lock()
				recs = append(recs, writeRecord{paneID, topic, kind})
				mu.Unlock()
				return nil
			},
		)
		if enqueue {
			t.Error("expected enqueue=false for an unparseable payload")
		}
		mu.Lock()
		defer mu.Unlock()
		if len(recs) != 0 {
			t.Errorf("writes = %v, want none for an unparseable payload", recs)
		}
	})
}

// --- helpers ---

// waitForWrites polls until recs holds at least n entries (or times out).
func waitForWrites(t *testing.T, mu *sync.Mutex, recs *[]writeRecord, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(*recs)
		mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d writes (have %d)", n, len(*recs))
}

// waitUntil polls until cond returns true (or times out).
func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}