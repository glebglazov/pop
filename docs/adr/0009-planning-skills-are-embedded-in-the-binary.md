# Planning skills are embedded in the binary

The **Workload planning skills** (grill-with-docs, to-prd, to-issues) and the **Pane skill** are `go:embed`-ed into the pop binary, like the existing pane skill content, rather than fetched from an external registry. A skill's version is therefore the binary's version: shipping a skill fix requires a pop release, and users pick it up on their next binary update via **Integration refresh**.

## Why

The skills previously lived in personal dotfiles, scattered from the tool that consumes their output. Consolidating them into pop demanded a distribution choice. Embedding reuses machinery that already exists and is tested — the per-agent install transforms, the dry-run staleness comparison, and the refresh-on-revision-change path — and keeps installs offline, deterministic, and provably in lockstep with the binary ("which skill version is live?" has one answer: the binary's). The accepted cost is coupling skill freshness to release cadence; a skill-only change becomes a patch release.

## Considered Options

- **Proxy to `npx skills add ...`.** Rejected: adds a network and Node dependency to a problem pop already solves offline, leaves the skills external (moving the scatter from dotfiles to a registry rather than removing it), and decouples skill version from binary version so the two can drift.
- **Embed with an opt-in remote refresh (`--latest`).** Rejected for now: reintroduces drift as an opt-in. Can be revisited if release cadence ever makes the coupling painful.
