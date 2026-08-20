# The standard to hold a changeset against

Nobody has written down what good code looks like here, so this is pop's own
answer: a review on two axes — how the code is written, and whether it does what
was asked — with a named baseline of smells for the first, generic by
construction. Any document written at a rank above this one — the team's
`docs/agents/code-review.md`, or either of your own — displaces it whole, so a
repository that has stated its taste is never reviewed against a stranger's.

Two rules bind everything below.

- **The repository overrides.** Read what this repository already says about
  itself before flagging anything, and let it win wherever it endorses something
  the baseline would flag. Three sources say it: the repository's own documents
  — `AGENTS.md` / `CLAUDE.md`, `CONTRIBUTING.md`, `docs/agents/`, `docs/adr/`, a
  style or architecture guide, the package comments of the code under review;
  what is already enforced — `.golangci.yml`, `.eslintrc`, `ruff.toml`,
  `.rubocop.yml`, `Makefile` targets, the CI workflow, the pre-commit hooks, read
  as configuration rather than as the tool's defaults; and the idiom of the
  surrounding non-generated code — how errors are handled and wrapped, how a test
  is written and what it exercises, how comments are used, how the concepts of
  the domain are named. Where a document and the surrounding code disagree, the
  document is the team's answer and the code is drift, which is itself worth
  reporting.
- **Always a judgement call.** Each smell below is a labelled heuristic, never a
  hard violation. Skip whatever tooling already enforces: a review that restates
  a check the build runs spends the reader's attention on something a machine
  says faster, so record those as *enforced elsewhere* and spend the review on
  what no linter can see.

## Axis 1 — Standards: how the code is written

A fixed set of Fowler smells (*Refactoring*, ch. 3). Each reads *what it is* →
*how to fix*:

- **Mysterious Name** — a function, variable, or type whose name doesn't reveal
  what it does or holds. → rename it; if no honest name comes, the design's murky.
- **Duplicated Code** — the same logic shape appears in more than one place. →
  extract the shared shape, call it from both.
- **Feature Envy** — a method that reaches into another object's data more than
  its own. → move the method onto the data it envies.
- **Data Clumps** — the same few fields or params keep travelling together (a
  type wanting to be born). → bundle them into one type, pass that.
- **Primitive Obsession** — a primitive or string standing in for a domain
  concept that deserves its own type. → give the concept its own small type.
- **Repeated Switches** — the same `switch`/`if`-cascade on the same type
  recurs. → replace with polymorphism, or one map both sites share.
- **Shotgun Surgery** — one logical change forces scattered edits across many
  files. → gather what changes together into one module.
- **Divergent Change** — one file or module is edited for several unrelated
  reasons. → split so each module changes for one reason.
- **Speculative Generality** — abstraction, parameters, or hooks added for a
  need nothing asks for. → delete it; inline back until a real need shows.
- **Message Chains** — long `a.b().c().d()` navigation the caller shouldn't
  depend on. → hide the walk behind one method on the first object.
- **Middle Man** — a class or function that mostly just delegates onward. →
  cut it, call the real target direct.
- **Refused Bequest** — a subclass or implementer that ignores or overrides
  most of what it inherits. → drop the inheritance, use composition.

## Axis 2 — Spec: whether the code does what was asked

Well-written code that answers a different question than the one asked is still
the wrong change, and only a reader of the diff can see it. Take what the work
set out to do from whatever states it — the request, the spec, the task titles —
and read the changed files against it:

- **Missing** — something the work was asked for that no file does. Name what is
  absent and where it would have gone.
- **Extra** — behaviour, configuration or abstraction the request never asked
  for. Say what it is; a change nobody wanted still has to be maintained.
- **Different** — what the code does where the request said something else, most
  often in an edge the request settled and the code decided for itself.

Read the diff for this axis rather than the report beside it: a summary, a
commit message and a checked box each state an intent, and the code is what
happened. Where the request is genuinely silent, say the request is silent
instead of inventing the requirement it did not state.

Another reader may reach this axis from the acceptance criteria rather than from
the files, and the two of you can disagree. That is the point of reading it here:
a second opinion from different evidence, in a document a human weighs.

## What a finding is worth saying

A finding names its axis, the location, and what the fix would be. A review that
lists what any good code anywhere would do gives the reader nothing to hold
*this* changeset against.

Your reply is the whole review. Change no files, record nothing on disk, and fix
nothing you found — what you write is read by a human who decides what to do
about it.
