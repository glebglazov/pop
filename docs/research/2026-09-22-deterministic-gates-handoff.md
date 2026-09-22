# Handoff: which parts of pop should be deterministic and are not

Date: 2026-09-22. Branch: `master` at `2ccdf95`. Read-only research session; no
code was changed. Extended the same day at `a8437e8` with a second read-only
session on whether the state machine can carry the proposal (the section "Is
the state machine ready?").

## The original question

Review the whole solution and tell which parts that should be deterministic
are not. Should tests be a deterministic step? The cost is that every project
has a different test pipeline, so the checks would have to be written per
project. Could pop detect the project's toolchain and produce a standard
manifest of the common commands, such as how to run lint and how to run tests,
and then run them deterministically after a task? That would move the work out
of the non-deterministic Verifier. The Verifier would then verify correctness.
What else is there?

## What was researched

Five parallel read-only audits over the code, each reporting file:line
references, split into LLM-made decisions, code-made decisions, heuristic
parsing fallbacks, and existing deterministic-check hooks:

1. Verify stage: `tasks/verify.go`, `verify_phase.go`, `verify_mark.go`,
   `verified_status.go`, `verify_report.go`, `remediation.go`,
   `tasks/prompts/verifier.tmpl.md`, `conventions/shipped/verification.md`.
2. Implement stage: `tasks/run_tasks.go`, `attempts.go`, `assess.go`,
   `digest.go`, `transition.go`, `scheduler.go`, `tasks/prompts/agent.tmpl.md`,
   the per-agent quota adapters `tasks/agent_*.go`.
3. Refine and Fold: `tasks/refine.go`, `refine_commit.go`, `refine_phase.go`,
   `tasks/prompts/refiner.tmpl.md`, `tasks/binding/fold*.go`,
   `tasks/fold_conflict.go`, `fold_landing.go`.
4. Planning, manifest, eval, conventions: `tasks/manifest.go`,
   `authoring_guide.go`, `integrate/skills/pop/to-tasks/SKILL.md`, `eval/`,
   `conventions/`.
5. Everything else that invokes an agent or parses agent text: topic
   derivation, Routines, Wayfinder Maps, spend, effort ladders, quota parsing.
6. The state machine itself, in the second session: `tasks/transition.go`,
   `status.go`, `verified_status.go`, `verify_status.go`, `run_tasks.go`,
   `implement_run.go`, `run_selected_task.go`, `terminal_switch.go`,
   `refine_phase.go`, `verify_phase.go`, `scheduler.go`, `attempts.go`,
   `attempt_cap_exhaustion.go`, `drain_store.go`, `tasks/drain/advance.go`,
   `store/store.go`, `store/drains.go`, plus one subagent audit of every
   write site that a status derivation later reads.

ADRs read for the contracts: 0040, 0055, 0056, 0086, 0102, 0109, 0148, 0207,
0227, 0228, 0229, 0231, 0245, 0248, 0252, 0258, 0260, 0265, 0268, 0274. The
glossary entries read: Verifier, Verification convention, Verify verdict,
Verification mark, Verified-at SHA, Verification episode, Verified status
resolution, Human completion, Completion sentinel, Failure reason, Planned
commit subject, Refine, Refine episode, Remediation task, Remediation depth,
Task transition, Drain, Drain outcome, Drain ending.

## What was found

### The boundary today

Code owns shape. LLMs own truth. Code decides enums, ids, file existence,
blocker resolution, the commit range, SHA caching, remediation depth, and every
fold step from git facts. An LLM decides whether a criterion is met, whether
the code is good, and whether the build and tests ran at all.

Pop never executes a project's build, test or lint command in the Task-set
pipeline. The only subprocesses in `tasks/` are git and the agent CLIs. The only
place a project's test suite gates anything is the eval harness:
`gate_commands` in `eval/cases/*/case.json`, run with `sh -c` in
`eval/grade.go:216-244`; a failed gate scores zero and skips the Grader
(ADR-0268 decision 5). Default gates are `go build ./...` and `go test ./...`.
No Task set, manifest field, convention kind or config key carries a command.
`[work.verify]` has only `enabled`, `agents`, `effort`,
`max_remediation_depth`, `max_tries`, `attempt_retry_delays`.

### Non-deterministic where it should not be, by damage

1. **Implement completion is a self-report.** `AssessCompletion`
   (`tasks/assess.go:54-89`) needs exit 0, a `TASK_COMPLETE` line, a
   `SUMMARY_START..SUMMARY_END` block, and every `- [x]` under
   `## Acceptance criteria` (`allAcceptanceChecked`, `assess.go:126-148`). The
   agent ticks its own boxes. `tasks/prompts/agent.tmpl.md` never asks for a
   build or a test run. Pop then commits (`createImplementationCommit`,
   `attempts.go:679-714`). The only machine fact about the work is a non-empty
   `git status --porcelain`.
2. **The Verifier finds, runs and self-reports the gates.**
   `conventions/shipped/verification.md` tells the agent to read the agent doc,
   then Makefile / justfile / Taskfile / package.json scripts / mix.exs /
   Rakefile, then CI, and to run a scoped and a whole-tree gate. Pop sees no
   command and no exit code. `ParseVerdict` (`verify.go:1621-1667`) prefix
   matches, so "PASS with caveats" is PASS. `verifierRole`
   (`verify.go:955-972`) does not set `ReadOnly`. Nothing inspects tool calls.
3. **Refine outcome defaults to the writing value.** `REFINE-OUTCOME:` decides
   commit / revert / nothing. Missing or unrecognised falls to `refined`
   (`tasks/refine_commit.go:47,59-64`). The gate that separates `abandoned`
   from `refined` is one the agent claims to have run. Only backstop: a pass
   that changed nothing commits nothing (`refine_commit.go:228-248`).
4. **Fold lands without a build.** Fold is otherwise the most deterministic
   stage (ADR-0229, ADR-0274; `tasks/binding/fold_recovery.go`). The landing
   gate's only quality check is the LLM Verifier (`runFoldSetVerify`,
   `tasks/fold_conflict.go:369-404`). The conflict prompt's verify item stamps a
   mid-rebase detached HEAD; ADR-0274 records this as a known defect.
5. **Commit subjects are agent prose, never validated.** Planned subjects
   (`manifest.go:114-122`), Verifier-rendered remediation subjects
   (`verify.go:1432-1460`) and Refiner subjects are only sanitised
   (`sanitizeAgentCommitSubject`, `verify.go:1416-1430`). No grammar check
   against the `commits` convention.
6. **Acceptance criteria have no executable form.** Manifest fields are `id,
   file, title, type, status, blocked_by, effort, origin, commit,
   commit_subject, failed_after` (`manifest.go:135-146`). The planning skill and
   `tasks/authoring_guide.go:307-311` give `- [ ] Criterion 1`. The Orientation
   section asks for "the exact build/test command that proves the slice"
   (`authoring_guide.go:118-123`) and nothing parses it.
7. **Routine outcome is a sentinel.** `ROUTINE_COMPLETE` plus a report file on
   disk is success (`routine/assess.go:41-61`); a sentinel failure pauses the
   Routine (ADR-0128).

Smaller items:

- No stall detection on a running agent; only the wall-clock timeout
  (`attempts.go:841`).
- `output = "text"` disables all structured parsing
  (`normalizeAgentOutput`, `tasks/agent.go:1485-1506`); a quota pause then
  reads as a failed attempt and burns retries; no Captured run is filed.
- ADR-0258 (refine and verify at every AFK standstill) is accepted but
  `AFKStandstill` exists in no Go file; `verify_phase.go:70-71` still gates on
  DONE / AWAITING-APPROVAL.
- `verify_verdicts` has no derivation stamp, unlike the Manifest memo after
  ADR-0265. Low value while the verdict is an LLM answer.

### Already deterministic and worth keeping as the model

- Fold: every decision is a git exit status or a human keystroke; agent output
  is re-checked against git (`listConflictedPaths`, `foldRebaseCompleted`).
- Commit range resolution (`resolveVerifyRange`, `verify.go:1124-1132`); an
  undetermined range stores NEEDS-HUMAN without calling an agent
  (`verify.go:581-594`).
- Quota and refusal detection: typed stream fields first, prose markers
  demoted (ADR-0233, ADR-0234); unparseable reset falls to a probed ceiling
  (ADR-0235).
- Effort to model ladders are Go literals (`tasks/agent.go:267-274`).
- `commit_convention` is pop's projection of the stack (ADR-0228).
- Eval Grader reply parsing is strict; any deviation is `ungraded`
  (`eval/grade.go:365-386`).

## The recommendation

### Gate manifest of Objective gates

Add a resolved, per-repository list of **Objective gates**: a shell command
with a name and a role. Reuse the eval harness's term because the meaning is
identical. Glossary caution: CONTEXT.md already uses "gate" for the HITL gate
and the Fold landing gate, which are human menus. Decide whether the overload
is acceptable.

| Role | Runs at | On failure |
| --- | --- | --- |
| `build` | after every implement attempt, before pop commits | attempt fails with a contract fault; output enters the ADR-0040 digest |
| `test` | same; before the Verifier; at the Fold landing gate | same; the Verifier is skipped, as the eval does |
| `lint` / `format` | after Refine, before the Refine commit | Refine outcome forced to `abandoned` |
| `scope` | after every attempt | paths outside the stated scope become a flag for the Verifier, not a failure |

A gate result must be three-way: passed, failed, could not run. A missing
toolchain must not read as a task failure. The shipped verification body already
states this rule for the agent.

### Resolution rides the convention stack

1. A written Gate manifest at project, global or repository rank, beside
   `docs/agents/verification.md`, through the same `conventions` ranks
   (ADR-0211, ADR-0223).
2. **Gate detection** beneath it: a deterministic probe that does what
   `conventions/shipped/verification.md` tells the agent to do. Read `AGENTS.md`
   code fences, then Makefile targets, `justfile`, `package.json` scripts,
   `go.mod`, `Cargo.toml`, `Gemfile`, `mix.exs`, `pyproject.toml`, then CI
   workflow steps. Map ecosystem to a default ladder: Go to
   `go build ./... && go vet ./... && go test ./...`; Ruby with rspec in the
   Gemfile to `bundle exec rspec`. The eval harness's Go defaults generalise
   here.
3. `pop conventions detect gates` writes the probe result to a rank as a draft
   the human confirms once per repository.

### What the Verifier keeps

Pop hands the Verifier the gate results as evidence instead of an instruction
to run them. The Verifier keeps what only judgment answers:

- The diff meets the acceptance criteria and the spec beyond what tests cover.
- Tests were not weakened to go green. Partly deterministic: count deleted or
  skipped test files and assertions in the range and pass the number in.
- Scope, given the scope flag.
- Test adequacy, clarity, unnecessary complexity (the eval Grader's quality
  dimensions).
- FIXABLE versus NEEDS-HUMAN triage, and the findings text.

### Two fixes that need no manifest

- Make `REFINE-OUTCOME` fail closed: missing or unrecognised becomes
  `abandoned`. `VERDICT` already fails closed to NEEDS-HUMAN.
- Validate agent commit subjects against the recorded `commits` convention when
  it is Conventional Commits; fall back to pop's default subject on mismatch.

### Where a red gate goes

A red gate is a fact. "The test is wrong but the behaviour is right" is a
judgment, so an agent must make it. No new stage is needed. Run the gates at
two points and route each result to the role that already owns that moment.

1. **After every implement attempt, scoped gates.** A red result fails the
   attempt with a new failure type, "gate red". The gate output enters the
   ADR-0040 digest and the next attempt of the same task receives it. The
   Builder fixes its own breakage while it has the context. Bounded by
   `max_tries`. A task red three times surfaces the gate output at the Failed
   gate, so the human sees the test, not "agent failed".
2. **At set quiescence, whole-tree gates, before the Verifier.** Pop hands the
   results to the Verifier as evidence. Pop refuses PASS while any gate is red:
   a `VERDICT: PASS` with a red gate is downgraded to FIXABLE by code. The
   Verifier decides the remediation content. A wrong test and a wrong caller
   are both FIXABLE and become a Remediation task. "Spec and test disagree and I
   cannot tell which is right" is NEEDS-HUMAN. The Remediation task drains
   through point 1 again.

The Refiner does not get this job. Its licence is the implementation
convention: a refactor changes no test file and moves no behaviour. Whether a
failing test is wrong is a behaviour question. The Refiner's relation to gates
is defensive only: pop runs the gates after the pass, and a red result forces
`abandoned` and reverts the tree.

Details to settle:

- **Baseline.** Run the gates once at the start of the first drain. Not at
  `base_commit`: the manifest records the Set base commit only at the set's
  first implementation commit (`tasks/transition.go:189-192`), so on the first
  drain there is no base yet. HEAD at drain start is the baseline, stored
  SHA-keyed like a quiescence result. A gate already red on trunk is not the
  set's fault; demote it to advisory for that set and report it as a Severe
  journal event.
- **Deleted tests.** An agent can turn a gate green by deleting the test. Count
  test files and assertions removed in the range and hand the number to the
  Verifier as a flag, the eval harness's scope-flag pattern.

## Is the state machine ready?

Yes. It needs no rewrite. It is already deterministic: a two-layer model with
one pure derivation, one write chokepoint and a durable execution store. What
it lacks is not determinism but an input. No fact about the work comes from a
subprocess other than git and the agent CLIs. The work is to add that input at
two existing seams, give it a place to live, and close the fail-open holes on
the way. One structural blemish is worth fixing at the same time: effective
status is overlaid in three places instead of one.

### What the machine is today

**Layer 1: the manifest, derived status.**

- A task has four statuses: `open`, `done`, `failed`, `skipped`
  (`tasks/transition.go:11-14`). No `in_progress` is ever persisted.
- Every task status write goes through `ApplyTransitions`
  (`tasks/transition.go:93`). Legality is a `(from, to, actor)` table
  (`:38-50`); the executor may drive only `open→done` and `open→failed`. The
  batch is validated before any write, then written once, atomically. The
  chokepoint also fires Verification invalidation when an AFK task moves into
  open or done (`:158-172`), records the Set base commit once (`:184`) and the
  Human completion bit (`:144`).
- Set status is a pure function of the manifest: `DeriveStatus`
  (`tasks/status.go:139`). `TerminalStatus` (`:172`) is the one definition of
  the terminal zone. The verdict is layered on by `DeriveStatusWithVerdict`
  (`:207`) and its read-side wrapper `ResolveVerifiedStatus`
  (`tasks/verified_status.go:54`), which every surface routes through.
- Other manifest writers write no status: `writeRemediationTask` appends a
  task (`tasks/remediation.go:315`); `writeCommitConventionKey` writes one key
  (`tasks/commit_convention.go:109`).

**Layer 2: the Drain store.**

- A Drain row is one supervised execution (ADR-0055). Its terminal is the
  process exit reason only: `running`, `finished`, `quota_paused`,
  `verify_failed`, `interrupted`, `crashed` (`store/drains.go:15-20`), plus a
  Drain ending for two clean stops. The work disposition is never copied onto
  it (ADR-0056).
- `verify_verdicts` is keyed `(repo, set_id, work_sha)` (`store/store.go:211`)
  and is a cache. Pop already authors one verdict row itself: the
  range-undetermined NEEDS-HUMAN stand-in (`tasks/verify.go:589-598`). The row
  says nothing about who authored it, except the human-Accept flag.
- `refine_episodes`, `spent_retry_caps`, `checkout_gate_holds`,
  `recovery_waiters`, `admission_waiters`, `spawn_intents` each have one
  writer and one reader. Deferrals are derived from them, not stored.

**The drain loop** (`implementRun.loop`, `tasks/run_tasks.go:199`) is not a
table-driven state machine. It is a fixed sequence of phases over a fresh
manifest refresh each tick: `explorePhase` once at the head; then
`SelectTaskInSet`; an eligible task goes to `runSelectedTask`
(`tasks/run_selected_task.go:49`); otherwise, in order, `refinePhase`
(`tasks/refine_phase.go:50`), `verifyPhase` (`tasks/verify_phase.go:59`),
`terminalStatus` (`tasks/terminal_switch.go:35`). Each phase hands back a small
directive enum shaped continue / return / fall through. The loop reads
manifest-derived `Row.Status`; the verify phase computes the effective status
itself.

**Inside one attempt** (`executeTaskAttempts`, `tasks/attempts.go:108`) the
agent's word becomes a manifest write on this path: run the agent →
`assessAttempt` (`:590`) → `completeSuccessfulTask` (`:605`) → `git add -A`
and commit (`createImplementationCommit`, `:679`) → `finalizeTaskDone`
(`:732`) → `ApplyTransitions`. After `max_tries` the Agent fallback walk
writes `failed` with a three-way `attemptFault`: provider, contract, overrun
(`tasks/attempt_cap_exhaustion.go:16-33`). Only a provider fault leaves the
task open.

**Where LLM text becomes state.** Five parsers, all code-owned:

| Parser | Default when the text is missing or bad | Direction |
| --- | --- | --- |
| `ParseVerdict` (`tasks/verify.go:1636`) | NEEDS-HUMAN | fails closed |
| `AssessCompletion` (`tasks/assess.go:54`) | failed | fails closed |
| `splitRefinerReply` (`tasks/refine_commit.go:47`) | `refined` | **fails open** |
| `COMMIT-SUBJECT:` (`refine_commit.go:64`, `verify.go:646`) | pop's default subject | safe |
| Explore park (`tasks/explore_park.go:34`) | derived from run outcome, not prose | safe |

### What is good and should stay

- One pure derivation for set status, one chokepoint for task status, one
  atomic write. This is the model the gates should follow.
- The verdict cache is keyed by work SHA, so a re-drain at an unchanged tree
  invokes nothing. A gate result at set quiescence can use the same key.
- Pop already writes a verdict row from a code fact. A gate-authored verdict
  is not a new kind of thing.
- `attemptFault` already separates "the provider fell over" from "the work
  missed the contract". A red gate on a clean exit is a contract fault; no new
  fault kind is needed.
- The retry digest (`tasks/digest.go:125`) is already the channel that hands
  the next attempt what the last one did.
- The phase directives keep the loop readable. A gate is an input, not a
  phase, so no fifth directive is needed.

### What blocks the proposal, or makes it worse than it needs to be

1. **No storage for a machine fact about the work.** Gate results need two
   lifetimes. A post-attempt gate runs on an uncommitted tree; its result
   belongs to the attempt, like a Captured run, and the digest must read it.
   Today `persistAttemptStream` records `outcome` and `reason` strings
   (`tasks/attempts.go:180`), and gate output is larger than a reason. A
   quiescence gate runs at HEAD on a clean tree; its result should be keyed
   `(repo, set_id, work_sha, gate)` so a re-drain reuses it, like a verdict.
   Proposed: a `gate_results` table beside `verify_verdicts`, plus the output
   as a file under `<set>/gates/`, filed with the `pass_report.go` pattern.
2. **`verify_verdicts` has no author column.** Once code downgrades a PASS to
   FIXABLE because a gate is red, or stores a gate-authored FIXABLE without
   invoking the Verifier, a reader must tell an agent verdict from a gate
   verdict from a human Accept. The derivation stamp called low value above
   becomes necessary: one migration, `source TEXT` with `agent`, `human`,
   `gate`, `range`.
3. **`REFINE-OUTCOME` fails open.** Already listed under "Two fixes that need
   no manifest". `commitRefinePass` already reverts an abandoned pass against
   the pre-invocation snapshot (`refine_commit.go:228-248`), so the change is
   one constant and its tests.
4. **Effective status is overlaid in three places.** `ResolveVerifiedStatus`
   is documented as the single read-side resolution, but two writes sit
   outside it: `applyExploreMarks` overwrites READY with EXPLORE-FAILED in
   place (`tasks/explore_park.go:58`), and `verifyPhase` overwrites
   `row.Status = StatusVerifyFailed` for display (`tasks/verify_phase.go:167`).
   Neither is wrong today. With a gate result as a third input the overlay
   should have one home. This is the one refactor worth doing before the
   gates, because every later "why does status say X" question lands here.
5. **The gate runner and the manifest resolver sit on two sides of `tasks`.**
   The only runner today is in the eval harness (`eval/grade.go:216-244`), a
   separate program. Extract it to a leaf package below `tasks` with a
   three-way outcome, used by both eval and tasks. The Gate manifest resolves
   through the conventions stack, and `conventions` sits above `tasks`, so the
   resolver reaches the drain through a seam on `RunTaskSetOptions`, as
   `VerificationConvention` and `ImplementationConvention` do
   (`tasks/run_tasks.go:75-88`), wired in `cmd`.

### Where the two gate points land in the existing code

1. **Post-attempt, scoped gates.** In `executeTaskAttempts`, between
   `assessAttempt` returning `Complete` and `completeSuccessfulTask`
   (`tasks/attempts.go:299-306`). A red result is a new named reason beside
   `reasonUncheckedBoxes` (`tasks/assess.go:37-43`); the attempt is persisted
   as failed with that reason, the output is filed for the digest, and the
   loop retries under `max_tries`. Exit code stays zero, so
   `attemptFaultForExit` yields `faultContract` and the task ends `failed` for
   a human after the cap. Could-not-run does not fail the attempt; it is
   recorded and passed on.
2. **At quiescence, whole-tree gates.** In `drainVerifyPhase`, before
   `ensureVerifyVerdict` (`tasks/verify.go:745-746`). Run or reuse the
   SHA-keyed results; hand them into `buildVerifierPrompt` as evidence. After
   `ParseVerdict`, code downgrades PASS to FIXABLE when any gate is red, with
   the output prepended to findings and `source = gate`. The remediation loop
   then runs unchanged: `spawnRemediationIfUnderCap` spawns the task, the
   chokepoint invalidates, the SHA moves, gates and Verifier re-fire.

Human completion: the verify phase already suspends every non-PASS
disposition on a human-completed set (`tasks/verify_phase.go:133-140`); a red
gate becomes a mark, not a veto, through the same branch. Fold:
`runFoldSetVerify` (`tasks/fold_conflict.go:369-404`) calls the set-scoped
runner first and only then the Verifier; the runner must sit below
`tasks/binding`, which the leaf package does. Refine: after
`refineResolvedSet` returns, run the lint and format gates; a red result
forces `abandoned` before `commitRefinePass`.

### Recommended order of work

1. Flip `REFINE-OUTCOME` to fail closed.
2. Add `source` to `verify_verdicts` and stamp the four existing writers.
3. Extract the gate runner from `eval/grade.go` into a leaf package.
4. Fold the Explore overwrite and the drain's display overwrite into
   `ResolveVerifiedStatus`.
5. Add `gate_results` and the `<set>/gates/` filing.
6. Gate manifest kind and detection through the conventions stack, with a
   seam on `RunTaskSetOptions`.
7. Insert the two gate points. Then the Fold and Refine hooks.

Steps 1 to 4 make the machine strictly better with or without gates. Steps 5
to 7 are the proposal.

### What not to do

- Rewrite the loop as a table-driven state machine. The phase sequence is
  readable and tested; a gate is an input, not a state.
- Add a task status. `failed` with a contract fault and a gate reason is the
  right ending after the cap.
- Add a set status for "gate could not run". It is a flag on the evidence and
  a journal event, not a place the set can be.

## Open design questions to grill first

1. Three-way gate outcome: how does "could not run" surface in status, the
   digest, and the dashboard? Does it park the set or continue?
2. Where does the Gate manifest live: a fifth convention kind with an
   executable Shape (today `kind.go:47-67` has only two prose shapes), or a
   config leaf under `[repo.<path>]`, or a file the stack resolves?
3. Does a red `test` gate after an implement attempt fail the task, or return
   the output to the same agent for one bounded self-correction first?
4. Gate detection: probe once at register time and freeze in the manifest, like
   `commit_convention` (ADR-0228), or resolve live at each phase?
5. Should the Fold landing gate run `test` by default, replacing the LLM verify
   item as the first option?
6. The word "gate": accept the overload or pick a new term for the executable
   kind.
7. Should a post-attempt red gate spend a try from `max_tries`, or get one
   bounded self-correction outside the cap? The retry loop has no notion of a
   free retry today; a timeout and a turn-cap exhaustion both spend a try.
   One budget is simpler and matches ADR-0231. This sharpens question 3.

## Suggested skills

- `grilling` or `grill-me` on the open questions above before any code.
- `domain-modeling` to settle the term (Objective gate, Gate manifest, Gate
  detection) and record glossary deltas and the ADR.
- `research` if the ecosystem detection table needs primary-source facts on
  task runners and CI formats.
- `code-review` after implementation, against ADR-0227 and ADR-0268.

## Conventions to run before changing code

`pop conventions get implementation` before writing code;
`pop conventions get commits` before committing. Verify with
`go build ./... && go vet ./<pkg>/... && go test ./<pkg>/...`.
