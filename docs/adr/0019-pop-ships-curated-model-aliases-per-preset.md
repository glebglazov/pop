---
status: accepted
---

# Pop ships curated model aliases per preset, not live listings

The recognized-agent catalog (`pop tasks agents`) shows, for each **Agent preset**, a short curated list of model aliases Pop ships in its binary — recommended value first — alongside the preset's binary and PATH availability. The list is a suggestion surface for filling a **Task agent**'s `--model`; it is advisory only and never gates **Manifest** validation. Each preset carries one such list, ordered most-recommended first; the concrete entries live in the code and are not fixed here. There is no live model listing and no model-source provenance: the curated alias list is the only mechanism, exposed to adapters as a plain `[]string`.

This supersedes the live model-source approach introduced earlier (the `opencode models` live listing and the `live`/`known aliases`/`empty` provenance abstraction), which is removed.

## Why

The point of surfacing models is to let a planner pick an appropriate one per task — a heavy refactor onto a strong model, a mechanical edit onto something cheap. Picking wants a short, ranked shortlist. A live listing produces the opposite: `cursor-agent`, `opencode`, and `pi` each report dozens of provider/version rows (every `-fast`/`-high`/`-xhigh` variant, every provider an account can reach). An exhaustive dump is noise at the moment of choosing, so the listing mechanism worked against its own purpose.

Curated also buys a single, honest philosophy. With every preset shipping a hand-picked list, three provenances collapse to one and there is nothing to render about *where* a list came from — so `AgentModelSource`, `AgentModelProvenance`, the model-source provider interface, the empty fallback, and the live-command helper all become ceremony with no surface, and a four-type abstraction flattens to one `[]string`. Two of five presets (`codex`, and historically `cursor`/`pi`) had no usable list source anyway, so "live where possible" would have left a permanently mixed surface.

Advisory, not gating, because the lists rot. Only `claude`'s aliases auto-resolve to the latest model; every other entry is a pinned version ID that ages as new models ship. A rotting list is a tolerable cosmetic staleness on a display column, but weaponising it into Manifest validation would turn it into a gate that rejects valid new models the day they release — exactly when a planner most wants them. So validation continues to check only the preset's first token (per ADR-0018); the `--model` value is never checked against the curated list.

## Considered Options

- **Live listing where the CLI supports it (`opencode`, `cursor`, `pi`), empty elsewhere.** Rejected: produces long, account-dependent dumps that defeat picking, and leaves a permanently mixed surface because `codex` has no list command. This is the approach being superseded.
- **Curated for the empty presets, keep `opencode` live.** Rejected: preserves a mixed philosophy (two provenances) and keeps `opencode` — the most multi-provider, noisiest source — as the one exhaustive dump, which is the worst place for it.
- **Gate Manifest validation on the curated list.** Rejected: a pinned list that rots becomes a gate rejecting valid new models on release; the planner's blanket escape hatch (any `--model` the agent CLI accepts) must keep working.
- **No model surface at all.** Rejected: a planner then has no in-tool hint of what to pass, pushing model knowledge entirely into out-of-band memory.

## Consequences

The curated lists are maintenance debt: as models change, the pinned non-`claude` entries go stale and must be hand-updated, and there is no automated check that they remain valid (by design — see the advisory decision). The recommended-first ordering is the only machine-readable signal of preference; a future planning workflow that auto-fills a **Task agent** reads list order to choose. Reintroducing a live source later means restoring the removed mechanism from history rather than extending what remains.
