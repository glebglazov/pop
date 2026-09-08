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
