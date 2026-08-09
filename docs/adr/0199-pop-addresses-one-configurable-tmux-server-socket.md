---
status: accepted
relates: "keeps the single tmux chokepoint of [ADR-0142](0142-tmux-operations-live-in-one-deep-internal-module.md) and reuses its rejection of speculative multiplexer abstraction; renders the base config per [ADR-0011](0011-integration-artifacts-render-to-pop-data-dir.md); declines the consent-wizard shape of [ADR-0010](0010-integrate-is-a-per-component-consent-wizard.md) for tmux keybindings; keeps the socket key out of the include whitelist of [ADR-0037](0037-includes-carry-a-whitelisted-config-subset.md); scopes the approximate session-name matching of [ADR-0005](0005-dashboard-session-name-is-approximate.md) to one server"
---

# Pop addresses one configurable tmux server socket

## Context

Pop is unusable to someone who does not already run tmux, and the reason is not
that pop depends on tmux — it is that pop depends on a tmux the *user* has
configured. Every popup in the intended workflow is a hand-written `bind-key
display-popup` in a personal `tmux.conf`; pop itself contains no
`display-popup` call anywhere, only suggested bindings in `pop project --help`
and `pop worktree --help`. A user willing to install tmux for pop's sake still
gets nothing until they learn enough tmux to write those bindings themselves.

Removing tmux is not the answer. `internal/tmux/` is one deep module
(ADR-0142), so a seam exists structurally — but the domain above it is tmux's:
a **Session** is a tmux session, a **Pane** is the unit of agent work and
attention, **Workbench** and **Layout** are defined by reference to tmux's own
session and window scopes (ADR-0074), a **Topic** lives in a `@pop_topic` user
option (ADR-0058), and a Ticket claim's owner is a pane id. Replacing tmux means
writing a multiplexer, not an adapter.

Nor is an emulator-native popup the answer. Ghostty's quick terminal is a
persistent global surface toggled by a keybind — a summon gesture, with no
scriptable "run this command in an ephemeral overlay and close on exit"
primitive; its remote control is `+new-window` with three flags, plus an
AppleScript dictionary on macOS. `tmux display-popup -E` is unusual in being
scriptable, ephemeral, per-session, and working over ssh. The requirement
decomposes into a *summon gesture*, which is the emulator's or tmux's job, and a
*payload*, which is pop's — and only the payload half was ever pop's problem.

What blocks the non-tmux user is therefore narrow: pop has no say over which
tmux server it talks to, and no way to give a user tmux keybindings they did not
write.

## Decision

1. **The server socket is one global config key.** `tmux.socket` selects the
   tmux server every pop command addresses, defaulting to tmux's own `default`
   socket — the one a plain `tmux` creates — so pop lives in the user's tmux out
   of the box. Setting it to `pop` gives pop an isolated server. The key is
   global-scope only and **excluded from the include whitelist** (ADR-0037): an
   include is content pop reads from a repository, and a repository that could
   redirect the socket could start a server pop then treats as the user's.

   **An unset key emits no `-L` flag at all** — not `-L default`. The two reach
   the same server, but only the first does so by being the same command pop
   already runs. Emitting `-L default` reaches it through a chain of reasoning
   (the default socket name happens to be `default`; `-L` resolves through
   `$TMUX_TMPDIR`-or-`/tmp` identically; the zero value of a Go string is mapped
   to the right constant), and every link is somewhere an upgrade can silently
   move an existing user onto a second server. No flag has no links. This also
   leaves room to accept a full socket path (`-S`) later, which `-L` cannot
   express.

2. **`InTmux()` is replaced by a socket-identity comparison.** `$TMUX` is
   `<socket-path>,<pid>,<session>`; the predicate is whether its first field
   matches the configured socket. This is load-bearing, not cosmetic — a bare
   `$TMUX != ""` check cannot tell which server it is in, so a pop aimed at its
   own socket while the user sits in a personal tmux would read as "inside" and
   issue `switch-client` against a server with no attached client.

   **Both sides must be compared as resolved paths.** The configured socket is a
   name, and constructing its path from tmux's own rule (`$TMUX_TMPDIR` or
   `/tmp`, then `tmux-UID/<name>`) yields `/tmp/tmux-501/default` on macOS while
   `$TMUX` and `#{socket_path}` both report the symlink-resolved
   `/private/tmp/tmux-501/default`. A naive string compare therefore fails on
   every macOS machine with `TMUX_TMPDIR` unset — and fails in the direction that
   reports "outside tmux" always, which turns every `switch-client` into an
   `attach-session` that tmux refuses as nesting. Resolve both sides
   (`filepath.EvalSymlinks`, or ask tmux for `#{socket_path}`) and test it: the
   naive version passes on Linux.

3. **Three states, and the third refuses.** Inside the configured socket →
   `switch-client`. Inside no tmux → `attach-session`. Inside a **foreign**
   server → pop refuses with a message naming both sockets and both fixes. Pop
   never nests tmux inside tmux.

4. **Config ownership follows the user, not the socket.** These are independent
   axes. If no user tmux config exists at `~/.tmux.conf` or
   `$XDG_CONFIG_HOME/tmux/tmux.conf`, pop passes `-f` with its **Base tmux
   config** when it starts the server — a complete opinionated config including
   pop's keybindings, which for that user simply *is* their tmux config. If a
   user config exists, it wins untouched and pop contributes nothing to the
   server.

5. **Pop writes nothing into the user's home.** The base config is regenerated
   into the pop data dir (ADR-0011) and reaches tmux only via `-f`.

6. **A tmux config include is the customization seam.** A config key holds the
   path to a user-authored file that pop sources and never writes.

7. **Servers pop supplied the base config to are version-stamped and
   re-sourced.** Pop stamps its version as a server option when — and only
   when — it started the server *and* passed `-f` with the base config. On a
   later run whose version exceeds the stamp, pop re-sources the regenerated base
   config and re-stamps. **An unstamped server is one whose config pop did not
   supply, so pop never touches it.** The base config must therefore stay
   re-sourceable: `set-option` and `bind-key` only, nothing with side effects.

   The stamp records *"pop's config is what runs here"*, not *"pop started
   this"*. The two coincide for a user with no tmux config of their own and
   diverge for everyone else: a user with their own config whose server happens
   to be started by a pop command (after a reboot, say) is correctly given no
   `-f` under decision 4 — stamping on start alone would then let a later
   upgrade source pop's bindings on top of their config without asking, which is
   the behaviour this decision rejected under *Considered Options*.

8. **The server is lazy and immortal, and only session-creating commands start
   it.** A command that needs a *session* — attach, spawn, drain — starts the
   server if absent. A command that only *reads* — the Work dashboard, status,
   the pickers — must never ensure one: it reports no sessions and creates
   nothing. tmux itself already behaves this way (`list-sessions` against an
   absent server errors rather than starting one), so the phantom server can only
   come from pop's own ensure logic. Without this, a first-time user whose first
   command happens to be `pop work dashboard` gets a tmux server started,
   configured, stamped and kept alive forever solely to render an empty table.

   Pop never reaps. Detached unattended work — supervisor drains, fan-out panes,
   background agent sessions — means "no sessions left" and "nothing running" are
   different questions, and a reaper would race the daemon.

9. **A user with their own tmux config is told what they are missing**, via a
   Project readiness finding and a command that prints pop's binding fragment
   for them to source or paste. Pop does not install it for them.

10. **No new surface.** Commands, pickers and dashboards are unchanged; this
    decision is entirely about which server they address and who configured it.

## Considered Options

- **A tmux-free pop** — rejected: the glossary above `internal/tmux/` is tmux's,
  so this is writing a multiplexer, not swapping a backend.
- **A multiplexer abstraction with a tmux driver** — rejected on ADR-0142's own
  reasoning, which refused a generic tmux client for a consumer count of one.
  Revisit when a second driver actually exists.
- **An adaptive socket** (pop's own, but adopt the ambient server when `$TMUX`
  is set) — rejected: it makes a command's target depend on where it was typed,
  so the same command addresses two different session universes.
- **Detecting whether a `default` server exists and using it, else `pop`** —
  rejected, and redundant once the default is `default`: nobody reaches the `pop`
  socket without typing it. Its failure is a reboot boundary. No server exists
  after boot, so a pop command that runs before the user opens tmux — a routine,
  the supervisor, or plain curiosity — finds no `default` and starts building on
  `pop`; the user then opens tmux, creating `default`, and pop's sessions are
  stranded in a server their prefix key cannot reach. "A default server exists
  right now" means only "someone ran tmux since boot", which is not a statement
  of intent. It also costs decision 2 its meaning: an identity comparison against
  a socket chosen per-process answers differently at different times.
- **An `integrate` component installing the keybindings** into the user's
  `tmux.conf` (ADR-0010's shape) — rejected for now. Deferred rather than
  refused; it remains the only path that reaches a server pop did not start,
  with consent.
- **`source-file` of pop's bindings into any live server** — rejected outright:
  pop silently rebinding keys in a server it does not own, on every invocation.
  Decision 7 permits this only against servers pop itself started and stamped.
- **Pop writing `$XDG_CONFIG_HOME/tmux/tmux.conf` when absent** — rejected
  despite fixing the race in the first consequence below; writing into a user's
  dotfiles is exactly what a consent wizard exists to ask about, and this
  decision declined the wizard.
- **Pop's base config sourcing the user's top-level tmux config** — rejected as
  unreachable: under decision 4 the two never coexist.
- **Forcing nested tmux** by clearing `$TMUX` — rejected: the user who chose
  isolation is handed the opposite, one server's prefix key swallowing the
  other's.
- **One summonable entry surface** ("Console") replacing the command family —
  rejected as out of scope; it decouples pop from `display-popup` but solves a
  problem the socket key already solves.

## Consequences

- **For an existing tmux user the upgrade is a no-op, by construction.** They
  gain no config key, so pop emits no `-L` and runs the command it already ran;
  their server is already up so nothing is started; they have their own tmux
  config so `-f` never applies; their server is unstamped so it is never sourced
  into. Same socket as a first-time user, opposite relationship — pop configures
  the newcomer's server and is merely a tenant in theirs.
- **`-f` is start-order dependent.** tmux loads a configuration file once, when
  the server process starts. A fresh user with no tmux config who types `tmux`
  before ever running a pop command gets a bare server, and pop's base config
  does not reach them until that server dies. Accepted over writing to `~`.
- **Config detection must check both search paths, and the classic one is often
  absent.** A user whose tmux config lives at `$XDG_CONFIG_HOME/tmux/tmux.conf`
  frequently has no `~/.tmux.conf` at all, so a detection that checks only the
  classic path concludes they have no tmux config. The damage is latent — their
  server is usually already running, so `-f` cannot fire — until the day a pop
  command is what restarts it, at which point pop brings up their tmux under
  pop's base config instead of their own, silently.
- **Isolation and a personal tmux are mutually exclusive.** `tmux.socket = "pop"`
  while attached to a `default` server hits decision 3's refusal. The supported
  configurations are: pop in your tmux with your sessions; or pop on its own
  server with you not using tmux personally. The combination is a known
  limitation. The right answer for that user is probably a popup *into* pop's
  server from their own tmux — which their tmux can do and pop cannot arrange
  for them, because this decision declined the integrate component.
- **Pop's session world is scoped to one server.** Sessions on any other socket
  are invisible to the picker, and ADR-0005's approximate session-name matching
  now operates within one server rather than across whatever happened to be
  running.
- **A migration for existing tmux users.** Defaulting to `default` means nothing
  changes for them unless they opt into `pop`, at which point their existing
  sessions leave pop's view.
- **What goes in the base config is deliberately unsettled.** Prefix, mouse,
  copy-mode, status line, and how plainly it tells a tmux novice how to detach
  decide whether that user survives their first session. That is content, better
  resolved with the file in hand than in the abstract, and it is the follow-on
  work this decision creates.
- **The socket flag has one home.** `execRunner` (`internal/tmux/runner.go`) is
  the only place pop spawns tmux, so decision 1 is a prepend in one struct —
  which is what ADR-0142's "no `Command(args...)` in the interface" bought.
