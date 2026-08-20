# Check the work by running this repository's own gates

Nobody has written down how work is checked here, so this is pop's own answer:
confirm reality, do not trust a self-report. It is generic by construction — pop
cannot know this project's toolchain — and any document written at a rank above
this one displaces it whole.

## Find the invocation before you run anything

The build and test invocation is a fact about this repository, and it is written
down somewhere in it. Read, in this order, whichever exist:

- the repository's agent document — `AGENTS.md`, `CLAUDE.md` or the equivalent —
  which usually states the one command a contributor is expected to run;
- the task runner the repository ships: `Makefile`, `justfile`, `Taskfile.yml`,
  the `scripts` block of `package.json`, `mix.exs`, `Rakefile`;
- the continuous-integration workflow, which is the invocation the project
  actually gates merges on when nothing else says.

Use what you find rather than a command you know from another project. A test
command that is not this repository's — the right tool with the wrong flags, a
package manager the lockfile does not match — proves nothing about this work
even when it exits 0.

## A scoped gate first, the whole-tree gate to finish

Two gates answer two different questions, and both are worth running:

- **Scoped** — build, vet and test only the packages the change touched. It is
  fast enough to run repeatedly while you read the diff, and it is the gate that
  tells you whether the change itself holds together.
- **Whole-tree** — the repository's single documented command over everything,
  the one its agent document or CI names. It is what catches a change that
  compiles where it was made and breaks a caller elsewhere, and it is the gate
  that has to be green before the work is believed.

Run the scoped gate while judging, and the whole-tree gate before concluding. A
scoped gate alone is not a verified build.

## What counts as evidence

Evidence is the output of a command you ran in this checkout, in this attempt.

- A command's own exit status and output count. Read the failures, not just the
  final line: a runner that reports a green summary while a package errored has
  not passed.
- The diff counts, and reading it is not optional. Fetch the files a claim turns
  on and read them, rather than accepting a summary of what they contain.
- A prior run's output does not count once the tree has changed under it.
- A claim in a report, a comment, a commit message or a checked box counts for
  nothing on its own. It is a statement of intent to be checked against the
  code and the gates.

## A flaky or unrunnable gate is a finding, not a pass

When a gate cannot run — a missing toolchain, a dependency that will not fetch,
a suite that hangs — say so plainly, name the command and what it did, and treat
the work as unchecked. When a failure looks like flake, re-run that gate alone
and report both outcomes; a test that fails under load and passes in isolation
is a fact worth stating, not one to bury.
