# A convention always answers, and a recipe is never a rank

[ADR-0223](0223-a-convention-resolves-to-one-answer-plus-the-overlay.md) made the **Convention recipe** the last rank of the stack so that a kind nobody had answered still resolved and `pop conventions get` always exited 0. The guarantee was right; the mechanism put a *method* in the slot a consumer reads an *answer* out of.

Two things fall out of that, and both were observed rather than predicted. A consumer that must branch on a banner will read only far enough to see the banner: a session in another repository re-read a resolved convention with `head -6` — enough to watch `METHOD` flip to `ANSWER` and nothing more — and so never reached the **Convention overlay**, which renders last because it appends. And a consumer that *cannot* branch gets a method anyway: `StackProse` always speaks, so `reviewer.tmpl.md`'s `{{if not .ConventionRecorded}}` arm is dead code, and a repository with no written standard hands the **Reviewer** the `code-review` recipe under the heading "This is the standard to hold the changeset against" — a method whose closing step is to write a file, given to an agent that ADR-0221 forbids from writing files.

The deeper error is a word doing two jobs. "How commit messages are made here" *is* a procedure, so a convention is always method-shaped; what does not belong in the same channel is the different method of *deriving* the convention. Conflating them is what forced the banner, and the banner is what taught readers to peek.

The decision, in six parts:

1. **`get` always returns rules to follow.** The recipe is no rank of the stack. Beneath the written ranks sits a **Shipped convention**: pop's own answer for the kind, embedded, and displaced whole by any written rank above it. The always-resolves guarantee is unchanged and strengthened — not merely "always resolves" but "always resolves to something followable" — and the `METHOD`/`ANSWER` banner, and every consumer's branch on it, is deleted.

2. **Pop ships an answer, and that is not a house style.** A Shipped convention is generic by construction, because pop is a work orchestrator and cannot know a project's taste: the `commits` entry says to read the recent log and match it, carrying the discard-pop-generated-commits guard that stops pop learning its own accent back. Generic derivation guidance living *inside* an answer is correct — it is the honest answer when nobody has stated a better one — and a team's document displaces it whole rather than arguing with it. This overrules the `code-review` recipe's own "pop does not ship a house style": pop already asserted defaults (a fallback commit subject, a shipped tracker doc, a smell baseline reaching the Reviewer through a slot meant for a standard). Labelled and last-ranked is better than the same content arriving by accident.

3. **Five ranks, named by author and scope.** Exactly one of the first four answers; the overlay is appended whenever it exists.

   ```
   user project   ~/.agents/docs/projects/<slug>/<kind>.md   yours, this project
   user global    ~/.agents/docs/<kind>.md                   yours, every repository
   repository     docs/agents/<kind>.md                      the team's, in version control
   shipped        built into pop                             pop's own, displaced by any above
   user overlay   ~/.agents/docs/<kind>.overlay.md           yours, appended to whichever answered
   ```

   "Defaults" is retired as a rank word: it named the human's global document while the new bottom rank wanted the same word, and a reader an hour into the design read `--defaults` as "override pop's built-in". **Convention defaults** becomes **Global convention**.

4. **The new top rank is the human's, per project, outside the repository.** A **Project convention** is keyed by the git remote — `github.com-tripledot-github_dashboard` — not by **Repository identity**, and this is the one place pop keys a repository by something other than that hash. The reason is that the subject differs: a store, a binding and a config override are about *this machine's checkout*, for which a moved repo genuinely is a new subject, whereas a convention document is about the project as a thing that outlives any one clone. Remote-keying is also derivable with no stored state, which a human-chosen name would not be. A repository with no remote falls back to the identity-keyed path.

5. **Pop memory is retired, and no agent writes a convention rank.** Memory existed as pop's stand-in for a written answer; a Shipped convention is that stand-in, done better, so the rank has no job left. It also had a defect no prose guard survives: an agent working a recipe's "write the result down" step wrote into the lowest rank a sentence asserting the human's overlay "does not apply to this repository's code commits" — the bottom of the stack countermanding the top. Removing the rank is structural where forbidding the sentence would have been a request. Existing memory files are simply never read again; nothing deletes them, because a rank that stops being consulted needs no migration code. Rank 0 is what a human reaches for when they want to override everything for one project, which is what memory was being used for.

6. **A write names its rank; nothing defaults.** `pop conventions set <kind> --project | --global | --overlay` (body on stdin), `unset` mirroring it, bare forms refused with the list. `--repository` is accepted and refused with the path, because a git-tracked team document should land through a diff a human reviews. `Set`, `SetOverlay`, `ClearOverlay` and the memory writer collapse into one rank-parameterised writer with one empty-body refusal — less code than today — and `ErrNoDerivation` with the `derived_from`/`derived_at` frontmatter goes with memory, all three writable ranks being the human's own statement. `pop conventions recipe` becomes `pop conventions default <kind>`, printing the Shipped convention so a human can base their own on it: the customise flow is a human asking, never a machine telling a machine to derive.

## Consequences

- The **Config dashboard**'s convention rows become read-only previews naming both writable paths. A convention is a document, not a value, and editing one from a list-of-addresses TUI is a different act from flipping a `key = value` line; with two writable human ranks, one `enter` key could not serve both without hiding the higher-stakes one. `storeConvention`, `conventionEditSeed` and `copySourceConvention` in `confighost/conventions.go` go, and the ctrl+y refusal is moot.
- The shipped `issue-tracker` symlink at `~/.agents/docs/issue-tracker.md` is retired: pop's tracker doc becomes the `issue-tracker` Shipped convention instead of squatting in the human's own rank, where it outranked the team's committed document. `pop integrate` removes the link only while it still points at pop's asset — the same non-clobbering rule that created it. Sequence this before anything can write `--global`.
- The `issue-tracker` recipe disappears entirely: its step 1 was "re-run `pop integrate` so pop installs its own answer", which is this ADR with extra steps.
- The provenance line reduces to origin and path plus the overlay clause; `memoryDerivation()` goes.
- Kind names are unchanged. `docs/agents/issue-tracker.md` is read by third-party skills under that exact name, and `code-review`/`verification` match pop's own step nouns, so a kind name stays an address whose virtues are stability and agreement with the rest of the glossary.
