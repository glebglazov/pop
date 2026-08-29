---
fragment: CB5AAE8F
generation: 0041
branch: master
---

- Code review

+ Refine
  The **Drain** step, formerly Code review, that enforces the resolved `refine`
  **Convention kind** on a **Task set**'s accumulated changeset: a fresh
  **Refiner** researches the changeset, fixes in place what the convention
  licenses, and writes the **Refine report** of what it fixed and the smells it
  left. It runs at AFK quiescence *before* the verify phase, so its edits are
  judged by the same **Agent verification** pass as the work they refine —
  reversing the after-verify placement, which was derived from the step moving
  nothing. Set-scoped, gated by `[work.refine].enabled` (off by default); it
  reaches no verdict and spawns no tasks — its outputs are the **Refine
  commit** and the report. Automatic Refine skips a **Human completion** (the
  drain never edits code a human declared done); `pop tasks refine` runs a full
  pass by hand on any set with a done AFK task and a non-empty commit range,
  including a human-completed or DONE one — the human re-opening the question.
  avoid: Code review, code quality check, review step, polish, lint step, QA
  under: Verification

+ Refiner
  Formerly the Reviewer. The agent that performs **Refine**, running in a fresh
  context and chosen independently of the implementing agents so it does not
  refine its own work — the **Verifier**'s independence rule, for the same
  reason. Its prompt is the resolved `refine` convention as its whole body,
  wrapped in pop's **Role preamble** and **Response contract**, plus the
  previous **Refine report** where one exists. Its license: fix in place what
  the convention names, where the fix is safe and local; anything structural,
  behavioral, or debatable stays a finding in the report. It reads the changed
  files itself — the commit range and the **Work diff view** are orientation
  only. No longer spawned under a **Read-only agent posture**, and it never
  commits: the runner captures its edits as the **Refine commit**.
  avoid: Reviewer, polisher, fixer, critic, linter
  under: Verification

+ Refine report
  Formerly the Review artifact. The single living document **Refine** maintains
  for a **Task set**: what the latest pass fixed, the smells it left, and the
  SHA of the tree it describes. Each pass supersedes rather than appends — the
  **Refiner** reads the previous report and writes the current one, carrying
  forward what is still true; prior documents stay under the set's directory
  and every reader takes the latest by timestamp. A **Task artifact**, so it
  never enters an **Implementation commit**; reaching a PR is an explicit human
  act. Humans meet it as a pointer at the **HITL gate prompt** and in the
  **Task set detail view**, and an **Assist session**'s prompt names its path.
  avoid: Review artifact, review report, findings file, smell list
  under: Verification

+ Refine episode
  Formerly the Review episode, with one carve-out: it ends only when the set's
  done-AFK composition changes through *non-remediation* work. A **Remediation
  task**'s completion never re-arms automatic **Refine**, so a
  verify-remediation lap re-verifies without re-refining — the heavy pass stays
  out of exactly the iteration that should be cheapest — at the accepted cost
  that a remediation diff lands unrefined until the next real work re-arms the
  episode. Manual `pop tasks refine` runs at any time regardless.
  avoid: Review episode, refine cycle, refine generation
  under: Verification

+ Refine pointer
  Formerly the Review pointer: the **Refine report** as every surface other
  than a reader carries it — its path and the instant it was written, never a
  line of what it says. Staleness is tolerated for the same reason as before —
  a report of a tree that has since moved is still worth reading — and the
  report itself now names the SHA it describes.
  avoid: Review pointer, review link, latest review
  under: Verification

+ Refine commit
  The runner-made commit capturing one **Refine** pass's in-place fixes: one
  commit per pass, subject rendered under the resolved commits convention, the
  set named in a trailer — agents never commit, so the **Refiner**'s edits
  reach history the same way an Implementer's do, and the tree is clean when
  the verify phase reads it. A prior verify PASS is not invalidated by it:
  within a **verification episode** the PASS stands and the **Verified-at SHA**
  badge shows the drift, exactly as for any other commit past a PASS. In the
  automatic flow this is moot — Refine precedes verify, so a pass's edits are
  verified together with the work they refine.
  avoid: polish commit, style commit, fix commit
  under: Verification

+ Refine convention
  The `refine` **Convention kind**, formerly `code-review`: what good code
  looks like in this repository and what a refine pass may fix. Role-driving —
  it is the **Refiner**'s prompt body — and doubly consumed: under **Refine
  convention inlining** the same resolved text also rides every implement
  prompt as a labelled block, so builders adhere upfront to the rules the
  Refiner later enforces. One text, both consumers, which is why its **Shipped
  convention** is short and rule-shaped rather than essay-length. Renamed with
  the step because names are addresses and one word everywhere beats two names
  for one thing: `docs/agents/refine.md`, `pop conventions get refine`.
  avoid: code-review convention, coding standard kind, quality rules
  under: Conventions

+ Refine convention inlining
  The `[work.implement].include_refine_convention` toggle, default false: when
  set, the resolved `refine` convention enters every implement prompt as a
  labelled block — planned tasks and **Remediation task**s alike, since both
  drain through the same prompt. Deliberately independent of
  `[work.refine].enabled`: telling builders the house style upfront needs no
  fixing pass, and a human may drive adherence for a while before enabling the
  step.
  avoid: work prompt convention, upfront adherence flag, build-time rules
  under: Conventions

~ Convention kind
  One member of the closed set of things pop can hold a **Repo convention**
  for — `commits`, `issue-tracker`, `refine` and `verification`. Closed
  because each kind ships a **Shipped convention** pop must have written, so a
  kind pop has never heard of has no answer to offer; an unknown kind is
  refused with the list of the ones that exist, as `pop config repo set`
  refuses an unknown key. A kind also declares its **Convention consumption
  shape**, which is what tells the author of the next kind what they owe it.
  Names are addresses and stay stable: `docs/agents/issue-tracker.md` is read
  by third-party skills under that exact name, and `refine` and `verification`
  match pop's own step nouns — `refine` renamed from `code-review` together
  with its step while the feature had a single user.
  avoid: convention type, convention name
  was: One member of the closed set of things pop can hold a **Repo convention** for — `commits`, `issue-tracker`, `code-review` and `verification`. Closed because each kind ships a **Shipped convention** pop must have written, so a kind pop has never heard of has no answer to offer; an unknown kind is refused with the list of the ones that exist, as `pop config repo set` refuses an unknown key. A kind also declares its **Convention consumption shape**, which is what tells the author of the next kind what they owe it. Names are addresses and stay stable: `docs/agents/issue-tracker.md` is read by third-party skills under that exact name, and `code-review` and `verification` match pop's own step nouns.

~ Convention consumption shape
  Whether a **Convention kind** reaches an agent as a prompt *body* or as a
  labelled *block*, declared by the kind and honoured at every call site
  (ADR-0227). A **role-driving** kind — `verification`, `refine` — is an
  agent's entire mandate, so the convention is the body and pop supplies only a
  **Role preamble** and a **Response contract** around it; there is then
  exactly one voice on what to check. A **step-informing** kind — `commits`,
  `issue-tracker` — is a fact a prompt about something else needs, so it stays
  a block inside pop's prompt. The shape names a kind's *mandate-bearing*
  consumption, not its only one: a role-driving kind may additionally be taken
  as a labelled block by a prompt it merely informs, as the implement prompt
  takes the `refine` convention under **Refine convention inlining**.
  avoid: prompt shape, envelope, injection mode
  was: Whether a **Convention kind** reaches an agent as a prompt *body* or as a labelled *block*, declared by the kind and honoured at every call site (ADR-0227). A **role-driving** kind — `verification`, `code-review` — is an agent's entire mandate, so the convention is the body and pop supplies only a **Role preamble** and a **Response contract** around it; there is then exactly one voice on what to check, where a convention that merely supplemented pop's own prompt would leave the team's answer arguing with pop's and no rule for which wins. A **step-informing** kind — `commits`, `issue-tracker` — is a fact a prompt about something else needs, so it stays a block inside pop's prompt, having no output contract to protect.

~ Read-only agent posture
  The **Adapter capability** by which an **Agent preset** is told, in argv, to
  run without the ability to change the checkout it was pointed at. claude
  contributes `--disallowedTools=Edit,Write,NotebookEdit`, codex `--sandbox
  read-only`, cursor `--mode ask`, pi `--exclude-tools edit,write`; opencode
  and kimi declare it blind, and pop states the posture it actually obtained
  rather than the one it wanted. No role is currently spawned under it: the
  **Refiner** — formerly its one consumer, as the Reviewer — now fixes in
  place. The capability stays declared per preset because it is a fact about
  what each CLI can do, not a preference, and the per-preset flag research
  should not be re-derived when the next read-only role appears.
  avoid: sandbox mode, review worktree, permission mode
  was: The **Adapter capability** by which an **Agent preset** is told, in argv, to run without the ability to change the checkout it was pointed at, and the one role pop spawns under it: the Reviewer. claude contributes `--disallowedTools=Edit,Write,NotebookEdit` (the `=` spelling because the flag is variadic and would otherwise swallow the prompt), codex `--sandbox read-only` in place of the headless prefix's own sandbox bypass, which outranks it wherever both are passed, cursor `--mode ask`, and pi `--exclude-tools edit,write`; opencode and kimi declare it blind, and pop states the posture it actually obtained rather than the one it wanted. The guarantee therefore differs by preset — codex's sandbox blocks a write made by any shell command, while a tool denial leaves bash in place — and both are adequate for a role that only runs `git diff`, `git log` and `git show`. It stops at the Reviewer on purpose: **Agent verification** runs the build and the test suite, and a build that cannot write its cache fails for reasons that have nothing to do with the code under judgment. Enforcement never replaces instruction — the Reviewer's prompt keeps its sentence forbidding file changes, since an agent told what it may do writes a better review than one that discovers a tool is missing (ADR-0221).

- Reviewer

- Review artifact

- Review episode

- Review pointer
