---
status: accepted
---

# Pop's config dir holds only hand-authored files; pop-authored assets and state live in the data dir

## Context

Two pop-authored files sit in `${XDG_CONFIG_HOME:-~/.config}/pop`. The **Work store doc** is seeded there create-if-absent and never overwritten ([ADR-0136](0136-planning-skills-publish-through-a-work-store-seam.md)), which priced staleness as "the user's prerogative"; the doc encodes pop's CLI contract (register flags, manifest schema, status vocabulary, storage layout), so staleness is not a preference but a wrong answer — `cmd/work-store.md` has one commit ever and already contradicts [ADR-0147](0147-managed-worktrees-are-provisioned-eagerly-at-the-operator-s-request.md) by describing managed worktrees as lazily provisioned. And `PersistRepoTrunk` (`config/repo_trunk.go:20`), reached only by `pop tasks register --managed --trunk`, writes `trunk = true` into the user's `config.toml` by round-tripping the whole document through `map[string]any` and re-marshalling — destroying comments, ordering, and inline-table formatting in a file the user (or their dotfile manager) authored.

[ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md) already states the user-first precedence law: hand-authored config beats runtime-generated config at any scope. What was missing is its locational twin — *where* each kind of file lives.

## Decision

- **The config dir is hand-authored-only.** `${XDG_CONFIG_HOME:-~/.config}/pop` holds only files a human writes. Pop may write there **if** the user knowingly asks it to, from a command whose entire purpose is editing config on their behalf (an interactive config helper). No such command exists today, so today pop writes nothing there. Everything pop authors — **Shipped asset**s and runtime-generated config — lives under `${XDG_DATA_HOME:-~/.local/share}/pop`.
- **The Work store doc becomes a Shipped asset**, at `${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md` (flat, not under `integrations/`, which is keyed by agent and component). Integration refresh **rewrites it whenever its bytes differ** from the embedded copy — no revision marker, no ownership tracking, no opt-out, since content is a pure function of the binary. `seedWorkStoreDoc` becomes a write-if-different rewrite. This amends ADR-0136: the doc tracks the binary, and staleness is no longer anyone's prerogative.
- **The machine-global override is removed.** Work store doc resolution stays two-layer: the repo-level `docs/agents/issue-tracker.md` wins when present; otherwise the Shipped asset. A user wanting different publish behaviour writes the repo doc, where the choice is versioned and visible to the team.
- **The seeded config-dir copy is deleted** on Integration refresh, unconditionally, with an **Integrate outcome line** naming the removal.
- **Trunk persistence moves to the runtime tier.** `--trunk` writes `trunk = true` into `config.runtime.toml[<checkout-path>]` (data dir, `config/merge.go:29`) through the existing runtime writer, instead of the user's `config.toml`. Hand-authored `[repo."<path>"] trunk = true` remains legal and still wins, by ADR-0083's existing ordering — no new resolution layer. Resolving `trunk` itself reads hand-authored layers 1–4 plus runtime layer 5 only, **never layer 6**: layer 6 is the trunk-anchored runtime entry, so consulting it would define the key in terms of itself. This amends ADR-0147's choice of persistence target, not its eager-provisioning semantics.

## Considered Options

- **A `.chezmoiignore` entry** (the symptom that surfaced this: an `exact_` dotfile dir kept deleting pop-written files, and a re-marshalled `config.toml` failed to parse). Rejected — it hides one machine's collision and leaves both misplacements in place.
- **Keeping the doc as config, seeded and never overwritten** (status quo). Rejected: the doc is a statement about the binary, and the ADR-0147 drift is that failure already realized.
- **Keeping a machine-global override under an opt-in name** (`work-store.override.md`, read-if-present, never written by pop). Satisfies the principle, and was rejected only because the capability is unused — the one seeded copy in existence is byte-identical to the embedded default. Re-addable later without disturbing anything decided here.
- **Delete the stale config-dir copy only when it is byte-identical to the shipped doc.** Rejected: pop knows only the *current* embedded bytes, so an unedited copy from an older revision is indistinguishable from an edited one and would linger forever — reintroducing the silent-staleness trap.
- **Trunk state in `repos/<repo-id>/repo.json`.** Rejected: `trunk` is a repo-scope *config* key with a defined precedence position, and `repo.json` sits outside the merge chain, so every reader would have to consult two sources of truth.
- **Dropping trunk persistence entirely** — `--trunk` prints a config block for the user to paste. Rejected: worst UX for the one flow (bare repos) that needs the flag at all.

## Consequences

- The path is quoted in four skill bodies (`to-tasks`, `to-spec`, `wayfinder`, `setup-matt-pocock-skills`), in `CLAUDE.md`, and in the doc's own opening paragraph, which also describes the never-overwrite contract it no longer has. All are rewritten together.
- The Work store doc's managed-worktree paragraph is corrected to ADR-0147's eager provisioning in the same change — shipping the relocation while leaving a known contradiction in the shipped bytes would undercut its rationale.
- `PersistRepoTrunk`'s whole-document re-marshal disappears with it. That round-trip is what mangled a user's `config.toml` into array-of-tables form and exposed a decoder gap in `TopicSteps.UnmarshalTOML` (fixed independently in `af4cb65`).
- A user who *had* edited the seeded doc loses those edits; the outcome line is the only notice, and recovery is to write the repo doc.
