# The .pop/ directory is the in-tree home, and .pop.toml becomes .pop/config.toml

Project routines ([ADR-0138](0138-project-routines-are-committed-prompts-discovered-live-from-pop-routines.md)) need a committed in-tree location for prompt files, which don't fit TOML. That gave the repo two in-tree pop surfaces: the flat `.pop.toml` config file and a new `.pop/` directory — a naming near-collision inviting confusion about which one a repo should carry.

Instead of tolerating the sibling pair, `.pop/` becomes the single in-tree home for everything a repo ships to pop: repo-scope config moves to `.pop/config.toml`, and routine prompts live under `.pop/routines/`. The two-anchor law (ADR-0083/0084) and the shared repo-scope enumerator (ADR-0122) are untouched except for the path they read.

There is no back-compat reader: a legacy flat `.pop.toml` is no longer read at all, and its presence draws a one-line warning pointing at the new path — warn-and-ignore, so a teammate's committed config never silently vanishes without a trace. The alternative — reading both locations with a precedence rule — was rejected: in-tree config has seen little to no real use yet, so a dual-read window would outlive the migration it serves and permanently complicate the enumerator for zero installed base.
