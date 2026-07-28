# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Agent skills

### Issue tracker

Issues, specs, and task sets live in pop's per-machine Work store. No repo
tracker doc by design — each machine resolves its own store via
`${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md`.

### Domain docs

Single-context: `CONTEXT.md` at the repo root plus `docs/adr/`. See
`docs/agents/domain.md`.
