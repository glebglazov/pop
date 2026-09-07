# An interrelated task set explores once before it implements

Every implement attempt in a **Task set** starts from nothing. `agent.tmpl.md`
hands a builder the task path, the checkout and the rules; nothing carries what
the last attempt learned about the code. `spec.md` records what was decided and
reaches only the **Verifier**; `progress.txt` is append-only with a terminal
grain and reaches only an **Assist session**'s human. So a five-task set
re-derives the same map five times, and — worse than the tokens — task four can
build against a picture of a seam that contradicts the one task two built
against.

The obvious host for a fix is one of the two documents already in the set
folder. Neither fits. `progress.txt` is append-only and terminal by definition;
a document whose whole job is to be rewritten is the opposite of it on both
counts. `spec.md` is intent, fixed at planning; exploration is the code as
found, which decays with every commit, and decaying facts inside a document
readers treat as settled intent make the intent unreliable too.

## The rule

1. **A third artifact, flat, beside the other two.** An **Exploration report**
   at `exploration.md` in the set directory, joining the flat list in
   `Artifacts()` ordered spec → exploration → progress: what was decided, what
   exists, what happened. No `explore/` directory of timestamped documents as
   refine and verify have — decision 3 makes the pass run once, and supersede
   machinery with nothing to supersede is cost without a payer.

2. **Single writer, with one correction duty.** The **Explore phase** owns the
   document. A builder edits it in exactly one case: it *falsified* a recorded
   line — moved the file, retired the seam the report names — and then it fixes
   that line and nothing else. The tempting version is a live journal every
   builder appends to, and it is refused twice over: the set's own progress is
   already carried by the **Progress record**, and a document any agent may add
   to becomes the scratch pad that makes a glossary unusable when it happens
   there. A false map is the whole risk this artifact carries, so correcting a
   falsified line serves the document where appending does not.

3. **It runs once, when the report is absent, and pop computes no freshness.**
   The report states in its own text the SHA it describes, as the **Refine
   report** does, and nothing compares that SHA to anything. Computed staleness
   was designed and dropped: an implement attempt commits, so a SHA-exact rule
   would mark the report stale after task one and regenerate it per task —
   spending an agent run per task to avoid spending exploration per task. The
   honest rules that remain are cheap: absent means generate, present means
   reuse, and a human reading the report can see how old its SHA is.

4. **The set's author decides it is needed, not a threshold.** The **Explore
   directive** — a set-level `"explore": true` — is the participation trigger,
   and the **Authoring guide** states when to set it: two or more AFK tasks
   touch the same seam and a later one depends on how an earlier one shapes it.
   Never a set's size, never the area being unfamiliar; those are the
   unfalsifiable forms ADR-0259 already caught. A count of three AFK tasks was
   considered and rejected — a count is pop guessing how interrelated a set's
   code is, and pop computes nothing about the target language. The author
   breaking the set down has just read that code. The directive sits *outside*
   the **Agent directive** object family, whose invariant is that a directive
   steers how a phase runs and never opts a set in; an optional `"explorer"`
   object beside it steers agents and effort as `"verifier"` and `"refiner"` do.

5. **It gates, and a pass that cannot produce the report parks the set.** This
   is the one agent phase that can stop a **Drain**, and it deliberately breaks
   the posture ADR-0260 set for the **Refine mark**, which gates nothing so an
   agent hiccup cannot park work. The asymmetry is the placement: refine judges
   work already done, so proceeding costs nothing, while exploration exists so
   that builders do not start from separate maps — proceeding unexplored on a
   set whose author said the tasks are interrelated delivers exactly the
   inconsistency the phase was added to prevent. So: implementation waits;
   failure is bounded by the standard per-phase retry cap so a single flake
   cannot park anything; and the park is a new **EXPLORE-FAILED**, sibling of
   VERIFY-FAILED and a sixth member of the drain-stop list. BLOCKED is not
   reused — it means a human owes a decision, and one word cannot carry both.

6. **The park is derived from a captured run, with no new column.** A parked set
   still has an eligible AFK task, so it would still derive as READY and the
   daemon would re-select it and re-explore forever. EXPLORE-FAILED therefore
   derives as the **Refine mark** does — a **Captured run** of phase `explore`
   exists and its outcome says which — rather than through a cache table like
   the **Verify verdict**'s, there being no verdict here to cache. Three doors
   leave the park: `pop tasks explore <set>` runs the pass by hand as `pop tasks
   refine` does, `--skip-explore` on `implement` drains this set once
   unexplored, and `"explore": false` retracts the author's judgment. The park
   also appends one set-level **Progress record** naming the phase and the
   reason, so the *why* is readable without opening the captured run.

7. **The report does not carry the prior-art check.** The intuitive move is to
   have the explore pass record the composed forms the repository already has,
   pre-paying the expensive rung of ADR-0259's ladder. Refused. That ADR's
   trigger is mechanism a builder is about to write, which is unknowable before
   the attempt, and its decision 1 already refused a further rank of prose on
   the evidence that four ranks existed and the failure sat below all of them —
   "nobody writes a sub-component list into an architecture document". Asking an
   agent to write that list reliably bets on the thing humans never do, and a
   report that looks helpful while being empty at the one moment it mattered is
   worse than no section at all. The check stays in pop's frame, unconditional.

## Consequences

- **This is the first agent phase that runs unasked.** Verify and refine both
  default off; `[work.explore]` defaults on. The cost is bounded by decision 4:
  a set carrying no directive never explores, so every set that predates this
  ADR and every hand-written set costs nothing.
- **A set drained twice, weeks apart, reuses its first report.** Decision 3's
  accepted residue. The first drain after a long gap is the case that motivated
  moving generation out of **to-tasks**, and it is served — a set that never
  drained has no report. A resumed set relies on the correction duty alone.
- **Documenting a repository's own house idioms is left for later, and needs no
  new mechanism.** It is the `implementation` **Convention kind** already —
  "the standard is what good code looks like here" (ADR-0246) — written at
  `docs/agents/implementation.md`. Delivering it to builders as well as to the
  **Refiner** means flipping `include_implementation_convention` for that one
  repository, which leaves ADR-0259's default intact.
- **"An agent phase gates nothing" is no longer a house rule.** It was one
  observation in ADR-0260 and reads like a principle. Decision 5 makes the
  question a phase must answer explicit: does proceeding without me cost more
  than parking? For refine the answer is no, and for explore it is yes.
