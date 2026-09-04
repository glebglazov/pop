# A builder proves prior art before it writes mechanism

An audited **Task set** shipped React that hand-rolled a busy flag, a
`try`/`catch` that turned an error into a flash message, a success notice and a
confirmation dialog — four things the house component library already composed
into one call. A teammate reverted the lot by hand a few days later.

The interesting part is what was *not* missing. The repository's `AGENTS.md`
named the library and told the builder to prefer it. Both task files'
**Orientation** sections named it again, one of them listing three of its
components. The composed form was already used in nine files, including the very
file the task told the builder to edit, ten lines above the code it wrote. All
sixty-seven implement attempts across that project's sets ran on Claude Opus or
Sonnet at high or medium effort; no small model touched it.

So every rank of prose named the primitive and none named its composed form,
because nobody writes a sub-component list into an architecture document. The
gap sits one level below what prose can carry, and the only thing at that level
is the surrounding code — which the builder had open and did not transfer from.
This ADR declines to answer it with more documentation.

## The rule

1. **A new Convention kind for architecture is rejected.** The obvious fix is a
   document describing how the front end and back end are built, resolved through
   the **Convention stack** like the others. Four ranks of exactly that already
   existed and the failure was below all of them. A fifth would have named
   `IconButton` too.

2. **The trigger is hand-written mechanism, not location.** Before writing a
   state flag, error-to-message handling, a success notice, a confirmation or a
   retry around a call, the builder looks for the composed form the repository
   already has. Plumbing written by hand is the signal; "read the surrounding
   code" is not, because in the audited case the builder was already in the right
   file and read it without transferring anything.

3. **The search is a ladder, cheapest first, stopping at the first hit**: the
   files the task's **Orientation** names, the directory the change lands in,
   then the repository. Orientation is already written for this purpose — its
   own rationale is that an unattended drain spends a large share of its tool
   calls rediscovering a map the author had.

4. **pop computes nothing.** pop is a Go orchestrator with no knowledge of the
   target language, and Orientation is deliberately free prose an author may
   omit — parsing it into a file list is brittle by construction and degrades to
   silence on every task that omits the section. The rule is an instruction the
   agent executes against the checkout it is standing in. pop resolves and
   delivers; agents read.

5. **It lives in pop's frame, unconditional.** The rule is in
   `agent.tmpl.md` after the edit-boundary paragraph and before the conditional
   convention block — so it fires before any code is written, and so that
   **Implementation convention inlining**, which defaults off, cannot gate it.
   A convention rank is displaceable whole by any rank above it, and this is the
   one rule that must survive a team stating its own standard: a team displacing
   pop's convention is stating its taste, not asking to be exempt from reading
   its own codebase. This is the line the Refiner's fix licence already draws —
   what is machinery lives in the frame, what is taste lives in the convention.

6. **The builder names what it matched.** One line in the summary block: the
   existing implementation the new code was matched against, or a plain
   statement that no prior art existed. An unfalsifiable rule is what we already
   had — the shipped implementation convention's "unfamiliar is not a finding"
   was in force and was not followed. The line is cheap, it lands in the
   **Progress record** a human already reads, and writing it forces the search
   to actually happen. A structured field pop parses is refused: no machinery
   would consume it.

7. **The Refiner gains the inverse of its own brake.** Its frame already says
   "unfamiliar is not a finding", which protects *existing* idiom from a
   Refiner's unfamiliarity. Nothing made a failure to *adopt* an available idiom
   nameable, which is why a Refiner reading the audited code could have found
   nothing wrong. The inverse joins the same brakes sentence, and because the
   licence is bounded by what the standard names, this is permission to fix as
   well as to report — swapping hand-rolled plumbing for the composed form is
   mechanical and undoable by inspection.

## Consequences

- **`[work.implement].include_implementation_convention` stays false.** Decision
  5 makes the trigger unconditional, so the toggle now carries much less weight
  than it appears to. Flipping it would add a page of generic prose to every
  implement prompt in every repository to deliver rules the trigger already
  covers for this failure.
- **The Rule of Three stands as counted, and the audited residue stays
  unreported.** The same audit found two 105-line near-clone components
  differing in eight lines. Two rules independently license silence: under three,
  copies stand, and the earlier copy predates the changeset that added the second,
  making it revealed rather than created. Both are correct here — the two files
  diverged in a way that matters, and parameterising them yields the shallow
  wrapper the convention warns against. A size qualifier on the Rule of Three is
  refused: "counted rather than felt" is the whole of its value.
- **The Verifier is untouched.** It judges acceptance criteria, and no criterion
  in the audited set was unmet. This failure was never verification's to catch.
