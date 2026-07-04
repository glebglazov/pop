package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
)

// topicDeriveJob is the payload the hook sends to the daemon to run the agent
// steps of the Topic derivation pipeline (ADR 0068). The prompt and
// transcript_path are already parsed off the agent's hook payload in the hook
// (per-agent adapter); the daemon never re-parses the agent payload. The pane
// id travels here (overriding $TMUX_PANE, which is meaningless in the daemon
// process). The daemon re-reads @pop_topic / @pop_topic_kind / session from
// tmux when it runs, so its gating reflects the seed the hook just wrote.
type topicDeriveJob struct {
	PaneID         string
	Prompt         string
	TranscriptPath string
}

// topicDerivationWriter persists a derived Topic + provenance on a pane. The
// dispatcher wraps this with the single-flight generation guard so a
// superseded derivation never overwrites a newer one's write.
type topicDerivationWriter func(paneID, topic, kind string) error

// topicDerivationDispatcher enforces per-pane single-flight for the agent phase
// of the Topic pipeline (ADR 0068): at most one agent derivation is in flight
// per pane. A newer prompt for the same pane supersedes (cancels and replaces)
// an in-flight derivation — its recipe's context is cancelled so a running
// ollama call is killed promptly rather than queuing — so `set_if = "always"`
// regeneration on a fast typist can't pile up overlapping model calls. Only the
// newest generation may write a final Topic: writes are serialized under the
// dispatcher lock and re-check the generation, so an older derivation that
// computed its result before a newer one was enqueued can never clobber the
// newer one's write. An older derivation already superseded before its write
// simply drops its result.
type topicDerivationDispatcher struct {
	mu      sync.Mutex
	latest  map[string]uint64            // paneID -> newest enqueued generation
	cancels map[string]context.CancelFunc // paneID -> in-flight ctx cancel

	// Dependencies for each background derivation. The production wiring
	// (newTopicDerivationDispatcher) uses the real config loader, state lookup,
	// note lookup, recipe runner, and tmux writer. Tests inject stubs.
	loadCfg func() *config.Config
	lookup  topicStateLookup
	hasNote paneNoteLookup
	run     topicRecipeRunner
	write   topicDerivationWriter
}

// newTopicDerivationDispatcher builds the production dispatcher used by the
// monitor daemon's "derive-topic" handler.
func newTopicDerivationDispatcher() *topicDerivationDispatcher {
	return &topicDerivationDispatcher{
		latest:  map[string]uint64{},
		cancels: map[string]context.CancelFunc{},
		loadCfg: func() *config.Config {
			cfg, err := config.Load(config.DefaultConfigPath())
			if err != nil {
				debug.Error("topic dispatcher: load config: %v", err)
			}
			if cfg == nil {
				cfg = &config.Config{}
			}
			return cfg
		},
		lookup:  defaultTopicStateLookup,
		hasNote: defaultPaneHasNote,
		run:     runTopicRecipe,
		write: func(paneID, topic, kind string) error {
			return setPaneTopicWithKind(defaultTmux, paneID, topic, kind)
		},
	}
}

// Enqueue kicks off the agent-phase derivation for job on a background goroutine
// and returns immediately — the hook never blocks on a model call. Any in-flight
// derivation for the same pane is cancelled (its recipe context is cancelled)
// and its eventual write is dropped by the generation guard, so only the newest
// generation may persist a final Topic.
func (d *topicDerivationDispatcher) Enqueue(job topicDeriveJob) {
	if job.PaneID == "" {
		return
	}
	d.mu.Lock()
	d.latest[job.PaneID]++
	gen := d.latest[job.PaneID]
	if cancel := d.cancels[job.PaneID]; cancel != nil {
		cancel() // supersede any in-flight derivation for this pane
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancels[job.PaneID] = cancel
	d.mu.Unlock()

	go func() {
		defer cancel()
		debug.Log("topic dispatcher: deriving agent topic for pane %s (gen %d)", job.PaneID, gen)
		runTopicAgentDerivationWith(ctx, job, d.loadCfg(), d.lookup, d.hasNote, d.run, func(paneID, topic, kind string) error {
			// Generation guard + serialized write (held under the lock so the
			// gen check is atomic with the write): a superseded derivation and a
			// cancelled one both drop here without touching tmux.
			d.mu.Lock()
			defer d.mu.Unlock()
			if ctx.Err() != nil || d.latest[paneID] != gen {
				debug.Log("topic dispatcher: dropping superseded write of %q on pane %s (gen %d)", topic, paneID, gen)
				return nil
			}
			return d.write(paneID, topic, kind)
		})
		// Cleanup: drop our cancel entry iff it is still ours (a newer enqueue
		// has already replaced it with its own).
		d.mu.Lock()
		if d.latest[job.PaneID] == gen {
			delete(d.cancels, job.PaneID)
		}
		d.mu.Unlock()
	}()
}

// runTopicAgentDerivationWith is the daemon-side agent phase of the Topic
// pipeline (ADR 0068). It runs ONLY the configured agent steps — the truncate
// seed was already written synchronously by the hook (see deriveTopicSeedWith)
// — each gated by set_if against the pane's current @pop_topic_kind (re-read
// from tmux), with a pane Note skipping every step. The first non-empty result
// wins and is written via writer (which the dispatcher wraps with the
// single-flight generation guard). parent bounds the recipe execs so a
// superseding derivation cancels a running model call promptly. Returns true
// iff a final Topic was written.
func runTopicAgentDerivationWith(parent context.Context, job topicDeriveJob, cfg *config.Config,
	lookup topicStateLookup, hasNote paneNoteLookup, run topicRecipeRunner,
	writer topicDerivationWriter,
) bool {
	if job.PaneID == "" || strings.TrimSpace(job.Prompt) == "" {
		return false
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	steps := cfg.PaneMonitoringTopicSteps()
	if len(steps) == 0 {
		return false
	}
	if hasNote(job.PaneID) {
		debug.Log("topic agent derive: skipping pane %s — has a Note", job.PaneID)
		return false
	}
	maxWords := cfg.PaneMonitoringTopicWords()
	recipeTimeout := cfg.PaneMonitoringTopicDerivationTimeout()
	prevTopic, kind, session := lookup(job.PaneID)
	currentKind := kind

	modelPrompt := buildTopicModelPrompt(job.Prompt, maxWords)
	payload, err := json.Marshal(topicRecipePayload{
		PrevTopic:      prevTopic,
		PrevTopicKind:  kind,
		Prompt:         job.Prompt,
		TranscriptPath: job.TranscriptPath,
		PaneID:         job.PaneID,
		Session:        session,
	})
	if err != nil {
		debug.Error("topic agent derive: marshal payload: %v", err)
		payload = nil
	}

	for _, step := range steps {
		if step.Type != config.TopicStepAgent {
			continue // truncate steps already ran in the hook
		}
		if !config.TopicSetIfAllows(step.SetIf, currentKind) {
			continue
		}
		recipe, ok := resolveTopicRecipe(step.Command)
		if !ok {
			debug.Log("topic agent derive: unknown recipe %q — skipping", step.Command)
			continue
		}
		argv, stdin := recipe.build(modelPrompt, payload, step.Args)
		debug.Log("topic agent derive: recipe %q model prompt=%q", step.Command, modelPrompt)
		stepTimeout := step.DerivationTimeout(recipeTimeout)
		ctx, cancel := context.WithTimeout(parent, stepTimeout)
		out, runErr := run(ctx, argv, stdin)
		cancel()
		if runErr != nil {
			debug.Error("topic agent derive: recipe %q failed: %v", step.Command, runErr)
			continue
		}
		debug.Log("topic agent derive: recipe %q raw model output=%q", step.Command, out)
		derived := slugifyTopic(capTopic(recipe.parse(out)), maxWords)
		if derived == "" {
			debug.Log("topic agent derive: recipe %q produced no usable topic", step.Command)
			continue
		}
		if err := writer(job.PaneID, derived, config.TopicKindFinal); err != nil {
			debug.Error("topic agent derive: write final on %s: %v", job.PaneID, err)
			return false
		}
		debug.Log("topic agent derive: recipe %q wrote final %q on pane %s", step.Command, derived, job.PaneID)
		return true // first non-empty result wins
	}
	return false
}

// deriveTopicSeedWith is the synchronous hook phase of `set-topic --derive`
// (ADR 0068): it parses the agent hook payload, runs the configured TRUNCATE
// steps (gated by set_if against the current @pop_topic_kind) and writes each
// resulting seed via seedWriter, then decides whether the agent phase should be
// enqueued on the daemon. It returns the topicDeriveJob and enqueue=true when
// at least one agent step's set_if would allow it against the post-truncate kind
// and the pane has no Note; otherwise enqueue=false (no daemon work). No agent
// recipe runs here — the hook returns as soon as the seed is written, so the
// prompt submit is never blocked by a model call.
func deriveTopicSeedWith(r io.Reader, args []string, cfg *config.Config, label string,
	lookup topicStateLookup, hasNote paneNoteLookup,
	seedWriter topicDerivationWriter,
) (job topicDeriveJob, enqueue bool) {
	paneID := os.Getenv("TMUX_PANE")
	if len(args) > 0 && strings.HasPrefix(args[0], "%") {
		paneID = args[0]
	}
	job.PaneID = paneID

	data, err := io.ReadAll(r)
	if err != nil {
		debug.Error("pane set-topic --derive: read stdin: %v", err)
		return job, false
	}
	prompt, transcriptPath, err := parseTopicPayload(data, label)
	if err != nil {
		debug.Error("pane set-topic --derive: %v", err)
		return job, false
	}
	debug.Log("pane set-topic --derive: parsed prompt=%q transcript_path=%q", prompt, transcriptPath)
	if strings.TrimSpace(prompt) == "" {
		return job, false
	}
	job.Prompt = prompt
	job.TranscriptPath = transcriptPath

	if cfg == nil {
		cfg = &config.Config{}
	}
	steps := cfg.PaneMonitoringTopicSteps()
	if len(steps) == 0 {
		return job, false
	}
	maxWords := cfg.PaneMonitoringTopicWords()

	_, topicKind, _ := lookup(paneID)
	currentKind := topicKind
	hasAgentStep := false

	// Phase 1 — truncate steps run synchronously in the hook, writing the seed
	// and evolving currentKind. Truncate steps always run regardless of their
	// position in the list (the docs order is [truncate, agent], but pop
	// honours any order).
	for _, step := range steps {
		if step.Type == config.TopicStepAgent {
			hasAgentStep = true
			continue
		}
		if !config.TopicSetIfAllows(step.SetIf, currentKind) {
			continue
		}
		derived := slugifyTopic(truncateTopic(prompt), maxWords)
		if derived == "" {
			continue
		}
		if err := seedWriter(paneID, derived, config.TopicKindSeed); err != nil {
			debug.Error("pane set-topic --derive: write seed on %s: %v", paneID, err)
		}
		currentKind = config.TopicKindSeed
	}

	if !hasAgentStep {
		return job, false
	}

	// Phase 2 — decide whether to enqueue the agent phase on the daemon. The
	// daemon gates each agent step against currentKind (the post-truncate kind),
	// so the enqueue decision mirrors that: enqueue iff some agent step's
	// set_if allows against currentKind and the pane has no Note. A Note is
	// also re-checked authoritatively by the daemon; this check only avoids a
	// pointless round-trip.
	if hasNote(paneID) {
		return job, false
	}
	for _, step := range steps {
		if step.Type != config.TopicStepAgent {
			continue
		}
		if config.TopicSetIfAllows(step.SetIf, currentKind) {
			return job, true
		}
	}
	return job, false
}

