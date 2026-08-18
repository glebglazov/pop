# How to work out this repository's code-review convention

Nobody has written down what good code looks like here, so a review has no
standard to read a changeset against.

**Pop does not ship a house style.** Inventing one would have a review judge
this repository against a stranger's taste. The standard belongs to the
codebase: derive it from the repository itself — its own documents, its
linters and formatter, and the idiom of the code that is already there — the
same way the `commits` recipe reads the team's log rather than pop's own
commits. What pop does ship is a floor beneath those three sources: a named
baseline of code smells, for a repository that has written nothing about
itself to still hold a changeset against something honest.

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

## 4. The smell baseline, as a floor

Beneath the three sources above sits a named baseline: a fixed set of Fowler
smells (*Refactoring*, ch. 3) that applies even when a repository has written
nothing about itself. Two rules bind it:

- **The repository overrides.** Anything derived from the three sources above
  wins — where a documented standard, an enforced rule or the code's own idiom
  endorses something the baseline would flag, suppress the smell.
- **Always a judgement call.** Each smell below is a labelled heuristic, never
  a hard violation, and — like anything derived above — skip whatever tooling
  already enforces.

Each smell reads *what it is* → *how to fix*:

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

## 5. Write the result down, in the repository's document

A derivation nobody records is one every future review pays for again.
Whether you derived it from the sources above or the human stated it in
session — "we never mock in tests here" — a review standard is prose about a
team's taste, and the repository's `docs/agents/code-review.md` is the only
right place for it: it is what version control carries to their colleagues.
Offer the addition, and let the human decide.

Keep it short enough to be read in full before every review, and keep it to what
is true here. A standard that lists what any good code anywhere would do gives a
review nothing to hold this changeset against.
