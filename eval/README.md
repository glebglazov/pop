# Eval

Eval compares the cost and graded quality of multiple ways to complete the same
work. Run it from the repository root with `go run ./eval`.

## Case layout

A Case is a portable directory under `eval/cases/<name>/`:

```text
case.json       repository URL, parent SHA, reference range, gates, scope, standards
spec.md         identical source words given to both arms
acceptance.md   Acceptance list, initially not approved
drafting/       Captured drafting run and its spend record
tasks/
  index.json    Pop arm Task-set manifest
  *.md          Pop arm AFK task files
```

The Case manifest contains no local repository or Task-storage path. A Case can
also be written by hand in this shape. `acceptance.md` must be reviewed and
approved before any Trial runs.

Draft the Acceptance list after Case preparation:

```sh
go run ./eval draft-acceptance <case-name>
```

The command uses the `claude` Agent preset by default. Use `--agent` to provide
another preset and model, for example `--agent "claude --model sonnet"`. It
writes `Status: draft` and a numbered list to `acceptance.md`. Review and edit
that list, then change its status line to `Status: approved` before a Trial.
The drafting Captured run and spend record live under the Case's `drafting/`
directory and do not belong to an Arm.

## Corpus selection

The five historical Task sets span about 10.2M to 29.3M tokens, as reported by
`pop tasks spend <set> --json`. Verify-report and drain-rendering are below the
15M to 55M middle spend band; they provide the refactor and output-convention
shapes in this corpus.

| Case | Shape | Source Task set | Historical tokens |
|---|---|---|---:|
| `2026-09-06-detail-keymap` | Multi-file feature | `2026-09-06-detail-keymap` in pop | 20,723,624 |
| `2026-08-28-work-lift` | Bug with a reproduction | `2026-08-28-work-lift` in pop | 29,348,958 |
| `2026-08-29-verify-report` | Refactor on one report seam | `2026-08-29-verify-report` in pop | 13,210,089 |
| `2026-08-21-drain-rendering` | Output-convention change | `2026-08-21-drain-rendering` in pop | 10,209,180 |
| `2026-09-05-carbon-hotkeys` | Multi-file feature | `2026-09-05-carbon-hotkeys` in vibe-coding-done-right | 28,425,513 |

The manifest is JSON with this shape:

```json
{
  "name": "case-name",
  "repository_url": "https://example.com/project.git",
  "parent_commit": "0123456789abcdef",
  "reference_range": "0123456789abcdef..fedcba9876543210",
  "gate_commands": ["go build ./...", "go test ./..."],
  "scope": ["."],
  "standard_documents": ["docs/agents/implementation.md", "AGENTS.md"]
}
```

Create a Case from a historical Task set:

```sh
go run ./eval prepare <repo-path> <set-id>
```

The default Case name is the Task-set identifier. The default Objective gates
are `go build ./...` and `go test ./...`; the default scope is `.`. Standard
documents default to `docs/agents/implementation.md` and `AGENTS.md` when each
exists at the parent commit. Repeat `--gate`, `--scope`, or `--standard` to set
explicit values. Use `--name` to select another Case name.

## Arms

The Bare arm gives the Case spec to one headless agent invocation. It does not
use task planning, retries, Refine, or a Verifier.

The Pop arm gives the same spec to Pop as the prepared Task split. Pop drains
the AFK tasks and can use its configured retries, Refine, and Verifier.

This comparison has a planning asymmetry: the Pop arm receives the same words
already split into tasks, while the Bare arm receives them as one spec. Case
preparation is not part of either arm's measured spend.

Each Arm is a TOML file at `eval/arms/<name>.toml`:

```toml
kind = "bare" # bare or pop
agent = "claude" # Agent preset spec, with optional arguments
model = "opus" # required; do not also set a model in agent
```

Pop arm files can also contain `[config]` and `[manifest]` tables. Their keys
and nested tables hold Pop config and Task-set manifest overrides. Bare arm
files cannot contain these overrides. Unknown top-level keys are refused.
Keep the Agent preset and model equal across the Bare and Pop arms to compare
Pop's effect. The shipped Bare arm uses `claude` with `opus`.

## Run the Trial Matrix

```sh
go run ./eval run --case <name> --arm bare
```

Repeat `--case`, `--arm`, and `--repeat` to select a Matrix. This is the
two-Case, two-Arm milestone command:

```sh
go run ./eval run --case <first-case> --case <second-case> --arm bare --arm pop --repeat 1
```

With no selection flags, the command runs every approved Case against the Bare
and Pop arms at repeat 1. A selected Acceptance list must have
`Status: approved`. The command checks approval before it creates a clone.
Trials run in repeat-major order: every selected Case and Arm runs for one
repeat before the next repeat starts. An existing `trial.json` is skipped, so
the same command resumes a stopped Matrix. An Invalid Trial runs one more time
in the same result location. A second invalid outcome stays recorded and does
not stop the rest of the Matrix.

The command grades each Trial after its final attempt. `--grade-timeout 1h`,
`--graders`, and `--config` set the same Grader inputs as the standalone
`grade` command.

Progress is plain text on stderr, so stdout remains available for saved paths
and JSON. A one-Trial Matrix reports the observable phases in this shape:

```text
Eval Matrix: Trials=1 results=/path/to/repository/eval/results
Trial 1/1 Case=example Arm=bare repeat=1 attempt=1
Trial Case=example Arm=bare repeat=1 attempt=1 preparation started
Trial Case=example Arm=bare repeat=1 attempt=1 preparation finished
Trial Case=example Arm=bare repeat=1 attempt=1 Arm execution started
Trial Case=example Arm=bare repeat=1 attempt=1 Arm execution finished: outcome=completed
Trial Case=example Arm=bare repeat=1 attempt=1 patch saving started
Trial Case=example Arm=bare repeat=1 attempt=1 patch saving finished: path=/path/to/result/diff.patch
Trial Case=example Arm=bare repeat=1 grading preparation started
Trial Case=example Arm=bare repeat=1 grading preparation finished
Trial Case=example Arm=bare repeat=1 Objective gate started: go test ./...
Trial Case=example Arm=bare repeat=1 Objective gate finished: passed exit=0 command=go test ./...
Trial Case=example Arm=bare repeat=1 Grader execution started
Trial Case=example Arm=bare repeat=1 Grader execution finished: outcome=completed grade=graded
Trial 1/1 Case=example Arm=bare repeat=1 finished: outcome=completed grade=graded acceptance=3/3 quality=4/5 result=eval/results/example/bare/01
Eval Matrix finished: completed=1 timed_out=0 invalid=0 graded=1 gate_failed=0 ungraded=0
Read the Rollup: go run ./eval rollup --results /path/to/repository/eval/results
```

When an operation stays quiet, the harness writes a bounded line every 30
seconds. For example:

```text
Trial Case=example Arm=bare repeat=1 waiting: phase=Bare-agent invocation elapsed=1m0s ceiling=4h0m0s
Trial Case=example Arm=bare repeat=1 waiting: phase=Objective gate 1/2 elapsed=30s
```

These lines prove only that the harness is still waiting for the named
operation. They do not measure agent progress or estimate a completion
percentage. A line shows a ceiling only when the harness enforces one for that
operation. The same plain stderr lines appear on a terminal and in redirected
logs; raw agent transcripts remain in their captured files.

Resume output says whether Eval skips a saved, graded Trial or grades a saved
patch without another Arm invocation. Invalid Trial retries, Trial ceiling
results, skipped grading, and failed phases also state their reason here.

For a Bare-arm Trial, the command clones the Case repository, checks out the
parent SHA detached, and makes one captured invocation. The prompt has a fixed
preamble with the repository URL, completion instruction, no-commit rule, and
Objective gate commands, followed by the spec without changes.

Use another `--repeat` flag to select another repeat number.
`--ceiling 4h` sets the agent's Trial ceiling (four hours by default).
`--cases`, `--arms`, `--work`, and `--results` change the directory roots.
Run the Pop arm with `--arm pop`. It uses the shipped `pop` binary on PATH;
`--pop /path/to/pop` selects a built binary. It registers the prepared Task set
without auto-drain, then runs whole-set implement with closed stdin. All Pop
commands use the Trial's own `data/` and `config/` directories. The config
pins the Arm agent for Implement, Verify, Refine, and Explore, enables Verify
and Refine, inlines the implementation convention, sets max tries to 3, and
removes the Turn cap. Both config layers stay inside the Trial. Explore is
enabled and requested.

The Pop record adds `set_status` and `set_spend`. `set_spend` retains the complete
Spend lens JSON, including phase totals and rows; the single-run `spend` and
`notional` fields apply to the Bare arm. Done, Verify-failed, and Failed are
completed Trials. A nonterminal stop is invalid. A ceiling expiry is timed out;
status and spend collection then have a separate one-minute bound.

The following files are declared and **not yet run**. Each is the Pop arm with
one override: `refine-off.toml`, `verify-off.toml`, `max-tries-1.toml`,
`convention-off.toml`, `explore-off.toml`, and `model-swap.toml` (Sonnet).
Do not run these arms until the Bare/Pop result is stable.

Records are written to `eval/results/<case>/<arm>/<NN>/trial.json` and
`diff.patch`. The record holds the Case, Arm, repeat, attempt count, requested and actual model,
start and end times, outcome, Captured run ID, spend, notional cost, and work
directory. Unknown spend figures retain the capture seam's presence flags.
The patch compares the final tree with the parent, includes new files and
binary data, and has no Trial path or Arm label added by the harness.

The outcomes are `completed`, `timed_out`, and `invalid`. A quota pause or
agent crash produces an Invalid Trial; its agent outcome and reason remain in
the record. The Matrix retries it once. The standalone `grade` command remains
available to grade a stored Trial again without running its Arm again.

Clones and Captured runs stay under `eval/work/trial-<random>/repository` and
`capture`. These names do not identify the Arm. A later Grader must receive only
the parent repository, patch, and Acceptance list, never the Trial record or
Captured run. `eval/work/` is local work and is ignored by Git; results remain
available to commit.

## Grade a Trial

```sh
go run ./eval grade <case> <arm> <repeat>
```

Grading restores `diff.patch` in a fresh detached clone at the Case parent SHA.
Every Objective gate runs through `sh -c` in that clone, in manifest order. The
Trial record's `grade.gates` stores each command, exit code, output, and error.
Any gate failure sets `grade.status` to `gate_failed`, gives zero list ratio and
quality, and skips the Grader. Invalid Trials remain ungraded and excluded;
Trials that reached the Trial ceiling receive zero without a Grader.

Scope entries are repository paths: a file or a directory and its descendants.
`.` permits the whole tree; an empty scope permits no changed files. Added,
deleted, and renamed paths are checked. Files outside scope are passed to the
Grader as a flag and do not fail a gate.

`eval/config.toml` selects `grader_arm`, loaded from `eval/graders/<name>.toml`
in the same file shape as a Bare arm. The default is Codex with `gpt-5.5`.
Grading rejects a Grader model used by any file in the Arm directory or by the
Trial's requested or reported model. Grader files have a separate directory so
that they are not counted as comparison Arms.

The Grader runs through the capture seam in another fresh clone at the parent
SHA. Its prompt contains only the Trial diff, approved Acceptance list, quoted
parent-tree standard documents, scope flag, and quality dimensions. Missing
`standard_documents` uses the Case preparation defaults; an explicit list
replaces them, and each named document must exist. No Arm label, Trial record,
Reference diff, or other Trial is supplied. The Grader is instructed to inspect
only the parent tree and supplied diff, without inspecting history or other
refs. This is an input contract, not an agent filesystem sandbox.

The reply has one line per numbered Acceptance-list item, followed by quality:

```text
ITEM 1 MET: The requested behaviour is present.
ITEM 2 NOT_MET: Existing output is missing.
QUALITY 3: The code is clear but leaves compatibility work.
```

Items must be in order, with nonempty one-line reasons. Quality must be 1–5
with a nonempty one-line rationale. Extra text, missing items, or invalid scores
leave `grade.status` as `ungraded`, with no guessed scores. `grade.reply` keeps
the whole reply in all cases. Successful grading writes `grade.scores`, including
the per-item decisions, list ratio, quality, and rationale.

`grade.grader` stores the Grader's Captured run ID, outcome, actual model, spend,
and notional cost separately from Arm spend. Captured runs are stored in the
Trial result directory under `grading/`. Regrading replaces the grade fields;
it does not run the Arm again or change its spend. Previous capture files stay.
`--config`, `--graders`, `--cases`, `--arms`, `--work`, and `--results` select other
paths. `--timeout` sets the Grader ceiling (one hour by default). Flags precede
the three positional arguments.

## Read the Rollup

```sh
go run ./eval rollup
go run ./eval rollup --json
```

The Rollup reads every `trial.json` below `eval/results` and prints one row per
Case and Arm. Each spend and work-shape cell is `median/spread`; spread is the
maximum minus the minimum. The cells cover total tokens, notional cost, turns,
peak input, and wall-clock seconds. The row also shows median Acceptance-list
ratio and quality score, plus gate-failure, Trial-ceiling, Invalid, and
ungraded counts. Invalid Trials do not contribute figures. An absent figure is
`—`; the final column counts token-, rate-, turn-, peak-, and wall-clock-blind
Trials. `--json` emits these same rows and uses `null` for absent medians and
spreads. Use `--results` to read another Trial record root.
