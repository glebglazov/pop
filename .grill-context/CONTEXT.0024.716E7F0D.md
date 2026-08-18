---
fragment: 716E7F0D
generation: 0024
branch: master
---

~ Artifact view
  The second list a **Task set detail view** shows in place of its task list,
  toggled with `v` — the readable subset of the set's **Task artifact**s, one
  row per document. Rows carry an artifact type (`review`, `spec`,
  `progress`), the bare filename, and the instant the document was written: a
  review takes it from the instant in its own name, everything else from its
  modification time. The order is a **type tier**, not one total recency order:
  every **Review artifact** first, newest first among themselves, then
  `spec.md`, then `progress.txt` last. Recency orders only the family that
  grows; the two singletons carry a modification time that says when pop last
  touched them rather than when they became worth reading, and `progress.txt`
  moves on every transition, so a total recency order made the list jump under
  the cursor after every drain step. It is a closed known list, not a directory
  dump — the manifest is the detail view itself rendered, task markdown is the
  other list, and captured runs are gzipped JSONL that `pop tasks stream`
  already serves. It is offered only when the container has at least one
  artifact, which is what makes the seam a silent no-op for a **Work kind** that
  publishes none.
  avoid: artifact list, document list, artifact tab
  was: The second list a **Task set detail view** shows in place of its task list, toggled with `v` — the readable subset of the set's **Task artifact**s, one row per document, newest first. Rows carry an artifact type (`review`, `spec`, `progress`), the bare filename, and the instant the document was written: a review takes it from the instant in its own name, everything else from its modification time, so one total order covers a family whose members do not all timestamp themselves. Every prior **Review artifact** is a row of its own, because retaining review history is only worth anything if it is reachable. It is a closed known list, not a directory dump — the manifest is the detail view itself rendered, task markdown is the other list, and captured runs are gzipped JSONL that `pop tasks stream` already serves. It is offered only when the container has at least one artifact, which is what makes the seam a silent no-op for a **Work kind** that publishes none.

~ Document peek
  A read-only nested view over any absolute file path a detail row carries — a
  task's markdown, a **Routine**'s last report, an **Artifact view** row —
  opened with `l` or Enter, scrolled Vim-style (`j`/`k`, `ctrl-d`/`ctrl-u`,
  `gg`/`G`), and dismissed with `h`/left/`esc` without changing anything. A
  `.md` path is rendered as formatted markdown; every other path is shown raw,
  because the peek's own non-markdown documents say so by extension and one of
  them, `progress.txt`, separates its records with `---` lines that a markdown
  renderer would turn into horizontal rules. The view reads whatever path the
  row hands it, which is why every row that names a file carries an absolute
  one.
  avoid: task editor, task modal, preview pane
  was: A read-only nested view over any absolute file path a detail row carries — a task's markdown, a **Routine**'s last report, an **Artifact view** row — opened with `l` or Enter, scrolled Vim-style (`j`/`k`, `ctrl-d`/`ctrl-u`, `gg`/`G`), and dismissed with `h`/left/`esc` without changing anything. The view reads whatever path the row hands it, which is why every row that names a file carries an absolute one.

+ Read-only agent posture
  The argument a headless **Agent preset** adds to take away its own ability to
  write files, declared as one more capability beside its auto-approval prefix:
  claude's `--disallowedTools`, codex's `--sandbox read-only`. It is what makes
  the one role whose whole job is reading — the **Reviewer** — enforced rather
  than merely instructed, since it otherwise runs under the same
  `--dangerously-skip-permissions` prefix as the **Implementer** and in the very
  checkout the human is about to fold. It does not extend to the **Verifier**,
  which must run the build and the test suite: a posture that blocks every write
  would fail verification for reasons that have nothing to do with the code.
  Strength differs by preset — codex's sandbox blocks writes from a shell
  command too, claude's tool denial does not — and a preset with no such
  argument declares the capability blind, so pop says what it enforces rather
  than implying a guarantee it cannot give.
  avoid: sandbox, read-only mode, safe mode
  under: Agents

+ Review pointer
  A **Review artifact** as every surface other than a reader carries it: the
  document's path plus the commit its own header records it was written
  against, never a line of what it says. One resolution (`latestReviewPointer`)
  feeds the **HITL** sign-off preamble, the **Task set detail view**, and the
  **Prompt preset** that puts it in every attended agent prompt — so the review
  travels as a path an agent opens, not as prose inlined into a context window.
  When the pointer's commit is behind the checkout's current one the surfaces
  say so: a review of a tree that has since moved is still worth reading and
  must not be read as current.
  avoid: review link, review reference, latest review

+ Prompt preset
  A named fragment shared by more than one of pop's agent prompts, defined once
  in `partials.tmpl.md` and included by each template that needs it — the task
  listing, a task body, the two clauses that hold an attended agent to drafting
  rather than deciding, the **Review pointer** block. A rule that binds several
  roles is written in one preset rather than restated per template, so the roles
  cannot drift apart in what they were told.
  avoid: partial, snippet, shared prompt, template fragment

~ Reviewer
  The agent that performs **Code review**, running in a fresh context, under a
  **Read-only agent posture**, and chosen independently of the implementing
  agents so it does not review its own work — the same independence rule the
  **Verifier** holds, for the same reason. Its prompt is pop's own review
  instruction, the previous **Review artifact** if one exists, and the resolved
  `code-review` **Convention kind**; so what counts as good code here is prose
  in the **Convention stack**, never pop configuration. Unlike the Verifier it
  is expected to read the changed files itself: it is given the commit range and
  the **Work diff view** for orientation only, because naming, structure and
  idiom cannot be judged from a `--stat` table.
  was: The agent that performs **Code review**, running in a fresh context and chosen independently of the implementing agents so it does not review its own work — the same independence rule the **Verifier** holds, for the same reason. Its prompt is pop's own review instruction, the previous **Review artifact** if one exists, and the resolved `code-review` **Convention kind**; so what counts as good code here is prose in the **Convention stack**, never pop configuration. Unlike the Verifier it is expected to read the changed files itself: it is given the commit range and the **Work diff view** for orientation only, because naming, structure and idiom cannot be judged from a `--stat` table.

~ Repo convention
  The prose answer to "how does this repository do X" for one **Convention
  kind** — commit grammar, issue-tracker behaviour, what good code looks like
  here — resolved for the repository the current checkout belongs to. It is one
  document plus the **Convention overlay**: the **Convention stack** picks a
  single winner and appends the overlay, never a set of layers for the caller to
  reconcile.
  avoid: project convention, house style, commit style
  was: The prose answer to "how does this repository do X" for one Convention kind — commit grammar, issue-tracker behaviour — resolved for the repository the current checkout belongs to. It is never a single document: what a caller gets is the Convention stack, and "the convention" means that stack reconciled.

~ Convention stack
  How a **Convention kind** resolves to one answer: the first of **Convention
  defaults**, the repository's `docs/agents/<kind>.md`, **Convention memory**
  and the kind's **Convention recipe** that holds something, with the
  **Convention overlay** appended whenever it exists. Winner-take-all, not
  composition — a lower rank that merely coexists with a better answer would go
  on reaching the agent forever, which is how a machine-local guess outlives the
  document that replaced it (ADR-0223, amending ADR-0211). The human's own
  document outranks the team's, because pop resolves conventions for agents
  working on one human's behalf on that human's machine. Because the recipe is
  the last rank, the stack always answers and `pop conventions get` always exits
  0.
  avoid: convention precedence, convention merge order, composed stack

+ Convention defaults
  `~/.agents/docs/<kind>.md` — the human's own complete answer for a
  **Convention kind**, applying in every repository and *replacing* whatever the
  repository, **Convention memory** or the **Convention recipe** would have
  said. Its counterpart at the other end of the **Convention stack**, the
  **Convention overlay**, *appends* instead: same author, same directory, and
  the choice between them is whether you are stating the whole answer or adding
  constraints to someone else's.
  avoid: user defaults, global convention, personal convention
  under: Conventions

~ Convention overlay
  `~/.agents/docs/<kind>.overlay.md` — the human's constraints that ride along
  with whichever answer the **Convention stack** picked ("never add a
  Co-Authored-By trailer"), appended whenever it exists and displaced by
  nothing. It is what an *override* is for a convention (ADR-0212 decision 2),
  and it differs from **Convention defaults** by mechanism rather than rank: the
  overlay adds, the defaults replace. Named overlay, not override, because pop
  already spends "override" on the **Repo override** and its runtime layer.
  User-global only: a repository's own answer is its document.
  avoid: convention override, merge doc, common rules
  was: `~/.agents/docs/<kind>.overlay.md` — the human's top-ranked layer, above even the repository's committed document, mirroring how `config.override.toml` outranks a hand-authored `config.toml`. It holds constraints that must survive any repository ("never add a Co-Authored-By trailer"), where the same human's bottom layer holds preferences a team is entitled to overrule. Named overlay, not override, because pop already spends "override" on the **Repo override** and its runtime layer, and because pop's overlay rules already mean "layered on top, beats what is underneath". User-global only: a repository's own top slot is its document.

~ Convention memory
  The pop-written rank of the **Convention stack**: one Markdown file per kind
  under the repository's **Task storage** directory, keyed by **Repository
  identity** so every worktree of a repository reads one value, carrying
  frontmatter recording what it was derived from and when. It is pop's stand-in
  for a written answer, so it stands down the moment any document exists — no
  staleness detection, because a document makes a guess redundant by definition.
  Nothing writes it for `code-review`: a review standard is prose about a team's
  taste, and one filed where a single machine can read it is the wrong artifact.
  avoid: remembered convention, convention cache, repo settings
  was: The pop-written layer of the Convention stack: one Markdown file per kind under the repository's **Task storage** directory, keyed by **Repository identity** so every worktree of a repository reads one value. Written by `pop repo conventions set` from stdin, removed by `unset`, and carrying frontmatter recording what the convention was derived from and when — the provenance the disclosure line quotes. It is where a derived convention lands; a convention a human states in session should be offered to the repository's document instead, because the team should own that in version control.

~ Convention recipe
  Pop's built-in, per-kind instruction for producing a **Repo convention**, and
  the last rank of the **Convention stack** rather than what prints when the
  stack misses — so the stack always answers. It keeps its banner: the last rank
  is a *method*, and the banner is what tells a consumer to work the steps
  rather than follow them. `commits` and `issue-tracker` stay pure method; the
  `code-review` recipe also ships a named smell baseline as a floor beneath its
  three derivation sources, so a repository that has written nothing still holds
  a changeset against something honest, and it records its result to
  `docs/agents/<kind>.md` alone.
  avoid: convention default, fallback convention, derivation instructions
  was: Pop's built-in, per-kind instruction for producing a Repo convention when the Convention stack is empty — embedded in the conventions package, printed by `pop repo conventions recipe <kind>` and by `get` when it exits 1. Distinct in kind from a convention: the stack returns an answer, the recipe returns the method for getting one, and conflating them would force every caller to branch on which it received. The `commits` recipe carries the derivation that was skill prose until now, including the discard-pop-generated-commits guard, plus where to write the result.
