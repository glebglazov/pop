package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AuthoringGuide is what `pop tasks authoring-guide` prints: how to hand-write a
// Task set. Every enum, filename and marker below comes from the value
// validateManifest reads, so the printed rules cannot drift from the enforced
// ones — a constant changes and the guide changes with it, in the same build.
//
// Unlike the Map guide it also carries the **judgment** rules — typing, effort,
// slicing, Orientation — and it is authoritative rather than a summary. Those
// rules are unenforceable, but unenforceability is an argument about validation,
// not about where text lives: splitting them out would make a session read two
// surfaces to author one artifact and open a fresh drift seam between the halves.
//
// It describes the artifact, not a workflow, so it serves every writer of a set:
// initial breakdown, a spec handed on by `to-spec`, and the assist agent editing
// the manifest mid-drain.
func AuthoringGuide() string {
	var b strings.Builder

	fmt.Fprintf(&b, `# Authoring a Task set by hand

pop owns a set's structure and validation; you write the files. There is no
authoring payload and no create verb: write the task markdown and %s
yourself, then run `+"`pop tasks register <task-set-name>`"+`.

Writing the files only **drafts** the set — it is inert, invisible to the
dashboard and never scheduled, until it is registered. Registration is the
explicit verb that makes it Work; see `+"`pop tasks register -h`"+` for its flags.

This guide is generated from the constants the validator reads. Where any
installed document disagrees with it about the shape of these files, this wins.

## Storage layout

A set is one folder in this repository's Work store; run `+"`pop work show-path`"+`
(or `+"`pop tasks show-path`"+` for the tasks/ directory itself) to resolve it.

    $(pop work show-path)/tasks/<task-set-name>/
    ├── %s        (optional — the spec this set came from)
    ├── %s
    ├── 01-<task-name>.md
    ├── 02-<task-name>.md
    └── progress.txt    (pop's; never hand-written)

- `+"`<task-set-name>`"+` is `+"`<timestamp>-<slug>`"+`. The slug is short and
  hyphen-delimited (e.g. `+"`user-auth`"+`); the timestamp is
  `+"`YYYY-MM-DD`"+`, or `+"`YYYY-MM-DD-HHMM`"+` (24-hour local) when a folder
  with that date and slug already exists. Examples:
  `+"`2026-05-31-user-auth`"+`, `+"`2026-05-31-2036-user-auth`"+`.
- Each slice is one markdown file named `+"`<number>-<task-name>.md`"+` at the
  root of the folder. The number orders the set and the stem is the task's id.
- A spec and its breakdown share **one** folder: when breaking down a co-located
  %s, write the task files beside it rather than minting a new folder.
- %s and the task markdown stay in 1:1 sync — every markdown file has
  exactly one manifest entry, and every entry names a file that exists.
  %s is the only markdown in the folder with no entry.

Write the files in dependency order (blockers first), so `+"`blocked_by`"+` names
ids you have already chosen.

## %s

Optional, and co-located rather than stored elsewhere, so the set's whole
context lives in one directory. The name is exactly %s — there is no
backward-compatible read of any other filename — and a set folder holding a spec
alone is a drafted spec with no slices yet.

When the work came from a Map, make `+"`Source map: <map-id>`"+` the **first
line**, above everything else. That line is human-facing prose which nothing
parses; the machine-readable halves of the link are the manifest's
`+"`source_map`"+` key and `+"`pop map spawned`"+`.

pop reads the spec as one body — the Verifier includes it whole when it is
present — so the sections inside it are the spec-writing skill's shape, not a
storage rule.

## Task markdown

`+"```markdown"+`
%s
`+"```"+`

Do not modify the spec or any other parent file while writing the slices.

### A slice is a vertical slice

"What to build" describes one **end-to-end behaviour** — request to response,
command to persisted state — not a layer of the system. A set sliced by layer
produces tasks that cannot be verified on their own; a set sliced by behaviour
produces tasks that each move the product.

Keep it path-free: durable intent outlives the tree, and a path written here goes
stale where a behaviour does not. The one exception is a prototype-derived
snippet that encodes a decision more precisely than prose can (a state machine, a
reducer, a schema, a type shape) — inline it trimmed to the decision-rich part,
and say it came from a prototype.

### Orientation is the one place paths belong

Orientation is explicitly the opposite of "What to build": perishable pointers,
stamped as of authoring and labelled stale-able, so the executor stops
re-deriving a map the author already had. An unattended drain spends a large
share of its tool calls on that rediscovery, and you write it for free from the
context you just used to slice the work.

Fill it from what you actually know — the files and symbols you touched or read
while planning, plus the exact build/test command that proves the slice. Omit the
section entirely for a slice with no code surface (a HITL sign-off). Never pad it
with guesses: a wrong pointer costs more than a missing one.

### HITL / AFK typing

Every slice is either **HITL** or **AFK**, and the type in the markdown must
match the type in %s. Prefer AFK wherever possible.

- **AFK** — can be implemented and merged without human interaction.
- **HITL** — contains ONLY human work: verification, decisions, manual testing.
  The executor never runs it. Write the body as instructions to the human: the
  exact steps to perform. A HITL slice whose "What to build" describes software
  is mis-typed.

**Split the slice.** If a HITL slice needs an artifact built first — a report
command, test data, a harness — the build goes in an AFK slice and the HITL slice
depends on it through "Blocked by", holding only the human steps.

HITL slices have two legitimate positions, at opposite ends of a set:

- **Approval at the end.** Agents are done and have verified their own work; the
  human signs off. Nothing depends on it, so the set reaches
  `+"`AWAITING-APPROVAL`"+`. This is the common, expected HITL.
- **Setup at the bottom.** The human provisions something the agent genuinely
  cannot — mainly accounts and secrets it cannot self-issue — before agents can
  run. AFK slices depend on it and the set sits `+"`BLOCKED`"+` until the human
  acts. Create one only when *absolutely necessary*, never for anything the model
  can discover or do itself (devices, environment details, readable config).

A HITL in the middle is valid when it is a genuine mid-flow human decision, but
it parks the set at `+"`BLOCKED`"+` mid-drain — which is the correct signal that
agent work is waiting behind a human.

### Effort is a named signal

Assign an effort to every slice, written explicitly:

- %s — architectural or cross-cutting refactors, or genuinely tricky
  algorithms.
- %s — large but mechanical work: renames, codemods, config, boilerplate.
- %s — everything else, and the default when no named signal clearly
  applies.

Effort is model-strength intent, not an agent choice: do not consult
`+"`pop tasks agents`"+` in the default flow.

## %s

One entry per task file, in the order the slices should be read:

`+"```json"+`
%s
`+"```"+`

Per-task fields:

- `+"`id`"+` — the filename stem (`+"`<number>-<task-name>`"+`); the identifier
  `+"`blocked_by`"+` references.
- `+"`file`"+` — the task's markdown filename, a bare name at the root of the
  set folder (no directory part).
- `+"`title`"+` — a short human label.
- `+"`type`"+` — %s, matching the markdown's Type section.
- `+"`effort`"+` — %s, per the heuristic
  above. Absent in an existing manifest reads as `+"`%s`"+`.
- `+"`status`"+` — %s. Always author
  `+"`%s`"+`. Never write `+"`in_progress`"+`: a run is live state, and a
  persisted `+"`in_progress`"+` is malformed.
- `+"`blocked_by`"+` — array of blocker ids; empty array if none.
- `+"`failed_after`"+` — optional integer written by a runner that gave up;
  never at authoring time.
- `+"`agent`"+` — optional escape hatch (ADR-0018). Fill it only when the user
  explicitly asks for a specific agent or model.
- `+"`commit_subject`"+` — optional Planned commit subject: the final, literal
  subject line pop commits this task's work under. It is used **verbatim** — pop
  renders nothing, substitutes nothing and reformats nothing — so write the whole
  line in the repository's own commit grammar (e.g.
  `+"`feat(auth): add token refresh`"+`). The body stays the agent's summary.
  Omit it and the commit falls back to pop's default format,
  `+"`tasks(<set-slug>): <task-id>`"+`.

Set-level keys:

- `+"`source_map`"+` — the id of the Map this set was spawned from. Written on
  **every** Map-sourced set, spec or no spec, so the back-link is never
  half-built.
- `+"`commit_convention`"+` — **pop's, not yours.** Register writes this key
  itself, from the same resolved commit convention
  `+"`pop conventions get commits`"+` prints: it is the prose an agent spawning
  a task mid-drain renders a new `+"`commit_subject`"+` from, and pop projects
  it rather than trust a retyped copy. Do not supply it — a value written here
  is overwritten when the set registers.
- `+"`verify`"+` / `+"`refine`"+` — optional booleans, and opt-**out** only:
  write `+"`false`"+` to decline the drain's Agent verification or its
  Refine for this set alone (generated or vendored code, say). Omit them and the
  set participates in whichever of the two the user has enabled globally; neither
  key can switch a globally disabled phase on, and a hand-run
  `+"`pop tasks verify`"+` / `+"`pop tasks refine`"+` runs regardless.
- `+"`verifier`"+` / `+"`refiner`"+` — optional
  `+"`{\"agents\": [...], \"effort\": \"...\"}`"+` objects steering *how* this
  set is verified or refined: they override the configured agent fallback list
  and effort for that phase, and CLI flags still win over them. They steer only —
  participation stays the `+"`verify`"+` / `+"`refine`"+` keys' business.
- No `+"`worktree`"+` and no `+"`auto_drain`"+` (ADR-0115): binding and
  auto-drain are `+"`register`"+` flags and dashboard toggles, never manifest
  keys. A legacy set carrying them is not malformed; they are ignored.

Keys pop does not read ride through a rewrite untouched.

## What registration enforces

`+"`pop tasks register`"+` reports the whole fix list at once, not the first
item, and is re-runnable until the set reads `+"`READY`"+` (or
`+"`DEFERRED`"+`, when every open task is HITL):

- the tasks array is non-empty;
- every task has an id, and no two share one;
- every `+"`file`"+` is a unique root-level markdown name that exists on disk;
- every task markdown has exactly one `+"`## %s`"+` section,
  with at least one checkbox under it;
- every `+"`type`"+`, `+"`effort`"+` and `+"`status`"+` is one of the words
  above, and no status is `+"`in_progress`"+`;
- every `+"`blocked_by`"+` id names a task in the manifest;
- every markdown file in the folder has an entry — %s aside, a file
  nothing lists is reported rather than silently ignored, because an unlisted
  slice is one that never runs.

A task is **done** when every box under its `+"`## %s`"+` section is
checked; that is the condition `+"`pop tasks implement`"+` reads back, and it is
why the section is mandatory.
`,
		ManifestFileName,
		SpecFileName, ManifestFileName,
		SpecFileName,
		ManifestFileName, SpecFileName,
		SpecFileName, SpecFileName,
		taskMarkdownTemplate(),
		ManifestFileName,
		effortWord("heavy"), effortWord("light"), effortWord(DefaultTaskEffort),
		ManifestFileName,
		taskManifestExample(),
		enumList(taskTypeOrder),
		enumList(ValidEfforts()), DefaultTaskEffort,
		enumList(taskStatusWords()), TaskOpen,
		AcceptanceCriteriaHeading, SpecFileName,
		AcceptanceCriteriaHeading,
	)

	return b.String()
}

// taskMarkdownTemplate renders the slice skeleton. Its acceptance-criteria
// heading and checkboxes come from the validator's own heading constant, so the
// template a session copies is one validateManifest accepts — the guide's test
// runs the validator over this very text.
func taskMarkdownTemplate() string {
	return strings.Join([]string{
		"## Parent",
		"",
		"A reference to the parent item — the spec, or the Map ticket this slice",
		"implements. Omit the section when there is no parent.",
		"",
		"## What to build",
		"",
		"The vertical slice: the end-to-end behaviour, not a layer-by-layer",
		"implementation. Path-free.",
		"",
		"## Orientation",
		"",
		"Perishable pointers, as of authoring: the files to touch, the symbols and",
		"types involved, and the exact build/test command that proves the slice.",
		"Verify before trusting — anything here may have moved.",
		"",
		"## Type",
		"",
		"HITL or AFK.",
		"",
		"## " + AcceptanceCriteriaHeading,
		"",
		"- [ ] Criterion 1",
		"- [ ] Criterion 2",
		"",
		"## Blocked by",
		"",
		"- The id of the blocking slice (if any), or \"None - can start immediately\".",
	}, "\n")
}

// taskManifestExample marshals real Task values through the manifest's own
// marshaller, so the printed keys are the ones LoadManifest reads back.
func taskManifestExample() string {
	tasks := []Task{
		{
			ID:             "01-login-form",
			File:           "01-login-form.md",
			Title:          "Login form",
			Type:           "AFK",
			Status:         TaskOpen,
			BlockedBy:      []string{},
			Effort:         DefaultTaskEffort,
			EffortExplicit: true,
			CommitSubject:  "feat(auth): add the login form",
		},
		{
			ID:             "02-sign-off",
			File:           "02-sign-off.md",
			Title:          "Sign off on the flow",
			Type:           "HITL",
			Status:         TaskOpen,
			BlockedBy:      []string{"01-login-form"},
			Effort:         "light",
			EffortExplicit: true,
		},
	}
	tasksJSON, err := json.Marshal(tasks)
	if err != nil {
		return ""
	}
	data, err := json.MarshalIndent(map[string]json.RawMessage{manifestTasksKey: tasksJSON}, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func taskStatusWords() []string {
	words := make([]string, 0, len(taskStatusOrder))
	for _, s := range taskStatusOrder {
		words = append(words, string(s))
	}
	return words
}

// effortWord backticks one tier, and refuses to name a tier the validator does
// not accept — the heuristic's prose is per-tier, so it cannot be generated from
// the list the way a flat enum can.
func effortWord(effort string) string {
	if !IsValidEffort(effort) {
		return "`" + effort + "` (UNKNOWN EFFORT — this is a bug)"
	}
	return "`" + effort + "`"
}

// enumList renders an enum the way the guide says one: `a` | `b` | `c`.
func enumList(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, "`"+w+"`")
	}
	return strings.Join(quoted, " | ")
}
