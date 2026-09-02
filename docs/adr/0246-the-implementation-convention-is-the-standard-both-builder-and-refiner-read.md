# The implementation convention is the standard both builder and Refiner read

[ADR-0252](0252-refine-fixes-in-place-before-the-verify-phase.md) made the shipped
`refine` convention serve two consumers: the **Refiner**'s whole mandate, and a
labelled block inlined into every implement prompt under
`[work.implement].include_refine_convention`. One text answering to two readers
is what made that text impossible to write well — every line had to earn its
place in a prompt about *judging* a changeset and in a prompt about *writing*
one, and the half that says what a pass may fix rode uselessly into every
implement prompt. This ADR retires that consequence. ADR-0252's central ruling
is untouched: refine writes, and it runs before verify.

A new **Convention kind** `implementation` holds what good code looks like in
this repository, **code and tests as one subject**. There is no separate `tests`
kind: how a repository tests is how it writes code, and splitting them would make
an author choose a file before they have a thought. It is *step-informing*, so it
reaches a prompt as a labelled block, and it is read by two agents — the
implementer, when
`[work.implement].include_implementation_convention` is set, and the **Refiner**,
always.

The decision, in four parts:

1. **The kind is `implementation`, and so is the config key.** The key is
   `[work.implement].include_implementation_convention`, stutter and all. The
   alternative — a key named for *standards* while the kind is named for
   *implementation* — buys a better-sounding key at the price of two words for one
   thing, in a repository whose whole discipline is that names are addresses. One
   word reaches the kind, the config key, the glossary term, `docs/agents/`, the
   overlay filename and `pop conventions get`.

2. **A convention states standards; a prompt grants licence.** The shipped text
   loses the "What a pass may fix" half it carries today. That half is the refine
   step's own procedure, it is already said in stronger words in
   `refiner.tmpl.md`, and it is dead weight to an implementer who has no pass to
   run. What remains is a statement about code, in the present tense, that an
   author and a judge can both act on.

3. **The Refiner carries it unconditionally; the implementer opts in.** The
   toggle's default stays `false`, unchanged from ADR-0252, so the steady state is
   that the implementer never sees the standard and the Refiner holds the work to
   it anyway. That asymmetry is deliberate rather than an accident of which config
   table the key sits in: refine exists to catch what the implementer did not do,
   and a standard the author saw in advance is a bonus, not a precondition for
   judging it afterwards.

4. **The Verifier is deliberately not a consumer.** It judges whether the
   acceptance criteria were met, and it holds a gate. Handing a quality standard
   to the one role that can produce VERIFY-FAILED is how a matter of taste becomes
   a blocked set — the failure mode
   [ADR-0214](0214-code-review-is-a-drain-step-that-maintains-a-living-document.md)
   and [ADR-0227](0227-a-role-driving-convention-is-the-prompt-body-pop-owns-only-the-frame.md)
   have both been steering around. Refine already holds the changeset to the
   standard, one step earlier, with a fix licence and no verdict.

## Consequences

- **Migration.** `KindRefine` → `KindImplementation`, with `Shape()`, `Desc()`,
  `Kinds()` and the shipped file following it;
  `conventions/shipped/implementation.md` replaces `conventions/shipped/refine.md`;
  the resolved paths become `docs/agents/implementation.md` and
  `~/.agents/docs/implementation.md`. `tasks/implement_refine_convention.go`
  becomes the implementation-convention seam under the same one-resolution-per-run
  shape.
- **The renamed config key errors rather than being ignored.**
  `include_refine_convention` is a `bool` defaulting to `false`, so a file that
  still sets it to `true` would silently lose the behaviour. It gets a load-time
  error naming the new key, as `execution_base` and `queue_base` do for `trunk`,
  rather than the silent-alias treatment `[select]` and `[dashboard]` got. This is
  the general habit: **a silently-ignored key gets a rename pointer; a mistyped
  CLI name gets the plain refusal**, because the first loses behaviour without a
  signal and the second is already loud and interactive.
- ADR-0252's **"one text, two consumers"** consequence is retired, and with it the
  constraint that the shipped text stay short enough to ride every implement
  prompt. ADR-0252's other half survives: a repository that writes a long document
  pays that length in its own prompts, its choice.
- **The Assist hint follows the kind.** ADR-0252's one line in the Assist prompt
  becomes `pop conventions get implementation`.
- pop writes no repository-rank `docs/agents/implementation.md` of its own. The
  shipped answer is the artifact under test and pop is the only repository
  exercising it; a repository-rank document would displace it whole and it would
  never run anywhere.
