<!--
No upstream base, so no drift pin and no verbatim region: this document is
pop-original (ADR-0253 decision 6). It holds every rule the two grilling
composers follow identically — the round-close beat for glossary writes, one
fact-finding activity, and the close, which is a commit and then a hand-off ask
— so each of their bodies is only what differs.

`grill-with-docs` owns it, because these rules were its own before the
fast sibling existed. Integration copies it into `grill-with-docs-fast` at
install time (`sharedSkillDocs` in integrate/catalog.go), so both bodies reach
it as `./GRILL-SESSION.md` wherever they are installed.

Why a document and not a fourth skill: a composer cannot *load* the rules from
its sibling — `grill-with-docs` is human-opened, and a `disable-model-invocation`
skill cannot be loaded by another skill, which is why `grilling` and
`domain-modeling` are agent-loaded at all. A procedure both sessions follow
verbatim is a document; only a session shape is a skill.
-->

# The grilling session's own rules

Both grilling composers load the same two skills — `grilling` for the interview
and `domain-modeling` for the model. The rules below are what the session adds
on top of them. Nothing here repeats a rule either skill already carries.

## Glossary writes ride the round

**Write once a round, not per term.** The interview settles decisions in rounds,
so in this session the glossary writes ride the same beat: at each round's close,
if that round settled any terms, write their ops to your fragment in a single
update, and skip rounds that settled nothing. This is the session beat
`domain-modeling` leaves to a composing workflow; it replaces that skill's
write-when-it-settles timing and nothing else — where the fragment lives, how its
generation is picked, and the op syntax are unchanged.

## Fact-finding is one activity

The interview's "find facts yourself, never ask the user," and the discipline's
"challenge against the glossary" and "cross-reference with code" are **the same
activity**, not three separate ones. Read the code and the base+fragment
glossary union directly — inline for a cheap check, a non-blocking sub-agent for
heavy exploration — and surface any contradiction between what you find and what
the user claimed. There is no path where you ask the user to supply a fact you
could have looked up.

## Closing the session

Once you've proposed the final glossary updates and any ADRs, and the user signals the design is settled (or asks to wrap up), **commit the artifacts this session produced automatically** — don't ask first. Committing is always desired at the close, so just do it and report what was committed. Do this once, at the natural close — don't commit mid-grill or after every individual fragment.

Why this matters: these artifacts often get carried into downstream work via a fresh git worktree forked from the current branch's HEAD (for example when `to-tasks` later turns the plan into work items). Anything not committed to HEAD is left behind. The session that produced the artifacts is the right place to commit them, so don't defer this to a later skill.

To commit:

1. **Skip if nothing to do.** If the working directory is not a git repository, or this session created/modified no committable repository files, say so and skip.
2. **Identify session paths.** From this conversation's history, list *exactly* the repository files this session created or modified — the base glossary (`CONTEXT.md`, `CONTEXT-MAP.md`), session fragments (`.grill-context/**`, plus any legacy `CONTEXT.*.md` colocated beside a base), ADRs (`docs/adr/**`), and any code or prototype the session touched. Commit CONTEXT fragments **as-is** — folding them into the base is the separate `grill-consolidate` pass, never part of this commit. Do **not** include files this session never touched, even if dirty; prior-session artifacts are intentionally out of scope.
3. **Stage exactly those paths** (never `git add -A`) and create a **single commit**. Derive a short `<topic-slug>` from the subject of the grilling session (the term or area discussed). The type follows content:
   - docs-only → `docs(<topic-slug>): <summary> (ADR-NNNN + glossary)` (drop whichever parenthetical part doesn't apply)
   - mixed code + docs → a fitting conventional type (`feat`, `chore`, …), still scoped `(<topic-slug>)`

   Write a short human `<summary>` of what the artifacts cover (e.g. `effort-model-resolution glossary + ADR-0032`).

   Before writing the subject, resolve the commit convention by asking pop:

   ```
   pop conventions get commits
   ```

   Do not derive the grammar yourself. The command always exits 0 and always
   prints rules to follow — pop's own shipped answer where nobody has written
   one — so take the printed convention as resolved and match its grammar and
   trailer.
   Read every line the command prints — do not pipe it through `head`, `tail`,
   `sed -n` or `grep`, since a prefix read drops rules you are still bound by;
   the output's first line says how many lines to read. The `type` still follows content (docs-only vs. mixed), as above.
4. **Report.** After committing, show the user the exact files staged and the commit subject. Separately, report any dirty files this session did *not* touch as "left alone — not staged" so nothing is silently swept or split.
5. **Ask what happens next, and wait.** The plan is settled and persisted; what to
   do with it is the user's word, never an inference from how settled the design
   looks. Ask which of two things to do — implement it now in this session, or
   leave it for a separate step (`to-tasks` is the usual one) — and wait for the
   answer. Either answer is an instruction this session then carries out, and
   "now" means implement it here, where the grilled context still lives.
   A licence the user granted mid-session covers the act it named and stops
   there — being told to write the text is not being told to close the session —
   so this ask happens however much permission is already in play.
