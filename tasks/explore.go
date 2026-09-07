package tasks

import (
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/prompt"
	"github.com/glebglazov/pop/project"
)

// DefaultExploreEffort is the model-strength tier the Explorer runs at when
// neither a CLI flag nor a per-set `explorer` object names one (ADR-0214).
// Exploration defaults where verification and refinement do, for a reason of
// its own: every builder in the set is handed this one document, so a seam read
// wrong here is read wrong by all of them.
const DefaultExploreEffort = "heavy"

// explorationReport is the header Explore stamps on its document. It carries no
// report family: the pass runs once and rewrites one file at
// `exploration.md` in the set directory, so there is no directory of
// timestamped documents and nothing to supersede (ADR-0262).
var explorationReport = reportHeader{
	Title:        "Exploration report",
	WrittenLabel: "Explored",
	AgentLabel:   "Explorer",
}

// ExploreOptions configures a `pop tasks explore <set>` run.
type ExploreOptions struct {
	ResolveInput ResolveInput
	// TaskSetID is the bare Task-set identifier to explore.
	TaskSetID string
	// Agents is the ordered CLI Explorer fallback list (`--agent`, repeatable).
	// Empty ⇒ resolution falls through to the per-set `explorer` object, then to
	// the implement agents.
	Agents []string
	// Effort is the CLI Explorer effort override (`--effort`). Empty ⇒ the
	// per-set object, then DefaultExploreEffort.
	Effort string
	// Timeout bounds the single Explorer attempt. Zero uses DefaultAttemptTimeout.
	Timeout time.Duration
	// Output receives the live agent stream and the run's chrome.
	Output io.Writer
	// Wait is the `--wait` / `--no-wait` tri-state for admission to the checkout
	// (ADR-0239). The unset default waits at a terminal and refuses elsewhere.
	Wait AdmissionWaitChoice
	// ConfirmIn is the invocation's input, read only to tell a human at a
	// terminal from a script when resolving Wait.
	ConfirmIn io.Reader
}

// ExploreResult is the outcome of one explore pass.
type ExploreResult struct {
	SetID   string
	WorkSHA string
	// Path is the Exploration report written.
	Path string
	// Body is the document itself.
	Body string
}

// exploreCoreOptions carries the already-resolved inputs to the core explore
// routine, so tests can drive it without real path-resolution git calls.
type exploreCoreOptions struct {
	DefPath     string
	RuntimePath string
	SetID       string
	Agents      []string
	Effort      string
	Timeout     time.Duration
	Output      io.Writer
	probeMemo   *agentAvailabilityProbeMemo
	// admission is the policy the Explorer acquires the checkout under
	// (ADR-0238/0239).
	admission AdmissionPolicy
	// checkoutHeld says the caller is already inside a claim on this checkout —
	// the drain's own explore step, which runs under the drain's running Drain
	// row. Explore then takes nothing: a second acquisition for the same set
	// would be refused by the Set claim the caller itself holds.
	checkoutHeld bool
	// runExplorer returns the report and the agent that wrote it, replacing the
	// real agent spawn in tests.
	runExplorer func(prompt string) (string, string, error)
}

// ExploreTaskSet explores a set using default dependencies.
func ExploreTaskSet(opts ExploreOptions) (*ExploreResult, error) {
	return ExploreTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// ExploreTaskSetWith is `pop tasks explore <set>` (ADR-0262): it resolves the
// set, hands a fresh Explorer the set's tasks and its spec, and writes what
// comes back as the set's Exploration report — the code as found in the area
// those tasks touch, which every builder in the set is then handed instead of
// re-deriving it per attempt.
//
// A hand run is the human re-opening the question, so it runs on any set: the
// set's own Explore directive declines the drain's automatic step, not a human
// asking, exactly as `"refine": false` and `"verify": false` do. It rewrites an
// existing report for the same reason.
func ExploreTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts ExploreOptions) (*ExploreResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, err
	}
	runtimePath, err := ResolveRuntimePathWith(d, resolved.ProjectPath, opts.ResolveInput.RuntimeOverride)
	if err != nil {
		return nil, err
	}
	cfg, _ := loadConfig(config.DefaultConfigPath())
	return exploreResolvedSet(d, cfg, exploreCoreOptions{
		DefPath:     resolved.DefinitionPath,
		RuntimePath: runtimePath,
		SetID:       strings.TrimSpace(opts.TaskSetID),
		Agents:      opts.Agents,
		Effort:      opts.Effort,
		Timeout:     opts.Timeout,
		Output:      opts.Output,
		admission:   opts.Wait.Policy(opts.ConfirmIn),
	})
}

// exploreResolvedSet is the resolved-path core of `pop tasks explore`: it runs
// the Explorer and writes the report it answered with.
//
// The report is written only from a pass that came back with a document. An
// interrupted or failed pass returns the error and writes nothing, so the set
// keeps whatever report it already had rather than trading a whole map for a
// fragment of one.
func exploreResolvedSet(d *Deps, cfg *config.Config, opts exploreCoreOptions) (*ExploreResult, error) {
	m, err := loadVerifiableManifest(d, verifyCoreOptions{SetID: opts.SetID, DefPath: opts.DefPath})
	if err != nil {
		return nil, err
	}
	// The Explorer reads the checkout — that reading is the whole pass — so it is
	// a Tree-stable operation and takes the checkout for its duration, waiting at
	// a terminal when something else holds it (ADR-0238). A map drawn from files
	// another drain is rewriting describes a tree that never existed, which is
	// the one failure this document cannot afford. A caller already holding the
	// checkout for this set — the drain's own explore step — takes nothing.
	if !opts.checkoutHeld {
		hold, err := AcquireTreeStable(d, opts.RuntimePath, opts.SetID, opts.Output, opts.admission)
		if err != nil {
			return nil, err
		}
		defer func() { _ = hold.Release() }()
	}
	// The work SHA is read before the Explorer runs: it is the commit the report
	// describes, and an Explorer that moved it would be doing something this pass
	// must not do.
	workSHA := verifyWorkSHA(d, opts.RuntimePath)
	body, agent, err := runExplorer(d, cfg, opts, m, workSHA)
	if err != nil {
		return nil, err
	}
	doc := explorationReport.renderDocument(d.Now().UTC(), opts.SetID, workSHA, "", agent, body)
	path, err := writeExplorationReport(d, m.Dir, doc)
	if err != nil {
		return nil, err
	}
	printExplorationWritten(opts.Output, opts.SetID, path)
	return &ExploreResult{SetID: opts.SetID, WorkSHA: workSHA, Path: path, Body: doc}, nil
}

// writeExplorationReport files the document flat in the set directory beside
// `spec.md` and `progress.txt` — in pop's Work store rather than in the
// repository, so the map can never be staged into a commit — replacing the
// set's previous one. The pass owns the document whole (ADR-0262), so a rewrite
// is what a second pass means: there is no earlier version to keep, and nothing
// reads one.
func writeExplorationReport(d *Deps, setDir, body string) (string, error) {
	path := filepath.Join(setDir, ExplorationFileName)
	if err := d.FS.WriteFile(path, []byte(ensureTrailingNewline(body)), 0o644); err != nil {
		return "", exitErr(ExitOperational, "write exploration report: %v", err)
	}
	return path, nil
}

// runExplorer builds the prompt and invokes the Explorer, returning the report
// and the agent that wrote it.
func runExplorer(d *Deps, cfg *config.Config, opts exploreCoreOptions, m *Manifest, workSHA string) (string, string, error) {
	text := buildExplorerPrompt(d, m, workSHA)
	run := opts.runExplorer
	if run == nil {
		sel, err := resolveExplorer(opts.Agents, opts.Effort, m, cfg)
		if err != nil {
			return "", "", err
		}
		run = func(prompt string) (string, string, error) {
			return runConfiguredExplorer(d, cfg, sel, m.Dir, opts.SetID, workSHA, opts.RuntimePath, prompt, opts.Output, opts.Timeout, opts.probeMemo)
		}
	}
	body, agent, err := run(text)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", "", exitErr(ExitOperational, "the Explorer produced no report for %q", opts.SetID)
	}
	return body, agent, nil
}

// resolveExplorer applies the Explorer precedence chain (ADR-0262), highest
// first: CLI flags → the per-set manifest `explorer` override → the implement
// agents / DefaultExploreEffort. Agents and effort resolve independently, the
// way the Verifier's and the Refiner's do.
//
// The chain never consults which agents implement the set's tasks: the Explorer
// is resolved from the human's configuration alone, so it is a fresh agent by
// construction — which is the point of the pass, since nothing it is told comes
// from an attempt.
func resolveExplorer(cliAgents []string, cliEffort string, m *Manifest, cfg *config.Config) (verifierSelection, error) {
	agents := nonEmptyStrings(cliAgents)
	effort := strings.TrimSpace(cliEffort)

	if over := m.ExplorerOverride(); over != nil {
		if len(agents) == 0 {
			agents = nonEmptyStrings(over.Agents)
		}
		if effort == "" {
			effort = strings.TrimSpace(over.Effort)
		}
	}
	if len(agents) == 0 {
		// The chain ends at what a human configured for building this repository:
		// exploring it is the same reading skill, and a list of its own would be a
		// key nobody has asked to set differently.
		agents = ResolveDefaultAgentPresets(nil, "", false, cfg)
	}
	if effort == "" {
		effort = DefaultExploreEffort
	}
	return verifierSelection{Agents: agents, Effort: effort}, nil
}

// runConfiguredExplorer walks the resolved Explorer agent list at the resolved
// effort through the shared fallback walk, and returns the report beside the
// agent that wrote it. The cap and retry schedule are the built-in task
// defaults: a report that cannot be produced this time costs nothing to ask
// for again.
func runConfiguredExplorer(d *Deps, cfg *config.Config, sel verifierSelection, taskSetDir, setID, workSHA, runtimePath, prompt string, out io.Writer, timeout time.Duration, probeMemo *agentAvailabilityProbeMemo) (string, string, error) {
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}
	quotaRetryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return "", "", exitErr(ExitSetup, "%v", err)
	}
	walked, err := runAgentFallbackWalk(d, agentFallbackWalk{
		role:            explorerRole(d, out, taskSetDir, setID, workSHA),
		sel:             sel,
		setID:           setID,
		runtimePath:     runtimePath,
		prompt:          prompt,
		out:             out,
		errOut:          out,
		timeout:         timeout,
		maxTries:        config.DefaultTaskMaxTries,
		retryDelays:     append([]time.Duration(nil), config.DefaultTaskAttemptRetryDelays...),
		quotaRetryAfter: quotaRetryAfter,
		cfg:             cfg,
		probeMemo:       probeMemo,
	})
	if err != nil {
		return "", "", err
	}
	// Prose from an attempt that timed out or died is half a map, and a builder
	// cannot tell which half. Once the cap is spent the pass reports nothing.
	if strings.TrimSpace(walked.Answer) != "" && !walked.AnswerRetryEligible {
		return walked.Answer, walked.Agent, nil
	}
	if len(walked.Unavailable) == 0 {
		return "", "", exitErr(ExitOperational, "the Explorer produced no report")
	}
	return "", "", exitErr(ExitSetup, "%s", formatHumanHealingExhaustionMessage(walked.Unavailable))
}

// explorerRole is what the shared fallback walk calls the Explorer: its name in
// the operator's output, the Captured run pair each invocation is filed as under
// the `explore` phase label, and the prose roles' retry rule — an Explore pass
// has no format to parse, so any prose from a run that reached its own ending is
// the Explorer answering.
//
// Its runs are filed for the reason the Verifier's and the Refiner's are: the
// pass spends the same agent quota on the same set, so hiding it would make
// `pop tasks spend` understate what the set cost. Nothing rides along in the
// run's verdict slot, because the pass reaches no verdict — the run's own ending
// is all it says about the pass.
//
// It is the one role spawned under the Read-only agent posture (ADR-0221). The
// Explorer's whole output is prose that pop files; it has no reason to touch the
// checkout, and withholding the tools is a stronger guarantee than a sentence in
// its prompt asking it not to.
func explorerRole(d *Deps, errOut io.Writer, taskSetDir, setID, workSHA string) agentRole {
	return agentRole{
		Noun:     "Explorer agent",
		Gerund:   "Exploring",
		Phase:    spendPhaseExplore,
		ReadOnly: true,
		Persist: func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int) {
			_ = persistExploreRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, try, outcome, reason, exitCode)
		},
		PersistAnswer: func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int, _ string) {
			_ = persistExploreRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, try, outcome, reason, exitCode)
		},
		PersistSkipped: func(rec *streamRecorder, invocation *AgentInvocation, model string, try int, reason string, exitCode int) {
			_ = persistSkippedExploreRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, model, try, reason, exitCode)
		},
		RetryEligible: proseAttemptRetryEligible,
	}
}

// printExplorationWritten tells the operator where the report went and how to
// read it. It says nothing about what the pass found: the report is the whole
// output of the pass, and a summary here would be a second, shorter map.
func printExplorationWritten(w io.Writer, setID, path string) {
	if w == nil {
		return
	}
	out := outputFor(w)
	out.line(ansiBold, "━━ Exploration for %s", setID)
	out.line(ansiDim, "   Document: %s", path)
	out.line(ansiDim, "   Read it: pop tasks artifacts %s --show %s", setID, ExplorationFileName)
}

// buildExplorerPrompt assembles the Explorer's prompt end to end: pop owns the
// role framing, what the report must contain and what stays out of it. Around
// them sit the set's manifest listing, every task body, and the spec when the
// set has one — the whole of what the set set out to do, since which code
// matters is decided by what is about to be built in it.
//
// The task bodies are inlined where the Verifier's are and the Refiner's are
// not: the Refiner reads a changeset that exists, while the Explorer's subject
// is code nobody has touched yet, so the tasks are the only statement of which
// area to describe.
//
// Nothing about prior art is asked for. The composed forms this repository
// already has are checked at the moment a builder is about to write mechanism,
// which is not knowable before the attempt (ADR-0262 decision 7, ADR-0259).
func buildExplorerPrompt(d *Deps, m *Manifest, workSHA string) string {
	view := explorerPromptView{
		TaskSet:     m.Stem,
		WorkSHALine: optionalLine("Work SHA: ", workSHA),
		Tasks:       gateTaskRows(m),
		TaskBodies:  explorerTaskBodies(d, m),
	}
	if spec, ok := readSpec(d, m); ok {
		view.SpecRecorded = true
		view.Spec = spec
	}
	return prompt.MustRender(promptTemplates, "explorer.tmpl.md", view)
}

// explorerPromptView is what the Explorer's template renders against. Like the
// other pass views, the one optional section is a named boolean beside the text
// it guards, so the template picks a whole section and never asks why a field is
// empty.
type explorerPromptView struct {
	TaskSet      string
	WorkSHALine  string
	Tasks        []taskRow
	TaskBodies   []explorerTaskBody
	SpecRecorded bool
	Spec         string
}

// explorerTaskBody is one task as the Explorer reads it: the heading every gate
// states a task under, and the inlined body (or the read failure standing in for
// it) the shared partial renders.
type explorerTaskBody struct {
	Heading string
	Body    taskBodyRow
}

// explorerTaskBodies inlines every task in the set, of any type and any status.
// The filters the Verifier's listing applies are about judging finished work; an
// Explorer describes the code a whole set is about to be built in, and a HITL
// gate's body says as much about which seam that is as an AFK task's does.
func explorerTaskBodies(d *Deps, m *Manifest) []explorerTaskBody {
	bodies := make([]explorerTaskBody, 0, len(m.Tasks))
	for _, task := range m.Tasks {
		bodies = append(bodies, explorerTaskBody{
			Heading: taskHeading(task),
			Body:    readTaskBody(d, filepath.Join(m.Dir, task.File)),
		})
	}
	return bodies
}
