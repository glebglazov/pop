# Workload execution is self-contained and AFK-only

`pop workload` implements its issue executor inside the pop binary. The executor runs eligible AFK issues only; HITL issues remain a human stop condition. Workload artifacts beneath `thoughts/` are updated locally but are not committed with implementation changes.

## Why

Workload scheduling and execution must remain usable when pop is distributed as a standalone Go binary. Shipping a second runtime component would complicate installation and split one user-facing workflow across separately distributed tools.

The to-issues manifest contract remains the behavioral reference: eligibility, acceptance-criteria verification, retries, manifest updates, progress records, and implementation commits. Pop intentionally narrows unattended eligibility to AFK issues. Running HITL work automatically would contradict its purpose.

The initial personal workflow keeps `thoughts/` ignored and machine-local. Force-adding those files would leak personal planning artifacts into implementation commits. Team-shared workload definitions may revisit this boundary later.

## Considered Options

- **Ship execution as a separate runtime component.** Rejected: smaller pop implementation, but a more complex installation and operational model.
- **Implement execution in pop and preserve automatic HITL fallback.** Rejected: unattended workload commands should stop when human input is required.
- **Commit workload artifacts with implementation changes.** Rejected for the personal-workload iteration: `thoughts/` remains machine-local.
- **Implement AFK-only execution in pop (chosen).** Pop owns its runtime dependencies and exposes a predictable automation boundary.

## Consequences

The initial implementation must cover single-issue execution and sequential Issue-set draining directly in Go. Parallel execution and automatic worktree creation are deferred.

Future team-shared workload definitions may require revisiting local-only artifact handling. Until then, tests should lock down AFK eligibility, local bookkeeping, and implementation-only commits.
