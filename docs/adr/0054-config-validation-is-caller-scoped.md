# Config validation is caller-scoped, never global

A config problem may degrade the *quality* of a command but must never prevent
the command from delivering its core capability. `Load()` therefore fails only
on unparseable TOML (class A); every other problem (class B — a renamed key, an
invalid effort tier, a bad `display_depth`) is collected as a **config finding**
keyed to its config path and carried on the returned config rather than thrown.
Value getters become `(value, error)`: a getter returns an error when *its* key
has a blocking finding, and **the caller decides what that error means** — fatal
if the value is essential to its capability, otherwise fall back to the default.
The same stale `[effort]` key is thus fatal to `pop tasks drain` (it reads
effort) and invisible to `pop project dashboard` (it never calls that getter).

This exists because `pop project dashboard` is a critical, hot-path tool: it
must always render the project list if the list is obtainable. Previously a
config rename in an unrelated section aborted every command that called `Load()`,
which eagerly validated the whole file — so a stale `[effort]` or `[queue]` key
killed a dashboard that never used it.

## Considered Options

- **Status quo — eager validation in `Load()`.** Simple and loud, but couples
  every command to every section: one stale key anywhere bricks unrelated tools.
- **Section-local rule** — hard-fail only on errors in sections a command
  consumes, via an explicit command→section registry. Rejected: it needs a
  hand-maintained table, and it still aborts on a bad *non-essential* value
  inside a consumed section (e.g. `display_depth`).
- **Caller-scoped via error-returning getters (chosen).** The call graph already
  encodes which command needs which key, so no registry is needed, and severity
  is decided per call site — capability-local falls out for free.

## Consequences

- Semantic validation moves from eager (in `Load()`) to lazy (at the getter /
  point of consumption), extending the existing `ResolveCommitConfigOverrides()`
  pattern.
- Getters that can surface an invalid configured value gain an `error` return;
  call sites choose fatal-vs-default per their capability.
- **Migration tripwires stay loud, but confined.** A deliberate hard-error
  rename like `queue_base`→`execution_base` still aborts `pop queue` / `pop tasks`
  (they consume execution config) and goes silent for commands that don't.
- Findings for keys a command never consumed still surface as a **non-blocking
  warning banner** (feeding the existing `cfg.Warnings` → picker path), so
  "always loads" never becomes "silently ignores your typo".
- `pop project dashboard`'s only remaining hard-fail paths are: unparseable TOML,
  and a `projects` table that yields zero usable directories (nothing to switch
  to). Partial expansion shows what resolved and warns about the rest.
