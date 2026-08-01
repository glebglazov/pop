# AGENTS.md

Guidance for coding agents working in this repository. `CLAUDE.md` is a symlink
to this file.

## Working in this repo

Verify with one chained call: `go build ./... && go vet ./<pkg>/... && go test
./<pkg>/...`; `make test` is the whole-tree gate. Package map, the oversized
files to read in ranges, and where agent-CLI wire facts live:
`docs/agents/navigation.md` — read it before grepping for a symbol.

## Agent skills

### Issue tracker

Issues, specs, and task sets live in pop's per-machine Work store. No repo
tracker doc by design — each machine resolves its own store via
`~/.agents/docs/issue-tracker.md`.

### Domain docs

Single-context: `CONTEXT.md` at the repo root plus `docs/adr/`. See
`docs/agents/domain.md`.
