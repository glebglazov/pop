---
status: accepted
---

# Task transfer is a batch, recency-ordered, atomic snapshot operation

`pop tasks transfer export`/`import` moved exactly one **Task set** per archive, with import enforcing "exactly one top-level directory." Users routinely want to move several sets between machines or repositories in one go, and doing it one archive at a time is tedious and easy to get half-wrong. We extend transfer to carry N sets in a single archive while keeping single-set transfer working unchanged.

## Decision

**Unify the archive format around "one or more top-level set directories."** A transfer archive holds one top-level directory per **Task set**, each named for that set's **Task identifier** and mirroring its **Task storage** layout. A single-set export is simply the N=1 case, so every archive ever produced by the old single-set path still imports without special handling. There is no wrapper manifest and no embedded ordering or priority metadata — this preserves the existing invariant that **Task set export** is a faithful filesystem snapshot of artifacts, not a carrier of the repository's **Task state**.

- **Export takes N identifiers.** `pop tasks transfer export A B C` writes all three sets into one `tar.gz`. Repeated ids are deduped. Default output is `<id>.tar.gz` for a single set (unchanged) and `pop-tasks-<YYYY-MM-DD-HHMM>.tar.gz` for multiple; `-o` overrides. Export is atomic: any **Missing** requested id fails the whole export and writes nothing.
- **Import installs all-or-nothing.** Import extracts every top-level set to a temp location, validates each against the task contract, and installs only if *every* set is well-formed and *every* target identifier is free after disambiguation. Any **Malformed** set or any still-unresolved identifier collision rejects the entire archive; nothing is written. Each set installs under its own directory name with the same `YYYY-MM-DD` / `YYYY-MM-DD-HHMM-<slug>` disambiguation as task-set creation, and each is **registered** at priority `0`, appended in identifier order.
- **`--as` stays single-set-only.** `--as <id>` renames the imported set, so it is meaningful only when the archive holds exactly one set; it is rejected for a multi-set archive.
- **Export completion orders newest-first.** Alone among completion surfaces — which sort task sets alphabetically — export completes N identifiers ordered newest-first (reverse **Task identifier** sort, exploiting the chronological id prefix) and excludes ids already on the command line, matching the recency-driven "export the set I just made" workflow.

## Considered options

- **A second, wrapper archive format for multi-set** (a top-level manifest listing members, order, priority) — rejected. It would let the archive carry registration metadata, but that directly contradicts the established rule that export is an artifact snapshot, not a copy of **Task state**. Unifying on "N top-level dirs" needs no format sniffing on import and keeps the single-set archive as a strict subset, so old archives keep importing.
- **Best-effort import** (install the sets that fit, skip and report collisions/malformed ones) — rejected. Friendlier for large archives, but it leaves a partial result and breaks the single-set "never partially apply / nothing is written" guarantee that import already makes. The chronological-prefix disambiguation already resolves the overwhelming majority of collisions automatically, so hard rejection is rare in practice; keeping it atomic preserves one predictable mental model.
- **Require `-o` for multi-set export** (no default filename) — rejected. Explicit, but adds friction to the common case. A synthesized `pop-tasks-<timestamp>.tar.gz` always works and is overridable; the timestamp keeps repeated exports from clobbering each other.
- **Keep export completion alphabetical like every other surface** — rejected. Transfer is the one task workflow driven by recency rather than schedule position; surfacing the just-created set first is worth the single deliberate deviation from the otherwise-uniform alphabetical ordering.

## Consequences

- Export changes from `ExactArgs(1)` to `MinimumNArgs(1)`; its completion diverges from the shared alphabetical enumerator to a reverse-sorted, command-line-aware one.
- Import relaxes its "exactly one top-level directory" check to "one or more," applying the per-entry traversal/absolute-path validation to each; the extract → validate → install pipeline gains a loop and a whole-archive commit point.
- `--as` grows a guard that rejects it against multi-set archives.
- The single-set paths (default `<id>.tar.gz`, single-directory archives, existing archives on disk) all keep working as the N=1 case — no migration.
