---
fragment: 045EDF62
generation: 0027
branch: master
---

~ Convention stack
  How a **Convention kind** resolves to one answer: the first of **Project
  convention**, **Global convention**, the repository's `docs/agents/<kind>.md`
  and the **Shipped convention** that holds something, with the **Convention
  overlay** appended whenever it exists. Winner-take-all, not composition — a
  lower rank that merely coexists with a better answer would go on reaching the
  agent forever (ADR-0223, amending ADR-0211). The human's own documents outrank
  the team's, and the more specific of the two outranks the general, because pop
  resolves conventions for agents working on one human's behalf on that human's
  machine. Because the last rank is pop's own answer rather than a method, the
  stack always resolves to *rules to follow* and `pop conventions get` always
  exits 0 (ADR-0226).
  avoid: convention precedence, convention merge order, composed stack
  was: How a **Convention kind** resolves to one answer: the first of **Convention defaults**, the repository's `docs/agents/<kind>.md`, **Convention memory** and the kind's **Convention recipe** that holds something, with the **Convention overlay** appended whenever it exists. Winner-take-all, not composition — a lower rank that merely coexists with a better answer would go on reaching the agent forever, which is how a machine-local guess outlives the document that replaced it (ADR-0223, amending ADR-0211). The human's own document outranks the team's, because pop resolves conventions for agents working on one human's behalf on that human's machine. Because the recipe is the last rank, the stack always answers and `pop conventions get` always exits 0.

+ Shipped convention
  Pop's own answer for a **Convention kind** — embedded in the binary, the last
  rank of the **Convention stack**, and displaced whole by any written rank above
  it (ADR-0226). It is generic by construction, pop being a work orchestrator
  that cannot know a project's taste: the `commits` entry says to read the recent
  log and match it, carrying the discard-pop-generated-commits guard that stops
  pop learning its own accent back from a log it wrote. Generic derivation
  guidance belongs *inside* it rather than in a rank of its own, being the honest
  answer where nobody has stated a better one. Not a house style, and not a
  floor beneath other ranks — a team's document replaces it rather than arguing
  with it. `pop conventions default <kind>` prints it on demand, because a human
  writing their own answer wants pop's to start from.
  avoid: pop defaults, fallback convention, house style, baseline
  under: Conventions

+ Project convention
  `~/.agents/docs/projects/<slug>/<kind>.md` — the human's answer for one
  project, and rank 0 of the **Convention stack**, above their own **Global
  convention** because the more specific of two documents by one author wins
  (ADR-0226). It is where a human reaches to override everything for one
  project, which is what **Convention memory** was being used for. Alone among
  pop's per-repository state it is keyed by the git remote rather than
  **Repository identity**: a store, a binding and a config override are about
  this machine's checkout, for which a moved repository genuinely is a new
  subject, while a convention document is about the project as a thing that
  outlives any one clone — and a remote is derivable with no stored state, which
  a human-chosen name would not be. A repository with no remote falls back to the
  identity-keyed path.
  avoid: convention memory, repo-local convention, project override
  under: Conventions

+ Global convention
  `~/.agents/docs/<kind>.md` — the human's own complete answer for a
  **Convention kind**, applying in every repository and *replacing* whatever the
  repository or the **Shipped convention** would have said. Renamed from
  Convention defaults because "defaults" came to name two ranks at opposite ends
  of the stack, and a reader took `--defaults` for "override pop's built-in"
  (ADR-0226). Its counterpart the **Convention overlay** *appends* instead: same
  author, same directory, and the choice between them is whether you are stating
  the whole answer or adding constraints to someone else's.
  avoid: convention defaults, user defaults, personal convention
  under: Conventions

- Convention defaults

- Convention recipe

- Convention memory

~ Convention overlay
  `~/.agents/docs/<kind>.overlay.md` — the human's constraints that ride along
  with whichever answer the **Convention stack** picked ("never add a
  Co-Authored-By trailer"), appended whenever it exists and displaced by
  nothing. It is what an *override* is for a convention (ADR-0212 decision 2),
  and it differs from the **Global convention** by mechanism rather than rank:
  the overlay adds, the global document replaces. Named overlay, not override,
  because pop already spends "override" on the **Repo override** and its runtime
  layer — and the name is *not* reused for pop's own non-negotiable **Role
  preamble** and **Response contract**, which are entirely pop's where this is
  entirely the human's. User-global only: a per-project constraint is stated as
  a whole **Project convention** instead, a second appending rank being the
  composition ADR-0223 removed. It renders last, and after ADR-0226 nothing
  invites a reader to stop before reaching it. Written by
  `pop conventions set <kind> --overlay`, no longer by the **Config dashboard**.
  avoid: convention override, merge doc, common rules
  was: `~/.agents/docs/<kind>.overlay.md` — the human's constraints that ride along with whichever answer the **Convention stack** picked ("never add a Co-Authored-By trailer"), appended whenever it exists and displaced by nothing. It is what an *override* is for a convention (ADR-0212 decision 2), and it differs from **Convention defaults** by mechanism rather than rank: the overlay adds, the defaults replace. Named overlay, not override, because pop already spends "override" on the **Repo override** and its runtime layer. User-global only: a repository's own answer is its document.

~ Convention kind
  One member of the closed set of things pop can hold a **Repo convention**
  for — `commits`, `issue-tracker`, `code-review` and `verification`. Closed
  because each kind ships a **Shipped convention** pop must have written, so a
  kind pop has never heard of has no answer to offer; an unknown kind is refused
  with the list of the ones that exist, as `pop config repo set` refuses an
  unknown key. A kind also declares its **Convention consumption shape**, which
  is what tells the author of the next kind what they owe it. Names are addresses
  and stay stable: `docs/agents/issue-tracker.md` is read by third-party skills
  under that exact name, and `code-review` and `verification` match pop's own
  step nouns.
  avoid: convention type, convention name
  was: One member of the closed set of things pop can hold a Repo convention for — `commits` and `issue-tracker` at first, with `code-review` and `verification` as the shape it was built for. Closed because each kind's Convention recipe is kind-specific work pop must know how to do; an unknown kind is refused with the list of the ones that exist, as `pop config repo set` refuses an unknown key.

+ Convention consumption shape
  Whether a **Convention kind** reaches an agent as a prompt *body* or as a
  labelled *block*, declared by the kind and honoured at every call site
  (ADR-0227). A **role-driving** kind — `verification`, `code-review` — is an
  agent's entire mandate, so the convention is the body and pop supplies only a
  **Role preamble** and a **Response contract** around it; there is then exactly
  one voice on what to check, where a convention that merely supplemented pop's
  own prompt would leave the team's answer arguing with pop's and no rule for
  which wins. A **step-informing** kind — `commits`, `issue-tracker` — is a fact
  a prompt about something else needs, so it stays a block inside pop's prompt,
  having no output contract to protect.
  avoid: prompt shape, envelope, injection mode
  under: Conventions

+ Role preamble
  The non-overridable opening pop puts before a role-driving convention: who the
  agent is, what it may touch, its posture. Pop's where the body is the team's
  or the human's (ADR-0227), and deliberately *not* called an overlay — that
  word is spent on the layer that is entirely the human's.
  avoid: prompt prefix, system prompt, envelope

+ Response contract
  The non-overridable closing pop puts after a role-driving convention: the
  reply format pop parses, and what a malformed reply resolves to. It is
  non-negotiable because pop needs it to work — a rewritten Verifier reply
  resolves to NEEDS-HUMAN, so a well-meaning replacement of the whole prompt
  would park every **Task set** at VERIFY-FAILED (ADR-0227). Named separately
  from the **Role preamble** because the two share nothing but authorship.
  avoid: prompt postfix, output format, envelope

~ Reviewer
  The agent that performs **Code review**, running in a fresh context, under a
  **Read-only agent posture**, and chosen independently of the implementing
  agents so it does not review its own work — the same independence rule the
  **Verifier** holds, for the same reason. Its prompt is the resolved
  `code-review` convention as its whole body, wrapped in pop's **Role preamble**
  and **Response contract**, plus the previous **Review artifact** where one
  exists; so what counts as good code here is prose in the **Convention stack**,
  never pop configuration. Its **Shipped convention** is Matt Pocock's two-axis
  Standards-and-Spec review, which knowingly overlaps the **Verifier**'s spec
  judgment — advisory only, since Code review reaches no verdict, so the overlap
  yields a second opinion rather than a conflicting gate (ADR-0227). Unlike the
  Verifier it is expected to read the changed files itself: it is given the
  commit range and the **Work diff view** for orientation only, because naming,
  structure and idiom cannot be judged from a `--stat` table.
  avoid: code reviewer, critic, linter
  was: The agent that performs **Code review**, running in a fresh context, under a **Read-only agent posture**, and chosen independently of the implementing agents so it does not review its own work — the same independence rule the **Verifier** holds, for the same reason. Its prompt is pop's own review instruction, the previous **Review artifact** if one exists, and the resolved `code-review` **Convention kind**; so what counts as good code here is prose in the **Convention stack**, never pop configuration. Unlike the Verifier it is expected to read the changed files itself: it is given the commit range and the **Work diff view** for orientation only, because naming, structure and idiom cannot be judged from a `--stat` table.

+ Verification convention
  The `verification` **Convention kind**: how work is checked in this
  repository — the build and test invocation, which of them is a whole-tree gate
  and which is scoped, what evidence counts as having run one. A fact about a
  repository's toolchain that no pop surface held, which every pop-spawned agent
  rediscovered and reached only where an agent CLI happened to auto-load
  `AGENTS.md`. Role-driving: it is the **Verifier**'s prompt body, and today's
  `verifier.tmpl.md` middle becomes its **Shipped convention**, while the
  acceptance-criteria-are-authoritative sentence and the done-AFK scope rule stay
  in the frame as pop's machinery rather than a standard (ADR-0227). Its name is
  the tenth glossary term under "verification" and coherently so: it is
  literally the standard **Agent verification** follows.
  avoid: checks convention, verify convention, test convention
  under: Conventions

~ Planned commit subject
  The final commit subject line rendered onto a task under the set's **Commit
  convention** — by planning for planned tasks, by the **Verifier** at spawn time
  for **Remediation task**s. The executor uses it verbatim as the
  implementation-commit subject, so commit time stays free of agent work; the
  body remains the agent summary. A task without one falls back to pop's
  task-derived format. Deliberately frozen at planning time: re-rendering it at
  commit time would have an agent generating subjects unattended, which is what
  produced an unattended amend of a trunk branch (ADR-0228).
  avoid: commit template, subject format string
  was: The final commit subject line rendered onto a task under the set's Commit convention — by planning for planned tasks, by the Verifier at spawn time for Remediation tasks. The executor uses it verbatim as the implementation-commit subject, so commit time stays free of agent work; the body remains the agent summary. A task without one falls back to pop's task-derived format.

~ Commit convention
  The `commits` **Convention kind**: the commit-message grammar a repository's
  team writes history in — types, scopes, subject style, and body style, which
  are one document and one sample rather than two kinds. Resolved through the
  **Convention stack**, whose repository rank is `docs/agents/commits.md` and
  whose **Shipped convention** says to read the recent log and match it.
  Step-informing, so it reaches the **Verifier** as a labelled block rather than
  a prompt body. Planning still resolves it once per **Task set** and records the
  resolved text on the set, so a draining set's grammar cannot change underfoot —
  but *pop* writes that key from the resolved stack at register time, not a
  planning agent retyping prose, a hand-copy having silently dropped four
  clauses including one of the human's own (ADR-0228).
  avoid: commit style, house style
  was: The `commits` Convention kind: the commit-message grammar a repository's team writes history in — types, scopes, subject style, and body style, which are one document and one sample rather than two kinds. Resolved through the Convention stack, whose repository layer is `docs/agents/commits.md`. Planning still resolves it once per Task set and records the resolved text on the set, so a draining set's grammar cannot change underfoot; what changed is that the resolution rule is the stack rather than a doc-then-log ladder in skill prose.

~ Config dashboard
  The one surface for everything in force in a directory: a searchable list
  of dotted paths on the left, a preview of the highlighted row on the
  right (ADR-0202, extended by ADR-0212 decision 8). Its config rows come
  in the two scopes an override lands at: the keys of the global surface,
  and — for the repository the dashboard was opened in — the leaves of its
  `[repo]` block, addressed `repo.<key>` and written into the **Override
  config layer**'s block for that repository. A key the walker unions
  across rungs is no row of the repository scope, there being no one layer
  whose value a preview could name. The repository scope is what makes this
  the place a **Preferred workbench** is chosen, that key having no global
  spelling. Last come that repository's conventions, addressed
  `conventions.<kind>`, because the right pane answers the same question
  for a convention as for a key — what is in force, and what produced it —
  differing only in the medium the answer is written in. A convention row
  previews what is in force and nothing else: the one rank that answered,
  labelled with its origin and reach, the **Convention overlay** appended
  beneath it where there is one, and the provenance line, rendered by the
  `conventions` package itself so this pane and `pop conventions get`
  cannot disagree (ADR-0223) — a kind nobody has written an answer for
  previews its **Shipped convention**, that rank being what is in force
  there. Convention rows are read-only: they name the **Project
  convention** and **Convention overlay** paths a write would land in and
  leave the writing to `pop conventions set`, a convention being a document
  rather than a value, and two writable human ranks having no way to share
  one enter key without hiding the higher-stakes one (ADR-0226). A
  convention may be a **Contested key**: it resolves to one answer, so a
  document the answer stood down is exactly a layer quietly losing. Outside
  a repository neither the repository keys nor the conventions are offered.
  A self-contained `ui/` component rather than a page of any one program,
  because it must open from three unrelated tea programs — the Work
  dashboard, the project picker and the worktree picker — plus `pop config
  dashboard`, which runs the same model standalone and refuses a
  non-terminal stdout. Rows carry the key's `desc` dimmed beneath where
  height allows, a marker where an override is in force, and a second
  marker beside it where the key is a **Contested key** — which also sorts
  it to the top of the list. A config key's preview is config format
  throughout: the effective value as a `key = value` TOML statement, the
  layer that produced it (**Override config layer**, `config.toml`, a
  built-in default, or a fallthrough naming the key walked on to), the
  value an override stands on, and the key's declared reach where it has
  one. It is the only *interactive* writer of the override layer: enter
  opens `$EDITOR` in place on the whole `key = value` line in force, ctrl+y
  copies the source value down, ctrl+x removes the override. Enter opens in
  place on a config row and the buffer is the one layer that write lands in
  — never the composed preview, which would have the human hand back layers
  they never wrote. The scriptable writers — `--trunk`, `--no-<component>`,
  **`pop config repo set`** and `pop workbench prefer` — reach the same
  layer through the same **Override edit gate**, because a bare
  repository's first managed register and a scripted integrate cannot open
  a TUI; what the model forbids is a second destination, never a second
  front-end (ADR-0212 decision 6). Two host contracts bind it: the host
  suspends its own keys while it is open, and it never writes to stdout on
  any path.
  was: The one surface for everything in force in a directory: a searchable list of dotted paths on the left, a preview of the highlighted row on the right (ADR-0202, extended by ADR-0212 decision 8). Its config rows come in the two scopes an override lands at: the keys of the global surface, and — for the repository the dashboard was opened in — the leaves of its `[repo]` block, addressed `repo.<key>` and written into the **Override config layer**'s block for that repository. A key the walker unions across rungs is no row of the repository scope, there being no one layer whose value a preview could name. The repository scope is what makes this the place a **Preferred workbench** is chosen, that key having no global spelling. Last come that repository's conventions, addressed `conventions.<kind>`, because the right pane answers the same question for a convention as for a key — what is in force, and what produced it — differing only in the medium the answer is written in. A convention row previews what is in force and nothing else: the one rank that answered, labelled with its origin and reach, the **Convention overlay** appended beneath it where there is one, and the provenance line, rendered by the `conventions` package itself so this pane and `pop conventions get` cannot disagree (ADR-0223) — a kind nobody has written an answer for previews its recipe under the banner marking it a method, that rank being what is in force there; enter edits that overlay as Markdown, ctrl+x removes it and ctrl+y refuses, the overlay being appended to the answer rather than laid over it, so copying the answer down would state it twice. A convention may be a **Contested key**: it resolves to one answer, so a document or a memory the answer stood down is exactly a layer quietly losing. Outside a repository neither the repository keys nor the conventions are offered. A self-contained `ui/` component rather than a page of any one program, because it must open from three unrelated tea programs — the Work dashboard, the project picker and the worktree picker — plus `pop config dashboard`, which runs the same model standalone and refuses a non-terminal stdout. Rows carry the key's `desc` dimmed beneath where height allows, a marker where an override is in force, and a second marker beside it where the key is a **Contested key** — which also sorts it to the top of the list. A config key's preview is config format throughout: the effective value as a `key = value` TOML statement, the layer that produced it (**Override config layer**, `config.toml`, a built-in default, or a fallthrough naming the key walked on to), the value an override stands on, and the key's declared reach where it has one. It is the only *interactive* writer of the override layer: enter opens `$EDITOR` in place on the whole `key = value` line in force, ctrl+y copies the source value down, ctrl+x removes the override. Enter opens in place for either sort of row and the buffer is the one layer that write lands in — never the composed preview, which would have the human hand back layers they never wrote — with pop's own note lines taken back out of it, since a prose buffer *is* the value. The scriptable writers — `--trunk`, `--no-<component>`, **`pop config repo set`** and `pop workbench prefer` — reach the same layer through the same **Override edit gate**, because a bare repository's first managed register and a scripted integrate cannot open a TUI; what the model forbids is a second destination, never a second front-end (ADR-0212 decision 6). Two host contracts bind it: the host suspends its own keys while it is open, and it never writes to stdout on any path.
