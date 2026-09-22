---
status: accepted
relates: "extends [ADR-0145](0145-package-internal-test-parallelism-rides-per-test-deps-not-process-env.md) from the store to the config layers; found by the grading failure that [ADR-0278](0278-an-objective-gate-the-parent-tree-fails-does-not-score-a-trial.md) records"
---

# Tests reach pop's config files only through injected Deps, and a guard enforces it

## Context

The first graded Trial failed on two `config` tests that read the grading
machine's own `config.override.toml`. The tests passed or failed with the
developer's files, not with the code. ADR-0145 moved store isolation onto a
per-test `Deps`, but config reads had no such rule, and four paths reached the
real files:

- code under test called the production edge — `config.Load(DefaultConfigPath())`
  or `config.DefaultDeps()` — although it held injected `Deps`;
- a caller that held `Deps` handed an injected loader a path it computed from
  the process environment, so the load ran through the test's `Deps` but opened
  the developer's file;
- the package-level tmux handles load `config.toml` at init, so every test
  binary that links them read it;
- `queuetest.TasksDeps` kept any `XDG_DATA_HOME` already set, and could not
  tell a test's value from the developer's.

## Decision

**1. A function that holds Deps reads config through them.** It resolves the
default path and every layer — `config.toml`, its includes, the override —
through the same `Deps`: `config.LoadWith`/`LoadDefaultWith`,
`tasks.Deps.ConfigDeps()`, the `cmd` layer's `loadConfig`/`defaultConfigPath`,
and `integrate.DefaultDepsWith`. `config.Load`, `DefaultConfigPath` and
`DefaultDeps` stay the production edge only.

**2. A guard makes a leak fail.** Under `go test`, `guardTestConfigFile` panics
when a test reads or writes a file inside the real pop config dir or data dir,
both recorded at package load, before any `t.Setenv`. Only file access trips
it: a default path computed for a loader that ignores it opens nothing.

**3. Loads at process init skip the machine config under `go test`.**
`ConfiguredTmuxSocket` and `ConfiguredTmuxInclude` answer with the unset value,
so a test binary never aims its default tmux handle at the developer's server.

**4. An inherited `XDG_DATA_HOME` is not isolation.** The drain, supervisor and
dashboard tests still isolate through the process environment (ADR-0145 skipped
that slice). Their helper treats the value the test binary inherited as the
developer's and replaces it; it keeps only a value a test set.

## Considered Options

- **Set `HOME` and the XDG dirs in `TestMain` or in `make test`.** Rejected: it
  is process-env isolation, which ADR-0145 rejects; a direct `go test ./pkg`
  still leaks; and the drain tests failed when the XDG dirs were set.
- **Panic when a default path is computed.** Rejected: many tests compute one
  for a fake loader that never opens it. The guard would force changes where
  nothing leaks.

## Consequences

- A test that reaches a real config file stops with the file's path in the
  panic, not with a result that depends on the machine.
- Tests now see no config where they saw the developer's. This showed that
  `pop tasks archive` refused to run without a `config.toml`; a missing file is
  now the defaults there, as it is for the other commands.
- The suite passes in the real environment, with only `HOME` isolated, and with
  `HOME` and all XDG dirs isolated.
