# Workload target references resolve from the current working directory

> Superseded by [ADR 0012](./0012-issue-sets-live-in-pop-data-dir-keyed-per-repository.md): path-form targets removed; targets are bare identifiers resolved via repository identity from the CWD.

Run issue, Run issues, and Issue reset accept positional **Workload target references** only. Run issue and Run issues each accept an optional positional path override; Issue reset requires a positional issue path. These commands do not expose `--issue-set` or `--issue` targeting flags. Pop normalizes every accepted reference to canonical Issue set and issue identifiers before selection. Resolved paths must match discovery under the command's workload definition path.

Positional targeting is path-only. Run issues accepts a CWD-relative path to a discovered Issue set directory, such as `thoughts/issues/<id>` or `.` when the shell is already inside the directory. Run issue and Issue reset accept a CWD-relative path to an issue markdown file beneath a discovered Issue set, such as `thoughts/issues/<id>/<file>.md`. A bare filename is accepted when it resolves from the current directory to such a markdown file. Bare **Workload identifiers**, absolute paths, titles, prefixes, fuzzy matches, unresolved paths, and other non-relative forms are rejected.

Rejection messages have two tiers. When input is not a relative path form, the error explains that a relative path is required. When a relative path fails to resolve, the error lists valid **Workload identifiers** only, not example paths.

Shell completion for Run issue, Run issues, and Issue reset positional arguments offers CWD-relative path segments only, including `./` and `../`; it does not offer bare identifiers. Set-priority remains unchanged: its `ISSUE_SET` positional argument accepts and completes bare Issue set identifiers.

The **Workload status table** prints failed-issue reset hints in the canonical copy-paste form `pop workload reset-issue thoughts/issues/<id>/<file>.md`, relative to the workload definition root.

## Why

Developers often target workload work from editor paths or tab completion copied from the repository tree. Path-only positional targeting makes those commands predictable: their syntax matches the artifact being selected and avoids a separate flag vocabulary.

Anchoring resolution to the current working directory matches other pop path flags and fits the common case of running workload commands from a project checkout root. Requiring paths to be relative to the workload definition path would break when the shell is elsewhere, and would duplicate the meaning of `--workload-definition-path` in a confusing way.

Discovery match remains mandatory so relative paths are ergonomic sugar for known Issue sets, not a way to execute arbitrary manifests outside the active workload.

## Considered Options

- **Definition-path anchor.** Rejected: paths would ignore the shell directory, surprise users who pass tree paths from their checkout, and blur the boundary between "where artifacts live" and "how I point at them from the CLI."
- **Definition path first, then CWD fallback.** Rejected: two anchors make errors harder to reason about and scripts non-deterministic without an explicit `--workload-definition-path`.
- **CWD anchor with discovery match (chosen).** Accept a relative path that resolves from the shell to a discovered Issue set or manifest issue of the kind expected by the command; reject paths outside that discovery.
- **Trust any directory with a valid manifest.** Rejected: would bypass registration, priority, and workload state keyed by definition path.

## Consequences

Workload commands need a shared resolver that canonicalizes CWD-relative paths and maps them onto discovered Issue sets and manifest entries. Run issue, Run issues, and Issue reset deliberately reject bare identifiers; Set-priority continues to use a bare Issue set identifier.

Shell completion for run and reset positionals must return path segments without mutating workload state. Set-priority completion remains identifier-based.

Scripts for run and reset commands must pass relative paths and become CWD-dependent; CI and automation should `cd` to a known directory before invoking them. Set-priority scripts that pass bare identifiers continue to work unchanged.

Future team-shared workload definitions do not change this decision: the anchor is how CLI input is interpreted, not where artifacts are stored.
