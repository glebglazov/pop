# The Reviewer runs under a read-only agent posture

> **Amended by [ADR-0240](0240-refine-fixes-in-place-before-the-verify-phase.md):**
> the Reviewer became the Refiner and fixes in place, so no role is currently
> spawned under this posture. The capability itself stands as declared preset
> metadata — the per-preset flag research below is why it is kept rather than
> retired.

Every headless role pop spawns — Implementer, Verifier, Reviewer — goes out through one `headlessPrefix` per Agent preset, and every one of those prefixes disables approval: `claude --dangerously-skip-permissions -p`, `codex exec --dangerously-bypass-approvals-and-sandbox`. That is correct for the Implementer, whose job is to write files. For the Reviewer it means the only thing standing between a reviewing agent and the tree the human is about to fold is one sentence of prose in `reviewer.tmpl.md`: *"Change no files — you are reading, not fixing."* The Reviewer runs in the set's bound checkout, after the Verifier has already passed on it, so an edit it made would arrive unattributed in work nobody re-verifies.

The decision: **a preset declares a read-only agent posture as one more capability beside its attended arguments and its headless prefix, and the Reviewer — alone among the headless roles — is spawned under it.** claude contributes `--disallowedTools Edit,Write,NotebookEdit`; codex contributes `--sandbox read-only`. A preset with no such argument declares the capability blind, and pop reports the posture it actually obtained rather than the one it wanted.

**It stops at the Reviewer, and the reason is that the two flags are not the same flag.** codex's sandbox blocks every write, including one made by a shell command; claude's tool denial leaves `Bash` in place, so a determined claude Reviewer could still redirect into a file. For a role that only runs `git diff`, `git log` and `git show`, both are adequate and the codex one is airtight. Give the same posture to the Verifier and the stronger of the two breaks it outright: verification runs the build and the test suite, and a build that cannot write its cache fails for reasons that have nothing to do with the code under test. Constraining the Verifier is a different decision needing a different mechanism — deny the editing tools, leave the sandbox open — and it should be made on its own evidence.

## Considered options

- **Leave it to the prompt** — rejected, and the status quo. A prompt sentence is a request; the whole point of an independent Reviewer is that its judgement is not the thing being trusted with the tree.
- **Run the Reviewer in a throwaway detached worktree at the work SHA** — rejected, though it is the strongest option. It enforces read-only-ness by making writes worthless rather than impossible, works identically on every preset, and needs no capability at all. It also buys a worktree provisioning path, a second checkout's disk per review, and a Reviewer that can no longer see the untracked files sitting in the real tree — which are frequently the interesting part of a changeset in progress.
- **One posture for every headless role** — rejected for the Verifier reason above.
- **A config key for the posture** — rejected. It is a property of what a preset's CLI can do, not a preference; there is nothing for a human to decide.

## Consequences

- **The guarantee differs by preset, and pop says which one it gave.** A blind preset spawns exactly as it does today with the prompt sentence as its only guard, and a surface that implied otherwise would be worse than saying nothing.
- **The Reviewer's prompt keeps the sentence.** Enforcement and instruction are not alternatives: an agent that is told what it may do writes a better review than one that discovers a tool is missing.
- Glossary: **Read-only agent posture** is added; **Reviewer** is redefined.
