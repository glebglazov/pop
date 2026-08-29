# Refine fixes in place before the verify phase

Code review as a report-only step failed in practice on all three fronts its
design accepted as costs: the artifact went unread and unactioned, its findings
were weak, and turning a finding into work was manual friction every time. The
step is renamed **Refine** and becomes a writing step: a fresh **Refiner**
researches the set's changeset, fixes in place what the resolved `refine`
convention licenses — safe, local, convention-named fixes — and writes the
**Refine report** of what it fixed and the smells it left, with the SHA it
describes. This overrules the central rulings of
[ADR-0214](0214-code-review-is-a-drain-step-that-maintains-a-living-document.md)
(the step reads and never writes; it runs after verify) and retires the
Reviewer's use of the read-only posture from
[ADR-0221](0221-the-reviewer-runs-under-a-read-only-agent-posture.md), while
keeping what survives of both: the step-not-task shape, the living superseding
document, the artifact staying a Task artifact, the convention being the
prompt body (ADR-0227), and the posture capability itself.

## Considered options

**Keep report-only and improve the report** — rejected. "Reaches no verdict,
gates nothing, spawns no work" predicts exactly the observed failure: a
document with no consequence decays into noise. ADR-0214's guardrail — no
automatic rewrites on aesthetic grounds with no human in the loop — is
replaced by a different one: the convention is the license. It is the human's
and the repository's own prose (the Convention stack always answers, and any
written rank displaces pop's shipped text whole), so fixes made strictly under
it are human-directed, just directed in advance.

**Spawn Remediation tasks from findings** — ADR-0214's originally rejected
option, rejected again. It re-creates the friction (a task ceremony per
finding batch), and quality findings do not need task-grain visibility: a
correctness finding is a claim the acceptance criteria were not met — that is
work, and stays a Remediation task — while a quality fix has no criteria to
satisfy. The two sibling steps fixing through different mechanisms is
deliberate.

**Keep the step after verify, with mandatory re-verify when it edits** —
rejected for the loop cost: every refine pass would force a second heavy
verification. Placed *before* the verify phase instead, the pass's edits are
judged by the same verification as the work they refine, for free.

## Consequences

- **Placement**: Refine runs at AFK quiescence, before the verify phase.
  ADR-0214's reason for the late placement (the document must describe the
  final tree) is demoted: the report now names the SHA it describes, and the
  Refine pointer already tolerates staleness.
- **The Refine episode gets one carve-out**: it re-arms only on
  *non-remediation* done-AFK composition change. A verify-remediation lap
  re-verifies without re-refining, so the heavy pass stays out of the
  iteration that must be cheapest. A remediation diff lands unrefined until
  real work re-arms the episode.
- **The runner commits the pass** — one **Refine commit** per pass, subject
  under the commits convention, the set named in a trailer. Agents still never
  commit, and the tree is clean when verification reads it.
- **Refine edits never invalidate a verify PASS.** They behave like any other
  commit past a PASS: the verdict stands within its episode and the
  Verified-at SHA badge shows the drift. In the automatic flow the ordering
  makes this moot.
- **Human completion**: automatic Refine skips a human-completed set — direct
  edits are stronger than the suspended verdict dispositions that rule already
  guards against. Manual `pop tasks refine` remains allowed on any eligible
  set, including human-completed and DONE ones: the human re-opening the
  question.
- **Full rename, including the Convention kind**: `code-review` → `refine`
  everywhere — glossary family (Refiner, Refine report, Refine episode, Refine
  pointer), `pop tasks refine`, `[work.refine]`, `docs/agents/refine.md`,
  overlay filenames, the artifact type tag and the set's reports directory.
  Acceptable precisely because the feature was beta with a single user; the
  kind follows the step so one word names the whole family.
- **Upfront adherence is a separate toggle**:
  `[work.implement].include_refine_convention` (default false) inlines the
  resolved `refine` convention as a labelled block in every implement prompt —
  planned and Remediation tasks alike — independent of `[work.refine].enabled`,
  so adherence can be driven before the pass is ever enabled. This is a second,
  block-shaped consumption of a role-driving kind; the consumption shape names
  the mandate-bearing one.
- **One text, two consumers**: the shipped `refine` convention is rewritten
  short and rule-shaped, fit to ride every implement prompt; a repository that
  writes a long document pays that length in its own prompts, its choice.
- **The read-only posture loses its only consumer but stays declared.** It is
  preset capability metadata, not pipeline code, and the per-preset flag
  research should not be re-derived for the next read-only role. The Refiner's
  prompt states its license instead: fix what the convention names where safe
  and local, report the rest.
- **Assist gains a hint, not a mechanism**: one line in the Assist prompt
  telling the agent to fetch `pop conventions get refine` when the session
  concerns code quality. The Assist prompt already carries the report pointer
  and already may edit the checkout when the human asks; `pop tasks refine`
  joins `implement` and `verify` on its do-not-invoke list.
