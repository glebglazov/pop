---
status: accepted
---

# Agent-integration state lives in a top-level `integrate` package; command families own only their own rendering

## Context

Five files in package `cmd` — `integrate.go` (1,587 LOC), `integrate_outcome.go` (305), `installer.go` (382), `uninstaller.go` (345), `catalog.go` (134) — hold everything behind `pop integrate`, with ~5,950 LOC of tests hanging off `integrateDeps`. Three shapes make the code resist change. First, the catalog models **Integration components**, never agents, so per-agent behaviour is five parallel switch statements (`catalog.go:120`, `uninstaller.go:91`, `uninstaller.go:227`, `installer.go:350`, plus the component-kind switches) that must be edited in lockstep to add an agent. Second, `integrateDeps` (integrate.go:119-176) fuses injection closures with mutable run-state (`changed`, `installed`, `overwrotePaths`, `prunedStale`) and per-invocation mode flags, so "what happened" is read back out of the dependency bag. Third, `cmd/doctor.go` reaches deep into those internals — `doctorDetectAgentIntent` (:809), `doctorComponentState` (:1041), direct iteration of `integrationAgents`/`integrationCatalog` — because there is no boundary to reach *at*. The package has no named Go interfaces; its only polymorphic seam is the `integrationComponent.install` func field. Its outbound deps are just `config`, `debug`, and cobra, and nothing in `tasks`/`config`/`internal` calls in — the boundary is already there in the dependency graph, only unexpressed.

## Decision

- **A new top-level `integrate` package**, not `internal/`: it owns a user-facing command family (like `work` and `queue`), not infrastructure (like `internal/tmux`). The `go:embed` trees move with it — `cmd/skills/`, `cmd/extensions/`, `cmd/work-store.md` — since embed directives cannot cross a package.
- **Injection, intent, and outcome are three types.** `Deps` carries injection closures only (FS/IO/symlink/config). `Request` carries per-invocation intent, including the mode flags `DryRun`, `OverwriteConflicts`, `AssumeYes`, `UpdateExisting`. `Report` is returned, not mutated into `Deps`. Entry points: `Install`, `Remove`, `EnsureForRevision`.
- **An [Agent integration profile](../../CONTEXT.md) per agent, in a registry**, replaces all five switches at once: status-wiring install/remove/detect, skill install roots, legacy artifacts to prune. Func fields in a struct, not a Go interface — five static entries do not need dynamic dispatch. The five status-wiring installers already share one signature, so the table assembles mechanically.
- **Doctor's integrate-family derivation moves in; Doctor's rendering does not.** `DetectAgentIntent` and `ComponentState` become exported package functions; `cmd/doctor.go` keeps **Doctor rendering** and its remediation copy (`doctorComponentFlag`, `integrateInvocation`) and holds only an `integrate.Deps`. The cut follows the glossary's existing **Doctor intent** / **Doctor rendering** line.
- **The Integrate outcome line renders inside `integrate`.** The `Outcome` type, its ordering, and `PrintOutcomes(io.Writer, …)` move together, because **Integrate outcome line** and **Integrate outcome ordering** are integrate-owned glossary terms with a specified format — and **Doctor rendering**'s own entry defers to that format by name. Rendering follows the term that defines it; this is why the boundary is not the flat rule "packages never print".
- **Revision-gated Integration refresh moves in.** `EnsureForRevision(rev)` is exported and cmd injects `buildRevision()` (`cmd/root.go:40`, the only cmd-local dependency); `ensureSystemState` (`monitor.go:297`) stays cmd-side and thin. Work-store seeding rides refresh per [ADR-0136](0136-planning-skills-publish-through-a-work-store-seam.md) — `seedWorkStoreDoc` has exactly one caller, `updateStaleIntegrations`.
- **cmd keeps the cobra surface**: `integrateCmd`, `integrateRemoveCmd`, flag vars, `Args` validation, `Request` construction, and the TTY implementation of `ConfirmOverwrite func(path) bool`. The package never reads stdin and never imports cobra.
- Docs-only for now; code lands later through the task pipeline.

## Considered Options

- **`internal/integrate`** — rejected: internal is pop's home for infrastructure, and this is a command family with its own domain vocabulary.
- **A Go interface for the agent seam** — rejected: ceremony over five static, compile-time-known agents; a descriptor struct with func fields gives the same call sites without the indirection.
- **Unify with `tasks/agent_catalog.go:12`** — rejected: that list is Agent presets, i.e. execution adapters. Same word, different concept; merging them would couple integration wiring to task execution.
- **Leave run-state on `Deps`** — rejected: it is the reason the tests are the size they are. The churn (47 `runIntegrateWith` call sites) is the price of the testability win, and it is confined to one slice.
- **Keep `printIntegrateOutcomes` in cmd** for symmetry with Doctor rendering — rejected, see the decision above; the asymmetry is the glossary's, not an inconsistency.
- **Slice the agent registry per agent** — rejected: the five switches are five views of one missing table, so a partial registry leaves every call site carrying both paths, an intermediate state worse than either end.

## Consequences

- Adding an agent becomes one registry entry instead of five coordinated switch edits.
- `cmd/doctor.go` stops depending on integrate internals; `doctorDeps.integrate` narrows to an injection-only value.
- Test migration is cheaper than the LOC suggests: 47 of `integrate_test.go`'s call sites already drive `runIntegrateWith(d, agent)` and only one goes through cobra, so per [ADR-0144](0144-behavior-tests-live-at-the-domain-contract-and-real-io-sits-behind-seams.md) these are already domain-contract tests and move as a package rename. cmd retains only arg/flag-surface tests; `doctor_test.go` stays but re-points its intent and component-state cases at the exported functions.
- Landing order matters: the file move happens first as a pure package rename, so the `Deps`/`Request`/`Report` split, the profile registry, the Doctor surface, and refresh all land as in-package work with no cross-package churn.
- `cmd/testdata/` needs an audit during the move — integrate-only fixtures follow the package, shared fixtures stay.
