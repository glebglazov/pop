package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/prompt"
	"github.com/glebglazov/pop/project"
)

// DefaultRefineEffort is the model-strength tier the Refiner runs at when
// neither a CLI flag nor [work.refine].effort names one (ADR-0214): judging
// naming, structure and idiom against prose standards is the strongest tier's
// work, so refine defaults where verification does.
const DefaultRefineEffort = "heavy"

// refineReport is Refine's report family on the shared pass-report machinery
// (ADR-0245): documents named `refine-<instant>.md` under the set's own
// `refine/` directory, headed by the facts a reader needs to know what was
// refined.
var refineReport = passReport{
	ArtifactType: ArtifactTypeRefine,
	DirName:      RefineDirName,
	FilePrefix:   "refine-",
	Title:        "Refine report",
	WrittenLabel: "Refined",
	AgentLabel:   "Refiner",
	Noun:         "refine",
	PointerLabel: "Refine",
}

// ImplementationConvention resolves this repository's `implementation`
// Convention stack as the prose a Refiner (and, when opted in, an implementer)
// is handed as a labelled block (ADR-0246). It is a seam rather than a direct
// call: the conventions package answers Repository identity through this one,
// so tasks cannot import it back. A nil seam, an error, or an empty answer all
// mean the same thing here — the prompt carries no convention block.
type ImplementationConvention func(cwd string) (string, error)

// DocumentOverlay resolves the Overlay ranks for a named document in this
// repository — keyed by name rather than by Convention kind, so `refine` can
// carry one after it left the kind set (ADR-0247). A nil seam, an error, or an
// empty answer all mean the same thing: the prompt carries no Overlay block.
type DocumentOverlay func(cwd string) (string, error)

// RefineOptions configures a `pop tasks refine <set>` run.
type RefineOptions struct {
	ResolveInput ResolveInput
	// TaskSetID is the bare Task-set identifier to refine.
	TaskSetID string
	// Agents is the ordered CLI Refiner fallback list (`--agent`, repeatable).
	// Empty ⇒ resolution falls through to [work.refine].agents, then
	// [work.implement].agents.
	Agents []string
	// Effort is the CLI Refiner effort override (`--effort`). Empty ⇒ config,
	// then DefaultRefineEffort.
	Effort string
	// Timeout bounds the single Refiner attempt. Zero uses DefaultAttemptTimeout.
	Timeout time.Duration
	// Output receives the live agent stream and the run's chrome.
	Output io.Writer
	// Show prints the set's latest Refine report and runs nothing — the path a
	// document takes to a pull request, where piping is the human's business.
	Show bool
	// Convention resolves the `implementation` convention for the checkout.
	Convention ImplementationConvention
	// Overlay resolves the `refine` Overlay for the checkout (ADR-0247).
	Overlay DocumentOverlay
	// Wait is the `--wait` / `--no-wait` tri-state for admission to the checkout
	// (ADR-0239). The unset default waits at a terminal and refuses elsewhere.
	Wait AdmissionWaitChoice
	// ConfirmIn is the invocation's input, read only to tell a human at a
	// terminal from a script when resolving Wait.
	ConfirmIn io.Reader
}

// RefineResult is the outcome of one refine pass.
type RefineResult struct {
	SetID   string
	WorkSHA string
	// Path is the Refine report written (or read, under Show).
	Path string
	// CommitSHA is the Refine commit the pass's in-place fixes landed as, empty
	// when the pass fixed nothing and so committed nothing.
	CommitSHA string
	// Body is the document itself.
	Body string
}

// refineCoreOptions carries the already-resolved inputs to the core refine
// routine, so tests can drive it without real path-resolution git calls. The
// runRefiner seam replaces the real agent spawn in tests.
type refineCoreOptions struct {
	DefPath     string
	RuntimePath string
	// Repo is the repository identity the set's Refine episode is keyed by (the
	// git common dir). Empty resolves nothing and records no episode, so a refine
	// pass still runs and still writes its report in a checkout pop cannot identify.
	Repo       string
	SetID      string
	Agents     []string
	Effort     string
	Timeout    time.Duration
	Output     io.Writer
	Show       bool
	Convention ImplementationConvention
	Overlay    DocumentOverlay
	// runRefiner returns the Refiner's document and the agent that wrote it.
	runRefiner func(prompt string) (string, string, error)
	probeMemo  *agentAvailabilityProbeMemo
	// admission is the policy the standalone Refiner acquires the checkout
	// under (ADR-0238/0239).
	admission AdmissionPolicy
	// checkoutHeld says the caller is already inside a claim on this checkout —
	// the drain's own refine step, which runs under the drain's running Drain
	// row. Refine then takes nothing: a second acquisition for the same set
	// would be refused by the Set claim the caller itself holds.
	checkoutHeld bool
}

// RefineTaskSet refines a set using default dependencies.
func RefineTaskSet(opts RefineOptions) (*RefineResult, error) {
	return RefineTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RefineTaskSetWith is `pop tasks refine <set>` (ADR-0240): it resolves the set,
// hands a fresh Refiner pop's refine instruction, the resolved `refine`
// convention and the set's previous Refine report, and writes what comes back
// as the set's current one. It reaches no verdict and writes no status, but it
// does take the checkout for its duration: a Refiner reading files another
// drain is rewriting judges a state that never existed (ADR-0238).
func RefineTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RefineOptions) (*RefineResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, err
	}
	runtimePath, err := ResolveRuntimePathWith(d, resolved.ProjectPath, opts.ResolveInput.RuntimeOverride)
	if err != nil {
		return nil, err
	}
	cfg, _ := loadConfig(config.DefaultConfigPath())
	repo := ""
	if id, idErr := ResolveRepositoryIdentity(d, runtimePath); idErr == nil {
		repo = id.CommonDir
	}
	return refineResolvedSet(d, cfg, refineCoreOptions{
		DefPath:     resolved.DefinitionPath,
		RuntimePath: runtimePath,
		Repo:        repo,
		SetID:       strings.TrimSpace(opts.TaskSetID),
		Agents:      opts.Agents,
		Effort:      opts.Effort,
		Timeout:     opts.Timeout,
		Output:      opts.Output,
		Show:        opts.Show,
		Convention:  opts.Convention,
		Overlay:     opts.Overlay,
		admission:   opts.Wait.Policy(opts.ConfirmIn),
	})
}

// refineResolvedSet is the resolved-path core of `pop tasks refine`. Under Show
// it reads and prints; otherwise it refuses anything it cannot refine, runs the
// Refiner, and supersedes the set's current report with what came back.
func refineResolvedSet(d *Deps, cfg *config.Config, opts refineCoreOptions) (*RefineResult, error) {
	m, err := loadVerifiableManifest(d, verifyCoreOptions{SetID: opts.SetID, DefPath: opts.DefPath})
	if err != nil {
		return nil, err
	}
	if opts.Show {
		// Reading the set's current document touches Task storage only, so it
		// contends for nothing and always runs (ADR-0238).
		return showLatestRefine(d, opts, m)
	}
	// Everything below reads the checkout — the commit range, the diff, and the
	// files the Refiner opens for itself — so the Refiner is a Tree-stable
	// operation and takes the checkout for its duration, waiting at a terminal
	// when something else holds it (ADR-0238). The hold is released as soon as
	// the report is written: no terminal is recorded, and Refine stays the
	// lighter surface it was.
	if !opts.checkoutHeld {
		hold, err := AcquireTreeStable(d, opts.RuntimePath, opts.SetID, opts.Output, opts.admission)
		if err != nil {
			return nil, err
		}
		defer func() { _ = hold.Release() }()
	}
	work, err := refinableWork(d, opts, m)
	if err != nil {
		return nil, err
	}
	previous, hasPrevious := refineReport.latestDocument(d, m.Dir)
	convention := resolveImplementationConvention(opts)
	overlay := resolveRefineOverlay(opts)
	// The work SHA is read before the Refiner runs: it is the commit the report
	// describes, and the Refiner that moved it would be doing something a refine
	// pass must not do.
	workSHA := verifyWorkSHA(d, opts.RuntimePath)
	// Capture before the Refiner so an abandoned pass can discard only what it
	// wrote, leaving any pre-existing dirty state untouched (ADR-0248).
	snap, err := captureRefineTree(d, opts.RuntimePath)
	if err != nil {
		return nil, err
	}
	body, agent, err := runRefiner(d, cfg, opts, m, workSHA, work, convention, overlay, previous, hasPrevious)
	if err != nil {
		return nil, err
	}
	// The reply carries three things: the pass outcome, the subject pop commits
	// under when the outcome is refined, and the report itself. Splitting here
	// keeps the channel lines out of the document a human reads.
	rendered, outcome, report := splitRefinerReply(body)
	at := d.Now().UTC()
	doc := refineReport.renderDocument(at, opts.SetID, workSHA, work.Range, agent, report)
	path, err := refineReport.writeDocument(d, m.Dir, at, doc)
	if err != nil {
		return nil, err
	}
	// The report is written before the commit/discard so a git failure cannot
	// cost the only account of what the pass did.
	commitSHA, err := commitRefinePass(d, cfg, opts.RuntimePath, opts.SetID, refineCommitSubject(m, opts.SetID, rendered), outcome, snap)
	if err != nil {
		return nil, err
	}
	// gate-blocked and abandoned record no episode: an episode means the
	// composition has been refined, and a pass that fixed nothing (or whose
	// edits were discarded) has not (ADR-0248 decision 15).
	if outcome == refineOutcomeRefined {
		recordRefineEpisode(d, opts.Output, refineEpisodeRecord(opts.Repo, opts.SetID, workSHA, refineComposition(m), path, at))
	}
	printRefineWritten(opts.Output, opts.SetID, path, commitSHA, hasPrevious)
	return &RefineResult{SetID: opts.SetID, WorkSHA: workSHA, Path: path, CommitSHA: commitSHA, Body: doc}, nil
}

// showLatestRefine prints the set's current Refine report to the output
// verbatim — no chrome, no ANSI — because the whole point of `--show` is that
// its stdout is pasteable into a pull request.
func showLatestRefine(d *Deps, opts refineCoreOptions, m *Manifest) (*RefineResult, error) {
	doc, ok := refineReport.latestDocument(d, m.Dir)
	if !ok {
		return nil, exitErr(ExitSetup, "task set %q has no refine report yet; run `pop tasks refine %s` first", opts.SetID, opts.SetID)
	}
	if opts.Output != nil {
		fmt.Fprint(opts.Output, ensureTrailingNewline(doc.Body))
	}
	return &RefineResult{SetID: opts.SetID, Path: doc.Path, Body: doc.Body}, nil
}

// refinableWork is the one gate on a refine pass: there must be finished agent
// work and a range naming it. A set with no done AFK task has nothing whose
// standards could be judged, and an empty or undetermined range means pop
// cannot say which commits are the set's — refining a guessed range would
// judge other people's code. Nothing else refuses: a set mid-drain, a set
// parked at VERIFY-FAILED and a set already signed off are all refinable.
func refinableWork(d *Deps, opts refineCoreOptions, m *Manifest) (workDiffView, error) {
	if !hasDoneAFKTask(m) {
		return workDiffView{}, exitErr(ExitSetup, "task set %q has no done AFK task to refine", opts.SetID)
	}
	work := verifyWorkDiff(d, opts.RuntimePath, opts.SetID, m)
	if work.Undetermined {
		return workDiffView{}, exitErr(ExitSetup, "task set %q: %s", opts.SetID, rangeUndeterminedFindings)
	}
	if work.Empty() {
		return workDiffView{}, exitErr(ExitSetup, "task set %q has no committed work to refine", opts.SetID)
	}
	return work, nil
}

// hasDoneAFKTask reports whether the set holds finished agent work. HITL tasks
// are not it: a human sign-off leaves no changeset behind for a Refiner to read.
func hasDoneAFKTask(m *Manifest) bool {
	if m == nil {
		return false
	}
	for _, task := range m.Tasks {
		if task.Type == "AFK" && task.Status == TaskDone {
			return true
		}
	}
	return false
}

// resolveImplementationConvention asks the seam for the repository's
// `implementation` convention — a labelled block in the Refiner's prompt
// (ADR-0246). It is best-effort by design: the stack always answers, so an empty
// result means no seam was wired or resolution failed, and the Refiner then runs
// on pop's own prompt alone rather than the run failing.
func resolveImplementationConvention(opts refineCoreOptions) string {
	if opts.Convention == nil {
		return ""
	}
	prose, err := opts.Convention(opts.RuntimePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}

// resolveRefineOverlay asks the seam for the `refine` Overlay — constraints
// appended to pop's own procedure (ADR-0247). Best-effort like the convention
// seam: nothing wired or nothing on disk means the prompt carries no block.
func resolveRefineOverlay(opts refineCoreOptions) string {
	if opts.Overlay == nil {
		return ""
	}
	prose, err := opts.Overlay(opts.RuntimePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}

// runRefiner builds the prompt and invokes the Refiner, returning the document
// and the agent that wrote it.
func runRefiner(d *Deps, cfg *config.Config, opts refineCoreOptions, m *Manifest, workSHA string, work workDiffView, convention, overlay string, previous passDocument, hasPrevious bool) (string, string, error) {
	text := buildRefinerPrompt(d, m, work, convention, overlay, previous, hasPrevious)
	run := opts.runRefiner
	if run == nil {
		sel, err := resolveRefiner(opts.Agents, opts.Effort, m, cfg)
		if err != nil {
			return "", "", err
		}
		run = func(prompt string) (string, string, error) {
			return runConfiguredRefiner(d, cfg, sel, m.Dir, opts.SetID, workSHA, opts.RuntimePath, prompt, opts.Output, opts.Timeout, opts.probeMemo)
		}
	}
	body, agent, err := run(text)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", "", exitErr(ExitOperational, "the Refiner produced no document for %q", opts.SetID)
	}
	return body, agent, nil
}

// resolveRefiner applies the Refiner precedence chain (ADR-0240), highest
// first: CLI flags → the per-set manifest `refiner` override → [work.refine] →
// [work.implement].agents / DefaultRefineEffort. Agents and effort resolve
// independently, the way the Verifier's do.
//
// The chain never consults which agents actually implemented the set's tasks:
// the Refiner is resolved from the human's configuration alone, so it is a
// fresh agent by construction rather than the implementing session asked a
// second question.
//
// Like the Verifier's, the fallthrough to [work.implement].agents is disabled by
// an override of `agents = []`, which resolves to an error rather than a
// selection (ADR-0202 decision 6).
func resolveRefiner(cliAgents []string, cliEffort string, m *Manifest, cfg *config.Config) (verifierSelection, error) {
	agents := nonEmptyStrings(cliAgents)
	effort := strings.TrimSpace(cliEffort)

	if over := m.RefinerOverride(); over != nil {
		if len(agents) == 0 {
			agents = nonEmptyStrings(over.Agents)
		}
		if effort == "" {
			effort = strings.TrimSpace(over.Effort)
		}
	}
	if r := cfg.RefineSettings(); r != nil && effort == "" {
		effort = strings.TrimSpace(r.Effort)
	}
	if len(agents) == 0 {
		resolved, err := ResolveAgentGroupPresets(cfg.RefineAgentList(), cfg)
		if err != nil {
			return verifierSelection{}, err
		}
		agents = resolved
	}
	if effort == "" {
		effort = DefaultRefineEffort
	}
	return verifierSelection{Agents: agents, Effort: effort}, nil
}

// runConfiguredRefiner walks the resolved Refiner agent list at the resolved
// effort through the shared fallback walk, and returns the report beside the
// agent that wrote it. The Refiner's cap and retry schedule are the built-in
// task defaults: [work.refine] carries no retry keys, because a report that
// cannot be produced this time costs nothing to ask for again.
func runConfiguredRefiner(d *Deps, cfg *config.Config, sel verifierSelection, taskSetDir, setID, workSHA, runtimePath, prompt string, out io.Writer, timeout time.Duration, probeMemo *agentAvailabilityProbeMemo) (string, string, error) {
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}
	quotaRetryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return "", "", exitErr(ExitSetup, "%v", err)
	}
	walked, err := runAgentFallbackWalk(d, agentFallbackWalk{
		role:            refinerRole(d, out, taskSetDir, setID, workSHA),
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
	// Prose from an attempt that timed out or died is half a report, and writing
	// it would supersede a complete earlier document with it. Once the cap is
	// spent the set keeps the report it already had.
	if strings.TrimSpace(walked.Answer) != "" && !walked.AnswerRetryEligible {
		return walked.Answer, walked.Agent, nil
	}
	if len(walked.Unavailable) == 0 {
		return "", "", exitErr(ExitOperational, "the Refiner produced no document")
	}
	return "", "", exitErr(ExitSetup, "%s", formatHumanHealingExhaustionMessage(walked.Unavailable))
}

// refinerRole is what the shared fallback walk calls the Refiner: its name in
// the operator's output, the Captured run pair each invocation is filed as under
// the `refine` phase label, and the rule for which attempts are worth retrying —
// a refine pass has no format to parse, so any prose from a run that reached its
// own ending is the Refiner answering.
//
// It carries no read-only posture. A Refiner fixes what the convention licenses
// (ADR-0240), so it is spawned with the same argv as any other writing role, and
// what it may change is stated in its prompt rather than withheld from its
// tools. The posture capability stays declared on every preset for the next
// read-only role that needs it.
//
// Its runs are filed exactly as the Verifier's are, and for the same reason: a
// Refiner spends the same agent quota on the same set, so hiding it would make
// `pop tasks spend` understate what a drain cost. No verdict rides along,
// because Refine reaches none.
func refinerRole(d *Deps, errOut io.Writer, taskSetDir, setID, workSHA string) agentRole {
	return agentRole{
		Noun:   "Refiner agent",
		Gerund: "Refining",
		Phase:  spendPhaseRefine,
		Persist: func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int) {
			_ = persistRefineRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, try, outcome, reason, exitCode)
		},
		PersistAnswer: func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int, _ string) {
			_ = persistRefineRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, try, outcome, reason, exitCode)
		},
		PersistSkipped: func(rec *streamRecorder, invocation *AgentInvocation, model string, try int, reason string, exitCode int) {
			_ = persistSkippedRefineRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, model, try, reason, exitCode)
		},
		RetryEligible: refineAttemptRetryEligible,
	}
}

// refineAttemptRetryEligible reports whether a Refiner invocation should be
// retried on the current preset. It reads the Verifier's rule with the verdict
// parse taken out: a refine pass has no format, so the ending is all there is
// to judge. A run that timed out, could not be launched, or exited non-zero was
// cut off mid-thought, and the prose it left is a fragment rather than a
// shorter report — retrying costs one more attempt, while accepting it would
// write that fragment over the set's last complete document. A clean run that
// said nothing at all is retried for the same reason it is under the Verifier.
func refineAttemptRetryEligible(outcome *attemptOutcome, raw string) bool {
	if outcome != nil && (outcome.timedOut || outcome.runErr != nil || outcome.exitCode != 0) {
		return true
	}
	return strings.TrimSpace(raw) == ""
}

// refineEnabled reports whether automatic Refine is enabled in user config
// (ADR-0240). Like Agent verification's switch it defaults off, and it gates only
// the drain's own refine phase: `pop tasks refine <set>` is a human asking, and
// runs whatever this says.
func refineEnabled(cfg *config.Config) bool {
	return cfg.RefineSettings() != nil && cfg.RefineSettings().Enabled
}

// RefineFileInstant reads a Refine report's name back as the instant it was
// written, and false for anything else in the directory. It is exported so the
// Task-set Work kind can use the report's own timestamp as its Artifact
// ordering key instead of duplicating the filename contract.
func RefineFileInstant(name string) (time.Time, bool) {
	return refineReport.fileInstant(name)
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// printRefineWritten tells the operator where the report went and how to read
// it. It says nothing about what the pass found: the report is the whole
// output, and a summary line here would be a verdict by another name.
func printRefineWritten(w io.Writer, setID, path, commitSHA string, superseded bool) {
	if w == nil {
		return
	}
	out := outputFor(w)
	out.line(ansiBold, "━━ Refine for %s", setID)
	out.line(ansiDim, "   Document: %s", path)
	if commitSHA != "" {
		out.line(ansiDim, "   Refine commit: %s", ShortSHA(commitSHA))
	}
	if superseded {
		out.line(ansiDim, "   Supersedes the previous report; earlier documents are kept.")
	}
	out.line(ansiDim, "   Read it: pop tasks refine %s --show", setID)
}

// buildRefinerPrompt assembles the Refiner's prompt end to end (ADR-0246,
// ADR-0247): pop owns the role framing (including the fix licence), the labelled
// `implementation` convention block, and the output expectation. Around them sit
// the previous report when one exists and the changeset's shape — its commit
// range and complete `git diff --stat`, never the diff bodies. The Refiner
// stands in the checkout it describes and reads the changed files itself, which
// is the deliberate divergence from how the Verifier is prompted: naming,
// structure and idiom cannot be judged from a file-name-and-linecount table
// (ADR-0214).
func buildRefinerPrompt(d *Deps, m *Manifest, work workDiffView, convention, overlay string, previous passDocument, hasPrevious bool) string {
	view := refinerPromptView{
		TaskSet:   m.Stem,
		WorkRange: work.Range,
		WorkStat:  work.Stat,
		Tasks:     refinerTaskRows(d, m),
	}
	if convention != "" {
		view.ConventionRecorded = true
		view.Convention = convention
	}
	if overlay != "" {
		view.OverlayRecorded = true
		view.Overlay = overlay
	}
	// The set's recorded Commit convention is what the Refiner renders its own
	// commit's subject under. A set without one is asked for no subject at all,
	// and its pass commits under pop's default format instead (ADR-0240).
	if commits := strings.TrimSpace(m.CommitConvention); commits != "" {
		view.CommitConventionRecorded = true
		view.CommitConvention = commits
	}
	if hasPrevious && strings.TrimSpace(previous.Body) != "" {
		view.PreviousRecorded = true
		view.Previous = strings.TrimSpace(previous.Body)
	}
	if spec, ok := readSpec(d, m); ok {
		view.SpecRecorded = true
		view.Spec = spec
	}
	return prompt.MustRender(promptTemplates, "refiner.tmpl.md", view)
}

// refinerPromptView is what the Refiner's template renders against. Like the
// Verifier's, every optional section is a named boolean beside the text it
// guards, so the template picks a whole section and never asks why a field is
// empty.
type refinerPromptView struct {
	TaskSet            string
	ConventionRecorded bool
	Convention         string
	OverlayRecorded    bool
	Overlay            string
	PreviousRecorded   bool
	Previous           string
	SpecRecorded       bool
	Spec               string
	Tasks              []refinerTaskRow
	WorkRange          string
	WorkStat           string
	// CommitConventionRecorded and CommitConvention carry the set's recorded
	// Commit convention, the prose the Refiner renders its commit subject under.
	CommitConventionRecorded bool
	CommitConvention         string
}

// refinerTaskRow is one line of what the set set out to do. The titles are what
// the Refiner weighs the changeset against when its standard asks whether the
// code does what was asked; the task bodies stay out because the checkboxes are
// the Verifier's evidence, and the Refiner reads the changed files instead.
type refinerTaskRow struct {
	ID    string
	Title string
}

// refinerTaskRows lists the set's done AFK work by title alone.
func refinerTaskRows(d *Deps, m *Manifest) []refinerTaskRow {
	var rows []refinerTaskRow
	for _, task := range m.Tasks {
		if task.Type != "AFK" || task.Status != TaskDone {
			continue
		}
		rows = append(rows, refinerTaskRow{ID: task.ID, Title: task.Title})
	}
	return rows
}
