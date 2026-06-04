# Integration artifacts render to pop's data dir; agent locations are symlinks

File-based **Integration components** (skills, agent extensions) are rendered per agent into pop's data directory (`~/.local/share/pop/integrations/<agent>/...`, respecting `XDG_DATA_HOME`) and the agent's own location receives a symlink into that tree. Hook-based wiring is excluded — hooks are JSON entries merged into agent settings files, not files pop can own. The render is per agent, not a single shared copy, because agent formats diverge (frontmatter name injection for pi and cursor, flat files for opencode, skill directories for claude).

## Why

A symlink pointing into pop's tree is a machine-checkable ownership marker: removal deletes only symlinks that resolve into pop's render tree, so pop can never destroy a user's hand-written skill — a stronger guarantee than the `pop-` name-prefix convention. It also gives one auditable answer to "what has pop put on my machine" (`ls` the render tree), makes **Doctor** checks trivial (symlink present, target exists, canonical bytes match embedded), and lets **Integration refresh** rewrite the render tree without touching agent directories.

## Considered Options

- **Copy files into agent directories (status quo).** Rejected: ownership is only inferable from naming conventions, removal is riskier, and installed state is scattered across five agent config trees.
- **One shared canonical file symlinked to all agents.** Rejected: per-agent transforms mean the installed bytes legitimately differ per agent, so a single source file cannot serve them.

## Consequences

Agents are assumed to follow symlinks; this is accepted untested and verified by use. If an agent turns out not to, that agent falls back to copy mode and loses the readlink ownership marker there. Existing copy-mode installs migrate naturally: the next wizard or refresh run replaces the file with a symlink via the existing wipe-and-rewrite path.
