# A convention read from the command announces its own length

A **Convention kind** reaches an agent by three paths, and only two of them are pop's to hand over. Pop injects a **role-driving** kind as the body of the Verifier's or Reviewer's prompt ([ADR-0227](0227-a-role-driving-convention-is-the-prompt-body-pop-owns-only-the-frame.md)), and it projects the `commits` answer into a **Task set**'s manifest for the Verifier to read ([ADR-0228](0228-pop-writes-the-manifest-s-commit-convention-itself.md)). On the third path the agent runs `pop conventions get` itself and reads the output: it is how `to-tasks`, `to-spec` and `wayfinder` reach the **Issue-tracker doc**, how `grill-with-docs` and `grill-consolidate` reach the **Commit convention**, and how any agent may reach any kind.

The third path is the only one a reader can shorten, and agents shorten long command output as a matter of habit — `head -30`, `sed -n 1,50p`, a grep for the section they expect. Pop's shipped `issue-tracker` answer renders to 343 lines, and a 30-line prefix of it ends mid-"Publishing a spec": the register call, the MALFORMED fix loop, the whole-set drain suggestion and every wayfinding rule are below the cut. The agent then publishes against a document it believes it read.

[ADR-0226](0226-a-convention-always-answers-and-a-recipe-is-never-a-rank.md) closed the one hazard that *taught* peeking — the METHOD banner an agent had to check the first lines for — and [ADR-0228](0228-pop-writes-the-manifest-s-commit-convention-itself.md) recorded the conclusion that an agent reading a convention it has not seen therefore reads it whole. That conclusion holds where 0228 drew it, on a path where pop hands over the prose entire. It does not hold on a path where the agent chooses how much of the output to look at, and this ADR qualifies it there.

The decision: **`pop conventions get` and `pop conventions default` print a Read-whole notice above each kind's block** — one line stating the number of lines below it and the obligation to read all of them — and **every shipped skill that names the command carries a sentence forbidding the pipe**, pinned by a walker over the embedded skill bodies rather than a list of today's call sites.

Three properties of the notice are the decision, not the wording:

- **A header, not a footer.** A header is the only part of the output that a prefix read cannot drop. A footer is lost by exactly the act it exists to report.
- **Unconditional, not length-gated.** A notice that appears only above some threshold is absent from `commits`, the kind agents read most often, and its absence is what teaches a reader the line is advisory.
- **The command path only.** Not the **Config dashboard** preview, which is scrolled rather than prefix-read, and not the prose pop injects into a prompt — there the notice would be pop instructing an agent not to truncate output that agent never ran.

The count is computed from the rendered block and asserted against it in tests rather than pinned in a golden. A notice that misstates its own length is worse than none, and only a computed check catches that; a golden would instead fail on every editorial pass over a shipped answer.

Two alternatives are declined.

**Printing a path instead of the document.** Having the command write the resolved convention to a file and print its path relocates the habit rather than removing it — an agent handed a path runs `cat … | head -30` — and it splits the command's contract, since a human typing `pop conventions get` wants the prose. Behaviour that differs between a terminal and a pipe is exactly what [ADR-0223](0223-a-convention-resolves-to-one-answer-plus-the-overlay.md)'s one-rendering rule exists to prevent.

**A per-section selector.** `pop conventions get issue-tracker --section "Publishing tickets"` would hand each caller the forty lines it already asks for by name, and a document short enough to read whole is one nobody truncates. This is the stronger idea and it is not rejected on merit: it changes the command surface and imposes structure on a human-written repository document, so it belongs to its own decision. The notice defends the document that exists today, whatever happens to that one.

## Consequences

- **Convention consumption shape** is a statement about prompt building, not about delivery. The glossary entry said a step-informing kind "stays a block inside pop's prompt"; that was never true of `issue-tracker`, which no pop prompt injects. The two axes are now named apart.
- Pop can guard a call site it does not author. A repository's own `AGENTS.md`, or a human's typed command, gets the notice without anything having warned the reader first — which is why the skill sentence is a convenience and the notice is the guard.
- Every surface still renders one answer. The notice sits above the block `StackProse` and `StackPreview` produce and does not enter it, so the pane, the prompt and the command continue to agree about what is in force.
