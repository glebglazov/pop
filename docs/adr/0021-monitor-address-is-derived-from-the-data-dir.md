# The monitor daemon address is derived from the data dir, with an env/config override

The monitor daemon's TCP address defaults to a port **derived from the data dir** (the `XDG_DATA_HOME`-scoped pop directory that holds `monitor.json`), rather than a single hardcoded constant. Resolution precedence is **`POP_MONITOR_ADDR` env > `[pane_monitoring] addr` config > derived default**. Within one data dir there is exactly one daemon: on startup a new daemon handshakes whoever holds the port and, if it is a pop daemon of any version, reaps it and reclaims the port (version-restart). A bind failure against a non-pop process is surfaced (Doctor / loud log), never swallowed.

## Why

Discovery stays a pure function of the data dir — no daemon-written endpoint file that can go stale, the way the PID file did. The port is the scope of the daemon; the data dir is the scope of the state (`monitor.json`). Tying the port to the data dir makes those two agree:

- **Same data dir** (e.g. old + new pop after an upgrade) → same port → the binaries coordinate (new reaps old), which is what you want: one shared daemon over one state file.
- **Different data dir** (e.g. a test binary that sets its own `XDG_DATA_HOME`) → different port → no collision.

The original bug came from exactly this mismatch: a leftover test daemon (`/tmp/poptest/pop-baseline-bin`, a *different* data dir) squatted the *same* hardcoded port `57341`. Liveness was judged by the PID file (absent at the real path) while the port was held by a stranger, so `pane status` reported "daemon not running" forever, every auto-restart silently failed to bind and exited, and only the direct-write fallback kept panes updating. Deriving the port from the data dir prevents that class of collision by construction.

Per [ADR 0001](0001-monitor-stays-config-free.md), the derived default lives in the `monitor` package (it already knows its own data dir); the env/config override is resolved in the command layer and passed in as a plain address — `monitor` never imports `config`.

## Considered Options

- **Keep the bare hardcoded port `57341`.** Rejected: machine-global port vs. data-dir-scoped state is the mismatch that caused the bug; any second data dir on the host collides.
- **Dynamic port + daemon-written endpoint file + process sweep.** Rejected: reintroduces a stale-able discovery file (the disease we are curing), and for the actual pop-vs-pop case fleeing to a new port causes split-brain (two daemons writing one state). Its only unique win — surviving a *non-pop* process on the port — is the rarest case and is covered by the manual override.
- **Derive the port from the data dir + env/config override + reap-on-version-change (chosen).** Auto-isolates by state scope, keeps discovery deterministic and file-free, and gives a manual escape hatch for the rare foreign-process collision.

## Consequences

- Config (`XDG_CONFIG_HOME`) and state (`XDG_DATA_HOME`) are separate dirs. A config that *pins* `addr` is shared across whatever data dirs use that config, so pinning reintroduces collision risk across data dirs. Hence the default must stay derived; pinning is an explicit, manual choice for single-instance setups.
- Tests that run an isolated daemon get isolation for free from the derived default (their `XDG_DATA_HOME` differs); they no longer need to set `POP_MONITOR_ADDR` to avoid clobbering a real daemon, though they still may.
- "Liveness" within a data dir is now answered by handshaking the port, not by trusting the PID file. The PID file remains at most a fast-path hint; the port's answer is authoritative.
