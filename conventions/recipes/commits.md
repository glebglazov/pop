# How to work out this repository's commits convention

Nobody has written this repository's commit grammar down where pop can read it.
Below is how to derive it. Work the steps in order and stop at the first one
that yields a grammar.

**One document, one sample.** The `commits` convention covers **both** the
subject grammar — the type set, the scope vocabulary, mood, capitalisation and
length — **and** the body style: whether there is a body at all, what it is
expected to explain, and how it is wrapped. They are one kind, answered by one
document and one git log sample, never two. Derive both together and write both
down together; a convention that pins the subject and says nothing about bodies
is half-derived.

## 1. The repository's own document wins

Read `docs/agents/commits.md`. If it exists, take the grammar from it and
**stop — do not sample the log**. A team that wrote its convention down has
already answered the question, and a log sample can only contradict it.

## 2. Otherwise infer it from the last five commits

Sample the last five commits — subjects and bodies — and read them as a set,
looking for what they share rather than what any one of them does.

**Discard pop-generated commits before sampling.** Pop's own default subjects
(`tasks(...)`) and pop's skill-commit shapes are pop's accent, not the team's.
Drop them and infer from the non-pop commits that remain. **Walk further back
when the recent window is all pop's**: take the next five, and keep walking
until you hold five non-pop commits or the history runs out. Without this guard
pop learns its own accent back from the log on every repository it has drained,
and the convention degenerates into a copy of itself.

## 3. Write the result down, in the layer that fits where it came from

A derivation nobody records is one the next agent pays for again.

- **Derived from history** — by you, from the log, in the step above — goes to
  the **pop memory** layer. It is pop's inference about one repository on one
  machine, held at pop's own rank, and it records what it was derived from so a
  reader can weigh it. `pop repo conventions get commits` names that layer's
  path.
- **Stated by the human in session** — "we write our scopes as the package
  name" — is a fact the team owns, so offer to put it in the repository's
  `docs/agents/commits.md` and let the human decide. Do not file a human's
  ruling away in pop memory as if pop had guessed it: it belongs in version
  control, where their colleagues get it too.

## 4. When there is no discernible convention, write **that** down

A log with no shared grammar is a real result, not a failed derivation. Record
it, in the pop memory layer, as plainly as you would record a grammar:

    No discernible commits convention. The last five non-pop commits share no
    type set, no scope vocabulary and no consistent subject or body shape.
    Derived from: <what you sampled>. Write commits in pop's default format
    until the team states one.

Written down, the next agent reads a settled answer and moves on. Left unwritten,
it re-derives the same nothing from the same log, forever.
