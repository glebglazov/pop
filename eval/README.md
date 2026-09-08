# Eval

Eval compares the cost and graded quality of multiple ways to complete the same
work. Run it from the repository root with `go run ./eval`.

## Case layout

A Case is a portable directory under `eval/cases/<name>/`:

```text
case.json       repository URL, parent SHA, reference range, gates, scope, standards
spec.md         identical source words given to both arms
acceptance.md   Acceptance list, initially not approved
drafting/       Captured drafting run and its spend record
tasks/
  index.json    Pop arm Task-set manifest
  *.md          Pop arm AFK task files
```

The Case manifest contains no local repository or Task-storage path. A Case can
also be written by hand in this shape. `acceptance.md` must be reviewed and
approved before any Trial runs.

Draft the Acceptance list after Case preparation:

```sh
go run ./eval draft-acceptance <case-name>
```

The command uses the `claude` Agent preset by default. Use `--agent` to provide
another preset and model, for example `--agent "claude --model sonnet"`. It
writes `Status: draft` and a numbered list to `acceptance.md`. Review and edit
that list, then change its status line to `Status: approved` before a Trial.
The drafting Captured run and spend record live under the Case's `drafting/`
directory and do not belong to an Arm.

The manifest is JSON with this shape:

```json
{
  "name": "case-name",
  "repository_url": "https://example.com/project.git",
  "parent_commit": "0123456789abcdef",
  "reference_range": "0123456789abcdef..fedcba9876543210",
  "gate_commands": ["go build ./...", "go test ./..."],
  "scope": ["."],
  "standard_documents": ["docs/agents/implementation.md", "AGENTS.md"]
}
```

Create a Case from a historical Task set:

```sh
go run ./eval prepare <repo-path> <set-id>
```

The default Case name is the Task-set identifier. The default Objective gates
are `go build ./...` and `go test ./...`; the default scope is `.`. Standard
documents default to `docs/agents/implementation.md` and `AGENTS.md` when each
exists at the parent commit. Repeat `--gate`, `--scope`, or `--standard` to set
explicit values. Use `--name` to select another Case name.

## Arms

The Bare arm gives the Case spec to one headless agent invocation. It does not
use task planning, retries, Refine, or a Verifier.

The Pop arm gives the same spec to Pop as the prepared Task split. Pop drains
the AFK tasks and can use its configured retries, Refine, and Verifier.

This comparison has a planning asymmetry: the Pop arm receives the same words
already split into tasks, while the Bare arm receives them as one spec. Case
preparation is not part of either arm's measured spend.
