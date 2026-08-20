# Pop writes the manifest's commit convention itself

A **Task set**'s manifest carries `commit_convention`: the resolved **Commit convention** prose, which an agent spawning a task mid-drain renders a new **Planned commit subject** from. Planning writes it, and planning is an agent, so the key is a hand-retyped copy of prose pop had already resolved. Nothing checks the two against each other.

In one observed session the copy was lossy in the way a transcription is lossy — the agent retyped it from its own draft rather than reading the resolved answer back — silently dropping a worked subject example, a multi-PR clause, a body-length rule, and a clause from the human's **Convention overlay**. It was caught only because a human asked whether the convention had been re-read.

The decision: **pop writes `commit_convention` itself at register time, from the resolved stack.** The authoring guide stops asking an agent for it, and a planning agent that supplies one is ignored.

Truncation is not the mechanism worth defending against here, and the fix that looked obvious is declined for that reason. [ADR-0226](0226-a-convention-always-answers-and-a-recipe-is-never-a-rank.md) removes the banner that taught agents to peek at a convention's first lines, so an agent reading a convention it has not seen reads it whole; rendering the overlay *before* the answer would defend a closed hazard while printing a block labelled `APPENDED:` above the thing it is appended to. The damage here was transcription, not truncation, and no amount of reading discipline fixes a copier that has no reason to exist.

Also declined: rendering the **Planned commit subject** at commit time instead of planning time. Freezing it is deliberate — it is what makes a drain reproducible, and moving the render later would have an agent generating subjects unattended, which is what produced an unattended amend of a `main` branch in the same session. Better to make the planning-time render correct than to move it.

## Consequences

- The manifest key stops being agent-authored, so it can be trusted as a projection of the stack rather than a claim about it.
- The `commit_convention` entry in `tasks/authoring_guide.go` becomes a statement that pop writes the key, not an instruction to supply it.
- A set registered outside a repository, or for a kind that somehow does not resolve, still gets a value: after ADR-0226 the stack always answers.
