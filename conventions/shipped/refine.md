# The standard a changeset is held to

Nobody has written down what good code looks like here, so this is pop's own
answer: a short list of rules, generic by construction. Any document written at
a rank above this one — the team's `docs/agents/refine.md`, or either of your
own — displaces it whole, so a repository that has stated its taste is never
held to a stranger's.

Two rules bind the rest.

- **The repository overrides.** Where this repository already says something
  about itself, it wins: its own documents (`AGENTS.md` / `CLAUDE.md`,
  `CONTRIBUTING.md`, `docs/`), what its tooling already enforces (linter
  config, `Makefile` targets, the CI workflow), and the idiom of the
  non-generated code around the change. Where a document and the surrounding
  code disagree, the document is the team's answer and the code is drift —
  which is itself worth reporting.
- **Skip what a machine says faster.** What the build, the linter or the
  formatter already catches is not worth a line here.

## What good code looks like here

- **Names say what the thing is.** A function, variable or type whose name does
  not reveal what it does or holds wants renaming; when no honest name comes,
  the design is murky, and that is the finding.
- **One shape, one place.** The same logic written twice wants extracting once
  and calling twice.
- **Behaviour sits with its data.** A function reaching into another type's
  internals more than its own belongs on that type; fields that keep travelling
  together are a type waiting to be born; a primitive standing in for a domain
  concept wants that concept's own small type.
- **One reason to change.** A file edited for unrelated reasons wants splitting,
  and one change forcing scattered edits across many files wants gathering.
- **Nothing speculative.** Abstraction, parameters and hooks added for a need
  nothing asks for are deleted, not kept warm. The same goes for a layer that
  only delegates onward.
- **Comments earn their line.** A comment restating the code is noise; one
  saying why the code is this way is not; one that has gone false is worse than
  none.
- **Errors and tests follow the file.** Handle and wrap errors the way the
  surrounding code does, and write tests that drive a real path rather than
  pinning one method in place.

## What a pass may fix

Fix in place, and record it as fixed:

- a name, where the honest one is obvious and the rename is mechanical
- a duplicated shape, where extracting it touches only the sites that share it
- a comment that is false, stale, or restates the code
- dead code, an unused parameter, a hook nothing calls
- drift from the idiom of the file the code sits in

Report it and leave it alone:

- anything that changes behaviour, an exported interface, or a stored shape
- anything that moves code between packages, types or files
- anything a reasonable reader could disagree with
- anything whose whole effect you cannot see in the files you touched

Between the two, report. A fix nobody asked for costs more than a finding
nobody acts on.
