---
status: accepted
relates: "supersedes decision 8 of [ADR-0196](0196-one-agent-override-picker-and-attended-gates-become-inline-tui.md) and deletes its picker; extends the hand-authored/pop-written split of [ADR-0150](0150-the-config-dir-holds-only-hand-authored-files.md) without amending it; deliberately does not reuse the repo keying of [ADR-0191](0191-repo-scoped-settings-pop-writes-live-in-an-identity-keyed-runtime-layer.md); follows the declared-on-the-key marker pattern of [ADR-0198](0198-a-config-key-declares-its-own-reach.md); exposes the four groups of [ADR-0194](0194-agent-lists-are-grouped-by-kind-of-work-and-attended-is-one-of-the-groups.md)"
---

# Config overrides are a top-ranked pop-written layer, edited by one component

## Context

ADR-0196 gave pop an **Agent override**: `alt+a` opened a two-level numeric
picker, promoted one **Agent entry** to the head of its **Work agent group**, and
the promotion lasted one OS process. Decision 8 made non-persistence a principle
— "an override set three days ago is exactly the invisible state decision 9
exists to prevent" — and decision 9 paid for that principle with four renders of
the effective entry, so a human could always see what was in force.

In use the feature is not discoverable. Nothing in it teaches that it exists: the
key is a bare modifier chord, the picker appears only when pressed, and its
effect vanishes when the process ends, so there is no state a human can find
afterwards and reason about. The renders decision 9 added report *what* is in
force but not *that* it can be changed anywhere a human would go looking. The
observed failure is not "the override went stale" — it is that the override is
never reached at all.

Meanwhile the settings themselves have no editor. `pop config keys` lists what
the schema accepts, `pop config show` prints the merge, `pop config repo set`
writes exactly the reflected repo-scope keys. Everything else is hand-edited
TOML. So a human who wants a different model for verification has two options:
a keystroke they do not know about whose effect evaporates, or an editor session
in `config.toml`, which ADR-0196 correctly says "is not an override".

There is one pop-written config file already, `config.runtime.toml` (ADR-0150),
and it is the wrong file for this. Its rank is *below* everything hand-authored —
`config/config.go:1224` calls it "a gap-filler" — and that rank is load-bearing:
`preferred_workbench`'s three-valued explicit-none logic exists precisely because
a runtime entry can no longer beat a hand-authored value above it (ADR-0078,
ADR-0083). It also keys by checkout and by repository identity, neither of which
is the keying an override of "which model do I want" wants.

## Decision

1. **A second pop-written file, `config.override.toml`, ranked above every
   hand-authored source.** It holds whole-key values a human has deliberately
   overridden. ADR-0150's split governs *which files pop may write*, not their
   precedence, so a second pop-written file with the opposite rank leaves that
   rule intact. Repurposing `config.runtime.toml` by inverting its precedence was
   rejected: one file would then carry two contradictory ranks, and each of its
   four existing writers — `[workbench.preferred]`, the repo trunk paths,
   `[integrations].skills`, and ADR-0191's identity-keyed repo settings — would
   need re-litigating against an editor none of them asked for.

2. **The unit of an override is one key's whole value, as TOML text.** Not a
   patch, not a structured per-entry edit. This is what generalizes to every key
   exposed later, and for the agent lists it dissolves ADR-0196's
   promote-versus-pin question: a human edits the ordered list itself, so the
   ordering left below the head line *is* the tail **Agent fallback** walks.

3. **A key declares its own exposure with an `override:"<scope>"` struct tag**,
   whose value names the scope the override lands at — `global` today, `repo`
   reserved. Reflection over the tag is the entire registry, exactly as `desc`
   already backs the key catalog and as ADR-0198 decision 6 marks the repo keys
   pop can write. An unmarked key is invisible to the editor. The tag carries the
   scope rather than a boolean because a boolean must be re-read or joined by a
   second tag the moment the first repo-scoped key appears.

4. **Four keys are exposed at the cut**: the `agents` list of each **Work agent
   group** — `work.implement.agents`, `work.verify.agents`,
   `work.routine.agents`, `work.attended.agents`. The editor lists only exposed
   keys; a catalog of ninety rows where four do something teaches the wrong
   thing.

5. **ADR-0196 decision 8 is superseded and its picker deleted** —
   `ui/agentoverride.go`, its tests, its `alt+a` wiring in the gate menus and
   dashboard, and `alt+a`'s exclusion from kind-supplied key space. Keeping a
   session-lived promotion beside a persisted override would put two overrides at
   two lifetimes on the same four lists, which is the invisible-state problem
   twice over rather than half of it. Decision 9's four renders **stay**, now
   resolving through the new layer: a persisted override needs them more than a
   session-lived one did.

6. **Removing an override is not the same as overriding to empty.** `ctrl+x`
   deletes the key from the override file, restoring the source — which for
   `verify` and `routine` means their documented fallthrough to `implement`'s
   list. An override set to `agents = []` disables that fallthrough. Both states
   render as an empty-looking value, so the preview names them in words. `ctrl+x`
   on a key with no override is a no-op, and there is no confirmation prompt: the
   source value is one `ctrl+y` away.

7. **The `$EDITOR` buffer is the full `key = value` line, and an empty buffer
   cancels.** Returning whitespace changes nothing rather than deleting the
   setting; `agents = []` is how emptiness is stated explicitly. The buffer
   carries the key so the editor never has to infer what the returned text is.

8. **The editor is stricter than the loader.** Pop's config validation is
   Finding-based and non-fatal (ADR-0054) — a malformed agent entry becomes a
   finding while the list around it loads. On return from `$EDITOR` the value is
   parsed *and* schema-validated, and a finding re-opens the editor instead of
   writing the file. A file pop wrote itself must never be the source of a
   finding.

9. **The precedence ladder for an exposed key is: repeated `--agent` flags, the
   override layer, hand-authored `config.toml`, built-in default.** A flag is
   scoped to one invocation and typed on purpose, so it stays on top; the rest is
   the existing ladder with one layer inserted above the hand-authored file.

10. **The surface is a self-contained `ui/` component, not a dashboard page.**
    The three hosts it must be reachable from are unrelated programs: the Work
    dashboard is `dashboardshell`'s multi-page shell, while `pop project
    dashboard` and `pop worktree dashboard` are each their own tea program over
    `ui/picker.go`, launched in a tmux `display-popup`. A page in the shell would
    be reachable from one of the three. The component shape is the one
    ADR-0196's picker already proved by working from gates and dashboard pages
    alike. `pop config dashboard` runs the same model as a top-level program, and
    `alt+c` opens it from any host — `alt+a` returns to the pool, because muscle
    memory for it points at a picker that no longer exists.

11. **Two host contracts bind the component.** While it is open the host's keys
    are fully suspended — `pop worktree dashboard` binds `ctrl-x` to *force
    delete worktree*, so an unsuspended host turns a removed override into a
    removed worktree. And it **never writes to stdout**, on any path including
    error, because in the picker hosts stdout is a data channel: the documented
    worktree binding is `cd "$(pop worktree dashboard)"`. Errors surface as a row
    in its own view.

12. **Layout: dotted-path rows on the left with search, config-format preview on
    the right.** The row is the dotted key path with `desc` as a dim second line,
    because the path is what `pop config keys` takes and what the preview's TOML
    shows; a prose label would be a fifth name for one key. Overridden rows carry
    a marker. The preview renders the effective TOML value, then a provenance
    line naming the layer that produced it (`override`, `config.toml`, `built-in
    default`, or `fallthrough → work.implement.agents`), then — when an override
    exists — the source value dimmed, so `ctrl+y` and `ctrl+x` have a visible
    target. Reach (ADR-0198) renders for keys that declare it; the four agent
    keys declare none in this pass.

13. **`$EDITOR` runs in place via `ExecProcess`, popup geometry
    notwithstanding.** A `display-popup` is a real terminal and a cramped editor
    window is the human's own layout choice; a component that behaves differently
    depending on how it was launched is harder to reason about than a small
    editor. A larger dedicated binding for `pop config dashboard` is documented
    beside the existing bindings instead.

14. **An override takes effect at the next config load, and nothing is
    hot-reloaded.**
    > **Amended by [ADR-0242](0242-the-dashboard-reads-truth-through-one-guarded-reload.md):**
    > the Work dashboard shell now also re-reads when a config file's mtime
    > changes, checked each poll — the modal's post-write re-read is no longer
    > the only hot reload.

    The supervisor already re-reads: `tick()` calls `LoadConfig`
    every pass (`supervisor/supervisor.go:64`) and every drain it spawns is a
    fresh process, so implement, verify and routine pick the override up with no
    new mechanism. In-flight drains legitimately finish on the list they started
    with. The one gap closed here is `dashboardshell`, which loads once in
    `newShell` and hands that `*Config` to every page: it re-reads and re-merges
    after a write, since it hosts the editor and its own preview would otherwise
    lie.

15. **The TUI is the only writer in this pass**, and the command refuses when
    stdout is not a TTY. A `pop config override set` sibling needs value parsing,
    per-key validation and shell-quoting of a TOML array on the command line —
    a second full surface, deferred deliberately rather than overlooked.

## Considered Options

- **Repurpose `config.runtime.toml` with inverted precedence.** Rejected in
  decision 1. It is empty on the machine where this was designed, but "empty
  here" is not "empty in the wild": anyone who has pressed `ctrl+w` has a
  `[workbench.preferred]` entry, and inverting the file makes that last-picked
  Workbench start beating a hand-authored repo default.
- **Keep the override layer at runtime's low rank.** Rejected: a layer that
  loses to `config.toml` is inert for precisely the people who configured
  something, which is everyone the feature is for.
- **Persist ADR-0196's promotion instead of the list.** Rejected. A persisted
  promotion is a second grammar over the same list — it has to be replayed
  against a list that may have changed underneath it — where an edited list is
  just the list.
- **A third page of `dashboardshell`.** Rejected in decision 10. It satisfies one
  of the three hosts.
- **A prose or semantic search over the key catalog.** Rejected. The TUI must
  open on a bare terminal mid-drain; a search that needs a model or a network is
  a search that fails when it is most wanted. Fuzzy over path plus `desc` is
  what `ui/picker.go` already ships.
- **Reach for the four agent keys in this pass.** Deferred, not rejected. ADR-0198
  put reach on the key, and the preview reads that indirection; declaring reach
  for the agent lists is a separate question about what those lists actually
  touch.

## Consequences

- **Config has two pop-written files with opposite precedence.** Anyone reading
  either must know which is the gap-filler and which is the override. This is the
  cost decision 1 accepts on purpose, and the reason the two are separate files
  rather than two sections of one.
- **A setting can now be in force that appears in no hand-authored file.** That
  is the point, and it is also the risk ADR-0196 decision 8 named. Decisions 5
  and 12 are the mitigation: the four renders survive, the layer appears in `pop
  config show`, `pop config keys` marks exposed keys, and the editor's list marks
  which keys are overridden.
- **Five gate menus and the dashboard lose `alt+a`.** Its inline picker was
  ADR-0196's newest surface and is deleted a release later. Golden-frame coverage
  for those menus is rewritten, as ADR-0196 decision 2 established.
- **Three hosts must each honour decision 11.** Nothing in the type system
  enforces "suspends host keys, never prints"; a fourth host added later can
  break the worktree picker's stdout contract silently. The constraint is
  recorded here because that is the only place it can be checked.
- **Overrides do not travel with a machine or a clone**, being global-scoped
  pop-written state. `override:"repo"` is reserved for the setting that turns out
  to describe a repository, on ADR-0191's terms.
