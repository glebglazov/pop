# How to work out this repository's code-review convention

Nobody has written down what good code looks like here, so a review has no
standard to read a changeset against.

**Pop does not ship one.** There is no house style in this tool to fall back
on, and inventing one would have a review judge this repository against a
stranger's taste. The standard belongs to the codebase: derive it from the
repository itself — its own documents, its linters and formatter, and the idiom
of the code that is already there — the same way the `commits` recipe reads the
team's log rather than pop's own commits.

Work all three sources. Unlike the other kinds, this one does **not** stop at
the first hit: a documented rule, an enforced rule and an unwritten habit are
three different parts of one standard, and a review that knows only one of them
reads half the code.

## 1. The repository's own documents

Read what the repository already says about itself: `AGENTS.md` / `CLAUDE.md`,
`CONTRIBUTING.md`, `docs/agents/`, `docs/adr/`, a style or architecture guide,
the package comments of the packages under review. Architectural decisions count
— a rule the team argued out in an ADR is a standard a review may hold code to.
This source outranks the other two: where a document and the surrounding code
disagree, the document is the team's answer and the code is drift, which is
itself worth reporting.

## 2. The linters, the formatter and the build

Read the configuration, not the tool's defaults: `.golangci.yml`, `.eslintrc`,
`ruff.toml`, `.rubocop.yml`, `Makefile` targets, the CI workflow, the pre-commit
hooks. Two things come out of it.

- **What is already enforced** — the checks that fail the build. A review that
  restates them wastes the reader's attention on something a machine says
  faster, so record them as *enforced elsewhere* and let the review spend itself
  on what no linter can see.
- **What the team chose to turn on**, which says what they care about: enabled
  complexity limits, an import ordering rule, a naming linter. The choices are
  evidence of the standard even where they are not the whole of it.

## 3. The idiom of the code itself

Read a handful of files in the areas the work touches, and take what they share:
how errors are handled and wrapped, how a test is written and what it exercises,
how comments are used and where, naming for the concepts of the domain, file
size and where a package boundary is drawn. Prefer recently-touched, non-generated
code — vendored trees, generated files and long-dead corners are not the team's
current idiom.

Write the habits down as habits, with an example location, so a later review can
tell an unwritten rule from an enforced one.

## 4. Write the result down, in the layer that fits where it came from

A derivation nobody records is one every future review pays for again.

- **Derived by you**, from the sources above, goes to the **pop memory** layer:
  pop's inference about one repository on one machine, recorded with what it was
  derived from so a reader can weigh it.
  `pop conventions get code-review` names that layer's path, and
  `pop conventions set code-review --derived-from "..."` writes it.
- **Stated by the human in session** — "we never mock in tests here" — is the
  team's rule, not pop's guess. Offer to put it in the repository's
  `docs/agents/code-review.md`, which outranks pop memory and which version
  control carries to their colleagues, and let the human decide.

Keep it short enough to be read in full before every review, and keep it to what
is true here. A standard that lists what any good code anywhere would do gives a
review nothing to hold this changeset against.

## 5. When nothing can be derived, write **that** down

A repository with no documents, no linter configuration and no shared idiom has
no standard, and that is a real result rather than a failed derivation. Do not
substitute a generic one. Record the nothing, in the pop memory layer, as plainly
as you would record a standard:

    No discernible code-review convention. No agent or contributor documents,
    no linter or formatter configuration, and no shared idiom across the files
    sampled. Derived from: <what you read>. Review only what the changeset's own
    stated intent asks for until the team states a standard.

Written down, the next review reads a settled answer and keeps its opinions to
itself. Left unwritten, every review re-derives the same nothing and fills the
gap with its own taste.
