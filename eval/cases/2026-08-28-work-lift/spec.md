## Parent

`docs/adr/0241-a-repository-pass-lifts-the-work-that-lives-where-the-pane-stands.md`,
decisions 8, 9 and 10.

## What to build

The Work dashboard's derived row ordering is called a **Work lift** everywhere, and
"pin" is left to the two manual senses the same product already spells that way (the
Pinned action menu, and the monitor dashboard's Following).

A Work view preset grants the behaviour by declaring `lift = true`. A config someone
already wrote saying `pin = true` keeps working exactly as before — the old spelling is
a permanent, silent alias, warning about nothing — but `lift` is the only spelling pop
itself writes or documents.

Nothing about *when* rows lift changes in this slice. This is the vocabulary moving, and
it goes first so the passes built on top of it are born with the right name.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

The behaviour and its grant:

- `config/work_view_presets.go` — the `Pin` field and its `desc` tag, the key allowlist
  that decides which preset keys are recognised, the shipped `active` preset that is the
  only one declaring it, and the decode arm that rejects a non-bool. The alias belongs in
  that decode arm: accept either key, prefer `lift` when both appear.
- `work/snapshot.go` — `SnapshotOptions.PinPane` and the `pinAttributed` function that
  performs the lift. Note the function's own local variable is already called `lifted`.
- `work/kind.go` — `Container.Pinned`, roughly line 155.
- `dashboard/dashboard.go` — the `Pinned:` list option around line 1068, and
  `preset.Pin = active.Pin` around line 2803.
- `ui/list.go` — `renderPrefix` and the `Pinned` option it reads for the `▸` cell.

Tests carrying the word in their names or filenames: `dashboard/pane_pin_test.go`,
`dashboard/pane_pin_rebuild_test.go`, `dashboard/pane_bound_checkout_test.go`,
`dashboardshell/pane_pin_test.go`, `cmd/work_status_pin_test.go`, and in
`config/work_view_presets_test.go` the two named `...DeclaresPin` and
`...PresetPinDecodes...`. Rename the files along with the identifiers.

**Do not rename these — they are different words that survived a grep:**
`store/drains.go`'s `ExhaustedPinned` (the retired Pinned quota backoff),
`dashboard/mute_menu.go`'s "pinned first" (about date ordering),
`tasks/agent_*.go`'s "pinned model" (a hand-set `--model`), and
`integrate/grill_overlay_drift_test.go`'s upstream drift pin.

Verify with: `go build ./... && go vet ./... && go test ./...`

## Type

AFK

## Acceptance criteria

- [x] A preset declaring `lift = true` grants the behaviour, and the shipped `active`
      preset is the only shipped one that declares it
- [x] A preset declaring `pin = true` grants exactly the same behaviour, silently — no
      warning, no config finding, no deprecation notice
- [x] A preset declaring neither lifts nothing, and one declaring a non-bool for either
      spelling is a config finding that does not fail the load
- [x] No identifier, filename or comment in the tree calls the derived behaviour a pin,
      and the four unrelated "pin" senses listed in Orientation are untouched
- [x] `go build ./... && go vet ./... && go test ./...` all pass

## Blocked by

- None - can start immediately

## Parent

`docs/adr/0241-a-repository-pass-lifts-the-work-that-lives-where-the-pane-stands.md`,
decisions 1 to 6.

## What to build

Open the Work dashboard from a shell standing anywhere in a repository, and that
repository's Task sets lift — even when no Task set is bound to the checkout you are
actually standing in.

This is the reported bug. A repository whose set is bound to its trunk currently gives
nothing to a shell in a sibling feature worktree, because the weakest attribution rung
asks which *bound checkout contains this directory* and a sibling worktree is inside
none of them. A set with no binding at all is unreachable by locality from anywhere,
including its own trunk.

So attribution gains a third and weakest pass, keyed on the repository rather than on a
checkout. It is reached only when the passes above it are silent, so a shell standing in
a checkout that really does have a set bound to it still gets exactly today's answer and
today's ordering — the new pass never competes with a better one. When it is reached, it
answers with every Task set in that repository's Task storage, bound or not.

Two structural points that are the substance of this slice, not incidentals:

- The pass **merges** across Work kinds where the two passes above it stop at the first
  kind that answers. Its meaning is plural — *the work of mine that lives here* — where a
  pane tag means *this pane is that work*, which is singular. Wiring a second kind into a
  first-hit pass would guarantee that kind never answers. Nothing but Task sets answers
  yet; the merge is what makes the next slice possible rather than dead code.
- The pane's repository is resolved **once, at launch**, and carried as one more pane
  fact beside the ones already read there. It must not be asked during a snapshot build:
  the git memo's lifetime is one load and the dashboard rebuilds every two seconds, so
  asking there forks roughly eighteen hundred times an hour for an answer that cannot
  change, since the pane's directory is read once and never re-read.

Two identities decide the match, and both already exist: a repository is its git common
directory, which is the identity Task storage is keyed under and is true by construction
for a sibling worktree. The project name was rejected — it is a config label two
repositories can collide on.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

The ladder and the seam:

- `work/attribute.go` — `PaneFacts`, `Attribution`, the `PaneAttributor` and
  `PaneNeighbourhoodAttributor` seams obtained by type assertion, and `AttributePane`,
  whose two passes over `kindsInPrecedence` are what the third pass joins. The merge is
  the one place this function is not first-hit; comment it as such.
- `work/attribute.go`'s `PaneFacts` is a comparable struct and `Empty()` compares it
  against its zero value — adding a string field is fine, but check that assumption
  holds after the edit.

The launch-time fact:

- `dashboard/panefacts.go` — `LaunchPaneFacts`, currently taking only a `tmux.Tmux` and
  making one `display-message` round-trip. The git call goes here, beside it.
- `dashboardshell/shell.go` around line 123 — `launchPaneFacts(d *drain.Deps)`, the sole
  caller, which already holds `Tasks.Git`.
- `internal/deps/git.go` for the `Git` seam, `tasks/deps.go` around line 35 for where it
  hangs off `tasks.Deps`. The field goes on `work.PaneFacts`, never on
  `internal/tmux.PaneFacts` — that package owns tmux knowledge and nothing else.

The Task-set answer:

- `tasks/setkind/attribute.go` — `recordPanes` (which today only appends to `k.bound`
  when a binding exists, and is where the unbound sets have to start being remembered),
  `AttributePaneNeighbourhood`, `attributeBoundCheckoutAt`, `rankBoundCandidates`,
  `canonPath` and `pathUnder`.
- `repogroup/repogroup.go` around line 46 — `Group.RepoCommonDir`, already carried on
  every group, is the left-hand side of the match.

Read-surface behaviour to preserve: `dashboard/status_render.go` builds with empty pane
facts, so `pop work status` must still lift nothing and must not fork git.

Verify with:
`go build ./... && go vet ./work/... ./dashboard/... ./dashboardshell/... ./tasks/... && go test ./work/... ./dashboard/... ./dashboardshell/... ./tasks/...`
then `make test` before finishing.

## Type

AFK

## Acceptance criteria

- [x] A pane in a sibling worktree of a checkout that a Task set is bound to lifts that
      set, where today it lifts nothing
- [x] A pane in a checkout that a set is bound to gets the same answer and the same
      leading order it gets today — the repository pass is not consulted
- [x] A pane carrying a Task-set tag gets the tag answer, unchanged, whatever the
      repository holds
- [x] A repository's Task sets lift whether or not they carry a Worktree binding
- [x] A pane standing outside any repository, or in a repository pop knows no work for,
      lifts nothing and reports nothing
- [x] The repository pass concatenates the answers of every kind that has work there,
      rather than stopping at the first — asserted with two answering kinds, so the
      arity is pinned by test and not by the next slice
- [x] The pane's repository is resolved once at launch: a dashboard left open over many
      rebuilds forks git no more than it does at launch, asserted against a counting git
      seam
- [x] `pop work status` lifts nothing and forks no git for attribution
- [x] A row the active preset hides is still not lifted, and the preset is not widened
- [x] `make test` passes

## Blocked by

- 01-lift-is-not-a-pin

## Parent

`docs/adr/0241-a-repository-pass-lifts-the-work-that-lives-where-the-pane-stands.md`,
decisions 2, 3 and 7.

## What to build

A live Map lifts for a shell standing in the repository it belongs to, beside that
repository's Task sets rather than instead of them.

A Map is Trunk-rooted and its only attribution today is the Work session stamp, which
fires solely inside `pop-map-<id>`. So an ordinary editor shell in the repository has
exactly the same blind spot for a Map that the previous slice just closed for Task sets:
you are standing in the work and the dashboard does not know it.

The pass and its merge already exist after the previous slice. What this slice adds is
the Map kind answering it, and the proof that the merge is real — a repository holding
both a Task set and a Map lifts both, in kind precedence order, where a first-hit pass
would have silently dropped the Maps.

Routines stay out, and that is a distinction rather than an oversight: a routine is
project-scoped with no container-level locality to narrow it, its pane is short-lived,
and page B has no equivalent of the preset narrowing that keeps this pass to a row or
two. A routine names a schedule; a Task set and a Map each name a definite piece of work
that lives in a definite repository. Leave the Routine kind answering the tag pass only,
and say why in a comment so the next reader does not "fix" it.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `wayfinder/workkind.go` — the Map's `work.Kind` adapter, which already implements the
  tag/stamp attribution seam; the repository seam goes beside it.
- A Map has no checkout of its own, so its repository comes from the Trunk it is rooted
  at — the same resolution `pop map open` and the Map session use. `wayfinder/session.go`
  and the Map-session parts of `wayfinder/` are where that resolution already lives;
  reuse it rather than re-deriving a trunk here.
- `work/attribute.go` — `kindsInPrecedence` is what puts Task sets ahead of Maps inside
  the merged answer.
- `routine/attribute.go` — leave it answering the tag pass only; this is where the
  comment explaining the asymmetry belongs.
- `work/conformance_test.go` — the per-kind conformance table is the natural place to
  pin which kinds answer which attribution seam.

Verify with:
`go build ./... && go vet ./work/... ./wayfinder/... ./routine/... ./dashboard/... && go test ./work/... ./wayfinder/... ./routine/... ./dashboard/...`
then `make test` before finishing.

## Type

AFK

## Acceptance criteria

- [x] A pane standing anywhere in a repository lifts that repository's live Maps
- [x] A repository holding both a Task set and a Map lifts both from one pane, with the
      Task set rows ahead of the Map rows
- [x] A pane inside a Map's own session still gets the stamp answer, unchanged — the
      stronger pass is not overtaken by the weaker one
- [x] A Routine never lifts by locality, and a comment records why that is deliberate
- [x] `make test` passes

## Blocked by

- 02-a-repository-pass-lifts-its-task-sets

## What to build

Resolve the verification findings below. An independent Verifier judged this task set's completed work at 73fb8605be32 and returned FIXABLE: the acceptance criteria are not yet met, but the gaps are ones an agent can close. Make the changes needed to satisfy the set's acceptance criteria. Do not edit the other task specs — they are stable, task-scoped intent; fix the code and artifacts they describe.

This is remediation cycle 1.

## Findings

**Gates (all green, run in this checkout at 73fb860, clean tree)**
- `go build ./...` → exit 0
- `go vet ./...` → exit 0
- `go test ./...` (the whole-tree gate `make test` runs) → exit 0
- Fresh scoped re-run, no cache: `go test -count=1 ./work/... ./dashboard/... ./dashboardshell/... ./tasks/setkind/... ./wayfinder/... ./routine/... ./config/... ./ui/... ./cmd/...` → exit 0

**01-lift-is-not-a-pin — one criterion fails**

Criterion "No identifier, filename or comment in the tree calls the derived behaviour a pin" is unmet. Two comments about the lift survived the rename, both in files the diff never touched (so neither is caught by reading the diff alone):

- `dashboardshell/shell.go:45` — "…kept for the page the toggle builds later — **the pin** is per page, the tmux read is not." This is the same sentence, about the same fact, as the `launchPaneFacts` doc comment eleven lines of diff away that *was* corrected to "the lift is simply computed per page".
- `dashboard/status_render.go:68` — "Empty pane facts, so nothing is attributed and **nothing pins** whatever the preset declares…" — the grant the preset declares is now `lift`, and `BuildStatusTables` is exactly the surface task 02 tests as lifting nothing.

The four unrelated senses are correctly untouched: `store/drains.go`'s `ExhaustedPinned`, `dashboard/mute_menu.go`'s "pinned first", the "pinned model" family in `tasks/`, and `integrate/grill_overlay_drift_test.go`'s drift pin. The monitor dashboard's Following 📌 (`ui/dashboard.go:1663,1683`) and the Pinned action menu references (`dashboard/dashboard.go:1660`) are the two allowed manual senses. All test files and identifiers renamed (`pane_lift_test.go`, `pane_lift_rebuild_test.go`, `dashboardshell/pane_lift_test.go`, `cmd/work_status_lift_test.go`, `Container.Lifted`, `Opts.Lifted`, `BuildOptions.LiftPane`, `liftAttributed`).

The other three criteria of task 01 hold: `lift = true` grants it and `Lift: true` appears on exactly one shipped preset (`active`, `config/work_view_presets.go:133`), pinned by `TestOnlyTheShippedActivePresetDeclaresLift`; `pin` stays in the key allowlist and decodes into `Lift` before `lift` is read, so it grants identically, `lift` wins when both appear, and no finding is emitted for a well-formed `pin` entry (`TestPinIsASilentAliasForLift`, `TestLiftWinsWhenAPresetDeclaresBothSpellings`); a non-bool for either spelling appends to `p.problems` and does not fail the load.

**02-a-repository-pass-lifts-its-task-sets — all criteria met**

`work.PaneRepositoryAttributor` is a third seam rather than a rung inside `AttributePaneNeighbourhood`, so any checkout answer outranks any repository answer; `attributeRepository` merges across kinds and is commented as the one non-first-hit pass. `recordPanes` now records every row into `k.inRepo` and only bound rows into `k.bound`, both sharing one `sortRow`. The repository is resolved in `LaunchPaneFacts` via `tasks.ResolveRepositoryIdentity` and carried on `work.PaneFacts.RepoCommonDir` (not on `internal/tmux`'s facts); `PaneFacts` stays comparable and `Empty()` still short-circuits `AttributePane`. `dashboardshell/shell.go:125` is the only production caller.

Every criterion is asserted end-to-end in `dashboard/pane_repository_test.go` against real worktrees: sibling-worktree lift, bound-checkout answered by the checkout rung alone with the third set left where the sort had it, tagged pane unchanged from either directory, bound-and-unbound sets alike, silence outside a repository and in an unknown one, `TestThePanesRepositoryIsResolvedOnceAtLaunch` (counting git seam over both `Tasks.Git` and `Project.Git`, one `--git-common-dir` fork at launch and none across ten rebuilds, with the lift re-asserted so the ceiling is not cheap for the wrong reason), `TestWorkStatusLiftsNothingAndForksNoGitForAttribution`, and `TestARepositoryLiftDoesNotWidenThePreset`. The two-answering-kind arity is pinned kind-independently in `work/attribute_repository_test.go:71`.

**03-maps-lift-with-their-repository — all criteria met**

`MapKind.AttributePaneRepository` answers from `panes.inRepo`, filed under the group's `RepoCommonDir` (the Trunk's repository, not a re-derived trunk), gated by `liveForLocality` (BROKEN and abandoned out, archived still a candidate since row visibility is the preset's business). `ref.Kinds()` orders task set before map, so the merge puts set rows first — asserted by `TestARepositoryLiftsItsTaskSetsAndItsMapsTogether`, with a Map-only case beside it. `TestMapSessionBeatsTheRepositoryPass` pins the stamp answer surviving. `routine/attribute.go` gains the comment explaining the asymmetry and ending "Do not wire `AttributePaneRepository` here", and `work/conformance_test.go` now pins the per-kind pass table (`tags` for Routine, `tags`+`repository` for Map, all three for the Task set).

## Acceptance criteria

- [x] Every finding above is resolved and the task set's acceptance criteria are met
