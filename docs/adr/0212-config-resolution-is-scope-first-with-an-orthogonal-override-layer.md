---
status: accepted
supersedes: [ADR-0083]
relates: "revives the scope-first law of [ADR-0077](0077-config-precedence-is-scope-first-config-breaks-equal-scope-ties.md) that ADR-0083 killed; retires the two-file cost and inverts the `override:` tag of [ADR-0202](0202-config-overrides-are-a-top-ranked-layer-edited-by-one-component.md); deletes the runtime file of [ADR-0150](0150-the-config-dir-holds-only-hand-authored-files.md); retargets the writer of [ADR-0191](0191-repo-scoped-settings-pop-writes-live-in-an-identity-keyed-runtime-layer.md); deletes `ctrl+w` and the per-checkout home of [ADR-0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md); retargets the opt-out store of [ADR-0065](0065-integrate-preferences-are-three-layer-config-merge.md); reshapes the stack of [ADR-0211](0211-a-repo-convention-resolves-through-a-composed-four-layer-stack.md) without changing what it composes; leaves the reach of [ADR-0198](0198-a-config-key-declares-its-own-reach.md) and the validation law of [ADR-0054](0054-config-validation-is-caller-scoped.md) intact"
---

# Config resolution is scope-first, with an orthogonal override layer

## Context

Pop has grown five ways to state a preference and no way to see them together.
A human setting something up must know which of `config.toml`, a
`[repo."<path>"]` block, `.pop/config.toml`, `config.runtime.toml`,
`config.override.toml`, `docs/agents/<kind>.md` or `~/.agents/docs/<kind>.md`
their intent belongs in — and each key admits an arbitrary subset of them.
`turn_cap` has two homes, `preferred_workbench` has four, an agent list has two
and both are global, and a per-repository agent list is not expressible at all.

Three symptoms made this legible.

**The same act lands at opposite ends of the ladder.** `pop config repo set
turn_cap 40` writes `config.runtime.toml`, rank 6 of 8, below every
hand-authored source. Editing an agent list in the Config dashboard writes
`config.override.toml`, rank 1. Both are a human stating what they want through
pop, and which end they land at is decided by which command was typed. ADR-0202
had to invent a second pop-written file precisely because the first one could
not hold both, and it recorded the resulting confusion as a permanent cost:
"anyone reading either must know which is the gap-filler and which is the
override."

**The precedence law surprises where it should reassure.** ADR-0083 ordered by
authorship — hand-authored beats pop-written, central beats in-tree — and
accepted the consequence in writing: "a global `config.toml` value shadows every
repo's committed value for that key." So a personal `preferred_workbench =
"solo"` silently overrides a team's committed choice in every repository, and
clawing it back for one repository means hand-authoring a `[repo."<path>"]`
block per checkout. The team said something specific; the human said something
general; the general one won.

**Conventions arrived with a different and better model.** ADR-0211 gave the
`commits` kind a four-layer stack the human holds both ends of, and — crucially
— it already cuts pop's own writes correctly: a convention derived from history
goes to pop's memory layer, while one a human states in session is offered to
`docs/agents/commits.md` in version control. Conventions distinguish *pop
guessed this* from *a human said this*. Config does not.

## Decision

### 1. Resolution is scope-first; strength breaks within-scope ties

The most specific scope wins. Within one scope, a **Declaration** (hand-authored
or committed) beats a **Config gap-filler**. Most specific first:

```
repository · declaration   config.toml [repo."<path>"] · ./.pop/config.toml · <trunk>/.pop/config.toml · docs/agents/<kind>.md
repository · gap-filler    Convention memory
global     · declaration   config.toml · ~/.agents/docs/<kind>.md
global     · gap-filler    (empty; see decision 5)
             default       embedded
```

This is ADR-0077's law, which ADR-0083 superseded on the grounds that it "let a
repo's committed value, or a runtime scratch entry, override the user's explicit
central config — the opposite of *pop is user-driven*." That objection was
correct when the human had no reliable way to win, and it expires with decision
2. Scope-first is safe exactly because the override sits outside the ladder.

### 2. The override is orthogonal, not a rank

An **Override config layer** entry is not a step in the ladder above. It is laid
over whatever the ladder resolved, and it always wins. For a scalar that means
replacement; for a **Repo convention** it means the human's prose is layered on
top of the composed stack rather than displacing a layer of it — which is what
ADR-0211's **Convention overlay** already does, now named as the general rule
rather than as rank 4 of one stack.

Within the override, the same specificity rule applies: a repository-scoped
override beats a global one.

pop is a personal tool. An operator must always be able to overrule a team's
choice on their own machine, and giving them one place to do it is what frees
every layer beneath from having to encode "and the human must be able to win."

### 3. Two scopes: repository and global

There is no checkout scope. `trunk` was its only member and becomes a
repository-scoped **path value** — `trunk = "<path>"` — rather than a boolean
marking one checkout's block. The repository has one fork base; saying so by
flagging one of its checkouts was a fact about the repository wearing a
per-checkout syntax. `pop tasks register --trunk <path>` already takes the path,
so the flag was the honest half and the storage was the odd one out.

### 4. Every leaf is overridable by default

ADR-0202's `override:"<scope>"` tag inverts: exposure is the default and
`overridable` is opt-out. Two keys are marked not overridable, both because they
shape *where config comes from* rather than holding a value: `includes`, which
selects the files that merge and so cannot be decided from a layer above them;
and `repo`, which is a scope selector spelled as a table — under decision 3 its
leaves are ordinary keys at repository scope, so the node itself has nothing of
its own to override.

Tables are never override units. Only leaves are, because the unit is one key's
whole value (ADR-0202 decision 2) and overriding a table wholesale silently
drops every sub-key the human did not retype.

### 5. The pop-written cut is intent versus record, and `config.runtime.toml` is deleted

What matters about a pop-written value is not that pop wrote it but whether it
**states intent** or **records what happened**. Statements go to the override
layer; records are gap-fillers at the bottom of their scope.

Every writer of `config.runtime.toml` is therefore reclassified:

| Section | Writer | Disposition |
| --- | --- | --- |
| `[repo_settings."<id>"]` | `pop config repo set` | statement → override layer |
| repo `trunk` | `--trunk` | statement → override layer |
| `[integrations].skills` | `--no-<component>` | statement → override layer |
| `[workbench.preferred]` | `ctrl+w` | deleted (decision 6) |

Nothing remains, and the file goes with them. ADR-0202's two-pop-written-files
cost is retired rather than paid forever, and ADR-0150's hand-authored /
pop-written split is replaced by this sharper cut — its locational rule (pop
never writes the config dir) survives untouched.

**Convention memory is the only gap-filler pop holds.** It stays above global
declarations, per decision 1: a derivation about *this repository* is more
specific than a preference stated for *every* repository, and specificity is the
law.

### 6. One destination, two front-ends

`ctrl+w` is deleted, as `alt+a` was before it and for the same reason ADR-0202
gave: a bare modifier chord that teaches nobody it exists. `preferred_workbench`
is edited in the Config dashboard, which already opens from the same three hosts
via `alt+c`. `pop workbench prefer` remains as the non-interactive twin, so the
key drops from three writers to two front-ends over one layer.

The Config dashboard is the only **interactive** writer of the override layer,
not the only writer. `--trunk`, `--no-<component>`, `pop config repo set` and
`pop workbench prefer` write the same layer through the same validation gate,
because a bare repository's first managed register and a scripted integrate
cannot open a TUI. This collects ADR-0202 decision 15's deferral of a
non-interactive setter, which was deferred rather than rejected. What the model
forbids is a second *destination*, never a second front-end.

### 7. The override layer is one file, shaped like `config.toml`

Global keys at the top, `[repo."<id>"]` blocks below, keyed by **Repository
identity** so every worktree of a repository reads one answer (ADR-0191's
keying, kept). A human who can read `config.toml` can read it on sight, and pop
stays at one written config file rather than three.

### 8. One editor for settings and conventions, contested keys first

The Config dashboard gains conventions as rows beside config leaves: the right
pane already answers the same question for both — what is in force, which layer
produced it, what it stands on — differing only in that a key resolves to one
value and a convention to a labelled stack. `$EDITOR` opens in place either way.

A **Contested key** — one that more than one layer holds a value for — sorts to
the top of the list and carries a marker. "What did I customize, and what is
quietly fighting it" is the question the surface exists to answer, and sorting
answers it without a second view.

### 9. `pop repo conventions` becomes `pop conventions`

`pop repo` held exactly one child, and every convention is a repository
convention, so the `repo` segment selected nothing. Dropping it makes `pop
config` and `pop conventions` siblings at the top level, which is the mental
model this record exists to reach. The `repo` in `pop config repo` is a
different word — it selects a scope — and keeping both would imply a `pop
conventions global` that does not exist.

### 10. The plan-time snapshot stays `commits`-only

ADR-0207's surviving rule — a convention resolves once per Task set at plan
time, snapshots into the manifest, and the executor commits the rendered
subjects verbatim — is not generalized. It is drift-proof and therefore also
correction-proof, which is right for commit grammar (a set's history should be
internally consistent) and wrong for verification, where noticing mid-drain that
the Verifier is judging the wrong thing should reach the next run. Verification
re-resolves per run and stays fully agentic; enforcement there is not achievable
mechanically and the mitigation is running it on capable agents.

## Considered options

- **Tier-first ordering** — sort by strength (override, declaration, derived,
  default) and use scope only within a tier. Rejected: it puts a repository-scoped
  derivation *below* a global hand-authored preference, which throws away
  specificity exactly where it is most informative. It would have moved
  Convention memory beneath `~/.agents/docs/<kind>.md`, so pop's derivation about
  one repository would lose to a preference stated for all of them.
- **Keeping ADR-0083's authorship-first law.** Rejected: it exists to guarantee
  the human can win, and decision 2 guarantees that better and in one place. Left
  alone, it keeps producing the shadowing surprise for every key with both a
  global and a repository home.
- **Killing the non-interactive writers outright.** Considered seriously, since
  "one editor" is cleaner to state. Rejected: a bare repository cannot complete
  its first managed register without `--trunk`, and integrate would stop being
  scriptable. The confusion was never two front-ends; it was two destinations.
- **Keeping checkout scope for `trunk`.** Rejected in decision 3. A third scope
  with one member means every key in the dashboard shows a slot it cannot use.
  It is additive if a second checkout-scoped key ever appears.
- **Treating `trunk` as a structural key with no scope at all.** Genuinely close,
  since it is machine topology rather than preference. Rejected because it *is* a
  per-repository value a human states and may want to change, and the override
  layer is where stated values live; marking it unreachable would leave `--trunk`
  as its only writer, which is the single-front-end shape decision 6 rejects.
- **A separate conventions dashboard.** Rejected: ADR-0202 decision 10 made the
  config editor a self-contained component precisely so one surface could serve
  three unrelated hosts, and a second modal means a second chord, a second host
  contract and three hosts to update — to ask the same question about a different
  value type.

## Consequences

- **ADR-0083's precedence law is superseded; its shared-schema decision
  survives.** `.pop/config.toml` and `[repo."<path>"]` still decode one key set,
  and repo scope stays curated. Only the ordering changes — and it changes back
  to the law ADR-0077 stated before it.
- **A team's committed `.pop/config.toml` now beats a personal global
  `config.toml`.** This is the behaviour change a user will notice first. It is
  the intended one: the specific statement wins, and disagreeing is one keypress
  in the Config dashboard rather than a hand-authored block per checkout.
- **A legacy `[repo."<path>"] trunk = true` no longer means anything**, decision
  3 having made trunk a path value. It needs a read-path fold to the new shape or
  a warn-and-ignore with the new spelling named, in the manner of the flat
  `.pop.toml` retirement (ADR-0137).
- **`config.runtime.toml` is deleted and its live contents migrate.** Anyone who
  has pressed `ctrl+w`, run `--trunk`, opted a component out, or set a turn cap
  has entries in it. Three of the four sections move to the override layer, where
  they now outrank the hand-authored values they used to lose to — a rank
  inversion for existing installs, and the one place this decision changes what
  an untouched machine resolves to.
- **`preferred_workbench`'s three-valued explicit-none logic can be
  re-examined.** ADR-0078 and ADR-0083 built it because a runtime entry could no
  longer beat a hand-authored value above it. That constraint is gone with the
  layer; whether the logic is still earning its keep is a separate question this
  record deliberately does not answer.
- **Overrides still travel with neither a machine nor a clone.** Repository-scoped
  ones are keyed by identity in a data-dir file, so a teammate gets none of them.
  A setting a team should share belongs in `.pop/config.toml` or
  `docs/agents/<kind>.md`, which is now a meaningful choice rather than a losing
  one.
- **Every host contract of ADR-0202 decision 11 still binds**, and the editor
  reaches more keys than the four it was built for, so a host that fails to
  suspend its keys now does so over a much larger surface.
- **ADR-0198's reach is unaffected** and becomes more useful: a key declaring
  what it actually touches is worth more when every key is settable from one
  surface.
