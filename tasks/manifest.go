package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The manifest's three enums, each in the order a reader wants them, plus the
// two file names and the acceptance-criteria heading. The validator's lookup
// maps and patterns are built from these, and the authoring guide prints the
// same values, so a printed rule is always the enforced one.
const (
	// ManifestFileName is the machine-readable half of a Task set, sitting beside
	// the task markdown it indexes.
	ManifestFileName = "index.json"
	// SpecFileName is the optional co-located enrichment file: the spec a set was
	// broken down from, and the one non-task markdown a set folder holds.
	SpecFileName = "spec.md"
	// legacySpecFileName is the retired name of the spec file. Nothing reads it —
	// there is no fallback — but a set folder that still carries one is not
	// malformed, on the same reasoning as the retired manifest keys (ADR-0115):
	// a rename must not turn every pre-rename set on a machine into a fix list.
	legacySpecFileName = "prd.md"
	// AcceptanceCriteriaHeading names the one section every task markdown must
	// carry, with at least one checkbox under it.
	AcceptanceCriteriaHeading = "Acceptance criteria"
	// manifestTasksKey is the manifest's only required top-level key.
	manifestTasksKey = "tasks"

	DefaultTaskEffort = "standard"
)

var (
	taskTypeOrder   = []string{"AFK", "HITL"}
	taskStatusOrder = []TaskStatus{TaskOpen, TaskDone, TaskFailed, TaskSkipped}
	taskEffortOrder = []string{"light", DefaultTaskEffort, "heavy"}

	allowedTaskTypes    = stringSet(taskTypeOrder)
	allowedTaskStatuses = statusSet(taskStatusOrder)
	allowedTaskEfforts  = stringSet(taskEffortOrder)

	acHeaderPattern = regexp.MustCompile(`(?i)^##\s+` + regexp.QuoteMeta(AcceptanceCriteriaHeading) + `\s*$`)
	checkboxPattern = regexp.MustCompile(`^-\s+\[[ xX]\]`)
)

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

func statusSet(values []TaskStatus) map[TaskStatus]bool {
	set := make(map[TaskStatus]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

// ValidEfforts returns the accepted effort tier names in ladder order.
func ValidEfforts() []string { return append([]string(nil), taskEffortOrder...) }

// IsValidEffort reports whether effort names an accepted model-strength tier.
func IsValidEffort(effort string) bool { return allowedTaskEfforts[effort] }

// Task represents one entry in an task manifest.
type Task struct {
	ID          string     `json:"id"`
	File        string     `json:"file"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	Status      TaskStatus `json:"status"`
	BlockedBy   []string   `json:"blocked_by"`
	FailedAfter *int       `json:"failed_after,omitempty"`
	// Effort selects the model-strength tier for this task. Missing manifests
	// resolve to DefaultTaskEffort; EffortExplicit records whether the key was
	// present so legacy manifests keep their previous invocation shape.
	Effort         string `json:"-"`
	EffortExplicit bool   `json:"-"`
	// Origin tags a Remediation task's provenance (ADR-0105): auto = Verifier
	// spawned on FIXABLE, human = spawned via the Remediate disposition. Empty on
	// non-remediation tasks and on legacy Remediation entries; an absent origin
	// reads as auto so old sets keep their prior depth-cap behavior. It rides
	// through as `origin`, omitted when empty so unrelated rewrites stay quiet.
	Origin string `json:"-"`
	// Commit records the implementation commit pop made for this task — its SHA
	// and the verbatim subject line it was committed with (ADR-0207). Together
	// with the set's BaseCommit it is what a later reader uses to find the set's
	// commit range, and the recorded subjects are the rewrite detector: a rebase
	// changes every SHA but keeps subjects, so a subject search recovers the range
	// a SHA lookup lost. Nil until the task's commit is made, and on tasks that
	// completed as a verified no-op (nothing to commit). A re-run of an already
	// committed task overwrites it: the latest commit is the reachable one. It
	// rides through as `commit`, omitted when nil.
	Commit *TaskCommit `json:"-"`
	// CommitSubject is the task's Planned commit subject: the final, literal
	// subject line the executor commits this task's work under, written at plan
	// time by whoever resolved the set's Commit convention (ADR-0207). It is used
	// **verbatim** — nothing renders, substitutes or reformats it — so the agent
	// that wrote it is the only author of the message. Empty means no subject was
	// planned, and the commit falls back to pop's built-in default format (see
	// CommitSubject, the function). It rides through as `commit_subject`, omitted
	// when empty.
	CommitSubject string `json:"-"`
}

// TaskCommit is the recorded identity of one implementation commit: the SHA it
// landed as, and the subject line it was written with, byte-for-byte as handed
// to git.
type TaskCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

type taskJSON struct {
	ID            string      `json:"id"`
	File          string      `json:"file"`
	Title         string      `json:"title"`
	Type          string      `json:"type"`
	Status        TaskStatus  `json:"status"`
	BlockedBy     []string    `json:"blocked_by"`
	FailedAfter   *int        `json:"failed_after,omitempty"`
	Effort        *string     `json:"effort,omitempty"`
	Origin        string      `json:"origin,omitempty"`
	Commit        *TaskCommit `json:"commit,omitempty"`
	CommitSubject string      `json:"commit_subject,omitempty"`
}

// UnmarshalJSON preserves the difference between an absent effort key and an
// explicit effort: "standard" while presenting both as standard to callers.
func (t *Task) UnmarshalJSON(data []byte) error {
	var raw taskJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.ID = raw.ID
	t.File = raw.File
	t.Title = raw.Title
	t.Type = raw.Type
	t.Status = raw.Status
	t.BlockedBy = raw.BlockedBy
	t.FailedAfter = raw.FailedAfter
	t.Origin = raw.Origin
	t.Commit = raw.Commit
	t.CommitSubject = raw.CommitSubject
	t.Effort = DefaultTaskEffort
	t.EffortExplicit = false
	if raw.Effort != nil {
		t.Effort = *raw.Effort
		t.EffortExplicit = true
	}
	return nil
}

// MarshalJSON omits effort unless it was explicitly present or set by code,
// avoiding churn when older manifests are rewritten for unrelated state.
func (t Task) MarshalJSON() ([]byte, error) {
	raw := taskJSON{
		ID:            t.ID,
		File:          t.File,
		Title:         t.Title,
		Type:          t.Type,
		Status:        t.Status,
		BlockedBy:     t.BlockedBy,
		FailedAfter:   t.FailedAfter,
		Origin:        t.Origin,
		Commit:        t.Commit,
		CommitSubject: t.CommitSubject,
	}
	if t.EffortExplicit || (t.Effort != "" && t.Effort != DefaultTaskEffort) {
		effort := t.Effort
		raw.Effort = &effort
	}
	return json.Marshal(raw)
}

// Manifest is a parsed and validated task manifest.
type Manifest struct {
	Stem    string
	Dir     string
	Path    string
	Tasks   []Task
	Raw     json.RawMessage
	Errors  []string
	Valid   bool
	Unknown map[string]json.RawMessage
	// SourceMap names the Map this set was spawned from, read from and written
	// back as the set-level `source_map` key. It is the set-side half of the one
	// lineage link pop keeps — the Map's own `spawned_sets` is the traversed half —
	// and it is recorded on every Map-sourced set, spec or no spec, so the link is
	// never half-built. Empty for a set with no Map behind it. Nothing derives from
	// it: `spec.md`'s `Source map:` line stays human-facing prose and is never
	// parsed.
	SourceMap string
	// BaseCommit is the Set base commit: the parent of the set's *first*
	// implementation commit, read from and written back as the set-level
	// `base_commit` key. It is what lets a later reader reconstruct the set's
	// commit range (`base..HEAD`) without grepping history for a subject format
	// (ADR-0207). It is recorded at first-commit time, not at set creation, so
	// commits that land between planning and draining stay outside the range, and
	// it is written exactly once — later commits never move it.
	//
	// The empty string with BaseCommitRecorded set is the root-commit edge: the
	// set's first implementation commit *is* the repository's first commit and has
	// no parent, persisted as an explicit JSON null (not the empty-tree hash, which
	// is not a commit and cannot stand on the left of a `base..HEAD` range). A
	// reader must therefore distinguish "no base recorded" (legacy set) from
	// "recorded, and the range starts at the root of history".
	BaseCommit string
	// BaseCommitRecorded reports whether the set carries a base at all. False for
	// every set authored before the field existed and for any set that has not yet
	// made its first implementation commit.
	BaseCommitRecorded bool
	// CommitConvention is the set's resolved Commit convention: the prose
	// description of this repository's commit grammar, read from and written back
	// as the set-level `commit_convention` key (ADR-0207). Nothing in the commit
	// path reads it — the per-task Planned commit subjects are already rendered —
	// it is carried for the agents that render a subject later, chiefly the
	// Verifier spawning a Remediation task mid-drain.
	//
	// pop writes it itself when the set registers, from the resolved stack
	// (ADR-0228, RecordCommitConvention): it is a projection of the Convention
	// stack, not an author's copy of it, and an authored value is overwritten.
	// Empty only for a set registered before pop wrote the key.
	CommitConvention string
	// HumanCompleted records that a human's own `complete` is what carried this set
	// terminal, read from and written back as the set-level `human_completed` key.
	// It lives in the manifest rather than the store because it is an assertion
	// about the set's work, not about a checkout's HEAD: Verify verdicts are keyed
	// by (repo, set, work SHA) because a Verifier's PASS expires when the branch
	// moves (ADR-0096), whereas "I am okay with this" does not — so the bit travels
	// with the set, survives later commits, and needs no SHA.
	//
	// It is cleared on the way out of the terminal zone (see WriteManifestAtomic):
	// a reopened task means the assertion no longer describes the set.
	HumanCompleted bool
	// DeprecatedKeys names retired set-level keys (`worktree`, `auto_drain`) that
	// are still present in the manifest but no longer read (ADR-0115). They are
	// ignored — never MALFORMED — and preserved verbatim in Unknown; register
	// surfaces them as a deprecation warning. Sorted for a stable warning order.
	DeprecatedKeys []string
}

// WorktreeDirective is a set-level worktree intent persisted with a set's
// registration. Exactly one arm is set: Managed requests a pop-provisioned
// managed worktree, Name adopts the existing worktree of that name on this
// machine. It is no longer read from the manifest (ADR-0115) — it survives as
// the store-backed registration intent shape (see tasks.RegisteredTaskSet).
type WorktreeDirective struct {
	Managed bool   `json:"managed,omitempty"`
	Name    string `json:"name,omitempty"`
}

// LoadManifest reads and validates an task manifest.
//
// The load is a pure function of the set directory's files — no store, no git, no
// config, no clock — so its answer is memoized for the life of the process under
// a key naming that content (manifestMemo, ADR-0189). Every surface that walks the
// same definition path therefore validates it once: a poll that finds an unchanged
// set pays a manifest read and a directory listing instead of a read, a
// line-split and two regexes per line for every task markdown in it.
//
// A read error is not memoized, only a parsed answer: an unreadable manifest is a
// question about a moment, not about content.
func LoadManifest(d *Deps, stem, manifestPath string) *Manifest {
	m := &Manifest{
		Stem: stem,
		Path: manifestPath,
		Dir:  filepath.Dir(manifestPath),
	}

	data, err := d.FS.ReadFile(manifestPath)
	if err != nil {
		m.Errors = append(m.Errors, fmt.Sprintf("read manifest: %v", err))
		return m
	}

	// One listing serves both the memo key and the orphan check below, so keying
	// on the directory's names costs no extra syscall.
	entries, listErr := d.FS.ReadDir(m.Dir)
	key, keyed := "", false
	if listErr == nil {
		key, keyed = manifestContentKey(manifestPath, data, entries)
	}
	if keyed {
		if cached, hit := manifestMemo.Get(key); hit {
			served := cached.clone()
			// The stem is the caller's name for the set, not something the directory
			// says, so it is the one field a hit must not inherit.
			served.Stem = stem
			return served
		}
	}

	m.Raw = append(json.RawMessage(nil), data...)
	if parseErr := parseManifestJSON(data, m); parseErr != nil {
		m.Errors = append(m.Errors, parseErr.Error())
	} else {
		validateManifest(d, m, entries, listErr)
		if len(m.Errors) == 0 {
			m.Valid = true
		}
	}
	if keyed {
		manifestMemo.Put(key, m.clone())
	}
	return m
}

func parseManifestJSON(data []byte, m *Manifest) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	tasksRaw, ok := raw[manifestTasksKey]
	if !ok {
		return fmt.Errorf("missing tasks array")
	}
	if err := json.Unmarshal(tasksRaw, &m.Tasks); err != nil {
		return fmt.Errorf("parse tasks: %w", err)
	}

	m.Unknown = make(map[string]json.RawMessage)
	for k, v := range raw {
		switch k {
		case manifestTasksKey:
			continue
		case "auto_drain", "worktree":
			// Retired set-level keys (ADR-0115): no longer read as registration
			// seeds. They are ignored with a deprecation warning at register — never
			// MALFORMED, regardless of value — and preserved verbatim (in Unknown) so
			// no forced migration rewrites the file.
			m.DeprecatedKeys = append(m.DeprecatedKeys, k)
			m.Unknown[k] = v
		case "source_map":
			// A malformed value is a diagnostic rather than a parse failure: the tasks
			// array is the set, and a bad back-link must not hide what is wrong with it.
			// The raw value rides through Unknown so a rewrite never eats it.
			if err := json.Unmarshal(v, &m.SourceMap); err != nil {
				m.Errors = append(m.Errors, "source_map: must be a map id string")
				m.Unknown[k] = v
			}
		case "base_commit":
			// Machine-written, so a malformed value means something outside pop edited
			// it. It reads as absent rather than MALFORMED — a reader that cannot trust
			// the base falls back to its pre-base path, which is strictly safer than
			// refusing to load the set — and the raw value rides through Unknown so the
			// rewrite that follows does not eat the evidence.
			if string(v) == "null" {
				// An explicit null is the root-commit edge: recorded, with no parent.
				m.BaseCommit = ""
				m.BaseCommitRecorded = true
				continue
			}
			if err := json.Unmarshal(v, &m.BaseCommit); err != nil || m.BaseCommit == "" {
				m.BaseCommit = ""
				m.BaseCommitRecorded = false
				m.Unknown[k] = v
				continue
			}
			m.BaseCommitRecorded = true
		case "commit_convention":
			// Advisory prose, not machinery: a malformed value reads as absent rather
			// than MALFORMED, because a set whose convention text is unusable still has
			// perfectly good tasks — the commits simply fall back to the default
			// format. The raw value rides through Unknown so a rewrite never eats it.
			if err := json.Unmarshal(v, &m.CommitConvention); err != nil {
				m.CommitConvention = ""
				m.Unknown[k] = v
			}
		case "human_completed":
			// A malformed value reads as absent rather than MALFORMED: this key is
			// hand-editable, and a typo in it must not hide what the set's tasks say.
			// Absent means "no human assertion", which is the pre-existing behaviour —
			// verification still gates the status — so the fail-safe direction is off.
			// The raw value rides through Unknown so a rewrite never eats it.
			if err := json.Unmarshal(v, &m.HumanCompleted); err != nil {
				m.HumanCompleted = false
				m.Unknown[k] = v
			}
		default:
			m.Unknown[k] = v
		}
	}
	sort.Strings(m.DeprecatedKeys)
	return nil
}

// validateManifest validates the parsed manifest against the set directory. The
// directory listing is passed in rather than read here because the caller already
// took it to key its memo; listErr is that listing's error, handled below exactly
// as a listing taken here would be.
func validateManifest(d *Deps, m *Manifest, entries []os.DirEntry, listErr error) {
	if len(m.Tasks) == 0 {
		m.Errors = append(m.Errors, "tasks array is empty")
	}

	ids := make(map[string]int)
	files := make(map[string]int)
	idSet := make(map[string]bool)

	for i, task := range m.Tasks {
		if task.ID == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("task[%d]: missing id", i))
			continue
		}
		if ids[task.ID] > 0 {
			m.Errors = append(m.Errors, fmt.Sprintf("duplicate task id %q", task.ID))
		}
		ids[task.ID]++
		idSet[task.ID] = true

		if task.File == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: missing file", task.ID))
		} else {
			if strings.Contains(task.File, "/") || strings.Contains(task.File, "\\") {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: file must be root-level markdown name, got %q", task.ID, task.File))
			}
			if files[task.File] > 0 {
				m.Errors = append(m.Errors, fmt.Sprintf("duplicate task file %q", task.File))
			}
			files[task.File]++

			mdPath := filepath.Join(m.Dir, task.File)
			if _, err := d.FS.Stat(mdPath); os.IsNotExist(err) {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: missing markdown file %q", task.ID, task.File))
			} else if err != nil {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: stat markdown %q: %v", task.ID, task.File, err))
			} else if err := validateAcceptanceCriteria(d, mdPath); err != nil {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: %v", task.ID, err))
			}
		}

		if !allowedTaskTypes[task.Type] {
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: invalid type %q", task.ID, task.Type))
		}

		if task.Effort == "" {
			m.Tasks[i].Effort = DefaultTaskEffort
			task.Effort = DefaultTaskEffort
		}
		if !allowedTaskEfforts[task.Effort] {
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: invalid effort %q", task.ID, task.Effort))
		}

		switch task.Status {
		case "in_progress":
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: persisted in_progress status is malformed", task.ID))
		case "":
			m.Errors = append(m.Errors, fmt.Sprintf("task %q: missing status", task.ID))
		default:
			if !allowedTaskStatuses[task.Status] {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: invalid status %q", task.ID, task.Status))
			}
		}
	}

	for _, task := range m.Tasks {
		for _, blocker := range task.BlockedBy {
			if !idSet[blocker] {
				m.Errors = append(m.Errors, fmt.Sprintf("task %q: unresolved blocker %q", task.ID, blocker))
			}
		}
	}

	validateNoOrphanMarkdown(m, entries, listErr, files)
}

// validateNoOrphanMarkdown closes the 1:1 sync in the direction the per-task
// check cannot: every entry names a file that exists, and this proves every file
// has an entry. Without it a slice written but never listed registers as READY
// and is invisible — never drained, never counted, never reported missing.
//
// The set folder is pop's storage rather than scratch space, so the only markdown
// exempt is the co-located spec; anything else is a stray file to move out or a
// slice to list.
func validateNoOrphanMarkdown(m *Manifest, entries []os.DirEntry, listErr error, listed map[string]int) {
	if listErr != nil {
		if os.IsNotExist(listErr) {
			return
		}
		m.Errors = append(m.Errors, fmt.Sprintf("list set folder: %v", listErr))
		return
	}
	var orphans []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if name == SpecFileName || name == legacySpecFileName || listed[name] > 0 {
			continue
		}
		orphans = append(orphans, name)
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		m.Errors = append(m.Errors, fmt.Sprintf(
			"%s: no manifest entry; every markdown in a set folder but %s is a task",
			name, SpecFileName))
	}
}

func validateAcceptanceCriteria(d *Deps, mdPath string) error {
	data, err := d.FS.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read markdown: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	inSection := false
	sectionCount := 0
	checkboxCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if acHeaderPattern.MatchString(trimmed) {
			sectionCount++
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "## ") {
			inSection = false
		}
		if inSection && checkboxPattern.MatchString(trimmed) {
			checkboxCount++
		}
	}

	if sectionCount == 0 {
		return fmt.Errorf("missing acceptance criteria section")
	}
	if sectionCount > 1 {
		return fmt.Errorf("multiple acceptance criteria sections")
	}
	if checkboxCount == 0 {
		return fmt.Errorf("acceptance criteria has no checkboxes")
	}
	return nil
}

// WriteManifestAtomic writes a manifest JSON file atomically, preserving unknown
// fields — including retired keys (`worktree`, `auto_drain`) that ride through in
// Unknown so a rewrite never strips a legacy manifest (ADR-0115).
func WriteManifestAtomic(d *Deps, m *Manifest) error {
	out := make(map[string]json.RawMessage)
	for k, v := range m.Unknown {
		out[k] = v
	}
	tasksData, err := json.Marshal(m.Tasks)
	if err != nil {
		return err
	}
	out[manifestTasksKey] = tasksData
	if m.SourceMap != "" {
		sourceMap, err := json.Marshal(m.SourceMap)
		if err != nil {
			return err
		}
		out["source_map"] = sourceMap
	}
	if m.CommitConvention != "" {
		convention, err := json.Marshal(m.CommitConvention)
		if err != nil {
			return err
		}
		out["commit_convention"] = convention
	}
	// A recorded base is written every time, so the key never depends on which
	// rewrite path ran; an unrecorded one leaves whatever Unknown carries (an
	// absent key, or a malformed value preserved verbatim) untouched.
	if m.BaseCommitRecorded {
		if m.BaseCommit == "" {
			out["base_commit"] = json.RawMessage("null")
		} else {
			base, err := json.Marshal(m.BaseCommit)
			if err != nil {
				return err
			}
			out["base_commit"] = base
		}
	}
	// The human-completion bit never outlives the terminal it describes. Every
	// path that changes a set's tasks — the transition chokepoint, a spawned
	// Remediation task — lands here, so clearing it on the way out of the terminal
	// zone is one rule in one place rather than a clear at each verb. A manifest
	// that does not validate is left alone: its derived status is MALFORMED, which
	// says nothing about whether the work is finished.
	delete(out, "human_completed")
	if m.HumanCompleted && m.Valid && !TerminalStatus(DeriveStatus(m)) {
		m.HumanCompleted = false
	}
	if m.HumanCompleted {
		out["human_completed"] = json.RawMessage("true")
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, m.Path, data, 0o644)
}

// VerifyOptedOut reports whether the set explicitly opted out of Agent
// verification with `"verify": false` in its manifest (ADR-0086). Verification
// is a per-set opt-out only: user config is the master gate, so an absent or
// truthy `verify` key means the set participates when the feature is globally
// enabled, and there is no per-set opt-*in* while the feature is off. A
// malformed value is treated as participating (fail toward verifying); the key
// rides through WriteManifestAtomic in Unknown, so a rewrite preserves it.
func (m *Manifest) VerifyOptedOut() bool {
	return m.optedOut("verify")
}

// ReviewOptedOut reports whether the set explicitly opted out of Code review
// with `"review": false` in its manifest (ADR-0214). It is the Verifier rule
// applied to the Reviewer, key for key: user config is the master gate, an
// absent or truthy `review` key means the set is reviewed while the feature is
// globally enabled, and a malformed value is treated as participating.
//
// A hand-run `pop tasks review <set>` ignores it, the way a hand-run verify
// ignores `"verify": false`: the key declines the automatic drain step, not a
// human asking the question.
func (m *Manifest) ReviewOptedOut() bool {
	return m.optedOut("review")
}

// optedOut reads a boolean set key as an opt-out. The key rides through
// WriteManifestAtomic in Unknown, so a rewrite preserves it; anything that does
// not parse as `false` leaves the set participating.
func (m *Manifest) optedOut(key string) bool {
	if m == nil {
		return false
	}
	raw, ok := m.Unknown[key]
	if !ok {
		return false
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return false
	}
	return !enabled
}

// AgentDirective is a set's per-set override of the agent list and effort a
// set-level phase runs at, read from the manifest's
// `"verifier": {"agents": [...], "effort": "..."}` object (ADR-0086) and its
// `"reviewer"` twin (ADR-0214). One type serves both because the two phases
// resolve the same two values by the same precedence.
//
// It overrides the phase's config default for that set, but it is opt-out only
// for participation: user config is the master gate, so a directive can steer
// *how* a set is verified or reviewed but never opt it *in* while the feature is
// globally off (that stays VerifyOptedOut / ReviewOptedOut and the config switch).
type AgentDirective struct {
	Agents []string `json:"agents,omitempty"`
	Effort string   `json:"effort,omitempty"`
}

// VerifierOverride returns the set's per-set Verifier override, or nil when the
// manifest carries no `verifier` object (or a malformed one — a bad value is
// ignored so it falls through to the config default).
func (m *Manifest) VerifierOverride() *AgentDirective {
	return m.agentDirective("verifier")
}

// ReviewerOverride returns the set's per-set Reviewer override, read from the
// manifest's `reviewer` object, or nil when there is none.
func (m *Manifest) ReviewerOverride() *AgentDirective {
	return m.agentDirective("reviewer")
}

// agentDirective parses one override object out of the manifest's unread keys.
// The key rides through WriteManifestAtomic in Unknown, so a rewrite preserves
// it; a malformed object resolves to nil rather than an error, leaving the phase
// on its config default.
func (m *Manifest) agentDirective(key string) *AgentDirective {
	if m == nil {
		return nil
	}
	raw, ok := m.Unknown[key]
	if !ok {
		return nil
	}
	var over AgentDirective
	if err := json.Unmarshal(raw, &over); err != nil {
		return nil
	}
	return &over
}
