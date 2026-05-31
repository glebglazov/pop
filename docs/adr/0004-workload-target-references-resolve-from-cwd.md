# Workload target references resolve from the current working directory

Workload commands accept **Workload target references** — bare **Workload identifiers** or CWD-relative paths — for `--issue-set`, `--issue`, and positional Issue-set arguments. Pop normalizes every accepted reference to canonical Issue set and issue identifiers before selection. Resolved Issue-set directories must match discovery under the command's workload definition path.

Issue-set paths may be bare identifiers, conventional tree paths such as `thoughts/issues/<id>`, or any other path that resolves from the shell's current directory to a discovered Issue set directory. Issue paths may be bare manifest IDs, Issue-set-relative markdown filenames, or spanning paths such as `thoughts/issues/<id>/<file>.md`. Bare values match manifest **id**; path-like values and `.md` suffixes match manifest **file**. Issue-set-relative issue paths require an explicit `--issue-set` or a spanning path; a spanning path may omit `--issue-set` when it resolves unambiguously. When both flags are supplied, they must agree. Rejection messages list valid Workload identifiers only.

Shell completion stays context-sensitive: bare identifiers by default; path segments and markdown files once the typed prefix looks path-like.

## Why

Developers often target workload work from editor paths or tab completion copied from the repository tree. Requiring bare identifiers forces mental translation from `thoughts/issues/demo/01-a.md` to `demo` and `01-a` on every invocation.

Anchoring resolution to the current working directory matches other pop path flags and fits the common case of running workload commands from a project checkout root. Requiring paths to be relative to the workload definition path would break when the shell is elsewhere, and would duplicate the meaning of `--workload-definition-path` in a confusing way.

Discovery match remains mandatory so relative paths are ergonomic sugar for known Issue sets, not a way to execute arbitrary manifests outside the active workload.

## Considered Options

- **Definition-path anchor.** Rejected: paths would ignore the shell directory, surprise users who pass tree paths from their checkout, and blur the boundary between "where artifacts live" and "how I point at them from the CLI."
- **Definition path first, then CWD fallback.** Rejected: two anchors make errors harder to reason about and scripts non-deterministic without an explicit `--workload-definition-path`.
- **CWD anchor with discovery match (chosen).** Accept any path that resolves from the shell to a discovered Issue set or manifest issue; reject paths outside that discovery.
- **Trust any directory with a valid manifest.** Rejected: would bypass registration, priority, and workload state keyed by definition path.

## Consequences

Workload commands need a shared resolver that canonicalizes CWD-relative paths, maps them onto discovered Issue sets and manifest entries, and preserves today's bare-identifier behavior.

Shell completion must observe the typed prefix to switch between identifier completion and path-segment completion without mutating workload state.

Scripts that relied on bare identifiers continue to work unchanged. Scripts that pass tree paths become CWD-dependent; CI and automation should `cd` to a known directory or keep using bare identifiers.

Future team-shared workload definitions do not change this decision: the anchor is how CLI input is interpreted, not where artifacts are stored.
