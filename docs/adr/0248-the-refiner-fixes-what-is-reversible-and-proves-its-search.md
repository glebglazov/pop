# The Refiner fixes what is reversible, and proves its search

[ADR-0252](0252-refine-fixes-in-place-before-the-verify-phase.md) gave the
**Refiner** a licence to fix "safe, local, convention-named" changes, and
`refiner.tmpl.md` spelled that out as: anything structural is report-only. In
practice that licenses almost nothing, and it is backwards from the argument it
was reaching for — Beck's point is that structural changes are the *reversible*
ones, which is precisely why they are the safe ones to make. The pass either
renames a variable or writes a finding.

This ADR widens the licence and pays for it with evidence rather than with
caution. Everything in it is prose in `tasks/prompts/refiner.tmpl.md`
(ADR-0247), so it is reversible by editing a document.

## The licence

1. **Reversibility replaces safe-and-local.** Two questions per fix: can this be
   undone by inspection, and can I see its whole effect? Both yes, fix it.
   Report-only becomes behaviour, exported interfaces, stored shapes, and Beck's
   own carve-out — structure changes that are expensive to reverse, such as
   extracting a service or crossing a module boundary.

2. **Reading is unbounded; editing is anchored.** The Refiner may read anything in
   the repository to prove a fix safe. It edits outside the changed files only
   where a fix inside them forces it — a rename's call sites. No repository
   sweeps.

3. **The report shows its search.** Since reading is what grants the licence, the
   Refiner names *how* it established that it found every affected site, rather
   than asserting a completeness it never checked.

4. **No numeric budget, and no closed list of permitted refactorings.** A name on
   a permit list licenses a kind of move but not the judgement of whether the move
   helps; "Extract Helper" on such a list is an invitation to extract forty
   helpers. The brakes are principles *with tests*: Ousterhout's conjoinment,
   deep-not-shallow, the Rule of Three counted rather than felt, and "unfamiliar is
   not a finding". The **Refine commit** being one revertible unit is the backstop.

## Findings

5. **Created versus revealed is the line, not location.** Did this changeset
   *create* the problem or *reveal* it? Created — merging locks the shape in, so
   it belongs to reviewing this work. Revealed — the shape predates the change and
   the fix costs the same later, so it is future refactoring work. It is checkable
   with `git show <base>:<file>`, which matters because the Refiner is an LLM and
   a checkable test beats a felt one.

6. **A revealed finding is stated once and never carried forward.** The report is a
   living document, but a revealed finding can never be fixed by a later pass, so
   carrying it forward turns the report into a backlog — the exact noise ADR-0252
   refused. Prior reports stay on disk under the set's directory, so nothing is
   destroyed, only unpointed. Every out-of-set finding names its refactoring, so it
   is executable rather than advice, and is admissible only when the changeset is
   the evidence: an existing coupling forced the change into scattered places, or
   the new code had to work around the old shape.

7. **The report grows a third part**: Fixed / Left in this changeset / Revealed by
   this changeset. The carry-forward rule is the instruction most likely to be
   quietly dropped on pass three, and a section boundary enforces it where a
   sentence cannot — replace this section, carry that one. It also splits the two
   different decisions a reader is making at merge time: one block is about this
   work, the other about the codebase.

8. **No task spawning.** ADR-0252's rejection stands. The human reads the report and
   acts.

## Gates and tests

9. **The Refiner runs the scoped gate before and after its pass**, and fetches
   `pop conventions get verification` itself rather than being handed a block —
   that kind is role-driving, so inlining it would re-create the wart ADR-0246
   removes, while *fetching* claims no shape and is precedented by `issue-tracker`
   and by ADR-0252's own Assist hint. A red gate on entry means the pass fixes
   nothing and reports. The after-run is what makes the widened licence safe. This
   is a *scoped* gate, not the whole-tree one ADR-0252 declined to re-run.

10. **A red gate on exit abandons the pass, and pop discards its changes.** After
    one bounded self-correction attempt, the Refiner reports the outcome and pop
    reverts, capturing the tree state before invoking the Refiner so "the pass's
    changes" is a real diff rather than everything dirty. Leaving a broken tree in
    a Refine commit is the one outcome that makes the widened licence a net loss;
    letting the Refiner iterate turns the pass into an unbounded debugging session
    on code it did not write.

11. **Test-blocked refactorings are licensed, via characterization tests.** Scoped
    gate green as found → write a behaviour-level characterization test, learning
    the expected value by running it rather than asserting it → **it must pass
    against the tree as found** → refactor → it must still pass → only now may the
    implementation-pinned test and its test-only accessor go.

12. **The one absolute: never edit a failing test's assertion to match the new
    code.** This is the mechanism by which an autonomous refactoring agent ships a
    regression with a green suite. Stated positively: never delete protection, only
    replace it, and show the replacement passing on both sides. Everything in
    decision 11 exists to make this rule costless to obey.

13. **A test may be deleted only when the Refiner can point at the survivor.**
    Deletion is licensed when another test — including a higher-level one that
    subsumes it, per Khorikov — asserts the same behaviour, and the report names
    that test by file and line. The brake is the pointer requirement, not a
    restriction on which kind of test may survive: "deleted X, protection remains
    at Y:NN" is checkable in five seconds at merge time, where "the integration
    test covers it" is a sentence that licenses deleting most of a unit suite.

## Mechanism

14. **The Refiner reports the pass outcome on a machine-read line**,
    `REFINE-OUTCOME: refined | gate-blocked | abandoned`, lifted by
    `splitRefinerReply` beside `COMMIT-SUBJECT:` — the shape `VERDICT:` already
    uses. pop needs the outcome, not the gate readings; the readings belong in the
    report prose. This is self-reported, so a Refiner that wants its work committed
    can write `refined` over a red gate. That is the same trust model as `VERDICT:`,
    and it is caught one step later by the verify phase, which is a fresh agent
    running the whole-tree gate — more expensively, but caught.

15. **A gate-blocked or abandoned pass records no Refine episode.** An episode
    means "this composition has been refined", and a pass that read nothing and
    wrote nothing has not refined it. Recording one would let a transient build
    failure cost the set its refine pass permanently. It re-arms and retries at the
    next quiescence, which is cheap because the gate-red path exits before any
    agent work.

## Consequences

- **Counted duplication replaces "written twice wants extracting".** The shipped
  text's current line is the single strongest bias an LLM brings to this job and it
  is wrong. Before extracting, the Refiner finds every occurrence in the repository
  and states the count: under three, silence — not even a finding; three or more,
  extract. This turns the Rule of Three from a heuristic about time, which the
  Refiner has none of, into mechanical evidence, using the unbounded reading of
  decision 2.
- **A duplication whose earlier copies predate the changeset is a revealed
  finding, not a fix**, even when the changeset added the third copy. Decision 6
  already admits it — the changeset is the evidence — and the alternative,
  "the changeset created the threshold crossing", is a sentence a model can talk
  itself into for almost any duplication.
- **"A refactor changes no test file" survives only as a report line, not as a
  gate.** Decision 11 writes test files by construction. Three traditions converge
  on the rule and it stays worth stating; it stops being checkable.
- **Refine becomes the heaviest step in the drain.** Accepted: ADR-0252's carve-out
  already keeps it off remediation laps, and decision 15 keeps a red gate from
  costing an agent invocation.
- **Commit-splitting is dropped from the text entirely.** Neither consumer can
  commit — both prompts forbid it and the runner does it — so the pipeline already
  achieves the outcome. The structure/behaviour *distinction* stays, as a way to
  see a change.
- Named authorities are used as terms of art — *reversible*, the Rule of Three, a
  characterization test — and nothing is quoted. A term of art is compression a
  model already has the weights for; a quotation would put the research's sourcing
  ledger on the critical path for no gain.
