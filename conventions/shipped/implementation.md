# The standard a changeset is held to

Nobody has written down what good code looks like here, so this is pop's own
answer: a short list of rules, generic by construction. Any document written at
a rank above this one — the team's `docs/agents/implementation.md`, or either of your
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
  the design is murky, and that is the finding. The same kind of thing carries
  the same name across the module.
- **Duplication follows the Rule of Three.** Three or more occurrences of the
  same logic share one shape; under three, the copies stand — silence, not even
  a finding.
- **Length is a reason to look.** A long function invites asking whether its
  parts are conjoined — must be read together to understand either — or would
  stand alone as a deep piece with a simple interface. Length alone never
  decides extraction; a shallow extract stays inline.
- **Behaviour sits with its data.** A function reaching into another type's
  internals more than its own belongs on that type; fields that keep travelling
  together are a type waiting to be born; a primitive standing in for a domain
  concept wants that concept's own small type.
- **One reason to change.** A file edited for unrelated reasons wants splitting,
  and one change forcing scattered edits across many files wants gathering.
- **Capabilities earn their keep.** Behaviour, features and hooks added for a
  need nothing asks for stay out. A general-purpose signature is not a finding —
  YAGNI is about capabilities, not interface shape. A layer that only delegates
  onward is not a layer: each abstraction differs from the ones around it.
- **Comments earn their line.** A comment restating the code is noise; one
  saying why the code is this way is not; one that has gone false is worse than
  none.
- **Errors follow the file.** Handle and wrap errors the way the surrounding
  code does.
- **Unfamiliar is not a finding.** An idiom the surrounding non-generated code
  already uses is the local answer, even when another style is more familiar.
- **A refactor changes no test file.** Tests couple to behaviour, not shape.
- **Assert the end result, not the steps.** The outcome a caller cares about is
  the assertion; the path taken to reach it is not.
- **A test is a liability like any code.** Confidence comes from driving a whole
  path and skipping the obvious — coverage is not the goal.
- **Test through the path a caller uses.** Visibility stays as callers need it;
  a test never widens what production exposes.
- **Prefer the real collaborator.** Double only what crosses a boundary you do
  not control.
