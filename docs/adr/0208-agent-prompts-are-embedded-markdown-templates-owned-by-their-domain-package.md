---
status: accepted
relates: "gives the advisory contracts of [ADR-0102](0102-verifier-judges-only-done-afk-work-and-runs-before-the-terminal-hitl-gate.md) and [ADR-0163](0163-interrupting-a-live-drain-lands-on-an-interrupt-gate.md) a single home; applies the domain-contract seam of [ADR-0144](0144-behavior-tests-live-at-the-domain-contract-and-real-io-sits-behind-seams.md) to prompt construction; follows the fixture-backed testing of [ADR-0165](0165-stream-shape-capabilities-are-declared-and-fixture-backed.md); deliberately does not extend the committed-prompt overridability of [ADR-0138](0138-project-routines-are-committed-prompts-discovered-live-from-pop-routines.md) to harness prompts"
---

# Agent prompts are embedded markdown templates owned by their domain package

## Context

Pop generates fourteen agent prompts from Go. Ten of them are substantial
documents: `BuildAgentPrompt`, the five gate/assist prompts and
`BuildFoldConflictPrompt` in `tasks`, `buildVerifierPrompt` in `tasks/verify.go`,
and `wrapRoutinePrompt` plus the two authoring prompts in `routine`. Each is
built by a run of `fmt.Fprintf(&b, "…\n")` calls, one per line of prose, so the
words and the assembly logic interleave line by line and neither reads as what it
is.

The cost is not aesthetic. The same block, having no single home, is restated per
site and has already drifted three ways:

1. **The disposition rule, five ways.** Every gate prompt closes with the same
   invariant — the human owns the transition — but each enumerates its own
   forbidden list. HITL forbids "complete or skipped"; Interrupt forbids
   "complete, skipped, or reset"; Failed forbids "done or reset"; Verify-fail
   forbids "a verdict or remediation"; Assist forbids "complete/skipped/open".
   An agent at the HITL gate is never told it may not reset a task. No test can
   catch this, because each string is only ever asserted against itself.
2. **The routine framework contract, forked.** `routine/authoring.go` and
   `routine/project_edit.go` maintain near-verbatim twins of the whole
   PREAMBLE/POSTAMBLE/SENTINEL explanation, and it has diverged — the personal
   one warns against emitting "a conflicting end marker", the project one
   truncates before that clause. Both *paraphrase* what `wrapRoutinePrompt`
   actually does, so a change to the wrapper leaves two prompts lying to the
   agent that writes prompts against it.
3. **The manifest listing, five times**, with the verifier's copy already forked
   semantically (it filters to done-AFK work; the gate copies deliberately do
   not).

`CONTEXT.md` names each of these prompts as a domain noun with a precise
behavioural contract, so the text is the gate's contract rendered — not incidental
string data.

## Decision

The ten substantial prompts become **Prompt templates**: markdown documents
embedded with `//go:embed`, executed with `text/template`, rendered against a
named **Prompt view** struct.

- **Per-package ownership.** `tasks/prompts/` and `routine/prompts/`, each with a
  `partials.tmpl.md`. No partial needs to cross the package boundary — the
  disposition invariant is Task-set-only, the framework contract is Routine-only —
  and a shared prompt package would separate the contract text from the code that
  enforces it.
- **`.tmpl.md`**, so editors render them as markdown. The directory already says
  they are not shipped documents, unlike `integrate/skills/**/*.md`.
- **Prompt views hold all logic.** Every read behind the `Deps` seam, every
  `filepath.Join`, every filter and truncation happens in Go; a shared
  `taskRow` type carries pre-joined paths so one `taskSetContext` partial serves
  all six manifest listings. Prose never lives in Go; a template's control flow
  never goes finer than a whole section — optional single facts like `Work SHA:`
  arrive as header-line rows, not per-field conditionals.
- **Marker-free templates plus a post-render normalizer.** Naked `{{if}}` and
  `{{range}}` lines, with one renderer pass collapsing blank-line runs, stripping
  trailing whitespace and fixing the trailing newline. Whitespace bookkeeping is a
  property of the renderer, not something each template hand-tunes with `{{- -}}`.
- **`template.Must` at init, panic on execute** via a `mustRender` helper, keeping
  every builder's `string` signature and all ~30 call sites. Panic-at-init is
  house style (42 `MustCompile` sites). A silently degraded prompt is worse than a
  crash here, because the agent it briefs then edits a checkout.
- **Goldens and substring tests, both, permanently.** A substring test says a fact
  must reach the agent and survives reflow; a golden makes a prompt change
  reviewable in a diff, which is the point of the whole change.
- **Two commits.** First a pure migration rendering all ten byte-identical, with
  the divergent per-gate text preserved verbatim — mechanically verifiable by
  diffing captured goldens. Then the prose unification, as a small reviewable
  diff on a foundation already proven lossless.

Out of scope by the same rule: `buildTopicModelPrompt` (a one-off nudge, no
contract, no shared block) stays a Go literal, as do the TUI input prompts and the
wayfinder slash-command strings. The rule for future cases: **a prompt that
carries a harness contract or shares a block with another prompt is an embedded
template owned by its domain package; a one-off nudge with neither stays a Go
literal.**

## Consequences

Three deliberate behavioural changes land in the second commit:

- Every gate agent now hears the full disposition prohibition. The shared partial
  forbids *effecting* a disposition while explicitly permitting *drafting* for
  human confirmation — without that permission it would revoke Verify-fail
  assistance's documented right to draft a Remediation task, and Assist's right to
  edit the task manifest.
- The project authoring prompt gains the "conflicting end marker" clause it
  currently lacks, and both authoring prompts stop paraphrasing the wrapper: they
  render `wrapRoutinePrompt` itself with placeholder body and report-path
  arguments, so the description cannot drift from the behaviour again. The
  advisory prose the wrapper output does not carry ("write it as the routine's
  task, not as setup/teardown") is retained verbatim as a partial.
- The five gate prompts move from bare label lines to markdown headings, matching
  the verifier and authoring prompts. Nothing parses prompt text at runtime —
  `assess.go` parses the agent's output — so the only fallout is our own
  assertions.

`text/template` becomes the tree's first and only templating dependency. Template
errors move from compile time to run time, bounded by parse-at-init and by
goldens that execute every template against a filled view.

Two things are rejected rather than deferred. **Operator-overridable prompts** —
the `.pop/routines` model of ADR-0138 does not extend here: these prompts encode
harness contracts that pop's own parser depends on, and an operator editing
`SUMMARY_START` out of the worker prompt breaks assessment. **A central prompts
package** — it would let any caller render any prompt and would divorce contract
text from enforcing code.

One adjacent defect is deferred, not fixed: `SUMMARY_START`, `SUMMARY_END`,
`TASK_COMPLETE` and `TASK_FAILED` are hardcoded literals in the worker prompt,
compiled independently into regexes in `tasks/assess.go`, and written a third
time in `tasks/digest.go`'s retry lessons, with no shared constant — while
`routine/prompt.go` does it correctly. It is the same defect class, but extracting
the constants touches `assess.go`'s regex construction and would break the
byte-identity check. It belongs in its own change.
