# Skill install names are a configurable prefix plus a frontmatter ownership marker

The installed name of an embedded skill is `<prefix><base>`, where `prefix` comes from `skill_prefix` in `[integrations]` (default `pop-`, empty for bare names). Because an empty prefix erases the `pop-` name signal that copy-mode installs relied on for ownership, ownership is no longer inferred from the name at all: a symlink is owned when it resolves into pop's render tree (unchanged from [ADR-0011](0011-integration-artifacts-render-to-pop-data-dir.md)), and a real file/dir is owned when its frontmatter carries `pop-owned: true`, written into every rendered skill. The pane skill's base is renamed `pane` → `tmux-pane` so the bare form (`tmux-pane`) stays specific enough to be collision-resistant.

## Why

A user replacing externally-managed skills (e.g. dotfiles) with pop's wants the installed names to match what they already invoke, not a forced `pop-` namespace. Making the prefix configurable delivers that. But ADR-0011 had two ownership signals — the strong symlink-into-render-tree marker and a weaker `pop-` name-prefix fallback for copy-mode — and an empty prefix destroys the fallback. Moving the copy-mode signal into a name-independent `pop-owned: true` frontmatter field restores it for every agent, so the prefix becomes purely cosmetic and bare names are safe everywhere. A plain boolean suffices: rename cleanup is set subtraction (re-render the catalog; prune any owned entry whose name is absent from the freshly rendered set), so no component identity needs to be tracked.

## Considered Options

- **Force `pop-` on copy-mode agents only.** Rejected: leaves ownership detection asymmetric across agents (readlink here, name there) and special-cases opencode in the install path. The frontmatter marker removes the asymmetry for a small, uniform write.
- **Component-identity frontmatter (`pop-component: <id>`).** Rejected as unnecessary: identity would only be needed to *match* a stale install back to a catalog entry, but cleanup never matches — it subtracts names not in the current render set.
- **No marker, reject empty prefix when a copy-mode agent is integrated.** Rejected: blocks bare mode entirely whenever opencode is in play, for no real gain over writing one frontmatter line.

## Consequences

- A config-only `skill_prefix` change does not trip the binary-revision-gated **Integration refresh**. It takes effect on the next explicit `pop integrate <agent>` (which re-renders the requested components unconditionally), or on `pop integrate --update-existing --force` — the `--force` flag bypasses the revision/staleness short-circuit and re-renders + re-links every currently-installed pop-owned component (it never resurrects opted-out components and never overrides conflicts, preserving [ADR-0010](0010-integrate-is-a-per-component-consent-wizard.md)). Either path re-renders under the resolved names and prunes the stale old-named pop-owned entries.
- The marker lives in rendered content, so a user who copies a rendered skill *and* severs the symlink inherits `pop-owned: true`; pop may then reclaim that name. Accepted — the `pop-` prefix carried the same exposure.
- Renaming the pane skill migrates existing `pop-pane` installs to `pop-tmux-pane` (or bare `tmux-pane`) via the same prune-stale-then-link path.
