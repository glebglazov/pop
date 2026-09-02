---
fragment: B389D0CD
generation: 0043
branch: master
---

- Refine convention

- Refine convention inlining

+ Implementation convention
  The `implementation` **Convention kind** (ADR-0246): what good code looks like
  in this repository, **code and tests as one subject** — there is no separate
  `tests` kind, because how a repository tests is how it writes code.
  Step-informing, so it reaches a prompt as a labelled block. Two consumers with
  deliberately different terms: the **Refiner** always carries it, and the
  implementer only when `[work.implement].include_implementation_convention` is
  set (default false). It states standards and never licence — what a pass may fix
  belongs to the **Refine** step's own procedure. The **Verifier** is not a
  consumer: it judges the spec and holds a gate, and a quality standard in the
  hands of the one role that can fail a set turns taste into VERIFY-FAILED.
  avoid: standards convention, refine convention, code-review convention, tests kind
  under: Conventions

+ Implementation convention inlining
  The `[work.implement].include_implementation_convention` toggle, default false:
  when set, the resolved **Implementation convention** enters every implement
  prompt as a labelled block — planned tasks and **Remediation task**s alike.
  Independent of `[work.refine].enabled`, so a repository may hold its builders to
  the standard long before the pass is switched on. Formerly
  `include_refine_convention`; the old key is a load-time error naming the new one
  rather than a silent alias, a `bool` defaulting to false being a key whose
  removal would otherwise cost behaviour with no signal.
  avoid: standards inlining, upfront adherence flag, include_standards_convention
  under: Conventions

+ Overlay
  The one layer that *appends* rather than replaces: the human's
  (`~/.agents/docs/<name>.overlay.md`) or the team's
  (`docs/agents/<name>.overlay.md`) prose, riding along with whichever document
  answered. Generalised from **Convention overlay** by ADR-0247 in two directions
  at once — it is keyed on a *named document* rather than a **Convention kind**, so
  it reaches pop's own step prompts as well, and it gained the repository rank that
  makes "not overridable" principled instead of lossy: a team with a house rule
  about a step it may not rewrite has somewhere committed to put it. One noun for
  one file shape and one semantic. Carries no frontmatter, being the author's own
  statement rather than something pop derived; a whitespace-only body is refused.
  avoid: convention overlay, step overlay, per-kind overlay, convention override
  under: Conventions

~ Convention kind
  One member of the closed set of things pop can hold a **Repo convention**
  for — `commits`, `issue-tracker`, `implementation` and `verification`. Closed
  because each kind ships a **Shipped convention** pop must have written, so a
  kind pop has never heard of has no answer to offer; an unknown kind is refused
  with the list of the ones that exist, as `pop config repo set` refuses an
  unknown key. A kind also declares its **Convention consumption shape**, which is
  what tells the author of the next kind what they owe it. What makes something a
  kind at all is the rule in ADR-0247: a convention holds a repository's facts and
  standards, while a step's procedure is pop's — which is why `refine` was one and
  is not, its standards having become `implementation` and its procedure having
  moved into the Refiner's prompt. Names are addresses and stay stable.
  avoid: convention type, convention name
  was: One member of the closed set of things pop can hold a **Repo convention** for — `commits`, `issue-tracker`, `refine` and `verification`. Closed because each kind ships a **Shipped convention** pop must have written, so a kind pop has never heard of has no answer to offer; an unknown kind is refused with the list of the ones that exist, as `pop config repo set` refuses an unknown key. A kind also declares its **Convention consumption shape**, which is what tells the author of the next kind what they owe it. Names are addresses and stay stable: `docs/agents/issue-tracker.md` is read by third-party skills under that exact name, and `refine` and `verification` match pop's own step nouns — `refine` renamed from `code-review` together with its step while the feature had a single user.

~ Convention consumption shape
  Whether a **Convention kind** reaches an agent as a prompt *body* or as a
  labelled *block*, declared by the kind and honoured at every call site
  (ADR-0227). A **role-driving** kind — `verification`, and after ADR-0247 only
  `verification` — is an agent's entire mandate, so the convention is the body and
  pop supplies only a **Role preamble** and a **Response contract** around it;
  there is then exactly one voice on what to check. A **step-informing** kind —
  `commits`, `issue-tracker`, `implementation` — is a fact a prompt about
  something else needs, so it stays a block inside pop's prompt. The shape names a
  kind's *mandate-bearing* consumption, not its only one.
  avoid: prompt shape, envelope, injection mode
  was: Whether a **Convention kind** reaches an agent as a prompt *body* or as a labelled *block*, declared by the kind and honoured at every call site (ADR-0227). A **role-driving** kind — `verification`, `refine` — is an agent's entire mandate, so the convention is the body and pop supplies only a **Role preamble** and a **Response contract** around it; there is then exactly one voice on what to check. A **step-informing** kind — `commits`, `issue-tracker` — is a fact a prompt about something else needs, so it stays a block inside pop's prompt. The shape names a kind's *mandate-bearing* consumption, not its only one: a role-driving kind may additionally be taken as a labelled block by a prompt it merely informs, as the implement prompt takes the `refine` convention under **Refine convention inlining**.

~ Refiner
  The agent that performs **Refine**, running in a fresh context and chosen
  independently of the implementing agents so it does not refine its own work. Its
  prompt is pop's own end to end (ADR-0247) — the procedure is not a convention —
  carrying the **Implementation convention** as a labelled block, the previous
  **Refine report** where one exists, and any **Overlay** on the step. Its licence
  is *reversibility*, not locality (ADR-0248): a fix that can be undone by
  inspection and whose whole effect it can see, it makes; behaviour, exported
  interfaces, stored shapes and expensive-to-reverse structure stay findings. It
  may read the whole repository to prove a fix safe and must show that search in
  the report, but it edits outside the changed files only where a fix inside them
  forces it. It runs the scoped gate before and after, fetching `pop conventions
  get verification` itself. It never commits.
  avoid: Reviewer, polisher, fixer, critic, linter
  was: Formerly the Reviewer. The agent that performs **Refine**, running in a fresh context and chosen independently of the implementing agents so it does not refine its own work — the **Verifier**'s independence rule, for the same reason. Its prompt is the resolved `refine` convention as its whole body, wrapped in pop's **Role preamble** and **Response contract**, plus the previous **Refine report** where one exists. Its license: fix in place what the convention names, where the fix is safe and local; anything structural, behavioral, or debatable stays a finding in the report. It reads the changed files itself — the commit range and the **Work diff view** are orientation only. No longer spawned under a **Read-only agent posture**, and it never commits: the runner captures its edits as the **Refine commit**.

~ Refine report
  The single living document **Refine** maintains for a **Task set**, in three
  parts (ADR-0248): what the pass **Fixed**, what it **Left in this changeset**,
  and what it found **Revealed by this changeset**. The first two are replaced
  each pass, carrying forward what is still true; the third is stated once and
  never carried forward, a revealed shape being one no later pass can fix, so
  carrying it would turn the report into the backlog ADR-0240 refused. The section
  boundary is what enforces that, a sentence being the thing a third pass drops.
  Prior documents stay under the set's directory, so a superseded revealed finding
  is unpointed rather than lost. A **Task artifact**; humans meet it as a
  **Refine pointer**.
  avoid: Review artifact, review report, findings file, smell list, backlog
  was: Formerly the Review artifact. The single living document **Refine** maintains for a **Task set**: what the latest pass fixed, the smells it left, and the SHA of the tree it describes. Each pass supersedes rather than appends — the **Refiner** reads the previous report and writes the current one, carrying forward what is still true; prior documents stay under the set's directory and every reader takes the latest by timestamp. A **Task artifact**, so it never enters an **Implementation commit**; reaching a PR is an explicit human act. Humans meet it as a pointer at the **HITL gate prompt** and in the **Task set detail view**, and an **Assist session**'s prompt names its path.

+ Refine outcome
  The machine-read line a **Refiner** puts before its report,
  `REFINE-OUTCOME: refined | gate-blocked | abandoned`, lifted by
  `splitRefinerReply` beside `COMMIT-SUBJECT:` the way `VERDICT:` is lifted from a
  **Verifier**'s reply (ADR-0248). `gate-blocked` is a red scoped gate on entry —
  no agent work, no commit, no **Refine episode**. `abandoned` is a red gate on
  exit, and pop discards the pass's changes against the tree state it captured
  before invoking the Refiner. pop needs the outcome, not the gate readings, which
  belong in the report prose. Self-reported, like a verdict, and caught one step
  later by the verify phase when it lies.
  avoid: gate result, refine status, pass verdict
  under: Verification

+ Revealed finding
  A finding whose shape predates the changeset that exposed it, as against a
  *created* one the changeset introduced — the line **Refine** draws instead of
  location, and checkable with `git show <base>:<file>` (ADR-0248). Merging locks
  a created problem in, so it belongs to reviewing this work; a revealed one costs
  the same to fix later, so it is future refactoring work. Admissible only when the
  changeset is the evidence — an existing coupling forced the change into scattered
  places, or the new code had to work around the old shape — and it always names
  the refactoring it wants, so it is executable rather than advice.
  avoid: pre-existing smell, out-of-scope finding, technical debt item
  under: Verification

~ Refine episode
  Formerly the Review episode, with two carve-outs. It ends only when the set's
  done-AFK composition changes through *non-remediation* work, so a
  verify-remediation lap re-verifies without re-refining. And a pass that read
  nothing and wrote nothing records none: a `gate-blocked` or `abandoned`
  **Refine outcome** leaves the episode armed, because an episode means "this
  composition has been refined" and a transient red gate must not cost the set its
  pass permanently. Retrying is cheap — the gate-red path exits before any agent
  work. Manual `pop tasks refine` runs at any time regardless.
  avoid: Review episode, refine cycle, refine generation
  was: Formerly the Review episode, with one carve-out: it ends only when the set's done-AFK composition changes through *non-remediation* work. A **Remediation task**'s completion never re-arms automatic **Refine**, so a verify-remediation lap re-verifies without re-refining — the heavy pass stays out of exactly the iteration that should be cheapest — at the accepted cost that a remediation diff lands unrefined until the next real work re-arms the episode. Manual `pop tasks refine` runs at any time regardless.

- Convention overlay

~ Refine
  The **Drain** step, formerly Code review, that holds a **Task set**'s
  accumulated changeset to the resolved **Implementation convention**: a fresh
  **Refiner** researches the changeset, fixes in place what its licence allows,
  and writes the **Refine report**. Its procedure is pop's own and not a
  **Convention kind** (ADR-0247) — a repository steers it with an **Overlay**
  rather than replacing it, while what counts as a problem stays entirely the
  repository's, in the convention. It runs at AFK quiescence *before* the verify
  phase, so its edits are judged by the same **Agent verification** pass as the
  work they refine. Set-scoped, gated by `[work.refine].enabled` (off by
  default); it reaches no verdict and spawns no tasks — its outputs are the
  **Refine commit** and the report. Automatic Refine skips a **Human
  completion**; `pop tasks refine` runs a full pass by hand on any eligible set.
  avoid: Code review, code quality check, review step, polish, lint step, QA
  was: The **Drain** step, formerly Code review, that enforces the resolved `refine` **Convention kind** on a **Task set**'s accumulated changeset: a fresh **Refiner** researches the changeset, fixes in place what the convention licenses, and writes the **Refine report** of what it fixed and the smells it left. It runs at AFK quiescence *before* the verify phase, so its edits are judged by the same **Agent verification** pass as the work they refine — reversing the after-verify placement, which was derived from the step moving nothing. Set-scoped, gated by `[work.refine].enabled` (off by default); it reaches no verdict and spawns no tasks — its outputs are the **Refine commit** and the report. Automatic Refine skips a **Human completion** (the drain never edits code a human declared done); `pop tasks refine` runs a full pass by hand on any set with a done AFK task and a non-empty commit range, including a human-completed or DONE one — the human re-opening the question.
