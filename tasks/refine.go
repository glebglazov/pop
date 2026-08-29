package tasks

import (
	"fmt"
	"io"
	"path/filepath"
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

// refineDirName is the sub-directory of a set's folder where Refine reports
// accumulate. It sits under the Task-set directory, which lives in pop's Work
// store rather than in the repository, so no report can ever be staged into a
// commit (ADR-0214).
const refineDirName = "refine"

// refineFilePrefix and refineFileTimeLayout spell a Refine report's name. The
// instant is in the file name rather than only in the document, because "the
// latest" is resolved by timestamp and a directory listing must be able to
// answer that without opening every file.
const (
	refineFilePrefix     = "refine-"
	refineFileTimeLayout = "20060102T150405Z"
)

// RefineConvention resolves this repository's `refine` Convention stack as
// the prose a Refiner is handed. It is a seam rather than a direct call: the
// conventions package answers Repository identity through this one, so tasks
// cannot import it back. A nil seam, an error, or an empty answer all mean the
// same thing here — the prompt carries no convention and says so.
type RefineConvention func(cwd string) (string, error)

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
	// Convention resolves the `refine` convention for the checkout.
	Convention RefineConvention
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
	Convention RefineConvention
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
	previous, hasPrevious := latestRefineDocument(d, m.Dir)
	convention := resolveRefineConvention(opts)
	// The work SHA is read before the Refiner runs: it is the commit the report
	// describes, and the Refiner that moved it would be doing something a refine
	// pass must not do.
	workSHA := verifyWorkSHA(d, opts.RuntimePath)
	body, agent, err := runRefiner(d, cfg, opts, m, workSHA, work, convention, previous, hasPrevious)
	if err != nil {
		return nil, err
	}
	at := d.Now().UTC()
	doc := renderRefineDocument(at, opts.SetID, workSHA, work.Range, agent, body)
	path, err := writeRefineDocument(d, m.Dir, at, doc)
	if err != nil {
		return nil, err
	}
	recordRefineEpisode(d, opts.Output, refineEpisodeRecord(opts.Repo, opts.SetID, workSHA, refineComposition(m), path, at))
	printRefineWritten(opts.Output, opts.SetID, path, hasPrevious)
	return &RefineResult{SetID: opts.SetID, WorkSHA: workSHA, Path: path, Body: doc}, nil
}

// showLatestRefine prints the set's current Refine report to the output
// verbatim — no chrome, no ANSI — because the whole point of `--show` is that
// its stdout is pasteable into a pull request.
func showLatestRefine(d *Deps, opts refineCoreOptions, m *Manifest) (*RefineResult, error) {
	doc, ok := latestRefineDocument(d, m.Dir)
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

// resolveRefineConvention asks the seam for the repository's `refine`
// convention — the body of the Refiner's prompt (ADR-0227). It is best-effort
// by design: the stack always answers, so an empty result means no seam was
// wired or resolution failed, and the Refiner then runs on pop's frame alone
// rather than the run failing.
func resolveRefineConvention(opts refineCoreOptions) string {
	if opts.Convention == nil {
		return ""
	}
	prose, err := opts.Convention(opts.RuntimePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}

// runRefiner builds the prompt and invokes the Refiner, returning the document
// and the agent that wrote it.
func runRefiner(d *Deps, cfg *config.Config, opts refineCoreOptions, m *Manifest, workSHA string, work workDiffView, convention string, previous refineDocument, hasPrevious bool) (string, string, error) {
	text := buildRefinerPrompt(d, m, work, convention, previous, hasPrevious)
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

// refineDocument is one Refine report on disk: where it is, when it was
// written, and what it says.
type refineDocument struct {
	Path string
	At   time.Time
	Body string
}

// latestRefineDocument returns the set's current report — the one with the
// newest timestamp in its name — and false when the set has never been refined.
// Every earlier document stays where it is; superseding is a matter of which one
// the readers take, not of deleting the ones before it (ADR-0214).
func latestRefineDocument(d *Deps, setDir string) (refineDocument, bool) {
	dir := filepath.Join(setDir, refineDirName)
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		return refineDocument{}, false
	}
	var found refineDocument
	ok := false
	for _, entry := range entries {
		at, isRefine := RefineFileInstant(entry.Name())
		if entry.IsDir() || !isRefine {
			continue
		}
		if ok && !at.After(found.At) {
			continue
		}
		found, ok = refineDocument{Path: filepath.Join(dir, entry.Name()), At: at}, true
	}
	if !ok {
		return refineDocument{}, false
	}
	body, err := d.FS.ReadFile(found.Path)
	if err != nil {
		return refineDocument{}, false
	}
	found.Body = strings.TrimSpace(string(body))
	return found, true
}

// RefineFileInstant reads a Refine report's name back as the instant it was
// written, and false for anything else in the directory. It is exported so the
// Task-set Work kind can use the report's own timestamp as its Artifact
// ordering key instead of duplicating the filename contract.
func RefineFileInstant(name string) (time.Time, bool) {
	stamp, ok := strings.CutPrefix(name, refineFilePrefix)
	if !ok {
		return time.Time{}, false
	}
	stamp, ok = strings.CutSuffix(stamp, ".md")
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse(refineFileTimeLayout, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

// writeRefineDocument files a new Refine report under the set's refine/
// directory and returns its path. A name already taken (two reports inside one
// second) advances the instant rather than overwriting: retention is the whole
// point of the directory, and the names are also the ordering.
func writeRefineDocument(d *Deps, setDir string, at time.Time, body string) (string, error) {
	dir := filepath.Join(setDir, refineDirName)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return "", exitErr(ExitOperational, "create refine directory: %v", err)
	}
	at = at.UTC().Truncate(time.Second)
	taken := map[string]bool{}
	if entries, err := d.FS.ReadDir(dir); err == nil {
		for _, entry := range entries {
			taken[entry.Name()] = true
		}
	}
	name := refineFilePrefix + at.Format(refineFileTimeLayout) + ".md"
	for taken[name] {
		at = at.Add(time.Second)
		name = refineFilePrefix + at.Format(refineFileTimeLayout) + ".md"
	}
	path := filepath.Join(dir, name)
	if err := d.FS.WriteFile(path, []byte(ensureTrailingNewline(body)), 0o644); err != nil {
		return "", exitErr(ExitOperational, "write refine report: %v", err)
	}
	return path, nil
}

// renderRefineDocument wraps the Refiner's prose in the four facts a reader
// needs to know what was refined: which set, at which commits, by whom and
// when. They ride the document rather than a side-car because the document
// leaves pop the moment a human pipes it somewhere.
//
// The Work SHA line is the report's own answer to which tree it describes: the
// pass's edits sit on top of that commit, so a reader who wants the state the
// report was written against checks out the SHA and applies nothing else. It is
// stamped here rather than asked of the Refiner, because pop read it before the
// pass began and the Refiner would only be copying it back.
func renderRefineDocument(at time.Time, setID, workSHA, commitRange, agent, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Refine report — %s\n\n", setID)
	fmt.Fprintf(&b, "- Refined: %s\n", at.UTC().Format(time.RFC3339))
	if commitRange != "" {
		fmt.Fprintf(&b, "- Commit range: %s\n", commitRange)
	}
	if workSHA != "" {
		fmt.Fprintf(&b, "- Work SHA: %s\n", ShortSHA(workSHA))
	}
	if strings.TrimSpace(agent) != "" {
		fmt.Fprintf(&b, "- Refiner: %s\n", strings.TrimSpace(agent))
	}
	fmt.Fprintf(&b, "\n%s", ensureTrailingNewline(strings.TrimSpace(body)))
	return b.String()
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
func printRefineWritten(w io.Writer, setID, path string, superseded bool) {
	if w == nil {
		return
	}
	out := outputFor(w)
	out.line(ansiBold, "━━ Refine for %s", setID)
	out.line(ansiDim, "   Document: %s", path)
	if superseded {
		out.line(ansiDim, "   Supersedes the previous report; earlier documents are kept.")
	}
	out.line(ansiDim, "   Read it: pop tasks refine %s --show", setID)
}

// buildRefinerPrompt assembles the Refiner's input as an envelope (ADR-0227):
// pop's role framing, then the resolved `refine` convention as the body,
// then the output expectation pop needs back. Around the body sit the previous
// report when one exists and the changeset's shape — its commit range and
// complete `git diff --stat`, never the diff bodies. The Refiner stands in the
// checkout it describes and reads the changed files itself, which is the
// deliberate divergence from how the Verifier is prompted: naming, structure and
// idiom cannot be judged from a file-name-and-linecount table (ADR-0214).
func buildRefinerPrompt(d *Deps, m *Manifest, work workDiffView, convention string, previous refineDocument, hasPrevious bool) string {
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
	PreviousRecorded   bool
	Previous           string
	SpecRecorded       bool
	Spec               string
	Tasks              []refinerTaskRow
	WorkRange          string
	WorkStat           string
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
