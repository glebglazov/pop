# Artifacts are ordered by type tier, not by recency

[ADR-0217](0217-artifacts-are-a-second-list-seam-on-the-work-detail-view.md) gave a Task set's artifacts "one total order, newest first, over a family that does not timestamp itself", reasoning that a review dates itself in its file name while `spec.md` and `progress.txt` carry only a modification time, and that one comparable instant per row was the cheapest thing that could work. It works, and it produces a list whose top row is almost always `progress.txt` — because a drain rewrites that file at every transition, so the one document a reader least wants sits above the ones they came for, and the row under the cursor moves whenever the drain writes.

The decision: **a Task set's artifacts are ordered by a type tier — every Review artifact first, newest-first among themselves, then `spec.md`, then `progress.txt` — and recency orders only within the review family.** The tier lives in `tasks.Artifacts`, the one function both the dashboard's Artifact view and `pop tasks artifacts` read, so the two surfaces cannot disagree.

The tier is not a display preference; it follows from what the timestamps mean. A review's instant is the moment a document was *written about a particular tree*, which is the fact a reader is choosing between when three reviews sit in the list. A modification time on `spec.md` or `progress.txt` says when pop last touched the file, which is a different question and not one anybody asks — there is only ever one of each, so ordering them against anything is answering a question with no stakes. Sorting a key that means two different things in one list was the mistake, and the fix is to stop asking the singletons to compete.

## Considered options

- **Special-case `progress` to sort last and leave the rest on recency** — rejected. It fixes the symptom and leaves `spec.md` free to float above or below the reviews by mtime, which is the same defect with a smaller blast radius.
- **Keep the total order and re-anchor the cursor after a refresh** — rejected. It keeps the list correct-looking while the underlying order stays wrong, and every future surface reading `Artifacts` inherits the wrongness.
- **Let each artifact type declare a sort key** — rejected as generality with no second customer. Three types, one tier order, stated once.

## Consequences

- **Amends [ADR-0217](0217-artifacts-are-a-second-list-seam-on-the-work-detail-view.md).** Its "one total order, newest first" clause is superseded; the rest of that ADR — artifacts as a second list seam, the closed known list, the silent no-op for a kind that publishes none — stands untouched.
- **The list is positionally stable.** A drain writing a progress block no longer moves the row under the cursor, which is what makes the Artifact view usable while a set is draining rather than only after.
- **`ArtifactSummary.NewestType` now means "first row", not "newest document".** It reads the head of the ordered list, and the head is now a tier position. The summary block says "newest: review" on a set whose progress record is seconds old, which is the honest answer to what the view will show.
- Glossary: **Artifact view** is redefined.
