---
name: grill-consolidate
description: Fold accumulated glossary fragments (from `.grill-context/`, plus any legacy colocated CONTEXT.<counter>.<uuid>.md) into canonical CONTEXT.md files, reconcile clashing sequential ADR numbers (re-sequencing duplicates and fixing the links that pointed at them), and stub superseded ADRs down to their forward pointer, as a deliberate single-writer maintenance pass. Use when the user asks to consolidate, fold, merge, reconcile, clean up, or resolve concurrent context/glossary fragments, duplicate ADR numbers, or superseded ADRs produced by grill-with-docs.
---

# Grill Consolidate

Use this skill for the single-writer maintenance pass over the artifacts `grill-with-docs` produces under parallel use:

1. Merge session glossary fragments into their base `CONTEXT.md`. Fragments live in `.grill-context/` at the repo root (`<slug>.<counter>.<uuid>.md`, or `CONTEXT.<counter>.<uuid>.md` in a single-context repo), plus any legacy `CONTEXT.<counter>.<uuid>.md` colocated beside a base.
2. Reconcile sequential ADR numbers under `docs/adr/` — detect duplicate `NNNN`, re-sequence the clash losers, and fix the links that referenced them.
3. Stub superseded ADRs — reduce each `status: superseded by ADR-NNNN` ADR to its frontmatter, title, and forward-pointing blockquote, cutting the now-dead body (recoverable from git history).

This is a single-writer operation. Do not run it speculatively, automatically, or in parallel with another consolidation pass. If a contested term or an ambiguous link requires a semantic decision, ask the user and wait.

## Find Contexts

Start at the repository root.

- If `CONTEXT-MAP.md` exists, read it. Each context's **link text**, slugified (lowercase, non-alphanumeric runs → `-`), is its slug; the link target's directory holds its base `CONTEXT.md`.
- If a root `CONTEXT.md` exists (no map), inspect that single context; its fragment slug is the literal `CONTEXT`.
- Find fragments in both locations:
  - `.grill-context/` at the root — `rg --files --hidden -g '.grill-context/*.md'`. Pass `--hidden`: `.grill-context/` is a dotdir, and default ripgrep skips hidden paths. A fragment `<slug>.<counter>.<uuid>.md` belongs to the context whose slug matches its prefix.
  - legacy colocated — `rg --files -g 'CONTEXT.*.md'`. A legacy fragment belongs to the `CONTEXT.md` in the same directory.

For each context, its fragment set is the union of the matching `.grill-context/` files and any legacy colocated ones. If a slug has fragments but no base `CONTEXT.md`, create the base only after at least one fragment will be folded into it.

## Read Inputs

For each context:

1. Read the base `CONTEXT.md`, if present.
2. Read every fragment in this context's set — `.grill-context/<slug>.*.*.md` (or `.grill-context/CONTEXT.*.*.md` for a single-context repo) and any legacy `CONTEXT.*.md` beside the base.
3. Parse each fragment filename. Dot-dir fragments are `<slug>.<counter>.<uuid>.md`; legacy fragments are `CONTEXT.<counter>.<uuid>.md`.
   - `counter` is a numeric generation, shared across both locations for this context. Sort it numerically, not lexicographically.
   - `uuid` identifies the writer/session, but does not decide precedence.
   - Legacy `CONTEXT.<uuid>.md` fragments (no counter), if present, have no ordering metadata; treat same-term overlap involving them as contested.
4. Parse fragment ops:
   - `+ Term` adds a term.
   - `~ Term` redefines a term and should include `was:`.
   - `- Term` retires a term.
5. Treat `avoid:` and `under:` as optional metadata.

Do not silently ignore malformed ops. If the intended meaning is obvious, preserve it and mention the cleanup. If it is ambiguous, ask.

## Merge Rules

- Apply `+`, `~`, and `-` ops into the base glossary.
- A fragment op beats the base definition.
- Higher generations beat lower generations for the same term. A later generation means the author had the earlier generation available and is intentionally refining or overriding it.
- Two or more fragments in the same generation touching the same term are contested. Do not pick a winner without the user.
- A legacy unnumbered fragment touching the same term as any other fragment is contested.
- For `~ Term`, compare `was:` to the effective definition at that point in generation order. If the underlying meaning drifted materially, treat it as contested.
- File terms with a valid `under:` hint into that section.
- Put terms without a clear home into the best existing section when obvious; otherwise ask the user.
- Preserve the `CONTEXT.md` contract: it is a glossary only, not a spec or implementation notes file.

## Already In The Base

A fragment op is often a **no-op** because the base already reflects it — that is exactly how legacy fragments accumulate on disk unremoved. An op that changes nothing is still an op that has *taken effect*; it does not exempt its fragment from deletion. Judge each op that touches a term already present in the base:

- **Satisfied** — the base already expresses the op's intent. For `+`/`~`, the base holds an equivalent-or-fuller definition (for `~`, compare against the op's *new* definition, not its `was:`); for `-`, the term is already absent. A satisfied op counts as **applied**: write nothing, but do not let it block deletion.
- **Base older/thinner** — the base predates the op and misses its contribution. Fold normally (the op beats the base); the fragment is then applied.
- **Ambiguous** — you cannot tell whether the base already covers the op or diverged from it. Contested: keep the fragment, surface it, decide with the user.

**Anti-regression guard:** a satisfied-check must never rewrite the base to an *older* fragment's wording. When the base already carries a newer, polished definition (folded from a higher generation) and a lower-generation fragment repeats an older phrasing of the same term, the op is **satisfied** — delete the fragment and leave the base untouched. Never let a stale low-generation fragment clobber a newer base definition.

## Glossary Format

Keep base files in this shape:

```md
# {Context Name}

{One or two sentence description of what this context is and why it exists.}

## Language

**Order**:
One or two sentences defining the term.
_Avoid_: Purchase, transaction
```

Rules:

- Be opinionated: pick one canonical term and list rejected synonyms under `_Avoid_`.
- Keep definitions to one or two sentences.
- Define what the term is, not what it does.
- Include only project-specific domain language.
- Use subheadings under `## Language` only when natural clusters exist.

## Reconcile ADR Numbers

ADRs are named `NNNN-slug.md` (four-digit sequence) and live in `docs/adr/`. Because creation picks the next number naively (`max+1`, no locking), parallel agents and teammates can land two ADRs on the **same** number. This pass detects those clashes, re-sequences, and repairs links.

Process each `docs/adr/` directory **independently** — the system-wide one plus any per-context ones (find them via `CONTEXT-MAP.md` and by searching `rg --files -g 'docs/adr/*.md'`). Each directory has its own sequence.

For each directory:

1. **Detect clashes.** Parse every `NNNN-slug.md` filename and group by `NNNN`. A group with two or more files is a clash. If there are no clashes, this directory needs nothing — leave it untouched.
2. **Pick the keeper.** Within a clashing group, the **chronologically older** ADR keeps the number. Determine age by git add-date (`git log --diff-filter=A --format=%at -1 -- <file>`); fall back to file mtime when a file is untracked or git is unavailable. The keeper is never renamed.
3. **Renumber the losers — minimally.** Every other file in the group is a loser. Give each loser the next free number = current `max(NNNN across the directory) + 1`, in keeper-age order (oldest loser first), incrementing `max` as you assign. Do **not** fill gaps and do **not** compact non-colliding ADRs to a contiguous run — gaps are harmless, but every rename risks breaking a link. Touch only the losers. (Example: a clash at `0003` with `0004` and `0005` already present sends the loser to `0006`.)
4. **Rename the loser file** from `NNNN-slug.md` to `MMMM-slug.md` (slug unchanged) with `git mv` when tracked.
5. **Fix the links** to each renamed loser, repo-wide (search beyond `docs/adr/` — ADRs get cited from CONTEXT files, code comments, READMEs):
   - **Filename / path links** (`[...](NNNN-slug.md)`, bare `NNNN-slug.md`): the slug disambiguates which ADR is meant, so rewrite the number to `MMMM` unambiguously.
   - **Bare `ADR-NNNN` references** (the `superseded by ADR-NNNN` style): after a clash these are ambiguous — `ADR-NNNN` could mean the keeper or the bumped loser. Resolve **best-effort** using context (proximity to the loser's slug or title, which document the reference sits in, what the surrounding text discusses) and rewrite the ones that clearly point at the loser to `ADR-MMMM`.
   - **Report every bare-`ADR-NNNN` rewrite** you made (file, line, old → new, and the cue you used) so the user can audit the guesses, and call out any bare references you left alone because the target was genuinely unclear.

Do not invent ADR content, merge ADRs, or change their bodies beyond the cross-reference fixes above. This pass only reconciles numbers and links.

## Nullify Superseded ADRs

A superseded ADR's rationale now lives in the ADR that replaced it. Keeping its full body in the tree means every reader — LLM or human — burns context on a dead decision before discovering it was retired. This step reduces each superseded ADR to a **stub**: frontmatter, title, and the forward-pointing blockquote, nothing else. The full body stays recoverable from git history.

Run this **after** [Reconcile ADR Numbers](#reconcile-adr-numbers), never before — nullify trusts the `ADR-NNNN` pointer, and reconcile is what makes that pointer correct. Process each `docs/adr/` directory the same way.

The stub is exactly:

```md
---
status: superseded by ADR-NNNN
---

# {original title, unchanged}

> **Superseded by [ADR-NNNN](NNNN-slug.md):** {the existing summary, verbatim}
```

Cut everything after that blockquote. Keep the frontmatter, the title heading, and the blockquote **verbatim** — this pass deletes, it never authors.

**Gut only when all four hold:**

1. Frontmatter carries `status: superseded by ADR-NNNN`.
2. `ADR-NNNN` resolves to a real file in this directory.
3. A `> **Superseded by ...**` blockquote is present.
4. The body still exceeds the stub (already-stubbed ADRs are a **no-op** — skip them; this is what makes the pass idempotent).

**Otherwise, contested — do not cut, surface it:**

- **Dangling pointer** — status cites `ADR-NNNN` but no such file exists (typo, superseder never landed, or a stale number reconcile didn't repair). Report it; leave the body intact.
- **No summary** — marked superseded but no blockquote. A stub with neither body nor forward summary is worse than the status quo. Tell the user to add a summary blockquote (or that you left it); never synthesize one yourself.

Report every ADR stubbed (which one, pointing where) and every one held back as contested with the reason.

## Finish

After all conflicts are resolved:

1. Update the base `CONTEXT.md`.
2. Delete every fragment whose ops are all **applied** — folded this pass *or* already satisfied by the base (see [Already In The Base](#already-in-the-base)) — wherever they live: `.grill-context/` and legacy colocated alike. A fragment that changed nothing because the base already covers it is still done; delete it. Remove `.grill-context/` itself if it ends up empty.
3. Apply the ADR renumbering: the loser renames (`git mv`) and every link edit they required.
4. Apply the ADR nullifications: the body cuts for every superseded ADR that passed all four gut conditions.
5. Inspect `git status --short` and identify only the artifacts produced by this consolidation pass — the base `CONTEXT.md` files, folded fragment deletions, ADR renames, ADR link edits, and stubbed ADR bodies.
6. Stage exactly those files. Do not stage unrelated existing work.
7. Commit immediately. First **sample the repo's house style** — `git log -5 --format='%s%n%b'` — and infer the prevailing convention: conventional-commits `type(scope): subject` or not, subject capitalization, and any trailer (e.g. `Co-Authored-By`). Compose the message to match that grammar and reproduce the trailer convention. Infer the *grammar*, don't copy the sampled type/scope verbatim — a skewed window (say five `fix(...)` commits) must not relabel this docs/maintenance pass. When the repo uses conventional commits, default to `docs(context): consolidate glossary fragments` (append `+ reconcile ADR numbers` when this pass renumbered any ADRs, and `+ stub superseded ADRs` when it nullified any); when it has no discernible convention, fall back to `Consolidate context fragments`.
8. Show the user the commit hash, the changed files, any terms that were added, redefined, retired, or manually resolved, any ADRs renumbered (old → new) with the link rewrites that followed, and any ADRs stubbed (pointing where) or held back as contested.

Never delete a fragment with an unresolved op (contested, or only partially applied). A fully **satisfied** fragment is applied — delete it; do not mistake "changed nothing" for "not applied."
Never commit if any folded output is ambiguous, contested, only partially applied, or mixed with unrelated edits that cannot be staged separately.

**Success invariant:** when this pass commits, no fragment remains on disk except ones deliberately held back as contested. If any non-contested fragment survives — including one the base already covered — the pass is not finished.
