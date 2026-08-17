---
status: accepted
relates: "gives the Task-set side the attended write surface [ADR-0184](0184-map-assist-and-the-authoring-contract.md) gave Maps, across the seam [ADR-0183](0183-authoring-rules-are-owned-by-the-binary.md) drew; narrows the human half of [ADR-0103](0103-human-verdict-disposition-is-accept-or-remediate.md)"
---

# An attended session holds an authoring grant, and remediation stops being a creation verb

## Context

A human at a HITL gate asked the assistance session to file a ticket for an
improvement. The session first hand-wrote a set folder and produced a MALFORMED
set, then ran `pop tasks verify <set> --remediate` and was refused: *"checkout is
held at a verification gate by set %q (PID %d since %s)"*.

Both failures were correct behaviour, and neither was the bug.

The refusal was right twice over. `--remediate` is an **Out-of-band mutation**
gated on checkout quiescence (`tasks/verify.go:278`, ADR-0104), and the HITL gate
had registered a non-claiming **Checkout gate hold** owned by the
`pop tasks implement` process (`tasks/terminal_switch.go:136`). Quiescence exempts
only a hold owned by the mutating process itself (`store/quiescence.go:228`), and
an assistance session is a different process. Separately, the shared prompt
partial already forbids the agent from spawning remediation at all.

Creation never consults quiescence: `mutateWithCheckoutQuiescence` has exactly two
call sites, human Accept and human Remediate. `pop tasks register` never asks. So
authoring a set from inside a HITL session already worked mechanically — the
session simply had no idea it was allowed to, and reached for the one adjacent
verb it had been told about.

Two things made that verb reachable. First, the prompt: the HITL gate hands the
agent a closed list of four *human* outcomes and no authoring rules at all, then
ends with the shared partial whose second half says "You may draft what the human
then confirms. A task body, **a Remediation task**, an edit to the task manifest,
or implementation under the runtime checkout". The only ticket-shaped artifact
ever named to an attended agent was a Remediation task. Second, the verb itself:
`--remediate` inspects no verdict (`tasks/verify.go:204`). It ignores the
verdict, `verify.enabled`, the set's verify opt-out and the **Remediation depth**
cap, while deleting the set's cached PASS and appending itself to `blocked_by` of
every open HITL task. It is a general "append an AFK task to this set" tool
wearing the name of verification repair. Only the two *interactive* entries are
verdict-gated (`tasks/verify_phase.go:134`, `tasks/assist.go:174`).

The Map side settled this shape already: ADR-0184 gives an attended Map session an
explicit write surface including "create tickets", hand-written and re-validated
by re-running `pop map register`. The Task-set side never got its half.

## Decision

**Every attended session holds an authoring grant, and remediation is a
disposition rather than a creation verb.**

### The authoring grant

An attended session may create a new Task set, append a task to the set at hand,
and run `pop tasks register` itself to validate what it wrote. Five workflow rules
carry it:

1. You may create a new Task set, or append a task to this one, when the human
   asks.
2. Default to *this* set; mint a new set only when the idea sits beyond this set's
   slice.
3. Run `pop tasks authoring-guide` before writing — it is authoritative.
4. Writing files only *drafts*. Run `pop tasks register` and work the MALFORMED
   fix list until the set reads READY.
5. Creating work is not a disposition — it completes, skips, accepts and
   remediates nothing at this gate.

Plus one judgment rule with no enforcement behind it: an appended task that the
set's open HITL gates should wait on is wired into their `blocked_by`, the way a
remediation spawn wires itself (`tasks/remediation.go:284`, ADR-0155). Nothing
derives that edge for a hand-append, and nothing should — an appended task
genuinely may belong outside a gate's scope. This is the same kind of stated,
unenforceable boundary ADR-0184 accepted.

The agent runs `register`, not the human. Registration mints a *new* container and
decides nothing about the gated set, so it costs no invariant, and it keeps one
validator instead of two surfaces that can disagree. `pop tasks authoring-guide`
gains nothing: ADR-0183 already made it authoritative for layout, templates,
manifest fields, HITL/AFK typing and a "What registration enforces" checklist.
Every rule needed was already printed there and merely never pointed at — the verb
appears in exactly one prompt today, Assist's.

### One paragraph becomes two blocks

`tasks/prompts/partials.tmpl.md`'s `disposition-invariant` holds a wall and a door
in one block, and widening the door means editing the paragraph that holds up the
wall. Split it, and name each block after its own opening sentence rather than a
coined noun:

- **`the-human-decides`** — the prohibition. "The human decides every outcome
  here", then the closed list: no task status change, no verdict recorded, no
  accept, no remediation spawned. This list is meant never to grow.
- **`you-may-draft-what-the-human-confirms`** — the grant. The drafting sentence
  moves here out of the prohibition, and the five rules above follow it.

The old opening sentence, "The human owns the transition", is dropped. **Task
transition** is an established term meaning the governed move of one task between
the four statuses — one of the four things the block forbids. The sentence borrowed
a term narrower than its own scope.

Both blocks are invoked by all five prompts that embed the invariant today
(`hitl-assistance`, `verify-failed-assistance`, `failed-assistance`,
`interrupt-assistance`, `assist`). `fold-conflict` embeds neither and stays
untouched. Assist's own "Operations you may perform" list is trimmed to defer to
the grant rather than state task-appending a second time.

### Remediation narrows

`pop tasks verify <set> --remediate` is refused unless the set's **Verification
mark** reads `verify-failed`. The refusal names the current mark and points at
`pop tasks authoring-guide` for adding work to a set that has not failed
verification. A refusal that only says no is what sends an agent hunting for the
next door, which is how this started. The auto path is unchanged — it is already
gated on a FIXABLE verdict under the depth cap.

## Considered Options

**Leave `--remediate` open and rely on prose.** Rejected. The flag being usable on
a passing set is what taught the agent it was a creation verb, and its side
effects are destructive: it deletes a cached PASS and blocks every open HITL gate.
Gating it makes "one creation path" true in code rather than only in the guide.

**The agent authors and the human registers, with a new `register --check`
dry-run.** Rejected. That is a second surface over one validator, and without it
the agent hands over an unvalidated draft — precisely the malformed set that
started this. The `--check` verb remains available as a fallback if agent-run
registration is ever withdrawn.

**Treat `to-tasks`'s `disable-model-invocation: true` as precedent against
agent-initiated work creation.** Rejected. That gate guards *closing a Map* — the
moment a human judges wayfinding done — not the creation of work. A follow-up
ticket at a gate ends nothing.

**Add the rules to each gate's "Allowed outcomes" list.** Rejected. Those lists
enumerate the *human's* dispositions at that gate; filing a ticket is not one.
Five copies is also the exact divergence the shared partial was created to end —
"five divergent forbidden-lists had left each gate silent about a different
mutation".

**Keep one block under a better name.** Rejected: a wall and a door have opposite
directions and different reasons to be edited.

**Make `register` warn when open AFK tasks are unreachable from any open HITL
gate.** Not rejected, deferred — a validator change with its own scope, not part
of this decision.

**A parent-set lineage key, mirroring `source_map`.** Rejected. It means touching
the validator, the guide and the marshaller for a link no read surface consumes;
a follow-up set's `spec.md` can name its parent in prose.

## Consequences

- Five prompts gain a block invocation, and the five golden files under
  `tasks/testdata/prompts/` need regenerating, since the rendered text changes.
- Attended sessions can create work unattended-of-the-human. Accepted: the grant
  is explicit, `register` validates every structural failure, and a drafted set is
  inert until registered.
- The `blocked_by` wiring rule is prose an agent may ignore, so a hand-appended
  task can leave a gate signable while it is still open. The pre-existing gap on
  Assist's append path is unchanged, not widened.
- `--remediate` loses its use as an out-of-band task-appender. The replacement is
  hand-authoring per the guide, which the refusal names.
- **Disposition invariant** is retired from the glossary — each block name now
  states its own rule in full — and **Authoring grant** replaces it as the term
  ADRs can name, because it is the half that will keep changing.
