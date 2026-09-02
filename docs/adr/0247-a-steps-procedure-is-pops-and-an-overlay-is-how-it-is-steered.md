# A step's procedure is pop's, and an overlay is how it is steered

[ADR-0227](0227-a-role-driving-convention-is-the-prompt-body-pop-owns-only-the-frame.md)
made a role-driving convention the whole body of an agent's prompt, on the
reasoning that a role's *mandate* is never pop's opinion.
[ADR-0246](0246-the-implementation-convention-is-the-standard-both-builder-and-refiner-read.md)
moves the refine step's standards into the `implementation` kind, which leaves
`refine` naming something else entirely: the procedure a **Refiner** follows —
the reversibility test, created-versus-revealed, when to run the gate, the shape
of the report. That is not a repository's opinion and never was. Nobody wants to
rewrite it; they want to add to it.

**A convention holds a repository's facts and standards. A step's procedure is
pop's, and is not overridable.** Under that rule `implementation`, `commits` and
`issue-tracker` are conventions untouched, and `refine` stops being a
**Convention kind** at all.

The decision, in four parts:

1. **`refine` leaves the kind set.** `pop conventions get refine` refuses with the
   ordinary unknown-kind message listing the four kinds that exist. The rejected
   alternative kept it as a kind with a shipped-only, non-writable stack: a stack
   with the stacking removed, costing `Kinds()`, `Shape()`, `Desc()`, `ParseKind`
   and a write path that refuses every rank, to serve a `get` nobody needs.

2. **The procedure moves into `tasks/prompts/refiner.tmpl.md`, whole.**
   ADR-0227's frame/body split existed because the body was someone else's text.
   With both halves pop's, keeping them in two files means the Refiner's mandate
   can never be read in one sitting — which is exactly what an author must do when
   the reports come back wrong.

3. **An overlay is keyed on a named document, not on a kind, and gains a
   repository rank.** The layer that *appends* — the human's constraints riding
   along with whichever text answered — becomes the general way anyone reaches a
   document they may not replace. Two changes: it is addressable for a step prompt
   (`refine`) as well as for a kind, and it exists at the repository rank
   (`docs/agents/<name>.overlay.md`) beside the human's
   (`~/.agents/docs/<name>.overlay.md`). Without the repository rank, "not
   overridable" would be lossy rather than principled: a *team* with a house rule
   about the pass — never touch the generated client — would have nowhere
   committed to put it.

4. **One noun: Overlay.** It is the human's or the team's text, appended to
   whichever document answered, whether that document is a convention rank or
   pop's own prompt. A second noun for one file shape and one semantic would be
   the stutter ADR-0246 decision 1 refuses, in the place it would cost most.

## Considered options

**Give `verification` the same treatment** — rejected, and the asymmetry is the
point. After ADR-0246 the `refine` document contains nothing a repository could
want to say, because its standards half moved out. `verification` is not in that
position: a team naming its scoped gate, excluding an e2e suite from it, or
recording a known flake is stating a genuine repository fact, and sending those to
`AGENTS.md` restores exactly the every-agent-rediscovers-it problem ADR-0227
decision 4 created the kind to end. `verification` stays a role-driving convention
kind.

**An overlay seam on every pop-authored step prompt** — implement, verify, refine,
assist — rejected as speculative. The seam exists where a procedure stopped being
overridable, which is one place. `verification` steers at a rank already; the
implementer's prompt has never been overridable and nobody has asked. Building
four seams to answer one need is the move `implementation.md`'s own "nothing
speculative" rule names, and this is the ADR where pop should be seen taking its
own advice. The mechanism is built generally enough that a second seam is a line.

## Consequences

- **`ShapeRoleDriving` is left with one member, `verification`, and its declared
  meaning is true again as written.** Amending ADR-0227 decision 1 to admit a body
  that is procedure rather than mandate is no longer needed — that amendment was
  only ever forced by `refine` staying a kind.
- The **Convention kind** set stays four: `implementation`, `commits`,
  `issue-tracker`, `verification`.
- **ADR-0252 decision 3's guarantee changes form.** "Fixes are human-directed
  because the convention is the human's prose" no longer holds as written, since
  the licence is now pop's. It survives in substance: *what counts as a problem*
  comes entirely from `implementation.md`, which is the human's and the team's, and
  only *how the pass acts on one* is pop's. The overlay is what keeps that honest —
  a repository can still say what this pass may not touch here.
- `ParseKind("refine")` refusing plainly, rather than pointing at `implementation`,
  is deliberate: a mistyped kind is a loud interactive error, unlike the config key
  in ADR-0246, which fails silently and so earns a pointer.
