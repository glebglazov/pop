# Grill skills are a marked overlay on a pinned copy of Matt Pocock's domain-modeling

Pop's `grill-with-docs` / `grill-consolidate` began as a 112-line self-contained fork of Matt Pocock's then-monolithic `grill-with-docs` (copied at upstream `aaf2453`, 2026-05-31). Matt has since refactored that monolith into `grilling` (the interview primitive) + `domain-modeling` (glossary/ADR discipline, owning the `CONTEXT-FORMAT.md` / `ADR-FORMAT.md`), leaving `grill-with-docs` a 7-line delegator. We restructure pop's copy to track that shape: the base is `grilling` + `domain-modeling` copied **verbatim at a pinned ref**, carrying a provenance header, and pop's parallel-safety additions live **below an explicit `POP OVERLAY` marker** in each file. The one behavioural override is stated as a single line — negating `domain-modeling`'s single-writer "update `CONTEXT.md` right there" instruction in favour of writing a session fragment (the [.grill-context](0089-context-fragments-live-in-a-single-grill-context-dir.md) scheme). We copy rather than reference upstream, per [ADR-0009](0009-planning-skills-are-embedded-in-the-binary.md) — pop ships to machines that don't have Matt's skills installed.

## Considered Options

- **Keep the opaque monolith (status quo):** zero upstream coupling, but drift versus upstream is unknowable without archaeology, and improvements to the `grilling` primitive are never picked up.
- **Live delegation to `/domain-modeling` + `/grilling`:** minimal duplication, but breaks self-containment (ADR-0009) — the skills wouldn't work where Matt's aren't installed.
- **Marked overlay on a pinned verbatim base (chosen):** copy-all preserves self-containment; the marker makes drift a mechanical above-the-marker diff against the pinned upstream; pop's default (fragments) stays authoritative.
- **Separate `*-OVERLAY.md` sidecar files:** cleanest possible base diff, but adds files the skill must stitch together for ~5% more diff-cleanliness than the marker gives.

## Consequences

- Drift review reduces to `diff <pop base-portion> <domain-modeling@newpin>`; the base already matches the vendored `domain-modeling@391a2701` byte-for-byte, so adopting this shape is mostly drawing the marker where the seam already is.
- Pop's cosmetic edits to the `ADR-FORMAT.md` base are reverted to upstream verbatim so the above-marker region diffs clean; all genuinely-pop content moves below the marker.
- `grill-consolidate` and the fragment appendix have no upstream twin — they are irreducibly pop-only and live entirely in the overlay.
- The three post-copy `grilling` refinements (confirmation gate, fact/decision split, anti-self-grill guard) arrive for free when the base is refreshed to the current primitive.
