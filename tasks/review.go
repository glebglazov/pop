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

// DefaultReviewEffort is the model-strength tier the Reviewer runs at when
// neither a CLI flag nor [work.review].effort names one (ADR-0214): judging
// naming, structure and idiom against prose standards is the strongest tier's
// work, so review defaults where verification does.
const DefaultReviewEffort = "heavy"

// reviewsDirName is the sub-directory of a set's folder where Review artifacts
// accumulate. It sits under the Task-set directory, which lives in pop's Work
// store rather than in the repository, so no review can ever be staged into a
// commit (ADR-0214).
const reviewsDirName = "reviews"

// reviewFilePrefix and reviewFileTimeLayout spell a Review artifact's name. The
// instant is in the file name rather than only in the document, because "the
// latest" is resolved by timestamp and a directory listing must be able to
// answer that without opening every file.
const (
	reviewFilePrefix     = "review-"
	reviewFileTimeLayout = "20060102T150405Z"
)

// ReviewConvention resolves this repository's `code-review` Convention stack as
// the prose a Reviewer is handed. It is a seam rather than a direct call: the
// conventions package answers Repository identity through this one, so tasks
// cannot import it back. A nil seam, an error, or an empty answer all mean the
// same thing here — the prompt carries no convention and says so.
type ReviewConvention func(cwd string) (string, error)

// ReviewOptions configures a `pop tasks review <set>` run.
type ReviewOptions struct {
	ResolveInput ResolveInput
	// TaskSetID is the bare Task-set identifier to review.
	TaskSetID string
	// Agents is the ordered CLI Reviewer fallback list (`--agent`, repeatable).
	// Empty ⇒ resolution falls through to [work.review].agents, then
	// [work.implement].agents.
	Agents []string
	// Effort is the CLI Reviewer effort override (`--effort`). Empty ⇒ config,
	// then DefaultReviewEffort.
	Effort string
	// Timeout bounds the single Reviewer attempt. Zero uses DefaultAttemptTimeout.
	Timeout time.Duration
	// Output receives the live agent stream and the run's chrome.
	Output io.Writer
	// Show prints the set's latest Review artifact and runs nothing — the path a
	// document takes to a pull request, where piping is the human's business.
	Show bool
	// Convention resolves the `code-review` convention for the checkout.
	Convention ReviewConvention
}

// ReviewResult is the outcome of one review.
type ReviewResult struct {
	SetID   string
	WorkSHA string
	// Path is the Review artifact written (or read, under Show).
	Path string
	// Body is the document itself.
	Body string
}

// reviewCoreOptions carries the already-resolved inputs to the core review
// routine, so tests can drive it without real path-resolution git calls. The
// runReviewer seam replaces the real agent spawn in tests.
type reviewCoreOptions struct {
	DefPath     string
	RuntimePath string
	// Repo is the repository identity the set's Review episode is keyed by (the
	// git common dir). Empty resolves nothing and records no episode, so a review
	// still runs and still writes its document in a checkout pop cannot identify.
	Repo       string
	SetID      string
	Agents     []string
	Effort     string
	Timeout    time.Duration
	Output     io.Writer
	Show       bool
	Convention ReviewConvention
	// runReviewer returns the Reviewer's document and the agent that wrote it.
	runReviewer func(prompt string) (string, string, error)
	probeMemo   *agentAvailabilityProbeMemo
}

// ReviewTaskSet reviews a set using default dependencies.
func ReviewTaskSet(opts ReviewOptions) (*ReviewResult, error) {
	return ReviewTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// ReviewTaskSetWith is `pop tasks review <set>` (ADR-0214): it resolves the set,
// hands a fresh Reviewer pop's review instruction, the resolved `code-review`
// convention and the set's previous Review artifact, and writes what comes back
// as the set's current one. It reaches no verdict, writes no status, and holds
// no lock — review is deliberately runnable mid-drain, where a standards
// correction is worth most.
func ReviewTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts ReviewOptions) (*ReviewResult, error) {
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
	return reviewResolvedSet(d, cfg, reviewCoreOptions{
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
	})
}

// reviewResolvedSet is the resolved-path core of `pop tasks review`. Under Show
// it reads and prints; otherwise it refuses anything it cannot review, runs the
// Reviewer, and supersedes the set's current document with what came back.
func reviewResolvedSet(d *Deps, cfg *config.Config, opts reviewCoreOptions) (*ReviewResult, error) {
	m, err := loadVerifiableManifest(d, verifyCoreOptions{SetID: opts.SetID, DefPath: opts.DefPath})
	if err != nil {
		return nil, err
	}
	if opts.Show {
		return showLatestReview(d, opts, m)
	}
	work, err := reviewableWork(d, opts, m)
	if err != nil {
		return nil, err
	}
	previous, hasPrevious := latestReviewDocument(d, m.Dir)
	convention := resolveReviewConvention(opts)
	// The work SHA is read before the Reviewer runs: it is the commit the document
	// describes, and the Reviewer that moved it would be doing something a review
	// must not do.
	workSHA := verifyWorkSHA(d, opts.RuntimePath)
	body, agent, err := runReviewer(d, cfg, opts, m, workSHA, work, convention, previous, hasPrevious)
	if err != nil {
		return nil, err
	}
	at := d.Now().UTC()
	doc := renderReviewDocument(at, opts.SetID, workSHA, work.Range, agent, body)
	path, err := writeReviewDocument(d, m.Dir, at, doc)
	if err != nil {
		return nil, err
	}
	recordReviewEpisode(d, opts.Output, reviewEpisodeRecord(opts.Repo, opts.SetID, workSHA, reviewComposition(m), path, at))
	printReviewWritten(opts.Output, opts.SetID, path, hasPrevious)
	return &ReviewResult{SetID: opts.SetID, WorkSHA: workSHA, Path: path, Body: doc}, nil
}

// showLatestReview prints the set's current Review artifact to the output
// verbatim — no chrome, no ANSI — because the whole point of `--show` is that
// its stdout is pasteable into a pull request.
func showLatestReview(d *Deps, opts reviewCoreOptions, m *Manifest) (*ReviewResult, error) {
	doc, ok := latestReviewDocument(d, m.Dir)
	if !ok {
		return nil, exitErr(ExitSetup, "task set %q has no review yet; run `pop tasks review %s` first", opts.SetID, opts.SetID)
	}
	if opts.Output != nil {
		fmt.Fprint(opts.Output, ensureTrailingNewline(doc.Body))
	}
	return &ReviewResult{SetID: opts.SetID, Path: doc.Path, Body: doc.Body}, nil
}

// reviewableWork is the one gate on a review: there must be finished agent work
// and a range naming it. A set with no done AFK task has nothing whose standards
// could be judged, and an empty or undetermined range means pop cannot say which
// commits are the set's — reviewing a guessed range would judge other people's
// code. Nothing else refuses: a set mid-drain, a set parked at VERIFY-FAILED and
// a set already signed off are all reviewable.
func reviewableWork(d *Deps, opts reviewCoreOptions, m *Manifest) (workDiffView, error) {
	if !hasDoneAFKTask(m) {
		return workDiffView{}, exitErr(ExitSetup, "task set %q has no done AFK task to review", opts.SetID)
	}
	work := verifyWorkDiff(d, opts.RuntimePath, opts.SetID, m)
	if work.Undetermined {
		return workDiffView{}, exitErr(ExitSetup, "task set %q: %s", opts.SetID, rangeUndeterminedFindings)
	}
	if work.Empty() {
		return workDiffView{}, exitErr(ExitSetup, "task set %q has no committed work to review", opts.SetID)
	}
	return work, nil
}

// hasDoneAFKTask reports whether the set holds finished agent work. HITL tasks
// are not it: a human sign-off leaves no changeset behind for a Reviewer to read.
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

// resolveReviewConvention asks the seam for the repository's `code-review`
// convention. It is best-effort by design: pop asserts no house standard, so a
// repository that has not derived one is reviewed against its own idiom and the
// prompt says exactly that rather than failing the run.
func resolveReviewConvention(opts reviewCoreOptions) string {
	if opts.Convention == nil {
		return ""
	}
	prose, err := opts.Convention(opts.RuntimePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}

// runReviewer builds the prompt and invokes the Reviewer, returning the document
// and the agent that wrote it.
func runReviewer(d *Deps, cfg *config.Config, opts reviewCoreOptions, m *Manifest, workSHA string, work workDiffView, convention string, previous reviewDocument, hasPrevious bool) (string, string, error) {
	text := buildReviewerPrompt(d, m, work, convention, previous, hasPrevious)
	run := opts.runReviewer
	if run == nil {
		sel := resolveReviewer(opts.Agents, opts.Effort, cfg)
		run = func(prompt string) (string, string, error) {
			return runConfiguredReviewer(d, cfg, sel, m.Dir, opts.SetID, workSHA, opts.RuntimePath, prompt, opts.Output, opts.Timeout, opts.probeMemo)
		}
	}
	body, agent, err := run(text)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", "", exitErr(ExitOperational, "the Reviewer produced no document for %q", opts.SetID)
	}
	return body, agent, nil
}

// resolveReviewer applies the Reviewer precedence chain (ADR-0214), highest
// first: CLI flags → [work.review] → [work.implement].agents / DefaultReviewEffort.
// Agents and effort resolve independently, the way the Verifier's do.
//
// The chain never consults which agents actually implemented the set's tasks:
// the Reviewer is resolved from the human's configuration alone, so it is a
// fresh agent by construction rather than the implementing session asked a
// second question.
func resolveReviewer(cliAgents []string, cliEffort string, cfg *config.Config) verifierSelection {
	agents := nonEmptyStrings(cliAgents)
	effort := strings.TrimSpace(cliEffort)

	if r := cfg.ReviewSettings(); r != nil {
		if len(agents) == 0 {
			agents = r.Agents.Commands()
		}
		if effort == "" {
			effort = strings.TrimSpace(r.Effort)
		}
	}
	if len(agents) == 0 {
		agents = ResolveDefaultAgentPresets(nil, "", false, cfg)
	}
	if effort == "" {
		effort = DefaultReviewEffort
	}
	return verifierSelection{Agents: agents, Effort: effort}
}

// runConfiguredReviewer walks the resolved Reviewer agent list at the resolved
// effort through the shared fallback walk, and returns the document beside the
// agent that wrote it. The Reviewer's cap and retry schedule are the built-in
// task defaults: [work.review] carries no retry keys, because a review that
// cannot be produced this time costs nothing to ask for again.
func runConfiguredReviewer(d *Deps, cfg *config.Config, sel verifierSelection, taskSetDir, setID, workSHA, runtimePath, prompt string, out io.Writer, timeout time.Duration, probeMemo *agentAvailabilityProbeMemo) (string, string, error) {
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}
	quotaRetryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return "", "", exitErr(ExitSetup, "%v", err)
	}
	walked, err := runAgentFallbackWalk(d, agentFallbackWalk{
		role:            reviewerRole(d, out, taskSetDir, setID, workSHA),
		sel:             sel,
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
	if strings.TrimSpace(walked.Answer) != "" {
		return walked.Answer, walked.Agent, nil
	}
	if len(walked.Unavailable) == 0 {
		return "", "", exitErr(ExitOperational, "the Reviewer produced no document")
	}
	return "", "", exitErr(ExitSetup, "%s", formatHumanHealingExhaustionMessage(walked.Unavailable))
}

// reviewerRole is what the shared fallback walk calls the Reviewer: its name in
// the operator's output, the Captured run pair each invocation is filed as under
// the `review` phase label, and the rule that the only failed attempt is one
// that came back with nothing to write down — a review has no format to parse,
// so any prose is the Reviewer answering.
//
// Its runs are filed exactly as the Verifier's are, and for the same reason: a
// Reviewer spends the same agent quota on the same set, so hiding it would make
// `pop tasks spend` understate what a drain cost. No verdict rides along,
// because a review reaches none.
func reviewerRole(d *Deps, errOut io.Writer, taskSetDir, setID, workSHA string) agentRole {
	return agentRole{
		Noun:   "Reviewer agent",
		Gerund: "Reviewing",
		Persist: func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int) {
			_ = persistReviewRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, try, outcome, reason, exitCode)
		},
		PersistAnswer: func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int, _ string) {
			_ = persistReviewRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, try, outcome, reason, exitCode)
		},
		PersistSkipped: func(rec *streamRecorder, invocation *AgentInvocation, model string, try int, reason string, exitCode int) {
			_ = persistSkippedReviewRun(d, errOut, taskSetDir, setID, workSHA, rec, invocation.AgentPreset(), invocation.RequestedAgent, model, try, reason, exitCode)
		},
		RetryEligible: func(_ *attemptOutcome, raw string) bool { return strings.TrimSpace(raw) == "" },
	}
}

// reviewEnabled reports whether automatic Code review is enabled in user config
// (ADR-0214). Like Agent verification's switch it defaults off, and it gates only
// the drain's own review phase: `pop tasks review <set>` is a human asking, and
// runs whatever this says.
func reviewEnabled(cfg *config.Config) bool {
	return cfg.ReviewSettings() != nil && cfg.ReviewSettings().Enabled
}

// reviewDocument is one Review artifact on disk: where it is, when it was
// written, and what it says.
type reviewDocument struct {
	Path string
	At   time.Time
	Body string
}

// latestReviewDocument returns the set's current review — the artifact with the
// newest timestamp in its name — and false when the set has never been reviewed.
// Every earlier document stays where it is; superseding is a matter of which one
// the readers take, not of deleting the ones before it (ADR-0214).
func latestReviewDocument(d *Deps, setDir string) (reviewDocument, bool) {
	dir := filepath.Join(setDir, reviewsDirName)
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		return reviewDocument{}, false
	}
	var found reviewDocument
	ok := false
	for _, entry := range entries {
		at, isReview := reviewFileInstant(entry.Name())
		if entry.IsDir() || !isReview {
			continue
		}
		if ok && !at.After(found.At) {
			continue
		}
		found, ok = reviewDocument{Path: filepath.Join(dir, entry.Name()), At: at}, true
	}
	if !ok {
		return reviewDocument{}, false
	}
	body, err := d.FS.ReadFile(found.Path)
	if err != nil {
		return reviewDocument{}, false
	}
	found.Body = strings.TrimSpace(string(body))
	return found, true
}

// reviewFileInstant reads a Review artifact's name back as the instant it was
// written, and false for anything else in the directory.
func reviewFileInstant(name string) (time.Time, bool) {
	stamp, ok := strings.CutPrefix(name, reviewFilePrefix)
	if !ok {
		return time.Time{}, false
	}
	stamp, ok = strings.CutSuffix(stamp, ".md")
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse(reviewFileTimeLayout, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

// writeReviewDocument files a new Review artifact under the set's reviews/
// directory and returns its path. A name already taken (two reviews inside one
// second) advances the instant rather than overwriting: retention is the whole
// point of the directory, and the names are also the ordering.
func writeReviewDocument(d *Deps, setDir string, at time.Time, body string) (string, error) {
	dir := filepath.Join(setDir, reviewsDirName)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return "", exitErr(ExitOperational, "create review directory: %v", err)
	}
	at = at.UTC().Truncate(time.Second)
	taken := map[string]bool{}
	if entries, err := d.FS.ReadDir(dir); err == nil {
		for _, entry := range entries {
			taken[entry.Name()] = true
		}
	}
	name := reviewFilePrefix + at.Format(reviewFileTimeLayout) + ".md"
	for taken[name] {
		at = at.Add(time.Second)
		name = reviewFilePrefix + at.Format(reviewFileTimeLayout) + ".md"
	}
	path := filepath.Join(dir, name)
	if err := d.FS.WriteFile(path, []byte(ensureTrailingNewline(body)), 0o644); err != nil {
		return "", exitErr(ExitOperational, "write review document: %v", err)
	}
	return path, nil
}

// renderReviewDocument wraps the Reviewer's prose in the four facts a reader
// needs to know what was reviewed: which set, at which commits, by whom and
// when. They ride the document rather than a side-car because the document
// leaves pop the moment a human pipes it somewhere.
func renderReviewDocument(at time.Time, setID, workSHA, commitRange, agent, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code review — %s\n\n", setID)
	fmt.Fprintf(&b, "- Reviewed: %s\n", at.UTC().Format(time.RFC3339))
	if commitRange != "" {
		fmt.Fprintf(&b, "- Commit range: %s\n", commitRange)
	}
	if workSHA != "" {
		fmt.Fprintf(&b, "- Work SHA: %s\n", ShortSHA(workSHA))
	}
	if strings.TrimSpace(agent) != "" {
		fmt.Fprintf(&b, "- Reviewer: %s\n", strings.TrimSpace(agent))
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

// printReviewWritten tells the operator where the document went and how to read
// it. It says nothing about what the review found: the document is the whole
// output, and a summary line here would be a verdict by another name.
func printReviewWritten(w io.Writer, setID, path string, superseded bool) {
	if w == nil {
		return
	}
	out := outputFor(w)
	out.line(ansiBold, "━━ Code review for %s", setID)
	out.line(ansiDim, "   Document: %s", path)
	if superseded {
		out.line(ansiDim, "   Supersedes the previous review; earlier documents are kept.")
	}
	out.line(ansiDim, "   Read it: pop tasks review %s --show", setID)
}

// buildReviewerPrompt assembles the Reviewer's input: pop's review instruction,
// the resolved `code-review` convention, the previous document when one exists,
// and the changeset's shape — its commit range and complete `git diff --stat`,
// never the diff bodies. The Reviewer stands in the checkout under review and
// reads the changed files itself, which is the deliberate divergence from how
// the Verifier is prompted: naming, structure and idiom cannot be judged from a
// file-name-and-linecount table (ADR-0214).
func buildReviewerPrompt(d *Deps, m *Manifest, work workDiffView, convention string, previous reviewDocument, hasPrevious bool) string {
	view := reviewerPromptView{
		TaskSet:   m.Stem,
		WorkRange: work.Range,
		WorkStat:  work.Stat,
		Tasks:     reviewerTaskRows(d, m),
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
	return prompt.MustRender(promptTemplates, "reviewer.tmpl.md", view)
}

// reviewerPromptView is what the Reviewer's template renders against. Like the
// Verifier's, every optional section is a named boolean beside the text it
// guards, so the template picks a whole section and never asks why a field is
// empty.
type reviewerPromptView struct {
	TaskSet            string
	ConventionRecorded bool
	Convention         string
	PreviousRecorded   bool
	Previous           string
	SpecRecorded       bool
	Spec               string
	Tasks              []reviewerTaskRow
	WorkRange          string
	WorkStat           string
}

// reviewerTaskRow is one line of what the set set out to do — an orientation
// listing, not a contract. A review judges how the code is written, not whether
// a criterion is met, so the task bodies are deliberately not inlined.
type reviewerTaskRow struct {
	ID    string
	Title string
}

// reviewerTaskRows lists the set's done AFK work by title alone.
func reviewerTaskRows(d *Deps, m *Manifest) []reviewerTaskRow {
	var rows []reviewerTaskRow
	for _, task := range m.Tasks {
		if task.Type != "AFK" || task.Status != TaskDone {
			continue
		}
		rows = append(rows, reviewerTaskRow{ID: task.ID, Title: task.Title})
	}
	return rows
}
