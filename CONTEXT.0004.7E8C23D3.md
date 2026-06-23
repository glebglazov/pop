---
fragment: 7E8C23D3
generation: 0004
branch: master
---

+ Config finding
  A single config-validation problem discovered during load, keyed to its config
  path (e.g. `effort.foo`, `projects[2].display_depth`) and carried on the loaded
  config instead of thrown. Surfaced two ways: as the `error` from the getter for
  that key, and as a non-blocking entry in the picker's warning banner.
  avoid: config error (when you mean a non-fatal finding, not unparseable TOML)
  under: Configuration

+ Core capability
  The one thing a command must produce to be worth running — e.g. the project
  list for `pop project dashboard`. A command aborts on a config problem only when
  a value it consumes is invalid *and* essential to this capability; every other
  config problem degrades to a default plus a warning, and the command still runs.
  under: Configuration
