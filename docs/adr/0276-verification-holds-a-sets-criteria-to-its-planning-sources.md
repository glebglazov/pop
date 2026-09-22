# Verification holds a set's criteria to the artifacts it was planned from

The **Verifier** judges a **Task set**'s work against its **Acceptance criteria**, and the criteria are declared authoritative in pop's own frame. Nothing asks whether the criteria are *right*. Where a set was planned from an external tracker item, the intent that item carried reaches the Verifier only insofar as a planning agent transcribed it — and if planning lost some of it, every gate stays green while the work misses what was asked for. Pop's whole vocabulary of provenance points inward: a per-task prose `## Parent` section, and an inert singular `source_map` manifest key whose own doc says nothing derives from it.

The decision, in six parts:

1. **A set declares its Planning sources, and a task claims the ones it answers.** The set-level list generalises `source_map`, which becomes one entry among others rather than a second mechanism answering the same question. The per-task claim mirrors `blocked_by`: ids in the manifest, prose left in the markdown. Both halves are needed — set-level alone cannot see that a declared source has no slice implementing it, which is exactly how planning drops a ticket, and per-task alone has no list to check completeness against.

2. **Intent drift is directionless.** A divergence between the criteria and a Planning source is reported as a divergence, never as the source being right. Design legitimately moves during a grilling session, leaving the source stale rather than the criteria wrong. The operator picks the repair: remediate the implementation, or amend the source. Presuming a direction would make every deliberate change read as a planning defect.

3. **Intent drift reaches NEEDS-HUMAN and never FIXABLE.** A **Remediation task** is spawned *from* the criteria, so it cannot repair criteria that lost the intent — it would be told to build the same wrong thing again. Only a human re-authoring the set, or amending the source, resolves it.

4. **Amending the source needs no new disposition.** The operator edits the tracker item and records an **Accept** with a note. The note already feeds forward as context into later Verifier prompts, which is precisely the behaviour wanted: once the source has moved, the next run must not re-flag what is no longer a divergence.

5. **The criteria still gate the work; the sources gate the criteria.** The rejected alternative made the Planning sources the direct contract and demoted the criteria to a hint. That breaks something structural rather than merely opinionated: the acceptance checkboxes are also the drain's *done* signal, the condition `pop tasks implement` reads back, so demoting them would leave the runner and the Verifier disagreeing about what the contract is. Under this reading the sources are still primary in the sense that matters — they are the only thing that can fail a set whose every criterion is met.

6. **`planning-sources` is a fifth Convention kind, not a widening of `issue-tracker`.** `issue-tracker` answers where work is filed; this answers where work comes from and how an agent reads one. In a team that plans in Jira and runs in pop's Work store those are different systems, and pop's shipped `issue-tracker` answer is 17KB of Work-store publish instructions that no prompt can carry. The new kind is step-informing — a short labelled block behind a config toggle, as the `implementation` convention rides implement prompts.

## Consequences

- For a set that declares Planning sources, verification gains a network and credential dependency. A source the Verifier cannot reach is an unrunnable gate, not a pass, so an unattended overnight drain parks on a tracker outage rather than finishing. This follows pop's own shipped verification standard, and the alternative — a PASS that silently skipped the primary reference — is indistinguishable from a real one. Sets declaring no sources are untouched.
- An implementer may read a Planning source for context, but the criteria remain its sole authority on what to build, and it does not report contradictions. Detecting drift stays with one role: two roles weighing the same question from different evidence, with nothing reconciling them, is the defect already noted between the Reviewer and the Verifier.
- `spec.md`'s `Source map:` prose line and the per-task `## Parent` section stay human-facing and unparsed, beside the manifest ids that are read.
